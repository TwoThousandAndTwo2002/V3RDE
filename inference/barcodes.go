package inference

// I1 (T9 2026-11-01, "A: one photo with many barcodes"): the app reads every
// retail code in the still photo (expo-camera scanFromURLAsync; on the web the
// Barcode Detection API, which answers every code with its bounding box) and
// sends them in the optional "barcodes" form field. The service looks up every
// code at the same time; each catalog HIT is one item. The vision path still
// runs for a photo with barcodes and other food (T9: "barcodes + vision"): a
// SAM box that holds the centre of a HIT code is that catalog item, so it is
// not listed twice.

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// LookupBudget is the time all the lookups of one scan may take (I1 review:
// they run with the scanner, so the scan keeps its time limit).
const LookupBudget = 3 * time.Second

// MaxBarcodes is the most codes one scan looks up (the rest are dropped).
const MaxBarcodes = 24

// barcodeWorkers is the most lookups in flight for one scan.
const barcodeWorkers = 8

// CodeBox is one code the app read in the photo and its box, in the pixels of
// an image FW x FH (the still the app read; the service scales it to SAM's
// frame). FW = 0: no box (the live camera read).
type CodeBox struct {
	Code string `json:"code"`
	X    int    `json:"x"`
	Y    int    `json:"y"`
	W    int    `json:"w"`
	H    int    `json:"h"`
	FW   int    `json:"fw"`
	FH   int    `json:"fh"`
}

// retailCode is a code with 8, 12, 13 or 14 digits (EAN-8, UPC-A, EAN-13,
// GTIN-14), as the app's barcodeOf; "" for anything else.
func retailCode(s string) string {
	if n := len(s); n != 8 && n != 12 && n != 13 && n != 14 {
		return ""
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return s
}

// ParseBarcodes reads the "barcodes" form field: a JSON list of CodeBox.
// Codes that are not retail codes, repeats and boxes that are not inside
// their image are dropped (a dropped box keeps its code, with no box); at
// most MaxBarcodes. Bad JSON = no codes (the scan goes on as today).
func ParseBarcodes(raw string) []CodeBox {
	if raw == "" || len(raw) > 8<<10 {
		return nil
	}
	var in []CodeBox
	if json.Unmarshal([]byte(raw), &in) != nil {
		return nil
	}
	var out []CodeBox
	seen := map[string]bool{}
	for _, c := range in {
		c.Code = retailCode(c.Code)
		if c.Code == "" || seen[c.Code] {
			continue
		}
		seen[c.Code] = true
		if c.FW <= 0 || c.FH <= 0 || c.W <= 0 || c.H <= 0 || c.X < 0 || c.Y < 0 || c.X+c.W > c.FW || c.Y+c.H > c.FH {
			c.X, c.Y, c.W, c.H, c.FW, c.FH = 0, 0, 0, 0, 0, 0
		}
		out = append(out, c)
		if len(out) == MaxBarcodes {
			break
		}
	}
	return out
}

// withLive adds the live camera code (the "barcode" field) when it is a
// retail code that is not in the list yet.
func withLive(list []CodeBox, code string) []CodeBox {
	code = retailCode(code)
	if code == "" {
		return list
	}
	for _, c := range list {
		if c.Code == code {
			return list
		}
	}
	return append([]CodeBox{{Code: code}}, list...)
}

// sameShape reports whether the still the app read (cw x ch) and the
// scanner's frame (fw x fh) have the same aspect, within 2 % (I1 review: a
// photo turned by EXIF in one and not in the other must not match boxes).
func sameShape(cw, ch, fw, fh int) bool {
	if cw <= 0 || ch <= 0 || fw <= 0 || fh <= 0 {
		return false
	}
	a, b := float64(cw)/float64(ch), float64(fw)/float64(fh)
	return math.Abs(a-b)/b <= 0.02
}

// centreIn reports whether the centre of the code's box is in box b
// ([left, top, right, bottom] in a frame fw x fh). A code with no box, or a
// still of another shape, is in no box.
func centreIn(c CodeBox, b [4]int, fw, fh int) bool {
	if !sameShape(c.FW, c.FH, fw, fh) {
		return false
	}
	x := (float64(c.X) + float64(c.W)/2) * float64(fw) / float64(c.FW)
	y := (float64(c.Y) + float64(c.H)/2) * float64(fh) / float64(c.FH)
	return x >= float64(b[0]) && x <= float64(b[2]) && y >= float64(b[1]) && y <= float64(b[3])
}

// codesKey is the codes of a scan as one short key (the async same-scan key):
// the sorted codes, so a resend with other JSON spacing is the same scan.
func codesKey(reads []CodeBox) string {
	var cs []string
	for _, c := range reads {
		cs = append(cs, c.Code)
	}
	sort.Strings(cs)
	return strings.Join(cs, ",")
}

// stillOnly reports whether the scan takes the path of many codes: two or
// more different codes, or (no live code) one code the app read in the
// still with its box (I1 review: a plate with one packet still runs the
// scanner). One live code alone is today's fast path.
func stillOnly(reads []CodeBox, live string) bool {
	if len(reads) >= 2 {
		return true
	}
	return len(reads) == 1 && retailCode(live) == "" && reads[0].FW > 0
}

// BarcodeRead is the ledger record of one code of a scan with many codes.
type BarcodeRead struct {
	Code    string `json:"code"`
	Outcome string `json:"outcome"`          // the catalog outcome (HIT, MISS, ...); "error" when the lookup failed
	Status  int    `json:"status,omitempty"` // the barcode service's HTTP status
	MS      int64  `json:"ms"`
	Item    int    `json:"item,omitempty"`  // the answer item it became
	Box     int    `json:"box,omitempty"`   // the SAM box (Boxes[].N) that holds it
	Note    string `json:"note,omitempty"`  // not_admissible | already_listed
	Stock   string `json:"stock,omitempty"` // N1: cutout | as_is | none; "" = not looked up
}

// codeAnswer is one lookup of lookupAll.
type codeAnswer struct {
	ans    barcodeAnswer
	status int
	err    error
	ms     int64
}

// lookupAll looks up every code at the same time (at most barcodeWorkers in
// flight), all within ctx; the answers are in the order of the codes.
func lookupAll(ctx context.Context, r *http.Request, hc *http.Client, base, trace string, codes []CodeBox) []codeAnswer {
	r = r.WithContext(ctx)
	out := make([]codeAnswer, len(codes))
	sem := make(chan struct{}, barcodeWorkers)
	var wg sync.WaitGroup
	for i, c := range codes {
		wg.Add(1)
		go func(i int, code string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			t := time.Now()
			a, st, err := lookupBarcode(r, hc, base, trace, code)
			out[i] = codeAnswer{ans: a, status: st, err: err, ms: time.Since(t).Milliseconds()}
		}(i, c.Code)
	}
	wg.Wait()
	return out
}
