// Package inference is inference_engine: one photo in, the food items in it
// out, each in analyze's strict eight-field contract (imported from the
// analyze package, so the two cannot drift). It is analyze's successor for
// the app (T9, 2026-10-26): a photo of ONE item gives exactly analyze's
// answer; a photo of several gives one validated item per product, with
// identical products grouped and number_of_serving = the count (1 per box).
//
// HOW (mode auto v2, T9 2026-10-26):
//  1. SAM 3.1 segments the concept "food" on the original photo: boxes +
//     box-local masks. Duplicates are dropped; nothing is capped (SAM counts).
//  2. SAM fails or finds 0: the agent (Muse picks product concepts, SAM is
//     its tool). An agent that ends with 0 or 1 box gives the whole call.
//  3. Exactly 1: ONE whole-photo call with analyze's prompt and the original
//     bytes -- byte-for-byte analyze.
//  4. 2+: ONE Muse call on the photo as Muse reads it (Prepare: the 4,096
//     image-token limit) with the boxes on Muse's 0–1000 grid, the largest
//     MaxNamed (24) of them; with Marks, the numbers are also drawn on the
//     photo. Every item must pass analyze.Validate. Boxes that fail get ONE
//     retry call with only those boxes; then they are rejected.
//  5. Group identical products (the model's same_as, then equal names).
//
// Nothing half-filled is returned: an item that fails the contract twice is
// rejected and reported, and a photo with no valid item is an error.
package inference

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	analyze "github.com/TwoThousandAndTwo2002/Icarus/server"
)

// Mode selects the strategy. Auto is production; the others exist so the
// probe can measure each strategy alone.
type Mode string

const (
	ModeAuto   Mode = "auto"
	ModeWhole  Mode = "whole"  // analyze-equivalent: one call on the whole photo
	ModeSingle Mode = "single" // SAM + one call for all boxes, no retry, no fallback
	ModeAgent  Mode = "agent"  // Muse orchestrates, SAM is its segment() tool
)

// Engine is configuration only; Run is safe for concurrent use.
type Engine struct {
	API      *Client
	Muse     string  // muse-spark-1.3
	SAM      string  // sam-3.1
	Concept  string  // "food"
	MaxNamed int     // boxes named per photo (24); counting has no cap
	MinConf  float64 // instances below this confidence are dropped (0 = keep all)
	Marks    bool    // draw the box numbers on the photo (Set-of-Mark)
	NoCoords bool    // with Marks: no coordinates in the text, the drawn numbers only
	// PackageConcept is SAM's second concept, run in parallel with Concept
	// ("package"; "" = off). A food box inside a package box is part of
	// that package: the package is the item, named from its label.
	PackageConcept string
	// TrayConcept is SAM's third parallel concept ("food tray"; "" = off).
	// Food inside a tray joins it; a tray is ONE listing whose servings the
	// model estimates from the tray size (T9, 2026-10-26).
	TrayConcept string
	// Size levels on the short side in the pixels Muse reads (T9,
	// 2026-10-26; v3 hand check: >=96 px 97.4% correct, 64-96 px 81%,
	// <64 px 46-71%): below NameMinPx a box is counted, not named; below
	// ConfidentPx it is named with Check = true.
	NameMinPx, ConfidentPx int
	// Split sends the named boxes in this many Muse calls AT THE SAME TIME
	// (0 or 1 = one call). Shorter answers, so less waiting.
	Split int
	// SplitMin: split only when at least this many boxes are named (12;
	// v4: split 3 cut the wait 59% on 24-box photos, cost +35%).
	SplitMin int
	// TwoPass: pass 1 names every box (NameEffort), pass 2 gives the
	// contract details once per product group (DetailEffort).
	TwoPass      bool
	NameEffort   string
	DetailEffort string
	SAMCache     *SAMCache
	// Sent, if set, gets every multi-item image as sent (probe: check the marks).
	Sent func(purpose string, jpeg []byte)
}

