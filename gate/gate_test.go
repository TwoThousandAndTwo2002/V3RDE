package gate

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"math"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---- the head -------------------------------------------------------------

func head(dim int, thr, intercept float64, coef []float64) *Head {
	if coef == nil {
		coef = make([]float64, dim)
	}
	return &Head{Encoder: "test", Precision: "float32", InputSide: 4, Dim: dim, Threshold: thr, Intercept: intercept, Coef: coef}
}

func TestParseHead(t *testing.T) {
	good := `{"encoder":"pe","precision":"fp16w","input_side":384,"embedding_dim":2,"threshold":0.4,"intercept":0.1,"coef":[1,2],
	"preprocess":{"geometry":"crop_first_centre_square","resample":"pillow_bicubic","mean":[0.5,0.5,0.5],"std":[0.5,0.5,0.5],"layout":"NHWC"}}`
	h, err := ParseHead([]byte(good))
	if err != nil || h.Dim != 2 || h.Spec().Resample != ResampleBicubic || h.Spec().Mean[0] != 0.5 {
		t.Fatalf("%+v %v", h, err)
	}
	old, err := ParseHead([]byte(`{"input_side":192,"embedding_dim":1,"threshold":0.26,"coef":[1]}`))
	if err != nil || old.Spec().Resample != ResampleBilinear || old.Spec().Std != [3]float32{1, 1, 1} || old.Spec().Mean != [3]float32{} {
		t.Fatalf("a head without preprocess is the first gate's: %+v %v", old.Spec(), err)
	}
	for body, want := range map[string]string{
		`{"input_side":4,"embedding_dim":3,"coef":[1,2]}`: "head has 2 coefficients but declares 3 dims",
		`{"input_side":0,"embedding_dim":1,"coef":[1]}`:   "head declares input_side 0, dim 1",
		`{"input_side":4,"embedding_dim":0,"coef":[]}`:    "head declares input_side 4, dim 0",
		`not json`: "invalid character",
		`{"input_side":4,"embedding_dim":1,"coef":[1],"preprocess":{"geometry":"resize_first","resample":"pillow_bicubic"}}`:                                            `preprocess geometry "resize_first" is not "crop_first_centre_square"`,
		`{"input_side":4,"embedding_dim":1,"coef":[1],"preprocess":{"geometry":"crop_first_centre_square","resample":"lanczos"}}`:                                       `preprocess resample "lanczos" is not supported`,
		`{"input_side":4,"embedding_dim":1,"coef":[1],"preprocess":{"geometry":"crop_first_centre_square","resample":"pillow_bilinear","layout":"NCHW","std":[1,1,1]}}`: `preprocess layout "NCHW" is not NHWC`,
		`{"input_side":4,"embedding_dim":1,"coef":[1],"preprocess":{"geometry":"crop_first_centre_square","resample":"pillow_bilinear","std":[1,0,1]}}`:                 "preprocess std[1] is 0",
	} {
		if _, err := ParseHead([]byte(body)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v (want %q)", body, err, want)
		}
	}
}

func TestProbabilityIsStableBothWays(t *testing.T) {
	for _, tc := range []struct {
		intercept, want float64
	}{{0, 0.5}, {1000, 1}, {-1000, 0}, {2, 1 / (1 + math.Exp(-2))}, {-2, math.Exp(-2) / (1 + math.Exp(-2))}} {
		p, err := head(1, 0.5, tc.intercept, []float64{0}).Probability([]float32{1})
		if err != nil || math.IsNaN(p) || math.Abs(p-tc.want) > 1e-15 {
			t.Errorf("z=%v: %v %v", tc.intercept, p, err)
		}
	}
	if _, err := head(2, 0.5, 0, nil).Probability([]float32{1}); err == nil {
		t.Fatal("a wrong size must be an error")
	}
}

