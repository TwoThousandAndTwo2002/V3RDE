package post

// R3 tests: the bulk rule (email) and the drop-off with directions links.

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestPickupAddress(t *testing.T) {
	for _, c := range []struct{ in, addr, zip string }{
		{"12 Main St, Denver, CO 80219, front desk\nring twice", "12 Main St, Denver, CO 80219", "80219"},
		{"12345 Oak Rd, Denver CO 80219-1234 (back door)", "12345 Oak Rd, Denver CO 80219-1234", "80219"},
		{"12 Main St, Denver, CO 80219, gate code 54321", "12 Main St, Denver, CO 80219", "80219"}, // review
		{"Denver CO 80219 80220", "Denver CO 80219", "80219"},
		{"12 elm st, denver, co 80219", "12 elm st, denver, co 80219", "80219"}, // lowercase after a comma
		{"NY 10001, CO 00000", "NY 10001", "10001"},                             // 00000 is skipped
		{"12345 W Colfax Ave, Lakewood CO", "", ""},                             // review: a house number
		{"Suite 10001, Denver CO", "", ""},                                      // review: a suite number
		{"Front desk, Hotel Blue, 80202", "", ""},                               // no state code
		{"Pickup in 80219", "", ""},                                             // "in" is a word, not Indiana
		{"Unit ZZ 80219", "", ""},                                               // not a state code
		{"Lobby. Call 303-555-01234 first", "", ""},
		{"Front desk before 6 pm", "", ""},
		{"CO 123456", "", ""},
		{"CO \uff18\uff10\uff12\uff11\uff19", "", ""}, // full-width digits are not a ZIP
		{"", "", ""},
	} {
		a, z := pickupAddress(c.in)
		if a != c.addr || z != c.zip {
			t.Fatalf("%q: got %q %q, want %q %q", c.in, a, z, c.addr, c.zip)
		}
	}
}

func TestDirectionsLink(t *testing.T) {
	p := Place{Name: "City Food Pantry", Address: "2 Oak Ave, Town, NY, 10001"}
	got := directionsLink("12 Main St, Denver, CO 80219", p)
	u, err := url.Parse(got)
	if err != nil || u.Scheme != "https" || u.Host != "www.google.com" || u.Path != "/maps/dir/" {
		t.Fatalf("%s %v", got, err)
	}
	q := u.Query()
	if q.Get("api") != "1" || q.Get("origin") != "12 Main St, Denver, CO 80219" ||
		q.Get("destination") != "City Food Pantry, 2 Oak Ave, Town, NY, 10001" {
		t.Fatalf("query %v", q)
	}
	if strings.Contains(directionsLink("", p), "origin=") {
		t.Fatal("an empty origin must be left out (Maps starts at the device)")
	}
	if got := directionsLink("", Place{Name: "Hope Kitchen"}); !strings.Contains(got, "destination=Hope+Kitchen") {
		t.Fatal(got)
	}
	if directionsLink("x", Place{}) != "" {
		t.Fatal("no name, no address: no link")
	}
	for _, r := range []string{"é", "東", "🍞"} { // 2-, 3- and 4-byte runes (review)
		l := directionsLink(strings.Repeat(r, 5000), Place{Name: strings.Repeat(r, 5000), Address: strings.Repeat(r, 5000)})
		if len(l) > 2048 || !strings.Contains(l, "origin=") {
			t.Fatalf("%s: %d characters", r, len(l))
		}
		if _, err := url.Parse(l); err != nil {
			t.Fatal(err)
		}
	}
}

func TestIsBulk(t *testing.T) {
	bulkDetails := &ListingDetails{ServingType: "bulk"}
	for _, c := range []struct {
		l    Listing
		src  string
		want bool
	}{
		{Listing{NumberOfServing: 30}, "", true},
		{Listing{NumberOfServing: 29}, ScanVision, false},
		{Listing{NumberOfServing: 2, Details: bulkDetails}, ScanVision, true},
		{Listing{NumberOfServing: 2, Details: bulkDetails}, ScanBarcode, false}, // a fixed value
		{Listing{NumberOfServing: 2, Details: bulkDetails}, "", false},
		{Listing{NumberOfServing: 2, Details: &ListingDetails{ServingType: "individual"}}, ScanVision, false},
	} {
		if got := isBulk(c.l, c.src, 30); got != c.want {
			t.Fatalf("%+v %q: %v", c.l, c.src, got)
		}
	}
}

// A small listing: the ZIP comes from the pickup address (no body needed), Muse
// gets the drop-off prompt, each place has a directions link from the pickup
// address and no mailto, /send refuses it, and the pickup text is not logged.
func TestRescueDropoff(t *testing.T) {
	e := newRS(t, `{"shelf_stable": false, "reason": "fresh bagels go stale"}`, 200, true)
	defer e.stop()
	e.cfg.MinServings = 30
	w := do(e.h, jsonReq("POST", "/api/rescue/l_0000000000000011", ``, "u1"))
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var res Rescue
	json.Unmarshal(w.Body.Bytes(), &res)
	if res.Mode != ModeDropoff || res.Kind != KindMealProgram || len(res.Places) != 3 || res.Draft != (Draft{}) {
		t.Fatalf("%+v", res)
	}
	if !strings.Contains(e.dir.query, "zip=80219") {
		t.Fatalf("directory query %s", e.dir.query)
	}
	for _, p := range res.Places {
		u, _ := url.Parse(p.Directions)
		if p.Mailto != "" || u == nil || u.Query().Get("origin") != "12 Main St, Denver, CO 80219" ||
			!strings.Contains(u.Query().Get("destination"), p.Name) {
			t.Fatalf("place %+v", p)
		}
	}
	text := (*e.muse)[0]["input"].([]any)[0].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if text != fmt.Sprintf(DropoffPrompt, "Bagels", 6) {
		t.Fatalf("prompt\n%s", text)
	}
	s := do(e.h, jsonReq("POST", "/api/rescue/"+res.RescueID+"/send", `{"place_id":"a2","to_email":"help@pantry.example.org"}`, "u1"))
	if s.Code != 409 || !strings.Contains(s.Body.String(), `"dropoff"`) || len(*e.sent) != 0 {
		t.Fatalf("send %d %s (sent %d)", s.Code, s.Body, len(*e.sent))
	}
	b, _ := os.ReadFile(e.cfg.Events.Path(e.now))
	if !strings.Contains(string(b), `"mode":"dropoff"`) || !strings.Contains(string(b), `"location_from":"pickup"`) ||
		strings.Contains(string(b), "Main St") || strings.Contains(string(b), "front desk") {
		t.Fatalf("events %s", b)
	}
}

