package post

// R1 (T9 2026-10-30: "i thought that we were recording what route a
// transaction was routed through"): the analyze service writes one events line
// per scan with its request_id, user_id and source (barcode | vision;
// inference/events.go). The listing keeps the scan's request_id (the app sends
// it with the post). scanSource reads that line, so rescue uses the scanner's
// meal_condition only for a scan that went the vision route, for this owner.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// The scan routes of the analyze events (inference: SourceBarcode, SourceVision).
const (
	ScanBarcode = "barcode"
	ScanVision  = "vision"
)

// requestIDForm is the analyze request_id (32 hex), the form the listings
// service keeps (listings/listing.go hex32).
var requestIDForm = regexp.MustCompile(`^[0-9a-f]{32}$`)

// MaxEventLine is the longest analyze events line scanSource reads (a scan of
// many items with boxes); a longer line stops the read of that file.
const MaxEventLine = 16 << 20

// scanSource is the route of the scan of listing l: the source of the analyze
// events line with l.RequestID, status 200 and the owner's user_id, in the
// events of the listing's UTC day and of the day before (a scan comes before
// its post). It is "" when dir is "", the id is not an analyze id, the files
// or the line are missing, or the line is another user's.
func scanSource(dir string, l Listing) string {
	if dir == "" || !requestIDForm.MatchString(l.RequestID) {
		return ""
	}
	t, err := time.Parse(time.RFC3339Nano, l.CreatedAt)
	if err != nil {
		return ""
	}
	t = t.UTC()
	for _, d := range []time.Time{t, t.AddDate(0, 0, -1)} {
		if s, ok := sourceIn(filepath.Join(dir, "events-"+d.Format("20060102")+".jsonl"), l.RequestID, l.OwnerID); ok {
			return s
		}
	}
	return ""
}

// sourceIn finds the answer line of requestID (status 200, a known route) in
// one events file; ok is false when the file has no such line. The source is
// "" when the line is another user's.
func sourceIn(path, requestID, owner string) (string, bool) {
	fh, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer fh.Close()
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 64<<10), MaxEventLine)
	for sc.Scan() {
		if !bytes.Contains(sc.Bytes(), []byte(requestID)) {
			continue
		}
		var ev struct {
			RequestID string `json:"request_id"`
			UserID    string `json:"user_id"`
			Source    string `json:"source"`
			Status    int    `json:"status"`
		}
		if json.Unmarshal(sc.Bytes(), &ev) != nil || ev.RequestID != requestID || ev.Status != 200 ||
			(ev.Source != ScanVision && ev.Source != ScanBarcode) {
			continue // not this scan's answer line (a label report is 204 and names no route): keep looking
		}
		if ev.UserID == "" || ev.UserID != owner {
			return "", true
		}
		return ev.Source, true
	}
	return "", false
}
