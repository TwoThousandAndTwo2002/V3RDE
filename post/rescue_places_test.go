package post

// X50d tests: the store of every place found, used before the web search.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAreaKey(t *testing.T) {
	lat, lng := 39.7412, -104.9903
	if k := AreaKey(" 80219 ", &lat, &lng); k != "zip:80219" {
		t.Fatal(k)
	}
	if k := AreaKey("", &lat, &lng); k != "pt:39.7,-105.0" {
		t.Fatal(k)
	}
	if k := AreaKey("", nil, &lng); k != "" {
		t.Fatal(k)
	}
}

func TestPlaceStoreKeepsAddsAndAges(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenPlaces(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 1, 6, 0, 0, 0, time.UTC)
	a := Place{Name: "Hope Shelter", Address: "5 Pine Rd", Tel: "tel:1", Directions: "https://x", Mailto: "mailto:y", Source: SourceWebSearch}
	b := Place{Name: "Westside Pantry", Address: "6 Oak Ave"}
	if err := s.Add("zip:80219", []Place{a, b, {Name: " "}}, SourceWebSearch, now); err != nil {
		t.Fatal(err)
	}
	// one copy of a place (name and address, any case); the links of one rescue are not kept
	if err := s.Add("zip:80219", []Place{{Name: "HOPE SHELTER", Address: "5 pine rd"}}, "directory", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	got := s.Get("zip:80219", now.Add(2*time.Hour), PlaceMaxAge)
	if len(got) != 2 || got[0].Name != "HOPE SHELTER" || got[1].Tel != "" || got[1].Directions != "" || got[1].Mailto != "" {
		t.Fatalf("%+v", got)
	}
	if s.Get("zip:10001", now, PlaceMaxAge) != nil || s.Get("", now, PlaceMaxAge) != nil {
		t.Fatal("another area")
	}
	if g := s.Get("zip:80219", now.Add(PlaceMaxAge+2*time.Hour), PlaceMaxAge); len(g) != 0 {
		t.Fatalf("old places are not used: %+v", g)
	}
	// the file keeps them (a restart)
	s2, err := OpenPlaces(dir)
	if err != nil || s2.Count() != 2 {
		t.Fatalf("%v %d", err, s2.Count())
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "places.json")); strings.Contains(string(b), "tel:1") {
		t.Fatal("a link was stored")
	}
	// the newest MaxPlacesPerArea only
	var many []Place
	for i := 0; i < MaxPlacesPerArea+5; i++ {
		many = append(many, Place{Name: "P" + string(rune('a'+i%26)) + string(rune('a'+i/26)), Address: "x"})
	}
	_ = s.Add("zip:1", many, "directory", now)
	if n := len(s.Get("zip:1", now, PlaceMaxAge)); n != MaxPlacesPerArea {
		t.Fatal(n)
	}
	var nilStore *PlaceStore
	if nilStore.Get("zip:1", now, PlaceMaxAge) != nil || nilStore.Add("zip:1", many, "x", now) != nil || nilStore.Count() != 0 {
		t.Fatal("a nil store is no store")
	}
}

// X50d: the second rescue in an area the directory cannot serve uses the stored places: no web search call.
func TestRescueUsesTheStoreBeforeTheWebSearch(t *testing.T) {
	m := &museTools{draft: `{"shelf_stable": false, "reason": "fresh bagels"}`, search: webPlaces}
	e := jobEnv(t, m, 0, `{"success":true,"count":0,"resources":[]}`)
	defer e.stop()
	ps, err := OpenPlaces(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e.cfg.PlaceStore = ps
	first := waitRescue(t, e, startRescue(t, e, "l_0000000000000011").RescueID)
	if first.FoundBy != SourceWebSearch || len(m.searchPrompts()) != 1 || ps.Count() == 0 {
		t.Fatalf("first: %+v, %d searches, %d stored", first, len(m.searchPrompts()), ps.Count())
	}
	// a new rescue of the same area (a new store of rescues: the listing gets a second rescue)
	st, err := OpenRescues(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e.cfg.Store = st
	second := waitRescue(t, e, startRescue(t, e, "l_0000000000000011").RescueID)
	if second.Status != StatusDone || second.FoundBy != FoundByStore || second.Attribution != WebAttribution || len(second.Places) == 0 {
		t.Fatalf("second: %+v", second)
	}
	if n := len(m.searchPrompts()); n != 1 {
		t.Fatalf("the web search ran again: %d searches", n)
	}
	if !strings.Contains(second.Places[0].Directions, "origin=12+Main+St") {
		t.Fatalf("the links are made for this rescue: %+v", second.Places[0])
	}
	if ev := rescueEvents(t, e); !strings.Contains(ev, `"found_by":"place_store"`) {
		t.Fatalf("events %s", ev)
	}
}

// X50d review: the directory fails and the store answers: the events keep the directory's error; a stored place has
// no distance (it was measured from another point); places of both sources give both credits.
func TestRescueStoreAnswersWhenTheDirectoryFails(t *testing.T) {
	m := &museTools{draft: `{"shelf_stable": false, "reason": "fresh bagels"}`, search: webPlaces}
	e := jobEnv(t, m, 500, ``)
	defer e.stop()
	ps, err := OpenPlaces(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := e.cfg.now()
	if err := ps.Add("zip:80219", []Place{
		{Name: "Hope Shelter", Type: "shelter", Address: "5 Pine Rd, Denver, CO 80219", DistanceMiles: 0.4, Source: SourceWebSearch},
		{Name: "Westside Pantry", Type: "food_pantry", Address: "10 Main St, Denver, CO 80219", DistanceMiles: 1.2},
	}, "directory", now); err != nil {
		t.Fatal(err)
	}
	e.cfg.PlaceStore = ps
	res := waitRescue(t, e, startRescue(t, e, "l_0000000000000011").RescueID)
	if res.Status != StatusDone || res.FoundBy != FoundByStore || len(res.Places) == 0 {
		t.Fatalf("%+v", res)
	}
	for _, p := range res.Places {
		if p.DistanceMiles != 0 {
			t.Fatalf("a stored distance was shown: %+v", p)
		}
	}
	if len(m.searchPrompts()) != 0 {
		t.Fatal("the web search ran")
	}
	if ev := rescueEvents(t, e); !strings.Contains(ev, `"directory_error"`) || !strings.Contains(ev, `"found_by":"place_store"`) {
		t.Fatalf("events %s", ev)
	}
	if res.Attribution != DirectoryAttribution+"; "+WebAttribution {
		t.Fatalf("attribution %q", res.Attribution)
	}
}
