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
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
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

// v3Server: SAM finds the item only for "fruit"; Muse Image (JSON) answers the
// fix prompt with a lighter copy of the photo and the bigger prompt with an
// unrelated picture; with rejectSize, a body with "size" gets a 400 first.
func v3Server(t *testing.T, photo []byte, rejectSize bool, bodies *atomic.Int32) *httptest.Server {
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
			if _, has := body["size"]; has && rejectSize {
				w.WriteHeader(400)
				_, _ = w.Write([]byte(`{"error":{"code":"unsupported_parameter","param":"size"}}`))
				return
			}
			out := other
			if strings.HasPrefix(prompt, "Correct the exposure") {
				out = itemJPEG(t, 400, 300, 25)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"b64_json": b64std.StdEncoding.EncodeToString(out)}}})
			return
		}
		if body["model"] != DefaultSAM {
			t.Errorf("not a SAM call: %v", body["model"])
		}
		line := "<0f>\n"
		if firstText(body) == "fruit" {
			line = fmt.Sprintf("<0f>0<|box;x1=100;y1=75;x2=299;y2=224;w=400;h=300;c=0.9|><|mask;x=0;y=0;data=40,40,%s;c=0.9|>\n", halfMask(t, 40, 40))
		}
		_ = json.NewEncoder(w).Encode(textResp("sam", line))
	}))
}

// The X48g variants: the studio modes cut the item out (the "fruit" mask, the
// first concept that found it) and keep its pixels; the fix edit passes the
// structure check; the bigger edit (a new picture) is refused.
func TestCompareX48g(t *testing.T) {
	photo := itemJPEG(t, 400, 300, 0)
	var edits atomic.Int32
	srv := v3Server(t, photo, false, &edits)
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
	if strings.Join(names, ",") != "original,studio_plain,studio_blur,muse_fixed,muse_bigger" || edits.Load() != 2 {
		t.Fatalf("variants %v, edits %d", names, edits.Load())
	}
	for _, n := range []string{"studio_plain", "studio_blur"} {
		x := by[n]
		want := 0.0 // one mask for both modes: its cost is counted once, on plain
		if n == "studio_plain" {
			want = 4 * PriceSAMPerImage
		}
		if x.Error != "" || x.Concept != "fruit" || x.Masks != 1 || len(x.JPEG) == 0 || x.CostUSD != want {
			t.Fatalf("%s: %+v", n, x)
		}
		// cut to the item (the mask box plus 12%), then made 2x bigger (the most)
		if x.Look.Crop[0] < 200 || x.Look.Crop[2] > 560 || x.W < 240 || x.W > 256 {
			t.Errorf("%s: crop %v, %dx%d", n, x.Look.Crop, x.W, x.H)
		}
	}
	f, b := by["muse_fixed"], by["muse_bigger"]
	if f.SSIM == nil || !*f.Passed || *f.SSIM < SSIMMin || f.CostUSD != ImageCallPrice {
		t.Errorf("fixed: %+v ssim %v", f, f.SSIM)
	}
	if b.SSIM == nil || *b.Passed || *b.SSIM >= SSIMMin {
		t.Errorf("bigger (a new picture) passed: ssim %v", b.SSIM)
	}
	if by["original"].Note != "the item photo" {
		t.Errorf("note %q", by["original"].Note)
	}
}

// No concept finds the item: the studio modes give the look only, with the
// reason; a 400 on "size" is tried once more without the optional fields.
func TestCompareNoMaskAndRetry(t *testing.T) {
	photo := itemJPEG(t, 400, 300, 0)
	var edits atomic.Int32
	srv := v3Server(t, photo, true, &edits)
	defer srv.Close()
	v := &V2{API: &Client{URL: srv.URL + "/responses", Key: "k"}, SAM: DefaultSAM, ImageURL: srv.URL, ImageModel: "muse-image-1.0"}
	vs, err := v.Compare(context.Background(), photo, nil, "", []string{"food", "product"})
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range vs {
		switch x.Name {
		case "studio_plain", "studio_blur":
			if !strings.Contains(x.Error, "no mask of the item") || len(x.JPEG) == 0 {
				t.Errorf("%s: %q", x.Name, x.Error)
			}
		case "muse_fixed":
			if len(x.JPEG) == 0 || !strings.Contains(x.Error, "first try: HTTP 400") {
				t.Errorf("retry: %q", x.Error)
			}
		}
	}
	if edits.Load() != 4 {
		t.Errorf("edit calls %d, want 4 (2 x 400 + 2 retries)", edits.Load())
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
