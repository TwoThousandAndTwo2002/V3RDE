package inference

// X48f: the touch-up v2 prototype (touchupv2.go).

import (
	"bytes"
	"context"
	b64std "encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	analyze "github.com/TwoThousandAndTwo2002/Icarus/server"
)

// halfMask is a w x h one_bit payload whose left half is set.
func halfMask(t *testing.T, w, h int) string {
	r := make([]byte, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w/2; x++ {
			r[y*w+x] = 1
		}
	}
	p, err := encodeOneBit(r, w, h)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// A box-local mask lands on the box of the raster, scaled to its size.
func TestMaskRaster(t *testing.T) {
	in := Instance{Left: 50, Top: 0, Right: 150, Bottom: 100, W: 200, H: 100, MaskW: 50, MaskH: 50,
		Payload: halfMask(t, 50, 50), MaskOK: true}
	m, n := MaskRaster([]Instance{in, {MaskOK: false}}, 400, 200)
	if n != 1 {
		t.Fatalf("masks %d", n)
	}
	for _, c := range []struct {
		x, y int
		want byte
	}{{99, 100, 0}, {101, 100, 1}, {198, 5, 1}, {202, 100, 0}, {299, 199, 0}, {350, 100, 0}} {
		if m[c.y*400+c.x] != c.want {
			t.Errorf("(%d,%d) = %d", c.x, c.y, m[c.y*400+c.x])
		}
	}
}

func solidJPEG(t *testing.T, w, h int, c color.RGBA) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// itemJPEG: an orange blob in the middle of a cluttered pattern.
func itemJPEG(t *testing.T, w, h int, light int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dx, dy := float64(x-w/2)/float64(w/4), float64(y-h/2)/float64(h/4)
			if dx*dx+dy*dy < 1 {
				img.SetRGBA(x, y, color.RGBA{uint8(min(255, 200+light)), uint8(min(255, 100+light+x%30)), 30, 255})
			} else {
				img.SetRGBA(x, y, color.RGBA{uint8((x*7)%200 + light/2), uint8((y*13)%200 + light/2), uint8((x * y) % 200), 255})
			}
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// fullMask is a w x h one_bit payload with every pixel set.
func fullMask(t *testing.T, w, h int) string {
	r := make([]byte, w*h)
	for i := range r {
		r[i] = 1
	}
	p, err := encodeOneBit(r, w, h)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// v3Server: SAM finds the item only for "fruit" and a plate around it for
// "plate"; Muse Image (JSON) answers with a lighter, bigger copy of the photo
// (or, with newPicture, an unrelated picture); with rejectSize, a body with
// "size" gets a 400 first. inputs records the long edge of each edit input.
func v3Server(t *testing.T, newPicture, rejectSize bool, bodies *atomic.Int32, inputs *atomic.Int32) *httptest.Server {
	other := solidJPEG(t, 400, 300, color.RGBA{20, 40, 220, 255})
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k" {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if strings.HasSuffix(r.URL.Path, "/images/edits") {
			bodies.Add(1)
			imgs, _ := body["images"].([]any)
			te, _ := body["tool_enablement"].(map[string]any)
			first, _ := imgs[0].(map[string]any)
			url, _ := first["image_url"].(string)
			prompt, _ := body["prompt"].(string)
			if r.Header.Get("Content-Type") != "application/json" || len(imgs) != 1 || !strings.HasPrefix(url, "data:image/jpeg;base64,") ||
				te["enable_image_search"] != false || te["enable_web_search"] != false || te["enable_shell"] != false ||
				!strings.Contains(prompt, "exactly as they are") {
				t.Errorf("edit body: %d images, tools %v, prompt %.40s", len(imgs), te, prompt)
				w.WriteHeader(400)
				return
			}
			var edited []byte // the input, a little brighter and 1.5x bigger: a true edit
			if raw, err := b64std.StdEncoding.DecodeString(strings.TrimPrefix(url, "data:image/jpeg;base64,")); err == nil {
				if im, _, err := image.Decode(bytes.NewReader(raw)); err == nil {
					b := im.Bounds()
					if inputs != nil {
						inputs.Store(int32(max(b.Dx(), b.Dy())))
					}
					big := analyze.Resize(im, max(b.Dx(), b.Dy())*3/2)
					edited, _ = analyze.JPEG(analyze.Enhance(big, analyze.Look{Exposure: 0.1, Saturation: 1.05}), 90)
				}
			}
			if _, has := body["size"]; has && rejectSize {
				w.WriteHeader(400)
				_, _ = w.Write([]byte(`{"error":{"code":"unsupported_parameter","param":"size"}}`))
				return
			}
			out := edited
			if newPicture {
				out = other
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"b64_json": b64std.StdEncoding.EncodeToString(out)}}})
			return
		}
		if body["model"] != DefaultSAM {
			t.Errorf("not a SAM call: %v", body["model"])
		}
		line := "<0f>\n"
		prompt := firstText(body)
		if strings.HasPrefix(prompt, "<0f>1<|") { // X48l: a box or point prompt asks for one_bit masks
			if md, _ := body["metadata"].(map[string]any); md["mask_encoding"] != "one_bit" {
				t.Errorf("a %.12s prompt without mask_encoding one_bit: %v", prompt, body["metadata"])
			}
		}
		switch {
		case strings.HasPrefix(prompt, "<0f>1<|box;"): // step 1: the item and a strip of its neighbour
			if !strings.Contains(prompt, ";w=") || strings.Contains(prompt, "point") {
				t.Errorf("box prompt %q", prompt)
			}
			line = fmt.Sprintf("<0f>1<|box;x1=100;y1=75;x2=349;y2=224;w=400;h=300|><|mask;x=0;y=0;data=40,40,%s|>\n", fullMask(t, 40, 40))
		case strings.HasPrefix(prompt, "<0f>1<|point;"): // step 2: one point and not-this points: the item only
			if !strings.Contains(prompt, "|>-<|point;") || strings.Contains(prompt, "box") {
				t.Errorf("point prompt %q", prompt)
			}
			line = fmt.Sprintf("<0f>1<|box;x1=100;y1=75;x2=249;y2=224;w=400;h=300|><|mask;x=0;y=0;data=40,40,%s|>\n", fullMask(t, 40, 40))
		}
		switch prompt {
		case "fruit":
			line = fmt.Sprintf("<0f>0<|box;x1=100;y1=75;x2=299;y2=224;w=400;h=300;c=0.9|><|mask;x=0;y=0;data=40,40,%s;c=0.9|>\n", halfMask(t, 40, 40))
		case "plate":
			line = fmt.Sprintf("<0f>0<|box;x1=60;y1=40;x2=339;y2=259;w=400;h=300;c=0.9|><|mask;x=0;y=0;data=40,40,%s;c=0.9|>\n", fullMask(t, 40, 40))
		}
		_ = json.NewEncoder(w).Encode(textResp("sam", line))
	}))
}