// Item is one product: the strict contract plus where it is and how many.
type Item struct {
	analyze.Result
	Count   int      `json:"count"`
	Boxes   [][4]int `json:"boxes"`           // [left, top, right, bottom], half-open, SAM pixels
	Norm    []Norm   `json:"norm_boxes"`      // the same boxes on the 0–1000 grid (display: /10 = CSS %)
	Seen    []Seen   `json:"seen"`            // each box's size in the image Muse read
	Members []int    `json:"members"`         // box numbers (1-based) in this group
	Source  string   `json:"source"`          // whole | single | retry | agent
	Check   bool     `json:"check,omitempty"` // every box is under ConfidentPx: the app marks it "check this"
	Tray    bool     `json:"tray,omitempty"`  // a tray of cooked food: one listing, servings from its size
	Pan     *TrayPan `json:"pan,omitempty"`   // the tray's pan size and how its servings were found
	sameAs  int
}

// Rejection is a box that never produced a valid item.
type Rejection struct {
	Box    int    `json:"box"`
	Reason string `json:"reason"`
}

// Points scores the numbered-box test: each named item's [x, y] point must
// fall in the box whose number it gave (2% tolerance).
type Points struct {
	Hit     int `json:"hit"`
	Miss    int `json:"miss"`
	Missing int `json:"missing"`
}

// Inventory is the full answer and its trace.
type Inventory struct {
	Mode      Mode `json:"mode"`
	Width     int  `json:"width"`
	Height    int  `json:"height"`
	PrepW     int  `json:"prep_width"` // the image Muse read (multi-item calls)
	PrepH     int  `json:"prep_height"`
	Instances int  `json:"sam_instances"`
	Kept      int  `json:"kept"`    // after duplicates: every one is counted
	Named     int  `json:"named"`   // boxes sent to Muse to name
	Unnamed   int  `json:"unnamed"` // counted but not named (over MaxNamed)
	Small     int  `json:"small"`   // counted but too small to name (under NameMinPx)
	Trays     int  `json:"trays"`   // tray boxes kept
	// CloserPhoto: some boxes were too small to name; the app asks for a
	// closer photo of them.
	CloserPhoto bool     `json:"closer_photo"`
	Concepts    []string `json:"concepts"`
	Packages    int      `json:"packages"`  // package boxes kept
	Contained   int      `json:"contained"` // food boxes merged into their package
	Items       []Item   `json:"items"`
	// NotListed: boxes the model called a product that is not food. They are
	// not deleted: the app shows them as "not listed" with "Add anyway".
	NotListed []Item      `json:"not_listed,omitempty"`
	Rejected  []Rejection `json:"rejected,omitempty"`
	Points    Points      `json:"points"`
	Calls     []*Call     `json:"calls"`
	MS        int64       `json:"ms"`
	Cost      float64     `json:"cost_usd"` // filled when Run returns
	mu        sync.Mutex
}

func (inv *Inventory) add(c *Call) {
	inv.mu.Lock()
	inv.Calls = append(inv.Calls, c)
	inv.mu.Unlock()
}

// ErrNoItem: not one item passed the contract (analyze answers 502 here).
var ErrNoItem = errors.New("inference: no item passed the contract")

func (e *Engine) defaults() {
	if e.Muse == "" {
		e.Muse = DefaultMuse
	}
	if e.SAM == "" {
		e.SAM = DefaultSAM
	}
	if e.Concept == "" {
		e.Concept = "food"
	}
	if e.MaxNamed <= 0 {
		e.MaxNamed = 24
	}
	if e.NameMinPx <= 0 {
		e.NameMinPx = 64
	}
	if e.ConfidentPx <= 0 {
		e.ConfidentPx = 96
	}
	if e.SplitMin <= 0 {
		e.SplitMin = 12
	}
}

// photo is one request's image in both forms.
type photo struct {
	once sync.Once
	raw  []byte // the original bytes (SAM, and the analyze-identical call)
	mime string
	src  image.Image // decoded original (nil if not decodable)
	prep *Prepared   // as Muse reads it
}

func (p *photo) prepared() *Prepared {
	p.once.Do(func() {
		if p.src != nil {
			p.prep = Prepare(p.src)
		}
	})
	return p.prep
}

