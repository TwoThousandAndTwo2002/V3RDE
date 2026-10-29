package inference

// X41a: the item photo bundle. The owner and request check, the order, the
// size target, the cache, the busy answer, the refusals and the pool.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// noisyJPEG is a w x h photo-like JPEG (gradients + noise), so the size
// search has real work to do.
func noisyJPEG(t *testing.T, w, h int, seed int64) []byte {
	t.Helper()
	r := rand.New(rand.NewSource(seed))
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			n := uint8(r.Intn(64))
			img.Set(x, y, color.RGBA{uint8(x*255/w) ^ n, uint8(y*255/h) ^ n, n * 3, 255})
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// saveItems stores n item photos of one analyze answer for user.
func saveItems(t *testing.T, s *svc, user, reqID string, n, w, h int) []string {
	t.Helper()
	ps := &PhotoStore{Dir: s.photos}
	var ids []string
	for i := 1; i <= n; i++ {
		id, err := ps.SaveItem(noisyJPEG(t, w, h, int64(i)), ItemPhotoMeta{UserID: user, RequestID: reqID, Item: i, Kind: "crop"})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

func bundleReq(path, auth string, body any) *http.Request {
	b, _ := json.Marshal(body)
	if s, ok := body.(string); ok {
		b = []byte(s)
	}
	r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	switch auth {
	case "":
	case "bearer":
		r.Header.Set("Authorization", "Bearer tok")
	default:
		r.AddCookie(&http.Cookie{Name: SessionCookie, Value: auth})
	}
	return r
}

func bundleOut(t *testing.T, w *httptest.ResponseRecorder) BundleAnswer {
	t.Helper()
	var a BundleAnswer
	if err := json.Unmarshal(w.Body.Bytes(), &a); err != nil {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	return a
}

var reqA = strings.Repeat("a", 32)

func TestBundleAnswer(t *testing.T) {
	s := newSvc(t, threeBoxes(t), "FOOD", 0, nil)
	ids := saveItems(t, s, "u1", reqA, 3, 1600, 1200)
	ids = []string{ids[2], ids[0], ids[1]} // the answer keeps the asked order
	w, _ := s.do(bundleReq("/api/analyze/photos", "u1", map[string]any{"request_id": reqA, "photo_ids": ids}))
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	a := bundleOut(t, w)
	if a.RequestID != reqA || a.Side != 800 || a.KB != 40 || a.Count != 3 || len(a.Items) != 3 || a.Cached {
		t.Fatalf("%+v", a)
	}
	for i, it := range a.Items {
		if it.PhotoID != ids[i] || it.Kind != "crop" {
			t.Fatalf("item %d: %+v", i, it)
		}
		raw, err := base64.StdEncoding.DecodeString(it.JPEG)
		if err != nil || len(raw) != it.Bytes {
			t.Fatalf("item %d: base64 %v, %d bytes, said %d", i, err, len(raw), it.Bytes)
		}
		sum := sha256.Sum256(raw)
		if hex.EncodeToString(sum[:]) != it.SHA256 {
			t.Fatalf("item %d: sha256", i)
		}
		cfg, err := jpeg.DecodeConfig(bytes.NewReader(raw))
		if err != nil || cfg.Width != 800 || cfg.Height != 600 || it.W != 800 || it.H != 600 {
			t.Fatalf("item %d: %v %dx%d", i, err, cfg.Width, cfg.Height)
		}
		if it.Bytes > 40*1024 && it.Q != bundleMinQ {
			t.Fatalf("item %d: %d bytes at q%d is over 40 KB", i, it.Bytes, it.Q)
		}
	}
	if a.Items[0].Item != 3 || a.Items[1].Item != 1 {
		t.Fatalf("item numbers %d %d", a.Items[0].Item, a.Items[1].Item)
	}
	// the same call again: from the cache, the same bytes
	w2, _ := s.do(bundleReq("/api/analyze/photos", "u1", map[string]any{"request_id": reqA, "photo_ids": ids}))
	b := bundleOut(t, w2)
	if !b.Cached || len(b.Items) != 3 || b.Items[0].SHA256 != a.Items[0].SHA256 || b.Items[2].SHA256 != a.Items[2].SHA256 {
		t.Fatalf("cache: %+v", b)
	}
	// another size is another bundle
	w3, _ := s.do(bundleReq("/api/analyze/photos", "u1", map[string]any{"request_id": reqA, "photo_ids": ids, "side": 282, "kb": 10}))
	c := bundleOut(t, w3)
	if c.Cached || c.Items[0].W != 282 || c.Items[0].H != 211 {
		t.Fatalf("282: %+v", c.Items[0])
	}
	var got []Event
	for _, e := range s.events(t) {
		if e.Step == "photos_bundle" {
			got = append(got, e)
		}
	}
	if len(got) != 3 || got[0].Bundle == nil || got[0].Bundle.ForRequestID != reqA || got[0].Bundle.Count != 3 ||
		got[0].Bundle.Cached || !got[1].Bundle.Cached || got[0].Caller != "session" || got[0].UserID != "u1" ||
		got[0].Bundle.BytesIn == 0 || got[0].Bundle.BytesOut == 0 || len(got[0].Bundle.Q) != 3 || got[0].Bundle.Workers < 1 {
		t.Fatalf("events %+v", got)
	}
	for _, e := range got {
		b, _ := json.Marshal(e)
		if strings.Contains(string(b), "jpeg_base64") || len(b) > 2000 {
			t.Fatalf("an event must not hold image bytes: %d bytes", len(b))
		}
	}
}

func TestBundleOwnerAndRequestCheck(t *testing.T) {
	s := newSvc(t, threeBoxes(t), "FOOD", 0, nil)
	mine := saveItems(t, s, "u1", reqA, 2, 300, 200)
	other := saveItems(t, s, "u2", reqA, 1, 300, 200)
	reqB := strings.Repeat("b", 32)
	laterMine := saveItems(t, s, "u1", reqB, 1, 300, 200)
	bearer := saveItems(t, s, "", reqA, 1, 300, 200)
	for name, tc := range map[string]struct {
		auth string
		req  string
		ids  []string
		want int
	}{
		"own":                     {"u1", reqA, mine, 200},
		"another user":            {"u2", reqA, mine, 404},
		"one of another user":     {"u1", reqA, append([]string{mine[0]}, other...), 404},
		"another answer":          {"u1", reqA, append([]string{mine[0]}, laterMine...), 404},
		"unknown id":              {"u1", reqA, []string{mine[0], strings.Repeat("c", 32)}, 404},
		"bearer reads bearer":     {"bearer", reqA, bearer, 200},
		"bearer cannot read user": {"bearer", reqA, mine, 404},
		"user cannot read bearer": {"u1", reqA, bearer, 404},
	} {
		w, _ := s.do(bundleReq("/v1/analyze/photos", tc.auth, map[string]any{"request_id": tc.req, "photo_ids": tc.ids}))
		if w.Code != tc.want {
			t.Errorf("%s: %d %s", name, w.Code, w.Body.String())
		}
		if tc.want == 404 && !strings.Contains(w.Body.String(), `"code":"not_found"`) {
			t.Errorf("%s: %s", name, w.Body.String())
		}
	}
}

func TestBundleRefusals(t *testing.T) {
	s := newSvc(t, threeBoxes(t), "FOOD", 0, nil)
	ids := saveItems(t, s, "u1", reqA, 1, 300, 200)
	many := make([]string, 25)
	for i := range many {
		many[i] = fmt.Sprintf("%032x", i+1)
	}
	for name, tc := range map[string]struct {
		auth string
		body any
		want int
	}{
		"no caller":   {"", map[string]any{"request_id": reqA, "photo_ids": ids}, 401},
		"not JSON":    {"u1", "{", 400},
		"no ids":      {"u1", map[string]any{"request_id": reqA, "photo_ids": []string{}}, 400},
		"25 ids":      {"u1", map[string]any{"request_id": reqA, "photo_ids": many}, 400},
		"same id 2x":  {"u1", map[string]any{"request_id": reqA, "photo_ids": []string{ids[0], ids[0]}}, 400},
		"bad id":      {"u1", map[string]any{"request_id": reqA, "photo_ids": []string{"../../etc"}}, 400},
		"bad request": {"u1", map[string]any{"request_id": "x", "photo_ids": ids}, 400},
		"side 500":    {"u1", map[string]any{"request_id": reqA, "photo_ids": ids, "side": 500}, 400},
		"kb 5":        {"u1", map[string]any{"request_id": reqA, "photo_ids": ids, "kb": 5}, 400},
		"kb 91":       {"u1", map[string]any{"request_id": reqA, "photo_ids": ids, "kb": 91}, 400},
		"body 5 KB":   {"u1", `{"request_id":"` + reqA + `","pad":"` + strings.Repeat("x", 5000) + `"}`, 400},
	} {
		w, _ := s.do(bundleReq("/api/analyze/photos", tc.auth, tc.body))
		if w.Code != tc.want {
			t.Errorf("%s: %d %s", name, w.Code, w.Body.String())
		}
	}
	w, _ := s.do(httptest.NewRequest(http.MethodGet, "/api/analyze/photos", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET: %d", w.Code)
	}
	// 24 ids is the limit, not an error (they are unknown, so 404 and not 400)
	w, _ = s.do(bundleReq("/api/analyze/photos", "u1", map[string]any{"request_id": reqA, "photo_ids": many[:24]}))
	if w.Code != 404 {
		t.Errorf("24 ids: %d", w.Code)
	}
}

func TestBundleBusy(t *testing.T) {
	s := newSvc(t, threeBoxes(t), "FOOD", 0, nil)
	ids := saveItems(t, s, "u1", reqA, 1, 300, 200)
	slots := (&ServiceConfig{}).bundleSlot()
	for i := 0; i < BundleConcurrent; i++ {
		slots <- struct{}{}
	}
	w, _ := s.do(bundleReq("/api/analyze/photos", "u1", map[string]any{"request_id": reqA, "photo_ids": ids}))
	for i := 0; i < BundleConcurrent; i++ {
		<-slots
	}
	if w.Code != 503 || !strings.Contains(w.Body.String(), `"code":"busy"`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	w, _ = s.do(bundleReq("/api/analyze/photos", "u1", map[string]any{"request_id": reqA, "photo_ids": ids}))
	if w.Code != 200 {
		t.Fatalf("after: %d", w.Code)
	}
	// a bundle that is all in the cache needs no slot
	for i := 0; i < BundleConcurrent; i++ {
		slots <- struct{}{}
	}
	w, _ = s.do(bundleReq("/api/analyze/photos", "u1", map[string]any{"request_id": reqA, "photo_ids": ids}))
	for i := 0; i < BundleConcurrent; i++ {
		<-slots
	}
	if w.Code != 200 || !bundleOut(t, w).Cached {
		t.Fatalf("cached while busy: %d", w.Code)
	}
}

// The review: the cache has a bound (only the default kb, one file per photo
// and side); the other targets are made again each time.
func TestBundleCacheBound(t *testing.T) {
	s := newSvc(t, threeBoxes(t), "FOOD", 0, nil)
	ids := saveItems(t, s, "u1", reqA, 3, 400, 300)
	calls := []map[string]any{
		{"request_id": reqA, "photo_ids": ids},
		{"request_id": reqA, "photo_ids": []string{ids[2], ids[1], ids[0]}},
		{"request_id": reqA, "photo_ids": ids[:2]},
		{"request_id": reqA, "photo_ids": ids, "side": 282},
		{"request_id": reqA, "photo_ids": ids, "kb": 30},
		{"request_id": reqA, "photo_ids": ids, "kb": 31},
	}
	var cached []bool
	for _, c := range calls {
		w, _ := s.do(bundleReq("/api/analyze/photos", "u1", c))
		if w.Code != 200 {
			t.Fatalf("%v: %d", c, w.Code)
		}
		cached = append(cached, bundleOut(t, w).Cached)
	}
	if fmt.Sprint(cached) != "[false true true false false false]" {
		t.Fatalf("cached %v", cached)
	}
	files, _ := filepath.Glob(filepath.Join(s.photos, "items", "small", "*"))
	if len(files) != 6 { // 3 photos x 2 sides
		t.Fatalf("%d cache files: %v", len(files), files)
	}
}

// The review: a "full" item photo is the upload as it was: it can be a PNG,
// and a JPEG can have an EXIF orientation.
func TestBundleFullPhotos(t *testing.T) {
	s := newSvc(t, threeBoxes(t), "FOOD", 0, nil)
	ps := &PhotoStore{Dir: s.photos}
	img := image.NewRGBA(image.Rect(0, 0, 1200, 900))
	for i := range img.Pix {
		img.Pix[i] = uint8(i)
	}
	var pngb bytes.Buffer
	if err := png.Encode(&pngb, img); err != nil {
		t.Fatal(err)
	}
	pid, _ := ps.SaveItem(pngb.Bytes(), ItemPhotoMeta{UserID: "u1", RequestID: reqA, Item: 1, Kind: "full"})
	jid, _ := ps.SaveItem(withOrientation(t, noisyJPEG(t, 1200, 900, 5), 6, "MM"), ItemPhotoMeta{UserID: "u1", RequestID: reqA, Item: 2, Kind: "full"})
	w, _ := s.do(bundleReq("/api/analyze/photos", "u1", map[string]any{"request_id": reqA, "photo_ids": []string{pid, jid}}))
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	a := bundleOut(t, w)
	if a.Items[0].W != 800 || a.Items[0].H != 600 {
		t.Fatalf("png: %dx%d", a.Items[0].W, a.Items[0].H)
	}
	if a.Items[1].W != 600 || a.Items[1].H != 800 { // orientation 6: 1200x900 is 900x1200 upright
		t.Fatalf("exif 6: %dx%d", a.Items[1].W, a.Items[1].H)
	}
}

// pngHeader is only a PNG signature and an IHDR chunk: enough for
// DecodeConfig, so a huge size is cheap to test.
func pngHeader(w, h uint32) []byte {
	ihdr := []byte{'I', 'H', 'D', 'R', byte(w >> 24), byte(w >> 16), byte(w >> 8), byte(w), byte(h >> 24), byte(h >> 16), byte(h >> 8), byte(h), 8, 2, 0, 0, 0}
	c := crc32.ChecksumIEEE(ihdr)
	b := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 13}
	b = append(b, ihdr...)
	return append(b, byte(c>>24), byte(c>>16), byte(c>>8), byte(c))
}

func TestBundleRoutesOptions(t *testing.T) {
	s := newSvc(t, threeBoxes(t), "FOOD", 0, func(c *ServiceConfig) { c.Origin = "https://app.example" })
	w, _ := s.do(httptest.NewRequest(http.MethodOptions, "/api/analyze/photos", nil))
	if w.Code != 204 || w.Header().Get("Access-Control-Allow-Origin") != "https://app.example" {
		t.Fatalf("preflight: %d %q", w.Code, w.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestShrinkSizes(t *testing.T) {
	small := noisyJPEG(t, 200, 100, 1)
	it, err := shrink(small, 800, 40*1024)
	if err != nil || it.W != 200 || it.H != 100 {
		t.Fatalf("a small photo is not made larger: %+v %v", it, err)
	}
	tall := noisyJPEG(t, 600, 1200, 2)
	it, err = shrink(tall, 282, 40*1024)
	if err != nil || it.W != 141 || it.H != 282 {
		t.Fatalf("the long side is the height: %+v %v", it, err)
	}
	// a target no quality can meet: q30 is kept and its real size reported
	it, err = shrink(noisyJPEG(t, 1600, 1200, 3), 800, 1024)
	if err != nil || it.Q != bundleMinQ || it.Bytes <= 1024 {
		t.Fatalf("q30 fallback: q%d %d bytes %v", it.Q, it.Bytes, err)
	}
	// a large target: the highest quality is used
	it, err = shrink(noisyJPEG(t, 300, 200, 4), 800, 90*1024)
	if err != nil || it.Q < 90 {
		t.Fatalf("a large target keeps a high quality: q%d %v", it.Q, err)
	}
	if _, err := shrink([]byte("not a jpeg"), 800, 1024); err == nil {
		t.Fatal("a file that is not an image must be an error")
	}
	if _, err := shrink(pngHeader(8000, 7000), 800, 1024); err == nil || !strings.Contains(err.Error(), "too many pixels") {
		t.Fatalf("56 MP must be refused before the decode: %v", err)
	}
}

// The review: the search must give the highest quality that fits: q fits,
// and q+1 does not (or q is 95).
func TestShrinkBestQuality(t *testing.T) {
	src := noisyJPEG(t, 640, 480, 7)
	img, _ := jpeg.Decode(bytes.NewReader(src))
	size := func(q int) int {
		var b bytes.Buffer
		_ = jpeg.Encode(&b, img, &jpeg.Options{Quality: q})
		return b.Len()
	}
	for _, kb := range []int{10, 15, 20, 25, 30, 40, 50, 60, 70, 80, 90} {
		it, err := shrink(src, 800, kb*1024)
		if err != nil {
			t.Fatal(err)
		}
		if it.Q > bundleMinQ && it.Bytes > kb*1024 {
			t.Fatalf("kb %d: q%d is %d bytes, over the target", kb, it.Q, it.Bytes)
		}
		if it.Q < bundleMaxQ && size(it.Q+1) <= kb*1024 {
			t.Fatalf("kb %d: q%d, but q%d also fits", kb, it.Q, it.Q+1)
		}
	}
}

func TestShrinkAllPool(t *testing.T) {
	dir := t.TempDir()
	put := func(name string, b []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	raws := []string{put("a.jpg", noisyJPEG(t, 400, 300, 1)), put("b.jpg", noisyJPEG(t, 300, 400, 2)), put("c.jpg", noisyJPEG(t, 500, 500, 3))}
	pool := make(chan struct{}, 2)
	out, err := shrinkAll(context.Background(), raws, 282, 40*1024, pool)
	if err != nil || len(out) != 3 || out[0].W != 282 || out[1].H != 282 || out[2].W != 282 {
		t.Fatalf("%v %+v", err, out)
	}
	bad := []string{raws[0], put("bad.jpg", []byte("bad")), raws[2]}
	if _, err := shrinkAll(context.Background(), bad, 282, 40*1024, pool); err == nil || strings.Contains(err.Error(), "canceled") {
		t.Fatalf("the real error must come back: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := shrinkAll(ctx, raws, 282, 40*1024, pool); err == nil {
		t.Fatal("a cancelled call must be an error")
	}
	ctx, cancel = context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond)
	if _, err := shrinkAll(ctx, raws, 282, 40*1024, make(chan struct{}, 1)); err != context.DeadlineExceeded {
		t.Fatalf("deadline: %v", err)
	}
	if len(pool) != 0 {
		t.Fatalf("the pool must be empty after the calls: %d", len(pool))
	}
}
