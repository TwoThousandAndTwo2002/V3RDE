package telemetry

// X44d (F5, "edge vs cloud, made visible"): how long each part of the
// pipeline takes, from the ledger only (no estimate). The edge parts run on
// the box (the PE-Core food gate and the barcode catalog, on a 2 GiB ARM
// t4g.small); the cloud parts are the Meta Model API calls (SAM 3.1, Muse
// Spark 1.3, Muse Image 1.0, Muse Voice Transcribe 1.0). A ledger line is a
// Shipped event (kind "event", the service's own line in "line") or a Sample
// (kind "sample": CPU and memory of a service, each second).

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Stat is the distribution of one measure (milliseconds, or MiB for memory).
type Stat struct {
	N      int     `json:"n"`
	Median float64 `json:"median"`
	P90    float64 `json:"p90"`
	Max    float64 `json:"max"`
}

// Row is one part of the pipeline.
type Row struct {
	Part   string `json:"part"` // edge | cloud | scan
	What   string `json:"what"`
	Source string `json:"source"` // the ledger field it comes from
	Unit   string `json:"unit"`   // ms | MiB | USD
	Stat
}

// Report is the answer of EdgeCloud.
type Report struct {
	Days  []string `json:"days"`
	Lines int      `json:"lines"`
	Bad   int      `json:"bad_lines"` // not JSON, or not a ledger line
	Rows  []Row    `json:"rows"`
}