// Run answers one photo.
func (e *Engine) Run(ctx context.Context, img []byte, mime string, mode Mode) (*Inventory, error) {
	e.defaults()
	t0 := time.Now()
	inv := &Inventory{Mode: mode}
	defer func() { inv.MS, inv.Cost = time.Since(t0).Milliseconds(), inv.CostUSD() }()
	ph := &photo{raw: img, mime: mime}
	if src, _, err := image.Decode(bytes.NewReader(img)); err == nil {
		ph.src = src
		inv.Width, inv.Height = src.Bounds().Dx(), src.Bounds().Dy()
	}
	if mode == ModeWhole || ph.src == nil {
		return inv, e.whole(ctx, inv, ph, nil)
	}
	if mode == ModeAgent {
		return inv, e.agent(ctx, inv, ph)
	}
	boxes, err := e.find(ctx, inv, ph)
	inv.Kept = len(boxes)
	if mode == ModeAuto {
		switch {
		case err != nil || len(boxes) == 0:
			return inv, e.agent(ctx, inv, ph)
		case len(boxes) == 1:
			return inv, e.whole(ctx, inv, ph, boxes)
		}
	}
	if err != nil {
		return inv, err
	}
	named := e.name(inv, boxes, ph.prepared())
	if len(named) == 0 { // every box is too small to name: analyze's answer + "closer photo"
		return inv, e.whole(ctx, inv, ph, nil)
	}
	if e.TwoPass {
		return inv, e.twoPass(ctx, inv, ph, named, mode)
	}
	got, failed := e.parallelSingle(ctx, inv, ph, named)
	if mode == ModeAuto && len(failed) > 0 {
		var more map[int]Item
		more, failed = e.single(ctx, inv, ph, named, failed, "retry")
		for n, it := range more {
			got[n] = it
		}
	}
	for _, n := range failed {
		inv.Rejected = append(inv.Rejected, Rejection{Box: n, Reason: "no valid item in the call(s)"})
	}
	inv.Items = group(got, named, ph.prepared())
	if len(inv.Items) == 0 {
		return inv, ErrNoItem
	}
	return inv, nil
}

// parallelSingle names the boxes in Split calls at the same time. Boxes are
// chunked in reading order (rows, then left to right), so identical
// products side by side stay in one call and keep their same_as links;
// across calls, equal names still group.
func (e *Engine) parallelSingle(ctx context.Context, inv *Inventory, ph *photo, named []Instance) (map[int]Item, []int) {
	k := min(max(e.Split, 1), len(named))
	if len(named) < e.SplitMin {
		k = 1
	}
	if k == 1 {
		return e.single(ctx, inv, ph, named, seq(len(named)), "single")
	}
	chunks := chunkReading(named, k)
	got := map[int]Item{}
	var failed []int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for ci, which := range chunks {
		wg.Add(1)
		go func(ci int, which []int) {
			defer wg.Done()
			g, f := e.single(ctx, inv, ph, named, which, fmt.Sprintf("single-%d", ci+1))
			mu.Lock()
			defer mu.Unlock()
			for n, it := range g {
				got[n] = it
			}
			failed = append(failed, f...)
		}(ci, which)
	}
	wg.Wait()
	sort.Ints(failed)
	return got, failed
}

// chunkReading splits box numbers into k contiguous chunks of near-equal
// size in reading order: rows by the box centre (a row is one median box
// height), then left to right.
func chunkReading(named []Instance, k int) [][]int {
	idx := seq(len(named))
	hs := make([]int, len(named))
	for i, b := range named {
		hs[i] = b.Bottom - b.Top
	}
	sort.Ints(hs)
	row := max(hs[len(hs)/2], 1)
	cy := func(b Instance) int { return (b.Top + b.Bottom) / 2 / row }
	sort.SliceStable(idx, func(i, j int) bool {
		a, b := named[idx[i]-1], named[idx[j]-1]
		if cy(a) != cy(b) {
			return cy(a) < cy(b)
		}
		return a.Left < b.Left
	})
	var out [][]int
	for c := 0; c < k; c++ {
		lo, hi := c*len(idx)/k, (c+1)*len(idx)/k
		if hi > lo {
			out = append(out, append([]int(nil), idx[lo:hi]...))
		}
	}
	return out
}

