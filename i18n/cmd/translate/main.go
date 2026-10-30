// Command translate runs rung 1 for one language (L4c): Muse Spark Open on
// Baseten translates every message of en.json k times, MetricX-24 scores them
// in one container run, and it writes <lang>.json and <lang>.ledger.jsonl to
// -out (outside the repo). Run it in i18n/. The Baseten key is in -key-file
// (mode 0600, made once by tools/lif_baseten_key.sh), else in BASETEN_API_KEY:
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
	"time"

	"v3rde/i18n"
	"v3rde/i18n/muse"
	"v3rde/i18n/rung"
)

func main() {
	lang := flag.String("lang", "es-419", "target language code from languages.json")
	k := flag.Int("k", 1, "translations per message (rung 1: 1)")
	workers := flag.Int("workers", 8, "requests at the same time")
	limit := flag.Int("limit", 0, "only the first N messages (0: all)")
	outDir := flag.String("out", "", "output directory, outside the repo (required)")
	image := flag.String("image", "v3rde-qe:L2", "the MetricX image (qe/Dockerfile)")
	keyFile := flag.String("key-file", muse.KeyFile(), "file with the Baseten key (mode 0600)")
	rpm := flag.Int("rpm", 0, "requests per minute (0: the limit that Baseten's answers give)")
	resume := flag.Bool("resume", false, "translate only the missing ids of <out>/<lang>.json and merge")
	force := flag.Bool("force", false, "translate again even when <out>/<lang>.json is complete")
	collapse := flag.Bool("collapse", false, "no requests: make <out>/<lang>.json follow the dedupe rule (lowest score per English text)")
	flag.Parse()
	if *collapse {
		if err := collapseFile(*lang, *outDir); err != nil {
			fmt.Fprintln(os.Stderr, "translate:", err)
			os.Exit(1)
		}
		return
	}
	if err := run(*lang, *k, *workers, *limit, *outDir, *image, *keyFile, *rpm, *resume, *force); err != nil {
		fmt.Fprintln(os.Stderr, "translate:", err)
		os.Exit(1)
	}
}

func run(code string, k, workers, limit int, outDir, image, keyFile string, rpm int, resume, force bool) error {
	if outDir == "" {
		return fmt.Errorf("-out is required")
	}
	var en i18n.EnFile
	var langs i18n.Languages
	if err := i18n.ReadJSON("en.json", &en); err != nil {
		return err
	}
	if err := i18n.ReadJSON("languages.json", &langs); err != nil {
		return err
	}
	var target *i18n.Language
	for i, l := range langs.Languages {
		if l.Code == code && l.Role != "source" {
			target = &langs.Languages[i]
		}
	}
	if target == nil {
		return fmt.Errorf("%s is not a target in languages.json", code)
	}
	client, err := muse.Load(keyFile)
	if err != nil {
		return err
	}
	client.Pace.SetRPM(rpm)
	all := en.Messages
	if limit > 0 && limit < len(all) {
		all = all[:limit]
	}
	msgs := all
	resPath := filepath.Join(outDir, code+".json")
	var old rung.Result
	if !resume && !force {
		var done rung.Result
		if err := i18n.ReadJSON(resPath, &done); err == nil && rung.Complete(done, en.Source.Commit, client.Settings.Model, k) && limit == 0 {
			fmt.Printf("translate: %s is complete (run %s): skipped (-force translates it again)\n", resPath, done.Run)
			return nil
		}
	}
	if resume {
		if err := i18n.ReadJSON(resPath, &old); err != nil {
			return fmt.Errorf("resume: %v", err)
		}
		miss := map[string]bool{}
		for _, id := range old.Missing {
			miss[id] = true
		}
		msgs = nil
		for _, m := range all {
			if miss[m.ID] {
				msgs = append(msgs, m)
			}
		}
		if len(msgs) == 0 {
			fmt.Println("translate: nothing is missing in", resPath)
			return nil
		}
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	run := time.Now().UTC().Format("20060102T150405Z")
	ledgerPath := filepath.Join(outDir, code+".ledger.jsonl")
	lf, err := os.OpenFile(ledgerPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer lf.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	fmt.Printf("translate: %d messages to %s, k=%d, %d workers, run %s (resume: %v)\n", len(msgs), code, k, workers, run, resume)
	cfg := rung.Config{Run: run, Lang: *target, Keep: langs.Keep, K: k, Workers: workers, Settings: client.Settings, Commit: en.Source.Commit}
	sc := rung.Docker{Script: filepath.Join("..", "qe", "score.sh"), Image: image, Log: os.Stderr}
	res, err := rung.Run(ctx, cfg, msgs, client, sc, lf)
	if err != nil {
		return err
	}
	if resume {
		if res, err = rung.Merge(old, res, all); err != nil {
			return err
		}
	}
	fmt.Printf("translate: request limit in use: %d per minute\n", client.Pace.RPM())
	b, _ := json.MarshalIndent(res, "", "  ")
	if err := os.WriteFile(resPath, append(b, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Printf("translate: %d kept, %d missing\n  %s\n  %s\n", len(res.Messages), len(res.Missing), resPath, ledgerPath)
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
