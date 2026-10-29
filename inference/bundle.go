package inference

// X41a (T9, 2026-10-29): the item photo bundle. One call after an analyze
// answer gives every item crop of that answer, already made small, as JSON
// with base64 JPEGs, so the app does not need one GET per item.
// Design: docs/icarus_photo_bundle_api_design_2026-10-28.md (project), with
// T9's choices: 800 px by default, the app and bearer callers, JSON + base64,
// pure Go (libjpeg-turbo only if a measurement says so).
//
//	POST /v1/analyze/photos   (bearer token)    POST /api/analyze/photos (session)
//	{"request_id": "<32 hex>", "photo_ids": ["<32 hex>", ...], "side": 800, "kb": 40}
//
// Each id must belong to the caller AND to that analyze answer; if one does
// not, the answer is 404 for the whole call (it tells nothing about other
// users' photos). The crops are made small in a bounded pool of workers.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // the analyze route takes WebP too (X41a review)

	analyze "github.com/TwoThousandAndTwo2002/Icarus/server"
)

// Bundle limits.
const (
	BundleMaxIDs     = 24 // the analyze item limit (D14)
	BundleSides      = "282 800"
	BundleDefSide    = 800 // T9 2026-10-29: the listing size
	BundleDefKB      = 40
	BundleMinKB      = 10
	BundleMaxKB      = 90
	BundleMaxBody    = 4 << 10
	BundleConcurrent = 2 // bundles at the same time; a third gets 503 busy
	BundleTimeout    = 20 * time.Second
	BundleMaxPixels  = 50_000_000 // a larger image is not decoded (the gate's limit too)
	bundleMinQ       = 30
	bundleMaxQ       = 95
	// CodeClientClosed: the caller went away before the answer (499).
	CodeClientClosed = "client_closed"
)

type bundleIn struct {
	RequestID string   `json:"request_id"`
	PhotoIDs  []string `json:"photo_ids"`
	Side      int      `json:"side"`
	KB        int      `json:"kb"`
}

// BundleItem is one small JPEG of the answer.
type BundleItem struct {
	PhotoID string `json:"photo_id"`
	Item    int    `json:"item"`
	Kind    string `json:"kind"`
	W       int    `json:"w"`
	H       int    `json:"h"`
	Bytes   int    `json:"bytes"`
	Q       int    `json:"q"`
	SHA256  string `json:"sha256"`
	JPEG    string `json:"jpeg_base64"`

	rawBytes int // the item photo's size on disk (for the event)
}

// BundleAnswer is the 200 body.
type BundleAnswer struct {
	RequestID string       `json:"request_id"` // the analyze answer's id
	Side      int          `json:"side"`
	KB        int          `json:"kb"`
	Count     int          `json:"count"`
	Items     []BundleItem `json:"items"`
	MS        BundleMS     `json:"ms"`
	Cached    bool         `json:"cached"`
}

// BundleMS is where the time went.
type BundleMS struct {
	Read     int64 `json:"read"`
	Compress int64 `json:"compress"`
	Total    int64 `json:"total"`
}

// BundleStat is the event's part for step "photos_bundle" (no image bytes).
type BundleStat struct {
	ForRequestID string `json:"for_request_id"`
	Count        int    `json:"count"`
	Side         int    `json:"side"`
	KB           int    `json:"kb"`
	BytesIn      int    `json:"bytes_in"`
	BytesOut     int    `json:"bytes_out"`
	ReadMS       int64  `json:"read_ms"`
	CompressMS   int64  `json:"compress_ms"`
	Workers      int    `json:"workers"`
	Cached       bool   `json:"cached"`
	Q            []int  `json:"q,omitempty"`
}

var (
	bundleOnce    sync.Once
	bundleSlots   chan struct{} // bundle calls at the same time
	bundleWorkers chan struct{} // decodes at the same time, for ALL bundle calls together
)

