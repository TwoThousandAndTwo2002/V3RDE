package inference

// X32a (T9, 2026-10-27): the server keeps every photo and cuts one crop per
// item. The crops are the listing photos (POST /api/listings photo_id) and,
// with the boxes in the ledger, a training set.
//
//	<dir>/<sha256>.jpg            the uploaded photo, as is (router's store)
//	<dir>/items/<photo_id>.jpg    one item's listing photo (crop, or the full photo)
//	<dir>/items/<photo_id>.json   its owner and where it came from
//
// photo_id is 32 random hex characters. Only the owner reads it back
// (OWASP API1:2023, object level authorization); bearer callers own "".

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
)

// ItemPhotoMeta is the .json next to an item photo.
type ItemPhotoMeta struct {
	PhotoID     string `json:"photo_id"`
	UserID      string `json:"user_id"` // "" = a bearer caller
	RequestID   string `json:"request_id"`
	ImageSHA256 string `json:"image_sha256"` // the uploaded photo it came from
	Item        int    `json:"item"`
	Kind        string `json:"kind"`             // crop | full | stock (N1)
	Stock       string `json:"stock,omitempty"`  // N1: cutout | as_is (Kind stock)
	GTIN14      string `json:"gtin14,omitempty"` // N1: the product of a stock photo
	Box         []int  `json:"box,omitempty"`    // [left, top, right, bottom], SAM pixels
	FrameW      int    `json:"frame_w,omitempty"`
	FrameH      int    `json:"frame_h,omitempty"`
	Bytes       int    `json:"bytes"`
	TS          string `json:"ts"`
}

// PhotoIDPattern is the only accepted photo_id form (listings checks it too).
var PhotoIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// ErrNoPhoto is an unknown photo_id, or one the caller does not own.
var ErrNoPhoto = errors.New("no such photo")

// PhotoStore writes into Dir (a volume shared read-only with listings).
type PhotoStore struct{ Dir string }

func newPhotoID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand does not fail on Linux
	}
	return hex.EncodeToString(b[:])
}

// writeOnce writes p via a temp file + rename; false when p already exists.
func writeOnce(p string, b []byte) (bool, error) {
	if _, err := os.Stat(p); err == nil {
		return false, nil
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".up-*")
	if err != nil {
		return false, err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return false, err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return false, err
	}
	if err := os.Rename(tmp.Name(), p); err != nil {
		os.Remove(tmp.Name())
		return false, err
	}
	return true, nil
}

// SaveOriginal keeps the uploaded photo under its sha256 (once).
func (s *PhotoStore) SaveOriginal(sha string, img []byte) (bool, error) {
	if s == nil || s.Dir == "" {
		return false, nil
	}
	return writeOnce(filepath.Join(s.Dir, sha+".jpg"), img)
}

// SaveItem writes one item photo and its owner record; it returns the photo_id.
func (s *PhotoStore) SaveItem(jpg []byte, m ItemPhotoMeta) (string, error) {
	if s == nil || s.Dir == "" {
		return "", errors.New("no photo store")
	}
	dir := filepath.Join(s.Dir, "items")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	m.PhotoID, m.Bytes = newPhotoID(), len(jpg)
	if _, err := writeOnce(filepath.Join(dir, m.PhotoID+".jpg"), jpg); err != nil {
		return "", err
	}
	mb, _ := json.Marshal(m)
	if _, err := writeOnce(filepath.Join(dir, m.PhotoID+".json"), mb); err != nil {
		os.Remove(filepath.Join(dir, m.PhotoID+".jpg"))
		return "", err
	}
	return m.PhotoID, nil
}

// ReadItem returns an item photo for its owner only (ErrNoPhoto otherwise).
func ReadItem(dir, id, userID string) ([]byte, ItemPhotoMeta, error) {
	var m ItemPhotoMeta
	if !PhotoIDPattern.MatchString(id) {
		return nil, m, ErrNoPhoto
	}
	mb, err := os.ReadFile(filepath.Join(dir, id+".json"))
	if err != nil || json.Unmarshal(mb, &m) != nil || m.PhotoID != id || m.UserID != userID {
		return nil, m, ErrNoPhoto
	}
	b, err := os.ReadFile(filepath.Join(dir, id+".jpg"))
	if err != nil {
		return nil, m, ErrNoPhoto
	}
	return b, m, nil
}