// The X48k variants: Muse gets the source made 1024 px in code and passes the
// check; every variant is cut from that edit; words keeps the X48j mask
// (fruit + plate); box asks SAM with the item's box; box_refine adds one point
// and a not-this point on the other object and gets the item without the strip.
func TestCompareX48k(t *testing.T) {
	photo := itemJPEG(t, 400, 300, 0)
	var edits, input atomic.Int32
	srv := v3Server(t, false, false, &edits, &input)
	defer srv.Close()
	v := &V2{API: &Client{URL: srv.URL + "/responses", Key: "k"}, SAM: DefaultSAM, ImageURL: srv.URL, ImageModel: "muse-image-1.0"}
	src, _, _ := image.Decode(bytes.NewReader(photo))
	target, others := image.Rect(100, 75, 250, 225), []image.Rectangle{image.Rect(260, 60, 400, 240)}
	vs, err := v.Compare(context.Background(), photo, src, target, others, "test", SubjectConcepts)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	by := map[string]Variant{}
	for _, x := range vs {
		names = append(names, x.Name)
		by[x.Name] = x
	}
	if strings.Join(names, ",") != "original,words,box,box_refine,muse_bigger" || edits.Load() != 1 || input.Load() != MuseInputEdge {
		t.Fatalf("variants %v, edits %d, Muse input %d px", names, edits.Load(), input.Load())
	}
	m := by["muse_bigger"]
	if m.SSIM == nil || !*m.Passed {
		t.Fatalf("muse: ssim %v error %q", m.SSIM, m.Error)
	}
	for _, n := range []string{"words", "box", "box_refine"} {
		if x := by[n]; x.Error != "" || len(x.JPEG) == 0 || x.SSIM == nil {
			t.Fatalf("%s: error %q, %d bytes", n, x.Error, len(x.JPEG))
		}
	}
	if !strings.HasPrefix(by["box"].Concept, "box + plate") || !strings.HasPrefix(by["box_refine"].Concept, "box + 1 point, 1 not-this point(s) + plate") {
		t.Errorf("concepts: box %q, refine %q", by["box"].Concept, by["box_refine"].Concept)
	}
	// costs: the edit + 1 box call + 4 container calls; refine: the edit + the box call + 1 point call + 4 container calls
	if b, r := by["box"].CostUSD, by["box_refine"].CostUSD; math.Abs(b-(ImageCallPrice+5*PriceSAMPerImage)) > 1e-9 || math.Abs(r-(ImageCallPrice+6*PriceSAMPerImage)) > 1e-9 {
		t.Errorf("costs: box %.4f refine %.4f", b, r)
	}
}

