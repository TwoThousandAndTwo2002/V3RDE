// Command check runs the checks that need no model on translation results
// (L4j): a pair with one translation, one English text with two
// translations, two English texts on one screen with one translation, and
// untranslated text in another script. It writes <lang>.check.json next to
// each result.
//
//	go run ./cmd/check ~/Documents/Morpheus_transfer/lif/runs-ctx/*/*.json
package main

import (
	"fmt"
	"os"
	"strings"

	"v3rde/i18n"
	"v3rde/i18n/judge"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "check:", err)
		os.Exit(1)
	}
}

func run(files []string) error {
	n := 0
	for _, p := range files {
		if !i18n.IsResult(p) {
			continue // only <lang>.json, not <lang>.check.json or <lang>.before-dedupe.json
		}
		s, err := judge.Load(p)
		if err != nil {
			return err
		}
		issues := judge.Checks(s)
		if issues == nil {
			issues = []judge.Issue{}
		}
		out := strings.TrimSuffix(p, ".json") + ".check.json"
		if err := os.WriteFile(out, i18n.Marshal(map[string]any{"lang": s.Lang.Code, "issues": issues}), 0o644); err != nil {
			return err
		}
		count := map[string]int{}
		for _, is := range issues {
			count[is.Check]++
		}
		fmt.Printf("check: %-8s %d issues %v -> %s\n", s.Lang.Code, len(issues), count, out)
		n++
	}
	if n == 0 {
		return fmt.Errorf("no <lang>.json given")
	}
	return nil
}
