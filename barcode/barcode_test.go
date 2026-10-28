package barcode

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

// The codes here have check digits computed by the GS1 rule (a short
// Python check, 2026-10-28): 4006381333931 (EAN-13), 036000291452 (UPC-A),
// 96385074 (GTIN-8), 10012345678902 (GTIN-14, a case pack).

// ---- the code -------------------------------------------------------------

func TestCheckDigit(t *testing.T) {
	for code, want := range map[string]bool{
		"4006381333931": true, "4006381333932": false,
		"036000291452": true, "036000291453": false,
		"96385074": true, "10012345678902": true, "00012345678905": true,
		"1234567": false, "123456789012345": false, "": false,
		"4006-3813-3393-1": true, // only the digits count
	} {
		if CheckDigitOK(code) != want {
			t.Errorf("%q: want %v", code, want)
		}
	}
}

func TestNormalizeKeepsASCIIDigitsOnly(t *testing.T) {
	if got := Normalize(" 0 36000-29145 2\n"); got != "036000291452" {
		t.Fatalf("%q", got)
	}
	if got := Normalize("٠١٢٣"); got != "" { // Arabic-Indic digits are not ASCII digits
		t.Fatalf("%q", got)
	}
	if got := Normalize("abc"); got != "" {
		t.Fatalf("%q", got)
	}
}

func TestCanonicalIsGTIN14(t *testing.T) {
	for in, want := range map[string]string{
		"036000291452":   "00036000291452",
		"0036000291452":  "00036000291452",
		"00036000291452": "00036000291452",
		"96385074":       "00000096385074",
		"4006381333931":  "04006381333931",
		"10012345678902": "10012345678902",
	} {
		got, err := Canonical(in)
		if err != nil || got != want || !CheckDigitOK(got) {
			t.Errorf("%s -> %s %v", in, got, err)
		}
		if again, _ := Canonical(got); again != got {
			t.Errorf("not stable: %s -> %s", got, again)
		}
	}
}

func TestCanonicalRefusals(t *testing.T) {
	for in, want := range map[string]string{
		"":                "no digits",
		"no code":         "no digits",
		"12345":           "5 digits; want GTIN-8/12/13/14",
		"123456789":       "9 digits; want GTIN-8/12/13/14",
		"123456789012345": "15 digits; want GTIN-8/12/13/14",
		"036000291453":    "fails its mod-10 check digit",
		"4006381333932":   "fails its mod-10 check digit",
		"10012345678903":  "fails its mod-10 check digit",
	} {
		if _, err := Canonical(in); err == nil || err.Error() != want {
			t.Errorf("%q: got %v, want %q", in, err, want)
		}
	}
}

func TestSameProduct(t *testing.T) {
	if !SameProduct("036000291452", "0 036000 291452") || !SameProduct("00036000291452", "036000291452") {
		t.Fatal("one product, two spellings")
	}
	if SameProduct("00012345678905", "10012345678902") {
		t.Fatal("a case pack is another product")
	}
	if SameProduct("036000291453", "036000291453") {
		t.Fatal("an invalid code is no product")
	}
}

func TestEquivalentKeys(t *testing.T) {
	for in, want := range map[string][]string{
		"036000291452":   {"036000291452", "0036000291452", "00036000291452"},
		"0036000291452":  {"0036000291452", "036000291452", "00036000291452"},
		"4006381333931":  {"4006381333931", "04006381333931"},
		"00036000291452": {"00036000291452", "0036000291452", "036000291452"},
		"10012345678902": {"10012345678902"},
		"01234567890128": {"01234567890128", "1234567890128"},
		"96385074":       {"96385074"},
	} {
		if got := EquivalentKeys(in); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %v", in, got)
		}
	}
	if EquivalentKeys("none") != nil {
		t.Fatal("no digits: no keys")
	}
}

// ---- the product ----------------------------------------------------------

func TestCompleteFillsOnlyWhatABarcodeDecides(t *testing.T) {
	p := Complete(Product{Title: "OAT CEREAL", Weight: 0.34})
	if p.NumberOfServing != 1 || p.ServingType != "individual" || p.MealCondition != "ready_to_eat" {
		t.Fatalf("%+v", p)
	}
	if p.Description != "" || p.IsNonVeg != 0 || p.MealCost != 0 {
		t.Fatal("a model field was set")
	}
	kept := Complete(Product{Title: "x", NumberOfServing: 12, ServingType: "bulk", MealCondition: "sealed", Weight: 2})
	if kept.NumberOfServing != 12 || kept.ServingType != "bulk" || kept.MealCondition != "sealed" {
		t.Fatalf("a value was changed: %+v", kept)
	}
	if Complete(Product{NumberOfServing: -3}).NumberOfServing != 1 {
		t.Fatal("servings below 1 become 1")
	}
}