// find runs SAM on every concept (food, package, tray) AND prepares the
// photo for Muse, all at the same time, then merges: every container box
// (package or tray; duplicates by IoU > 0.7 dropped; containers never
// merge into each other), plus every food box that is not 80% inside a
// container or the same object as one. Largest first.
func (e *Engine) find(ctx context.Context, inv *Inventory, ph *photo) ([]Instance, error) {
	concepts := []string{e.Concept}
	for _, c := range []string{e.PackageConcept, e.TrayConcept} {
		if c != "" {
			concepts = append(concepts, c)
		}
	}
	found := make([][]Instance, len(concepts))
	errs := make([]error, len(concepts))
	var wg sync.WaitGroup
	for i, c := range concepts {
		wg.Add(1)
		go func(i int, c string) { defer wg.Done(); found[i], errs[i] = e.segment(ctx, inv, ph, c) }(i, c)
	}
	wg.Add(1)
	go func() { defer wg.Done(); ph.prepared() }() // the resize does not need the boxes
	wg.Wait()
	if len(concepts) == 1 {
		return e.keep(found[0]), errs[0]
	}
	allFailed := true
	for _, err := range errs {
		allFailed = allFailed && err != nil
	}
	if allFailed {
		return nil, errs[0]
	}
	// containers: duplicates only by IoU (a pan inside a board is NOT a
	// duplicate), then holders are dropped
	var cand []Instance
	for i := 1; i < len(concepts); i++ {
		cand = append(cand, found[i]...)
	}
	cont := dropHolders(e.keepIoU(cand))
	for _, c := range cont {
		if c.Concept == e.TrayConcept {
			inv.Trays++
		} else {
			inv.Packages++
		}
	}
	out := append([]Instance(nil), cont...)
	for _, f := range e.keep(found[0]) {
		inside := false
		for _, p := range cont {
			if u, s := iou(f, p); u > 0.7 || (s > 0.8 && f.Area() <= p.Area()) {
				inside = true
				break
			}
		}
		if inside {
			inv.Contained++
		} else {
			out = append(out, f)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Area() > out[j].Area() })
	return out, nil
}

func seq(n int) []int {
	s := make([]int, n)
	for i := range s {
		s[i] = i + 1
	}
	return s
}

// name takes the largest MaxNamed boxes to send to Muse; the rest are
// counted (Unnamed) but not named.
func (e *Engine) name(inv *Inventory, boxes []Instance, p *Prepared) []Instance {
	var ok []Instance
	for _, b := range boxes {
		if p == nil || SeenSize(b, p.W, p.H).ShortPx >= float64(e.NameMinPx) {
			ok = append(ok, b)
		} else {
			inv.Small++
		}
	}
	inv.CloserPhoto = inv.Small > 0
	n := min(len(ok), e.MaxNamed)
	inv.Named, inv.Unnamed = n, len(ok)-n
	return ok[:n]
}

// ---- SAM ---------------------------------------------------------------------

// SAMCache lets the probe compare variants on the SAME boxes: one SAM call
// per photo and concept. A cached answer is logged with the cost it would
// have had (production pays it every time) and Cached = true.
type SAMCache struct {
	mu sync.Mutex
	m  map[[32]byte]samHit
}

type samHit struct {
	inst []Instance
	call Call
}

func (e *Engine) segment(ctx context.Context, inv *Inventory, ph *photo, concept string) ([]Instance, error) {
	inv.mu.Lock()
	inv.Concepts = append(inv.Concepts, concept)
	inv.mu.Unlock()
	key := sha256.Sum256(append(append([]byte(concept), 0), ph.raw...))
	if e.SAMCache != nil {
		e.SAMCache.mu.Lock()
		h, ok := e.SAMCache.m[key]
		e.SAMCache.mu.Unlock()
		if ok {
			c := h.call
			c.Cached = true
			inv.add(&c)
			inv.mu.Lock()
			inv.Instances += len(h.inst)
			inv.mu.Unlock()
			return append([]Instance(nil), h.inst...), nil
		}
	}
	c, err := e.API.Do(ctx, "sam:"+concept, map[string]any{
		"model":    e.SAM,
		"input":    UserMessage(TextPart(concept), ImagePart(ph.raw, ph.mime)),
		"metadata": map[string]any{"include_confidence": "true", "mask_encoding": "one_bit"},
	})
	inv.add(c)
	if err != nil {
		return nil, err
	}
	out, _ := ParseSAM(c.Text())
	for i := range out {
		out[i].Concept = concept
	}
	inv.mu.Lock()
	inv.Instances += len(out)
	inv.mu.Unlock()
	if e.SAMCache != nil {
		e.SAMCache.mu.Lock()
		if e.SAMCache.m == nil {
			e.SAMCache.m = map[[32]byte]samHit{}
		}
		e.SAMCache.m[key] = samHit{inst: append([]Instance(nil), out...), call: *c}
		e.SAMCache.mu.Unlock()
	}
	return out, nil
}

