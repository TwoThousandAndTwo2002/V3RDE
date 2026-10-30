// Command compare puts ways of translating side by side (L4k), with the same
// measures for each: model calls, MetricX QE scores, checks with no model, and
// the two judges. It gives no accuracy numbers: there are no human labels.
//
//	go run ./cmd/compare -arm plain=runs -arm context=runs-ctx:round1 -arm loop=runs-ctx \
//	    -arm batch=runs-batch:round1 -arm batch+loop=runs-batch -out compare.json -md compare.md
//
// An arm is name=dir (the final result <dir>/<lang>/<lang>.json and its
// <lang>.judge.json) or name=dir:round1 (the result before the fix round,
// <lang>.before-round1.json, and <lang>.judge.round1.json).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"v3rde/i18n/judge"
	"v3rde/i18n/rung"
)

type arms []string

func (a *arms) String() string     { return strings.Join(*a, " ") }
func (a *arms) Set(v string) error { *a = append(*a, v); return nil }

func main() {
	var as arms
	flag.Var(&as, "arm", "name=dir or name=dir:round1 (more than one)")
	langs := flag.String("langs", "es-419,pt-BR,zh-Hans,hi,ar", "languages")
	out := flag.String("out", "compare.json", "the JSON file to write")
	md := flag.String("md", "compare.md", "the Markdown table to write")
	flag.Parse()
	if err := run(as, strings.Split(*langs, ","), *out, *md); err != nil {
		fmt.Fprintln(os.Stderr, "compare:", err)
		os.Exit(1)
	}
}

func readLax(p string, v any) error {
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func one(name, spec, lang string) (judge.Arm, error) {
	dir, stage, _ := strings.Cut(spec, ":")
	d := filepath.Join(dir, lang)
	res, rep := filepath.Join(d, lang+".json"), filepath.Join(d, lang+".judge.json")
	if stage == "round1" {
		res, rep = filepath.Join(d, lang+".before-round1.json"), filepath.Join(d, lang+".judge.round1.json")
	} else if stage != "" {
		return judge.Arm{}, fmt.Errorf("arm %s: stage %q is not round1", name, stage)
	}
	s, err := judge.Load(res)
	if err != nil {
		return judge.Arm{}, err
	}
	var r rung.Result
	if err := readLax(res, &r); err != nil {
		return judge.Arm{}, err
	}
	led := filepath.Join(d, lang+".ledger.jsonl")
	ts, err := rung.TextScores(led) // the scores of a copy (round1) are in the ledger
	if err != nil {
		return judge.Arm{}, err
	}
	sc := judge.Scores{}
	for _, c := range r.Messages {
		sc[c.ID] = c.Score
		if v, ok := ts[c.ID+"\x00"+c.Text]; ok && c.Score == nil {
			sc[c.ID] = &v
		}
	}
	calls, err := rung.Calls(led, r)
	if err != nil {
		return judge.Arm{}, err
	}
	var rp *judge.Report
	if _, err := os.Stat(rep); err == nil {
		var x judge.Report
		if err := readLax(rep, &x); err != nil {
			return judge.Arm{}, fmt.Errorf("%s: %v", rep, err)
		}
		rp = &x
	}
	return judge.Measure(name, s, sc, calls, rp), nil
}

func run(as []string, langs []string, out, md string) error {
	if len(as) == 0 {
		return fmt.Errorf("no -arm")
	}
	var all []judge.Arm
	var names []string
	for _, a := range as {
		name, spec, ok := strings.Cut(a, "=")
		if !ok {
			return fmt.Errorf("-arm %q: want name=dir[:round1]", a)
		}
		names = append(names, name)
		for _, l := range langs {
			x, err := one(name, spec, l)
			if err != nil {
				return fmt.Errorf("arm %s, %s: %v", name, l, err)
			}
			all = append(all, x)
		}
	}
	b, _ := json.MarshalIndent(all, "", " ")
	if err := os.WriteFile(out, append(b, '\n'), 0o644); err != nil {
		return err
	}
	t := table(all, names, langs)
	fmt.Print(t)
	return os.WriteFile(md, []byte(t), 0o644)
}

func sum(m map[string]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}

func failed(a judge.Arm) string {
	if !a.Judged {
		return "not judged"
	}
	var ks []string
	for k := range a.GroupsFailed {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	var p []string
	for _, k := range ks {
		short := k
		if strings.Contains(k, "open") {
			short = "open"
		} else if strings.Contains(k, "1.3") {
			short = "1.3"
		}
		p = append(p, fmt.Sprintf("%s %d", short, a.GroupsFailed[k]))
	}
	if len(p) == 0 {
		p = []string{"0"}
	}
	return fmt.Sprintf("%s of %d", strings.Join(p, ", "), a.Groups)
}

// table: one row for each arm and language, then one total row for each arm.
func table(all []judge.Arm, names, langs []string) string {
	var b strings.Builder
	b.WriteString("Measures only (no human labels): calls = model calls; MetricX QE 0-25, lower is better, ranks tries and is not accuracy; checks = issues found with no model; flagged = texts a check or a judge marked; both = texts both judges marked; groups failed = concept and screen groups each judge failed.\n\n")
	b.WriteString("| arm | lang | texts | calls | MetricX median | MetricX > 5 | check issues | flagged | by both judges | groups failed |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|\n")
	for _, a := range all {
		fl, both := "-", "-"
		if a.Judged {
			fl, both = fmt.Sprint(a.Flagged), fmt.Sprint(a.ByBoth)
		}
		med, over := "not scored", "-"
		if a.Scored > 0 {
			med, over = fmt.Sprintf("%.2f", a.Median), fmt.Sprint(a.Over5)
		}
		fmt.Fprintf(&b, "| %s | %s | %d | %d | %s | %s | %d | %s | %s | %s |\n", a.Arm, a.Lang, a.Texts, a.Calls, med, over, sum(a.Checks), fl, both, failed(a))
	}
	b.WriteString("\n| arm | languages | calls | mean of MetricX medians | MetricX > 5 | check issues | flagged | by both judges |\n|---|---|---|---|---|---|---|---|\n")
	for _, n := range names {
		var calls, over, chk, fl, both, k int
		med, judged, scored := 0.0, true, true
		for _, a := range all {
			if a.Arm != n {
				continue
			}
			k++
			calls, over, chk, fl, both = calls+a.Calls, over+a.Over5, chk+sum(a.Checks), fl+a.Flagged, both+a.ByBoth
			med += a.Median
			judged = judged && a.Judged
			scored = scored && a.Scored > 0
		}
		if k == 0 {
			continue
		}
		fs, bs := fmt.Sprint(fl), fmt.Sprint(both)
		if !judged {
			fs, bs = "-", "-"
		}
		ms, os := fmt.Sprintf("%.2f", med/float64(k)), fmt.Sprint(over)
		if !scored {
			ms, os = "not scored", "-"
		}
		fmt.Fprintf(&b, "| %s | %d | %d | %s | %s | %d | %s | %s |\n", n, k, calls, ms, os, chk, fs, bs)
	}
	return b.String()
}