func TestProvenance(t *testing.T) {
	prov := Provenance(Complete(Product{Title: "OAT CEREAL", NumberOfServing: 10, Weight: 0.34}))
	want := map[string]string{"title": "fdc", "number_of_serving": "fdc", "weight": "fdc",
		"serving_type": "constant", "meal_condition": "constant"}
	if !reflect.DeepEqual(prov, want) {
		t.Fatalf("%v", prov)
	}
	bare := Provenance(Complete(Product{}))
	if _, ok := bare["title"]; ok || bare["number_of_serving"] != "constant" {
		t.Fatalf("%v", bare)
	}
	for _, f := range ModelFields {
		if _, ok := prov[f]; ok {
			t.Errorf("%s has a provenance", f)
		}
	}
}

func TestAdmissible(t *testing.T) {
	for p, want := range map[Product]bool{
		{Title: "x", NumberOfServing: 1, Weight: 0.1}:  true,
		{Title: "  ", NumberOfServing: 1, Weight: 0.1}: false,
		{Title: "x", NumberOfServing: 0, Weight: 0.1}:  false,
		{Title: "x", NumberOfServing: 1, Weight: 0}:    false,
		{Title: "x", NumberOfServing: 1, Weight: -0.5}: false,
	} {
		if Admissible(p) != want {
			t.Errorf("%+v: want %v", p, want)
		}
	}
}

func TestProductJSONHasEveryKey(t *testing.T) {
	b, _ := json.Marshal(Complete(Product{Barcode: "00036000291452", Title: "x", Weight: 1, Source: "fdc"}))
	var m map[string]any
	json.Unmarshal(b, &m)
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if strings.Join(keys, " ") != "barcode description is_non_veg meal_condition meal_cost number_of_serving serving_type source title weight" {
		t.Fatalf("%v", keys)
	}
	if m["is_non_veg"] != 0.0 || m["meal_cost"] != 0.0 {
		t.Fatalf("zero values must be in the JSON: %s", b)
	}
}

// ---- the lookup -----------------------------------------------------------

var catalog = MapStore{"00036000291452": {Title: "CHEESE CRACKERS", Servings: 6, WeightKg: 0.198}}

func TestLookupEverySpelling(t *testing.T) {
	for _, code := range []string{"036000291452", "0036000291452", "00036000291452", "0-36000-29145-2"} {
		p, g14, found, invalid, err := Lookup(catalog, code)
		if !found || invalid != nil || err != nil || g14 != "00036000291452" {
			t.Fatalf("%s: %v %v %v", code, found, invalid, err)
		}
		if p.Title != "CHEESE CRACKERS" || p.NumberOfServing != 6 || p.Weight != 0.198 || p.Source != "fdc" ||
			p.Barcode != "00036000291452" || p.ServingType != "individual" {
			t.Fatalf("%+v", p)
		}
	}
	if _, g14, found, invalid, err := Lookup(catalog, "4006381333931"); found || invalid != nil || err != nil || g14 != "04006381333931" {
		t.Fatal("a miss has its key and no error")
	}
	if _, g14, _, invalid, _ := Lookup(catalog, "036000291453"); invalid == nil || g14 != "" {
		t.Fatal("an invalid code has no key")
	}
}

type failing struct{}

func (failing) Get(string) (Row, bool, error) { return Row{}, false, errors.New("disk I/O error") }
func (failing) Rows() int                     { return 9 }

// ---- the service ----------------------------------------------------------

func call(t *testing.T, h http.Handler, method, path, trace string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	r := httptest.NewRequest(method, path, nil)
	if trace != "" {
		r.Header.Set("X-Trace-Id", trace)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("not JSON: %s", w.Body)
	}
	return w, m
}

