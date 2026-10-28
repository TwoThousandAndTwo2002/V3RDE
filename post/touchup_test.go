package post

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func tinyImage(w, h int) *image.RGBA {
	im := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			im.Set(x, y, color.RGBA{uint8(x * 7), uint8(y * 5), 90, 255})
		}
	}
	return im
}

func tinyJPEG(t *testing.T) []byte {
	var b bytes.Buffer
	if err := jpeg.Encode(&b, tinyImage(40, 30), nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func tinyPNG(t *testing.T) []byte {
	var b bytes.Buffer
	if err := png.Encode(&b, tinyImage(20, 10)); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// metaFake is the Meta Model API: /images/edits and /responses.
type metaFake struct {
	t          *testing.T
	editStatus int
	editPNG    bool
	checkText  string
	checkCode  int
	mu         sync.Mutex
	editForm   map[string]string
	editMime   string
	checkCalls int
}

func (f *metaFake) server() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer key" {
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/images/edits":
			if err := r.ParseMultipartForm(10 << 20); err != nil {
				w.WriteHeader(400)
				return
			}
			f.mu.Lock()
			f.editForm = map[string]string{}
			for k, v := range r.MultipartForm.Value {
				f.editForm[k] = v[0]
			}
			if fh := r.MultipartForm.File["image"]; len(fh) == 1 {
				f.editMime = fh[0].Header.Get("Content-Type")
			}
			f.mu.Unlock()
			if f.editStatus != 0 && f.editStatus != 200 {
				w.WriteHeader(f.editStatus)
				return
			}
			out := tinyJPEG(f.t)
			if f.editPNG {
				out = tinyPNG(f.t)
			}
			WriteJSON(w, 200, map[string]any{"id": "img_1", "data": []any{map[string]any{"b64_json": base64.StdEncoding.EncodeToString(out)}}})
		case "/responses":
			f.mu.Lock()
			f.checkCalls++
			f.mu.Unlock()
			if f.checkCode != 0 && f.checkCode != 200 {
				w.WriteHeader(f.checkCode)
				return
			}
			WriteJSON(w, 200, map[string]any{"id": "resp_c",
				"output": []any{map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": f.checkText}}}},
				"usage":  map[string]any{"input_tokens": 3000, "output_tokens": 50}})
		default:
			w.WriteHeader(404)
		}
	}))
}

type tuEnv struct {
	h     http.Handler
	cfg   *TouchupConfig
	fake  *metaFake
	items string
	stop  func()
}

func newTU(t *testing.T, f *metaFake, check bool) *tuEnv {
	f.t = t
	srv := f.server()
	dir := t.TempDir()
	cfg := &TouchupConfig{
		Editor:     &ImageEditor{Base: srv.URL, Key: "key", Model: DefaultImage},
		Session:    fakeAuth{},
		Budget:     NewBudget(1, 0.03, nil),
		Events:     NewEvents(filepath.Join(dir, "events")),
		Dir:        filepath.Join(dir, "touchups"),
		ItemPhotos: filepath.Join(dir, "items"),
		JobTimeout: 10 * time.Second,
	}
	if check {
		cfg.Checker = &Muse{Base: srv.URL, Key: "key", Model: DefaultModel, Effort: "low"}
	}
	return &tuEnv{h: NewTouchup(cfg), cfg: cfg, fake: f, items: cfg.ItemPhotos, stop: srv.Close}
}

// seed puts one analyze item photo of user in the item-photo folder and
// returns its photo_id (the only touch-up input, X36a).
func seed(t *testing.T, e *tuEnv, user string) string {
	t.Helper()
	os.MkdirAll(e.items, 0o755)
	pid := newHex(16)
	os.WriteFile(filepath.Join(e.items, pid+".jpg"), tinyJPEG(t), 0o644)
	os.WriteFile(filepath.Join(e.items, pid+".json"), []byte(`{"photo_id":"`+pid+`","user_id":"`+user+`"}`), 0o644)
	return pid
}

// tuReq is POST /api/touchup for a seeded photo of user (query: extra params).
func tuReq(t *testing.T, e *tuEnv, user, query string) *http.Request {
	r := httptest.NewRequest("POST", "/api/touchup?photo_id="+seed(t, e, user)+query, nil)
	return asUser(r, user)
}

func asUser(r *http.Request, u string) *http.Request {
	r.AddCookie(&http.Cookie{Name: SessionCookie, Value: u})
	return r
}