// No other object: box_refine is the box mask. No box (a whole-photo item):
// the box variants say so. A refused Muse edit: the code look on the photo.
func TestCompareX48kEdges(t *testing.T) {
	photo := itemJPEG(t, 400, 300, 0)
	var edits atomic.Int32
	srv := v3Server(t, true, false, &edits, nil)
	defer srv.Close()
	v := &V2{API: &Client{URL: srv.URL + "/responses", Key: "k"}, SAM: DefaultSAM, ImageURL: srv.URL, ImageModel: "muse-image-1.0"}
	src, _, _ := image.Decode(bytes.NewReader(photo))
	vs, err := v.Compare(context.Background(), photo, src, image.Rect(100, 75, 250, 225), nil, "test", SubjectConcepts)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]Variant{}
	for _, x := range vs {
		by[x.Name] = x
	}
	if by["muse_bigger"].Passed == nil || *by["muse_bigger"].Passed || !strings.Contains(by["box"].Error, "refused") {
		t.Fatalf("refused edit: %v %q", by["muse_bigger"].Passed, by["box"].Error)
	}
	if !strings.HasPrefix(by["box_refine"].Concept, "no other object in the scan: the box mask") || len(by["box_refine"].JPEG) == 0 ||
		by["box_refine"].CostUSD != by["box"].CostUSD {
		t.Errorf("no others: %q, cost %.4f vs box %.4f", by["box_refine"].Concept, by["box_refine"].CostUSD, by["box"].CostUSD)
	}
	// an other object whose centre is inside the item's box (its plate) is not a not-this point
	vs, _ = v.Compare(context.Background(), photo, src, image.Rect(100, 75, 250, 225), []image.Rectangle{image.Rect(60, 40, 300, 260)}, "test", SubjectConcepts)
	for _, x := range vs {
		if x.Name == "box_refine" && !strings.HasPrefix(x.Concept, "no other object in the scan") {
			t.Errorf("the plate became a not-this point: %q", x.Concept)
		}
	}
	vs, _ = v.Compare(context.Background(), photo, nil, image.Rectangle{}, nil, "", SubjectConcepts)
	for _, x := range vs {
		if (x.Name == "box" || x.Name == "box_refine") && !strings.Contains(x.Error, "no item box from the scan") {
			t.Errorf("%s with no box: %q", x.Name, x.Error)
		}
	}
}