// A bulk listing keeps the X35b email flow (prompt byte for byte, mailto),
// and its places also get directions links; the body's ZIP is used when the
// listing has no pickup ZIP.
func TestRescueBulkKeepsTheEmail(t *testing.T) {
	e := newRS(t, stableDraft, 200, false)
	defer e.stop()
	e.cfg.MinServings = 24
	w := do(e.h, jsonReq("POST", "/api/rescue/l_000000000000000a", `{"zip":"10001"}`, "u1"))
	var res Rescue
	json.Unmarshal(w.Body.Bytes(), &res)
	if w.Code != 200 || res.Mode != ModeEmail || res.Draft.Subject == "" || res.Places[1].Mailto == "" ||
		!strings.HasPrefix(res.Places[1].Directions, "https://www.google.com/maps/dir/?api=1&destination=") ||
		strings.Contains(res.Places[1].Directions, "origin=") {
		t.Fatalf("%d %+v", w.Code, res)
	}
	text := (*e.muse)[0]["input"].([]any)[0].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if text != fmt.Sprintf(RescuePrompt, "Canned beans", 24) {
		t.Fatalf("bulk prompt changed\n%s", text)
	}
	if !strings.Contains(e.dir.query, "zip=10001") {
		t.Fatalf("directory query %s", e.dir.query)
	}
	b, _ := os.ReadFile(e.cfg.Events.Path(e.now))
	if !strings.Contains(string(b), `"mode":"email"`) || !strings.Contains(string(b), `"location_from":"request"`) {
		t.Fatalf("events %s", b)
	}
}

// No pickup ZIP and no body: 400; a half lat/lng is still a 400.
func TestRescueNeedsALocation(t *testing.T) {
	e := newRS(t, stableDraft, 200, false)
	defer e.stop()
	for _, body := range []string{``, `{}`, `{"lat":40}`} {
		w := do(e.h, jsonReq("POST", "/api/rescue/l_000000000000000a", body, "u1"))
		if w.Code != 400 {
			t.Fatalf("%q: %d %s", body, w.Code, w.Body)
		}
	}
	if len(*e.muse) != 0 {
		t.Fatal("Muse was called without a location")
	}
}

// X49j: the address the giver chose gives the ZIP and the directions origin (the pickup text is the instructions).
func TestRescueUsesThePickupAddress(t *testing.T) {
	e := newRS(t, `{"shelf_stable": true, "reason": "packaged"}`, 200, true)
	defer e.stop()
	e.cfg.MinServings = 30
	w := do(e.h, jsonReq("POST", "/api/rescue/l_0000000000000012", ``, "u1"))
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var res Rescue
	json.Unmarshal(w.Body.Bytes(), &res)
	if !strings.Contains(e.dir.query, "zip=80302") || len(res.Places) == 0 {
		t.Fatalf("directory query %s, %d places", e.dir.query, len(res.Places))
	}
	u, _ := url.Parse(res.Places[0].Directions)
	if u == nil || u.Query().Get("origin") != "40 Elm St, Boulder, CO 80302, USA" {
		t.Fatalf("directions %s", res.Places[0].Directions)
	}
	b, _ := os.ReadFile(e.cfg.Events.Path(e.now))
	if !strings.Contains(string(b), `"location_from":"pickup"`) || strings.Contains(string(b), "Elm St") || strings.Contains(string(b), "ask for Sam") {
		t.Fatalf("events %s", b)
	}
}

// X49j review: an address with no ZIP: rescue searches at its point (not at the ZIP of the instructions), and the
// directions start from the address.
func TestRescueAddressWithoutZipUsesItsPoint(t *testing.T) {
	e := newRS(t, `{"shelf_stable": true, "reason": "packaged"}`, 200, true)
	defer e.stop()
	e.cfg.MinServings = 30
	w := do(e.h, jsonReq("POST", "/api/rescue/l_0000000000000013", ``, "u1"))
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var res Rescue
	json.Unmarshal(w.Body.Bytes(), &res)
	if strings.Contains(e.dir.query, "80219") || !strings.Contains(e.dir.query, "lat=40.02") || !strings.Contains(e.dir.query, "lng=-105.28") {
		t.Fatalf("directory query %s", e.dir.query)
	}
	u, _ := url.Parse(res.Places[0].Directions)
	if u == nil || u.Query().Get("origin") != "1 Pearl St, Boulder, CO, USA" {
		t.Fatalf("directions %s", res.Places[0].Directions)
	}
	b, _ := os.ReadFile(e.cfg.Events.Path(e.now))
	if !strings.Contains(string(b), `"location_from":"pickup"`) {
		t.Fatalf("events %s", b)
	}
}
