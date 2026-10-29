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

// Scorer gives one MetricX score per pair, in order.
type Scorer interface {
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
}

// Entry is one ledger line: one try for one message.
type Entry struct {
	Time            string        `json:"time"`
	Run             string        `json:"run"`
	ID              string        `json:"id"`
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
}

// Chosen is the kept translation of one message.
type Chosen struct {
	ID         string   `json:"id"`
	Text       string   `json:"text"`
	Score      *float64 `json:"score,omitempty"` // nil when copied
	Candidates int      `json:"candidates"`
}

// Result is <lang>.json.
type Result struct {
	Run      string   `json:"run"`
	Lang     string   `json:"lang"`
	Commit   string   `json:"en_commit"`
	Model    string   `json:"model"`
	K        int      `json:"k"`
	Messages []Chosen `json:"messages"`
	Missing  []string `json:"missing"` // ids with no valid candidate
}

// Run translates msgs, scores them and writes one ledger line per try.
func Run(ctx context.Context, cfg Config, msgs []i18n.Message, tr Translator, sc Scorer, ledger io.Writer) (Result, error) {
	if cfg.K < 1 || cfg.Workers < 1 {
		return Result{}, fmt.Errorf("k and workers must be 1 or more")
	}
	now := func() string { return time.Now().UTC().Format(time.RFC3339) }
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
				e := Entry{Time: now(), Run: cfg.Run, ID: m.ID, Lang: cfg.Lang.Code, Try: j.try, Source: m.Text,
					Masked: mk.Text, Settings: cfg.Settings, Commit: cfg.Commit}
				sys, user := muse.Prompt(cfg.Lang.Name, cfg.Lang.Code, m.ID, mk.Text)
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
	for e := range out {
		entries = append(entries, e)
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	for i, m := range msgs {
		if onlyTags(masks[i].Text) {
			entries = append(entries, Entry{Time: now(), Run: cfg.Run, ID: m.ID, Lang: cfg.Lang.Code, Try: 0, Source: m.Text,
				Masked: masks[i].Text, Text: m.Text, Copied: true, Settings: cfg.Settings, Commit: cfg.Commit})
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
		}
	}
	// the ledger: message order, then try
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
	enc := json.NewEncoder(ledger)
	enc.SetEscapeHTML(false)
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			return Result{}, err
		}
	}
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
