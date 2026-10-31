package inference

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

const stockGTIN = "00078742370026" // the fake catalog's hit (service_test.go)

func jpegOf(tag string) []byte { return append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, []byte(tag)...) }

func stockDir(t *testing.T, files map[string][]byte) string {
	d := t.TempDir()
	for rel, b := range files {
		p := filepath.Join(d, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

// N1: the cutout first, else the photo as is; nothing for a bad key; a file
// that is not a JPEG, or one over MaxStockBytes, is "unreadable" (review).
func TestStockFind(t *testing.T) {
	d := stockDir(t, map[string][]byte{
		"b/00000000000017.jpg": jpegOf("cut"), "a/00000000000017.jpg": jpegOf("asis"),
		"a/00000000000024.jpg":    jpegOf("asis2"),
		"b/00000000000031.jpg":    []byte("not a jpeg"),
		"b/00000000000048.jpg":    append(jpegOf("big"), make([]byte, MaxStockBytes)...),
		"a/../00000000000055.jpg": jpegOf("outside"),
	})
	s := &StockPhotos{Dir: d}
	for _, c := range []struct{ g, kind, tag string }{
		{"00000000000017", StockCutout, "cut"}, {"00000000000024", StockAsIs, "asis2"},
		{"00000000000031", StockUnreadable, ""}, {"00000000000048", StockUnreadable, ""}, {"00000000000062", StockNone, ""},
		{"../00000000000055", StockNone, ""}, {"0000000000017", StockNone, ""}, {"", StockNone, ""},
	} {
		b, kind := s.Find(c.g)
		if kind != c.kind || (c.tag == "" && b != nil) || (c.tag != "" && !bytes.Equal(b, jpegOf(c.tag))) {
			t.Fatalf("%q: %q %q", c.g, kind, b)
		}
	}
	var off *StockPhotos
	if b, k := off.Find("00000000000017"); b != nil || k != StockNone {
		t.Fatal("nil StockPhotos found a photo")
	}
}

// N1: a session caller's barcode hit gets the stock photo as its item photo
// (with the credit); the camera shot stays stored; the photo route serves the
// stock photo to its owner only. A bearer caller keeps the camera shot.
func TestAnalyzeBarcodeStock(t *testing.T) {
	cut := jpegOf("marinara cutout")
	dir := stockDir(t, map[string][]byte{"b/" + stockGTIN + ".jpg": cut, "a/" + stockGTIN + ".jpg": jpegOf("as is")})
	s := newSvc(t, threeBoxes(t), "FOOD", 0, func(c *ServiceConfig) { c.Stock = &StockPhotos{Dir: dir} })
	shot := testPhoto(t)
	w, a := s.do(req(t, "/api/analyze", "u1", shot, "078742370026"))
	if w.Code != 200 || a["source"] != SourceBarcode {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	it := items(a)[0]
	if it["photo_credit"] != StockCredit {
		t.Fatalf("credit %v", it["photo_credit"])
	}
	pid := it["photo_id"].(string)
	b, m, err := ReadItem(filepath.Join(s.photos, "items"), pid, "u1")
	if err != nil || !bytes.Equal(b, cut) || m.Kind != "stock" || m.Stock != StockCutout || m.GTIN14 != stockGTIN {
		t.Fatalf("%v %+v", err, m)
	}
	if _, err := os.Stat(filepath.Join(s.photos, s.events(t)[0].ImageSHA256+".jpg")); err != nil {
		t.Fatal("the camera shot is not stored:", err)
	}
	if ev := s.events(t)[0]; ev.StockPhoto != StockCutout || len(ev.PhotoIDs) != 1 || ev.PhotoIDs[0] != pid {
		t.Fatalf("%+v", ev)
	}
	for _, c := range []struct {
		user string
		code int
	}{{"u1", 200}, {"u2", 404}} {
		r := httptest.NewRequest(http.MethodGet, "/api/analyze/photo/"+pid, nil)
		r.AddCookie(&http.Cookie{Name: SessionCookie, Value: c.user})
		rw := httptest.NewRecorder()
		s.h.ServeHTTP(rw, r)
		if rw.Code != c.code || (c.code == 200 && !bytes.Equal(rw.Body.Bytes(), cut)) {
			t.Fatalf("%s: %d", c.user, rw.Code)
		}
	}
	// bearer (/v1/analyze): the camera shot, no credit, no look-up
	w, a = s.do(req(t, "/v1/analyze", "bearer", shot, "078742370026"))
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	bi := items(a)[0]
	if _, has := bi["photo_credit"]; has {
		t.Fatal("bearer answer has a credit")
	}
	b, m, err = ReadItem(filepath.Join(s.photos, "items"), bi["photo_id"].(string), "")
	if err != nil || !bytes.Equal(b, shot) || m.Kind != "full" {
		t.Fatalf("%v %+v", err, m)
	}
	if ev := s.events(t)[1]; ev.StockPhoto != "" {
		t.Fatalf("bearer stock_photo %q", ev.StockPhoto)
	}
}

// N1: no stock photo for the product = the camera shot (stock_photo none);
// the folder off = the camera shot, nothing looked up, the JSON as before.
func TestAnalyzeBarcodeNoStock(t *testing.T) {
	for _, c := range []struct {
		name  string
		stock *StockPhotos
		ev    string
	}{{"empty folder", &StockPhotos{Dir: t.TempDir()}, StockNone}, {"off", nil, ""}} {
		s := newSvc(t, threeBoxes(t), "FOOD", 0, func(sc *ServiceConfig) { sc.Stock = c.stock })
		shot := testPhoto(t)
		w, a := s.do(req(t, "/api/analyze", "u1", shot, "078742370026"))
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", c.name, w.Code, w.Body)
		}
		it := items(a)[0]
		if _, has := it["photo_credit"]; has {
			t.Fatalf("%s: credit", c.name)
		}
		b, m, err := ReadItem(filepath.Join(s.photos, "items"), it["photo_id"].(string), "u1")
		if err != nil || !bytes.Equal(b, shot) || m.Kind != "full" || m.Stock != "" {
			t.Fatalf("%s: %v %+v", c.name, err, m)
		}
		if ev := s.events(t)[0]; ev.StockPhoto != c.ev {
			t.Fatalf("%s: stock_photo %q", c.name, ev.StockPhoto)
		}
		raw, _ := json.Marshal(it)
		if bytes.Contains(raw, []byte("photo_credit")) {
			t.Fatalf("%s: %s", c.name, raw)
		}
	}
}
