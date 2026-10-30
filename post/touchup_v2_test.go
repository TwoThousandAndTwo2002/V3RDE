package post

// X48o: the touch-up service runs the auto touch-up (inference.V2) when V2 is
// set: the item cut again from the upload, the Muse bigger edit checked in
// code, the scene rule's mask, the studio blur.

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/TwoThousandAndTwo2002/Icarus/inference"
	analyze "github.com/TwoThousandAndTwo2002/Icarus/server"
)

// foodJPEG is a w x h photo: a round "item" in the middle, a busy background.
func foodJPEG(t *testing.T, w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dx, dy := float64(x-w/2)/float64(w/4), float64(y-h/2)/float64(h/4)
			if dx*dx+dy*dy < 1 {
				img.SetRGBA(x, y, color.RGBA{200, uint8(100 + x%30), 30, 255})
			} else {
				img.SetRGBA(x, y, color.RGBA{uint8((x * 7) % 200), uint8((y * 13) % 200), uint8((x * y) % 200), 255})
			}
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

var boxPrompt = regexp.MustCompile(`^<0f>1<\|box;(x1=\d+;y1=\d+;x2=\d+;y2=\d+;w=\d+;h=\d+)\|>`)

// v2Fake: Muse Image (JSON) answers with the input a little lighter (a true
// edit); SAM finds nothing for words (so the scene rule takes the box) and a
// full mask for a box prompt. sam counts the SAM calls.
func v2Fake(t *testing.T, sam *atomic.Int32) *httptest.Server {
	full := make([]byte, 16*16)
	for i := range full {
		full[i] = 1
	}
	enc, err := inference.EncodeOneBit(full, 16, 16)
	if err != nil {
		t.Fatal(err)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer key" {
			w.WriteHeader(401)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch r.URL.Path {
		case "/images/edits":
			imgs, _ := body["images"].([]any)
			first, _ := imgs[0].(map[string]any)
			url, _ := first["image_url"].(string)
			raw, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(url, "data:image/jpeg;base64,"))
			im, _, err := image.Decode(bytes.NewReader(raw))
			if err != nil {
				w.WriteHeader(400)
				return
			}
			out, _ := analyze.JPEG(analyze.Enhance(im, analyze.Look{Exposure: 0.1, Saturation: 1.05}), 90)
			WriteJSON(w, 200, map[string]any{"data": []any{map[string]any{"b64_json": base64.StdEncoding.EncodeToString(out)}}})
		case "/responses":
			sam.Add(1)
			text := ""
			in, _ := body["input"].([]any)
			for _, m := range in {
				mm, _ := m.(map[string]any)
				cs, _ := mm["content"].([]any)
				for _, c := range cs {
					if cm, _ := c.(map[string]any); cm["type"] == "input_text" {
						text, _ = cm["text"].(string)
					}
				}
			}
			line := "<0f>\n"
			if m := boxPrompt.FindStringSubmatch(text); m != nil {
				line = fmt.Sprintf("<0f>1<|box;%s|><|mask;x=0;y=0;data=16,16,%s|>\n", m[1], enc)
			}
			WriteJSON(w, 200, map[string]any{"id": "sam", "output": []any{map[string]any{"type": "message",
				"content": []any{map[string]any{"type": "output_text", "text": line}}}},
				"usage": map[string]any{"input_tokens": 10, "output_tokens": 5}})
		default:
			w.WriteHeader(404)
		}
	}))
}