func TestVerdictRules(t *testing.T) {
	h := head(2, 0.5, 0, []float64{0, 0}) // p = 0.5 for every unit vector
	if label, p, _, _ := h.Verdict([]float32{1, 0}); label != NotFood || p != 0.5 {
		t.Fatalf("p == threshold is NOT_FOOD (strictly greater): %s %v", label, p)
	}
	h.Threshold = 0.4999
	if label, _, _, why := h.Verdict([]float32{0, 1}); label != Food || why != "" {
		t.Fatalf("%s %q", label, why)
	}
	for _, v := range [][]float32{{0.98, 0}, {1.02, 0}, {0, 0}} {
		label, p, norm, why := h.Verdict(v)
		if label != Unavailable || p != -1 || !strings.HasPrefix(why, "embedding not unit norm") || norm != L2(v) {
			t.Errorf("%v: %s %v %q", v, label, p, why)
		}
	}
	// the band edges (the X38g review: moved bounds were not caught)
	for _, tc := range []struct {
		n    float32
		want bool // in the band
	}{{0.99, true}, {1.01, true}, {0.9899, false}, {1.0101, false}, {1.0, true}} {
		label, _, _, _ := h.Verdict([]float32{tc.n, 0})
		if (label != Unavailable) != tc.want {
			t.Errorf("norm %v: %s", tc.n, label)
		}
	}
	if label, p, _, why := head(3, 0.5, 0, nil).Verdict([]float32{1, 0}); label != Unavailable || p != -1 || !strings.Contains(why, "head expects 3") {
		t.Fatalf("a wrong size: %s %q", label, why)
	}
}

// ---- the preprocess -------------------------------------------------------

func TestCropBox(t *testing.T) {
	for _, tc := range []struct{ w, h, x, y, s int }{{640, 480, 80, 0, 480}, {480, 640, 0, 80, 480}, {5, 4, 0, 0, 4}, {4, 7, 0, 1, 4}, {9, 9, 0, 0, 9}} {
		if r := CropBox(tc.w, tc.h); r != image.Rect(tc.x, tc.y, tc.x+tc.s, tc.y+tc.s) {
			t.Errorf("%dx%d: %v", tc.w, tc.h, r)
		}
	}
}

func sumOf(b []uint8) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func TestResizeIsPillowBilinear(t *testing.T) {
	for _, g := range bilinearGoldens {
		box := CropBox(g.w, g.h)
		out := ResizeBilinear(Crop(pattern(g.w, g.h), g.w, box), box.Dx(), box.Dy(), g.s, g.s)
		if len(out) != g.s*g.s*3 || sumOf(out) != g.sha {
			t.Errorf("%dx%d -> %d: %s", g.w, g.h, g.s, sumOf(out)[:12])
		}
	}
}

func TestResizeIsPillowBicubic(t *testing.T) {
	for _, g := range bicubicGoldens {
		box := CropBox(g.w, g.h)
		out := ResizeBicubic(Crop(pattern(g.w, g.h), g.w, box), box.Dx(), box.Dy(), g.s, g.s)
		if len(out) != g.s*g.s*3 || sumOf(out) != g.sha {
			t.Errorf("%dx%d -> %d: %s", g.w, g.h, g.s, sumOf(out)[:12])
		}
	}
}

func TestNonSquareResizeIsPillows(t *testing.T) {
	for _, g := range nonSquareGoldens {
		resize := ResizeBilinear
		if g.filter == "bicubic" {
			resize = ResizeBicubic
		}
		out := resize(pattern(g.w, g.h), g.w, g.h, g.ow, g.oh)
		if len(out) != g.ow*g.oh*3 || sumOf(out) != g.sha {
			t.Errorf("%dx%d -> %dx%d %s: %s", g.w, g.h, g.ow, g.oh, g.filter, sumOf(out)[:12])
		}
	}
}

