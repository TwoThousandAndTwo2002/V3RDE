// Command translate runs rung 1 for one language (L4c): Muse Spark Open on
// Baseten translates every message of en.json k times, MetricX-24 scores them
// in one container run, and it writes <lang>.json and <lang>.ledger.jsonl to
// -out (outside the repo). Run it in i18n/. The Baseten key is in -key-file
// (mode 0600, made once by tools/lif_baseten_key.sh), else in BASETEN_API_KEY.
//
// L4h: by default each prompt has the context of the text from the text graph
// (context.json, graph.json, and the earlier translations in <out>/<lang>.json
// for the same idea). -context=false sends the plain prompt. -ids, -effort and
// -temperature run a small batch with other settings (the sweep).
//
//	go run ./cmd/translate -lang es-419 -out ~/Documents/Morpheus_transfer/lif/runs/es-419
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
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

type options struct {
	code, outDir, image, keyFile, ids, effort, prevFile, fix string
	k, workers, limit, rpm                                   int
	temperature                                              float64
	resume, force, withContext, score, spread                bool
}

func main() {
	var o options
	flag.StringVar(&o.code, "lang", "es-419", "target language code from languages.json")
	flag.IntVar(&o.k, "k", 1, "translations per message (rung 1: 1)")
	flag.IntVar(&o.workers, "workers", 8, "requests at the same time")
	flag.IntVar(&o.limit, "limit", 0, "only the first N messages (0: all)")
	flag.StringVar(&o.outDir, "out", "", "output directory, outside the repo (required)")
	flag.StringVar(&o.image, "image", "v3rde-qe:L2", "the MetricX image (qe/Dockerfile)")
	flag.StringVar(&o.keyFile, "key-file", muse.KeyFile(), "file with the Baseten key (mode 0600)")
	flag.IntVar(&o.rpm, "rpm", 0, "requests per minute (0: the limit that Baseten's answers give)")
	flag.BoolVar(&o.resume, "resume", false, "translate only the texts that <out>/<lang>.json has no translation for, and merge")
	flag.BoolVar(&o.force, "force", false, "translate again even when <out>/<lang>.json is complete")
	flag.BoolVar(&o.withContext, "context", true, "give each prompt the context of the text from the text graph (L4h)")
	flag.StringVar(&o.ids, "ids", "", "only these message ids, comma-separated (a small batch, for the sweep)")
	flag.StringVar(&o.effort, "effort", "", "reasoning_effort for this run (default: low)")
	flag.Float64Var(&o.temperature, "temperature", 0, "temperature for this run (0: the default 1.0)")
	flag.BoolVar(&o.score, "score", true, "score with MetricX at the end of this run; -score=false leaves it to cmd/score (one container run for many results)")
	flag.BoolVar(&o.spread, "spread", true, "with -fix: also translate again, once, the texts that a corrected text reaches through the graph (L4j)")
	flag.StringVar(&o.fix, "fix", "", "a <lang>.judge.json: translate again, once, the texts it flags, with the problems in the prompt, and merge (L4j)")
	flag.StringVar(&o.prevFile, "prev", "", "earlier result for the same idea in the prompt (default: <out>/<lang>.json if it is there)")
	collapse := flag.Bool("collapse", false, "no requests: make <out>/<lang>.json follow the dedupe rule (lowest score per English text)")
	flag.Parse()
	if *collapse {
		if err := collapseFile(o.code, o.outDir); err != nil {
			fmt.Fprintln(os.Stderr, "translate:", err)
			os.Exit(1)
		}
		return
	}
	if err := run(o); err != nil {
		fmt.Fprintln(os.Stderr, "translate:", err)
		os.Exit(1)
	}
}

