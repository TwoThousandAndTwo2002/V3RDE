package inference

// I1: one photo with many barcodes (T9 2026-11-01): the codes field, the
// lookups at the same time, barcodes + vision (a SAM box that holds a code is
// that catalog item), the catalog answer when the vision path cannot answer,
// and the ledger.

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const (
	codeMarinara = "078742370026"
	codeHershey  = "0034000002405"
	codeMiss     = "000000000000"
)

// reqCodes is req with the I1 "barcodes" field (and the live code, "" = none).
func reqCodes(t *testing.T, user, live string, codes []CodeBox) *http.Request {
	t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	pw, _ := mw.CreatePart(map[string][]string{"Content-Disposition": {`form-data; name="image"; filename="p.jpg"`}, "Content-Type": {"image/jpeg"}})
	_, _ = pw.Write(testPhoto(t))
	if live != "" {
		_ = mw.WriteField("barcode", live)
	}
	j, _ := json.Marshal(codes)
	_ = mw.WriteField("barcodes", string(j))
	_ = mw.Close()
	r := httptest.NewRequest(http.MethodPost, "/api/analyze", &b)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.AddCookie(&http.Cookie{Name: SessionCookie, Value: user})
	return r
}

// at is a code with a 80 x 40 box centred on (x, y) of SAM's 800 x 600 frame,
// sent in a 1600 x 1200 still (the service scales it).
func at(code string, x, y int) CodeBox {
	return CodeBox{Code: code, X: 2*x - 40, Y: 2*y - 20, W: 80, H: 40, FW: 1600, FH: 1200}
}

func names(a map[string]any) []string {
	var out []string
	for _, it := range items(a) {
		out = append(out, it["fields"].(map[string]any)["name"].(string))
	}
	return out
}

func TestParseBarcodes(t *testing.T) {
	got := ParseBarcodes(`[{"code":"078742370026","x":10,"y":10,"w":20,"h":10,"fw":100,"fh":100},
		{"code":"078742370026"},{"code":"12ab"},{"code":"1234567"},{"code":"00034000002405","x":90,"y":0,"w":20,"h":10,"fw":100,"fh":100},
		{"code":"12345678","x":-1,"y":0,"w":5,"h":5,"fw":100,"fh":100}]`)
	if len(got) != 3 || got[0].Code != codeMarinara || got[0].FW != 100 || got[1].Code != "00034000002405" || got[1].FW != 0 ||
		got[2].Code != "12345678" || got[2].FW != 0 {
		t.Fatalf("%+v", got) // a repeat, bad digits and a short code are dropped; a box outside its image keeps its code with no box
	}
	var many []string
	for i := 0; i < MaxBarcodes+5; i++ {
		many = append(many, `{"code":"`+strings.Repeat("0", 4)+string(rune('0'+i/10))+string(rune('0'+i%10))+`00"}`)
	}
	if n := len(ParseBarcodes("[" + strings.Join(many, ",") + "]")); n != MaxBarcodes {
		t.Fatal("cap", n)
	}
	for _, bad := range []string{"", "{", `{"code":"078742370026"}`, "[" + strings.Repeat(" ", 9<<10) + "]"} {
		if ParseBarcodes(bad) != nil {
			t.Fatal(bad)
		}
	}
	if l := withLive([]CodeBox{{Code: codeMarinara}}, codeMarinara); len(l) != 1 {
		t.Fatal("live code twice", l)
	}
	if l := withLive(nil, "abc"); len(l) != 0 {
		t.Fatal("bad live code", l)
	}
	if l := withLive([]CodeBox{{Code: codeMarinara}}, codeHershey); len(l) != 2 || l[0].Code != codeHershey {
		t.Fatal(l)
	}
}

