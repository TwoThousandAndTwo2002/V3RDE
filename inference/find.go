package inference

// I3 (T9 2026-11-01): POST /api/analyze/find, "find a missed item". The
// donor's session only (the scan's owner). Body: the scan's request_id, and
// either name (2a: "sandwich") or box + label (2b: the box the donor drew on
// the photo, in the pixels of an image fw x fh, and its name). The service
// reads the scan from the analyze events line (the same request id and user;
// as the rescue reads it), reads the scan's photo, and runs FindItems with
// the scan's listed boxes (and those of earlier finds) to avoid. The answer
// has the analyze answer's shape (items with a crop photo each), plus
// not_found when nothing new was found. Rate limit, busy and the daily cap
// are as for a scan; the events line has step "find".

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	analyze "github.com/TwoThousandAndTwo2002/Icarus/server"
)

// Find refusal codes.
const (
	CodeScanNotFound = "scan_not_found"
	CodeFindBadAsk   = "find_bad_ask"
	maxFindBody      = 4 << 10
	// SourceFind is the answer source of a find.
	SourceFind = "find"
)

// FindBox is the box the donor drew (2b), in the pixels of an image FW x FH.
type FindBox struct {
	X  int `json:"x"`
	Y  int `json:"y"`
	W  int `json:"w"`
	H  int `json:"h"`
	FW int `json:"fw"`
	FH int `json:"fh"`
}

type findIn struct {
	RequestID string   `json:"request_id"`
	Name      string   `json:"name"`
	Box       *FindBox `json:"box"`
	Label     string   `json:"label"`
}

// FindRec is what the find asked, for the events line.
type FindRec struct {
	Word string   `json:"word"`          // the name (2a) or the label (2b), as FindWord made it
	Box  *FindBox `json:"box,omitempty"` // 2b
}

// FindAnswer is the 200 body of a find.
type FindAnswer struct {
	Answer
	NotFound bool `json:"not_found"`
}

// scanRef is the scan a find is for.
type scanRef struct {
	sha            string
	frameW, frameH int
	avoid          [][4]int // listed boxes, in the scan's frame
}

// findScan reads the scan request_id of user from the events of the day of
// now and the day before: its photo and the boxes of its list, plus the
// boxes of the finds made for it. ok is false when there is no such scan.
func findScan(dir, requestID, userID string, now time.Time) (scanRef, bool) {
	var ref scanRef
	found := false
	type line struct {
		Step        string     `json:"step"`
		RequestID   string     `json:"request_id"`
		Of          string     `json:"of"`
		UserID      string     `json:"user_id"`
		Status      int        `json:"status"`
		ImageSHA256 string     `json:"image_sha256"`
		FrameW      int        `json:"frame_w"`
		FrameH      int        `json:"frame_h"`
		Boxes       []EventBox `json:"boxes"`
	}
	var finds []line
	now = now.UTC()
	for _, d := range []time.Time{now.AddDate(0, 0, -1), now} {
		fh, err := os.Open(filepath.Join(dir, "events-"+d.Format("20060102")+".jsonl"))
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(fh)
		sc.Buffer(make([]byte, 1<<20), 1<<24)
		for sc.Scan() {
			if !bytes.Contains(sc.Bytes(), []byte(requestID)) {
				continue
			}
			var l line
			if json.Unmarshal(sc.Bytes(), &l) != nil || l.UserID != userID || l.Status != http.StatusOK {
				continue
			}
			switch {
			case l.Step == "analyze" && l.RequestID == requestID && l.ImageSHA256 != "":
				found = true
				ref = scanRef{sha: l.ImageSHA256, frameW: l.FrameW, frameH: l.FrameH}
				for _, b := range l.Boxes { // I3 review: an item, or a product already live in the listings
					if b.Outcome == "item" || b.Outcome == "already_listed" {
						ref.avoid = append(ref.avoid, b.Box)
					}
				}
			case l.Step == "find" && l.Of == requestID:
				finds = append(finds, l)
			}
		}
		fh.Close()
	}
	if !found {
		return ref, false
	}
	for _, f := range finds { // earlier finds: their items are on the list too
		for _, b := range f.Boxes {
			if b.Outcome != "item" {
				continue
			}
			if ref.frameW > 0 && f.FrameW > 0 && (f.FrameW != ref.frameW || f.FrameH != ref.frameH) {
				sx, sy := float64(ref.frameW)/float64(f.FrameW), float64(ref.frameH)/float64(f.FrameH)
				b.Box = [4]int{int(float64(b.Box[0]) * sx), int(float64(b.Box[1]) * sy), int(float64(b.Box[2]) * sx), int(float64(b.Box[3]) * sy)}
			}
			if ref.frameW == 0 { // a barcode scan has no frame: the find's frame
				ref.frameW, ref.frameH = f.FrameW, f.FrameH
			}
			ref.avoid = append(ref.avoid, b.Box)
		}
	}
	return ref, true
}

