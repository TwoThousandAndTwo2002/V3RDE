package inference

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// Size levels (T9): < 64 px seen = counted, not named (closer photo);
// 64-96 px = named with Check; >= 96 px = named.
func TestSizeLevels(t *testing.T) {
	var prompt string
	f := &fakeAPI{sam: func(string) (int, string) {
		// 800 x 600 photo, not resized: short sides 150, 80, 40 px
		return 200, samLine(t, [4]int{0, 0, 149, 199}, [4]int{300, 0, 379, 99}, [4]int{600, 400, 639, 439})
	}, muse: func(p string, _ map[string]any) map[string]any {
		prompt = p
		return textResp("m", itemsText(ent(1, nil, "Soup"), ent(2, nil, "Beans")))
	}}
	e, done := engine(t, f)
	defer done()
	inv, err := e.Run(context.Background(), testPhoto(t), "image/jpeg", ModeAuto)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(prompt, "\n3: [") || inv.Small != 1 || !inv.CloserPhoto || inv.Named != 2 || inv.Counted() != 3 {
		t.Fatalf("small %d named %d counted %d\n%s", inv.Small, inv.Named, inv.Counted(), prompt)
	}
	byName := map[string]Item{}
	for _, it := range inv.Items {
		byName[it.Name] = it
	}
	if byName["Soup"].Check || !byName["Beans"].Check {
		t.Fatalf("%+v", inv.Items)
	}
}

// Every box too small: analyze's answer for the photo, and "closer photo".
func TestAllBoxesTooSmallGivesWholeAndCloserPhoto(t *testing.T) {
	f := &fakeAPI{sam: func(string) (int, string) {
		return 200, samLine(t, [4]int{0, 0, 39, 39}, [4]int{100, 100, 139, 139})
	}, muse: func(string, map[string]any) map[string]any {
		b, _ := json.Marshal(contract("Canned goods"))
		return textResp("m", string(b))
	}}
	e, done := engine(t, f)
	defer done()
	inv, err := e.Run(context.Background(), testPhoto(t), "image/jpeg", ModeAuto)
	if err != nil || !inv.CloserPhoto || inv.Small != 2 || len(inv.Items) != 1 || inv.Items[0].Source != "whole" {
		t.Fatal(err, inv.CloserPhoto, inv.Small, inv.Items)
	}
}

// A tray is ONE listing, servings from the model (tray size); food inside
// it joins it; two identical trays stay two listings; a package inside a
// tray stays its own item (containers never merge).
func TestTraysAreOneListingWithModelServings(t *testing.T) {
	f := &fakeAPI{sam: func(c string) (int, string) {
		switch c {
		case "food tray":
			return 200, samLine(t, [4]int{0, 0, 299, 299}, [4]int{400, 0, 699, 299})
		case "package":
			return 200, samLine(t, [4]int{20, 20, 119, 119}) // a box of rolls in tray 1
		}
		// chicken pieces inside the trays
		return 200, samLine(t, [4]int{150, 150, 249, 249}, [4]int{450, 50, 549, 149}, [4]int{550, 150, 649, 249})
	}, muse: func(p string, _ map[string]any) map[string]any {
		t1, t2, r := ent(1, nil, "Roasted chicken tray"), ent(2, 1.0, "Roasted chicken tray"), ent(3, nil, "Dinner rolls")
		t1["tray"], t2["tray"] = true, true
		t1["number_of_serving"], t2["number_of_serving"] = 8.0, 10.0
		return textResp("m", itemsText(t1, t2, r))
	}}
	e, done := engine(t, f)
	defer done()
	e.PackageConcept, e.TrayConcept = "package", "food tray"
	inv, err := e.Run(context.Background(), testPhoto(t), "image/jpeg", ModeAuto)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Trays != 2 || inv.Packages != 1 || inv.Contained != 3 || len(inv.Items) != 3 {
		t.Fatalf("trays %d packages %d contained %d items %+v", inv.Trays, inv.Packages, inv.Contained, inv.Items)
	}
	servings := map[float64]bool{}
	for _, it := range inv.Items {
		if it.Tray {
			if it.Count != 1 {
				t.Fatal("a tray is one listing", it)
			}
			servings[it.NumberOfServing] = true
		}
	}
	if !servings[8] || !servings[10] {
		t.Fatalf("%+v", inv.Items)
	}
}

