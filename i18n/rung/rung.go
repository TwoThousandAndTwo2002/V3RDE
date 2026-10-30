// Package rung runs rung 1 of the translation ladder (L4c): for each English
// message, k translations from Muse Spark Open (separate calls, n = 1 each),
// the tags checked and restored, every candidate scored by MetricX-24 in QE
// mode (0..25, lower is better), the best one kept, and every try written to a
// ledger. With k = 1 it is one translation per message, scored.
package rung

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"

	"v3rde/i18n"
	"v3rde/i18n/mask"
	"v3rde/i18n/muse"
)

// Translator is the model (muse.Client, or a fake in tests).
type Translator interface {
	Chat(ctx context.Context, system, user string) (muse.Reply, error)
}

// Pair is one source and translation for the scorer.
type Pair struct {
	Source     string `json:"source"`
	Hypothesis string `json:"hypothesis"`
}

// Scorer gives one MetricX score per pair, in order. Check runs before the
// first request to the model, so a scorer that cannot run costs no requests.
type Scorer interface {
	Check(ctx context.Context) error
	Score(ctx context.Context, pairs []Pair) ([]float64, error)
}

// Config is one run.
type Config struct {
	Run      string // run id, in the ledger
	Lang     i18n.Language
	Keep     []string
	K        int // candidates per message (rung 1: 1)
	Workers  int
	Settings muse.Settings
	Commit   string // the ui commit of en.json
	// L4h: the context of a text for its prompt (nil: the plain prompt), the
	// dedupe key (nil: the English text), and the name of the prompt.
	About  func(id string, mk mask.Masked) string
	Key    func(m i18n.Message) string
	Prompt string
}

// Tags maps each protected part of a masked text to its tag: "{plural}" -> "<x1/>".
func Tags(mk mask.Masked) map[string]string {
	t := map[string]string{}
	for k, o := range mk.Orig {
		if _, ok := t[o]; !ok {
			t[o] = fmt.Sprintf("<x%d/>", k+1)
		}
	}
	return t
}

// Entry is one ledger line (kind "try"): one try for one message text. It is
// written when the answer comes in, before the scoring.
type Entry struct {
	Kind            string        `json:"kind"`
	Time            string        `json:"time"`
	Run             string        `json:"run"`
	ID              string        `json:"id"`
	Also            []string      `json:"also,omitempty"` // other ids with the same English text
	Lang            string        `json:"lang"`
	Try             int           `json:"try"`
	Source          string        `json:"source"`
	Masked          string        `json:"masked"`
	Raw             string        `json:"raw,omitempty"`
	Text            string        `json:"text,omitempty"`
	Error           string        `json:"error,omitempty"`
	Copied          bool          `json:"copied,omitempty"` // only keep names: no model call
	Score           *float64      `json:"score,omitempty"`
	ReasoningTokens int           `json:"reasoning_tokens,omitempty"`
	Settings        muse.Settings `json:"settings"`
	Commit          string        `json:"en_commit"`
	Prompt          string        `json:"prompt,omitempty"`
}

// ScoreLine is one ledger line (kind "score"), written after the scoring.
type ScoreLine struct {
	Kind  string  `json:"kind"`
	Time  string  `json:"time"`
	Run   string  `json:"run"`
	ID    string  `json:"id"`
	Lang  string  `json:"lang"`
	Try   int     `json:"try"`
	Score float64 `json:"score"`
}

// Chosen is the kept translation of one message.
type Chosen struct {
	ID         string   `json:"id"`
	Text       string   `json:"text"`
	Score      *float64 `json:"score,omitempty"` // nil when copied
	Candidates int      `json:"candidates"`
	SameAs     string   `json:"same_as,omitempty"` // the id whose translation this one reuses (same English text)
}

// Result is <lang>.json.
type Result struct {
	Run      string   `json:"run"`
	Lang     string   `json:"lang"`
	Commit   string   `json:"en_commit"`
	Model    string   `json:"model"`
	K        int      `json:"k"`
	Messages []Chosen `json:"messages"`
	Missing  []string `json:"missing"`                 // ids with no valid candidate
	Previous []string `json:"previous_runs,omitempty"` // earlier runs kept by Merge
	Dedupe   bool     `json:"dedupe"`                  // each English text translated once (L4f)
	Prompt   string   `json:"prompt,omitempty"`        // plain or context-v1 (L4h); empty: plain
}

func promptOf(r Result) string {
	if r.Prompt == "" {
		return muse.PromptPlain
	}
	return r.Prompt
}

// Collapse makes an earlier result (made before dedupe) follow the dedupe rule
// with no new request: for each English text that several ids share, the
// translation with the lowest score is kept for all of them (a tie keeps the
// first id). The ids that reuse it get SameAs.
func Collapse(r Result, msgs []i18n.Message) Result {
	by := map[string]Chosen{}
	for _, c := range r.Messages {
		by[c.ID] = c
	}
	best := map[string]Chosen{} // text -> kept translation
	for _, m := range msgs {
		c, ok := by[m.ID]
		if !ok {
			continue
		}
		b, seen := best[m.Text]
		if !seen || (c.Score != nil && (b.Score == nil || *c.Score < *b.Score)) {
			best[m.Text] = c
		}
	}
	out := r
	out.Messages, out.Missing, out.Dedupe = nil, []string{}, true
	for _, m := range msgs {
		b, ok := best[m.Text]
		if !ok {
			out.Missing = append(out.Missing, m.ID)
			continue
		}
		c := b
		c.SameAs = ""
		if c.ID != m.ID {
			c.ID, c.SameAs = m.ID, b.ID
		}
		out.Messages = append(out.Messages, c)
	}
	return out
}

