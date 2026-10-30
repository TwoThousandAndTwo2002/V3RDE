package round

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"v3rde/i18n"
	"v3rde/i18n/judge"
	"v3rde/i18n/mask"
	"v3rde/i18n/muse"
	"v3rde/i18n/rung"
)

// Options are the flags of cmd/round.
type Options struct {
	Repo     string  // the V3RDE checkout at the deployed commit (read only)
	State    string  // the persistent state directory
	Out      string  // the published files
	Langs    string  // languages.json ("" : <repo>/i18n/languages.json)
	Seed     string  // first run: a directory of earlier results (<lang>/<lang>.json or <lang>.json); optional
	DryRun   bool    // everything but the model calls; writes nothing
	Batch    int     // texts in one call (L4k: 12)
	Workers  int     // calls at the same time (translation)
	JWorkers int     // calls at the same time for each judge
	QEMax    float64 // > 0: a MetricX QE score above this flags the text (0: scores rank only, as in L4c)
}

// Models are the model clients and the scorer. Open is called only when a
// language has texts to translate, so a round with nothing to do needs no key.
type Models struct {
	Translator rung.Translator
	Judges     []judge.Model
	Scorer     rung.Scorer
	Settings   muse.Settings
}

// Deps are what Run needs from outside.
type Deps struct {
	Open func() (Models, error)
	Now  func() time.Time
	Log  io.Writer // the progress lines
}

// Summary is what a round did.
type Summary struct {
	Tag     string
	Nothing bool     // nothing changed: no round
	Failed  []string // languages that failed (they keep their live file)
	Review  int      // texts for a person
}

// Plan is rounds/<tag>.json.
type Plan struct {
	Tag        string               `json:"tag"`
	Time       string               `json:"time"`
	Commit     string               `json:"commit"`
	Branch     string               `json:"branch,omitempty"`
	PrevTag    string               `json:"previous_round,omitempty"`
	PrevCommit string               `json:"previous_commit,omitempty"`
	Seeded     []string             `json:"seeded,omitempty"` // first run: the languages that started from earlier results
	Texts      int                  `json:"texts"`
	Added      []string             `json:"added"`
	Changed    []string             `json:"changed"`
	Removed    []string             `json:"removed"`
	Reached    []i18n.Reached       `json:"reached"`
	Stale      map[string][]string  `json:"stale_contexts,omitempty"` // missing, changed: these texts are translated with no or an old context
	Dropped    []string             `json:"stale_concepts,omitempty"` // hand-made edges of concepts.json left out of the graph
	Langs      map[string]*LangPlan `json:"languages"`
	DryRun     bool                 `json:"dry_run,omitempty"`
}

// LangPlan is one language of the plan, and its result.
type LangPlan struct {
	Work    []Why    `json:"work"`
	Removed []string `json:"removed"`
	Calls   int      `json:"calls"`
	Flagged int      `json:"flagged_round1"`
	Fixed   int      `json:"fix_round_texts"`
	Passed  int      `json:"passed"`
	Review  int      `json:"review"`
	Status  string   `json:"status"` // planned, done, failed
	Error   string   `json:"error,omitempty"`
}

// StateFile is round.json in the state: the last round.
type StateFile struct {
	Tag    string `json:"tag"`
	Commit string `json:"commit"`
	Time   string `json:"time"`
	// Failed: the languages that failed in that round; the next round tries
	// their texts again even when the app text did not change (X49b review)
	Failed []string `json:"failed,omitempty"`
}

type lang struct {
	l       i18n.Language
	live    rung.Result
	src     map[string]string
	plan    *LangPlan
	ledger  string
	r1, r2  rung.Result
	f1, f2  map[string][]string
	bad     map[string]bool   // flagged or missing in round 1
	from    map[string]string // spread: reached id -> flagged id
	err     error
	outcome map[string]Outcome
}

type runner struct {
	unscored bool // X49b review: a scorer run failed in this round (the tries kept no score)
	o        Options
	d        Deps
	m        Models
	tag      string
	commit   string
	cur      []i18n.Message
	text     map[string]string
	newG     i18n.GraphFile
	ctx      i18n.ContextFile
	about    map[string]string
	keep     []string
}

func (x *runner) logf(format string, a ...any) { fmt.Fprintf(x.d.Log, "round: "+format+"\n", a...) }

func short(c string) string {
	if len(c) > 7 {
		return c[:7]
	}
	return c
}