func iou(a, b Instance) (iou, inSmaller float64) {
	l, t := max(a.Left, b.Left), max(a.Top, b.Top)
	r, bt := min(a.Right, b.Right), min(a.Bottom, b.Bottom)
	if r <= l || bt <= t {
		return 0, 0
	}
	in := float64((r - l) * (bt - t))
	return in / float64(a.Area()+b.Area()-int(in)), in / float64(min(a.Area(), b.Area()))
}

// keepIoU drops low-confidence instances and duplicates of the same object
// (IoU > 0.7) only, largest first. For containers: a pan inside a board, or
// a package inside a donation box, is a different object.
func (e *Engine) keepIoU(all []Instance) []Instance {
	var cand []Instance
	for _, in := range all {
		if e.MinConf > 0 && in.Conf >= 0 && in.Conf < e.MinConf {
			continue
		}
		cand = append(cand, in)
	}
	sort.SliceStable(cand, func(i, j int) bool { return cand[i].Area() > cand[j].Area() })
	var out []Instance
	for _, in := range cand {
		dup := false
		for _, k := range out {
			if u, _ := iou(in, k); u > 0.7 {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, in)
		}
	}
	return out
}

// dropHolders removes a container that holds 2 or more other containers
// (each 80% inside it): a serving board under pans, a crate or a donation
// box of packages. The containers inside are the items (T9: one listing
// per tray). A container with exactly 1 inside that fills half or more of
// it is the same object seen twice (a box and its front panel): the inner
// one is dropped. A small single item on a tray stays its own item.
func dropHolders(cont []Instance) []Instance {
	drop := make([]bool, len(cont))
	for i, c := range cont {
		var in []int
		for j, d := range cont {
			if i == j || d.Area() >= c.Area() {
				continue
			}
			if _, s := iou(d, c); s > 0.8 {
				in = append(in, j)
			}
		}
		switch {
		case len(in) >= 2:
			drop[i] = true
		case len(in) == 1 && 2*cont[in[0]].Area() >= c.Area():
			drop[in[0]] = true
		}
	}
	var out []Instance
	for i, c := range cont {
		if !drop[i] {
			out = append(out, c)
		}
	}
	return out
}

// keep drops low-confidence instances, then duplicates (IoU > 0.7, or a box
// 90% inside a kept one: the same item seen twice), largest first. No cap:
// every kept box is counted.
func (e *Engine) keep(all []Instance) []Instance {
	var cand []Instance
	for _, in := range all {
		if e.MinConf > 0 && in.Conf >= 0 && in.Conf < e.MinConf {
			continue
		}
		cand = append(cand, in)
	}
	sort.SliceStable(cand, func(i, j int) bool { return cand[i].Area() > cand[j].Area() })
	var out []Instance
	for _, in := range cand {
		dup := false
		for _, k := range out {
			if u, s := iou(in, k); u > 0.7 || s > 0.9 {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, in)
		}
	}
	return out
}

// ---- Muse --------------------------------------------------------------------

func (e *Engine) contractCall(ctx context.Context, inv *Inventory, purpose string, img []byte, mime string) (analyze.Result, error) {
	c, err := e.API.Do(ctx, purpose, map[string]any{
		"model": e.Muse, "input": UserMessage(TextPart(analyze.Prompt), ImagePart(img, mime))})
	inv.add(c)
	if err != nil {
		return analyze.Result{}, err
	}
	m, err := analyze.ExtractJSON(c.Text())
	if err != nil {
		return analyze.Result{}, fmt.Errorf("contract: %w", err)
	}
	return analyze.Validate(m)
}

// whole is analyze, unchanged: the same prompt on the original bytes.
func (e *Engine) whole(ctx context.Context, inv *Inventory, ph *photo, boxes []Instance) error {
	r, err := e.contractCall(ctx, inv, "muse:whole", ph.raw, ph.mime)
	if err != nil {
		inv.Rejected = append(inv.Rejected, Rejection{Box: 0, Reason: err.Error()})
		return ErrNoItem
	}
	r.NumberOfServing = analyze.DefaultServings
	it := Item{Result: r, Count: 1, Source: "whole", Boxes: [][4]int{}, Norm: []Norm{}, Seen: []Seen{}, Members: []int{}}
	if len(boxes) == 1 {
		it.Boxes, it.Norm, it.Members = [][4]int{boxes[0].Box()}, []Norm{NormBox(boxes[0])}, []int{1}
		if p := ph.prepared(); p != nil {
			it.Seen = []Seen{SeenSize(boxes[0], p.W, p.H)}
		}
	}
	inv.Items = []Item{it}
	return nil
}