func TestCentreIn(t *testing.T) {
	c := at(codeMarinara, 700, 150) // centre (1400, 300) of 1600 x 1200 = (700, 150) of 800 x 600
	if !centreIn(c, [4]int{600, 0, 799, 299}, 800, 600) || centreIn(c, [4]int{0, 0, 199, 299}, 800, 600) {
		t.Fatal("scaled centre")
	}
	if centreIn(CodeBox{Code: codeMarinara}, [4]int{0, 0, 799, 599}, 800, 600) {
		t.Fatal("a code with no box is in no box")
	}
}

// Barcodes + vision: Marinara's code is in the Beans box (that box is the
// catalog item, not a second item), Hershey's code is in no box, a MISS is
// logged; Soup (vision) follows the catalog items.
func TestAnalyzeManyBarcodesAndVision(t *testing.T) {
	s := newSvc(t, threeBoxes(t), "FOOD", 0, nil)
	w, a := s.do(reqCodes(t, "u1", "", []CodeBox{at(codeMarinara, 700, 150), at(codeHershey, 400, 500), {Code: codeMiss}}))
	if w.Code != 200 || a["source"] != SourceVision || strings.Join(names(a), "|") != "Great Value Marinara Pasta Sauce|Hershey's Chocolate Syrup|Soup" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	it := items(a)
	if it[0]["source"] != SourceBarcode || it[1]["source"] != SourceBarcode || it[2]["source"] != nil || it[0]["item"] != 1.0 || it[2]["item"] != 3.0 {
		t.Fatalf("%s", w.Body)
	}
	if it[0]["fields"].(map[string]any)["number_of_serving"] != 1.0 {
		t.Fatal("catalog servings start at the default")
	}
	ev := s.events(t)[0]
	if len(ev.Barcodes) != 3 || ev.Barcodes[0].Item != 1 || ev.Barcodes[0].Box != 3 || ev.Barcodes[1].Item != 2 || ev.Barcodes[1].Box != 0 ||
		ev.Barcodes[2].Outcome != "MISS" || ev.Barcodes[2].Item != 0 {
		t.Fatalf("%+v", ev.Barcodes)
	}
	if ev.Boxes[2].Outcome != "item" || ev.Boxes[2].Item != 1 || ev.Items != 3 || len(ev.PhotoIDs) != 3 {
		t.Fatalf("%+v", ev)
	}
	if s.gate != 1 {
		t.Fatal("the vision path runs for a photo with codes and other food", s.gate)
	}
}