func bundleInit() {
	bundleOnce.Do(func() {
		bundleSlots = make(chan struct{}, BundleConcurrent)
		bundleWorkers = make(chan struct{}, max(1, runtime.NumCPU()))
	})
}

func (c *ServiceConfig) bundleSlot() chan struct{} {
	bundleInit()
	return bundleSlots
}

// checkBundle validates the body; the side and kb defaults are applied here.
func checkBundle(in *bundleIn) string {
	if !PhotoIDPattern.MatchString(in.RequestID) {
		return "request_id must be 32 hex characters (the analyze answer's request_id)"
	}
	if len(in.PhotoIDs) == 0 || len(in.PhotoIDs) > BundleMaxIDs {
		return "photo_ids must hold 1 to " + strconv.Itoa(BundleMaxIDs) + " ids"
	}
	seen := map[string]bool{}
	for _, id := range in.PhotoIDs {
		if !PhotoIDPattern.MatchString(id) {
			return "each photo_id must be 32 hex characters"
		}
		if seen[id] {
			return "photo_ids must not repeat"
		}
		seen[id] = true
	}
	if in.Side == 0 {
		in.Side = BundleDefSide
	}
	if !strings.Contains(" "+BundleSides+" ", " "+strconv.Itoa(in.Side)+" ") {
		return "side must be 282 or 800"
	}
	if in.KB == 0 {
		in.KB = BundleDefKB
	}
	if in.KB < BundleMinKB || in.KB > BundleMaxKB {
		return "kb must be 10 to 90"
	}
	return ""
}

// smallPath is the cache file of one item photo at one side. Only the
// default size target is cached, so there are at most 2 files per photo
// (X41a review: a key over the id order, side and kb had no bound).
func smallPath(dir, id string, side int) string {
	return filepath.Join(dir, "small", id+"_"+strconv.Itoa(side)+".json")
}

// itemMeta is ReadItem's owner check without reading the photo itself.
func itemMeta(dir, id, userID string) (ItemPhotoMeta, error) {
	var m ItemPhotoMeta
	if !PhotoIDPattern.MatchString(id) {
		return m, ErrNoPhoto
	}
	mb, err := os.ReadFile(filepath.Join(dir, id+".json"))
	if err != nil || json.Unmarshal(mb, &m) != nil || m.PhotoID != id || m.UserID != userID {
		return m, ErrNoPhoto
	}
	return m, nil
}

