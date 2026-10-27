package inference

import (
	"bytes"
	"context"
	b64std "encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"

	analyze "github.com/TwoThousandAndTwo2002/Icarus/server"
)

// ---- a fake Meta API ---------------------------------------------------------

type fakeAPI struct {
	t      *testing.T
	mu     sync.Mutex
	sam    func(concept string) (int, string)                           // status, SAM text
	muse   func(purposeText string, body map[string]any) map[string]any // full response body
	bodies []map[string]any
}

func (f *fakeAPI) server() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k" {
			w.WriteHeader(401)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.bodies = append(f.bodies, body)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if body["model"] == DefaultSAM {
			st, text := f.sam(firstText(body))
			w.WriteHeader(st)
			_ = json.NewEncoder(w).Encode(textResp("sam", text))
			return
		}
		_ = json.NewEncoder(w).Encode(f.muse(firstText(body), body))
	}))
}

func firstText(body map[string]any) string {
	in, _ := body["input"].([]any)
	for _, m := range in {
		mm, _ := m.(map[string]any)
		cs, _ := mm["content"].([]any)
		for _, c := range cs {
			cm, _ := c.(map[string]any)
			if cm["type"] == "input_text" {
				return cm["text"].(string)
			}
		}
	}
	return ""
}

func textResp(id, text string) map[string]any {
	return map[string]any{"id": id, "output": []any{map[string]any{"type": "message",
		"content": []any{map[string]any{"type": "output_text", "text": text}}}},
		"usage": map[string]any{"input_tokens": 10.0, "output_tokens": 5.0}}
}

func contract(name string) map[string]any {
	return map[string]any{"name": name, "description": "A " + name + ".", "is_non_veg": 0.0, "number_of_serving": 3.0,
		"serving_type": "individual", "meal_condition": "sealed", "weight": 0.5, "meal_cost": 4.0}
}

func itemsText(entries ...map[string]any) string {
	b, _ := json.Marshal(map[string]any{"items": entries})
	return "Here you go:\n" + string(b)
}

func ent(box int, sameAs any, name string) map[string]any {
	m := contract(name)
	m["box"], m["same_as"] = float64(box), sameAs
	return m
}

// samLine builds SAM output for boxes [x1,y1,x2,y2] (inclusive) with a real
// one_bit payload from the goldens.
func samLine(t *testing.T, boxes ...[4]int) string {
	g := loadGoldens(t)
	m := g.Masks[3]
	var recs []string
	for i, b := range boxes {
		recs = append(recs, fmt.Sprintf("%d<|box;x1=%d;y1=%d;x2=%d;y2=%d;w=800;h=600;c=0.9|><|mask;x=0;y=0;data=%d,%d,%s;c=0.9|>",
			i, b[0], b[1], b[2], b[3], m.H, m.W, m.Payload))
	}
	return "<0f>" + strings.Join(recs, ",") + "\n"
}

func testPhoto(t *testing.T) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 800, 600))
	for y := 0; y < 600; y++ {
		for x := 0; x < 800; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 90, 255})
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func engine(t *testing.T, f *fakeAPI) (*Engine, func()) {
	f.t = t
	srv := f.server()
	return &Engine{API: &Client{URL: srv.URL, Key: "k"}}, srv.Close
}

func purposes(inv *Inventory) []string {
	var p []string
	for _, c := range inv.Calls {
		p = append(p, c.Purpose)
	}
	return p
}

// ---- tests ------------------------------------------------------------------

func TestOneItemPhotoIsExactlyAnalyze(t *testing.T) {
	var prompt string
	f := &fakeAPI{sam: func(string) (int, string) { return 200, samLine(t, [4]int{100, 100, 399, 499}) },
		muse: func(p string, _ map[string]any) map[string]any {
			prompt = p
			b, _ := json.Marshal(contract("Marinara sauce"))
			return textResp("m", string(b))
		}}
	e, done := engine(t, f)
	defer done()
	inv, err := e.Run(context.Background(), testPhoto(t), "image/jpeg", ModeAuto)
	if err != nil {
		t.Fatal(err)
	}
	if prompt != analyze.Prompt {
		t.Fatal("the one-item path must send analyze.Prompt verbatim")
	}
	it := inv.Items[0]
	want, _ := analyze.Validate(contract("Marinara sauce"))
	want.NumberOfServing = 1
	if len(inv.Items) != 1 || it.Result != want || it.Count != 1 || it.Source != "whole" ||
		len(it.Boxes) != 1 || it.Boxes[0] != [4]int{100, 100, 400, 500} || func() analyze.Result { r, _ := inv.TopLevel(); return r }() != want {
		t.Fatalf("%+v", inv.Items)
	}
	if p := purposes(inv); strings.Join(p, ",") != "sam:food,muse:whole" {
		t.Fatal(p)
	}
}

