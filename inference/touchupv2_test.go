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

// v2Server: Muse Spark answers a look, SAM a "food" mask (the left half of
// the photo), Muse Image a blue photo (or 500 when edits fail).
func v2Server(t *testing.T, editFails bool, edits *atomic.Int32) *httptest.Server {
	blue := solidJPEG(t, 400, 300, color.RGBA{20, 40, 220, 255})
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k" {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/images/edits") {
			edits.Add(1)
			if err := r.ParseMultipartForm(8 << 20); err != nil || r.FormValue("model") != "muse-image-1.0" || r.FormValue("prompt") == "" {
				w.WriteHeader(400)
				return
			}
			if editFails {
				w.WriteHeader(500)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"b64_json": b64std.StdEncoding.EncodeToString(blue)}}})
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["model"] == DefaultSAM {
			if firstText(body) != "food" {
				t.Errorf("SAM concept %q", firstText(body))
			}
			line := fmt.Sprintf("<0f>0<|box;x1=0;y1=0;x2=399;y2=299;w=400;h=300;c=0.9|><|mask;x=0;y=0;data=40,40,%s;c=0.9|>\n", halfMask(t, 40, 40))
			_ = json.NewEncoder(w).Encode(textResp("sam", line))
			return
		}
		if !strings.HasPrefix(firstText(body), "This is a photo of food") {
			t.Errorf("look prompt: %.40s", firstText(body))
		}
		_ = json.NewEncoder(w).Encode(textResp("m", `{"exposure": 0.9, "saturation": 1.2, "crop": [0, 0, 1000, 1000]}`))
	}))
}

// Every variant comes back in order; smart takes the clamped look; smart_bg
// keeps the food pixels of smart (the mask) and takes the background of the
// edit; the costs add up.
func TestCompareVariants(t *testing.T) {
	var edits atomic.Int32
	srv := v2Server(t, false, &edits)
	defer srv.Close()
	v := &V2{API: &Client{URL: srv.URL + "/responses", Key: "k"}, Muse: DefaultMuse, SAM: DefaultSAM,
		ImageURL: srv.URL, ImageModel: "muse-image-1.0"}
	photo := solidJPEG(t, 400, 300, color.RGBA{200, 120, 40, 255})
	vs, err := v.Compare(context.Background(), photo, true, true)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	by := map[string]Variant{}
	for _, x := range vs {
		names = append(names, x.Name)
		by[x.Name] = x
		if x.Error != "" || len(x.JPEG) == 0 {
			t.Errorf("%s: %q", x.Name, x.Error)
		}
	}
	if strings.Join(names, ",") != "original,enhance,smart,smart_bg,current" || edits.Load() != 2 {
		t.Fatalf("variants %v, edits %d", names, edits.Load())
	}
	if by["enhance"].W != 800 || by["enhance"].H != 600 || by["smart"].Look.Exposure != 0.5 || by["smart"].Look.Saturation != 1.2 {
		t.Fatalf("enhance %dx%d, smart look %+v", by["enhance"].W, by["enhance"].H, by["smart"].Look)
	}
	g := by["smart_bg"]
	img, err := jpeg.Decode(bytes.NewReader(g.JPEG))
	if err != nil || g.Masks != 1 || g.W != 800 {
		t.Fatalf("smart_bg: %v %d %d", err, g.Masks, g.W)
	}
	if r, _, b, _ := img.At(100, 300).RGBA(); r>>8 < 150 || b>>8 > 120 {
		t.Errorf("the food side is not the real photo: r %d b %d", r>>8, b>>8)
	}
	if r, _, b, _ := img.At(700, 300).RGBA(); b>>8 < 150 || r>>8 > 100 {
		t.Errorf("the background is not the edit: r %d b %d", r>>8, b>>8)
	}
	if g.CostUSD < ImageCallPrice+PriceSAMPerImage || by["current"].CostUSD != ImageCallPrice || by["enhance"].CostUSD != 0 {
		t.Errorf("costs: bg %.4f current %.4f enhance %.4f", g.CostUSD, by["current"].CostUSD, by["enhance"].CostUSD)
	}
	// thumbs
	tb, err := Thumb(g.JPEG, 200, 80)
	if c, _, e2 := image.DecodeConfig(bytes.NewReader(tb)); err != nil || e2 != nil || c.Width != 200 {
		t.Fatalf("thumb: %v %v %d", err, e2, c.Width)
	}
}

// A failed Muse Image call: smart_bg and current carry the error, the code
// variants still come back.
func TestCompareEditFails(t *testing.T) {
	var edits atomic.Int32
	srv := v2Server(t, true, &edits)
	defer srv.Close()
	v := &V2{API: &Client{URL: srv.URL + "/responses", Key: "k"}, Muse: DefaultMuse, SAM: DefaultSAM,
		ImageURL: srv.URL, ImageModel: "muse-image-1.0"}
	vs, err := v.Compare(context.Background(), solidJPEG(t, 300, 300, color.RGBA{90, 160, 60, 255}), true, true)
	if err != nil || len(vs) != 5 {
		t.Fatal(err, len(vs))
	}
	for _, x := range vs {
		failed := x.Name == "smart_bg" || x.Name == "current"
		if failed != (x.Error != "") || (!failed && len(x.JPEG) == 0) {
			t.Errorf("%s: %q %d bytes", x.Name, x.Error, len(x.JPEG))
		}
	}
	if _, err := v.Compare(context.Background(), []byte("not an image"), false, false); err == nil {
		t.Fatal("a bad photo gave variants")
	}
}