// Every vision item is a code: the answer is the catalog items.
func TestAnalyzeManyBarcodesAllCovered(t *testing.T) {
	s := newSvc(t, threeBoxes(t), "FOOD", 0, nil)
	w, a := s.do(reqCodes(t, "u1", "", []CodeBox{at(codeMarinara, 100, 100), at(codeHershey, 700, 100)}))
	if w.Code != 200 || a["source"] != SourceBarcode || a["model"] != "catalog" || strings.Join(names(a), "|") != "Great Value Marinara Pasta Sauce|Hershey's Chocolate Syrup" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

// The vision path cannot answer (not food, the daily cap, the scanner is
// down): the codes answer. With no HIT the old errors stay.
func TestAnalyzeManyBarcodesWhenVisionFails(t *testing.T) {
	s := newSvc(t, threeBoxes(t), "NOT_FOOD", 0, nil)
	w, a := s.do(reqCodes(t, "u1", "", []CodeBox{at(codeMarinara, 100, 100), at(codeHershey, 700, 100)}))
	if w.Code != 200 || a["source"] != SourceBarcode || a["count"] != 2.0 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if ev := s.events(t)[0]; ev.Status != 200 || !strings.HasPrefix(ev.Error, "vision "+CodeNotFood) {
		t.Fatalf("%+v", ev)
	}
	w, a = s.do(reqCodes(t, "u1", "", []CodeBox{{Code: codeMiss}, {Code: "00000000"}}))
	errorIs(t, w, a, 422, CodeNotFood)

	c := newSvc(t, threeBoxes(t), "FOOD", 0, func(c *ServiceConfig) { c.Budget.Cap = 0 })
	w, a = c.do(reqCodes(t, "u1", codeHershey, []CodeBox{at(codeMarinara, 100, 100)}))
	if w.Code != 200 || a["count"] != 2.0 || c.gate != 0 {
		t.Fatalf("the cap: %d %s gate %d", w.Code, w.Body, c.gate)
	}
	w, a = c.do(reqCodes(t, "u1", "", []CodeBox{{Code: codeMiss}, {Code: "00000000"}}))
	errorIs(t, w, a, 503, CodeDailyCap)
}

// The live code joins the codes of the still; one code (live and still the
// same) is the old single path.
func TestAnalyzeManyBarcodesLiveCode(t *testing.T) {
	s := newSvc(t, threeBoxes(t), "NOT_FOOD", 0, nil)
	w, a := s.do(reqCodes(t, "u1", codeHershey, []CodeBox{at(codeMarinara, 100, 100)}))
	if w.Code != 200 || strings.Join(names(a), "|") != "Hershey's Chocolate Syrup|Great Value Marinara Pasta Sauce" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	w, a = s.do(reqCodes(t, "u1", codeMarinara, []CodeBox{at(codeMarinara, 100, 100)}))
	if w.Code != 200 || a["count"] != 1.0 || items(a)[0]["source"] != nil || s.gate != 1 {
		t.Fatalf("single path: %d %s gate %d", w.Code, w.Body, s.gate)
	}
	if ev := s.events(t)[1]; ev.BarcodeOutcome != "HIT" || len(ev.Barcodes) != 0 {
		t.Fatalf("%+v", ev)
	}
}

// A code of a product that is live in the caller's listings, and a HIT that
// is not admissible, are not items; the ledger says why.
func TestAnalyzeManyBarcodesDedupAndAdmissible(t *testing.T) {
	s := newSvc(t, threeBoxes(t), "NOT_FOOD", 0, nil)
	s.listing["u1"] = []map[string]any{live("Hershey's Chocolate Syrup", time.Now().Add(time.Hour))}
	w, a := s.do(reqCodes(t, "u1", "", []CodeBox{at(codeMarinara, 100, 100), at(codeHershey, 700, 100), {Code: "04000000000001"}}))
	if w.Code != 200 || strings.Join(names(a), "|") != "Great Value Marinara Pasta Sauce" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	ev := s.events(t)[0]
	if ev.DroppedListed != 1 || ev.Barcodes[1].Note != "already_listed" || ev.Barcodes[2].Note != "not_admissible" {
		t.Fatalf("%+v", ev)
	}
}

// I1 review: the smallest box that holds the code is the product; a bigger
// box around it (a bag, a tray) stays its own item.
func TestAnalyzeManyBarcodesSmallestBox(t *testing.T) {
	f := &fakeAPI{sam: func(string) (int, string) {
		return 200, samLine(t, [4]int{0, 0, 499, 599}, [4]int{620, 0, 799, 199}, [4]int{400, 200, 599, 399})
	}, muse: func(string, map[string]any) map[string]any {
		return textResp("m", itemsText(ent(1, nil, "Bag of bread"), ent(2, nil, "Beans"), ent(3, nil, "Jar of sauce")))
	}}
	s := newSvc(t, f, "FOOD", 0, nil)
	w, a := s.do(reqCodes(t, "u1", "", []CodeBox{at(codeMarinara, 450, 300), at(codeHershey, 700, 100)}))
	got := strings.Join(names(a), "|")
	if w.Code != 200 || !strings.Contains(got, "Bag of bread") || strings.Contains(got, "Jar of sauce") || strings.Contains(got, "Beans") {
		t.Fatalf("%d %s %+v %+v", w.Code, got, s.events(t)[0].Boxes, s.events(t)[0].Barcodes)
	}
}

// I1 review: a HIT with no box is the scanner's item with the same name.
func TestAnalyzeManyBarcodesNoBoxByName(t *testing.T) {
	s := newSvc(t, threeBoxes(t, "Soup", "Soup", "Hershey's Chocolate Syrup"), "FOOD", 0, nil)
	w, a := s.do(reqCodes(t, "u1", codeHershey, []CodeBox{at(codeMarinara, 100, 100)}))
	if w.Code != 200 || strings.Join(names(a), "|") != "Hershey's Chocolate Syrup|Great Value Marinara Pasta Sauce" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

// I1 review: a still of another shape (EXIF turned) matches no box.
func TestAnalyzeManyBarcodesOtherShape(t *testing.T) {
	s := newSvc(t, threeBoxes(t), "FOOD", 0, nil)
	turned := CodeBox{Code: codeMarinara, X: 1360, Y: 260, W: 80, H: 40, FW: 1600, FH: 900}
	w, a := s.do(reqCodes(t, "u1", "", []CodeBox{turned, {Code: codeHershey}}))
	if w.Code != 200 || strings.Join(names(a), "|") != "Great Value Marinara Pasta Sauce|Hershey's Chocolate Syrup|Soup|Beans" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if !sameShape(1600, 1200, 800, 600) || sameShape(1600, 900, 800, 600) || sameShape(0, 1, 800, 600) {
		t.Fatal("sameShape")
	}
}

// I1 review: one code in the still and no live code runs the scanner too (a
// plate with one packet); one live code alone is today's fast path.
func TestAnalyzeOneStillCodeRunsTheScanner(t *testing.T) {
	s := newSvc(t, threeBoxes(t), "FOOD", 0, nil)
	w, a := s.do(reqCodes(t, "u1", "", []CodeBox{at(codeMarinara, 700, 150)}))
	if w.Code != 200 || strings.Join(names(a), "|") != "Great Value Marinara Pasta Sauce|Soup" || s.gate != 1 {
		t.Fatalf("%d %s gate %d", w.Code, w.Body, s.gate)
	}
	if !stillOnly([]CodeBox{at(codeMarinara, 1, 1)}, "") || stillOnly([]CodeBox{at(codeMarinara, 1, 1)}, codeMarinara) ||
		stillOnly([]CodeBox{{Code: codeMarinara}}, "") {
		t.Fatal("stillOnly")
	}
	if codesKey([]CodeBox{{Code: "2"}, {Code: "1"}}) != "1,2" {
		t.Fatal("codesKey")
	}
}

// I1 review: every code already listed and nothing else found = 409 (as the
// one-code path); listed codes stay counted when the scanner answers.
func TestAnalyzeManyBarcodesListed(t *testing.T) {
	s := newSvc(t, threeBoxes(t), "NOT_FOOD", 0, nil)
	s.listing["u1"] = []map[string]any{live("Hershey's Chocolate Syrup", time.Now().Add(time.Hour)), live("Great Value Marinara Pasta Sauce", time.Now().Add(time.Hour))}
	w, a := s.do(reqCodes(t, "u1", "", []CodeBox{at(codeMarinara, 100, 100), at(codeHershey, 700, 100)}))
	errorIs(t, w, a, 409, CodeAlreadyListed)
	v := newSvc(t, threeBoxes(t), "FOOD", 0, nil)
	v.listing["u1"] = []map[string]any{live("Hershey's Chocolate Syrup", time.Now().Add(time.Hour))}
	w, a = v.do(reqCodes(t, "u1", "", []CodeBox{at(codeMarinara, 700, 150), at(codeHershey, 400, 500)}))
	if w.Code != 200 || strings.Join(names(a), "|") != "Great Value Marinara Pasta Sauce|Soup" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if ev := v.events(t)[0]; ev.DroppedListed != 1 {
		t.Fatalf("listed codes counted: %+v", ev)
	}
}