func TestTensors(t *testing.T) {
	rgb := pattern(53, 31)
	old := Tensor(rgb, 53, 31, 16)
	same := TensorFor(rgb, 53, 31, 16, head(1, 0, 0, nil).Spec())
	if len(old) != 16*16*3 || len(same) != len(old) {
		t.Fatal(len(old))
	}
	for i := range old {
		if old[i] != same[i] || old[i] < 0 || old[i] > 1 {
			t.Fatalf("a head without preprocess must give the first gate's tensor (%d: %v %v)", i, old[i], same[i])
		}
	}
	pe := PreprocessSpec{Geometry: GeometryCropFirst, Resample: ResampleBicubic, Mean: [3]float32{0.5, 0.5, 0.5}, Std: [3]float32{0.5, 0.5, 0.5}}
	x := TensorFor(rgb, 53, 31, 16, pe)
	raw := make([]byte, 0, len(x)*4)
	for _, f := range x {
		raw = binary.LittleEndian.AppendUint32(raw, math.Float32bits(f))
	}
	if got := sumOf(raw); got != peTensorGolden {
		t.Fatalf("the PE tensor is not the Python reference's: %s", got[:12])
	}
}

func TestDecodePNGAndGIF(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	img.Set(0, 0, color.RGBA{10, 20, 30, 255})
	img.Set(2, 1, color.RGBA{200, 100, 50, 255})
	var pb bytes.Buffer
	png.Encode(&pb, img)
	rgb, w, h, format, err := Decode(pb.Bytes())
	if err != nil || format != "png" || w != 3 || h != 2 || !bytes.Equal(rgb[:3], []byte{10, 20, 30}) || !bytes.Equal(rgb[15:], []byte{200, 100, 50}) {
		t.Fatalf("png %v %s %dx%d %v", rgb, format, w, h, err)
	}
	pal := image.NewPaletted(image.Rect(0, 0, 2, 2), color.Palette{color.RGBA{1, 2, 3, 255}, color.RGBA{250, 0, 0, 255}})
	pal.SetColorIndex(1, 1, 1)
	var gb bytes.Buffer
	gif.Encode(&gb, pal, nil)
	rgb, _, _, format, err = Decode(gb.Bytes())
	if err != nil || format != "gif" || !bytes.Equal(rgb, []byte{1, 2, 3, 1, 2, 3, 1, 2, 3, 250, 0, 0}) {
		t.Fatalf("gif %v %s %v", rgb, format, err)
	}
	if _, _, _, _, err := Decode([]byte("not an image")); err == nil || !strings.HasPrefix(err.Error(), "decode: ") {
		t.Fatalf("garbage: %v", err)
	}
}

// ---- the handler ----------------------------------------------------------

type fakeModel struct {
	v       []float32
	err     error
	block   chan struct{}
	entered chan int // when set, gets the input size at each call
	mu      sync.Mutex
	calls   int
}

func (f *fakeModel) Embed(x []float32) ([]float32, error) {
	if f.entered != nil {
		f.entered <- len(x)
	}
	if f.block != nil {
		<-f.block
	}
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	return f.v, f.err
}
func (f *fakeModel) InputShape() []int  { return []int{1, 4, 4, 3} }
func (f *fakeModel) OutputShape() []int { return []int{1, 2} }

func photo(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 7, 5))
	for i := 0; i < 35; i++ {
		img.Set(i%7, i/7, color.RGBA{uint8(i * 7), 120, 30, 255})
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func post(t *testing.T, h http.Handler, ctype string, body []byte, trace string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/v1/gate", bytes.NewReader(body))
	r.Header.Set("Content-Type", ctype)
	if trace != "" {
		r.Header.Set("X-Trace-Id", trace)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var m map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &m)
	return w, m
}

func events(t *testing.T, rec *Recorder) []Event {
	t.Helper()
	rec.Close()
	files, _ := filepath.Glob(filepath.Join(rec.Dir, "events-*.jsonl"))
	var out []Event
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			var e Event
			if err := json.Unmarshal([]byte(l), &e); err != nil {
				t.Fatalf("line %q: %v", l, err)
			}
			out = append(out, e)
		}
	}
	return out
}

