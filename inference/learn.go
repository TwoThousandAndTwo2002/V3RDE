package inference

// LR2 (I1, T9 2026-11-01: "i approve of I1"): the daily self-improvement
// report. It reads the analyze events files only (the scans and the donors'
// labels of X32g/X48u) and says, for each prompt version (P1), how often the
// donors kept what the model gave, and which fields they change. It changes
// nothing: a person reads it, and a prompt or a setting changes only by a
// SUP change with a new prompt version (P1).
//
// A labels line names the request it labels. Only the last labels line of a
// request counts (the app can send more than one), in the order of the lines
// (the files are read day by day, each in the order it was written). An analyze line made
// before P1 has no prompt fields: its group is "before P1". An analyze line
// made before LR1 has no answer items: its items count by the line's source.

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

// LearnTally is one group of scans and of the labeled items of those scans.
type LearnTally struct {
	Group    string  `json:"group"`
	Scans    int     `json:"scans"`
	CostUSD  float64 `json:"cost_usd"`
	P50MS    int64   `json:"p50_ms"`
	P95MS    int64   `json:"p95_ms"`
	Items    int     `json:"items"` // labeled items of posted label sets
	Kept     int     `json:"kept"`
	NameSame int     `json:"name_same"`
	ServSame int     `json:"servings_same"`
	Removed  int     `json:"removed"`
	ms       []int64
}

// FieldEdits is the donors' changes of one field.
type FieldEdits struct {
	Field string         `json:"field"`
	Edits int            `json:"edits"`
	ByHow map[string]int `json:"by_how"`
	Top   []Change       `json:"top,omitempty"` // the most common from -> to (not for pickup)
}

// Change is one from -> to pair and how often it was made.
type Change struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Count int    `json:"count"`
}

// ServingsBias compares the model's servings with the final ones (kept items).
type ServingsBias struct {
	Items     int     `json:"items"`
	Same      int     `json:"same"`
	ModelHigh int     `json:"model_high"` // the model gave more than the donor kept
	ModelLow  int     `json:"model_low"`
	MeanDiff  float64 `json:"mean_diff"` // mean of model - final
}

// LearnReport is the answer of Learn.
type LearnReport struct {
	AnalyzeLines   int            `json:"analyze_lines"`
	ByStatus       map[string]int `json:"by_status"`
	Vision         int            `json:"vision_scans"`
	Barcode        int            `json:"barcode_scans"`
	SpentUSD       float64        `json:"spent_usd"`
	LabelSets      int            `json:"label_sets"` // the last one of each request
	Posted         int            `json:"posted"`
	NoScan         int            `json:"no_scan"`         // posted label sets whose scan line is not in these files
	UnknownPrompts int            `json:"unknown_prompts"` // calls with prompt_id "unknown" (P1: must be 0)
	ByPrompt       []LearnTally   `json:"by_prompt"`
	ByVia          []LearnTally   `json:"by_via"`
	Fields         []FieldEdits   `json:"fields"`
	Servings       ServingsBias   `json:"servings"`
	Calibration    CalibReport    `json:"calibration"`
}

// LearnTop is how many from -> to pairs each field keeps.
const LearnTop = 5

type learnCall struct {
	Purpose string `json:"purpose"`
	Status  int    `json:"status"`
	ID      string `json:"prompt_id"`
	Version string `json:"prompt_version"`
}

type learnLine struct {
	TS        string      `json:"ts"`
	Step      string      `json:"step"`
	RequestID string      `json:"request_id"`
	Status    int         `json:"status"`
	Source    string      `json:"source"`
	CostUSD   float64     `json:"cost_usd"`
	TotalMS   int64       `json:"total_ms"`
	Calls     []learnCall `json:"calls"`
	Answer    []EventItem `json:"answer"`
	Labels    *LabelSet   `json:"labels"`
}