func (c *ServiceConfig) photos(w http.ResponseWriter, r *http.Request) {
	t0 := time.Now()
	c.cors(w)
	id := analyze.NewRequestID()
	w.Header().Set("X-Request-Id", id)
	remote, fwd := analyze.ClientIPs(r)
	ev := Event{TS: t0.UTC().Format(time.RFC3339Nano), Service: "analyze", Step: "photos_bundle", RequestID: id,
		Route: "photos", RemoteIP: remote, ForwardedIP: fwd, Build: c.Build, Model: c.Engine.Muse}
	st := &BundleStat{}
	done := func(status int, code string) {
		ev.Status, ev.Code, ev.TotalMS, ev.Bundle = status, code, time.Since(t0).Milliseconds(), st
		c.Events.Record(ev)
	}
	fail := func(status int, code, msg string) {
		done(status, code)
		writeJSONResp(w, status, ErrorAnswer{RequestID: id, Error: msg, Code: code})
	}
	kind, u, ok := c.caller(r)
	if !ok {
		fail(http.StatusUnauthorized, CodeUnauthorized, "sign in, or send the bearer token")
		return
	}
	ev.Caller, ev.UserID = kind, u.ID
	body, err := io.ReadAll(io.LimitReader(r.Body, BundleMaxBody+1))
	if err != nil || len(body) > BundleMaxBody {
		fail(http.StatusBadRequest, CodeBadRequest, "the body must be JSON of at most 4 KB")
		return
	}
	var in bundleIn
	if json.Unmarshal(body, &in) != nil {
		fail(http.StatusBadRequest, CodeBadRequest, "the body must be JSON")
		return
	}
	if why := checkBundle(&in); why != "" {
		fail(http.StatusBadRequest, CodeBadRequest, why)
		return
	}
	st.ForRequestID, st.Count, st.Side, st.KB = in.RequestID, len(in.PhotoIDs), in.Side, in.KB
	if c.Photos == nil || c.Photos.Dir == "" {
		fail(http.StatusNotFound, CodeNotFound, "no such photo")
		return
	}
	dir := filepath.Join(c.Photos.Dir, "items")
	// the owner and request check first, for every id, before the cache and
	// before any photo is read (only the small .json records here)
	tr := time.Now()
	metas := make([]ItemPhotoMeta, len(in.PhotoIDs))
	for i, pid := range in.PhotoIDs {
		m, err := itemMeta(dir, pid, u.ID)
		if err != nil || m.RequestID != in.RequestID {
			fail(http.StatusNotFound, CodeNotFound, "no such photo")
			return
		}
		metas[i] = m
	}
	items := make([]BundleItem, len(in.PhotoIDs))
	cacheOK := in.KB == BundleDefKB
	var miss []int
	for i, pid := range in.PhotoIDs {
		if cacheOK {
			if b, err := os.ReadFile(smallPath(dir, pid, in.Side)); err == nil {
				var it BundleItem
				if json.Unmarshal(b, &it) == nil && it.PhotoID == pid && it.JPEG != "" {
					items[i] = it
					continue
				}
			}
		}
		miss = append(miss, i)
	}
	st.ReadMS = time.Since(tr).Milliseconds()
	if len(miss) > 0 {
		slots := c.bundleSlot()
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			fail(http.StatusServiceUnavailable, CodeBusy, "busy; try again in a few seconds")
			return
		}
		// each worker reads its own photo, so at most cap(bundleWorkers) photos
		// are in memory at a time (the review: a crop can be over 10 MB)
		paths := make([]string, len(miss))
		for k, i := range miss {
			paths[k] = filepath.Join(dir, in.PhotoIDs[i]+".jpg")
		}
		ctx, cancel := context.WithTimeout(r.Context(), BundleTimeout)
		defer cancel()
		tc := time.Now()
		st.Workers = cap(bundleWorkers)
		made, err := shrinkAll(ctx, paths, in.Side, in.KB*1024, bundleWorkers)
		st.CompressMS = time.Since(tc).Milliseconds()
		if err != nil {
			switch {
			case errors.Is(err, context.DeadlineExceeded):
				fail(http.StatusGatewayTimeout, CodeTimeout, "the photos took too long")
			case errors.Is(err, context.Canceled):
				fail(499, CodeClientClosed, "the caller went away")
			default:
				fail(http.StatusInternalServerError, CodeInternal, "a photo could not be read")
			}
			return
		}
		for k, i := range miss {
			st.BytesIn += made[k].rawBytes
			made[k].PhotoID = in.PhotoIDs[i]
			items[i] = made[k]
			if cacheOK {
				if b, err := json.Marshal(made[k]); err == nil && os.MkdirAll(filepath.Join(dir, "small"), 0o755) == nil {
					_, _ = writeOnce(smallPath(dir, in.PhotoIDs[i], in.Side), b) // a failed write only costs the next call its work
				}
			}
		}
	}
	a := BundleAnswer{RequestID: in.RequestID, Side: in.Side, KB: in.KB, Count: len(items), Items: items,
		Cached: len(miss) == 0}
	st.Cached = a.Cached
	for i := range a.Items {
		a.Items[i].PhotoID, a.Items[i].Item, a.Items[i].Kind = metas[i].PhotoID, metas[i].Item, metas[i].Kind
		st.BytesOut += a.Items[i].Bytes
		st.Q = append(st.Q, a.Items[i].Q)
	}
	a.MS = BundleMS{Read: st.ReadMS, Compress: st.CompressMS, Total: time.Since(t0).Milliseconds()}
	done(http.StatusOK, "")
	writeJSONResp(w, http.StatusOK, a)
}