func gateWith(t *testing.T, m Embedder, h *Head) (http.Handler, *Recorder) {
	t.Helper()
	rec := testRecorder(t, 64)
	return New(Config{Model: m, Head: h, ModelSHA256: "msum", HeadSHA256: "hsum", Build: "b1", Recorder: rec}), rec
}

func TestFoodAnswerAndEvent(t *testing.T) {
	g, rec := gateWith(t, &fakeModel{v: []float32{0.6, 0.8}}, head(2, 0.5, 0, []float64{4, 4}))
	img := photo(t)
	w, m := post(t, g, "image/png", img, "scan-1")
	if w.Code != 200 || m["verdict"] != Food || m["food"] != true || m["trace_id"] != "scan-1" || m["threshold"] != 0.5 ||
		m["image_sha256"] != sumOf(img) || w.Header().Get("X-Request-Id") != m["request_id"] {
		t.Fatalf("%d %v", w.Code, m)
	}
	ms, _ := m["ms"].(map[string]any)
	for _, k := range []string{"queue", "decode", "prep", "invoke", "head", "total"} {
		if _, ok := ms[k]; !ok {
			t.Errorf("ms.%s missing", k)
		}
	}
	ev := events(t, rec)
	if len(ev) != 1 {
		t.Fatalf("%d events", len(ev))
	}
	e := ev[0]
	if e.Step != "gate" || e.Service != "gate" || e.Verdict != Food || e.Status != 200 || e.TraceID != "scan-1" || e.Format != "png" ||
		e.Width != 7 || e.Height != 5 || e.ImageBytes != len(img) || e.ModelSHA256 != "msum" || e.Build != "b1" || math.Abs(e.Norm-1) > 1e-6 {
		t.Fatalf("%+v", e)
	}
}

func TestNotFoodAndBodies(t *testing.T) {
	g, _ := gateWith(t, &fakeModel{v: []float32{1, 0}}, head(2, 0.9, 0, []float64{0, 0}))
	img := photo(t)
	var mp bytes.Buffer
	mw := multipart.NewWriter(&mp)
	fw, _ := mw.CreateFormFile("image", "p.png")
	fw.Write(img)
	mw.Close()
	for _, b := range []struct {
		ct   string
		body []byte
	}{
		{"image/png", img},
		{mw.FormDataContentType(), mp.Bytes()},
		{"application/json", []byte(`{"image_base64":"` + base64.StdEncoding.EncodeToString(img) + `"}`)},
		{"application/json", []byte(`{"image_base64":"  ` + base64.StdEncoding.EncodeToString(img) + `\n"}`)}, // spaces around are trimmed
	} {
		w, m := post(t, g, b.ct, b.body, "")
		if w.Code != 200 || m["verdict"] != NotFood || m["food"] != false || m["p"] != 0.5 || m["image_sha256"] != sumOf(img) {
			t.Errorf("%s: %d %v", b.ct, w.Code, m)
		}
	}
}

