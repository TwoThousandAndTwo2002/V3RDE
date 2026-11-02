package inference

import (
	"encoding/json"
	"strings"
	"testing"

	analyze "github.com/TwoThousandAndTwo2002/Icarus/server"
)

func jline(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func scanLine(id, source string, cost float64, ms int64, calls []EventCall, answer []EventItem) Event {
	return Event{TS: "2026-11-01T10:00:00Z", Service: "analyze", Step: "analyze", RequestID: id, Status: 200,
		Source: source, CostUSD: cost, TotalMS: ms, Calls: calls, Answer: answer}
}

func museCall(purpose, id, version string) EventCall {
	return EventCall{Purpose: purpose, Status: 200, PromptRef: analyze.PromptRef{ID: id, Version: version}}
}

func labelLine(id, ts, outcome string, items []LabelItem, edits []LabelEdit) Event {
	return Event{TS: ts, Service: "analyze", Step: "labels", RequestID: id, Status: 204,
		Labels: &LabelSet{Outcome: outcome, Items: items, Edits: edits}}
}

func TestLearnReport(t *testing.T) {
	sam := EventCall{Purpose: "sam:food", Status: 200, PromptRef: analyze.PromptRef{ID: "sam.concept", Version: "1"}}
	lines := [][]byte{
		// A: two-pass prompts v1, two items
		jline(t, scanLine("a", SourceVision, 0.02, 9000, []EventCall{sam, museCall("muse:names", "twopass.names", "1"),
			museCall("muse:details", "twopass.details", "1")},
			[]EventItem{{Item: 1, Via: "two-pass"}, {Item: 2, Via: "two-pass"}})),
		// B: the whole-photo prompt, one item
		jline(t, scanLine("b", SourceVision, 0.01, 4000, []EventCall{museCall("muse:whole", "analyze.whole", "1")},
			[]EventItem{{Item: 1, Via: "whole"}})),
		// C: before P1 (no prompt fields), no answer items (before LR1)
		jline(t, scanLine("c", SourceVision, 0.01, 5000, []EventCall{{Purpose: "muse:whole", Status: 200}}, nil)),
		// D: a barcode hit, no model call
		jline(t, scanLine("d", SourceBarcode, 0, 300, nil, []EventItem{{Item: 1, Via: SourceBarcode}})),
		// an error line and an unknown prompt
		jline(t, Event{Step: "analyze", RequestID: "e", Status: 422, CostUSD: 0.005,
			Calls: []EventCall{museCall("muse:other", analyze.Unknown, "")}}),
		[]byte("not json"),
		// labels: A posted twice (the last one counts), B posted, C retake, D posted, X no scan line
		// (the earlier line has the later-looking ts text: RFC3339Nano drops zeros; the line order decides)
		jline(t, labelLine("a", "2026-11-01T10:02:00.5Z", "posted", []LabelItem{{Item: 1, ModelTitle: "Beans", FinalTitle: "Old", Kept: true}}, nil)),
		jline(t, labelLine("a", "2026-11-01T10:02:00Z", "posted", []LabelItem{
			{Item: 1, ModelTitle: "Canned Beans", FinalTitle: "canned bean", ModelServings: 4, FinalServings: 2, Kept: true},
			{Item: 2, ModelTitle: "Rice", FinalTitle: "Rice", Kept: false}},
			[]LabelEdit{{Item: 1, Field: "servings", From: "4", To: "2", How: "hand"},
				{Item: 2, Field: "remove", How: "hand"}, // no values: no pair
				{Item: 1, Field: "condition", From: "sealed", To: "ready_to_eat", How: "voice"},
				{Item: 0, Field: "pickup", How: "text"}})),
		jline(t, labelLine("b", "2026-11-01T10:03:00Z", "posted", []LabelItem{
			{Item: 1, ModelTitle: "Soup", FinalTitle: "Tomato soup", ModelServings: 1, FinalServings: 3, Kept: true}},
			[]LabelEdit{{Item: 1, Field: "title", From: "Soup", To: "Tomato soup", How: "hand"},
				{Item: 1, Field: "servings", From: "1", To: "3", How: "hand"},
				{Item: 1, Field: "servings", From: "3", To: "1", How: "undo"}})),
		jline(t, labelLine("c", "2026-11-01T10:04:00Z", "retake", nil, nil)),
		jline(t, labelLine("d", "2026-11-01T10:05:00Z", "posted", []LabelItem{
			{Item: 1, ModelTitle: "Sauce", FinalTitle: "Sauce", ModelServings: 13, FinalServings: 13, Kept: true}}, nil)),
		jline(t, labelLine("x", "2026-11-01T10:06:00Z", "posted", []LabelItem{{Item: 1, ModelTitle: "Tea", FinalTitle: "Tea", Kept: true}}, nil)),
	}
	r := Learn(lines, 96)
	if r.AnalyzeLines != 5 || r.ByStatus["200"] != 4 || r.ByStatus["422"] != 1 || r.Vision != 3 || r.Barcode != 1 {
		t.Fatalf("counts %+v", r)
	}
	if r.LabelSets != 5 || r.Posted != 4 || r.NoScan != 1 || r.UnknownPrompts != 1 || r.SpentUSD != 0.045 {
		t.Fatalf("labels %d posted %d noscan %d unknown %d spent %v", r.LabelSets, r.Posted, r.NoScan, r.UnknownPrompts, r.SpentUSD)
	}
	g := map[string]LearnTally{}
	for _, x := range r.ByPrompt {
		g[x.Group] = x
	}
	two := g["twopass.details@1 + twopass.names@1"]
	if two.Scans != 1 || two.Items != 2 || two.Kept != 1 || two.NameSame != 1 || two.ServSame != 0 || two.Removed != 1 || two.P50MS != 9000 {
		t.Errorf("two-pass group %+v", two)
	}
	if w := g["analyze.whole@1"]; w.Scans != 1 || w.Kept != 1 || w.NameSame != 0 {
		t.Errorf("whole group %+v", w)
	}
	if g["before P1"].Scans != 1 || g["no model call"].Scans != 1 || g["no model call"].Kept != 1 {
		t.Errorf("groups %+v", r.ByPrompt)
	}
	v := map[string]LearnTally{}
	for _, x := range r.ByVia {
		v[x.Group] = x
	}
	if v["two-pass"].Items != 2 || v["whole"].Items != 1 || v[SourceBarcode].Items != 1 || v["no scan line"].Items != 1 {
		t.Errorf("by via %+v", r.ByVia)
	}
	f := map[string]FieldEdits{}
	for _, x := range r.Fields {
		f[x.Field] = x
	}
	if s := f["servings"]; s.Edits != 3 || s.ByHow["undo"] != 1 || len(s.Top) != 2 {
		t.Errorf("servings edits %+v", s)
	}
	if c := f["condition"]; c.Edits != 1 || c.Top[0] != (Change{From: "sealed", To: "ready_to_eat", Count: 1}) {
		t.Errorf("condition edits %+v", c)
	}
	if rm := f["remove"]; rm.Edits != 1 || len(rm.Top) != 0 {
		t.Errorf("remove edits %+v", rm)
	}
	if p := f["pickup"]; p.Edits != 1 || len(p.Top) != 0 {
		t.Errorf("pickup must give no values %+v", p)
	}
	if r.Fields[0].Field != "servings" {
		t.Errorf("fields order %+v", r.Fields)
	}
	// kept items: a (4->2), b (1->3), d (13), x (0)
	if s := r.Servings; s.Items != 4 || s.Same != 2 || s.ModelHigh != 1 || s.ModelLow != 1 || s.MeanDiff != 0 {
		t.Errorf("servings %+v", s)
	}
	md := r.Markdown()
	for _, want := range []string{"twopass.details@1 + twopass.names@1", "| servings | 3 |", `"sealed" -> "ready_to_eat" (1)`,
		"unknown prompt: 1", "Calibration (X44g)"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown has no %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "u1") || strings.Contains(md, "photo_id") {
		t.Error("markdown names a user or a photo")
	}
}

func TestLearnEmpty(t *testing.T) {
	r := Learn(nil, 96)
	if r.AnalyzeLines != 0 || len(r.ByPrompt) != 0 || r.Servings.Items != 0 {
		t.Fatalf("%+v", r)
	}
	_ = r.Markdown()
}