// Run runs one round.
func Run(ctx context.Context, o Options, d Deps) (Summary, error) {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Log == nil {
		d.Log = io.Discard
	}
	if o.Batch < 1 {
		o.Batch = 12
	}
	if o.Workers < 1 {
		o.Workers = 8
	}
	if o.JWorkers < 1 {
		o.JWorkers = 4
	}
	x := &runner{o: o, d: d}
	conf := filepath.Join(o.Repo, "i18n")
	if o.Langs == "" {
		o.Langs = filepath.Join(conf, "languages.json")
	}

	// 1. the app text at HEAD of the checkout
	var src i18n.Source
	var lists i18n.Lists
	var langs i18n.Languages
	var cf i18n.ConceptFile
	for p, v := range map[string]any{filepath.Join(conf, "source.json"): &src, filepath.Join(conf, "rules.json"): &lists,
		filepath.Join(conf, "context.json"): &x.ctx, filepath.Join(conf, "concepts.json"): &cf, o.Langs: &langs} {
		if err := i18n.ReadJSON(p, v); err != nil {
			return Summary{}, fmt.Errorf("%s: %v", p, err)
		}
	}
	commit, branch, err := i18n.HeadCommit(o.Repo)
	if err != nil {
		return Summary{}, err
	}
	x.commit, x.keep = commit, langs.Keep
	src.Commit, src.Branch = commit, branch
	files, err := i18n.ReadTree(o.Repo, src.Dir)
	if err != nil {
		return Summary{}, err
	}
	if x.cur, _, err = i18n.All(files, &lists); err != nil {
		return Summary{}, fmt.Errorf("extract: %v", err)
	}
	x.text = map[string]string{}
	for _, m := range x.cur {
		x.text[m.ID] = m.Text
	}
	x.logf("[1/7] extract: %d texts from %s/%s at %s (%s)", len(x.cur), filepath.Base(o.Repo), src.Dir, short(commit), branch)

	// 2. the graph, and the diff with the last round
	var dropped []string
	if x.newG, dropped, err = Graph(src, files, x.cur, cf); err != nil {
		return Summary{}, fmt.Errorf("graph: %v", err)
	}
	for _, d := range dropped {
		x.logf("[2/7] warning: concepts.json is older than the app text: %s (left out of the graph)", d)
	}
	x.about = map[string]string{}
	for n, c := range cf.Concepts {
		x.about[n] = c.About
	}
	var st StateFile
	_ = i18n.ReadJSON(filepath.Join(o.State, "round.json"), &st)
	var oldEn i18n.EnFile
	var oldG i18n.GraphFile
	haveState := i18n.ReadJSON(filepath.Join(o.State, "en.json"), &oldEn) == nil
	if haveState {
		if err := i18n.ReadJSON(filepath.Join(o.State, "graph.json"), &oldG); err != nil {
			return Summary{}, fmt.Errorf("state graph.json: %v", err)
		}
	}
	targets := []*lang{}
	var seeded []string
	for _, l := range langs.Languages {
		if l.Role == "source" {
			continue
		}
		lr := &lang{l: l, src: map[string]string{}, ledger: filepath.Join(o.State, "ledgers", l.Code+".ledger.jsonl")}
		if i18n.ReadJSON(filepath.Join(o.State, "live", l.Code+".json"), &lr.live) == nil {
			if err := i18n.ReadJSON(filepath.Join(o.State, "live", l.Code+".source.json"), &lr.src); err != nil {
				return Summary{}, fmt.Errorf("state live/%s.source.json: %v", l.Code, err)
			}
		}
		targets = append(targets, lr)
	}
	if !haveState {
		// first run: start from the committed results, if there are any
		var baseEn i18n.EnFile
		var baseG i18n.GraphFile
		if i18n.ReadJSON(filepath.Join(conf, "en.json"), &baseEn) == nil && i18n.ReadJSON(filepath.Join(conf, "graph.json"), &baseG) == nil {
			text := map[string]string{}
			for _, m := range baseEn.Messages {
				text[m.ID] = m.Text
			}
			for _, lr := range targets {
				if len(lr.live.Messages) > 0 { // X49b review: a first round stopped early left this language's work
					continue
				}
				r, from, ok := findSeed(o, conf, lr.l.Code)
				if !ok {
					continue
				}
				if r.Commit != baseEn.Source.Commit {
					x.logf("[2/7] seed %s: %s is for en commit %s, not %s: not used", lr.l.Code, from, short(r.Commit), short(baseEn.Source.Commit))
					continue
				}
				lr.live = r
				for _, c := range r.Messages {
					if t, ok := text[c.ID]; ok {
						lr.src[c.ID] = t
					}
				}
				seeded = append(seeded, lr.l.Code)
				x.logf("[2/7] seed %s: %d translations from %s", lr.l.Code, len(r.Messages), from)
			}
			if len(seeded) > 0 {
				oldEn, oldG = baseEn, baseG
			}
		}
	}
	im := i18n.ImpactOf(oldEn.Messages, x.cur, oldG, x.newG)
	x.logf("[2/7] graph: %d nodes, %d edges; since %s: added %d, changed %d, removed %d, reached %d",
		len(x.newG.Nodes), len(x.newG.Edges), orNone(st.Tag, oldEn.Source.Commit), len(im.Added), len(im.Changed), len(im.Removed), len(im.Reached))

	newLang := false
	for _, lr := range targets {
		if len(lr.live.Messages) == 0 && len(lr.src) == 0 {
			newLang = true
		}
	}
	// X49b review: a language that failed in the last round is tried again even when the app text did
	// not change; texts that went to a person (review) are not (T9: after two tries, a person decides)
	pending := 0
	for _, lr := range targets {
		if slices.Contains(st.Failed, lr.l.Code) {
			w, _ := WorkList(lr.src, im, x.cur)
			pending += len(w)
		}
	}
	if haveState && len(im.Added)+len(im.Changed)+len(im.Removed) == 0 && !newLang && pending == 0 {
		x.logf("nothing changed since round %s (commit %s): no translation round", st.Tag, short(st.Commit))
		if _, err := os.Stat(filepath.Join(o.Out, "index.json")); err != nil && !o.DryRun {
			live := map[string]rung.Result{}
			for _, lr := range targets {
				live[lr.l.Code] = lr.live
			}
			if err := Publish(o.Out, st.Tag, st.Commit, langs, x.cur, live); err != nil {
				return Summary{}, err
			}
			x.logf("published the live files of round %s again to %s (no index.json there)", st.Tag, o.Out)
		}
		return Summary{Tag: st.Tag, Nothing: true}, nil
	}

	// 3. the tag and the plan
	now := d.Now()
	x.tag = Tag(now)
	plan := &Plan{Tag: x.tag, Time: now.UTC().Format(time.RFC3339), Commit: commit, Branch: branch, PrevTag: st.Tag, PrevCommit: oldEn.Source.Commit,
		Seeded: seeded, Texts: len(x.cur), Added: nz(im.Added), Changed: nz(im.Changed), Removed: nz(im.Removed), Reached: im.Reached,
		Langs: map[string]*LangPlan{}, DryRun: o.DryRun, Dropped: dropped}
	if plan.Reached == nil {
		plan.Reached = []i18n.Reached{}
	}
	if missing, changed, _ := i18n.Stale(x.cur, x.ctx); len(missing)+len(changed) > 0 {
		plan.Stale = map[string][]string{"missing": nz(missing), "changed": nz(changed)}
		x.logf("[3/7] warning: %d texts have no context and %d an old one in i18n/context.json (they are translated with what the graph gives)", len(missing), len(changed))
	}
	total := 0
	for _, lr := range targets {
		work, removed := WorkList(lr.src, im, x.cur)
		lr.plan = &LangPlan{Work: nz(work), Removed: nz(removed), Status: "planned"}
		plan.Langs[lr.l.Code] = lr.plan
		total += len(work)
	}
	x.logf("[3/7] tag %s: %d texts to translate in %d languages", x.tag, total, len(targets))
	if o.DryRun {
		for _, lr := range targets {
			n := map[string]int{}
			for _, w := range lr.plan.Work {
				n[w.Reason]++
			}
			calls := len(i18n.BatchGroups(x.newG, x.msgsOf(lr.plan.Work), o.Batch))
			x.logf("dry-run %-8s %d texts (added %d, missing %d, changed %d, reached %d), %d removed, about %d batch calls",
				lr.l.Code, len(lr.plan.Work), n["added"], n["missing"], n["changed"], n["reached"], len(lr.plan.Removed), calls)
			for _, w := range lr.plan.Work {
				fmt.Fprintf(d.Log, "    %-8s %-46s %s\n", w.Reason, w.ID, w.Via)
			}
		}
		x.logf("dry-run: no model calls, nothing written")
		return Summary{Tag: x.tag}, nil
	}
	if err := writeJSON(filepath.Join(o.State, "rounds", x.tag+".json"), plan); err != nil {
		return Summary{}, err
	}

	// 4. translate, score, check and judge, one fix round, again
	if total > 0 {
		if x.m, err = d.Open(); err != nil {
			return Summary{}, err
		}
		if err := x.m.Scorer.Check(ctx); err != nil {
			return Summary{}, fmt.Errorf("the scorer cannot run, so no request was sent: %v", err)
		}
		if err := x.translateAll(ctx, targets); err != nil {
			return Summary{}, err
		}
	}

	// 5. merge into the live files
	var review []ReviewItem
	sum := Summary{Tag: x.tag}
	live := map[string]rung.Result{}
	for _, lr := range targets {
		if lr.err != nil {
			lr.plan.Status, lr.plan.Error = "failed", lr.err.Error()
			sum.Failed = append(sum.Failed, lr.l.Code)
			x.logf("[5/7] %-8s FAILED, the live file stays as it was: %v", lr.l.Code, lr.err)
			if len(lr.live.Messages) > 0 {
				live[lr.l.Code] = lr.live
			}
			continue
		}
		res, newSrc, rv := MergeLive(lr.l.Code, lr.live, lr.src, x.cur, lr.outcome)
		res.Run, res.Commit, res.Dedupe, res.K = x.tag, commit, true, 1
		if x.m.Settings.Model != "" {
			res.Model, res.Prompt = x.m.Settings.Model, muse.PromptBatch
		}
		if lr.live.Run != "" && lr.live.Run != res.Run {
			res.Previous = append(append([]string{}, lr.live.Previous...), lr.live.Run)
		}
		res.Unscored = x.unscored // true when a scorer run of this round failed
		for _, o := range lr.outcome {
			if o.Pass {
				lr.plan.Passed++
			}
		}
		lr.plan.Review, lr.plan.Status = len(rv), "done"
		review = append(review, rv...)
		if err := writeJSON(filepath.Join(o.State, "live", lr.l.Code+".json"), res); err != nil {
			return sum, err
		}
		if err := writeJSON(filepath.Join(o.State, "live", lr.l.Code+".source.json"), newSrc); err != nil {
			return sum, err
		}
		live[lr.l.Code] = res
		x.logf("[5/7] %-8s merged: %d passed, %d for review, %d removed; live %d texts, %d with no translation",
			lr.l.Code, lr.plan.Passed, len(rv), len(lr.plan.Removed), len(res.Messages), len(res.Missing))
	}
	sum.Review = len(review)
	if review == nil {
		review = []ReviewItem{}
	}
	if err := writeJSON(filepath.Join(o.State, "review", x.tag+".json"), map[string]any{"round": x.tag, "commit": commit, "texts": review}); err != nil {
		return sum, err
	}

	// 6. publish
	if err := Publish(o.Out, x.tag, commit, langs, x.cur, live); err != nil {
		return sum, err
	}
	x.logf("[6/7] published %d languages and index.json to %s", len(live)+1, o.Out)

	// 7. the state of this round: written last, so a crash before it makes the next round do the same work again
	if err := writeJSON(filepath.Join(o.State, "rounds", x.tag+".json"), plan); err != nil {
		return sum, err
	}
	enOut := i18n.EnFile{Source: src, Language: "en", Messages: x.cur}
	for _, f := range []struct { // a fixed order: en.json last of the three (the next round compares with it)
		p string
		v any
	}{{"graph.json", x.newG}, {"context.json", x.ctx}, {"en.json", enOut}} {
		if err := writeJSON(filepath.Join(o.State, f.p), f.v); err != nil {
			return sum, err
		}
	}
	if err := writeJSON(filepath.Join(o.State, "round.json"), StateFile{Tag: x.tag, Commit: commit, Time: plan.Time, Failed: sum.Failed}); err != nil {
		return sum, err
	}
	x.logf("[7/7] round %s done: %d languages, %d failed %v, %d texts for review (review/%s.json)", x.tag, len(targets), len(sum.Failed), sum.Failed, sum.Review, x.tag)
	return sum, nil
}