// gridRule states Muse's coordinate grid in its own documentation's words
// (dev.meta.ai/docs/image-understanding: "State the exact fields and axis
// order in your prompt and ask for JSON").
const gridRule = `Coordinates use a 0-1000 grid: (0, 0) is the top-left corner and (1000, 1000) is the bottom-right; x increases left to right, y increases top to bottom.`

// itemFields is the per-box answer: the box number, a point to check the
// number against the box, the grouping link, and analyze.Prompt's fields.
const itemFields = `{"items": [{"box": <box number>, "point": [x, y] (the centre of the item you identified, on the same grid),
"same_as": <number of an earlier box holding the identical product, or null>,
"food": true, or false if the item is a product that is not food (for example paper towels or soap); fill every field either way,
"tray": true if the item is a tray or pan of cooked food, else false. For a tray also give
"pan": the pan size compared with a standard 12 x 20 inch hotel pan: "full", "two_thirds", "half", "third", "quarter", "sixth", "ninth", or "other" (not a hotel pan),
"depth": "shallow" (about 2.5 inches), "medium" (about 4 inches) or "deep" (about 6 inches),
"dish": "main" or "side",
and number_of_serving = your estimate of the servings in the whole tray. Use a serving spoon, tongs, a plate or another non-food item in the photo as a size reference,
name, description, is_non_veg (0 vegetarian or 1), number_of_serving,
serving_type (individual or bulk), meal_condition
(ready_to_eat, cooking_required, sealed), weight (kg), meal_cost}]}
One entry per box. description must be ONE sentence, 10 words maximum.
Use conservative estimates if unsure.`

// MultiPrompt asks for every numbered box at once in the analyze contract.
func MultiPrompt(boxes []Instance, marks, coords bool) string {
	return boxList(boxes, marks, coords) + "For EVERY box, identify the item in it. Return ONLY valid JSON:\n" + itemFields
}

// boxList introduces the numbered boxes: as coordinates in the text, as
// tags drawn on the photo, or both. Without coordinates, the drawn numbers
// are the only link between a number and a place in the photo.
func boxList(boxes []Instance, marks, coords bool) string {
	var sb strings.Builder
	switch {
	case marks && !coords:
		fmt.Fprintf(&sb, "Each item to identify in this photo is outlined and has a number from 1 to %d in a coloured tag at its top-left corner. "+
			"Points use a 0-1000 grid: (0, 0) is the top-left corner and (1000, 1000) is the bottom-right; x increases left to right, y increases top to bottom.\n", len(boxes))
		return sb.String()
	case marks:
		sb.WriteString("Each numbered box below marks one item in this photo. " + gridRule + "\n")
		sb.WriteString("Each box is also drawn on the photo, with its number in a coloured tag at its top-left corner.\n")
	default:
		sb.WriteString("Each numbered box below marks one item in this photo. " + gridRule + "\n")
	}
	for i, b := range boxes {
		n := NormBox(b)
		fmt.Fprintf(&sb, "%d: [%d, %d, %d, %d]\n", i+1, n[0], n[1], n[2], n[3])
	}
	return sb.String()
}

type entry struct {
	box     int
	sameAs  int
	point   *[2]float64
	notFood bool
	tray    bool
	pan     TrayPan
	res     analyze.Result
}

