package inference

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"
	"sync"
	"testing"
)

// The measured limit: a photo over 4,096 image tokens is resized to just
// under it, aspect kept; a photo under it is not touched.
func TestPrepareFitsTheMeasuredTokenLimit(t *testing.T) {
	// the ie-tokens measurements (2026-10-26): image tokens = tokens + 2 fixed
	for _, m := range []struct{ w, h, tok int }{{640, 480, 414}, {1280, 960, 1610}, {1792, 1344, 3072}} {
		if got := ImageTokens(m.w, m.h); got != m.tok {
			t.Fatalf("%dx%d: %d tokens, measured %d", m.w, m.h, got, m.tok)
		}
	}
	big := image.NewRGBA(image.Rect(0, 0, 3264, 2448))
	p := Prepare(big)
	tok := ImageTokens(p.W, p.H)
	if !p.Resized || tok > MaxImageTokens || tok < MaxImageTokens*97/100 {
		t.Fatalf("%s", p)
	}
	if a, b := float64(p.W)/float64(p.H), 3264.0/2448.0; math.Abs(a-b)/b > 0.01 {
		t.Fatal("aspect changed", a, b)
	}
	small := image.NewRGBA(image.Rect(0, 0, 1300, 956))
	if q := Prepare(small); q.Resized || q.Img != image.Image(small) || q.Scale != 1 {
		t.Fatalf("%s", q)
	}
}

func TestNormBoxAndSeenSize(t *testing.T) {
	b := Instance{Left: 100, Top: 50, Right: 300, Bottom: 250, W: 1000, H: 500}
	if n := NormBox(b); n != (Norm{100, 100, 300, 500}) {
		t.Fatal(n)
	}
	// the prepared photo is half size: 200 px box -> 100 px seen
	s := SeenSize(b, 500, 250)
	if s.ShortPx != 100 || s.AreaPct != 8 || s.Tokens != math.Round(100*100/784.0*10)/10 {
		t.Fatalf("%+v", s)
	}
}

// Set-of-Mark: each box gets an outline and its number in a tag.
func TestMarksAreDrawnInThePreparedPhoto(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 800, 600))
	for i := range src.Pix {
		src.Pix[i] = 128
	}
	p := Prepare(src)
	boxes := []Instance{{Left: 100, Top: 100, Right: 400, Bottom: 400, W: 800, H: 600},
		{Left: 500, Top: 200, Right: 700, Bottom: 500, W: 800, H: 600}}
	m := DrawMarks(p, boxes).(*image.RGBA)
	if c := m.RGBAAt(250, 399); c != markColors[0] { // bottom edge of box 1
		t.Fatal("box 1 outline", c)
	}
	if c := m.RGBAAt(699, 350); c != markColors[1] { // right edge of box 2
		t.Fatal("box 2 outline", c)
	}
	white := 0
	for y := 100; y < 117; y++ {
		for x := 100; x < 111; x++ {
			if m.RGBAAt(x, y) == (color.RGBA{255, 255, 255, 255}) {
				white++
			}
		}
	}
	if white == 0 {
		t.Fatal("no number drawn in box 1's tag")
	}
	if src.Pix[0] != 128 || src.RGBAAt(250, 399).R != 128 {
		t.Fatal("the original photo was changed")
	}
	if pr := MultiPrompt(boxes, true, true); !strings.Contains(pr, "drawn on the photo") {
		t.Fatal(pr)
	}
}

// The numbered-box test: a point in its own box is a hit, outside is a
// miss, no point is missing.
func TestPointsScoreTheNumbers(t *testing.T) {
	f := &fakeAPI{sam: func(string) (int, string) {
		return 200, samLine(t, [4]int{0, 0, 199, 299}, [4]int{400, 0, 599, 299}, [4]int{0, 400, 199, 499})
	}, muse: func(string, map[string]any) map[string]any {
		a, b, c := ent(1, nil, "A"), ent(2, nil, "B"), ent(3, nil, "C")
		a["point"] = []any{120.0, 240.0} // box 1 is [0,0,250,500]
		b["point"] = []any{120.0, 240.0} // box 2 is [500,0,750,500]: a miss
		return textResp("m", itemsText(a, b, c))
	}}
	e, done := engine(t, f)
	defer done()
	e.Marks = true
	var sent []string
	e.Sent = func(purpose string, jb []byte) { sent = append(sent, fmt.Sprintf("%s %d", purpose, len(jb))) }
	inv, err := e.Run(context.Background(), testPhoto(t), "image/jpeg", ModeAuto)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Points != (Points{Hit: 1, Miss: 1, Missing: 1}) || len(sent) != 1 || !strings.HasPrefix(sent[0], "single ") {
		t.Fatalf("%+v %v", inv.Points, sent)
	}
	if inv.PrepW != 800 || inv.PrepH != 600 || len(inv.Items[0].Norm) != 1 || len(inv.Items[0].Seen) != 1 {
		t.Fatalf("%+v", inv)
	}
}

