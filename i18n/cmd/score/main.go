// Command score scores, in one MetricX container run, every translation that
// has no score yet in the given results (L4i). Translate first with
// -score=false (many languages, many settings), then run once:
//
//	go run ./cmd/score ~/Documents/Morpheus_transfer/lif/runs/*/*.json
//
// Each argument is a <lang>.json; its ledger <lang>.ledger.jsonl is next to it.
// Files named *.before-dedupe.json are skipped. The model loads once for all.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"v3rde/i18n"
	"v3rde/i18n/rung"
)

func main() {
	image := flag.String("image", "v3rde-qe:L2", "the MetricX image (qe/Dockerfile)")
	flag.Parse()
	var files []string
	for _, a := range flag.Args() {
		if i18n.IsResult(a) { // not <lang>.check.json, <lang>.judge*.json or <lang>.before-dedupe.json
			files = append(files, a)
		}
	}
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "score: no <lang>.json given")
		os.Exit(2)
	}
	var en i18n.EnFile
	if err := i18n.ReadJSON("en.json", &en); err != nil {
		fmt.Fprintln(os.Stderr, "score:", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	t0 := time.Now()
	sc := rung.Docker{Script: filepath.Join("..", "qe", "score.sh"), Image: *image, Log: os.Stderr}
	n, err := rung.ScoreAll(ctx, files, en.Messages, sc)
	if err != nil {
		fmt.Fprintln(os.Stderr, "score:", err)
		os.Exit(1)
	}
	fmt.Printf("score: %d translations scored in one container run (%s), %d results rewritten\n", n, time.Since(t0).Round(time.Second), len(files))
}
