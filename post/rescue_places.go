package post

// X50d (T9 Nov 1: "ideally we should be accruing a set of all food recovery locations so in the future we can reduce
// our ai bill by not having to do web searches all the time"): every place a rescue finds (the food directory or the
// web search) goes into one store on the server, by area. A later rescue in the same area that the directory cannot
// serve takes the stored places first; the web search (a paid model call) runs only when the store has none that are
// fresh enough. The store is <data>/places.json next to rescues.json.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// FoundByStore is Rescue.FoundBy (and the events detail) when the places came from the store.
const FoundByStore = "place_store"

// PlaceMaxAge is how long a stored place is used before a new search (opening hours and programs change).
const PlaceMaxAge = 30 * 24 * time.Hour

// MaxPlacesPerArea keeps an area's list small (the newest first). A stored place has no distance (it was measured
// from the point of the search that found it).
const MaxPlacesPerArea = 40

// StoredPlace is a place with the area it was found for and when.
type StoredPlace struct {
	Place
	FoundAt string `json:"found_at"` // RFC 3339, UTC
	FoundBy string `json:"found_by"` // "directory" or SourceWebSearch
}

// PlaceStore keeps the places by area key.
type PlaceStore struct {
	path  string
	mu    sync.Mutex
	areas map[string][]StoredPlace
}

// OpenPlaces opens <dir>/places.json (a missing file is an empty store).
func OpenPlaces(dir string) (*PlaceStore, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	s := &PlaceStore{path: filepath.Join(dir, "places.json"), areas: map[string][]StoredPlace{}}
	b, err := os.ReadFile(s.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, err
	case len(b) > 0:
		if err := json.Unmarshal(b, &s.areas); err != nil {
			return nil, fmt.Errorf("%s: %w", s.path, err)
		}
	}
	return s, nil
}

// AreaKey is the area of a search: the ZIP code when there is one, else the point rounded to 0.1 degree (about
// 11 km north-south), so near searches share their places. "" when there is neither.
func AreaKey(zip string, lat, lng *float64) string {
	if zip = strings.TrimSpace(zip); zip != "" {
		return "zip:" + zip
	}
	if lat == nil || lng == nil {
		return ""
	}
	return fmt.Sprintf("pt:%.1f,%.1f", *lat, *lng)
}

func placeKey(p Place) string {
	return strings.ToLower(strings.TrimSpace(p.Name)) + "|" + strings.ToLower(strings.TrimSpace(p.Address))
}

// Get returns the places of area key found less than maxAge before now (newest first); nil when none.
func (s *PlaceStore) Get(key string, now time.Time, maxAge time.Duration) []Place {
	if s == nil || key == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Place
	for _, sp := range s.areas[key] {
		at, err := time.Parse(time.RFC3339, sp.FoundAt)
		if err != nil || now.Sub(at) > maxAge {
			continue
		}
		out = append(out, sp.Place)
	}
	return out
}

// Add puts the places found for area key (one copy of each place: name and address), keeps the newest
// MaxPlacesPerArea, and writes the file. The links of one rescue (tel, mailto, directions) are not kept.
func (s *PlaceStore) Add(key string, places []Place, foundBy string, now time.Time) error {
	if s == nil || key == "" || len(places) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	at := now.UTC().Format(time.RFC3339)
	byKey := map[string]StoredPlace{}
	for _, sp := range s.areas[key] {
		byKey[placeKey(sp.Place)] = sp
	}
	for _, p := range places {
		if strings.TrimSpace(p.Name) == "" {
			continue
		}
		p.Tel, p.Mailto, p.Directions = "", "", ""
		p.DistanceMiles = 0 // X50d review: measured from another search's point: not shown for this one
		byKey[placeKey(p)] = StoredPlace{Place: p, FoundAt: at, FoundBy: foundBy}
	}
	list := make([]StoredPlace, 0, len(byKey))
	for _, sp := range byKey {
		list = append(list, sp)
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].FoundAt != list[j].FoundAt {
			return list[i].FoundAt > list[j].FoundAt
		}
		return placeKey(list[i].Place) < placeKey(list[j].Place)
	})
	if len(list) > MaxPlacesPerArea {
		list = list[:MaxPlacesPerArea]
	}
	s.areas[key] = list
	b, err := json.MarshalIndent(s.areas, "", " ")
	if err != nil {
		return err
	}
	return WriteFileSync(s.path, b, 0o600)
}

// Count is the number of stored places (all areas).
func (s *PlaceStore) Count() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, l := range s.areas {
		n += len(l)
	}
	return n
}
