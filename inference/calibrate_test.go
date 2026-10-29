package inference

// X44g: the calibration report from the donors' labels.

import (
	"encoding/json"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreparedSizeIsPrepare(t *testing.T) {
	for _, wh := range [][2]int{{800, 600}, {4032, 3024}, {1792, 1792}, {2241, 2240}} {
		p := Prepare(image.NewRGBA(image.Rect(0, 0, wh[0], wh[1])))
		if w, h := PreparedSize(wh[0], wh[1]); w != p.W || h != p.H {
			t.Fatalf("%v: PreparedSize %dx%d, Prepare %dx%d", wh, w, h, p.W, p.H)
		}
	}
}

func evLine(t *testing.T, e Event) []byte {
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func scanEv(t *testing.T, id string, boxes ...EventBox) []byte {
	return evLine(t, Event{TS: "2026-10-29T10:00:00Z", Service: "analyze", Step: "analyze", RequestID: id, Status: 200,
		Source: SourceVision, Width: 800, Height: 600, FrameW: 800, FrameH: 600, Boxes: boxes})
}

func labelEv(t *testing.T, ts, id, outcome string, items ...LabelItem) []byte {
	return evLine(t, Event{TS: ts, Service: "analyze", Step: "labels", RequestID: id, Status: 204,
		Labels: &LabelSet{Outcome: outcome, Items: items}})
}

func tallyOf(t *testing.T, ts []Tally, group string) Tally {
	t.Helper()
	for _, x := range ts {
		if x.Group == group {
			return x
		}
	}
	t.Fatalf("no group %q in %+v", group, ts)
	return Tally{}
}

func TestCalibrateJoinsLabelsAndBoxes(t *testing.T) {
	id, other := strings.Repeat("a", 32), strings.Repeat("b", 32)
	lines := [][]byte{
		scanEv(t, id,
			EventBox{N: 1, Box: [4]int{0, 0, 200, 300}, Outcome: "item", Item: 1},     // 200 px short side
			EventBox{N: 2, Box: [4]int{300, 0, 340, 40}, Outcome: "item", Item: 1},    // smaller box of item 1: not used
			EventBox{N: 3, Box: [4]int{500, 100, 540, 150}, Outcome: "item", Item: 2}, // 40 px
			EventBox{N: 4, Box: [4]int{600, 0, 700, 100}, Outcome: "not_food"}),
		labelEv(t, "2026-10-29T10:01:00Z", id, "posted",
			LabelItem{Item: 1, ModelTitle: "Tomato Soups", FinalTitle: "tomato soup", ModelServings: 2, FinalServings: 2, Kept: true},
			LabelItem{Item: 2, ModelTitle: "Beans", FinalTitle: "Lentils", ModelServings: 1, FinalServings: 3, Kept: true},
			LabelItem{Item: 3, ModelTitle: "Bread", Kept: false}),
		labelEv(t, "2026-10-29T10:02:00Z", id, "retake", LabelItem{Item: 1, ModelTitle: "x", FinalTitle: "y", Kept: true}),
		labelEv(t, "2026-10-30T09:00:00Z", other, "posted", LabelItem{Item: 1, ModelTitle: "Rice", FinalTitle: "Rice", ModelServings: 4, FinalServings: 4, Kept: true}),
		[]byte("not json"),
	}
	r := Calibrate(lines, 96)
	if r.Scans != 1 || r.LabelSets != 3 || r.Posted != 2 || r.NoScan != 1 {
		t.Fatalf("counts %+v", r)
	}
	if g := tallyOf(t, r.BySize, ">= 192 px"); g != (Tally{Group: ">= 192 px", Items: 1, Kept: 1, NameSame: 1, ServSame: 1, FromPx: 192}) {
		t.Fatalf("200 px item: %+v", g)
	}
	if g := tallyOf(t, r.BySize, "0-64 px"); g.Items != 1 || g.NameSame != 0 || g.ServSame != 0 {
		t.Fatalf("40 px item: %+v", g)
	}
	if g := tallyOf(t, r.BySize, "whole photo (no box)"); g.Items != 1 || g.Removed != 1 {
		t.Fatalf("item with no box: %+v", g)
	}
	if g := tallyOf(t, r.BySize, "no scan line"); g.Items != 1 || g.NameSame != 1 {
		t.Fatalf("no scan: %+v", g)
	}
	if d := tallyOf(t, r.ByDay, "2026-10-29"); d.Items != 3 || d.Kept != 2 || d.Removed != 1 {
		t.Fatalf("day: %+v", d)
	}
	if r.BySize[0].Group != "0-64 px" || r.BySize[len(r.BySize)-1].Group != "no scan line" {
		t.Fatalf("order: %+v", r.BySize)
	}
	if r.Suggest != 0 || !strings.Contains(r.Why, "not enough labels") {
		t.Fatalf("suggestion from 2 items: %v %q", r.Suggest, r.Why)
	}
	md := r.Markdown()
	if !strings.Contains(md, "| >= 192 px | 1 | 1 | 1 | 1 | 0 |") || !strings.Contains(md, "No suggestion") {
		t.Fatal(md)
	}
}

func TestCalibrateSuggests(t *testing.T) {
	var lines [][]byte
	n := 0
	add := func(short, same, changed int) { // items of this short side: same names, changed names
		for i := 0; i < same+changed; i++ {
			n++
			id := strings.Repeat("0", 28) + string("0123456789abcdef"[n/256%16]) + string("0123456789abcdef"[n/16%16]) + string("0123456789abcdef"[n%16]) + "f"
			lines = append(lines, scanEv(t, id, EventBox{N: 1, Box: [4]int{0, 0, short, short}, Outcome: "item", Item: 1}))
			final := "Soup"
			if i >= same {
				final = "Stew"
			}
			lines = append(lines, labelEv(t, "2026-10-29T10:00:00Z", id, "posted",
				LabelItem{Item: 1, ModelTitle: "Soup", FinalTitle: final, ModelServings: 1, FinalServings: 1, Kept: true}))
		}
	}
	add(300, 20, 0) // >= 192: 20 of 20
	add(150, 19, 1) // 128-192: 19 of 20 = the target
	add(100, 18, 2) // 96-128: under the target
	add(80, 20, 0)  // 64-96: good, but under a group that is not
	r := Calibrate(lines, 96)
	if r.Suggest != 128 || !strings.Contains(r.Why, "from 128 px up") {
		t.Fatalf("suggest %v %q %+v", r.Suggest, r.Why, r.BySize)
	}
}

func TestReadEventsDays(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "events-20261029.jsonl"), []byte("{\"step\":\"a\"}\n{\"step\":\"b\"}\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "events-20261030.jsonl"), []byte("{\"step\":\"c\"}\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x\n"), 0o644)
	all, days, err := ReadEvents(dir, nil)
	if err != nil || len(all) != 3 || strings.Join(days, ",") != "20261029,20261030" {
		t.Fatal(err, len(all), days)
	}
	one, days, err := ReadEvents(dir, []string{"20261030", "20261101"})
	if err != nil || len(one) != 1 || strings.Join(days, ",") != "20261030" {
		t.Fatal(err, len(one), days)
	}
}