// ItemSource cuts the box again from the upload, in the upload's pixels,
// with the margin, and gives the item's box in the cut.
func TestItemSource(t *testing.T) {
	orig := itemJPEG(t, 800, 600, 0)
	m := ItemPhotoMeta{Kind: "crop", Box: []int{150, 100, 250, 200}, FrameW: 400, FrameH: 300}
	src, box, os2, err := ItemSource(orig, m, 0.35, [4]int{260, 100, 360, 200}, [4]int{0, 0, 10, 10})
	if err != nil {
		t.Fatal(err)
	}
	// the other box (x 260-360 in the frame) is 520-720 in the upload; the cut starts at 230: 290-340 in the cut (cut edge); the far box is outside
	if len(os2) != 1 || os2[0].Min.X < 285 || os2[0].Min.X > 295 {
		t.Fatalf("others %v", os2)
	}
	if b := src.Bounds(); b.Dx() < 335 || b.Dx() > 345 || b.Dy() < 335 || b.Dy() > 345 {
		t.Fatalf("size %v (want about 340 x 340: 200 px of box + 2 x 70 px margin)", b)
	}
	if box.Dx() < 198 || box.Dx() > 202 || box.Min.X < 68 || box.Min.X > 72 {
		t.Fatalf("item box %v (want about (70,70)-(270,270))", box)
	}
	if _, _, _, err := ItemSource(orig, ItemPhotoMeta{Kind: "full"}, 0.35); err == nil {
		t.Fatal("a full photo was cut")
	}
}

// With a target, SubjectMask keeps the instance that matches the item's box
// and leaves out a neighbour of the same concept (the next cereal box, the
// tomatoes next to the bananas); with no target, both touch the middle.
func TestSubjectMaskTarget(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		line := "<0f>\n"
		if firstText(body) == "food" { // the item (left) and a neighbour (right), both near the middle
			line = fmt.Sprintf("<0f>0<|box;x1=40;y1=40;x2=219;y2=259;w=400;h=300;c=0.9|><|mask;x=0;y=0;data=40,40,%s;c=0.9|>,"+
				"1<|box;x1=220;y1=40;x2=379;y2=259;w=400;h=300;c=0.9|><|mask;x=0;y=0;data=40,40,%s;c=0.9|>\n", fullMask(t, 40, 40), fullMask(t, 40, 40))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(textResp("sam", line))
	}))
	defer srv.Close()
	v := &V2{API: &Client{URL: srv.URL, Key: "k"}, SAM: DefaultSAM}
	img, _, _ := image.Decode(bytes.NewReader(itemJPEG(t, 400, 300, 0)))
	m, label, n, _, err := v.SubjectMask(context.Background(), img, []string{"food"}, nil, image.Rect(50, 50, 210, 250))
	if err != nil || !strings.HasPrefix(label, "food (SAM kept/found: food 1/2)") || n != 1 || m[150*400+100] != 1 || m[150*400+300] != 0 {
		t.Fatalf("target: err %v label %q masks %d item %d neighbour %d", err, label, n, m[150*400+100], m[150*400+300])
	}
	if _, _, n, _, _ := v.SubjectMask(context.Background(), img, []string{"food"}, nil, image.Rectangle{}); n != 2 {
		t.Fatalf("no target: %d masks, want 2", n)
	}
	if _, _, _, _, err := v.SubjectMask(context.Background(), img, []string{"food"}, nil, image.Rect(0, 0, 30, 30)); err == nil {
		t.Fatal("a target that no instance matches gave a mask")
	}
}

