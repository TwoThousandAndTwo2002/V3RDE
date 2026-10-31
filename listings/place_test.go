package listings

// X49e tests: the place, the distance list, "Your pickup" and release.

import (
	"encoding/json"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestParsePlace(t *testing.T) {
	if p, err := ParsePlace("", " ", ""); p != nil || err != nil {
		t.Fatalf("empty: %v %v", p, err)
	}
	if p, err := ParsePlace("40.75", "-73.99", ""); err != nil || p.Lat != 40.75 || p.Lng != -73.99 {
		t.Fatalf("lat lng: %v %v", p, err)
	}
	if p, err := ParsePlace("", "", " 10001 "); err != nil || math.Abs(p.Lat-40.750649) > 1e-9 || math.Abs(p.Lng+73.997298) > 1e-9 {
		t.Fatalf("zip: %v %v", p, err)
	}
	for _, c := range [][3]string{{"40", "", ""}, {"", "-73", ""}, {"91", "0", ""}, {"0", "181", ""}, {"NaN", "0", ""},
		{"x", "1", ""}, {"40", "-73", "10001"}, {"", "", "1000"}, {"", "", "10001-1234"}, {"", "", "abcde"}} {
		if _, err := ParsePlace(c[0], c[1], c[2]); err != ErrPlace {
			t.Errorf("%q: %v", c, err)
		}
	}
	if _, err := ParsePlace("", "", "00000"); err != ErrZip {
		t.Fatalf("unknown zip: %v", err)
	}
}

func TestZipTableIsTheWholeGazetteer(t *testing.T) {
	if n := ZipCount(); n != 33791 {
		t.Fatalf("zips %d, want 33791 (2024 Gazetteer)", n)
	}
	if p, ok := ZipPlace("94103"); !ok || p.Lat != 37.772983 || p.Lng != -122.411343 {
		t.Fatalf("94103: %v %v", p, ok)
	}
}

func TestMilesKnownDistances(t *testing.T) {
	// one degree of latitude on the sphere is 2*pi*R/360
	if d, want := Miles(Place{0, 0}, Place{1, 0}), 2*math.Pi*EarthMiles/360; math.Abs(d-want) > 1e-9 || math.Abs(d-69.09) > 0.01 {
		t.Fatalf("1 degree: %v", d)
	}
	if d := Miles(Place{40, -74}, Place{40, -74}); d != 0 {
		t.Fatalf("same point: %v", d)
	}
	// a quarter of the equator
	if d, want := Miles(Place{0, 0}, Place{0, 90}), math.Pi*EarthMiles/2; math.Abs(d-want) > 1e-6 {
		t.Fatalf("quarter: %v", d)
	}
}

func TestSnapIsAGridOfAHundredthOfAMile(t *testing.T) {
	half := GridMiles / 2 * math.Sqrt2 // the farthest a point moves: half a cell, on the diagonal
	for _, p := range []Place{{40.750649, -73.997298}, {0.0001, 0.0001}, {64.8378, -147.7164}, {-33.8688, 151.2093}, {89.99999, 10}} {
		s := Snap(p)
		if d := Miles(p, s); d > half+1e-6 {
			t.Errorf("%v -> %v moved %.5f mi", p, s, d)
		}
		if again := Snap(s); Miles(again, s) > 1e-6 {
			t.Errorf("snap twice moved %v -> %v", s, again)
		}
	}
	// two points 0.003 mi apart land on the same grid point or next ones, never farther
	a, b := Place{40.75, -74}, Place{40.75 + 0.003/69.09, -74}
	if d := Miles(Snap(a), Snap(b)); d > GridMiles+1e-6 {
		t.Fatalf("neighbors %.4f mi apart", d)
	}
}

// createAt makes a listing of user at a lat/lng (or a zip when lng is "").
func (e *env) createAt(t *testing.T, user, lat, lng string) Listing {
	t.Helper()
	f := okFields()
	delete(f, "lat")
	delete(f, "lng")
	if lng == "" {
		f["zip"] = lat
	} else {
		f["lat"], f["lng"] = lat, lng
	}
	w, body := call(e.h, form(t, user, f, jpeg, "image/jpeg"))
	if w.Code != 201 {
		t.Fatalf("create at %s,%s: %d %v", lat, lng, w.Code, body)
	}
	var l Listing
	json.Unmarshal(w.Body.Bytes(), &l)
	return l
}

func TestCreateNeedsAPlaceAndKeepsItHidden(t *testing.T) {
	e := newEnv(t, nil)
	for _, c := range []map[string]string{{}, {"lat": "40"}, {"zip": "1234"}, {"zip": "00000"}, {"lat": "40", "lng": "-74", "zip": "10001"}} {
		f := okFields()
		delete(f, "lat")
		delete(f, "lng")
		for k, v := range c {
			f[k] = v
		}
		w, body := call(e.h, form(t, "u_donor", f, jpeg, "image/jpeg"))
		if w.Code != 400 || !strings.Contains(body["error"].(string), "zip") {
			t.Errorf("%v: %d %v", c, w.Code, body)
		}
	}
	l := e.createAt(t, "u_donor", "10001", "")
	w, _ := call(e.h, get("u_r", "/api/listings?zip=10001"))
	if strings.Contains(w.Body.String(), `"place"`) || strings.Contains(w.Body.String(), `"lat"`) {
		t.Fatalf("the place left the server: %s", w.Body)
	}
	b, _ := os.ReadFile(filepath.Join(e.dir, "listings.json"))
	var file map[string]stored
	json.Unmarshal(b, &file)
	p := file[l.ID].Place
	z, _ := ZipPlace("10001")
	if p == nil || *p != Snap(z) || file[l.ID].DistanceMi != nil {
		t.Fatalf("data file place %v distance %v", p, file[l.ID].DistanceMi)
	}
}

func TestNearIsNearestFirstUpTo25Miles(t *testing.T) {
	e := newEnv(t, nil)
	// along a meridian: 1 degree of latitude = 69.09 mi
	far := e.createAt(t, "u_donor", "40.4", "-74")   // 24.2 mi from 40.75
	near := e.createAt(t, "u_donor", "40.76", "-74") // 0.69 mi
	e.tick(time.Second)
	mid := e.createAt(t, "u_donor", "40.7", "-74")   // 3.45 mi
	e.createAt(t, "u_donor", "40.3", "-74")          // 31.1 mi: not listed
	e.createAt(t, "u_me", "40.75", "-74")            // the caller's own: the app hides it, the server lists it
	old := e.createAt(t, "u_donor", "40.751", "-74") // expires first
	e.st.items[old.ID] = func() stored { r := e.st.items[old.ID]; r.ExpiresAt = "2026-10-28T18:00:01Z"; return r }()
	r := e.st.items[mid.ID]
	r.Place = nil // a listing from before X49e has no place: not in a place list
	noPlace := mid.ID
	e.st.items[noPlace] = r
	mid = e.createAt(t, "u_donor", "40.7", "-74")
	e.tick(time.Second)

	w, body := call(e.h, get("u_me", "/api/listings?lat=40.75&lng=-74"))
	if w.Code != 200 || body["next"] != "" {
		t.Fatalf("%d %v", w.Code, body)
	}
	var ids []string
	var dist []float64
	for _, it := range body["items"].([]any) {
		m := it.(map[string]any)
		ids = append(ids, m["id"].(string))
		dist = append(dist, m["distance_mi"].(float64))
	}
	if len(ids) != 4 || ids[1] != near.ID || ids[2] != mid.ID || ids[3] != far.ID {
		t.Fatalf("order %v %v", ids, dist)
	}
	// one decimal only (the review: 0.01 mi from made-up places would find the giver's point)
	for k, want := range []float64{0, 0.7, 3.5, 24.2} {
		if dist[k] != want {
			t.Errorf("distance %d: %v want %v", k, dist[k], want)
		}
	}
	// pages: the cursor is the key of the last listing of the page before
	w, body = call(e.h, get("u_me", "/api/listings?lat=40.75&lng=-74&limit=3"))
	next, _ := body["next"].(string)
	if len(body["items"].([]any)) != 3 || !strings.HasPrefix(next, "3.5~") {
		t.Fatalf("page 1: %v (the cursor has the shown distance, never the exact one)", body)
	}
	w, body = call(e.h, get("u_me", "/api/listings?lat=40.75&lng=-74&limit=3&after="+url.QueryEscape(next)))
	if len(body["items"].([]any)) != 1 || body["next"] != "" || body["items"].([]any)[0].(map[string]any)["id"] != far.ID {
		t.Fatalf("page 2: %v", body)
	}
	for _, q := range []string{"lat=40.75&lng=-74&after=l_x", "lat=40.75&lng=-74&after=3", "lat=40.75&lng=-74&after=zz~1~l_a", "lat=40.75&lng=-74&after=NaN~1~l_a",
		"lat=40.75&lng=-74&after=1~x~l_a", "lat=40.75", "zip=1"} {
		if w, _ := call(e.h, get("u_me", "/api/listings?"+q)); w.Code != 400 {
			t.Errorf("%s: %d", q, w.Code)
		}
	}
	// no place: the list as before, newest first, no distance
	w, _ = call(e.h, get("u_me", "/api/listings"))
	if strings.Contains(w.Body.String(), "distance_mi") {
		t.Fatalf("distance without a place: %s", w.Body)
	}
}

func TestGrabbedAndRelease(t *testing.T) {
	e := newEnv(t, nil)
	l := e.createAt(t, "u_donor", "40.76", "-74")
	other := e.createAt(t, "u_donor", "40.77", "-74")
	if w, _ := call(e.h, post("u_r", "/api/listings/"+l.ID+"/grab")); w.Code != 200 {
		t.Fatalf("grab %d", w.Code)
	}
	e.tick(time.Minute)
	call(e.h, post("u_s", "/api/listings/"+other.ID+"/grab"))
	w, body := call(e.h, get("u_r", "/api/listings/grabbed?lat=40.75&lng=-74"))
	items := body["items"].([]any)
	if w.Code != 200 || len(items) != 1 || items[0].(map[string]any)["id"] != l.ID || items[0].(map[string]any)["distance_mi"] != 0.7 {
		t.Fatalf("grabbed: %d %v", w.Code, body)
	}
	if w, body = call(e.h, get("u_nobody", "/api/listings/grabbed")); strings.TrimSpace(w.Body.String()) != `{"items":[]}` {
		t.Fatalf("none: %s", w.Body)
	}
	// the listing is not in the list for others while it is grabbed
	if w, _ := call(e.h, get("u_t", "/api/listings?lat=40.75&lng=-74")); strings.Contains(w.Body.String(), l.ID) {
		t.Fatal("a grabbed listing is listed")
	}
	for _, c := range []struct {
		user, id string
		code     int
		msg      string
	}{
		{"u_r", "l_0000000000000000", 404, "listing not found"},
		{"u_s", l.ID, 409, "you did not grab this listing"},
		{"u_donor", l.ID, 409, "you did not grab this listing"},
		{"", l.ID, 401, "not logged in"},
	} {
		if w, body := call(e.h, post(c.user, "/api/listings/"+c.id+"/release")); w.Code != c.code || body["error"] != c.msg {
			t.Errorf("%s %s: %d %v", c.user, c.id, w.Code, body)
		}
	}
	w, body = call(e.h, post("u_r", "/api/listings/"+l.ID+"/release"))
	if w.Code != 200 || body["status"] != "active" || body["collected_by"] != nil || body["collected_at"] != nil || body["expires_at"] != l.ExpiresAt {
		t.Fatalf("release: %d %v", w.Code, body)
	}
	if w, body = call(e.h, post("u_r", "/api/listings/"+l.ID+"/release")); w.Code != 409 {
		t.Fatalf("release two times: %d %v", w.Code, body)
	}
	if w, _ := call(e.h, get("u_t", "/api/listings?lat=40.75&lng=-74")); !strings.Contains(w.Body.String(), l.ID) {
		t.Fatal("a released listing is back in the list")
	}
	if w, _ := call(e.h, post("u_t", "/api/listings/"+l.ID+"/grab")); w.Code != 200 {
		t.Fatal("someone else can grab it again")
	}
	// the timer ended: the grab is final, and it leaves "Your pickup"
	*e.clock = time.Date(2026, 10, 28, 18, 30, 0, 0, time.UTC)
	if w, body := call(e.h, post("u_t", "/api/listings/"+l.ID+"/release")); w.Code != 409 || body["error"] != "listing expired" {
		t.Fatalf("after the timer: %d %v", w.Code, body)
	}
	if _, body := call(e.h, get("u_t", "/api/listings/grabbed")); len(body["items"].([]any)) != 0 {
		t.Fatalf("an ended grab is listed: %v", body)
	}
}

func TestReleaseMemoryRestoredWhenSaveFails(t *testing.T) {
	e := newEnv(t, nil)
	l := e.create(t, "u_donor")
	if _, err := e.st.Grab(l.ID, "u_r"); err != nil {
		t.Fatal(err)
	}
	os.Mkdir(filepath.Join(e.dir, "listings.json.tmp"), 0o700)
	w, body := call(e.h, post("u_r", "/api/listings/"+l.ID+"/release"))
	if w.Code != 500 || body["error"] != "could not release" {
		t.Fatalf("%d %v", w.Code, body)
	}
	if got := e.st.Mine("u_donor")[0]; got.Status != "collected" {
		t.Fatalf("memory changed: %+v", got)
	}
}

func TestReleaseReachesFanoutAfterTheClaim(t *testing.T) {
	f := newFanout(t, 200)
	h, done := hookFor(f, 0)
	e := newEnv(t, h)
	l := e.create(t, "u_donor")
	f.mu.Lock() // the claim waits at fan-out until the release is queued
	if w, _ := call(e.h, post("u_r", "/api/listings/"+l.ID+"/grab")); w.Code != 200 {
		t.Fatal("grab")
	}
	if w, _ := call(e.h, post("u_r", "/api/listings/"+l.ID+"/release")); w.Code != 200 {
		t.Fatal("release")
	}
	time.Sleep(50 * time.Millisecond)
	f.mu.Unlock()
	wait(t, done)
	wait(t, done)
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.bodies) != 2 || !strings.Contains(f.bodies[0], `"claimed"`) || !strings.Contains(f.bodies[1], `"released"`) {
		t.Fatalf("order %v", f.bodies)
	}
}