// Split: the named boxes go out in Split calls at the same time, in
// reading order; identical products in different calls still group by name.
func TestSplitCallsRunInParallel(t *testing.T) {
	var mu sync.Mutex
	inFlight, peak := 0, 0
	var wg sync.WaitGroup
	wg.Add(2)
	f := &fakeAPI{sam: func(string) (int, string) {
		// two rows of two boxes
		return 200, samLine(t, [4]int{0, 0, 149, 149}, [4]int{200, 0, 349, 149}, [4]int{0, 300, 159, 449}, [4]int{200, 300, 359, 449})
	}, muse: func(p string, _ map[string]any) map[string]any {
		mu.Lock()
		inFlight++
		peak = max(peak, inFlight)
		mu.Unlock()
		wg.Done()
		wg.Wait() // both calls must be open together
		mu.Lock()
		inFlight--
		mu.Unlock()
		return textResp("m", itemsText(ent(1, nil, "Cola can"), ent(2, nil, "Cola can")))
	}}
	e, done := engine(t, f)
	defer done()
	e.Split, e.SplitMin = 2, 2
	inv, err := e.Run(context.Background(), testPhoto(t), "image/jpeg", ModeAuto)
	if err != nil {
		t.Fatal(err)
	}
	p := strings.Join(purposes(inv), ",")
	if peak != 2 || !strings.Contains(p, "muse:single-1") || !strings.Contains(p, "muse:single-2") {
		t.Fatal(peak, p)
	}
	if len(inv.Items) != 1 || inv.Items[0].Count != 4 {
		t.Fatalf("%+v", inv.Items)
	}
}

func TestChunkReadingOrder(t *testing.T) {
	// boxes numbered by area; reading order is top row left to right, then the next row
	named := []Instance{
		{Left: 200, Top: 300, Right: 400, Bottom: 450}, // 1: bottom right
		{Left: 0, Top: 0, Right: 190, Bottom: 150},     // 2: top left
		{Left: 0, Top: 300, Right: 180, Bottom: 450},   // 3: bottom left
		{Left: 200, Top: 0, Right: 370, Bottom: 150},   // 4: top right
	}
	if got := fmt.Sprint(chunkReading(named, 2)); got != "[[2 4] [3 1]]" {
		t.Fatal(got)
	}
}

// The agent's segment calls in one turn run at the same time.
func TestAgentSegmentsInParallel(t *testing.T) {
	var mu sync.Mutex
	inFlight, peak := 0, 0
	var wg sync.WaitGroup
	wg.Add(2)
	f := &fakeAPI{sam: func(c string) (int, string) {
		mu.Lock()
		inFlight++
		peak = max(peak, inFlight)
		mu.Unlock()
		wg.Done()
		wg.Wait()
		mu.Lock()
		inFlight--
		mu.Unlock()
		if c == "apple" {
			return 200, samLine(t, [4]int{0, 0, 199, 199})
		}
		return 200, samLine(t, [4]int{400, 0, 599, 199})
	}}
	turn := 0
	f.muse = func(_ string, body map[string]any) map[string]any {
		turn++
		if turn == 1 {
			return map[string]any{"id": "r1", "output": []any{
				map[string]any{"type": "function_call", "call_id": "c1", "name": "segment", "arguments": `{"concept":"apple"}`},
				map[string]any{"type": "function_call", "call_id": "c2", "name": "segment", "arguments": `{"concept":"pear"}`}}}
		}
		return textResp("r2", itemsText(ent(1, nil, "Apple"), ent(2, nil, "Pear")))
	}
	e, done := engine(t, f)
	defer done()
	inv, err := e.Run(context.Background(), testPhoto(t), "image/jpeg", ModeAgent)
	if err != nil || peak != 2 || len(inv.Items) != 2 {
		t.Fatal(err, peak, inv.Items)
	}
}

// Split only when enough boxes are named.
func TestSplitOnlyFromSplitMin(t *testing.T) {
	f := &fakeAPI{sam: func(string) (int, string) {
		return 200, samLine(t, [4]int{0, 0, 149, 149}, [4]int{200, 0, 349, 149})
	}, muse: func(string, map[string]any) map[string]any {
		return textResp("m", itemsText(ent(1, nil, "A"), ent(2, nil, "B")))
	}}
	e, done := engine(t, f)
	defer done()
	e.Split = 3 // SplitMin stays 12
	inv, err := e.Run(context.Background(), testPhoto(t), "image/jpeg", ModeAuto)
	if p := strings.Join(purposes(inv), ","); err != nil || p != "sam:food,muse:single" {
		t.Fatal(err, p)
	}
}