func run(o options) error {
	if o.outDir == "" {
		return fmt.Errorf("-out is required")
	}
	var en i18n.EnFile
	var langs i18n.Languages
	var g i18n.GraphFile
	var cf i18n.ContextFile
	for p, v := range map[string]any{"en.json": &en, "languages.json": &langs, "graph.json": &g, "context.json": &cf} {
		if err := i18n.ReadJSON(p, v); err != nil {
			return fmt.Errorf("%s: %v", p, err)
		}
	}
	var target *i18n.Language
	for i, l := range langs.Languages {
		if l.Code == o.code && l.Role != "source" {
			target = &langs.Languages[i]
		}
	}
	if target == nil {
		return fmt.Errorf("%s is not a target in languages.json", o.code)
	}
	if o.withContext {
		if m, c, gone := i18n.Stale(en.Messages, cf); len(m)+len(c)+len(gone) > 0 {
			return fmt.Errorf("stale contexts (go run ./cmd/graph): missing %v, changed %v, gone %v", m, c, gone)
		}
	}
	client, err := muse.Load(o.keyFile)
	if err != nil {
		return err
	}
	client.Pace.SetRPM(o.rpm)
	if o.effort != "" {
		client.Settings.ReasoningEffort = o.effort
	}
	if o.temperature > 0 {
		client.Settings.Temperature = o.temperature
	}
	prompt := muse.PromptPlain
	if o.withContext {
		prompt = muse.PromptContext
	}
	all := en.Messages
	if o.ids != "" {
		want := map[string]bool{}
		for _, id := range strings.Split(o.ids, ",") {
			want[strings.TrimSpace(id)] = true
		}
		all = nil
		for _, m := range en.Messages {
			if want[m.ID] {
				all = append(all, m)
				delete(want, m.ID)
			}
		}
		if len(want) > 0 {
			return fmt.Errorf("-ids: not in en.json: %v", want)
		}
	}
	if o.limit > 0 && o.limit < len(all) {
		all = all[:o.limit]
	}
	msgs := all
	resPath := filepath.Join(o.outDir, o.code+".json")
	var old rung.Result
	haveOld := i18n.ReadJSON(resPath, &old) == nil
	if o.fix != "" && !o.withContext {
		return fmt.Errorf("-fix needs the context prompt (do not use -context=false)")
	}
	if !o.resume && !o.force && o.fix == "" && haveOld && rung.Complete(old, all, en.Source.Commit, client.Settings.Model, o.k, prompt) {
		fmt.Printf("translate: %s is complete (run %s, prompt %s): skipped (-force translates it again)\n", resPath, old.Run, prompt)
		return nil
	}
	fixes := map[string]string{}
	flagged := map[string][]string{} // -fix: the flagged ids and the reasons
	if o.fix != "" {
		if !haveOld {
			return fmt.Errorf("-fix: no %s", resPath)
		}
		var rep judge.Report
		if err := i18n.ReadJSON(o.fix, &rep); err != nil {
			return fmt.Errorf("-fix: %v", err)
		}
		if rep.Lang != o.code {
			return fmt.Errorf("-fix: %s is for %s, not %s", o.fix, rep.Lang, o.code)
		}
		revised := map[string]bool{}
		earlier := map[string]string{}
		for _, c := range old.Messages {
			revised[c.ID], earlier[c.ID] = c.Revised, c.Text
		}
		msgs = nil
		var human []string
		for _, m := range all {
			why := rep.Flagged[m.ID]
			switch {
			case len(why) == 0:
			case revised[m.ID]: // once only: still flagged after one retranslation
				human = append(human, m.ID)
			default:
				fixes[m.ID] = "Problems found in the earlier translation " + strconv.Quote(earlier[m.ID]) + ": " + strings.Join(why, " | ") + "\nDo not repeat these problems."
				msgs = append(msgs, m)
			}
		}
		if len(human) > 0 {
			fmt.Printf("translate: %d texts still flagged after one retranslation, for human review: %s\n", len(human), strings.Join(human, ", "))
		}
		if len(msgs) == 0 {
			fmt.Println("translate: nothing more to translate again in", o.fix)
			return nil
		}
		// the report of this round is kept as the next free <lang>.judge.roundN.json;
		// the next cmd/judge writes a new <lang>.judge.json
		var round string
		for n := 1; ; n++ {
			round = strings.TrimSuffix(o.fix, ".judge.json") + ".judge.round" + strconv.Itoa(n) + ".json"
			if _, err := os.Stat(round); os.IsNotExist(err) {
				break
			}
		}
		if err := os.Rename(o.fix, round); err != nil {
			return err
		}
		// the result before this round, for the review page (cmd/report)
		before := filepath.Join(o.outDir, o.code+".before-"+strings.TrimSuffix(filepath.Base(round), ".json")[len(o.code)+len(".judge."):]+".json")
		raw, err := os.ReadFile(resPath)
		if err != nil {
			return err
		}
		if err := os.WriteFile(before, raw, 0o644); err != nil {
			return err
		}
		flagged = rep.Flagged
		o.resume = true // merge into the result
		fmt.Printf("translate: %d flagged texts to translate again (report kept as %s)\n", len(msgs), round)
	} else if o.resume {
		if !haveOld {
			return fmt.Errorf("resume: no %s", resPath)
		}
		if msgs = rung.Todo(old, all); len(msgs) == 0 && o.fix == "" {
			fmt.Println("translate: nothing is missing in", resPath)
			return nil
		}
	}
	// earlier translations for the same idea: -prev, else the result in -out
	prev := map[string]string{}
	prevPath := o.prevFile
	if prevPath == "" && haveOld {
		prevPath = resPath
	}
	if prevPath != "" {
		var p rung.Result
		if err := i18n.ReadJSON(prevPath, &p); err != nil {
			return fmt.Errorf("-prev: %v", err)
		}
		for _, c := range p.Messages {
			prev[c.ID] = c.Text
		}
	}
	if err := os.MkdirAll(o.outDir, 0o755); err != nil {
		return err
	}
	run := time.Now().UTC().Format("20060102T150405Z")
	ledgerPath := filepath.Join(o.outDir, o.code+".ledger.jsonl")
	lf, err := os.OpenFile(ledgerPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer lf.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	fmt.Printf("translate: %d messages to %s, k=%d, %d workers, run %s, prompt %s, effort %s, temperature %.2f (resume: %v)\n",
		len(msgs), o.code, o.k, o.workers, run, prompt, client.Settings.ReasoningEffort, client.Settings.Temperature, o.resume)
	cfg := rung.Config{Run: run, Lang: *target, Keep: langs.Keep, K: o.k, Workers: o.workers, Settings: client.Settings,
		Commit: en.Source.Commit, Prompt: prompt}
	if o.withContext {
		kn := i18n.NewKnowledge(en.Messages, g, cf)
		cfg.Key = func(m i18n.Message) string { return kn.Key(m.ID) }
		cfg.About = func(id string, mk mask.Masked) string {
			b := kn.Block(id, rung.Tags(mk), prev)
			if f := fixes[id]; f != "" {
				b += "\n" + f
			}
			return b
		}
	}
	if len(fixes) > 0 {
		cfg.Fix = func(id string) string { return fixes[id] }
	}
	var sc rung.Scorer = rung.Docker{Script: filepath.Join("..", "qe", "score.sh"), Image: o.image, Log: os.Stderr}
	if !o.score {
		sc = nil // cmd/score scores later, in one container run
	}
	res, err := rung.Run(ctx, cfg, msgs, client, sc, lf)
	if err != nil {
		return err
	}
	if o.resume {
		if res, err = rung.Merge(old, res, all); err != nil {
			return err
		}
	}
	if len(fixes) > 0 && o.spread {
		// L4j: the fix spreads through the graph. Each corrected text that
		// changed reaches its pairs, joined parts and the texts of the same
		// concept; each of them gets one try that knows the correction.
		if res, err = spread(ctx, o, cfg, run, res, old, fixes, flagged, all, g, prev, client, sc, lf); err != nil {
			return err
		}
	}
	fmt.Printf("translate: request limit in use: %d per minute\n", client.Pace.RPM())
	b, _ := json.MarshalIndent(res, "", "  ")
	if err := os.WriteFile(resPath, append(b, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Printf("translate: %d kept, %d missing\n  %s\n  %s\n", len(res.Messages), len(res.Missing), resPath, ledgerPath)
	if res.Unscored {
		fmt.Println("translate: not scored yet: go run ./cmd/score", resPath)
	}
	return nil
}

// collapseFile rewrites <out>/<lang>.json with rung.Collapse; the old file is kept as <lang>.before-dedupe.json.
func collapseFile(code, outDir string) error {
	var en i18n.EnFile
	if err := i18n.ReadJSON("en.json", &en); err != nil {
		return err
	}
	p := filepath.Join(outDir, code+".json")
	raw, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	var r rung.Result
	if err := json.Unmarshal(raw, &r); err != nil {
		return err
	}
	if r.Commit != en.Source.Commit {
		return fmt.Errorf("%s is for en.json commit %s, not %s", p, r.Commit, en.Source.Commit)
	}
	keep := filepath.Join(outDir, code+".before-dedupe.json")
	if _, err := os.Stat(keep); err == nil {
		return fmt.Errorf("%s exists: this file was collapsed before", keep)
	}
	if err := os.WriteFile(keep, raw, 0o644); err != nil {
		return err
	}
	c := rung.Collapse(r, en.Messages)
	b, _ := json.MarshalIndent(c, "", "  ")
	if err := os.WriteFile(p, append(b, '\n'), 0o644); err != nil {
		return err
	}
	same := 0
	for _, m := range c.Messages {
		if m.SameAs != "" {
			same++
		}
	}
	fmt.Printf("translate: %s collapsed: %d messages, %d reuse another id's translation, %d missing (old file: %s)\n", p, len(c.Messages), same, len(c.Missing), keep)
	return nil
}

// spread translates again, once, the texts that a corrected text reaches
// through the graph (i18n.Spread): each try gets the correction in its prompt.
// A text that is flagged, already revised, missing or that reuses another id's
// translation is not reached. The first corrected text that reaches a text wins.
func spread(ctx context.Context, o options, cfg rung.Config, run string, res, old rung.Result, fixes map[string]string,
	flagged map[string][]string, all []i18n.Message, g i18n.GraphFile, prev map[string]string,
	tr rung.Translator, sc rung.Scorer, lf *os.File) (rung.Result, error) {
	before := map[string]rung.Chosen{}
	for _, c := range old.Messages {
		before[c.ID] = c
	}
	now := map[string]rung.Chosen{}
	for _, c := range res.Messages {
		now[c.ID] = c
	}
	en := map[string]i18n.Message{}
	for _, m := range all {
		en[m.ID] = m
	}
	var starts []string
	for id := range fixes {
		if c, ok := now[id]; ok && c.Revised && c.Text != before[id].Text {
			starts = append(starts, id)
		}
	}
	sort.Strings(starts)
	from := map[string]string{}
	notes := map[string]string{}
	for _, s := range starts {
		for _, r := range i18n.Spread(g, s) {
			c, ok := now[r.ID]
			if _, done := from[r.ID]; done || !ok || c.Revised || c.SameAs != "" || len(flagged[r.ID]) > 0 || en[r.ID].ID == "" {
				continue
			}
			from[r.ID] = s
			notes[r.ID] = "A related text was corrected (" + r.Via + "): " + strconv.Quote(en[s].Text) + " was " +
				strconv.Quote(before[s].Text) + ", now " + strconv.Quote(now[s].Text) + ". Reasons: " + strings.Join(flagged[s], " | ") +
				". Use the same word for the same idea. Change this translation only where it must change."
		}
	}
	if len(from) == 0 {
		fmt.Println("translate: spread: no related text to translate again")
		return res, nil
	}
	var msgs []i18n.Message
	for _, m := range all {
		if from[m.ID] != "" {
			msgs = append(msgs, m)
		}
	}
	for id, c := range now { // the prompts see the corrected translations
		prev[id] = c.Text
	}
	fmt.Printf("translate: spread: %d corrected texts reach %d related texts through the graph\n", len(starts), len(msgs))
	cfg.Run = run + "-spread"
	cfg.Fix = func(id string) string { return notes[id] }
	cfg.From = func(id string) string { return from[id] }
	for id, n := range notes { // cfg.About adds fixes[id] to the prompt
		fixes[id] = n
	}
	add, err := rung.Run(ctx, cfg, msgs, tr, sc, lf)
	if err != nil {
		return res, err
	}
	return rung.Merge(res, add, all)
}