func TestServiceOutcomes(t *testing.T) {
	h := New(Config{Store: catalog, CatalogSHA256: "sum", Build: "b1"})
	w, m := call(t, h, "GET", "/v1/barcode/036000291452", "scan-1")
	prod, _ := m["product"].(map[string]any)
	if w.Code != 200 || m["outcome"] != Hit || m["found"] != true || m["admissible"] != true || m["gtin14"] != "00036000291452" ||
		prod["title"] != "CHEESE CRACKERS" || prod["weight"] != 0.198 || m["trace_id"] != "scan-1" ||
		w.Header().Get("X-Trace-Id") != "scan-1" || w.Header().Get("X-Request-Id") != m["request_id"] {
		t.Fatalf("hit %d %v", w.Code, m)
	}
	if prov, _ := m["provenance"].(map[string]any); prov["title"] != "fdc" {
		t.Fatalf("provenance %v", m["provenance"])
	}
	_, m = call(t, h, "GET", "/v1/barcode/4006381333931", "")
	if m["outcome"] != Miss || m["product"] != nil || m["found"] != false || m["gtin14"] != "04006381333931" {
		t.Fatalf("miss %v", m)
	}
	if _, has := m["provenance"]; has {
		t.Fatal("a miss has no provenance")
	}
	_, m = call(t, h, "GET", "/v1/barcode/036000291453", "")
	if m["outcome"] != Invalid || m["why"] != "fails its mod-10 check digit" || m["gtin14"] != nil {
		t.Fatalf("invalid %v", m)
	}
	w, m = call(t, h, "POST", "/v1/barcode/036000291452", "")
	if w.Code != 405 || m["outcome"] != Invalid || m["why"] != "GET only" {
		t.Fatalf("post %d %v", w.Code, m)
	}
	_, m = call(t, h, "GET", "/v1/barcode/036000291452", "bad trace id!")
	if m["trace_id"] != m["request_id"] || len(m["request_id"].(string)) != 32 {
		t.Fatalf("a bad trace id is replaced: %v", m)
	}
	ms, _ := m["ms"].(map[string]any)
	if _, ok := ms["lookup"]; !ok {
		t.Fatalf("ms %v", m)
	}
}

func TestServiceFailsClosed(t *testing.T) {
	h := New(Config{LoadError: "catalog sha256 abc is not the pin"})
	w, m := call(t, h, "GET", "/v1/barcode/036000291452", "")
	if w.Code != 200 || m["outcome"] != Unavailable || m["found"] != false || m["why"] != "catalog not loaded: catalog sha256 abc is not the pin" {
		t.Fatalf("%d %v", w.Code, m)
	}
	w, m = call(t, h, "GET", "/healthz", "")
	if w.Code != 503 || m["ok"] != false || m["error"] != "catalog sha256 abc is not the pin" {
		t.Fatalf("healthz %d %v", w.Code, m)
	}
	hf := New(Config{Store: failing{}})
	_, m = call(t, hf, "GET", "/v1/barcode/036000291452", "")
	if m["outcome"] != Unavailable || m["why"] != "disk I/O error" {
		t.Fatalf("a failing store %v", m)
	}
	if _, m := call(t, hf, "GET", "/healthz", ""); m["unavailable"] != 1.0 {
		t.Fatalf("unavailable count %v", m)
	}
}

func TestHealthzCounts(t *testing.T) {
	rec := NewRecorder(t.TempDir(), 16)
	defer rec.Close()
	h := New(Config{Store: catalog, CatalogSHA256: "sum", Build: "b1", Recorder: rec})
	call(t, h, "GET", "/v1/barcode/036000291452", "")
	call(t, h, "GET", "/v1/barcode/036000291452", "")
	call(t, h, "GET", "/v1/barcode/4006381333931", "")
	call(t, h, "GET", "/v1/barcode/1", "")
	call(t, h, "POST", "/v1/barcode/036000291452", "") // a 405 counts as INVALID
	w, m := call(t, h, "GET", "/healthz", "")
	if w.Code != 200 || m["ok"] != true || m["hit"] != 2.0 || m["miss"] != 1.0 || m["invalid"] != 2.0 || m["unavailable"] != 0.0 ||
		m["catalog_rows"] != 1.0 || m["catalog_sha256"] != "sum" || m["build"] != "b1" || m["service"] != "barcode" {
		t.Fatalf("%v", m)
	}
	if _, ok := m["events_written"]; !ok {
		t.Fatal("no events counts")
	}
}

