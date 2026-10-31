package listings

import (
	"cmp"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// stored is one record of the data file: the listing and its photo file.
// The file is a JSON object of id -> stored; this format is the one the
// data volume already holds.
type stored struct {
	Listing
	PhotoFile string `json:"photo_file"`
	// Place (X49e): the pickup place, snapped to GridMiles; only in the data
	// file (the app gets distance_mi). Records before X49e have none.
	Place *Place `json:"place,omitempty"`
	// Address (X49j): the chosen pickup address; only in the data file and in
	// the answers to the owner and to the user who grabbed it (forOwnerOrGrabber).
	Address *PickupAddress `json:"address,omitempty"`
}

// forEveryone (X49j review) is the listing of r as the feed shows it to every
// user: with a chosen address the pickup text (the instructions at the door)
// goes only with the address, after a grab, so a street said or typed into
// the instructions does not reach the feed.
func forEveryone(r stored) Listing {
	l := r.Listing
	if r.Address != nil {
		l.PickupText = ""
	}
	return l
}

// forOwnerOrGrabber (X49j) is the listing of r with its pickup address.
func forOwnerOrGrabber(r stored) Listing {
	l := r.Listing
	if r.Address != nil {
		a := *r.Address
		l.PickupAddress = &a
	}
	return l
}

// Store keeps the listings in memory and in <dir>/listings.json (written
// again as a whole after each change: a temp file, a sync, a rename), with
// the photos in <dir>/photos.
type Store struct {
	path   string
	photos string
	ttl    time.Duration
	mu     sync.Mutex
	items  map[string]stored
	now    func() time.Time
}

// Open loads dir, or makes it with an empty data file. ttl <= 0 is DefaultTTL.
func Open(dir string, ttl time.Duration) (*Store, error) {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	s := &Store{path: filepath.Join(dir, "listings.json"), photos: filepath.Join(dir, "photos"),
		ttl: ttl, items: map[string]stored{}, now: time.Now}
	if err := os.MkdirAll(s.photos, 0o750); err != nil {
		return nil, err
	}
	s.removeTemps()
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if len(b) > 0 {
		if err := json.Unmarshal(b, &s.items); err != nil {
			return nil, fmt.Errorf("%s: %w", s.path, err)
		}
	}
	return s, nil
}

// writeAtomic writes b to path through path+".tmp": a crash leaves the old
// file or the new one, never half of one. A failure removes the temp file,
// and its error names path, not the temp file (the X38b review: the error
// text can reach the caller).
func writeAtomic(path string, b []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
	if err != nil {
		return namePath(err, path)
	}
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
		return namePath(err, path)
	}
	return nil
}

// namePath makes a file error name path (an os.PathError or an
// os.LinkError of the rename); other errors stay as they are.
func namePath(err error, path string) error {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return &os.PathError{Op: pe.Op, Path: path, Err: pe.Err}
	}
	var le *os.LinkError
	if errors.As(err, &le) {
		return &os.PathError{Op: le.Op, Path: path, Err: le.Err}
	}
	return err
}

// removeTemps removes the temp files a crash during a write can leave.
func (s *Store) removeTemps() {
	os.Remove(s.path + ".tmp")
	if tmps, err := filepath.Glob(filepath.Join(s.photos, "*.tmp")); err == nil {
		for _, t := range tmps {
			os.Remove(t)
		}
	}
}

// saveLocked writes the data file; the caller holds mu.
func (s *Store) saveLocked() error {
	b, err := json.MarshalIndent(s.items, "", " ")
	if err != nil {
		return err
	}
	return writeAtomic(s.path, b, 0o600)
}

// newID is prefix + 16 random hex characters.
func newID(prefix string) string {
	var b [8]byte
	_, _ = rand.Read(b[:]) // never fails since Go 1.24 (docs/sources/X38a.md D119)
	return prefix + hex.EncodeToString(b[:])
}