// One SAM call per photo and concept across variants; the cached answer
// keeps its production cost but is not spent again.
func TestSAMCacheSharesOneCall(t *testing.T) {
	var mu sync.Mutex
	samCalls := 0
	f := &fakeAPI{sam: func(string) (int, string) {
		mu.Lock()
		samCalls++
		mu.Unlock()
		return 200, samLine(t, [4]int{0, 0, 199, 299}, [4]int{300, 0, 499, 299})
	}, muse: func(string, map[string]any) map[string]any {
		return textResp("m", itemsText(ent(1, nil, "Soup"), ent(2, nil, "Beans")))
	}}
	e, done := engine(t, f)
	defer done()
	e.SAMCache = &SAMCache{}
	a, err1 := e.Run(context.Background(), testPhoto(t), "image/jpeg", ModeAuto)
	b, err2 := e.Run(context.Background(), testPhoto(t), "image/jpeg", ModeAuto)
	if err1 != nil || err2 != nil || samCalls != 1 || a.Calls[0].Cached || !b.Calls[0].Cached {
		t.Fatal(err1, err2, samCalls)
	}
	if a.Cost != b.Cost || math.Abs(b.SpentUSD()-(b.Cost-PriceSAMPerImage)) > 1e-12 || a.SpentUSD() != a.Cost {
		t.Fatal(a.Cost, b.Cost, b.SpentUSD())
	}
}

// More boxes than MaxNamed: only the largest MaxNamed are named, every box
// is counted.
func TestNameCapCountsEveryBox(t *testing.T) {
	var boxes [][4]int
	for i := 0; i < 30; i++ {
		x := (i % 10) * 80
		y := (i / 10) * 200
		boxes = append(boxes, [4]int{x, y, x + 69 - i%3, y + 189})
	}
	var prompt string
	f := &fakeAPI{sam: func(string) (int, string) { return 200, samLine(t, boxes...) },
		muse: func(p string, _ map[string]any) map[string]any {
			prompt = p
			var es []map[string]any
			for n := 1; n <= 24; n++ {
				es = append(es, ent(n, nil, "Cola can"))
			}
			return textResp("m", itemsText(es...))
		}}
	e, done := engine(t, f)
	defer done()
	inv, err := e.Run(context.Background(), testPhoto(t), "image/jpeg", ModeAuto)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(prompt, "\n2") == 0 || strings.Contains(prompt, "\n25: [") || !strings.Contains(prompt, "\n24: [") {
		t.Fatal(prompt)
	}
	if inv.Kept != 30 || inv.Named != 24 || inv.Unnamed != 6 || inv.Units() != 24 || inv.Counted() != 30 ||
		len(inv.Items) != 1 || inv.Items[0].NumberOfServing != 24 {
		t.Fatalf("kept %d named %d unnamed %d units %d", inv.Kept, inv.Named, inv.Unnamed, inv.Units())
	}
}

// The agent that finds one box gives the analyze-identical whole call.
func TestAgentWithOneBoxGivesWhole(t *testing.T) {
	var lastPrompt string
	f := &fakeAPI{sam: func(c string) (int, string) {
		if c == "food" {
			return 200, "<0f>\n"
		}
		return 200, samLine(t, [4]int{100, 100, 399, 499})
	}}
	turn := 0
	f.muse = func(p string, body map[string]any) map[string]any {
		turn++
		lastPrompt = p
		if turn == 1 {
			return map[string]any{"id": "r1", "output": []any{
				map[string]any{"type": "function_call", "call_id": "c1", "name": "segment", "arguments": `{"concept":"jar"}`}}}
		}
		if turn == 2 {
			return textResp("r2", itemsText(ent(1, nil, "Jar")))
		}
		b, _ := json.Marshal(contract("Marinara sauce"))
		return textResp("m", string(b))
	}
	e, done := engine(t, f)
	defer done()
	inv, err := e.Run(context.Background(), testPhoto(t), "image/jpeg", ModeAuto)
	if err != nil {
		t.Fatal(err)
	}
	if p := purposes(inv); strings.Join(p, ",") != "sam:food,muse:agent:0,sam:jar,muse:agent:1,muse:whole" {
		t.Fatal(p)
	}
	if lastPrompt != analyzePrompt() || inv.Items[0].Name != "Marinara sauce" || inv.Items[0].Count != 1 ||
		len(inv.Items[0].Boxes) != 1 {
		t.Fatalf("%+v", inv.Items)
	}
}

// Many runs of the same photo at the same time: SAM is called once.
func TestSAMCacheOneCallUnderConcurrency(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	f := &fakeAPI{sam: func(string) (int, string) {
		mu.Lock()
		calls++
		mu.Unlock()
		return 200, samLine(t, [4]int{0, 0, 199, 299}, [4]int{300, 0, 499, 299})
	}, muse: func(string, map[string]any) map[string]any {
		return textResp("m", itemsText(ent(1, nil, "Soup"), ent(2, nil, "Beans")))
	}}
	e, done := engine(t, f)
	defer done()
	e.SAMCache = &SAMCache{}
	e.defaults() // once, before the goroutines, as NewService does (X32a: the race test found the missing call)
	var wg sync.WaitGroup
	paid := 0
	var pm sync.Mutex
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			inv, err := e.Run(context.Background(), testPhoto(t), "image/jpeg", ModeAuto)
			if err != nil {
				t.Error(err)
				return
			}
			pm.Lock()
			if !inv.Calls[0].Cached {
				paid++
			}
			pm.Unlock()
		}()
	}
	wg.Wait()
	if calls != 1 || paid != 1 {
		t.Fatal("SAM calls", calls, "paid", paid)
	}
}