func do(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// wait polls GET until the job is not running.
func wait(t *testing.T, e *tuEnv, path, user string) Job {
	t.Helper()
	for i := 0; i < 200; i++ {
		w := do(e.h, asUser(httptest.NewRequest("GET", path, nil), user))
		if w.Code != 200 {
			t.Fatalf("GET %s: %d %s", path, w.Code, w.Body)
		}
		var j Job
		json.Unmarshal(w.Body.Bytes(), &j)
		if j.Status != JobRunning {
			return j
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("job did not finish")
	return Job{}
}

func start(t *testing.T, e *tuEnv, r *http.Request) string {
	t.Helper()
	w := do(e.h, r)
	if w.Code != http.StatusAccepted {
		t.Fatalf("start: %d %s", w.Code, w.Body)
	}
	var a map[string]any
	json.Unmarshal(w.Body.Bytes(), &a)
	return a["touchup_id"].(string)
}

func TestTouchupDoneStoresItemPhotoAndOwnerOnly(t *testing.T) {
	e := newTU(t, &metaFake{checkText: `{"same_food": true, "reason": "same items, better light"}`}, true)
	defer e.stop()
	id := start(t, e, tuReq(t, e, "u1", "&mode=background"))
	j := wait(t, e, "/api/touchup/"+id, "u1")
	if j.Status != JobDone || j.PhotoID == "" || j.Mode != ModeBackground {
		t.Fatalf("job %+v", j)
	}
	if want := 0.01 + (3000*1.25+50*4.25)/1e6; !near(j.CostUSD, want) {
		t.Fatalf("cost %v want %v", j.CostUSD, want)
	}
	// the edit request: the fixed prompt for the mode, jpeg, the photo's aspect
	f := e.fake.editForm
	if f["prompt"] != EditPrompts[ModeBackground] || f["model"] != DefaultImage || f["output_format"] != "jpeg" ||
		f["size"] != "40x30" || f["n"] != "1" || e.fake.editMime != "image/jpeg" {
		t.Fatalf("edit form %v mime %s", f, e.fake.editMime)
	}
	// stored in the item-photo format, readable for the owner only
	if b, err := ReadItemPhoto(e.cfg.Dir, j.PhotoID, "u1"); err != nil || len(b) == 0 {
		t.Fatalf("owner read: %v", err)
	}
	if _, err := ReadItemPhoto(e.cfg.Dir, j.PhotoID, "u2"); err == nil {
		t.Fatal("another user read the edit")
	}
	w := do(e.h, asUser(httptest.NewRequest("GET", "/api/touchup/"+id+"/photo", nil), "u1"))
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("photo: %d", w.Code)
	}
	if w := do(e.h, asUser(httptest.NewRequest("GET", "/api/touchup/"+id, nil), "u2")); w.Code != 404 {
		t.Fatalf("other user GET: %d", w.Code)
	}
	// one event line for the job, with both calls
	b, _ := os.ReadFile(e.cfg.Events.Path(time.Now()))
	if !strings.Contains(string(b), `"route":"touchup.job"`) || !strings.Contains(string(b), `"purpose":"touchup:check"`) ||
		!strings.Contains(string(b), `"remote_ip":"192.0.2.1"`) {
		t.Fatalf("events %s", b)
	}
}

func TestTouchupKeepsOriginalWhenFoodChanged(t *testing.T) {
	e := newTU(t, &metaFake{checkText: `{"same_food": false, "reason": "one apple is missing"}`}, true)
	defer e.stop()
	id := start(t, e, tuReq(t, e, "u1", ""))
	j := wait(t, e, "/api/touchup/"+id, "u1")
	if j.Status != JobKeptOriginal || j.PhotoID != "" || j.Reason != "one apple is missing" || j.Mode != ModeLight {
		t.Fatalf("job %+v", j)
	}
	if w := do(e.h, asUser(httptest.NewRequest("GET", "/api/touchup/"+id+"/photo", nil), "u1")); w.Code != 409 {
		t.Fatalf("photo of a kept original: %d", w.Code)
	}
	if fs, _ := os.ReadDir(e.cfg.Dir); len(fs) != 0 {
		t.Fatal("a rejected edit was stored")
	}
}

func TestTouchupFailsClosed(t *testing.T) {
	cases := []struct {
		name  string
		fake  *metaFake
		check bool
		want  string
	}{
		{"check HTTP 500", &metaFake{checkCode: 500}, true, JobKeptOriginal},
		{"check not JSON", &metaFake{checkText: "looks the same to me"}, true, JobKeptOriginal},
		{"check not a bool", &metaFake{checkText: `{"same_food": "yes"}`}, true, JobKeptOriginal},
		{"no checker", &metaFake{}, false, JobKeptOriginal},
		{"edit HTTP 500", &metaFake{editStatus: 500}, true, JobFailed},
	}
	for _, c := range cases {
		e := newTU(t, c.fake, c.check)
		id := start(t, e, tuReq(t, e, "u1", ""))
		if j := wait(t, e, "/api/touchup/"+id, "u1"); j.Status != c.want || j.PhotoID != "" {
			t.Fatalf("%s: %+v", c.name, j)
		}
		e.stop()
	}
}

func TestTouchupPNGEditIsStoredAsJPEG(t *testing.T) {
	e := newTU(t, &metaFake{editPNG: true, checkText: `{"same_food": true}`}, true)
	defer e.stop()
	id := start(t, e, tuReq(t, e, "u1", ""))
	j := wait(t, e, "/api/touchup/"+id, "u1")
	b, err := ReadItemPhoto(e.cfg.Dir, j.PhotoID, "u1")
	if err != nil || http.DetectContentType(b) != "image/jpeg" {
		t.Fatalf("%v %s", err, http.DetectContentType(b))
	}
}

func TestTouchupPhotoIDInput(t *testing.T) {
	e := newTU(t, &metaFake{checkText: `{"same_food": true}`}, true)
	defer e.stop()
	os.MkdirAll(e.items, 0o755)
	pid := strings.Repeat("ab", 16)
	os.WriteFile(filepath.Join(e.items, pid+".jpg"), tinyJPEG(t), 0o644)
	os.WriteFile(filepath.Join(e.items, pid+".json"), []byte(`{"photo_id":"`+pid+`","user_id":"u1"}`), 0o644)
	id := start(t, e, asUser(httptest.NewRequest("POST", "/api/touchup?photo_id="+pid, nil), "u1"))
	if j := wait(t, e, "/api/touchup/"+id, "u1"); j.Status != JobDone || j.SourcePhotoID != pid {
		t.Fatalf("%+v", j)
	}
	if w := do(e.h, asUser(httptest.NewRequest("POST", "/api/touchup?photo_id="+pid, nil), "u2")); w.Code != 404 {
		t.Fatalf("another user's photo_id: %d", w.Code)
	}
}

// X36a: the only input is photo_id. No upload, no bearer route, so no image
// reaches the paid calls without the analyze run's gate.
func TestTouchupOnlyPhotoIDInput(t *testing.T) {
	e := newTU(t, &metaFake{checkText: `{"same_food": true}`}, true)
	defer e.stop()
	for _, p := range []string{"/v1/touchup?photo_id=" + seed(t, e, "u1"), "/v1/touchup/x", "/v1/touchup/x/photo"} {
		for _, m := range []string{"POST", "GET"} {
			r := httptest.NewRequest(m, p, nil)
			r.Header.Set("Authorization", "Bearer tok")
			if w := do(e.h, r); w.Code != 404 && w.Code != 405 {
				t.Fatalf("%s %s: %d (the bearer route must not exist)", m, p, w.Code)
			}
		}
	}
	if w := do(e.h, asUser(httptest.NewRequest("POST", "/api/touchup", nil), "u1")); w.Code != 400 ||
		!strings.Contains(w.Body.String(), "photo_id is required") {
		t.Fatalf("no photo_id: %d %s", w.Code, w.Body)
	}
	up := asUser(httptest.NewRequest("POST", "/api/touchup?photo_id="+seed(t, e, "u1"), strings.NewReader("jpegbytes")), "u1")
	up.Header.Set("Content-Type", "image/jpeg")
	if w := do(e.h, up); w.Code != 400 || !strings.Contains(w.Body.String(), "no body") {
		t.Fatalf("upload with photo_id: %d %s", w.Code, w.Body)
	}
	if w := do(e.h, asUser(httptest.NewRequest("POST", "/api/touchup?photo_id=../../etc/passwd", nil), "u1")); w.Code != 404 {
		t.Fatalf("bad photo_id: %d", w.Code)
	}
	if e.fake.editForm != nil {
		t.Fatal("a refused request reached the paid edit call")
	}
}

func TestTouchupRefusals(t *testing.T) {
	e := newTU(t, &metaFake{checkText: `{"same_food": true}`}, true)
	defer e.stop()
	if w := do(e.h, httptest.NewRequest("POST", "/api/touchup?photo_id="+seed(t, e, "u1"), nil)); w.Code != 401 {
		t.Fatalf("no session: %d", w.Code)
	}
	if w := do(e.h, tuReq(t, e, "u1", "&mode=cartoon")); w.Code != 400 {
		t.Fatalf("free mode: %d", w.Code)
	}
	e.cfg.Budget = NewBudget(0.01, 0.03, nil)
	if w := do(e.h, tuReq(t, e, "u1", "")); w.Code != 503 ||
		!strings.Contains(w.Body.String(), CodeDailyCap) {
		t.Fatalf("cap: %d %s", w.Code, w.Body)
	}
	e.cfg.Budget = NewBudget(1, 0.03, nil)
	for i := 0; i < cap(e.cfg.slots); i++ {
		e.cfg.slots <- struct{}{}
	}
	if w := do(e.h, tuReq(t, e, "u1", "")); w.Code != 503 ||
		!strings.Contains(w.Body.String(), "busy") {
		t.Fatalf("busy: %d %s", w.Code, w.Body)
	}
}

func TestAsJPEGAndAspect(t *testing.T) {
	j, err := AsJPEG(tinyPNG(t))
	if err != nil || http.DetectContentType(j) != "image/jpeg" {
		t.Fatalf("png: %v", err)
	}
	if _, err := AsJPEG([]byte("RIFF\x00\x00\x00\x00WEBPVP8 ")); err == nil {
		t.Fatal("webp accepted")
	}
	if s := AspectSize(tinyJPEG(t)); s != "40x30" {
		t.Fatalf("aspect %q", s)
	}
	if s := AspectSize([]byte("nope")); s != "" {
		t.Fatalf("aspect of junk %q", s)
	}
	_ = io.Discard
}
