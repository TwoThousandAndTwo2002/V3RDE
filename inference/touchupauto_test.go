package inference

// X48o: the touch-up the service runs (touchupauto.go).

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// The scene rule in one run: a words mask inside the box -> words (no box
// call); no words mask -> the box; no box -> words. Every result is the Muse
// edit, cut and blurred, and the costs add up.
func TestTouchupAuto(t *testing.T) {
	photo := itemJPEG(t, 400, 300, 0)
	var edits atomic.Int32
	srv := v3Server(t, false, false, &edits, nil)
	defer srv.Close()
	v := &V2{API: &Client{URL: srv.URL + "/responses", Key: "k"}, SAM: DefaultSAM, ImageURL: srv.URL, ImageModel: "muse-image-1.0"}
	src, _, _ := image.Decode(bytes.NewReader(photo))
	box := image.Rect(100, 75, 250, 225)
	for _, c := range []struct {
		name     string
		target   image.Rectangle
		concepts []string
		prefix   string
	}{
		{"a package (the words mask stays in the box)", box, []string{"fruit"}, "words: the words mask stays"},
		{"a pile (no words mask)", box, []string{"food"}, "box: the words found no mask"},
		{"a whole-photo meal (no box)", image.Rectangle{}, []string{"fruit"}, "words: no item box"},
	} {
		r, err := v.Touchup(context.Background(), TouchItem{Src: src, Target: c.target, Concepts: c.concepts, Note: "test"})
		if err != nil || !r.Muse || r.SSIM < SSIMMin || !strings.HasPrefix(r.Mask, c.prefix) || len(r.JPEG) == 0 || r.CostUSD < ImageCallPrice {
			t.Errorf("%s: err %v muse %v ssim %.2f mask %q cost %.4f", c.name, err, r.Muse, r.SSIM, r.Mask, r.CostUSD)
		}
	}
	// a refused edit (a new picture): the code look on the photo, said so
	var e2 atomic.Int32
	bad := v3Server(t, true, false, &e2, nil)
	defer bad.Close()
	v.API.URL, v.ImageURL = bad.URL+"/responses", bad.URL
	r, err := v.Touchup(context.Background(), TouchItem{Src: src, Target: box, Concepts: []string{"fruit"}})
	if err != nil || r.Muse || !strings.HasPrefix(r.Mask, "the Muse edit was refused") || len(r.JPEG) == 0 {
		t.Fatalf("refused: err %v muse %v mask %q", err, r.Muse, r.Mask)
	}
	if _, err := v.Touchup(context.Background(), TouchItem{}); err == nil {
		t.Fatal("no picture gave a touch-up")
	}
}

// LoadItem: the record and the upload give the cut, the box and the scan's
// name; with no record, the item photo as it is.
func TestLoadItem(t *testing.T) {
	photos, events := t.TempDir(), t.TempDir()
	items := filepath.Join(photos, "items")
	_ = os.MkdirAll(items, 0o755)
	upload := itemJPEG(t, 800, 600, 0)
	_ = os.WriteFile(filepath.Join(photos, "abc.jpg"), upload, 0o644)
	id := "0123456789abcdef0123456789abcdef"
	meta, _ := json.Marshal(ItemPhotoMeta{PhotoID: id, UserID: "u", RequestID: "r1", ImageSHA256: "abc", Item: 1, Kind: "crop",
		Box: []int{100, 75, 300, 225}, FrameW: 400, FrameH: 300, TS: "2026-10-30T23:59:00Z"})
	_ = os.WriteFile(filepath.Join(items, id+".json"), meta, 0o644)
	_ = os.WriteFile(filepath.Join(events, "events-20261030.jsonl"),
		[]byte(`{"request_id":"r1","boxes":[{"n":1,"outcome":"item","item":1,"name":"Ripe Plantains","box":[100,75,300,225]}]}`+"\n"), 0o644)
	item := itemJPEG(t, 480, 360, 0)
	it := LoadItem(photos, events, items, id, item, 500, 0.35)
	if it.Target.Empty() || it.Concepts[0] != "ripe plantains" || !strings.Contains(it.Note, "wider") || it.Src.Bounds().Dx() < 500 {
		t.Fatalf("item: target %v concepts %v note %q size %v", it.Target, it.Concepts, it.Note, it.Src.Bounds())
	}
	// X48o review: only the record's day and the next day are read
	_ = os.WriteFile(filepath.Join(events, "events-20261031.jsonl"),
		[]byte(`{"request_id":"r2","boxes":[{"n":1,"outcome":"item","item":1,"name":"Pears"}]}`+"\n"), 0o644)
	_ = os.WriteFile(filepath.Join(events, "events-20261101.jsonl"),
		[]byte(`{"request_id":"r3","boxes":[{"n":1,"outcome":"item","item":1,"name":"Kiwis"}]}`+"\n"), 0o644)
	if n, _ := ItemScanNear(events, "r2", 1, "2026-10-30T23:59:00Z"); n != "Pears" {
		t.Fatalf("next day: %q", n)
	}
	if n, _ := ItemScanNear(events, "r3", 1, "2026-10-30T23:59:00Z"); n != "" {
		t.Fatalf("two days later was read: %q", n)
	}
	if n, _ := ItemScanNear(events, "r1", 1, "not a time"); n != "" {
		t.Fatalf("no time: %q", n)
	}
	none := LoadItem(photos, events, items, "ffffffffffffffffffffffffffffffff", item, 500, 0.35)
	if !none.Target.Empty() || none.Src == nil || none.Note != "the item photo (no record)" {
		t.Fatalf("no record: %+v", none.Note)
	}
}