// shrinkAll makes each photo small, with at most cap(workers) decodes at the
// same time over ALL bundle calls (the pool is shared, so two calls do not
// use twice the CPU). Each result lands in its own slot, so the order is kept
// with no lock. The first error stops the jobs that have not started.
func shrinkAll(parent context.Context, paths []string, side, target int, workers chan struct{}) ([]BundleItem, error) {
	out := make([]BundleItem, len(paths))
	errs := make([]error, len(paths))
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	var wg sync.WaitGroup
start:
	for i := range paths {
		select {
		case workers <- struct{}{}:
		case <-ctx.Done():
			break start
		}
		wg.Add(1)
		go func(i int) {
			defer func() { <-workers; wg.Done() }()
			if ctx.Err() != nil {
				errs[i] = ctx.Err()
				return
			}
			raw, err := os.ReadFile(paths[i])
			if err != nil {
				errs[i] = err
				cancel()
				return
			}
			it, err := shrink(raw, side, target)
			it.rawBytes = len(raw)
			if err != nil {
				errs[i] = err
				cancel() // the first error stops the jobs that have not started
				return
			}
			out[i] = it
		}(i)
	}
	wg.Wait()
	if err := parent.Err(); err != nil { // the deadline, or the caller went away
		return nil, err
	}
	for _, e := range errs {
		if e != nil && !errors.Is(e, context.Canceled) {
			return nil, e
		}
	}
	for _, e := range errs {
		if e != nil {
			return nil, e
		}
	}
	return out, nil
}

// shrink decodes one item photo (JPEG, PNG, GIF or WebP: a "full" item
// photo is the upload as it was), turns it upright (EXIF), fits its long side
// to `side` (never larger) and encodes it at the highest JPEG quality in
// 30..95 that fits `target` bytes (a binary search: at most 7 encodes, and
// one more if even 30 does not fit; then 30 is kept and the real size given).
func shrink(raw []byte, side, target int) (BundleItem, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return BundleItem{}, err
	}
	if cfg.Width*cfg.Height > BundleMaxPixels {
		return BundleItem{}, errors.New("the photo has too many pixels")
	}
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return BundleItem{}, err
	}
	img := fitLong(Upright(src, Orientation(raw)), side)
	enc := func(q int) ([]byte, error) {
		var buf bytes.Buffer
		err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: q})
		return buf.Bytes(), err
	}
	lo, hi := bundleMinQ, bundleMaxQ
	var best []byte
	bestQ := 0
	for lo <= hi {
		q := (lo + hi + 1) / 2
		b, err := enc(q)
		if err != nil {
			return BundleItem{}, err
		}
		if len(b) <= target {
			best, bestQ, lo = b, q, q+1
		} else {
			hi = q - 1
		}
	}
	if best == nil {
		if best, err = enc(bundleMinQ); err != nil {
			return BundleItem{}, err
		}
		bestQ = bundleMinQ
	}
	sum := sha256.Sum256(best)
	b := img.Bounds()
	return BundleItem{W: b.Dx(), H: b.Dy(), Bytes: len(best), Q: bestQ,
		SHA256: hex.EncodeToString(sum[:]), JPEG: base64.StdEncoding.EncodeToString(best)}, nil
}

// fitLong scales img so its long side is at most `side` (CatmullRom); an
// image that is already small enough is returned as it is.
func fitLong(img image.Image, side int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	long := max(w, h)
	if long <= side {
		return img
	}
	nw, nh := w*side/long, h*side/long
	nw, nh = max(nw, 1), max(nh, 1)
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
	return dst
}