// promptGroup is the Muse prompts of a scan: "id@version" of each answered
// Muse call, sorted, joined with " + "; "before P1" when the calls have no
// prompt fields; "no model call" for a barcode hit.
func promptGroup(l learnLine) string {
	seen := map[string]bool{}
	var keys []string
	muse, untagged := 0, 0
	for _, c := range l.Calls {
		if !strings.HasPrefix(c.Purpose, "muse:") || c.Status/100 != 2 {
			continue
		}
		muse++
		if c.ID == "" {
			untagged++
			continue
		}
		k := c.ID + "@" + c.Version
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	switch {
	case muse == 0:
		return "no model call"
	case len(keys) == 0:
		return "before P1"
	}
	sort.Strings(keys)
	if untagged > 0 {
		keys = append(keys, "before P1")
	}
	return strings.Join(keys, " + ")
}

func percentile(ms []int64, p float64) int64 {
	if len(ms) == 0 {
		return 0
	}
	s := append([]int64(nil), ms...)
	sort.Slice(s, func(a, b int) bool { return s[a] < s[b] })
	i := int(math.Ceil(p*float64(len(s)))) - 1
	return s[max(0, min(len(s)-1, i))]
}

// Learn reads analyze events lines (any order).
func Learn(lines [][]byte, confidentPx float64) LearnReport {
	rep := LearnReport{ByStatus: map[string]int{}}
	scans := map[string]learnLine{}
	last := map[string]learnLine{}
	for _, b := range lines {
		var l learnLine
		if json.Unmarshal(b, &l) != nil {
			continue
		}
		switch {
		case l.Step == "analyze":
			rep.AnalyzeLines++
			rep.ByStatus[fmt.Sprint(l.Status)]++
			rep.SpentUSD += l.CostUSD
			for _, c := range l.Calls {
				if c.ID == "unknown" {
					rep.UnknownPrompts++
				}
			}
			if l.Status != 200 {
				continue
			}
			switch l.Source {
			case SourceVision:
				rep.Vision++
			case SourceBarcode:
				rep.Barcode++
			}
			scans[l.RequestID] = l
		case l.Step == "labels" && l.Labels != nil:
			last[l.RequestID] = l // a later line replaces an earlier one
		}
	}
	rep.SpentUSD = math.Round(rep.SpentUSD*1e6) / 1e6
	byPrompt, byVia := map[string]*LearnTally{}, map[string]*LearnTally{}
	tally := func(m map[string]*LearnTally, k string) *LearnTally {
		t := m[k]
		if t == nil {
			t = &LearnTally{Group: k}
			m[k] = t
		}
		return t
	}
	for _, s := range scans {
		t := tally(byPrompt, promptGroup(s))
		t.Scans++
		t.CostUSD += s.CostUSD
		t.ms = append(t.ms, s.TotalMS)
	}
	count := func(t *LearnTally, it LabelItem) {
		t.Items++
		if !it.Kept {
			t.Removed++
			return
		}
		t.Kept++
		if normName(it.FinalTitle) == normName(it.ModelTitle) {
			t.NameSame++
		}
		if it.FinalServings == it.ModelServings {
			t.ServSame++
		}
	}
	fields := map[string]*FieldEdits{}
	pairs := map[string]map[[2]string]int{}
	ids := make([]string, 0, len(last))
	for id := range last {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	rep.LabelSets = len(ids)
	var diffs []float64
	for _, id := range ids {
		l := last[id]
		if l.Labels.Outcome != "posted" {
			continue
		}
		rep.Posted++
		s, ok := scans[id]
		if !ok {
			rep.NoScan++
		}
		via := map[int]string{}
		for _, a := range s.Answer {
			via[a.Item] = a.Via
		}
		for _, it := range l.Labels.Items {
			if ok {
				count(tally(byPrompt, promptGroup(s)), it)
			}
			v := via[it.Item]
			switch {
			case !ok:
				v = "no scan line"
			case v == "":
				v = s.Source + " (before LR1)"
			}
			count(tally(byVia, v), it)
			if it.Kept {
				rep.Servings.Items++
				d := it.ModelServings - it.FinalServings
				switch {
				case d == 0:
					rep.Servings.Same++
				case d > 0:
					rep.Servings.ModelHigh++
				default:
					rep.Servings.ModelLow++
				}
				diffs = append(diffs, float64(d))
			}
		}
		for _, e := range l.Labels.Edits {
			f := fields[e.Field]
			if f == nil {
				f = &FieldEdits{Field: e.Field, ByHow: map[string]int{}}
				fields[e.Field] = f
				pairs[e.Field] = map[[2]string]int{}
			}
			f.Edits++
			f.ByHow[e.How]++
			if e.Field != "pickup" && e.How != "undo" && (e.From != "" || e.To != "") {
				pairs[e.Field][[2]string{e.From, e.To}]++
			}
		}
	}
	if len(diffs) > 0 {
		sum := 0.0
		for _, d := range diffs {
			sum += d
		}
		rep.Servings.MeanDiff = math.Round(sum/float64(len(diffs))*100) / 100
	}
	for k, f := range fields {
		for p, n := range pairs[k] {
			f.Top = append(f.Top, Change{From: p[0], To: p[1], Count: n})
		}
		sort.Slice(f.Top, func(a, b int) bool {
			if f.Top[a].Count != f.Top[b].Count {
				return f.Top[a].Count > f.Top[b].Count
			}
			return f.Top[a].From+"\x00"+f.Top[a].To < f.Top[b].From+"\x00"+f.Top[b].To
		})
		if len(f.Top) > LearnTop {
			f.Top = f.Top[:LearnTop]
		}
		rep.Fields = append(rep.Fields, *f)
	}
	sort.Slice(rep.Fields, func(a, b int) bool {
		if rep.Fields[a].Edits != rep.Fields[b].Edits {
			return rep.Fields[a].Edits > rep.Fields[b].Edits
		}
		return rep.Fields[a].Field < rep.Fields[b].Field
	})
	flat := func(m map[string]*LearnTally) []LearnTally {
		var out []LearnTally
		for _, t := range m {
			t.P50MS, t.P95MS = percentile(t.ms, 0.5), percentile(t.ms, 0.95)
			t.CostUSD = math.Round(t.CostUSD*1e6) / 1e6
			out = append(out, *t)
		}
		sort.Slice(out, func(a, b int) bool {
			if out[a].Scans+out[a].Items != out[b].Scans+out[b].Items {
				return out[a].Scans+out[a].Items > out[b].Scans+out[b].Items
			}
			return out[a].Group < out[b].Group
		})
		return out
	}
	rep.ByPrompt, rep.ByVia = flat(byPrompt), flat(byVia)
	rep.Calibration = Calibrate(lines, confidentPx)
	return rep
}

// pct is n of d as a percent with one decimal ("-" when d is 0).
func pct(n, d int) string {
	if d == 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f%%", 100*float64(n)/float64(d))
}

// Markdown is the report for a person: counts only, no user id, no photo.
func (r LearnReport) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d analyze lines (", r.AnalyzeLines)
	var st []string
	for k := range r.ByStatus {
		st = append(st, k)
	}
	sort.Strings(st)
	for i, k := range st {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s: %d", k, r.ByStatus[k])
	}
	fmt.Fprintf(&b, "); %d vision and %d barcode answers; spent $%.4f.\n", r.Vision, r.Barcode, r.SpentUSD)
	fmt.Fprintf(&b, "%d label sets, %d posted (%d without their scan line). Calls with an unknown prompt: %d.\n\n",
		r.LabelSets, r.Posted, r.NoScan, r.UnknownPrompts)
	table := func(title string, ts []LearnTally) {
		fmt.Fprintf(&b, "| %s | Scans | Cost | p50 ms | p95 ms | Items | Kept | Name kept | Servings kept | Removed |\n|---|---|---|---|---|---|---|---|---|---|\n", title)
		for _, t := range ts {
			fmt.Fprintf(&b, "| %s | %d | $%.4f | %d | %d | %d | %d | %s | %s | %d |\n", t.Group, t.Scans, t.CostUSD, t.P50MS, t.P95MS,
				t.Items, t.Kept, pct(t.NameSame, t.Kept), pct(t.ServSame, t.Kept), t.Removed)
		}
		b.WriteString("\n")
	}
	table("Prompts (id@version)", r.ByPrompt)
	b.WriteString("| How the item was made | Items | Kept | Name kept | Servings kept | Removed |\n|---|---|---|---|---|---|\n")
	for _, t := range r.ByVia {
		fmt.Fprintf(&b, "| %s | %d | %d | %s | %s | %d |\n", t.Group, t.Items, t.Kept, pct(t.NameSame, t.Kept), pct(t.ServSame, t.Kept), t.Removed)
	}
	b.WriteString("\n")
	b.WriteString("| Field | Edits | By how | Most common changes |\n|---|---|---|---|\n")
	for _, f := range r.Fields {
		var hows []string
		for h, n := range f.ByHow {
			hows = append(hows, fmt.Sprintf("%s %d", h, n))
		}
		sort.Strings(hows)
		var top []string
		for _, c := range f.Top {
			top = append(top, fmt.Sprintf("%q -> %q (%d)", c.From, c.To, c.Count))
		}
		fmt.Fprintf(&b, "| %s | %d | %s | %s |\n", f.Field, f.Edits, strings.Join(hows, ", "), strings.Join(top, "; "))
	}
	s := r.Servings
	fmt.Fprintf(&b, "\nServings of %d kept items: %d the same, the model gave more for %d and less for %d (mean model - final: %.2f).\n\n",
		s.Items, s.Same, s.ModelHigh, s.ModelLow, s.MeanDiff)
	b.WriteString("Calibration (X44g):\n\n")
	b.WriteString(r.Calibration.Markdown())
	return b.String()
}