// Create validates in, writes the photo, then the record. When the record
// cannot be written, the photo is removed again, so no file is left alone.
func (s *Store) Create(ownerID string, in Input) (Listing, error) {
	if err := in.Validate(); err != nil {
		return Listing{}, err
	}
	now := s.now().UTC()
	ttl := s.ttl
	if in.TTL > 0 { // X44h: a test account's own timer (the handler checked it)
		ttl = in.TTL
	}
	id := newID("l_")
	file := id + photoExt[in.Mime]
	photo := filepath.Join(s.photos, file)
	if err := writeAtomic(photo, in.Photo, 0o640); err != nil {
		return Listing{}, err
	}
	var place *Place
	if in.Place != nil {
		p := Snap(*in.Place)
		place = &p
	}
	var addr *PickupAddress
	if in.Address != nil {
		a := *in.Address
		addr = &a
	}
	r := stored{PhotoFile: file, Place: place, Address: addr, Listing: Listing{
		ID: id, OwnerID: ownerID, Title: in.Title, NumberOfServing: in.NumberOfServing,
		PhotoURL: "/api/listings/" + id + "/photo", PickupText: in.PickupText, Status: "active",
		CreatedAt: now.Format(time.RFC3339Nano), ExpiresAt: now.Add(ttl).Format(time.RFC3339Nano),
		RequestID: in.RequestID, Details: in.Details, PhotoCredit: in.PhotoCredit,
	}}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[id] = r
	if err := s.saveLocked(); err != nil {
		delete(s.items, id)
		os.Remove(photo)
		return Listing{}, err
	}
	return forOwnerOrGrabber(r), nil
}

// active: status "active" and not expired at now.
func active(r stored, now time.Time) bool {
	if r.Status != "active" {
		return false
	}
	exp, err := time.Parse(time.RFC3339Nano, r.ExpiresAt)
	return err == nil && now.Before(exp)
}

// newestFirstLocked is every record, newest first by the time of
// created_at; the id breaks a tie, so a page cursor is stable. The time is
// compared, not the text: RFC 3339 with nanoseconds drops the zeros at the
// end, so the text order is not the time order in one second (the X38b
// review: "...:57.52Z" sorted after "...:57.521569942Z"). A created_at that
// does not parse sorts as the oldest.
func (s *Store) newestFirstLocked() []stored {
	type keyed struct {
		at time.Time
		r  stored
	}
	all := make([]keyed, 0, len(s.items))
	for _, r := range s.items {
		at, _ := time.Parse(time.RFC3339Nano, r.CreatedAt)
		all = append(all, keyed{at, r})
	}
	slices.SortFunc(all, func(a, b keyed) int {
		if c := b.at.Compare(a.at); c != 0 {
			return c
		}
		return cmp.Compare(b.r.ID, a.r.ID)
	})
	out := make([]stored, len(all))
	for i, k := range all {
		out[i] = k.r
	}
	return out
}

// Active is one page of active listings after the cursor (a listing id; ""
// is the first page) and the next cursor ("" when there is no more).
// A limit <= 0 or > PageSize is PageSize.
func (s *Store) Active(after string, limit int) ([]Listing, string) {
	if limit <= 0 || limit > PageSize {
		limit = PageSize
	}
	now := s.now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.newestFirstLocked()
	if after != "" {
		i := slices.IndexFunc(all, func(r stored) bool { return r.ID == after })
		if i < 0 {
			return nil, "" // an unknown cursor: no page
		}
		all = all[i+1:]
	}
	var page []Listing
	for _, r := range all {
		if !active(r, now) {
			continue
		}
		if len(page) == limit {
			return page, page[len(page)-1].ID
		}
		page = append(page, forEveryone(r))
	}
	return page, ""
}

// withDistance is the listing of r with distance_mi from `from`, and the
// exact distance; ok is false when r has no place or is farther than
// MaxMiles. distance_mi is rounded to ShownMiles (X49e review: with 0.01 mi,
// three made-up places would find the giver's point; the app shows 0.1 mi).
func withDistance(r stored, from Place) (Listing, float64, bool) {
	if r.Place == nil {
		return Listing{}, 0, false
	}
	d := Miles(from, *r.Place)
	if d > MaxMiles {
		return Listing{}, 0, false
	}
	l := forEveryone(r)
	m := roundMiles(d)
	l.DistanceMi = &m
	return l, d, true
}

// nearKey is the order of a place list: nearest first by the distance the
// app gets (ShownMiles steps: the cursor must not hold the exact distance,
// the X49e re-check), then newest first, then the id.
type nearKey struct {
	d  float64 // the rounded distance (distance_mi)
	at int64   // created_at, unix nanoseconds
	id string
}