func TestFailsClosed(t *testing.T) {
	img := photo(t)
	rec := testRecorder(t, 8)
	g := New(Config{LoadError: "model sha256 abc is not the pin", Recorder: rec})
	w, m := post(t, g, "image/png", img, "")
	if w.Code != 200 || m["verdict"] != Unavailable || m["food"] != false || m["p"] != -1.0 || m["why"] != "gate not loaded: model sha256 abc is not the pin" {
		t.Fatalf("%d %v", w.Code, m)
	}
	hw := httptest.NewRecorder()
	g.ServeHTTP(hw, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	var hm map[string]any
	json.Unmarshal(hw.Body.Bytes(), &hm)
	if hw.Code != 503 || hm["ok"] != false || hm["error"] != "model sha256 abc is not the pin" || hm["jpeg_decoder"] != JPEGDecoder {
		t.Fatalf("healthz %d %v", hw.Code, hm)
	}
	g2, _ := gateWith(t, &fakeModel{err: errors.New("litert run: status 3")}, head(2, 0.5, 0, nil))
	if _, m := post(t, g2, "image/png", img, ""); m["verdict"] != Unavailable || m["why"] != "litert run: status 3" {
		t.Fatalf("a model error: %v", m)
	}
	g3, _ := gateWith(t, &fakeModel{v: []float32{0.5, 0.5}}, head(2, 0.5, 0, nil))
	if _, m := post(t, g3, "image/png", img, ""); m["verdict"] != Unavailable || !strings.HasPrefix(m["why"].(string), "embedding not unit norm") {
		t.Fatalf("a bad norm: %v", m)
	}
}

func TestRefusals(t *testing.T) {
	f := &fakeModel{v: []float32{1, 0}}
	g, rec := gateWith(t, f, head(2, 0.5, 0, nil))
	noField := formWithout(t)
	get := httptest.NewRequest(http.MethodGet, "/v1/gate", nil)
	gw := httptest.NewRecorder()
	g.ServeHTTP(gw, get)
	if gw.Code != 405 || !strings.Contains(gw.Body.String(), `"error":"POST only"`) {
		t.Fatalf("GET %d %s", gw.Code, gw.Body)
	}
	for _, tc := range []struct {
		ct   string
		body []byte
		code int
		msg  string
	}{
		{"text/plain", []byte("x"), 415, "send multipart/form-data, application/json or image/*"},
		{"image/png", nil, 400, "empty image"},
		{"image/png", make([]byte, 6<<20), 413, "image too large"},
		{"application/json", []byte(`{"image_base64":"%%"}`), 400, "image_base64 is not base64"},
		{"application/json", []byte(`{`), 400, "bad JSON body"},
		{"image/png", []byte("not an image"), 422, "decode: image: unknown format"},
		{noField.ct, noField.body, 400, "multipart field 'image' missing"},
	} {
		w, m := post(t, g, tc.ct, tc.body, "")
		if w.Code != tc.code || m["error"] != tc.msg || len(m["request_id"].(string)) != 32 {
			t.Errorf("%s %d bytes: %d %v", tc.ct, len(tc.body), w.Code, m)
		}
	}
	if f.calls != 0 {
		t.Fatalf("a refused photo reached the model %d times", f.calls)
	}
	if n := len(events(t, rec)); n != 8 {
		t.Fatalf("every refusal writes a line: %d", n)
	}
}

func TestBadTraceIDIsReplaced(t *testing.T) {
	g, _ := gateWith(t, &fakeModel{v: []float32{1, 0}}, head(2, 0.5, 0, nil))
	w, m := post(t, g, "image/png", photo(t), "has spaces!")
	if m["trace_id"] != m["request_id"] || w.Header().Get("X-Trace-Id") != m["request_id"] {
		t.Fatalf("%v", m)
	}
}

func TestQueueFullIsBusy(t *testing.T) {
	f := &fakeModel{v: []float32{1, 0}, block: make(chan struct{}), entered: make(chan int, 4)}
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(f.block) }) }) // a failed check does not leave goroutines blocked
	rec := testRecorder(t, 64)
	g := New(Config{Model: f, Head: head(2, 0.5, 0, nil), Recorder: rec, Queue: 1})
	img := photo(t)
	codes := make(chan int, 2)
	go func() { w, _ := post(t, g, "image/png", img, ""); codes <- w.Code }() // in the slot
	<-f.entered                                                               // it is in the model now
	go func() { w, _ := post(t, g, "image/png", img, ""); codes <- w.Code }() // takes the one queue place
	time.Sleep(100 * time.Millisecond)                                        // the second request is decoded and waiting
	w, m := post(t, g, "image/png", img, "")                                  // the queue is full
	if w.Code != 503 || m["error"] != "busy: gate queue full" {
		t.Fatalf("%d %v", w.Code, m)
	}
	once.Do(func() { close(f.block) })
	if a, b := <-codes, <-codes; a != 200 || b != 200 {
		t.Fatalf("%d %d", a, b)
	}
	hw := httptest.NewRecorder()
	g.ServeHTTP(hw, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	var hm map[string]any
	json.Unmarshal(hw.Body.Bytes(), &hm)
	if hm["busy"] != 1.0 || hm["served"] != 2.0 {
		t.Fatalf("healthz %v", hm)
	}
}