func TestLongCodeIsCut(t *testing.T) {
	dir := t.TempDir()
	rec := NewRecorder(dir, 4)
	h := New(Config{Store: catalog, Recorder: rec})
	_, m := call(t, h, "GET", "/v1/barcode/"+strings.Repeat("9", 500), "")
	rec.Close()
	if m["outcome"] != Invalid || m["why"] != "64 digits; want GTIN-8/12/13/14" {
		t.Fatalf("%v", m)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if len(files) != 1 {
		t.Fatalf("files %v", files)
	}
	b, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	var e Event
	if err := json.Unmarshal(b, &e); err != nil {
		t.Fatal(err)
	}
	if len(e.Code) != maxCode {
		t.Fatalf("the events line keeps %d digits", len(e.Code))
	}
}

// ---- the events log -------------------------------------------------------

func TestEventsLines(t *testing.T) {
	dir := t.TempDir()
	rec := NewRecorder(dir, 16)
	rec.now = func() time.Time { return time.Date(2026, 10, 28, 23, 59, 0, 0, time.FixedZone("ET", -4*3600)) }
	h := New(Config{Store: catalog, CatalogSHA256: "sum", Build: "b1", Recorder: rec})
	call(t, h, "GET", "/v1/barcode/0%2036000-29145%202", "scan-9")
	call(t, h, "GET", "/v1/barcode/036000291453", "")
	rec.Close()
	b, err := os.ReadFile(filepath.Join(dir, "events-20261029.jsonl")) // the UTC day
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 || rec.Written() != 2 {
		t.Fatalf("lines %d", len(lines))
	}
	var e Event
	if err := json.Unmarshal([]byte(lines[0]), &e); err != nil {
		t.Fatal(err)
	}
	if e.Service != "barcode" || e.Step != "barcode" || e.TraceID != "scan-9" || e.Outcome != Hit || e.Status != 200 ||
		e.Code != "036000291452" || e.GTIN14 != "00036000291452" || !e.Admissible || e.CatalogSHA256 != "sum" ||
		e.CatalogRows != 1 || e.Build != "b1" {
		t.Fatalf("%+v", e)
	}
	e = Event{} // a fresh value: omitempty keys that are absent keep old values
	if err := json.Unmarshal([]byte(lines[1]), &e); err != nil {
		t.Fatal(err)
	}
	if e.Outcome != Invalid || e.Why != "fails its mod-10 check digit" || e.GTIN14 != "" {
		t.Fatalf("%+v", e)
	}
}

func TestRecorderNeverBlocks(t *testing.T) {
	rec := NewRecorder(filepath.Join(t.TempDir(), "missing"), 1) // writes fail: the folder is not there
	start := time.Now()
	for i := 0; i < 500; i++ {
		rec.Record(Event{})
	}
	if time.Since(start) > time.Second {
		t.Fatal("Record blocked")
	}
	rec.Close()
	if rec.Written() != 0 || rec.Dropped() != 500 {
		t.Fatalf("written %d dropped %d", rec.Written(), rec.Dropped())
	}
	var none *Recorder
	none.Record(Event{})
}

// The answer JSON is the shape the inference service reads: every key,
// in this order (ids and times masked).
func TestAnswerJSONShape(t *testing.T) {
	h := New(Config{Store: catalog})
	mask := regexp.MustCompile(`"(request_id|trace_id)":"[0-9a-f]{32}"|"ms":\{[^}]*\}`)
	for path, want := range map[string]string{
		"/v1/barcode/036000291452": `{"request_id":X,"trace_id":X,"outcome":"HIT","found":true,"gtin14":"00036000291452","admissible":true,` +
			`"product":{"barcode":"00036000291452","title":"CHEESE CRACKERS","description":"","is_non_veg":0,"number_of_serving":6,` +
			`"serving_type":"individual","meal_condition":"ready_to_eat","weight":0.198,"meal_cost":0,"source":"fdc"},` +
			`"provenance":{"meal_condition":"constant","number_of_serving":"fdc","serving_type":"constant","title":"fdc","weight":"fdc"},X}`,
		"/v1/barcode/4006381333931": `{"request_id":X,"trace_id":X,"outcome":"MISS","found":false,"gtin14":"04006381333931","admissible":false,"product":null,X}`,
		"/v1/barcode/036000291453":  `{"request_id":X,"trace_id":X,"outcome":"INVALID","found":false,"admissible":false,"product":null,"why":"fails its mod-10 check digit",X}`,
	} {
		r := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		got := mask.ReplaceAllString(strings.TrimSpace(w.Body.String()), "X")
		want = strings.NewReplacer(`"request_id":X`, `X`, `"trace_id":X`, `X`).Replace(want)
		if got != want || w.Header().Get("Content-Type") != "application/json" {
			t.Errorf("%s:\n got %s\nwant %s", path, got, want)
		}
	}
}
