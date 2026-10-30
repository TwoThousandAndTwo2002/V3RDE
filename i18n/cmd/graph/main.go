// Command graph shows what a change of the app text reaches (L4g). Run it in
// i18n/ after a new ui commit is put in source.json, before go run ./cmd/extract:
// it compares the committed en.json and graph.json with the new app text, walks
// the graph from every added, removed and changed text, and lists the texts to
// review, most connected first. It also lists the contexts that are stale.
//
//	go run ./cmd/graph            (the commit in source.json)
//	go run ./cmd/graph -commit X  (another ui commit, to look before a change)
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"v3rde/i18n"
)

func main() {
	repo := flag.String("repo", "..", "the V3RDE git repo (the ui commit must be fetched)")
	commit := flag.String("commit", "", "the ui commit to compare (default: source.json)")
	asJSON := flag.String("json", "", "also write the report as JSON to this file")
	flag.Parse()
	if err := run(*repo, *commit, *asJSON); err != nil {
		fmt.Fprintln(os.Stderr, "graph:", err)
		os.Exit(1)
	}
}

func run(repo, commit, asJSON string) error {
	var src i18n.Source
	var lists i18n.Lists
	var oldEn i18n.EnFile
	var oldG i18n.GraphFile
	var ctx i18n.ContextFile
	for p, v := range map[string]any{"source.json": &src, "rules.json": &lists, "en.json": &oldEn, "graph.json": &oldG, "context.json": &ctx} {
		if err := i18n.ReadJSON(p, v); err != nil {
			return fmt.Errorf("%s: %v", p, err)
		}
	}
	if commit != "" {
		src.Commit = commit
	}
	files, err := i18n.ReadSource(repo, src)
	if err != nil {
		return err
	}
	msgs, _, err := i18n.All(files, &lists)
	if err != nil {
		return err
	}
	newG, err := i18n.GraphOf(src, files, msgs, ".")
	if err != nil {
		return err
	}
	im := i18n.ImpactOf(oldEn.Messages, msgs, oldG, newG)
	missing, changed, gone := i18n.Stale(msgs, ctx)
	fmt.Printf("graph: en.json %s -> ui %s: %d texts, %d nodes, %d edges\n", short(oldEn.Source.Commit), short(src.Commit), len(msgs), len(newG.Nodes), len(newG.Edges))
	fmt.Printf("  added %d, removed %d, changed %d\n", len(im.Added), len(im.Removed), len(im.Changed))
	for _, l := range []struct {
		name string
		ids  []string
	}{{"added", im.Added}, {"removed", im.Removed}, {"changed", im.Changed}} {
		if len(l.ids) > 0 {
			fmt.Printf("  %s: %s\n", l.name, strings.Join(l.ids, ", "))
		}
	}
	fmt.Printf("  to review (most connected first): %d\n", len(im.Reached))
	for _, r := range im.Reached {
		fmt.Printf("    %-46s degree %-3d via %s\n", r.ID, r.Degree, r.Via)
	}
	fmt.Printf("  stale contexts: %d missing, %d changed English, %d for texts that are gone\n", len(missing), len(changed), len(gone))
	for _, l := range [][]string{missing, changed, gone} {
		if len(l) > 0 {
			fmt.Printf("    %s\n", strings.Join(l, ", "))
		}
	}
	if asJSON != "" {
		b, _ := json.MarshalIndent(struct {
			i18n.Impact
			Missing []string `json:"stale_missing"`
			Changed []string `json:"stale_changed"`
			Gone    []string `json:"stale_gone"`
		}{im, missing, changed, gone}, "", "  ")
		return os.WriteFile(asJSON, append(b, '\n'), 0o644)
	}
	return nil
}

func short(c string) string {
	if len(c) > 7 {
		return c[:7]
	}
	return c
}