func orNone(tag, commit string) string {
	switch {
	case tag != "":
		return "round " + tag
	case commit != "":
		return "the committed en.json " + short(commit)
	}
	return "nothing (first round)"
}

func nz[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func writeJSON(path string, v any) error { return WriteAtomic(path, i18n.Marshal(v)) }

// findSeed: the first earlier result of a language: <seed>/<lang>/<lang>.json,
// <seed>/<lang>.json, then the committed <repo>/i18n/<lang>.json.
func findSeed(o Options, conf, code string) (rung.Result, string, bool) {
	var ps []string
	if o.Seed != "" {
		ps = append(ps, filepath.Join(o.Seed, code, code+".json"), filepath.Join(o.Seed, code+".json"))
	}
	ps = append(ps, filepath.Join(conf, code+".json"))
	for _, p := range ps {
		var r rung.Result
		if i18n.ReadJSON(p, &r) == nil && len(r.Messages) > 0 {
			return r, p, true
		}
	}
	return rung.Result{}, "", false
}

func (x *runner) msgsOf(work []Why) []i18n.Message {
	in := map[string]bool{}
	for _, w := range work {
		in[w.ID] = true
	}
	var out []i18n.Message
	for _, m := range x.cur {
		if in[m.ID] {
			out = append(out, m)
		}
	}
	return out
}

// valid: the live translations whose English is still the current text.
func (x *runner) valid(lr *lang) map[string]rung.Chosen {
	out := map[string]rung.Chosen{}
	for _, c := range lr.live.Messages {
		if t, ok := x.text[c.ID]; ok && lr.src[c.ID] == t {
			out[c.ID] = c
		}
	}
	return out
}

func (x *runner) translateAll(ctx context.Context, targets []*lang) error {
	// round 1: the work texts, the batch method (one screen in one call)
	for _, lr := range targets {
		msgs := x.msgsOf(lr.plan.Work)
		if len(msgs) == 0 {
			lr.outcome = map[string]Outcome{}
			continue
		}
		prev := map[string]string{}
		for id, c := range x.valid(lr) {
			prev[id] = c.Text
		}
		lr.r1, lr.err = x.translate(ctx, lr, msgs, x.tag, nil, nil, prev)
		if lr.err == nil && len(lr.r1.Messages) == 0 {
			lr.err = fmt.Errorf("no translation came back for any of the %d texts (the errors are in %s)", len(msgs), lr.ledger)
		}
		if lr.err == nil {
			lr.plan.Calls += x.calls(lr, x.tag)
			x.logf("[4/7] translate %-8s %d texts, %d calls, %d with no good answer", lr.l.Code, len(msgs), x.calls(lr, x.tag), len(lr.r1.Missing))
		}
	}
	x.score(ctx, targets, func(lr *lang) *rung.Result { return &lr.r1 }, "1")
	for _, lr := range targets {
		if lr.err != nil || len(lr.r1.Messages)+len(lr.r1.Missing) == 0 {
			continue
		}
		lr.f1, lr.err = x.judgeLang(ctx, lr, x.cands(lr, false), lr.r1, "r1")
		if lr.err == nil {
			lr.plan.Flagged = len(lr.f1)
			x.logf("[4/7] check+judge %-8s round 1: %d texts flagged, %d with no translation", lr.l.Code, len(lr.f1), len(lr.r1.Missing))
		}
	}
	// the fix round: the flagged texts with the reasons, and the texts they reach, in one call
	for _, lr := range targets {
		if lr.err != nil || len(lr.r1.Messages)+len(lr.r1.Missing) == 0 {
			continue
		}
		msgs, notes := x.planFix(lr)
		lr.plan.Fixed = len(msgs)
		if len(msgs) == 0 {
			continue
		}
		prev := map[string]string{}
		for id, c := range x.cands(lr, false) {
			prev[id] = c.Text
		}
		run := x.tag + "-fix"
		lr.r2, lr.err = x.translate(ctx, lr, msgs, run, notes, lr.from, prev)
		if lr.err == nil {
			lr.plan.Calls += x.calls(lr, run)
			x.logf("[4/7] fix %-8s %d flagged or missing + %d reached texts, %d calls", lr.l.Code, len(msgs)-len(lr.from), len(lr.from), x.calls(lr, run))
		}
	}
	x.score(ctx, targets, func(lr *lang) *rung.Result { return &lr.r2 }, "2")
	for _, lr := range targets {
		if lr.err != nil || len(lr.r2.Messages)+len(lr.r2.Missing) == 0 {
			continue
		}
		lr.f2, lr.err = x.judgeLang(ctx, lr, x.cands(lr, true), lr.r2, "r2")
		if lr.err == nil {
			x.logf("[4/7] check+judge %-8s after the fix round: %d texts still flagged", lr.l.Code, len(lr.f2))
		}
	}
	for _, lr := range targets {
		if lr.err == nil && lr.outcome == nil {
			lr.outcome = x.outcomes(lr)
		}
	}
	return nil
}

func (x *runner) calls(lr *lang, run string) int {
	n, _ := rung.Calls(lr.ledger, rung.Result{Run: run})
	return n
}

// cands: the translations that the checks and judges see: the valid live
// translations, round 1, and (after) the fix round.
func (x *runner) cands(lr *lang, after bool) map[string]rung.Chosen {
	out := x.valid(lr)
	for _, w := range lr.plan.Work {
		delete(out, w.ID) // its live translation is for another text, or it is translated again
	}
	for _, c := range lr.r1.Messages {
		out[c.ID] = c
	}
	if after {
		for _, c := range lr.r2.Messages {
			out[c.ID] = c
		}
	}
	return out
}

func (x *runner) translate(ctx context.Context, lr *lang, msgs []i18n.Message, run string, notes, from map[string]string, prev map[string]string) (rung.Result, error) {
	if err := os.MkdirAll(filepath.Dir(lr.ledger), 0o755); err != nil {
		return rung.Result{}, err
	}
	lf, err := os.OpenFile(lr.ledger, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return rung.Result{}, err
	}
	defer lf.Close()
	set := x.m.Settings
	if set.Model == "" {
		set = muse.Default()
	}
	if set.MaxTokens < 8192 {
		set.MaxTokens = 8192 // one answer holds the whole group (L4k)
	}
	kn := i18n.NewKnowledge(x.cur, x.newG, x.ctx)
	cfg := rung.Config{Run: run, Lang: lr.l, Keep: x.keep, K: 1, Workers: x.o.Workers, Settings: set, Commit: x.commit, Prompt: muse.PromptBatch,
		Key: func(m i18n.Message) string { return kn.Key(m.ID) }}
	cfg.About = func(id string, mk mask.Masked) string {
		b := kn.Block(id, rung.Tags(mk), prev)
		if n := notes[id]; n != "" {
			b += "\n" + n
		}
		return b
	}
	max := x.o.Batch
	cfg.Batch = func(ms []i18n.Message) [][]i18n.Message { return i18n.BatchGroups(x.newG, ms, max) }
	if notes != nil {
		cfg.Fix = func(id string) string { return notes[id] }
		cfg.From = func(id string) string { return from[id] }
		cfg.Batch = func(ms []i18n.Message) [][]i18n.Message { return byFrom(ms, from, max) }
	}
	return rung.Run(ctx, cfg, msgs, x.m.Translator, nil, lf)
}

// byFrom: one call for a flagged text and the texts it reaches (at most max in a call).
func byFrom(ms []i18n.Message, from map[string]string, max int) [][]i18n.Message {
	by := map[string][]i18n.Message{}
	var keys []string
	for _, m := range ms {
		k := m.ID
		if f := from[m.ID]; f != "" {
			k = f
		}
		if _, ok := by[k]; !ok {
			keys = append(keys, k)
		}
		by[k] = append(by[k], m)
	}
	sort.Strings(keys)
	var out [][]i18n.Message
	for _, k := range keys {
		for part := by[k]; len(part) > 0; {
			parts := (len(part) + max - 1) / max
			n := (len(part) + parts - 1) / parts
			out = append(out, part[:n])
			part = part[n:]
		}
	}
	return out
}

// scoreAll scores the new translations of every language in one scorer call,
// so MetricX loads its model once for all languages. A text that only copies
// the English (kept names) is not scored. The scores go to each ledger.
// score runs scoreAll; a scorer failure (for example MetricX stopped at the
// memory limit) does not lose the round (X49b review): with QEMax 0 the
// scores only rank, so the tries stay unscored and the round goes on; with
// QEMax > 0 the scores decide, so each language of the round fails.
func (x *runner) score(ctx context.Context, targets []*lang, pick func(*lang) *rung.Result, round string) {
	err := x.scoreAll(ctx, targets, pick, round)
	if err == nil {
		return
	}
	if x.o.QEMax <= 0 {
		x.unscored = true
		x.logf("[4/7] warning: %v: the tries of round %s stay unscored (scores do not decide)", err, round)
		return
	}
	for _, lr := range targets {
		if lr.err == nil {
			lr.err = err
		}
	}
}

func (x *runner) scoreAll(ctx context.Context, targets []*lang, pick func(*lang) *rung.Result, round string) error {
	type ref struct {
		lr *lang
		k  int
	}
	var pairs []rung.Pair
	var refs []ref
	for _, lr := range targets {
		if lr.err != nil {
			continue
		}
		r := pick(lr)
		for k, c := range r.Messages {
			if c.SameAs == "" && c.Score == nil && c.Text != x.text[c.ID] {
				pairs = append(pairs, rung.Pair{Source: x.text[c.ID], Hypothesis: c.Text})
				refs = append(refs, ref{lr, k})
			}
		}
	}
	if len(pairs) == 0 {
		return nil
	}
	t0 := time.Now()
	scores, err := x.m.Scorer.Score(ctx, pairs)
	if err == nil && len(scores) != len(pairs) {
		err = fmt.Errorf("%d scores for %d pairs", len(scores), len(pairs))
	}
	if err != nil {
		return fmt.Errorf("MetricX (round %s): %v", round, err)
	}
	led := map[*lang][]rung.ScoreLine{}
	now := time.Now().UTC().Format(time.RFC3339)
	for i, rf := range refs {
		r := pick(rf.lr)
		s := scores[i]
		r.Messages[rf.k].Score = &s
		led[rf.lr] = append(led[rf.lr], rung.ScoreLine{Kind: "score", Time: now, Run: r.Run, ID: r.Messages[rf.k].ID, Lang: rf.lr.l.Code, Try: 1, Score: s})
	}
	for lr, lines := range led {
		r := pick(lr)
		rep := map[string]*float64{}
		for _, c := range r.Messages {
			if c.SameAs == "" {
				rep[c.ID] = c.Score
			}
		}
		for k, c := range r.Messages {
			if c.SameAs != "" {
				r.Messages[k].Score = rep[c.SameAs]
			}
		}
		r.Unscored = false
		if err := appendLines(lr.ledger, lines); err != nil {
			lr.err = err
		}
	}
	x.logf("[4/7] MetricX round %s: %d translations of %d languages in one scorer run (%s)", round, len(pairs), len(led), time.Since(t0).Round(time.Second))
	return nil
}

func appendLines(path string, lines []rung.ScoreLine) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, l := range lines {
		if err := enc.Encode(l); err != nil {
			return err
		}
	}
	return nil
}