// Complete: r has a translation for every message of msgs, with this commit,
// model, k and prompt, made with dedupe. cmd/translate skips such a language
// unless -force.
func Complete(r Result, msgs []i18n.Message, commit, model string, k int, prompt string) bool {
	if len(r.Missing) != 0 || r.Commit != commit || r.Model != model || r.K != k || !r.Dedupe || promptOf(r) != prompt {
		return false
	}
	have := map[string]bool{}
	for _, c := range r.Messages {
		have[c.ID] = true
	}
	for _, m := range msgs {
		if !have[m.ID] {
			return false
		}
	}
	return len(msgs) > 0
}

// Todo: the messages of msgs that r has no translation for (also the texts that
// are new in en.json since r was made).
func Todo(r Result, msgs []i18n.Message) []i18n.Message {
	have := map[string]bool{}
	for _, c := range r.Messages {
		have[c.ID] = true
	}
	var out []i18n.Message
	for _, m := range msgs {
		if !have[m.ID] {
			out = append(out, m)
		}
	}
	return out
}

// Merge adds the messages of a resume run (add) to an earlier result (old):
// the order of msgs, a new answer for an id replaces the old one, and the
// missing ids are the ones that neither run has. Old must be the same
// language, en.json commit, model and k.
func Merge(old, add Result, msgs []i18n.Message) (Result, error) {
	if old.Lang != add.Lang || old.Commit != add.Commit || old.Model != add.Model || old.K != add.K || old.Dedupe != add.Dedupe || promptOf(old) != promptOf(add) {
		return Result{}, fmt.Errorf("resume: the earlier result is %s/%s/%s/k=%d, this run is %s/%s/%s/k=%d",
			old.Lang, old.Commit, old.Model, old.K, add.Lang, add.Commit, add.Model, add.K)
	}
	by := map[string]Chosen{}
	for _, c := range old.Messages {
		by[c.ID] = c
	}
	for _, c := range add.Messages {
		by[c.ID] = c
	}
	out := add
	out.Messages, out.Missing = nil, []string{}
	out.Previous = append(append([]string{}, old.Previous...), old.Run)
	for _, m := range msgs {
		if c, ok := by[m.ID]; ok {
			out.Messages = append(out.Messages, c)
		} else {
			out.Missing = append(out.Missing, m.ID)
		}
	}
	return out, nil
}

// Run translates each English text of msgs once (the first id with that text;
// the other ids reuse its translation), scores the answers, and writes the
// ledger as it goes. The scorer is checked before the first request.
func Run(ctx context.Context, cfg Config, msgs []i18n.Message, tr Translator, sc Scorer, ledger io.Writer) (Result, error) {
	if cfg.K < 1 || cfg.Workers < 1 {
		return Result{}, fmt.Errorf("k and workers must be 1 or more")
	}
	if err := sc.Check(ctx); err != nil {
		return Result{}, fmt.Errorf("the scorer cannot run, so no request was sent: %v", err)
	}
	key := cfg.Key
	if key == nil {
		key = func(m i18n.Message) string { return m.Text }
	}
	first := map[string]string{}  // key -> first id
	also := map[string][]string{} // first id -> other ids
	var uniq []i18n.Message
	for _, m := range msgs {
		if id, ok := first[key(m)]; ok {
			also[id] = append(also[id], m.ID)
			continue
		}
		first[key(m)] = m.ID
		uniq = append(uniq, m)
	}
	res, err := runUnique(ctx, cfg, uniq, also, tr, sc, ledger)
	if err != nil {
		return Result{}, err
	}
	by := map[string]Chosen{}
	for _, c := range res.Messages {
		by[c.ID] = c
	}
	res.Messages, res.Missing, res.Dedupe, res.Prompt = nil, []string{}, true, cfg.Prompt
	if res.Prompt == "" {
		res.Prompt = muse.PromptPlain
	}
	for _, m := range msgs {
		rep := first[key(m)]
		c, ok := by[rep]
		if !ok {
			res.Missing = append(res.Missing, m.ID)
			continue
		}
		if m.ID != rep {
			c.ID, c.SameAs = m.ID, rep
		}
		res.Messages = append(res.Messages, c)
	}
	return res, nil
}