// formWithout is a multipart body with no "image" field.
func formWithout(t *testing.T) struct {
	ct   string
	body []byte
} {
	t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	mw.WriteField("photo", "x")
	mw.Close()
	return struct {
		ct   string
		body []byte
	}{mw.FormDataContentType(), b.Bytes()}
}

// ---- the shadow -----------------------------------------------------------

// The shadow uses its own head: its own size and preprocess, not the
// primary's (the X38g review: a shadow built with the primary head passed).
func TestShadowUsesItsOwnHead(t *testing.T) {
	sf := &fakeModel{v: []float32{1, 0}, entered: make(chan int, 1)}
	shead := head(2, 0.5, 0, nil)
	shead.InputSide = 2
	shadow := &Shadow{Model: sf, Head: shead}
	pf := &fakeModel{v: []float32{1, 0}, entered: make(chan int, 1)}
	g := New(Config{Model: pf, Head: head(2, 0.5, 0, nil), Shadow: shadow})
	post(t, g, "image/png", photo(t), "")
	shadow.Wait()
	if p, sh := <-pf.entered, <-sf.entered; p != 4*4*3 || sh != 2*2*3 {
		t.Fatalf("primary input %d, shadow input %d", p, sh)
	}
}

// A call that did not get a verdict (503 busy) never starts a shadow run.
func TestShadowNeverRunsAfterABusyAnswer(t *testing.T) {
	block := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(block) }) })
	pf := &fakeModel{v: []float32{1, 0}, block: block, entered: make(chan int, 4)}
	shadow := &Shadow{Model: &fakeModel{v: []float32{1, 0}}, Head: head(2, 0.5, 0, nil)}
	rec := testRecorder(t, 64)
	g := New(Config{Model: pf, Head: head(2, 0.5, 0, nil), Recorder: rec, Queue: 1, Shadow: shadow})
	img := photo(t)
	done := make(chan struct{}, 2)
	go func() { post(t, g, "image/png", img, ""); done <- struct{}{} }()
	<-pf.entered
	go func() { post(t, g, "image/png", img, ""); done <- struct{}{} }()
	time.Sleep(100 * time.Millisecond)
	if w, _ := post(t, g, "image/png", img, ""); w.Code != 503 { // the shadow slot is free now
		t.Fatalf("want busy, got %d", w.Code)
	}
	shadow.Wait()
	once.Do(func() { close(block) })
	<-done
	<-done
	shadow.Wait()
	for _, e := range events(t, rec) {
		if e.Step == "gate_shadow" && e.PrimaryVerdict == "" {
			t.Fatalf("a shadow ran for a call with no verdict: %+v", e)
		}
	}
}

func TestShadowNeverChangesTheAnswer(t *testing.T) {
	shadow := &Shadow{Model: &fakeModel{v: []float32{0, 1}}, Head: head(2, 0.01, 0, []float64{0, 9}), ModelSHA256: "smodel", HeadSHA256: "shead"}
	rec := testRecorder(t, 64)
	g := New(Config{Model: &fakeModel{v: []float32{1, 0}}, Head: head(2, 0.9, 0, nil), Recorder: rec, Build: "b", Shadow: shadow})
	w, m := post(t, g, "image/png", photo(t), "scan-7")
	shadow.Wait()
	if w.Code != 200 || m["verdict"] != NotFood {
		t.Fatalf("%d %v", w.Code, m)
	}
	ev := events(t, rec)
	if len(ev) != 2 {
		t.Fatalf("%d events", len(ev))
	}
	var sh Event
	for _, e := range ev {
		if e.Step == "gate_shadow" {
			sh = e
		}
	}
	if sh.Verdict != Food || sh.PrimaryVerdict != NotFood || sh.TraceID != "scan-7" || sh.ModelSHA256 != "smodel" ||
		sh.ImageSHA256 == "" || sh.RequestID != m["request_id"] || sh.Threshold != 0.01 {
		t.Fatalf("shadow line %+v", sh)
	}
}