// judgeLang runs the checks and both judges on the groups that hold a text of
// res, with the other translations of the language as they are. Only the texts
// of res are flagged. With QEMax, a MetricX score above it flags the text too.
func (x *runner) judgeLang(ctx context.Context, lr *lang, cands map[string]rung.Chosen, res rung.Result, name string) (map[string][]string, error) {
	focus := map[string]bool{}
	rep := map[string]string{}
	for _, c := range res.Messages {
		focus[c.ID] = true
		if c.SameAs != "" {
			rep[c.ID] = c.SameAs
		}
	}
	s := judge.Set{Lang: lr.l, Keep: x.keep, Msgs: x.cur, Graph: x.newG, Ctx: x.ctx, Trans: map[string]string{}, Shared: map[string]string{}}
	for id, c := range cands {
		s.Trans[id] = c.Text
		if c.SameAs != "" {
			s.Shared[id] = c.SameAs
		}
	}
	var issues []judge.Issue
	for _, is := range judge.Checks(s) {
		for _, id := range is.IDs {
			if focus[id] {
				issues = append(issues, is)
				break
			}
		}
	}
	var groups []judge.Group
	for _, g := range judge.Groups(s) {
		for _, id := range g.IDs {
			if focus[id] {
				groups = append(groups, g)
				break
			}
		}
	}
	var vs []judge.Verdict
	if len(groups) > 0 && len(x.m.Judges) > 0 {
		vs = judge.Judge(ctx, s, groups, x.m.Judges, x.about, x.o.JWorkers)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		errs := 0
		for _, v := range vs {
			if v.Error != "" {
				errs++
			}
		}
		if errs == len(vs) {
			return nil, fmt.Errorf("every judge call failed (%d), for example: %s", errs, vs[0].Error)
		}
		if errs > 0 {
			x.logf("[4/7] check+judge %-8s %s: %d of %d judge calls failed (their groups are checked by the other judge only)", lr.l.Code, name, errs, len(vs))
		}
	}
	all := judge.Flagged(append(issues, judge.Issues(vs)...))
	flags := map[string][]string{}
	for id, why := range all {
		if focus[id] {
			flags[id] = why
		}
	}
	if x.o.QEMax > 0 {
		for _, c := range res.Messages {
			if c.Score != nil && *c.Score > x.o.QEMax {
				flags[c.ID] = append(flags[c.ID], fmt.Sprintf("metricx: QE score %.1f is more than %.1f (0 is best)", *c.Score, x.o.QEMax))
			}
		}
	}
	for id, r := range rep { // a shared text has the result of the text it reuses
		if why := flags[r]; len(why) > 0 && len(flags[id]) == 0 {
			flags[id] = why
		}
	}
	report := judge.Report{Lang: lr.l.Code, Result: res.Run, Checks: nz(issues), Verdicts: nz(vs), Flagged: flags}
	for _, v := range vs {
		if v.Error != "" {
			report.Errors++
		}
	}
	if err := writeJSON(filepath.Join(x.o.State, "judge", x.tag, lr.l.Code+"."+name+".json"), report); err != nil {
		return nil, err
	}
	return flags, nil
}