// parseEntries reads {"items":[...]}; each entry must name a known box and
// pass analyze.Validate. Returns valid entries by box, and reasons by box.
func parseEntries(text string, n int) (map[int]entry, map[int]string) {
	ok, bad := map[int]entry{}, map[int]string{}
	m, err := analyze.ExtractJSON(text)
	if err != nil {
		return ok, bad
	}
	items, _ := m["items"].([]any)
	for _, raw := range items {
		obj, _ := raw.(map[string]any)
		bf, isNum := obj["box"].(float64)
		b := int(bf)
		if !isNum || float64(b) != bf || b < 1 || b > n {
			continue
		}
		r, err := analyze.Validate(obj)
		if err != nil {
			bad[b] = err.Error()
			continue
		}
		s := 0
		if sf, ok := obj["same_as"].(float64); ok && sf >= 1 && sf < float64(b) && sf == float64(int(sf)) {
			s = int(sf)
		}
		var pt *[2]float64
		if p, ok := obj["point"].([]any); ok && len(p) == 2 {
			x, okx := p[0].(float64)
			y, oky := p[1].(float64)
			if okx && oky {
				pt = &[2]float64{x, y}
			}
		}
		nf := false
		if f, isBool := obj["food"].(bool); isBool && !f {
			nf = true
		}
		tr, _ := obj["tray"].(bool)
		var tp TrayPan
		if tr {
			tp.Size, _ = obj["pan"].(string)
			tp.Depth, _ = obj["depth"].(string)
			tp.Dish, _ = obj["dish"].(string)
			if n, ok := PanServings(tp); ok {
				tp.ModelServings, r.NumberOfServing = r.NumberOfServing, float64(n)
				tp.FromTable = true
			}
		}
		if _, dup := ok[b]; !dup {
			ok[b] = entry{box: b, sameAs: s, point: pt, notFood: nf, tray: tr, pan: tp, res: r}
		}
	}
	return ok, bad
}

// score adds one named box's point to the numbered-box test.
func (inv *Inventory) score(b Instance, pt *[2]float64) {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	if pt == nil {
		inv.Points.Missing++
		return
	}
	n := NormBox(b)
	const tol = 20 // 2% of the grid
	if pt[0] >= float64(n[0]-tol) && pt[0] <= float64(n[2]+tol) && pt[1] >= float64(n[1]-tol) && pt[1] <= float64(n[3]+tol) {
		inv.Points.Hit++
	} else {
		inv.Points.Miss++
	}
}

// single is one Muse call for the boxes in which (numbers into named). The
// call sees only those boxes, renumbered 1..k. Returns items by the
// original number and the numbers still without a valid item.
func (e *Engine) single(ctx context.Context, inv *Inventory, ph *photo, named []Instance, which []int, purpose string) (map[int]Item, []int) {
	got := map[int]Item{}
	sub := make([]Instance, len(which))
	for i, n := range which {
		sub[i] = named[n-1]
	}
	p := ph.prepared()
	inv.mu.Lock()
	inv.PrepW, inv.PrepH = p.W, p.H
	inv.mu.Unlock()
	pic := p.Img
	if e.Marks {
		pic = DrawMarks(p, sub)
	}
	jb, err := JPEG(pic)
	if err != nil {
		return got, which
	}
	if e.Sent != nil {
		e.Sent(purpose, jb)
	}
	c, err := e.API.Do(ctx, "muse:"+purpose, map[string]any{
		"model": e.Muse, "input": UserMessage(TextPart(MultiPrompt(sub, e.Marks, !e.NoCoords)), ImagePart(jb, "image/jpeg"))})
	inv.add(c)
	if err != nil {
		return got, which
	}
	ents, _ := parseEntries(c.Text(), len(sub))
	var failed []int
	for i, n := range which {
		en, ok := ents[i+1]
		if !ok {
			failed = append(failed, n)
			continue
		}
		inv.score(sub[i], en.point)
		if en.notFood {
			inv.notListed(sub[i], n, en.res, purpose)
			continue
		}
		sa := 0
		if en.sameAs > 0 {
			sa = which[en.sameAs-1]
		}
		got[n] = Item{Result: en.res, Source: purpose, Members: []int{n}, sameAs: sa, Tray: en.tray, Pan: en.panRef(),
			Check: SeenSize(sub[i], p.W, p.H).ShortPx < float64(e.ConfidentPx)}
	}
	return got, failed
}

// notListed keeps a not-food box for the app ("not listed", "Add anyway").
func (inv *Inventory) notListed(b Instance, n int, r analyze.Result, source string) {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	inv.NotListed = append(inv.NotListed, Item{Result: r, Count: 1, Boxes: [][4]int{b.Box()}, Norm: []Norm{NormBox(b)},
		Members: []int{n}, Source: source})
}