// SAM finds nothing (or fails): the agent runs; an agent that segments
// nothing gives the analyze-identical whole call.
func TestNoInstanceOrSAMFailureGoesToAgentThenWhole(t *testing.T) {
	for _, sam := range []func(string) (int, string){
		func(string) (int, string) { return 200, "<0f>\n" },
		func(string) (int, string) { return 500, "boom" },
	} {
		f := &fakeAPI{sam: sam, muse: func(string, map[string]any) map[string]any {
			b, _ := json.Marshal(contract("Rice bowl"))
			return textResp("m", string(b))
		}}
		e, done := engine(t, f)
		inv, err := e.Run(context.Background(), testPhoto(t), "image/jpeg", ModeAuto)
		done()
		if err != nil || len(inv.Items) != 1 || inv.Items[0].Name != "Rice bowl" || len(inv.Items[0].Boxes) != 0 ||
			inv.Items[0].NumberOfServing != 1 || inv.Items[0].Source != "whole" {
			t.Fatal(err, inv)
		}
		if p := purposes(inv); strings.Join(p, ",") != "sam:food,muse:agent:0,muse:whole" {
			t.Fatal(p)
		}
	}
}

func TestSeveralItemsOneCallGroupsIdenticalProducts(t *testing.T) {
	var prompt string
	f := &fakeAPI{sam: func(string) (int, string) {
		return 200, samLine(t, [4]int{0, 0, 199, 299}, [4]int{200, 0, 399, 299}, [4]int{500, 300, 599, 399})
	}, muse: func(p string, _ map[string]any) map[string]any {
		prompt = p
		return textResp("m", itemsText(ent(1, nil, "Cheerios"), ent(2, 1.0, "Cheerios cereal"), ent(3, nil, "Honey Maid")))
	}}
	e, done := engine(t, f)
	defer done()
	inv, err := e.Run(context.Background(), testPhoto(t), "image/jpeg", ModeAuto)
	if err != nil {
		t.Fatal(err)
	}
	// boxes on Muse's 0-1000 grid (800 x 600 photo; SAM's x2/y2 are inclusive)
	if !strings.Contains(prompt, "1: [0, 0, 250, 500]") || !strings.Contains(prompt, "3: [625, 500, 750, 667]") ||
		!strings.Contains(prompt, gridRule) || strings.Contains(prompt, "drawn on the photo") {
		t.Fatal(prompt)
	}
	if len(inv.Items) != 2 || inv.Items[0].Name != "Cheerios" || inv.Items[0].Count != 2 || inv.Items[0].NumberOfServing != 2 ||
		fmt.Sprint(inv.Items[0].Members) != "[1 2]" || inv.Items[1].Name != "Honey Maid" || inv.Items[1].NumberOfServing != 1 {
		t.Fatalf("%+v", inv.Items)
	}
	if p := purposes(inv); strings.Join(p, ",") != "sam:food,muse:single" {
		t.Fatal(p)
	}
}

func TestEqualNamesGroupWithoutSameAs(t *testing.T) {
	got := map[int]Item{1: {Result: analyze.Result{Name: "Bush's Baked Beans"}}, 2: {Result: analyze.Result{Name: "bush s baked beans!"}},
		3: {Result: analyze.Result{Name: "Chili"}}}
	boxes := []Instance{{Right: 10, Bottom: 10}, {Right: 10, Bottom: 10}, {Right: 50, Bottom: 50}}
	g := group(got, boxes, nil)
	if len(g) != 2 || g[0].Count != 2 || g[1].Name != "Chili" {
		t.Fatalf("%+v", g)
	}
}