// planFix: the flagged and missing texts of round 1, each with the reasons, and
// the texts they reach through the graph (i18n.Spread), each with a note.
func (x *runner) planFix(lr *lang) ([]i18n.Message, map[string]string) {
	notes := map[string]string{}
	lr.bad, lr.from = map[string]bool{}, map[string]string{}
	got := map[string]rung.Chosen{}
	for _, c := range lr.r1.Messages {
		got[c.ID] = c
	}
	for id, why := range lr.f1 {
		lr.bad[id] = true
		notes[id] = "Problems found in the earlier translation " + strconv.Quote(got[id].Text) + ": " + strings.Join(why, " | ") + "\nDo not repeat these problems."
	}
	for _, id := range lr.r1.Missing {
		lr.bad[id] = true
		notes[id] = "The earlier try gave no usable translation (a tag was lost, or the answer had no text). Keep every tag exactly once."
	}
	cands := x.cands(lr, false)
	var starts []string
	for id := range lr.f1 {
		starts = append(starts, id)
	}
	sort.Strings(starts)
	for _, s := range starts {
		for _, r := range i18n.Spread(x.newG, s) {
			c, ok := cands[r.ID]
			if _, done := lr.from[r.ID]; done || !ok || lr.bad[r.ID] || c.SameAs != "" || x.text[r.ID] == "" {
				continue
			}
			lr.from[r.ID] = s
			notes[r.ID] = "A related text is corrected in this same round (" + r.Via + "): " + strconv.Quote(x.text[s]) +
				" (earlier " + strconv.Quote(got[s].Text) + "). Reasons: " + strings.Join(lr.f1[s], " | ") +
				". Use the same word for the same idea. Change this translation only where it must change."
		}
	}
	var msgs []i18n.Message
	for _, m := range x.cur {
		if lr.bad[m.ID] || lr.from[m.ID] != "" {
			msgs = append(msgs, m)
		}
	}
	return msgs, notes
}