// ItemName reads the scan's name of the answer item from the events.
func TestItemName(t *testing.T) {
	dir := t.TempDir()
	lines := `{"request_id":"r1","boxes":[{"n":1,"outcome":"item","item":1,"name":"Ripe plantains"},{"n":2,"outcome":"item","item":2,"name":"Roma tomatoes"}]}
{"request_id":"r2","boxes":[{"n":1,"outcome":"not_food"}]}
`
	if err := os.WriteFile(filepath.Join(dir, "events-20261030.jsonl"), []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ItemName(dir, "r1", 2); got != "Roma tomatoes" {
		t.Fatalf("got %q", got)
	}
	if name, others := ItemScan(dir, "r1", 2); name != "Roma tomatoes" || len(others) != 1 {
		t.Fatalf("scan: %q %v", name, others)
	}
	// an earlier event of the same request without the item is skipped
	extra := `{"request_id":"r3","boxes":[{"n":1,"outcome":"not_food"}]}
{"request_id":"r3","boxes":[{"n":1,"outcome":"item","item":1,"name":"Pears","box":[1,2,3,4]},{"n":2,"outcome":"not_food","box":[5,6,7,8]}]}
`
	if err := os.WriteFile(filepath.Join(dir, "events-20261031.jsonl"), []byte(extra), 0o644); err != nil {
		t.Fatal(err)
	}
	if name, others := ItemScan(dir, "r3", 1); name != "Pears" || len(others) != 1 || others[0] != [4]int{5, 6, 7, 8} {
		t.Fatalf("retry event: %q %v", name, others)
	}
	if ItemName(dir, "r2", 1) != "" || ItemName(dir, "r9", 1) != "" || ItemName(dir, "", 1) != "" {
		t.Fatal("a name for no item")
	}
}

// X48j, one rule for every kind of item: a pile that SAM gives as many small
// masks (one per tomato) is the item when the pieces inside the box fill it;
// a mask that SAM made over two piles is cut to the item's box (+5%).
func TestSubjectMaskPileAndCut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		line := "<0f>\n"
		switch firstText(body) {
		case "roma tomatoes": // 12 pieces inside the box (x 160-400), none at the left
			var recs []string
			for k := 0; k < 12; k++ {
				x, y := 165+(k%4)*58, 10+(k/4)*95
				recs = append(recs, fmt.Sprintf("%d<|box;x1=%d;y1=%d;x2=%d;y2=%d;w=400;h=300;c=0.9|><|mask;x=0;y=0;data=20,20,%s;c=0.9|>",
					k, x, y, x+54, y+90, fullMask(t, 20, 20)))
			}
			line = "<0f>" + strings.Join(recs, ",") + "\n"
		case "food": // one mask over everything (the plantains at the left and the tomatoes)
			line = fmt.Sprintf("<0f>0<|box;x1=0;y1=0;x2=399;y2=299;w=400;h=300;c=0.9|><|mask;x=0;y=0;data=40,40,%s;c=0.9|>\n", fullMask(t, 40, 40))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(textResp("sam", line))
	}))
	defer srv.Close()
	v := &V2{API: &Client{URL: srv.URL, Key: "k"}, SAM: DefaultSAM}
	img, _, _ := image.Decode(bytes.NewReader(itemJPEG(t, 400, 300, 0)))
	target := image.Rect(160, 0, 400, 300)
	m, label, n, _, err := v.SubjectMask(context.Background(), img, []string{"roma tomatoes", "food"}, nil, target)
	if err != nil || !strings.HasPrefix(label, "roma tomatoes") || n != 12 || m[150*400+60] != 0 || m[60*400+200] != 1 {
		t.Fatalf("pile: err %v label %q masks %d left %d piece %d", err, label, n, m[150*400+60], m[60*400+200])
	}
	m, label, _, _, err = v.SubjectMask(context.Background(), img, []string{"food"}, nil, target)
	if err != nil || !strings.HasPrefix(label, "food") || m[150*400+60] != 0 || m[150*400+150] != 1 || m[150*400+300] != 1 {
		t.Fatalf("cut: err %v label %q far left %d edge (in the 5%%) %d inside %d", err, label, m[150*400+60], m[150*400+150], m[150*400+300])
	}
}