func TestShadowIsSkippedWhenBusyAndNeverOnErrors(t *testing.T) {
	block := make(chan struct{})
	shadow := &Shadow{Model: &fakeModel{v: []float32{1, 0}, block: block}, Head: head(2, 0.5, 0, nil)}
	rec := testRecorder(t, 64)
	g := New(Config{Model: &fakeModel{v: []float32{1, 0}}, Head: head(2, 0.5, 0, nil), Recorder: rec, Shadow: shadow})
	post(t, g, "image/png", photo(t), "") // the shadow holds its slot
	post(t, g, "image/png", photo(t), "") // skipped
	post(t, g, "image/png", []byte("bad"), "")
	close(block)
	shadow.Wait()
	hw := httptest.NewRecorder()
	g.ServeHTTP(hw, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	var hm map[string]any
	json.Unmarshal(hw.Body.Bytes(), &hm)
	sh, _ := hm["shadow"].(map[string]any)
	if sh["runs"] != 1.0 || sh["skipped"] != 1.0 || sh["ok"] != true || sh["resample"] != ResampleBilinear {
		t.Fatalf("%v", sh)
	}
	shadows := 0
	for _, e := range events(t, rec) {
		if e.Step == "gate_shadow" {
			shadows++
		}
	}
	if shadows != 1 {
		t.Fatalf("%d shadow lines", shadows)
	}
}

func TestShadowThatDidNotLoadIsShownAndNeverRuns(t *testing.T) {
	shadow := &Shadow{LoadError: "a shadow gate needs -shadow-model-sha256 and -shadow-head-sha256"}
	g := New(Config{Model: &fakeModel{v: []float32{1, 0}}, Head: head(2, 0.5, 0, nil), Shadow: shadow})
	if w, m := post(t, g, "image/png", photo(t), ""); w.Code != 200 || m["verdict"] != NotFood {
		t.Fatalf("%d %v", w.Code, m)
	}
	hw := httptest.NewRecorder()
	g.ServeHTTP(hw, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	var hm map[string]any
	json.Unmarshal(hw.Body.Bytes(), &hm)
	sh, _ := hm["shadow"].(map[string]any)
	if sh["ok"] != false || sh["runs"] != 0.0 || !strings.Contains(sh["error"].(string), "needs") {
		t.Fatalf("%v", sh)
	}
}

// ---- the events log -------------------------------------------------------

func TestRecorderNeverBlocks(t *testing.T) {
	rec := NewRecorder(filepath.Join(t.TempDir(), "missing"), 1)
	start := time.Now()
	for i := 0; i < 500; i++ {
		rec.Record(Event{})
	}
	if time.Since(start) > time.Second {
		t.Fatal("Record blocked")
	}
	rec.Close()
	if rec.Written() != 0 || rec.Dropped() != 500 {
		t.Fatalf("%d %d", rec.Written(), rec.Dropped())
	}
	var none *Recorder
	none.Record(Event{})
}

func TestRecorderUsesTheUTCDay(t *testing.T) {
	dir := t.TempDir()
	rec := NewRecorder(dir, 4)
	rec.now = func() time.Time { return time.Date(2026, 10, 28, 22, 0, 0, 0, time.FixedZone("MDT", -6*3600)) }
	rec.Record(Event{Step: "gate"})
	rec.Close()
	if _, err := os.Stat(filepath.Join(dir, "events-20261029.jsonl")); err != nil {
		t.Fatal(err)
	}
}

// testRecorder is a Recorder whose writer is stopped before t's TempDir is
// removed (t.Cleanup runs last-in first-out). X42c: without it the writer
// could still make the day's file during the removal, and the test failed
// with "directory not empty" (seen 1 time in 15 runs).
func testRecorder(t *testing.T, buf int) *Recorder {
	t.Helper()
	r := NewRecorder(t.TempDir(), buf)
	t.Cleanup(r.Close)
	return r
}