// The review: a grab between two pages must not make the next page skip a
// listing (a count cursor did), and a new nearer listing must not repeat one.
func TestNearPagesDoNotSkipWhenTheListChanges(t *testing.T) {
	e := newEnv(t, nil)
	var ids []string
	for i := 0; i < 6; i++ { // 0.7, 1.4, ... mi along a meridian
		ids = append(ids, e.createAt(t, "u_donor", strconv.FormatFloat(40.75+0.01*float64(i+1), 'f', 4, 64), "-74").ID)
	}
	page := func(after string) ([]string, string) {
		_, body := call(e.h, get("u_r", "/api/listings?lat=40.75&lng=-74&limit=3&after="+url.QueryEscape(after)))
		var got []string
		for _, it := range body["items"].([]any) {
			got = append(got, it.(map[string]any)["id"].(string))
		}
		return got, body["next"].(string)
	}
	p1, next := page("")
	if strings.Join(p1, ",") != strings.Join(ids[:3], ",") {
		t.Fatalf("page 1 %v", p1)
	}
	if _, err := e.st.Grab(ids[1], "u_s"); err != nil { // someone grabs one already shown
		t.Fatal(err)
	}
	e.createAt(t, "u_donor", "40.7501", "-74") // and a nearer one is listed
	p2, next2 := page(next)
	if strings.Join(p2, ",") != strings.Join(ids[3:], ",") || next2 != "" {
		t.Fatalf("page 2 %v (want %v) next %q", p2, ids[3:], next2)
	}
}

