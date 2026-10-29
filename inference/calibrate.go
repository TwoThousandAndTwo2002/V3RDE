package inference

// X44g (F6, "the self-improving pipeline"): what the donors' own corrections
// (the labels of X32g) say about the scanner, from the events files only.
//
// A labels line (step "labels") names the analyze request it labels; the
// analyze line of that request (step "analyze", 200, source vision) has the
// boxes of every item (SAM pixels, frame_w x frame_h) and the photo's size.
// So each labeled item gets the size of its largest box as Muse read it
// (PreparedSize + SeenSize: the same numbers as NameMinPx and ConfidentPx),
// and whether the donor kept the model's name and servings.
//
// The report gives, by box size and by day: how many items, how many names
// and servings the donor kept unchanged, how many items the donor removed.
// It suggests a ConfidentPx only from enough labels (MinLabels in the size
// group and in every bigger one, each at the Target share); otherwise it
// says there are not enough labels. It never changes a setting: a person
// reads it and changes the flag.

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

// Size groups (the short side of the box, in the pixels Muse reads). The
// limits are the engine's NameMinPx (64) and ConfidentPx (96), and above.
var calibEdges = []float64{64, 96, 128, 192}

// Calibration settings: a suggestion needs MinLabels items in the group and
// in each bigger group, each with at least Target of the names kept.
const (
	CalibMinLabels = 20
	CalibTarget    = 0.95
)

// Tally is one group of labeled items.
type Tally struct {
	Group    string  `json:"group"`
	Items    int     `json:"items"`     // labeled items (posted answers)
	Kept     int     `json:"kept"`      // the donor kept the item
	NameSame int     `json:"name_same"` // kept, and the final name is the model's (normName)
	ServSame int     `json:"servings_same"`
	Removed  int     `json:"removed"`
	FromPx   float64 `json:"from_px,omitempty"` // size groups: the lower edge
}

// CalibReport is the answer of Calibrate.
type CalibReport struct {
	Scans       int     `json:"scans"`      // vision scans with boxes
	LabelSets   int     `json:"label_sets"` // labels lines
	Posted      int     `json:"posted"`     // label sets with outcome posted (the ones counted)
	NoScan      int     `json:"no_scan"`    // label sets whose scan line is not in these files
	BySize      []Tally `json:"by_size"`
	ByDay       []Tally `json:"by_day"`
	ConfidentPx float64 `json:"confident_px"` // the engine's value now
	Suggest     float64 `json:"suggest_confident_px,omitempty"`
	Why         string  `json:"why"`
}

type calibScan struct {
	w, h, fw, fh int
	box          map[int][4]int // answer item -> its largest box
}

type calibLine struct {
	TS        string     `json:"ts"`
	Step      string     `json:"step"`
	RequestID string     `json:"request_id"`
	Status    int        `json:"status"`
	Source    string     `json:"source"`
	Width     int        `json:"width"`
	Height    int        `json:"height"`
	FrameW    int        `json:"frame_w"`
	FrameH    int        `json:"frame_h"`
	Boxes     []EventBox `json:"boxes"`
	Labels    *LabelSet  `json:"labels"`
}

// seenPx is the short side of an item's box as Muse read it; 0 = no box
// (a whole-photo answer).
func (s calibScan) seenPx(item int) float64 {
	b, ok := s.box[item]
	if !ok || s.fw <= 0 || s.fh <= 0 || s.w <= 0 || s.h <= 0 {
		return 0
	}
	pw, ph := PreparedSize(s.w, s.h)
	return SeenSize(Instance{Left: b[0], Top: b[1], Right: b[2], Bottom: b[3], W: s.fw, H: s.fh}, pw, ph).ShortPx
}

func sizeGroup(px float64) (string, float64) {
	if px <= 0 {
		return "whole photo (no box)", math.Inf(1)
	}
	lo := 0.0
	for _, e := range calibEdges {
		if px < e {
			return fmt.Sprintf("%.0f-%.0f px", lo, e), lo
		}
		lo = e
	}
	return fmt.Sprintf(">= %.0f px", lo), lo
}