// checkFind validates the ask; "" when it is good.
func checkFind(in *findIn) string {
	switch {
	case !labelReqRe.MatchString(in.RequestID):
		return "request_id must be the scan's request_id (32 hex)"
	case (in.Name == "") == (in.Box == nil):
		return "send name, or box and label"
	case in.Box == nil && FindWord(in.Name) == "":
		return "name must be a word (letters)"
	case in.Box != nil && FindWord(in.Label) == "":
		return "label must be a word (letters)"
	case in.Box != nil && (in.Box.FW <= 0 || in.Box.FH <= 0 || in.Box.W <= 0 || in.Box.H <= 0 || in.Box.X < 0 || in.Box.Y < 0 ||
		in.Box.X+in.Box.W > in.Box.FW || in.Box.Y+in.Box.H > in.Box.FH || in.Box.FW > 20000 || in.Box.FH > 20000):
		return "box must be inside its image"
	}
	return ""
}

func (c *ServiceConfig) find(w http.ResponseWriter, r *http.Request) {
	t0 := time.Now()
	id := analyze.NewRequestID()
	w.Header().Set("X-Request-Id", id)
	c.cors(w)
	remote, fwd := analyze.ClientIPs(r)
	ev := Event{TS: t0.UTC().Format(time.RFC3339Nano), Service: "analyze", Step: "find", TraceID: id,
		RequestID: id, RemoteIP: remote, ForwardedIP: fwd, Build: c.Build, Model: c.Engine.Muse}
	finish := func(status int, route string) {
		ev.Status, ev.Route, ev.TotalMS = status, route, time.Since(t0).Milliseconds()
		if c.Budget != nil {
			ev.SpentTodayUSD = c.Budget.Spent()
		}
		c.Events.Record(ev)
	}
	fail := func(status int, code, msg string) {
		ev.Code = code
		if ev.Error == "" {
			ev.Error = msg
		}
		finish(status, "error")
		writeJSONResp(w, status, ErrorAnswer{RequestID: id, Error: msg, Code: code})
	}
	kind, user, ok := c.caller(r)
	if !ok || kind != "session" { // the scan's owner only: a bearer caller has no user
		fail(http.StatusUnauthorized, CodeUnauthorized, "sign in")
		return
	}
	ev.Caller, ev.UserID = kind, user.ID
	key := "user:" + user.ID
	ev.CapKey = key
	if c.Limiter != nil && !c.Limiter.Allow(key) {
		fail(http.StatusTooManyRequests, CodeRateLimited, "too many photos this minute; wait a minute")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxFindBody+1))
	if err != nil || len(body) > maxFindBody {
		fail(http.StatusRequestEntityTooLarge, CodeTooLarge, "the ask is over 4 KB")
		return
	}
	var in findIn
	if json.Unmarshal(body, &in) != nil {
		fail(http.StatusBadRequest, CodeBadRequest, "the ask must be JSON")
		return
	}
	if why := checkFind(&in); why != "" {
		fail(http.StatusBadRequest, CodeFindBadAsk, why)
		return
	}
	word := FindWord(in.Name)
	if in.Box != nil {
		word = FindWord(in.Label)
	}
	ev.Of, ev.Find = in.RequestID, &FindRec{Word: word, Box: in.Box}
	scan, ok := findScan(c.Events.Dir, in.RequestID, user.ID, c.now())
	if !ok {
		fail(http.StatusNotFound, CodeScanNotFound, "no scan of yours with this request_id today")
		return
	}
	ev.ImageSHA256 = scan.sha
	if c.Photos == nil || c.Photos.Dir == "" { // I3 review: no photo store = no photo to search
		fail(http.StatusNotFound, CodeScanNotFound, "the scan's photo is not kept")
		return
	}
	img, err := os.ReadFile(filepath.Join(c.Photos.Dir, scan.sha+".jpg"))
	if err != nil {
		fail(http.StatusNotFound, CodeScanNotFound, "the scan's photo is not kept")
		return
	}
	ev.ImageBytes = len(img)
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	default:
		fail(http.StatusServiceUnavailable, CodeBusy, "the scanner is busy; try again in a few seconds")
		return
	}
	var hold *Hold
	if c.Budget != nil {
		var why string
		if hold, why = c.Budget.ReserveFor(key); hold == nil {
			if why == CodeCallerCap {
				fail(http.StatusTooManyRequests, CodeCallerCap, "your daily scan limit is reached; try again after 00:00 UTC")
				return
			}
			fail(http.StatusServiceUnavailable, CodeDailyCap, "the daily limit is reached; try again after 00:00 UTC")
			return
		}
	}
	ask := FindAsk{Hint: word, Avoid: scan.avoid, AvoidW: scan.frameW, AvoidH: scan.frameH}
	if in.Box == nil {
		ask.Concept = word
	} else {
		cfg, _, err := image.DecodeConfig(bytes.NewReader(img))
		if err != nil {
			if hold != nil {
				hold.Settle(0)
			}
			fail(http.StatusBadGateway, CodeUpstream, "the scan's photo does not decode")
			return
		}
		pw, ph := cfg.Width, cfg.Height
		if o := Orientation(img); o >= 5 { // turned a quarter: the upright photo is ph x pw
			pw, ph = ph, pw
		}
		if !sameShape(in.Box.FW, in.Box.FH, pw, ph) {
			if hold != nil {
				hold.Settle(0)
			}
			fail(http.StatusBadRequest, CodeFindBadAsk, "the box is for an image of another shape")
			return
		}
		sx, sy := float64(pw)/float64(in.Box.FW), float64(ph)/float64(in.Box.FH)
		ask.Box = image.Rect(int(float64(in.Box.X)*sx), int(float64(in.Box.Y)*sy),
			int(float64(in.Box.X+in.Box.W)*sx+0.5), int(float64(in.Box.Y+in.Box.H)*sy+0.5))
	}
	ctx, cancel := context.WithTimeout(r.Context(), c.Timeout)
	defer cancel()
	inv, err := c.Engine.FindItems(ctx, img, "image/jpeg", ask)
	if hold != nil {
		hold.Settle(inv.SpentUSD())
	}
	ev.fromInventory(inv)
	ev.FrameW, ev.FrameH = inv.FrameW, inv.FrameH
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			fail(http.StatusGatewayTimeout, CodeTimeout, "the search took too long; try again")
			return
		}
		fail(http.StatusBadGateway, CodeUpstream, "the scanner could not search this photo: "+err.Error())
		return
	}
	ans := FindAnswer{Answer: Answer{RequestID: id, Model: c.Engine.Muse, Source: SourceFind, Items: []AnswerItem{}}}
	src, _, derr := image.Decode(bytes.NewReader(img))
	if derr == nil {
		src = Upright(src, Orientation(img))
	}
	answerOf := map[int]int{}
	for i, it := range inv.Items {
		n := len(ans.Items) + 1
		var pid string
		if b, ok := inv.LargestBox(it); ok && src != nil {
			if jb, cerr := CropJPEG(src, b, CropMargin); cerr == nil {
				pid, err = c.Photos.SaveItem(jb, ItemPhotoMeta{UserID: user.ID, RequestID: id, ImageSHA256: scan.sha,
					Item: n, Kind: "crop", Box: boxSlice(b), FrameW: b.W, FrameH: b.H, TS: t0.UTC().Format(time.RFC3339)})
				if err != nil {
					fail(http.StatusInternalServerError, CodeInternal, "photo store: "+err.Error())
					return
				}
			}
		}
		if pid == "" { // no crop: the item has no photo of its own, so it is not answered
			continue
		}
		answerOf[i] = n
		ans.Items = append(ans.Items, AnswerItem{Item: n, Fields: it.Result, PhotoID: pid})
	}
	ledgerBoxes(&ev, inv, answerOf, nil, nil)
	for _, b := range scan.avoid { // I3 review: the scan's list, so a touch-up of a found item knows its neighbours
		ev.Boxes = append(ev.Boxes, EventBox{Box: b, Outcome: "scan_listed"})
	}
	ans.Count, ans.LatencyMS, ans.NotFound = len(ans.Items), time.Since(t0).Milliseconds(), len(ans.Items) == 0
	ev.Source, ev.Items = SourceFind, ans.Count
	for _, it := range ans.Items {
		ev.PhotoIDs = append(ev.PhotoIDs, it.PhotoID)
	}
	if ans.NotFound && ev.Error == "" {
		ev.Error = "not found: " + strings.TrimSpace(word)
	}
	finish(http.StatusOK, SourceFind)
	writeJSONResp(w, http.StatusOK, ans)
}