func runUnique(ctx context.Context, cfg Config, msgs []i18n.Message, also map[string][]string, tr Translator, sc Scorer, ledger io.Writer) (Result, error) {
	now := func() string { return time.Now().UTC().Format(time.RFC3339) }
	enc := json.NewEncoder(ledger)
	enc.SetEscapeHTML(false)
	var entries []Entry
	type job struct{ m, try int }
	jobs := make(chan job)
	out := make(chan Entry)
	var wg sync.WaitGroup
	masks := make([]mask.Masked, len(msgs))
	for i, m := range msgs {
		mk, err := mask.Mask(m.Text, cfg.Keep)
		if err != nil {
			return Result{}, fmt.Errorf("%s: %v", m.ID, err)
		}
		masks[i] = mk
	}
	for w := 0; w < cfg.Workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				m, mk := msgs[j.m], masks[j.m]
				e := Entry{Kind: "try", Time: now(), Run: cfg.Run, ID: m.ID, Also: also[m.ID], Lang: cfg.Lang.Code, Try: j.try, Source: m.Text,
					Masked: mk.Text, Settings: cfg.Settings, Commit: cfg.Commit}
				about := ""
				if cfg.About != nil {
					about = cfg.About(m.ID, mk)
				}
				sys, user := muse.PromptWith(cfg.Lang.Name, cfg.Lang.Code, m.ID, mk.Text, about)
				e.Prompt = cfg.Prompt
				r, err := tr.Chat(ctx, sys, user)
				e.ReasoningTokens = r.ReasoningTokens
				if err != nil {
					e.Error = err.Error()
				} else {
					e.Raw = r.Text
					if txt, err := mk.Restore(r.Text); err != nil {
						e.Error = "tags: " + err.Error()
					} else {
						e.Text = txt
					}
				}
				out <- e
			}
		}()
	}
	go func() {
		for i := range msgs {
			if onlyTags(masks[i].Text) {
				continue // only keep names and values: nothing to translate
			}
			for t := 1; t <= cfg.K; t++ {
				select {
				case jobs <- job{i, t}:
				case <-ctx.Done():
				}
			}
		}
		close(jobs)
		wg.Wait()
		close(out)
	}()
	var werr error
	for e := range out {
		entries = append(entries, e)
		if werr == nil {
			werr = enc.Encode(e) // now, so a later failure keeps the paid answers
		}
	}
	if werr != nil {
		return Result{}, fmt.Errorf("ledger: %v", werr)
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	for i, m := range msgs {
		if onlyTags(masks[i].Text) {
			e := Entry{Kind: "try", Time: now(), Run: cfg.Run, ID: m.ID, Also: also[m.ID], Lang: cfg.Lang.Code, Try: 0, Source: m.Text,
				Masked: masks[i].Text, Text: m.Text, Copied: true, Settings: cfg.Settings, Commit: cfg.Commit}
			entries = append(entries, e)
			if err := enc.Encode(e); err != nil {
				return Result{}, fmt.Errorf("ledger: %v", err)
			}
		}
	}
	// score every valid model candidate in one batch
	var pairs []Pair
	var idx []int
	for k, e := range entries {
		if e.Error == "" && !e.Copied {
			pairs = append(pairs, Pair{Source: e.Source, Hypothesis: e.Text})
			idx = append(idx, k)
		}
	}
	if len(pairs) > 0 {
		scores, err := sc.Score(ctx, pairs)
		if err != nil {
			return Result{}, fmt.Errorf("score: %v", err)
		}
		if len(scores) != len(pairs) {
			return Result{}, fmt.Errorf("score: %d scores for %d pairs", len(scores), len(pairs))
		}
		for n, k := range idx {
			s := scores[n]
			entries[k].Score = &s
			if err := enc.Encode(ScoreLine{Kind: "score", Time: now(), Run: cfg.Run, ID: entries[k].ID, Lang: cfg.Lang.Code, Try: entries[k].Try, Score: s}); err != nil {
				return Result{}, fmt.Errorf("ledger: %v", err)
			}
		}
	}
	// message order, then try (for the choice below)
	pos := map[string]int{}
	for i, m := range msgs {
		pos[m.ID] = i
	}
	sort.SliceStable(entries, func(a, b int) bool {
		if pos[entries[a].ID] != pos[entries[b].ID] {
			return pos[entries[a].ID] < pos[entries[b].ID]
		}
		return entries[a].Try < entries[b].Try
	})
	// keep the lowest score (MetricX: lower is better); a tie keeps the first try
	res := Result{Run: cfg.Run, Lang: cfg.Lang.Code, Commit: cfg.Commit, Model: cfg.Settings.Model, K: cfg.K, Missing: []string{}}
	best := map[string]*Chosen{}
	count := map[string]int{}
	for _, e := range entries {
		if e.Error != "" {
			continue
		}
		count[e.ID]++
		c := best[e.ID]
		if c == nil || (e.Score != nil && c.Score != nil && *e.Score < *c.Score) {
			best[e.ID] = &Chosen{ID: e.ID, Text: e.Text, Score: e.Score}
		}
	}
	for _, m := range msgs {
		if c := best[m.ID]; c != nil {
			c.Candidates = count[m.ID]
			res.Messages = append(res.Messages, *c)
		} else {
			res.Missing = append(res.Missing, m.ID)
		}
	}
	return res, nil
}

// onlyTags: the masked text has nothing but tags and spaces or punctuation.
func onlyTags(s string) bool {
	return !hasLetter(reTagAll.ReplaceAllString(s, ""))
}
