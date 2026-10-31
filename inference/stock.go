package inference

// N1 (T9 2026-10-31: "stock photo only"): after a barcode hit, the listing
// photo is the product's front photo from Open Food Facts, not the camera
// shot. The photos were made on the Mac (X33, T5 set: US products with an
// English front photo, a name, a brand, a barcode in our catalog and a
// quantity), 800 px, at most about 40 KB each:
//
//	<Dir>/b/<gtin14>.jpg   the white cutout (X36: "use the white cutout")
//	<Dir>/a/<gtin14>.jpg   the photo as Open Food Facts has it (when no cutout)
//
// Open Food Facts photos are CC BY-SA (https://world.openfoodfacts.org/terms-of-use):
// the answer item carries StockCredit, so a client can show the credit. T9
// 2026-10-31: no change to the app interface in this change.

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
)

// StockPhotos reads the folder above; Dir "" = off.
type StockPhotos struct{ Dir string }

// StockCredit is the credit of a stock photo (answer item photo_credit).
const StockCredit = "Open Food Facts, CC BY-SA"

// Stock photo kinds (ItemPhotoMeta.Stock and the events line).
const (
	StockCutout = "cutout"
	StockAsIs   = "as_is"
	StockNone   = "none" // the folder is on, the product has no photo
	// StockUnreadable (N1 review): a file is there but cannot be read (for
	// example its mode), not a JPEG, or too large: the events line shows it.
	StockUnreadable = "unreadable"
)

// MaxStockBytes is the largest stock photo read (the files are about 40 KB).
const MaxStockBytes = 1 << 20

var gtin14Re = regexp.MustCompile(`^[0-9]{14}$`)

// Find returns the cutout, else the photo as is, and its kind; nil and
// StockNone when there is none (or the file is not a JPEG, or too large).
func (s *StockPhotos) Find(gtin14 string) ([]byte, string) {
	if s == nil || s.Dir == "" || !gtin14Re.MatchString(gtin14) {
		return nil, StockNone
	}
	kind := StockNone
	for _, v := range []struct{ sub, kind string }{{"b", StockCutout}, {"a", StockAsIs}} {
		b, missing := readJPEG(filepath.Join(s.Dir, v.sub, gtin14+".jpg"))
		if b != nil {
			return b, v.kind
		}
		if !missing {
			kind = StockUnreadable
		}
	}
	return nil, kind
}

// readJPEG returns the file, or nil and whether the file is missing.
func readJPEG(p string) ([]byte, bool) {
	f, err := os.Open(p)
	if err != nil {
		return nil, errors.Is(err, fs.ErrNotExist)
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, MaxStockBytes+1))
	if err != nil || len(b) > MaxStockBytes || !bytes.HasPrefix(b, []byte{0xFF, 0xD8, 0xFF}) {
		return nil, false
	}
	return b, false
}
