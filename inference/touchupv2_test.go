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
		switch firstText(body) {
		case "fruit":
			line = fmt.Sprintf("<0f>0<|box;x1=100;y1=75;x2=299;y2=224;w=400;h=300;c=0.9|><|mask;x=0;y=0;data=40,40,%s;c=0.9|>\n", halfMask(t, 40, 40))
		case "plate":
			line = fmt.Sprintf("<0f>0<|box;x1=60;y1=40;x2=339;y2=259;w=400;h=300;c=0.9|><|mask;x=0;y=0;data=40,40,%s;c=0.9|>\n", fullMask(t, 40, 40))
		}
		_ = json.NewEncoder(w).Encode(textResp("sam", line))
	}))
}

// The X48h variants: Muse gets the source made 1024 px in code; its edit
// passes the check; the combo cuts the item AND its plate out of the edit;
// code_blur does the same on the source.
func TestCompareX48h(t *testing.T) {
	photo := itemJPEG(t, 400, 300, 0)
	var edits, input atomic.Int32
	srv := v3Server(t, false, false, &edits, &input)
	defer srv.Close()
	v := &V2{API: &Client{URL: srv.URL + "/responses", Key: "k"}, SAM: DefaultSAM, ImageURL: srv.URL, ImageModel: "muse-image-1.0"}
	vs, err := v.Compare(context.Background(), photo, nil, "", SubjectConcepts)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	by := map[string]Variant{}
	for _, x := range vs {
		names = append(names, x.Name)
		by[x.Name] = x
	}
	if strings.Join(names, ",") != "original,sharp_blur,code_blur,muse_bigger" || edits.Load() != 1 || input.Load() != MuseInputEdge {
		t.Fatalf("variants %v, edits %d, Muse input %d px", names, edits.Load(), input.Load())
	}
	s, c, m := by["sharp_blur"], by["code_blur"], by["muse_bigger"]
	if m.SSIM == nil || !*m.Passed || *m.SSIM < SSIMMin {
		t.Fatalf("muse: ssim %v passed %v error %q", m.SSIM, m.Passed, m.Error)
	}
	for _, x := range []Variant{s, c} {
		if x.Error != "" || x.Concept != "fruit + plate" || x.Masks != 2 || len(x.JPEG) == 0 {
			t.Fatalf("%s: error %q concept %q masks %d", x.Name, x.Error, x.Concept, x.Masks)
		}
	}
	// 8 SAM calls (4 concepts + 4 containers) for each of the two masks, and the edit
	want := ImageCallPrice + 8*PriceSAMPerImage
	if math.Abs(s.CostUSD-want) > 1e-9 || math.Abs(c.CostUSD-8*PriceSAMPerImage) > 1e-9 {
		t.Errorf("costs: combo %.4f (want %.4f), code %.4f", s.CostUSD, want, c.CostUSD)
	}
	// the plate is kept: the combo is cut to the plate's box, wider than the fruit's half box
	if s.W*3 < s.H*3 || s.W < 900 {
		t.Errorf("combo %dx%d: the plate was cut away", s.W, s.H)
	}
}

// A new picture from Muse: refused; the combo is the code result. No mask:
// the look only, with the reason. A 400 on "size" is tried once more.
func TestCompareRefusedAndNoMask(t *testing.T) {
	photo := itemJPEG(t, 400, 300, 0)
	var edits atomic.Int32
	srv := v3Server(t, true, true, &edits, nil)
	defer srv.Close()
	v := &V2{API: &Client{URL: srv.URL + "/responses", Key: "k"}, SAM: DefaultSAM, ImageURL: srv.URL, ImageModel: "muse-image-1.0"}
	vs, err := v.Compare(context.Background(), photo, nil, "", []string{"food", "product"})
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]Variant{}
	for _, x := range vs {
		by[x.Name] = x
	}
	m, s, c := by["muse_bigger"], by["sharp_blur"], by["code_blur"]
	if m.Passed == nil || *m.Passed || !strings.Contains(m.Error, "first try: HTTP 400") || edits.Load() != 2 {
		t.Fatalf("muse: passed %v error %q, edits %d", m.Passed, m.Error, edits.Load())
	}
	if !strings.Contains(s.Error, "refused") || string(s.JPEG) != string(c.JPEG) {
		t.Errorf("combo did not fall back to the code result: %q", s.Error)
	}
	if !strings.Contains(c.Error, "no mask of the item") {
		t.Errorf("code_blur: %q", c.Error)
	}
}

// WiderSource cuts the box again from the upload, in the upload's pixels,
// with the wider margin.
func TestWiderSource(t *testing.T) {
	orig := itemJPEG(t, 800, 600, 0)
	m := ItemPhotoMeta{Kind: "crop", Box: []int{150, 100, 250, 200}, FrameW: 400, FrameH: 300}
	src, err := WiderSource(orig, m, 0.35)
	if err != nil {
		t.Fatal(err)
	}
	if b := src.Bounds(); b.Dx() < 335 || b.Dx() > 345 || b.Dy() < 335 || b.Dy() > 345 {
		t.Fatalf("size %v (want about 340 x 340: 200 px of box + 2 x 70 px margin)", b)
	}
	if _, err := WiderSource(orig, ItemPhotoMeta{Kind: "full"}, 0.35); err == nil {
		t.Fatal("a full photo was cut")
	}
}