// CropJPEG cuts one box (plus margin, a fraction of the box size) out of
// the photo as a JPEG: the listing photo of one item.
func CropJPEG(src image.Image, b Instance, margin float64) ([]byte, error) {
	bd := src.Bounds()
	sx, sy := 1.0, 1.0 // SAM's frame size vs the decoded photo
	if b.W > 0 && b.H > 0 {
		sx, sy = float64(bd.Dx())/float64(b.W), float64(bd.Dy())/float64(b.H)
	}
	mx, my := margin*float64(b.Right-b.Left), margin*float64(b.Bottom-b.Top)
	r := image.Rect(int((float64(b.Left)-mx)*sx), int((float64(b.Top)-my)*sy),
		int((float64(b.Right)+mx)*sx+0.5), int((float64(b.Bottom)+my)*sy+0.5)).Add(bd.Min).Intersect(bd)
	if r.Empty() {
		return nil, errors.New("crop: empty box")
	}
	sub, ok := src.(interface {
		SubImage(image.Rectangle) image.Image
	})
	if !ok {
		return nil, errors.New("crop: image type cannot crop")
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, sub.SubImage(r), &jpeg.Options{Quality: 90}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ---- grouping ----------------------------------------------------------------

var nonWord = regexp.MustCompile(`[^a-z0-9]+`)

// normName makes names from different calls comparable: lower case, "&"
// = "and", no filler words, singular (a final "s" on words of 4+ letters).
// "Go Pure Fruit and Oat Bars" = "Go Pure Fruit Oat Bar".
func normName(s string) string {
	s = strings.ReplaceAll(strings.ToLower(s), "&", " and ")
	var out []string
	for _, w := range strings.Fields(nonWord.ReplaceAllString(s, " ")) {
		switch w {
		case "and", "the", "with", "of", "a":
			continue
		}
		if len(w) >= 4 && strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") {
			w = strings.TrimSuffix(w, "s")
		}
		out = append(out, w)
	}
	return strings.Join(out, " ")
}

// group merges boxes of the identical product: the model's same_as links,
// then equal normalised names. The first box of a group gives its fields;
// number_of_serving = count (1 per box). Groups are ordered by count, then
// by total box area.
func group(got map[int]Item, boxes []Instance, p *Prepared) []Item {
	parent := map[int]int{}
	var find func(int) int
	find = func(x int) int {
		if parent[x] != x {
			parent[x] = find(parent[x])
		}
		return parent[x]
	}
	keys := make([]int, 0, len(got))
	for n := range got {
		keys = append(keys, n)
		parent[n] = n
	}
	sort.Ints(keys)
	union := func(a, b int) {
		ra, rb := find(a), find(b)
		if ra == rb {
			return
		}
		if ra < rb {
			parent[rb] = ra
		} else {
			parent[ra] = rb
		}
	}
	byName := map[string]int{}
	for _, n := range keys {
		it := got[n]
		if it.Tray { // one listing per tray (T9)
			continue
		}
		if s := it.sameAs; s > 0 {
			if o, ok := got[s]; ok && !o.Tray {
				union(n, s)
			}
		}
		if k := normName(it.Name); k != "" {
			if first, ok := byName[k]; ok {
				union(n, first)
			} else {
				byName[k] = n
			}
		}
	}
	groups := map[int]*Item{}
	var roots []int
	area := map[int]int{}
	for _, n := range keys {
		r := find(n)
		g, ok := groups[r]
		if !ok {
			first := got[r]
			g = &Item{Result: first.Result, Source: first.Source, Tray: first.Tray, Pan: first.Pan, Check: true}
			groups[r] = g
			roots = append(roots, r)
		}
		b := boxes[n-1]
		g.Members = append(g.Members, n)
		g.Boxes = append(g.Boxes, b.Box())
		g.Norm = append(g.Norm, NormBox(b))
		if p != nil {
			g.Seen = append(g.Seen, SeenSize(b, p.W, p.H))
		}
		g.Count++
		g.Check = g.Check && got[n].Check
		area[r] += b.Area()
	}
	out := make([]Item, 0, len(roots))
	for _, r := range roots {
		g := groups[r]
		if !g.Tray {
			g.NumberOfServing = float64(g.Count)
		} else if g.NumberOfServing < 1 {
			g.NumberOfServing = 1
		}
		out = append(out, *g)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return area[out[i].Members[0]] > area[out[j].Members[0]]
	})
	return out
}

// TopLevel is the analyze-shaped answer: the largest group (ties: box area).
func (inv *Inventory) TopLevel() analyze.Result { return inv.Items[0].Result }

// Dedup is keep() for tools: duplicates dropped, largest first, no cap.
func Dedup(all []Instance) []Instance { return (&Engine{}).keep(all) }