// Boxes with no valid item get ONE retry call that shows only those boxes,
// renumbered; what still fails is rejected.
func TestInvalidItemsGetOneRetry(t *testing.T) {
	bad := ent(2, nil, "Pasta")
	delete(bad, "meal_condition")
	var prompts []string
	var mu sync.Mutex
	f := &fakeAPI{sam: func(string) (int, string) {
		return 200, samLine(t, [4]int{0, 0, 299, 299}, [4]int{300, 0, 499, 199}, [4]int{600, 400, 699, 499})
	}, muse: func(p string, body map[string]any) map[string]any {
		mu.Lock()
		prompts = append(prompts, p)
		n := len(prompts)
		mu.Unlock()
		if n == 1 {
			return textResp("m", itemsText(ent(1, nil, "Soup"), bad)) // box 3 missing
		}
		return textResp("m", itemsText(ent(1, nil, "Pasta"), ent(2, 1.0, "Pasta")))
	}}
	e, done := engine(t, f)
	defer done()
	inv, err := e.Run(context.Background(), testPhoto(t), "image/jpeg", ModeAuto)
	if err != nil {
		t.Fatal(err)
	}
	if p := purposes(inv); strings.Join(p, ",") != "sam:food,muse:single,muse:retry" {
		t.Fatal(p)
	}
	// the retry shows boxes 2 and 3 only, as 1 and 2
	if !strings.Contains(prompts[1], "1: [375, 0, 625, 333]") || !strings.Contains(prompts[1], "2: [750, 667, 875, 833]") ||
		strings.Contains(prompts[1], "3: [") {
		t.Fatal(prompts[1])
	}
	// the retry's same_as (its box 1) maps back to box 2: one Pasta group of 2
	if len(inv.Items) != 2 || inv.Items[0].Name != "Pasta" || fmt.Sprint(inv.Items[0].Members) != "[2 3]" ||
		inv.Items[0].Source != "retry" || inv.Items[1].Source != "single" || len(inv.Rejected) != 0 {
		t.Fatalf("%+v %v", inv.Items, inv.Rejected)
	}
}

