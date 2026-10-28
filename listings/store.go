package listings

import (
	"cmp"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

// stored is one record of the data file: the listing and its photo file.
// The file is a JSON object of id -> stored; this format is the one the
// data volume already holds.
type stored struct {
	Listing
	PhotoFile string `json:"photo_file"`
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
	id := newID("l_")
	file := id + photoExt[in.Mime]
	photo := filepath.Join(s.photos, file)
	if err := writeAtomic(photo, in.Photo, 0o640); err != nil {
		return Listing{}, err
	}
	r := stored{PhotoFile: file, Listing: Listing{
		ID: id, OwnerID: ownerID, Title: in.Title, NumberOfServing: in.NumberOfServing,
		PhotoURL: "/api/listings/" + id + "/photo", PickupText: in.PickupText, Status: "active",
		CreatedAt: now.Format(time.RFC3339Nano), ExpiresAt: now.Add(s.ttl).Format(time.RFC3339Nano),
		RequestID: in.RequestID,
	}}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[id] = r
	if err := s.saveLocked(); err != nil {
		delete(s.items, id)
		os.Remove(photo)
		return Listing{}, err
	}
	return r.Listing, nil
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
		page = append(page, r.Listing)
	}
	return page, ""
}

// Mine is the listings of ownerID, any status, newest first.
func (s *Store) Mine(ownerID string) []Listing {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Listing
	for _, r := range s.newestFirstLocked() {
		if r.OwnerID == ownerID {
			out = append(out, r.Listing)
		}
	}
	return out
}

// Grab marks the listing collected by userID. The checks are in this
// order: unknown, own listing, already collected, expired.
func (s *Store) Grab(id, userID string) (Listing, error) {
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