// before reports whether a comes before b in the list.
func (a nearKey) before(b nearKey) bool {
	if a.d != b.d {
		return a.d < b.d
	}
	if a.at != b.at {
		return a.at > b.at
	}
	return a.id > b.id
}

// cursor is the key as text: <distance_mi>~<nanoseconds>~<id>.
func (a nearKey) cursor() string {
	return strconv.FormatFloat(a.d, 'f', -1, 64) + "~" + strconv.FormatInt(a.at, 10) + "~" + a.id
}

var errCursor = errors.New("after is not a cursor of a list with a place")

func parseNearCursor(c string) (nearKey, error) {
	f := strings.Split(c, "~")
	if len(f) != 3 || f[2] == "" {
		return nearKey{}, errCursor
	}
	d, err1 := strconv.ParseFloat(f[0], 64)
	at, err2 := strconv.ParseInt(f[1], 10, 64)
	if err1 != nil || err2 != nil || math.IsNaN(d) || d < 0 {
		return nearKey{}, errCursor
	}
	return nearKey{d, at, f[2]}, nil
}

// Near is one page of the active listings within MaxMiles of from, nearest
// first (then newest first), each with distance_mi (X49e). The cursor is the
// key of the last listing of the page before ("" = the start), so a grab,
// a release or a new listing between two pages does not make the next page
// skip a listing (X49e review); next is "" at the end. The app splits near
// and far by the distance the user picked.
func (s *Store) Near(from Place, after string, limit int) ([]Listing, string, error) {
	if limit <= 0 || limit > PageSize {
		limit = PageSize
	}
	var start *nearKey
	if after != "" {
		k, err := parseNearCursor(after)
		if err != nil {
			return nil, "", err
		}
		start = &k
	}
	now := s.now().UTC()
	type near struct {
		l Listing
		k nearKey
	}
	var all []near
	s.mu.Lock()
	for _, r := range s.items {
		if !active(r, now) {
			continue
		}
		l, _, ok := withDistance(r, from)
		if !ok {
			continue
		}
		at, _ := time.Parse(time.RFC3339Nano, r.CreatedAt)
		k := nearKey{*l.DistanceMi, at.UnixNano(), r.ID}
		if start != nil && !start.before(k) {
			continue
		}
		all = append(all, near{l, k})
	}
	s.mu.Unlock()
	slices.SortFunc(all, func(a, b near) int {
		switch {
		case a.k.before(b.k):
			return -1
		case b.k.before(a.k):
			return 1
		}
		return 0
	})
	if len(all) <= limit {
		page := make([]Listing, len(all))
		for i, n := range all {
			page[i] = n.l
		}
		return page, "", nil
	}
	page := make([]Listing, limit)
	for i, n := range all[:limit] {
		page[i] = n.l
	}
	return page, all[limit-1].k.cursor(), nil
}

// Grabbed is the listings userID grabbed whose timer still runs, newest grab
// first (X49e "Your pickup"; a grab can be released until the timer ends).
// With from, each has distance_mi (no MaxMiles limit: the user's own grab).
func (s *Store) Grabbed(userID string, from *Place) []Listing {
	now := s.now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Listing
	for _, r := range s.newestFirstLocked() {
		if r.Status != "collected" || r.CollectedBy == nil || *r.CollectedBy != userID || !running(r, now) {
			continue
		}
		l := forOwnerOrGrabber(r) // X49j: the grabber sees the address
		if from != nil && r.Place != nil {
			m := roundMiles(Miles(*from, *r.Place))
			l.DistanceMi = &m
		}
		out = append(out, l)
	}
	// newest grab first, by the time (RFC 3339 text with nanoseconds does not sort as the time: X38b)
	slices.SortStableFunc(out, func(a, b Listing) int { return grabTime(b).Compare(grabTime(a)) })
	return out
}

func grabTime(l Listing) time.Time {
	if l.CollectedAt == nil {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339Nano, *l.CollectedAt)
	return t
}

// running: the timer of r has not ended at now (any status).
func running(r stored, now time.Time) bool {
	exp, err := time.Parse(time.RFC3339Nano, r.ExpiresAt)
	return err == nil && now.Before(exp)
}