func decodeDataURL(t *testing.T, u string) image.Config {
	_, b64, _ := strings.Cut(u, ",")
	raw, err := base64Decode(b64)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestNothingHalfFilled(t *testing.T) {
	f := &fakeAPI{sam: func(string) (int, string) { return 200, samLine(t, [4]int{0, 0, 99, 99}, [4]int{200, 200, 299, 299}) },
		muse: func(string, map[string]any) map[string]any { return textResp("m", `{"name": "x"}`) }}
	e, done := engine(t, f)
	defer done()
	inv, err := e.Run(context.Background(), testPhoto(t), "image/jpeg", ModeAuto)
	if !errors.Is(err, ErrNoItem) || len(inv.Items) != 0 || len(inv.Rejected) != 2 {
		t.Fatal(err, inv.Items, inv.Rejected)
	}
}

// keep drops duplicates and weak boxes but caps nothing; name caps only
// what is named.
func TestKeepCountsAllAndNameCaps(t *testing.T) {
	e := &Engine{MinConf: 0.3, MaxNamed: 8}
	e.defaults()
	in := []Instance{
		{ID: "a", Left: 0, Top: 0, Right: 100, Bottom: 100, Conf: 0.9},
		{ID: "dup", Left: 2, Top: 2, Right: 100, Bottom: 100, Conf: 0.8},    // IoU > 0.7
		{ID: "inside", Left: 10, Top: 10, Right: 30, Bottom: 30, Conf: 0.8}, // 100% inside a
		{ID: "weak", Left: 500, Top: 500, Right: 600, Bottom: 600, Conf: 0.1},
	}
	for i := 0; i < 10; i++ {
		in = append(in, Instance{ID: fmt.Sprint(i), Left: 120 + i*60, Top: 200, Right: 170 + i*60, Bottom: 200 + 50 - i, Conf: -1})
	}
	out := e.keep(in)
	if len(out) != 11 || out[0].ID != "a" {
		t.Fatalf("%d kept, first %s", len(out), out[0].ID)
	}
	for _, o := range out {
		if o.ID == "dup" || o.ID == "inside" || o.ID == "weak" {
			t.Fatal("kept", o.ID)
		}
	}
	inv := &Inventory{}
	if named := e.name(inv, out, nil); len(named) != 8 || inv.Named != 8 || inv.Unnamed != 3 {
		t.Fatal(len(named), inv.Named, inv.Unnamed)
	}
}

func TestAgentCallsSegmentPerConcept(t *testing.T) {
	var concepts []string
	var cmu sync.Mutex
	f := &fakeAPI{sam: func(c string) (int, string) {
		cmu.Lock()
		concepts = append(concepts, c)
		cmu.Unlock()
		if c == "cereal box" {
			return 200, samLine(t, [4]int{0, 0, 199, 299}, [4]int{200, 0, 399, 299})
		}
		return 200, samLine(t, [4]int{0, 0, 199, 299}, [4]int{600, 400, 699, 499}) // first is a duplicate
	}}
	turn := 0
	f.muse = func(_ string, body map[string]any) map[string]any {
		turn++
		switch turn {
		case 1:
			if body["tools"] == nil {
				t.Fatal("no tools")
			}
			return map[string]any{"id": "r1", "output": []any{
				map[string]any{"type": "function_call", "call_id": "c1", "name": "segment", "arguments": `{"concept":"cereal box"}`},
				map[string]any{"type": "function_call", "call_id": "c2", "name": "segment", "arguments": `{"concept":"can"}`},
				map[string]any{"type": "function_call", "call_id": "c3", "name": "segment", "arguments": `{}`}}}
		default:
			if body["previous_response_id"] != "r1" {
				t.Fatal("no previous_response_id")
			}
			outs := body["input"].([]any)
			ids := []string{}
			for _, o := range outs {
				m := o.(map[string]any)
				ids = append(ids, m["call_id"].(string))
				if m["call_id"] == "c2" && !strings.Contains(m["output"].(string), `"box":3`) {
					t.Fatal("the can is box 3 (the duplicate is dropped):", m["output"])
				}
				if m["call_id"] == "c3" && !strings.Contains(m["output"].(string), "error") {
					t.Fatal("bad arguments must get an error")
				}
			}
			if strings.Join(ids, ",") != "c1,c2,c3" {
				t.Fatal(ids)
			}
			return textResp("r2", itemsText(ent(1, nil, "Cheerios"), ent(2, 1.0, "Cheerios"), ent(3, nil, "Chili")))
		}
	}
	e, done := engine(t, f)
	defer done()
	inv, err := e.Run(context.Background(), testPhoto(t), "image/jpeg", ModeAgent)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(concepts) // the two segment calls run at the same time
	if strings.Join(concepts, ",") != "can,cereal box" || inv.Kept != 3 || len(inv.Items) != 2 || inv.Items[0].Count != 2 ||
		inv.Items[0].Source != "agent" {
		t.Fatalf("%v %d %+v", concepts, inv.Kept, inv.Items)
	}
}

func base64Decode(s string) ([]byte, error) { return b64std.StdEncoding.DecodeString(s) }

func TestCostFollowsMetaPrices(t *testing.T) {
	sam := &Call{Model: DefaultSAM, Status: 200}
	muse := &Call{Model: DefaultMuse, Status: 200, Usage: analyze.Usage{InputTokens: 2000, OutputTokens: 400}, CachedTokens: 1000}
	failed := &Call{Model: DefaultMuse, Status: 429, Usage: analyze.Usage{InputTokens: 2000}}
	if sam.Cost() != 0.0025 || failed.Cost() != 0 {
		t.Fatal(sam.Cost(), failed.Cost())
	}
	want := (1000*1.25 + 1000*0.15 + 400*4.25) / 1e6
	if d := muse.Cost() - want; d > 1e-12 || d < -1e-12 {
		t.Fatal(muse.Cost(), want)
	}
	inv := &Inventory{Calls: []*Call{sam, muse, failed}, Items: []Item{{Count: 2}, {Count: 1}}}
	if inv.CostUSD() != sam.Cost()+muse.Cost() || inv.Listings() != 2 || inv.Units() != 3 {
		t.Fatal(inv.CostUSD(), inv.Listings(), inv.Units())
	}
}

func TestEveryRunReportsItsCost(t *testing.T) {
	f := &fakeAPI{sam: func(string) (int, string) { return 200, samLine(t, [4]int{0, 0, 199, 299}, [4]int{300, 0, 499, 299}) },
		muse: func(string, map[string]any) map[string]any {
			return textResp("m", itemsText(ent(1, nil, "Soup"), ent(2, nil, "Beans")))
		}}
	e, done := engine(t, f)
	defer done()
	inv, err := e.Run(context.Background(), testPhoto(t), "image/jpeg", ModeAuto)
	// fake usage: 10 input + 5 output tokens per Muse call
	want := 0.0025 + (10*1.25+5*4.25)/1e6
	if err != nil || inv.Cost < want-1e-12 || inv.Cost > want+1e-12 || inv.Calls[1].CostUSD == 0 {
		t.Fatal(err, inv.Cost, want)
	}
}

func analyzePrompt() string { return analyze.Prompt }

// X32c: with EarlyWhole the whole-photo call starts with SAM. 0 boxes and
// SAM failure: its answer, no agent. 1 box: its answer with the box. Boxes
// all too small to name: its answer. 2+: the multi-item answer; the early
// call is paid and logged, not used.
func TestEarlyWholeSkipsTheAgentAndIsLoggedOnMulti(t *testing.T) {
	cases := []struct {
		name  string
		sam   func(string) (int, string)
		calls string // sorted purposes
		items string
		boxes int
	}{
		{"0 boxes", func(string) (int, string) { return 200, "<0f>\n" }, "muse:whole:early,sam:food", "Rice bowl", 0},
		{"SAM fails", func(string) (int, string) { return 500, "boom" }, "muse:whole:early,sam:food", "Rice bowl", 0},
		{"1 box", func(string) (int, string) { return 200, samLine(t, [4]int{100, 100, 399, 499}) }, "muse:whole:early,sam:food", "Rice bowl", 1},
		{"2 boxes", func(string) (int, string) {
			return 200, samLine(t, [4]int{0, 0, 199, 299}, [4]int{500, 300, 599, 399})
		}, "muse:single,muse:whole:early,sam:food", "Cheerios,Honey Maid", 0},
		// every box too small to name: the early call is the answer (no second whole call)
		{"2 tiny boxes", func(string) (int, string) {
			return 200, samLine(t, [4]int{10, 10, 19, 19}, [4]int{40, 10, 49, 19})
		}, "muse:whole:early,sam:food", "Rice bowl", 0},
	}
	for _, c := range cases {
		f := &fakeAPI{sam: c.sam, muse: func(p string, _ map[string]any) map[string]any {
			if p == analyze.Prompt {
				b, _ := json.Marshal(contract("Rice bowl"))
				return textResp("m", string(b))
			}
			return textResp("m", itemsText(ent(1, nil, "Cheerios"), ent(2, nil, "Honey Maid")))
		}}
		e, done := engine(t, f)
		e.EarlyWhole = true
		inv, err := e.Run(context.Background(), testPhoto(t), "image/jpeg", ModeAuto)
		done()
		if err != nil {
			t.Fatal(c.name, err)
		}
		p := purposes(inv)
		sort.Strings(p)
		var names []string
		for _, it := range inv.Items {
			names = append(names, it.Name)
		}
		if strings.Join(p, ",") != c.calls || strings.Join(names, ",") != c.items {
			t.Fatal(c.name, p, names)
		}
		if c.boxes == 1 && (len(inv.Items[0].Boxes) != 1 || inv.Items[0].Boxes[0] != [4]int{100, 100, 400, 500} || inv.Items[0].Source != "whole") {
			t.Fatalf("%s: %+v", c.name, inv.Items[0])
		}
		if c.items == "Rice bowl" && (inv.Items[0].NumberOfServing != 1 || (c.boxes == 0 && len(inv.Items[0].Boxes) != 0)) {
			t.Fatalf("%s: %+v", c.name, inv.Items[0])
		}
		if inv.Cost <= 0 || len(inv.Calls) != strings.Count(c.calls, ",")+1 {
			t.Fatal(c.name, "every call is paid and logged", inv.Cost, len(inv.Calls))
		}
	}
}
