// Command extract writes i18n/en.json, i18n/skipped.json and i18n/graph.json from the app
// code named in i18n/source.json (run it in i18n/: go run ./cmd/extract).
// With -check it writes nothing and fails when the files are not up to date.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"v3rde/i18n"
)

func main() {
	repo := flag.String("repo", "..", "the V3RDE git repo (the ui commit must be fetched)")
	dir := flag.String("dir", ".", "the i18n directory")
	check := flag.Bool("check", false, "fail when en.json, skipped.json or graph.json is not up to date; write nothing")
	flag.Parse()
	en, sk, gr, n, err := i18n.Build(*repo, *dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "extract:", err)
		os.Exit(1)
	}
	for name, want := range map[string][]byte{"en.json": en, "skipped.json": sk, "graph.json": gr} {
		p := filepath.Join(*dir, name)
		if *check {
			got, _ := os.ReadFile(p)
			if !bytes.Equal(got, want) {
				fmt.Fprintf(os.Stderr, "extract: %s is not up to date (go run ./cmd/extract)\n", name)
				os.Exit(1)
			}
			continue
		}
		if err := os.WriteFile(p, want, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "extract:", err)
			os.Exit(1)
		}
	}
	fmt.Printf("extract: %d messages\n", n)
}
