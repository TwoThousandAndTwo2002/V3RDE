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
	flag.Parse()
	if err := run(*lang, *k, *workers, *limit, *outDir, *image, *keyFile, *rpm, *resume); err != nil {
		fmt.Fprintln(os.Stderr, "translate:", err)
		os.Exit(1)
	}
}

func run(code string, k, workers, limit int, outDir, image, keyFile string, rpm int, resume bool) error {
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