// X49j: the pickup address the giver chose: kept, shown only to the owner and
// to the user who grabbed it (T9: the address and the pin after a grab).
func TestPickupAddressOnlyForOwnerAndGrabber(t *testing.T) {
	e := newEnv(t, nil)
	f := okFields()
	f["lat"], f["lng"] = "40.7600", "-74.0000"
	f["address"], f["address_zip"], f["pickup_text"] = "12 Main St, New York, NY 10001, USA", "10001", ""
	w, body := call(e.h, form(t, "u_donor", f, jpeg, "image/jpeg"))
	if w.Code != 201 || body["pickup_address"] == nil || body["pickup_text"] != "" {
		t.Fatalf("create with an address and no instructions: %d %v", w.Code, body)
	}
	id := body["id"].(string)
	a := body["pickup_address"].(map[string]any)
	if a["text"] != "12 Main St, New York, NY 10001, USA" || a["zip"] != "10001" || a["lat"] != 40.76 || a["lng"] != -74.0 {
		t.Fatalf("address %v", a)
	}
	// the review: the instructions go only with the address (a street in them would reach the feed)
	f2 := okFields()
	f2["lat"], f2["lng"], f2["address"], f2["pickup_text"] = "40.7601", "-74.0000", "9 Oak Ave, New York, NY 10001", "ring at 12 Main St"
	if w, body := call(e.h, form(t, "u_donor", f2, jpeg, "image/jpeg")); w.Code != 201 || body["pickup_text"] != "ring at 12 Main St" {
		t.Fatalf("the owner gets the instructions: %d %v", w.Code, body)
	}
	for _, path := range []string{"/api/listings", "/api/listings?lat=40.75&lng=-74"} {
		if w, _ := call(e.h, get("u_r", path)); strings.Contains(w.Body.String(), "ring at") || strings.Contains(w.Body.String(), "Oak Ave") {
			t.Fatalf("%s shows the instructions of a listing with an address: %s", path, w.Body)
		}
	}
	for _, path := range []string{"/api/listings", "/api/listings?lat=40.75&lng=-74"} {
		if w, _ := call(e.h, get("u_r", path)); strings.Contains(w.Body.String(), "pickup_address") || strings.Contains(w.Body.String(), "Main St") {
			t.Fatalf("%s shows the address before a grab: %s", path, w.Body)
		}
	}
	if w, _ := call(e.h, get("u_donor", "/api/listings/mine")); !strings.Contains(w.Body.String(), `"pickup_address"`) {
		t.Fatalf("the owner does not see the address: %s", w.Body)
	}
	w, body = call(e.h, post("u_r", "/api/listings/"+id+"/grab"))
	if w.Code != 200 || body["pickup_address"] == nil {
		t.Fatalf("grab answer without the address: %d %v", w.Code, body)
	}
	if w, _ := call(e.h, get("u_r", "/api/listings/grabbed")); !strings.Contains(w.Body.String(), `"pickup_address"`) {
		t.Fatalf("Your pickup without the address: %s", w.Body)
	}
	if w, _ := call(e.h, get("u_s", "/api/listings/grabbed")); strings.Contains(w.Body.String(), "Main St") {
		t.Fatalf("another user sees the address: %s", w.Body)
	}
	// the rules: the address needs lat and lng; a bad zip; too long; no address and no instructions
	for _, c := range []struct {
		set  map[string]string
		want string
	}{
		{map[string]string{"address": "x", "lat": "", "lng": "", "zip": "10001"}, "send lat and lng with the address"},
		{map[string]string{"address": "x", "address_zip": "1234"}, "address_zip must be a 5-digit US zip"},
		{map[string]string{"address": strings.Repeat("a", MaxAddress+1)}, "address must be 1-200 characters"},
		{map[string]string{"pickup_text": ""}, "pickup_text must be 1-200 characters"},
	} {
		f := okFields()
		for k, v := range c.set {
			if v == "" {
				delete(f, k)
			} else {
				f[k] = v
			}
		}
		if w, body := call(e.h, form(t, "u_donor", f, jpeg, "image/jpeg")); w.Code != 400 || body["error"] != c.want {
			t.Errorf("%v: %d %v", c.set, w.Code, body)
		}
	}
}

func TestConfigGivesTheMapsKeyToASession(t *testing.T) {
	e := newEnv(t, nil)
	e.h = New(Config{Store: e.st, Auth: fakeAuth{}, MapsKey: "k-123"})
	if w, _ := call(e.h, get("", "/api/listings/config")); w.Code != 401 {
		t.Fatalf("no session: %d", w.Code)
	}
	if w, body := call(e.h, get("u_r", "/api/listings/config")); w.Code != 200 || body["maps_key"] != "k-123" {
		t.Fatalf("config: %d %v", w.Code, body)
	}
}