// StatOf is the nearest-rank median, p90 and the max of v: the p-th
// percentile is the smallest value with at least p of the values at or
// under it (the value at rank ceil(p*N) of the sorted values).
func StatOf(v []float64) Stat {
	if len(v) == 0 {
		return Stat{}
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	rank := func(p float64) float64 { return s[max(int(math.Ceil(p*float64(len(s))))-1, 0)] }
	return Stat{N: len(s), Median: rank(0.5), P90: rank(0.9), Max: s[len(s)-1]}
}

// rowSpec says which values go into a row.
type rowSpec struct {
	part, what, source, unit string
}

var specs = []rowSpec{
	{"edge", "Food gate, PE-Core-S16-384 on the box (whole request)", "gate total_ms", "ms"},
	{"edge", "Food gate, PE-Core-S16-384 on the box (model only)", "gate invoke_ms", "ms"},
	{"edge", "Barcode lookup, catalog on the box", "barcode total_ms", "ms"},
	{"edge", "Food gate service memory", "gate sample rss_mib", "MiB"},
	{"edge", "Barcode service memory", "barcode sample rss_mib", "MiB"},
	{"cloud", "SAM 3.1: find every item", "analyze calls sam:*", "ms"},
	{"cloud", "Muse Spark 1.3: name the items", "analyze calls muse:*", "ms"},
	{"cloud", "Muse Image 1.0: photo touch-up", "touchup calls touchup:edit", "ms"},
	{"cloud", "Muse Spark 1.3: check the touch-up", "touchup calls touchup:check", "ms"},
	{"cloud", "Muse Voice Transcribe 1.0: speech to text", "voice calls voice:transcribe", "ms"},
	{"cloud", "Muse Spark 1.3: speech to listing changes", "voice calls voice:map", "ms"},
	{"cloud", "Muse Spark 1.3: rescue draft", "rescue calls rescue:draft", "ms"},
	{"scan", "One scan, barcode path (no model)", "analyze total_ms source barcode", "ms"},
	{"scan", "One scan, vision path (gate + SAM + Muse)", "analyze total_ms source vision", "ms"},
	{"scan", "Cost of one vision scan", "analyze cost_usd source vision", "USD"},
}

type ledgerLine struct {
	Kind    string          `json:"kind"`
	Service string          `json:"service"`
	RSSMiB  float64         `json:"rss_mib"`
	Line    json.RawMessage `json:"line"`
}

type eventLine struct {
	Step     string  `json:"step"`
	Status   int     `json:"status"`
	TotalMS  float64 `json:"total_ms"`
	InvokeMS float64 `json:"invoke_ms"`
	Source   string  `json:"source"`
	CostUSD  float64 `json:"cost_usd"`
	Calls    []struct {
		Purpose string  `json:"purpose"`
		Status  int     `json:"status"`
		MS      float64 `json:"ms"`
	} `json:"calls"`
}

// Summarize builds the report from ledger lines (any order).
func Summarize(lines [][]byte) Report {
	vals := map[string][]float64{}
	add := func(src string, v float64) { vals[src] = append(vals[src], v) }
	rep := Report{Lines: len(lines)}
	for _, b := range lines {
		var l ledgerLine
		if json.Unmarshal(b, &l) != nil || l.Service == "" {
			rep.Bad++
			continue
		}
		if l.Kind == "sample" {
			if l.Service == "gate" || l.Service == "barcode" {
				if l.RSSMiB > 0 {
					add(l.Service+" sample rss_mib", l.RSSMiB)
				}
			}
			continue
		}
		if l.Kind != "event" {
			continue
		}
		var e eventLine
		if json.Unmarshal(l.Line, &e) != nil {
			rep.Bad++
			continue
		}
		for _, c := range e.Calls { // a call that did not answer 2xx has no useful time
			if c.Status/100 != 2 || c.MS <= 0 {
				continue
			}
			p := c.Purpose
			switch {
			case l.Service == "analyze" && strings.HasPrefix(p, "sam:"):
				add("analyze calls sam:*", c.MS)
			case l.Service == "analyze" && strings.HasPrefix(p, "muse:"):
				add("analyze calls muse:*", c.MS)
			case l.Service != "analyze":
				add(l.Service+" calls "+p, c.MS)
			}
		}
		if e.Status != 200 {
			continue
		}
		switch {
		case l.Service == "gate" && e.Step == "gate":
			add("gate total_ms", e.TotalMS)
			if e.InvokeMS > 0 {
				add("gate invoke_ms", e.InvokeMS)
			}
		case l.Service == "barcode" && e.Step == "barcode":
			add("barcode total_ms", e.TotalMS)
		case l.Service == "analyze" && e.Step == "analyze" && e.Source != "":
			add("analyze total_ms source "+e.Source, e.TotalMS)
			if e.Source == "vision" {
				add("analyze cost_usd source vision", e.CostUSD)
			}
		}
	}
	for _, s := range specs {
		rep.Rows = append(rep.Rows, Row{Part: s.part, What: s.what, Source: s.source, Unit: s.unit, Stat: StatOf(vals[s.source])})
	}
	return rep
}

// EdgeCloud reads the ledger and samples files of days (YYYYMMDD; empty =
// every day in dir). Report.Days are the days that had a file.
func EdgeCloud(dir string, days []string) (Report, error) {
	var files []string
	if len(days) == 0 {
		for _, pre := range []string{"ledger-", "samples-"} {
			m, _ := filepath.Glob(filepath.Join(dir, pre+"????????.jsonl"))
			files = append(files, m...)
		}
	} else {
		for _, d := range days {
			files = append(files, filepath.Join(dir, "ledger-"+d+".jsonl"), filepath.Join(dir, "samples-"+d+".jsonl"))
		}
	}
	sort.Strings(files)
	var lines [][]byte
	found := map[string]bool{}
	for _, f := range files {
		fh, err := os.Open(f)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return Report{}, err
		}
		sc := bufio.NewScanner(fh)
		sc.Buffer(make([]byte, 1<<20), 16<<20)
		for sc.Scan() {
			lines = append(lines, append([]byte(nil), sc.Bytes()...))
		}
		err = sc.Err()
		fh.Close()
		if err != nil {
			return Report{}, fmt.Errorf("%s: %w", f, err)
		}
		name := strings.TrimSuffix(filepath.Base(f), ".jsonl")
		found[name[strings.LastIndex(name, "-")+1:]] = true
	}
	rep := Summarize(lines)
	for d := range found {
		rep.Days = append(rep.Days, d)
	}
	sort.Strings(rep.Days)
	return rep, nil
}

// Markdown is the report as one table (n = how many values; the values are
// the median, the 90th percentile and the max).
func (r Report) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Ledger days %s, %d lines (%d not readable).\n\n", strings.Join(r.Days, ", "), r.Lines, r.Bad)
	b.WriteString("| Where | Part | n | Median | p90 | Max | Ledger field |\n|---|---|---|---|---|---|---|\n")
	for _, x := range r.Rows {
		f := func(v float64) string {
			if x.N == 0 {
				return "-"
			}
			switch x.Unit {
			case "USD":
				return fmt.Sprintf("$%.4f", v)
			case "MiB":
				return fmt.Sprintf("%.1f MiB", v)
			}
			return fmt.Sprintf("%.0f ms", v)
		}
		fmt.Fprintf(&b, "| %s | %s | %d | %s | %s | %s | `%s` |\n", x.Part, x.What, x.N, f(x.Median), f(x.P90), f(x.Max), x.Source)
	}
	return b.String()
}
