// Command report writes lif_review.json for the review page and the graph page
// (L4j): for each language, each text's translation before and after the fix
// round, the findings of the checks and the judges, and the status.
//
//	go run ./cmd/report -out ../lif_review.json runs-ctx/*/*.json
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"v3rde/i18n"
	"v3rde/i18n/judge"
)

func main() {
	out := flag.String("out", "lif_review.json", "the file to write")
	flag.Parse()
	if err := run(*out, flag.Args()); err != nil {
		fmt.Fprintln(os.Stderr, "report:", err)
		os.Exit(1)
	}
}

// optional reads p into v; a missing file is not an error.
func optional(p string, v any) (bool, error) {
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return false, nil
	}
	return true, read(p, v)
}

// read: judge.Result has only the fields the review needs, so other fields are allowed.
func read(p string, v any) error {
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%s: %v", p, err)
	}
	return nil
}

func run(out string, args []string) error {
	var en i18n.EnFile
	var g i18n.GraphFile
	var cf i18n.ContextFile
	var lf i18n.Languages
	for p, v := range map[string]any{"en.json": &en, "graph.json": &g, "context.json": &cf, "languages.json": &lf} {
		if err := i18n.ReadJSON(p, v); err != nil {
			return err
		}
	}
	byCode := map[string]i18n.Language{}
	for _, l := range lf.Languages {
		byCode[l.Code] = l
	}
	var langs []judge.Lang
	for _, p := range args {
		if !i18n.IsResult(p) {
			continue
		}
		code := strings.TrimSuffix(filepath.Base(p), ".json")
		lang, ok := byCode[code]
		if !ok {
			return fmt.Errorf("%s: %s is not in languages.json", p, code)
		}
		l := judge.Lang{Language: lang}
		if err := read(p, &l.Final); err != nil {
			return err
		}
		dir := filepath.Dir(p)
		var b judge.Result
		var r1, now judge.Report
		if ok, err := optional(filepath.Join(dir, code+".before-round1.json"), &b); err != nil {
			return err
		} else if ok {
			l.Before = &b
		}
		if ok, err := optional(filepath.Join(dir, code+".judge.round1.json"), &r1); err != nil {
			return err
		} else if ok {
			l.Round1 = &r1
		}
		if ok, err := optional(filepath.Join(dir, code+".judge.json"), &now); err != nil {
			return err
		} else if ok {
			l.Now = &now
		}
		langs = append(langs, l)
	}
	if len(langs) == 0 {
		return fmt.Errorf("no <lang>.json given")
	}
	rv := judge.BuildReview(en, g, cf, langs)
	b, _ := json.MarshalIndent(rv, "", " ")
	if err := os.WriteFile(out, append(b, '\n'), 0o644); err != nil {
		return err
	}
	for _, s := range rv.Langs {
		fmt.Printf("report: %-8s round1 flagged %3d | now flagged %3d | %v\n", s.Lang, s.Round1, s.Now, s.Status)
	}
	fmt.Println("report:", out)
	return nil
}
