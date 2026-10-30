// Command judge checks translation results as groups (L4j). For each result
// (<lang>.json) it runs the checks that need no model, then asks the judges
// about each concept and each screen of the text graph: Muse Spark Open
// (Baseten, the translator's model) and Muse Spark 1.3 (Meta Model API,
// another model). Until the judges are benchmarked, a text is flagged when any
// check or any judge marks it. It writes <lang>.judge.json next to each result.
//
//	go run ./cmd/judge ~/Documents/Morpheus_transfer/lif/runs-ctx/*/*.json
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"

	"v3rde/i18n"
	"v3rde/i18n/judge"
	"v3rde/i18n/meta"
	"v3rde/i18n/muse"
)

func main() {
	models := flag.String("models", "open,meta", "judges: open (Muse Spark Open, Baseten) and/or meta (Muse Spark 1.3, Meta Model API)")
	workers := flag.Int("workers", 4, "calls at the same time for each judge")
	effort := flag.String("effort", "low", "reasoning effort of the judges")
	flag.Parse()
	if err := run(flag.Args(), *models, *workers, *effort); err != nil {
		fmt.Fprintln(os.Stderr, "judge:", err)
		os.Exit(1)
	}
}

func results(args []string) []string {
	var out []string
	for _, p := range args {
		if i18n.IsResult(p) {
			out = append(out, p) // <lang>.json, not <lang>.judge.json or <lang>.check.json
		}
	}
	return out
}

func run(args []string, models string, workers int, effort string) error {
	files := results(args)
	if len(files) == 0 {
		return fmt.Errorf("no <lang>.json given")
	}
	var ms []judge.Model
	for _, m := range strings.Split(models, ",") {
		switch strings.TrimSpace(m) {
		case "open":
			c, err := muse.Load(muse.KeyFile())
			if err != nil {
				return err
			}
			c.Settings.ReasoningEffort = effort
			ms = append(ms, judge.Baseten{C: c})
		case "meta":
			c, err := meta.Load(meta.KeyFile())
			if err != nil {
				return err
			}
			c.Effort = effort
			ms = append(ms, c)
		default:
			return fmt.Errorf("-models: %q is not open or meta", m)
		}
	}
	var cf i18n.ConceptFile
	if err := i18n.ReadJSON("concepts.json", &cf); err != nil {
		return err
	}
	about := map[string]string{}
	for n, c := range cf.Concepts {
		about[n] = c.About
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	for _, p := range files {
		s, err := judge.Load(p)
		if err != nil {
			return err
		}
		var res struct {
			Run string `json:"run"`
		}
		i18n.ReadJSON(p, &res)
		groups := judge.Groups(s)
		checks := judge.Checks(s)
		vs := judge.Judge(ctx, s, groups, ms, about, workers)
		if err := ctx.Err(); err != nil {
			return err
		}
		rep := judge.Report{Lang: s.Lang.Code, Result: res.Run, Checks: checks, Verdicts: vs}
		if rep.Checks == nil {
			rep.Checks = []judge.Issue{}
		}
		rep.Flagged = judge.Flagged(append(append([]judge.Issue{}, checks...), judge.Issues(vs)...))
		fails := map[string]int{}
		for _, v := range vs {
			if v.Error != "" {
				rep.Errors++
			} else if v.Verdict == "fail" {
				fails[v.Model]++
			}
		}
		out := strings.TrimSuffix(p, ".json") + ".judge.json"
		if err := os.WriteFile(out, i18n.Marshal(rep), 0o644); err != nil {
			return err
		}
		var fs []string
		for m, n := range fails {
			fs = append(fs, fmt.Sprintf("%s %d", m, n))
		}
		sort.Strings(fs)
		fmt.Printf("judge: %-8s %d groups x %d judges; checks %d; groups failed: %s; judge errors %d; flagged texts %d -> %s\n",
			s.Lang.Code, len(groups), len(ms), len(checks), strings.Join(fs, ", "), rep.Errors, len(rep.Flagged), out)
	}
	return nil
}