// outcomes: a work text that passed round 1 and is not in the fix round
// passes. A text of the fix round passes when its new translation passes. A
// flagged or missing text that fails again is failed twice: it keeps its live
// translation. A reached text that fails keeps what it had before the fix round.
func (x *runner) outcomes(lr *lang) map[string]Outcome {
	r1, r2 := map[string]rung.Chosen{}, map[string]rung.Chosen{}
	for _, c := range lr.r1.Messages {
		r1[c.ID] = c
	}
	for _, c := range lr.r2.Messages {
		r2[c.ID] = c
	}
	focus := map[string]bool{}
	for _, w := range lr.plan.Work {
		focus[w.ID] = true
	}
	for id := range lr.from {
		focus[id] = true
	}
	inWork := map[string]bool{}
	for _, w := range lr.plan.Work {
		inWork[w.ID] = true
	}
	out := map[string]Outcome{}
	for id := range focus {
		fixed := lr.bad[id] || lr.from[id] != ""
		c1, ok1 := r1[id]
		if !fixed {
			if ok1 {
				out[id] = Outcome{Pass: true, New: c1}
			} else {
				out[id] = Outcome{Reasons: []string{"no usable translation"}}
			}
			continue
		}
		c2, ok2 := r2[id]
		if ok2 && len(lr.f2[id]) == 0 {
			out[id] = Outcome{Pass: true, New: c2}
			continue
		}
		o := Outcome{Reasons: append(append([]string{}, lr.f1[id]...), lr.f2[id]...)}
		if ok1 {
			o.Tried = append(o.Tried, c1.Text)
		}
		if ok2 {
			o.Tried = append(o.Tried, c2.Text)
		} else {
			o.Reasons = append(o.Reasons, "the fix round gave no usable translation")
		}
		if !lr.bad[id] { // reached: it passed before the fix round (or it is a live text)
			o.Reasons = append([]string{"reached by the correction of " + lr.from[id]}, o.Reasons...)
			o.Tried = nil
			if ok2 {
				o.Tried = []string{c2.Text}
			}
			if ok1 && inWork[id] {
				c := c1
				o.Fallback = &c
			}
		}
		out[id] = o
	}
	return out
}