// Release gives a grab back (X49e, T9 2026-10-30: "until the timer ends"):
// the listing is active again for everyone, with the same timer. Only the
// user who grabbed it can. The checks are in this order: unknown
// (ErrNotFound), not grabbed by userID (ErrNotGrabbed), timer ended
// (ErrExpired).
func (s *Store) Release(id, userID string) (Listing, error) { return s.ReleaseThen(id, userID, nil) }

// ReleaseThen is Release; after runs as in GrabThen.
func (s *Store) ReleaseThen(id, userID string, after func(Listing)) (Listing, error) {
	now := s.now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.items[id]
	switch {
	case !ok:
		return Listing{}, ErrNotFound
	case r.Status != "collected" || r.CollectedBy == nil || *r.CollectedBy != userID:
		return Listing{}, ErrNotGrabbed
	case !running(r, now):
		return Listing{}, ErrExpired
	}
	before := r
	r.Status, r.CollectedBy, r.CollectedAt = "active", nil, nil
	s.items[id] = r
	if err := s.saveLocked(); err != nil {
		s.items[id] = before
		return Listing{}, err
	}
	if after != nil {
		after(r.Listing)
	}
	return r.Listing, nil
}

// Mine is the listings of ownerID, any status, newest first.
func (s *Store) Mine(ownerID string) []Listing {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Listing
	for _, r := range s.newestFirstLocked() {
		if r.OwnerID == ownerID {
			out = append(out, forOwnerOrGrabber(r)) // X49j: the owner sees the address
		}
	}
	return out
}

// Grab marks the listing collected by userID. The checks are in this
// order: unknown, own listing, already collected, expired.
func (s *Store) Grab(id, userID string) (Listing, error) { return s.GrabThen(id, userID, nil) }

// GrabThen is Grab; after (when not nil) runs with the grabbed listing while
// the store is still locked (X49e review: the claim hook queues its fan-out
// call in the order of the store changes). after must not block.
func (s *Store) GrabThen(id, userID string, after func(Listing)) (Listing, error) {
	now := s.now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.items[id]
	switch {
	case !ok:
		return Listing{}, ErrNotFound
	case r.OwnerID == userID:
		return Listing{}, ErrOwn
	case r.Status == "collected":
		return Listing{}, ErrCollected
	case !active(r, now):
		return Listing{}, ErrExpired
	}
	at := now.Format(time.RFC3339Nano)
	by := userID
	r.Status, r.CollectedBy, r.CollectedAt = "collected", &by, &at
	before := s.items[id]
	s.items[id] = r
	if err := s.saveLocked(); err != nil {
		s.items[id] = before // the file did not change: the memory does not either
		return Listing{}, err
	}
	if after != nil {
		after(r.Listing)
	}
	return forOwnerOrGrabber(r), nil // X49j: the grabber gets the address
}

// Delete removes listing id of ownerID (X48t, T9 2026-10-30: "the user
// should be able to delete a post that was made already", only before a
// grab). Another owner's listing is ErrNotFound (an id cannot be probed); a
// collected one is ErrGrabbed. The photo file goes after the data file is
// saved; when the save fails, nothing changes.
func (s *Store) Delete(id, ownerID string) (Listing, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.items[id]
	switch {
	case !ok || r.OwnerID != ownerID:
		return Listing{}, ErrNotFound
	case r.Status == "collected":
		return Listing{}, ErrGrabbed
	}
	delete(s.items, id)
	if err := s.saveLocked(); err != nil {
		s.items[id] = r
		return Listing{}, err
	}
	if name := filepath.Base(r.PhotoFile); r.PhotoFile != "" && name != "." && name != ".." && name != string(filepath.Separator) {
		_ = os.Remove(filepath.Join(s.photos, name)) // a file of the photos folder only, never the folder
	}
	return r.Listing, nil
}

// Photo is the path and the type of the photo of listing id.
func (s *Store) Photo(id string) (string, string, error) {
	s.mu.Lock()
	r, ok := s.items[id]
	s.mu.Unlock()
	if !ok {
		return "", "", ErrNotFound
	}
	mime := "image/jpeg"
	switch filepath.Ext(r.PhotoFile) {
	case ".png":
		mime = "image/png"
	case ".webp":
		mime = "image/webp"
	}
	return filepath.Join(s.photos, r.PhotoFile), mime, nil
}