// A board under 3 pans is a holder: the pans are the items. A donation box
// with 2 packages is a holder. A tray with 1 package on it stays.
func TestDropHolders(t *testing.T) {
	board := Instance{ID: "board", Left: 0, Top: 0, Right: 900, Bottom: 300}
	pans := []Instance{{ID: "p1", Left: 10, Top: 10, Right: 290, Bottom: 290}, {ID: "p2", Left: 310, Top: 10, Right: 590, Bottom: 290},
		{ID: "p3", Left: 610, Top: 10, Right: 890, Bottom: 290}}
	tray := Instance{ID: "tray", Left: 0, Top: 400, Right: 300, Bottom: 600}
	box := Instance{ID: "box", Left: 10, Top: 410, Right: 100, Bottom: 500}
	out := dropHolders(append(append([]Instance{board}, pans...), tray, box))
	var ids []string
	for _, o := range out {
		ids = append(ids, o.ID)
	}
	if strings.Join(ids, ",") != "p1,p2,p3,tray,box" {
		t.Fatal(ids)
	}
	// a package and its front panel (one inside, more than half its area): one object
	pk := Instance{ID: "pk", Left: 0, Top: 0, Right: 200, Bottom: 300}
	panel := Instance{ID: "panel", Left: 10, Top: 10, Right: 190, Bottom: 290}
	if out := dropHolders([]Instance{pk, panel}); len(out) != 1 || out[0].ID != "pk" {
		t.Fatal(out)
	}
	// and keepIoU keeps the pans that keep() would drop as "inside"
	e := &Engine{}
	if n := len(e.keepIoU(append([]Instance{board}, pans...))); n != 4 {
		t.Fatal(n)
	}
}

func TestNormNameAcrossCalls(t *testing.T) {
	for _, c := range [][2]string{
		{"Go Pure Fruit and Oat Bars", "Go Pure Fruit Oat Bar"},
		{"Mac & Cheese", "mac and cheese"},
		{"The Doritos Nacho Cheese", "doritos nacho cheese"},
	} {
		if normName(c[0]) != normName(c[1]) {
			t.Fatal(c, normName(c[0]), normName(c[1]))
		}
	}
	if normName("Santitas White Corn Tortilla Chips") == normName("Santitas Tortilla Chips") || normName("Glass") != "glass" {
		t.Fatal("over-merged")
	}
}

// Tray servings come from the Vigor pan chart (main 6 oz, side 4 oz); a
// non-hotel pan keeps the model's estimate.
func TestPanServingsTable(t *testing.T) {
	for _, c := range []struct {
		p    TrayPan
		want int
		ok   bool
	}{
		{TrayPan{Size: "half", Depth: "shallow", Dish: "main"}, 22, true},
		{TrayPan{Size: "half", Depth: "shallow", Dish: "side"}, 34, true},
		{TrayPan{Size: "full", Depth: "deep", Dish: "main"}, 114, true},
		{TrayPan{Size: "quarter", Depth: "", Dish: ""}, 10, true}, // unknown = shallow, main
		{TrayPan{Size: "other"}, 0, false},
	} {
		if n, ok := PanServings(c.p); n != c.want || ok != c.ok {
			t.Fatal(c.p, n, ok)
		}
	}
	f := &fakeAPI{sam: func(c string) (int, string) {
		if c == "food tray" {
			return 200, samLine(t, [4]int{0, 0, 299, 299}, [4]int{400, 0, 699, 299})
		}
		return 200, "<0f>\n"
	}, muse: func(string, map[string]any) map[string]any {
		a, b := ent(1, nil, "Chicken wings"), ent(2, nil, "Cheese platter")
		a["tray"], a["pan"], a["depth"], a["dish"], a["number_of_serving"] = true, "half", "shallow", "main", 30.0
		b["tray"], b["pan"], b["number_of_serving"] = true, "other", 9.0
		return textResp("m", itemsText(a, b))
	}}
	e, done := engine(t, f)
	defer done()
	e.TrayConcept = "food tray"
	inv, err := e.Run(context.Background(), testPhoto(t), "image/jpeg", ModeAuto)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Item{}
	for _, it := range inv.Items {
		got[it.Name] = it
	}
	w, c := got["Chicken wings"], got["Cheese platter"]
	if w.NumberOfServing != 22 || w.Pan == nil || !w.Pan.FromTable || w.Pan.ModelServings != 30 || c.NumberOfServing != 9 || c.Pan.FromTable {
		t.Fatalf("%+v %+v", w, c)
	}
}