// v2Env: the service with V2 on, one upload and the item photo of user cut
// from it (record + analyze event with the scan's name).
func v2Env(t *testing.T, sam *atomic.Int32) (*tuEnv, string, func()) {
	srv := v2Fake(t, sam)
	dir := t.TempDir()
	photos, events := filepath.Join(dir, "photos"), filepath.Join(dir, "analyze")
	items := filepath.Join(photos, "items")
	_ = os.MkdirAll(items, 0o755)
	_ = os.MkdirAll(events, 0o755)
	_ = os.WriteFile(filepath.Join(photos, "abc.jpg"), foodJPEG(t, 800, 600), 0o644)
	pid := newHex(16)
	meta, _ := json.Marshal(inference.ItemPhotoMeta{PhotoID: pid, UserID: "u1", RequestID: "r1", ImageSHA256: "abc", Item: 1,
		Kind: "crop", Box: []int{100, 75, 300, 225}, FrameW: 400, FrameH: 300, TS: "2026-10-30T12:00:00Z"})
	_ = os.WriteFile(filepath.Join(items, pid+".json"), meta, 0o644)
	_ = os.WriteFile(filepath.Join(items, pid+".jpg"), foodJPEG(t, 600, 450), 0o644)
	_ = os.WriteFile(filepath.Join(events, "events-20261030.jsonl"),
		[]byte(`{"request_id":"r1","boxes":[{"n":1,"outcome":"item","item":1,"name":"Ripe Plantains","box":[100,75,300,225]}]}`+"\n"), 0o644)
	hc := &http.Client{Timeout: 20 * time.Second}
	cfg := &TouchupConfig{
		Editor:     &ImageEditor{Base: srv.URL, Key: "key", Model: DefaultImage},
		Session:    fakeAuth{},
		Budget:     NewBudget(1, 0.06, nil),
		Events:     NewEvents(filepath.Join(dir, "events")),
		Dir:        filepath.Join(dir, "touchups"),
		ItemPhotos: items,
		JobTimeout: 20 * time.Second,
		V2: &inference.V2{API: &inference.Client{URL: srv.URL + "/responses", Key: "key", HTTP: hc},
			Muse: inference.DefaultMuse, SAM: inference.DefaultSAM, ImageURL: srv.URL, ImageModel: DefaultImage, HTTP: hc},
		Photos: photos, AnalyzeEvents: events,
	}
	return &tuEnv{h: NewTouchup(cfg), cfg: cfg, items: items, stop: srv.Close}, pid, srv.Close
}

func TestTouchupV2Background(t *testing.T) {
	var sam atomic.Int32
	e, pid, stop := v2Env(t, &sam)
	defer stop()
	id := start(t, e, asUser(httptest.NewRequest("POST", "/api/touchup?mode=background&photo_id="+pid, nil), "u1"))
	j := wait(t, e, "/api/touchup/"+id, "u1")
	if j.Status != JobDone || j.PhotoID == "" || j.CheckModel != "ssim" || !strings.HasPrefix(j.Reason, "box: ") || j.CostUSD <= 0.01 {
		t.Fatalf("job %+v", j)
	}
	if sam.Load() < 2 { // the words mask (none), then the box
		t.Fatalf("SAM calls %d", sam.Load())
	}
	b, err := ReadItemPhoto(e.cfg.Dir, j.PhotoID, "u1")
	if err != nil {
		t.Fatalf("owner read: %v", err)
	}
	if _, _, err := image.Decode(bytes.NewReader(b)); err != nil {
		t.Fatalf("stored edit: %v", err)
	}
	if _, err := ReadItemPhoto(e.cfg.Dir, j.PhotoID, "u2"); err == nil {
		t.Fatal("another user read the edit")
	}
	ev, _ := os.ReadFile(e.cfg.Events.Path(time.Now()))
	if !strings.Contains(string(ev), `"v2":true`) || !strings.Contains(string(ev), `"muse":true`) ||
		!strings.Contains(string(ev), "Ripe Plantains") { // the scan's name, in the note
		t.Fatalf("events %s", ev)
	}
	w := do(e.h, httptest.NewRequest("GET", "/healthz", nil))
	if !strings.Contains(w.Body.String(), `"v2":true`) {
		t.Fatalf("healthz %s", w.Body)
	}
}

// "light": the Muse bigger edit only: no SAM call, no new background.
func TestTouchupV2Light(t *testing.T) {
	var sam atomic.Int32
	e, pid, stop := v2Env(t, &sam)
	defer stop()
	id := start(t, e, asUser(httptest.NewRequest("POST", "/api/touchup?mode=light&photo_id="+pid, nil), "u1"))
	j := wait(t, e, "/api/touchup/"+id, "u1")
	if j.Status != JobDone || j.PhotoID == "" || j.Reason != "light: no new background" || sam.Load() != 0 {
		t.Fatalf("job %+v sam %d", j, sam.Load())
	}
}