// Calibrate reads analyze events lines (any order).
func Calibrate(lines [][]byte, confidentPx float64) CalibReport {
	rep := CalibReport{ConfidentPx: confidentPx}
	scans := map[string]calibScan{}
	var sets []calibLine
	for _, b := range lines {
		var l calibLine
		if json.Unmarshal(b, &l) != nil {
			continue
		}
		switch {
		case l.Step == "analyze" && l.Status == 200 && l.Source == SourceVision:
			s := calibScan{w: l.Width, h: l.Height, fw: l.FrameW, fh: l.FrameH, box: map[int][4]int{}}
			for _, x := range l.Boxes {
				if x.Outcome != "item" || x.Item == 0 {
					continue
				}
				area := func(b [4]int) int { return (b[2] - b[0]) * (b[3] - b[1]) }
				if old, ok := s.box[x.Item]; !ok || area(x.Box) > area(old) {
					s.box[x.Item] = x.Box
				}
			}
			scans[l.RequestID] = s
		case l.Step == "labels" && l.Labels != nil:
			sets = append(sets, l)
		}
	}
	rep.Scans, rep.LabelSets = len(scans), len(sets)
	bySize, byDay := map[string]*Tally{}, map[string]*Tally{}
	count := func(m map[string]*Tally, key string, from float64, it LabelItem) {
		t := m[key]
		if t == nil {
			t = &Tally{Group: key, FromPx: from}
			m[key] = t
		}
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
	for _, l := range sets {
		if l.Labels.Outcome != "posted" {
			continue
		}
		rep.Posted++
		s, ok := scans[l.RequestID]
		if !ok {
			rep.NoScan++
		}
		day := l.TS
		if len(day) >= 10 {
			day = day[:10]
		}
		for _, it := range l.Labels.Items {
			g, from := "no scan line", math.NaN()
			if ok {
				g, from = sizeGroup(s.seenPx(it.Item))
			}
			count(bySize, g, from, it)
			count(byDay, day, 0, it)
		}
	}
	for _, t := range bySize {
		if math.IsInf(t.FromPx, 1) || math.IsNaN(t.FromPx) {
			t.FromPx = 0
		}
		rep.BySize = append(rep.BySize, *t)
	}
	order := func(g string) float64 {
		for _, t := range bySize {
			if t.Group == g {
				if strings.HasPrefix(g, "whole") {
					return 1e9
				}
				if g == "no scan line" {
					return 2e9
				}
				return t.FromPx
			}
		}
		return 3e9
	}
	sort.Slice(rep.BySize, func(a, b int) bool { return order(rep.BySize[a].Group) < order(rep.BySize[b].Group) })
	for _, t := range byDay {
		rep.ByDay = append(rep.ByDay, *t)
	}
	sort.Slice(rep.ByDay, func(a, b int) bool { return rep.ByDay[a].Group < rep.ByDay[b].Group })
	rep.Suggest, rep.Why = suggest(rep.BySize)
	return rep
}

// suggest is the lowest size edge from which every size group has
// CalibMinLabels kept items and CalibTarget of their names unchanged.
func suggest(groups []Tally) (float64, string) {
	var sized []Tally
	for _, t := range groups {
		if strings.HasSuffix(t.Group, " px") {
			sized = append(sized, t)
		}
	}
	best := -1.0
	for i := len(sized) - 1; i >= 0; i-- {
		t := sized[i]
		if t.Kept < CalibMinLabels {
			break
		}
		if float64(t.NameSame) < CalibTarget*float64(t.Kept) {
			break
		}
		best = t.FromPx
	}
	switch {
	case len(sized) == 0:
		return 0, "no labeled item with a box yet"
	case best < 0:
		return 0, fmt.Sprintf("not enough labels: every size group from the top needs %d kept items with %.0f%% of the names unchanged", CalibMinLabels, CalibTarget*100)
	}
	return best, fmt.Sprintf("from %.0f px up, every size group has at least %d kept items and %.0f%% of the names unchanged", best, CalibMinLabels, CalibTarget*100)
}

// ReadEvents reads the analyze events files of days (YYYYMMDD; empty =
// every events file in dir).
func ReadEvents(dir string, days []string) ([][]byte, []string, error) {
	var files []string
	if len(days) == 0 {
		files, _ = filepath.Glob(filepath.Join(dir, "events-????????.jsonl"))
	} else {
		for _, d := range days {
			files = append(files, filepath.Join(dir, "events-"+d+".jsonl"))
		}
	}
	sort.Strings(files)
	var lines [][]byte
	var found []string
	for _, f := range files {
		fh, err := os.Open(f)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		sc := bufio.NewScanner(fh)
		sc.Buffer(make([]byte, 1<<20), 16<<20)
		for sc.Scan() {
			lines = append(lines, append([]byte(nil), sc.Bytes()...))
		}
		err = sc.Err()
		fh.Close()
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", f, err)
		}
		found = append(found, strings.TrimSuffix(strings.TrimPrefix(filepath.Base(f), "events-"), ".jsonl"))
	}
	return lines, found, nil
}

// Markdown is the report as two tables and the suggestion.
func (r CalibReport) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d vision scans, %d label sets (%d posted, %d without their scan line).\n\n", r.Scans, r.LabelSets, r.Posted, r.NoScan)
	table := func(title string, ts []Tally) {
		fmt.Fprintf(&b, "| %s | Items | Kept | Name unchanged | Servings unchanged | Removed |\n|---|---|---|---|---|---|\n", title)
		for _, t := range ts {
			fmt.Fprintf(&b, "| %s | %d | %d | %d | %d | %d |\n", t.Group, t.Items, t.Kept, t.NameSame, t.ServSame, t.Removed)
		}
		b.WriteString("\n")
	}
	table("Box size (as Muse reads it)", r.BySize)
	table("Day (UTC)", r.ByDay)
	fmt.Fprintf(&b, "ConfidentPx now: %.0f. ", r.ConfidentPx)
	if r.Suggest > 0 {
		fmt.Fprintf(&b, "Suggested: %.0f (%s).\n", r.Suggest, r.Why)
	} else {
		fmt.Fprintf(&b, "No suggestion: %s.\n", r.Why)
	}
	return b.String()
}
