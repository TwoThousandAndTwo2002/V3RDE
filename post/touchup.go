package post

// Touch-up (X35a). A listing photo is made to look better for the post by
// Muse Image 1.0 (POST /v1/images/edits), then Muse Spark 1.3 compares the
// original and the edit; the edit is offered only when the food did not
// change. The original is always kept and is the default.
//
//	POST /api/touchup?mode=light|background&photo_id=<id>   (session cookie)
//	      no body; photo_id = an item photo of the analyze service that
//	      belongs to the caller -> 202 {request_id, touchup_id, status:"running"}
//	POST /api/touchup/batch?mode=light|background&photo_ids=<id>,<id>,...
//	      X44c: up to 6 photos of one scan in one call (no body); each is a
//	      job; a job waits in a queue for a free slot ("queued") instead of
//	      503 busy -> 202 {request_id, mode, jobs: [{photo_id, touchup_id,
//	      status}], skipped: [{photo_id, code, error}]}
//	GET  /api/touchup/{id}        -> 200 Job
//	GET  /api/touchup/{id}/photo  -> the edited JPEG (status "done" only)
//	GET  /healthz
//
// Only photo_id (X36a, T9 2026-10-28): the donor taps the touch-up button on
// the review list after the analyze run, so the photo has passed the food
// gate and is upright (inference Upright) before any paid call here. There is
// no photo upload and no bearer route: the analyze token is in the app
// bundle, so it is public (server/guard.go), and a bearer route would let
// anyone spend this service's cap on any image.
//
// Why a job and not one answer: the public name is behind API Gateway, which
// stops every request at 30 s (X32c, D58); Muse Image is a reasoning model and
// its time is not measured yet, so the edit must not hold the request open.
//
// The edited photo is stored in the analyze service's item-photo format
// (<photo_id>.jpg + <photo_id>.json with photo_id and user_id), so the
// listings service can take it as photo_id when its folder is mounted there
// (integration step; listings/listings.go itemPhoto).

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/TwoThousandAndTwo2002/Icarus/inference"
	analyze "github.com/TwoThousandAndTwo2002/Icarus/server"
)

// Touch-up modes: a fixed list, never a prompt from the user, so the service
// cannot be used as a general image generator.
const (
	ModeLight      = "light"      // light, white balance, sharpness only
	ModeBackground = "background" // the same, and a plain, uncluttered background
)

// EditPrompts is the instruction for each mode. Every one forbids a change
// to the food (T9 2026-10-28: the post must show the real food).
var EditPrompts = map[string]string{
	ModeLight: "Improve this food photo for a food-sharing post: correct the exposure, the white balance " +
		"and the sharpness only. Do not add, remove, move, resize or change any food item, package, " +
		"container, label, text or quantity. Do not change the color, the texture or how fresh the food " +
		"looks. Keep the same framing and the same camera angle.",
	ModeBackground: "Improve this food photo for a food-sharing post: correct the exposure, the white " +
		"balance and the sharpness, and replace the clutter behind the food with a plain, neutral surface. " +
		"Do not add, remove, move, resize or change any food item, package, container, label, text or " +
		"quantity. Do not change the color, the texture or how fresh the food looks. Keep the same framing " +
		"and the same camera angle.",
}

// VerifyPrompt asks Muse Spark whether the edit changed the food. Image 1 is
// the original, image 2 the edit.
const VerifyPrompt = `Image 1 is the original photo. Image 2 is an edited version of it.
Compare only the food and its packaging. Return ONLY valid JSON:
{"same_food": true or false, "reason": "one sentence, 12 words maximum"}
same_food is true only if every food item, package, label and text in image 1 is in image 2,
image 2 adds nothing, the count is the same, and the color and freshness of the food look the same.
Light, white balance, sharpness and the background may differ.`

// Job states.
const (
	JobQueued       = "queued" // X44c: a batch job waits for a free slot
	JobRunning      = "running"
	JobDone         = "done"          // the edit passed the check: photo_id is the edit
	JobKeptOriginal = "kept_original" // the check said the food changed, or it could not check
	JobFailed       = "failed"        // the edit itself failed (nothing to offer)
)

// Job is a touch-up's state (the GET answer).
type Job struct {
	TouchupID     string  `json:"touchup_id"`
	RequestID     string  `json:"request_id"`
	Status        string  `json:"status"`
	Mode          string  `json:"mode"`
	SourcePhotoID string  `json:"source_photo_id,omitempty"`
	PhotoID       string  `json:"photo_id,omitempty"` // the edited photo (status done)
	Reason        string  `json:"reason,omitempty"`
	CostUSD       float64 `json:"cost_usd"`
	LatencyMS     int64   `json:"latency_ms"`
	ImageModel    string  `json:"image_model"`
	CheckModel    string  `json:"check_model,omitempty"`
	// X42d (T9, 2026-10-29): an edit that the check refused is kept for
	// review (GET .../rejected). The app never shows it as a listing photo.
	RejectedEdit bool `json:"rejected_edit,omitempty"`
	userID       string
	bearer       bool // made by a bearer caller (/v1): only bearer callers see it
	remoteIP     string
	forwardedIP  string
	capKey       string // X44a: the caller's daily-cap key
	ended        time.Time
}

// ImageEditor calls POST <Base>/images/edits (multipart, the OpenAI Images
// shape that Meta's endpoint mirrors).
type ImageEditor struct {
	Base      string
	Key       string
	Model     string // muse-image-1.0
	Reasoning string // reasoning_strength (high|low); "" = the API default
	HTTP      *http.Client
}

// Edit returns the edited image bytes and the call.
func (e *ImageEditor) Edit(ctx context.Context, img []byte, mime, prompt, size string) ([]byte, Call, error) {
	model := e.Model
	if model == "" {
		model = DefaultImage
	}
	call := Call{Purpose: "touchup:edit", Model: model}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fields := [][2]string{{"model", model}, {"prompt", prompt}, {"n", "1"},
		{"output_format", "jpeg"}, {"response_format", "b64_json"}}
	if size != "" {
		fields = append(fields, [2]string{"size", size})
	}
	if e.Reasoning != "" {
		fields = append(fields, [2]string{"reasoning_strength", e.Reasoning})
	}
	for _, f := range fields {
		_ = mw.WriteField(f[0], f[1])
	}
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="image"; filename="photo`+extFor(mime)+`"`)
	h.Set("Content-Type", mime)
	pw, _ := mw.CreatePart(h)
	_, _ = pw.Write(img)
	_ = mw.Close()
	base := e.Base
	if base == "" {
		base = DefaultBase
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/images/edits", &buf)
	if err != nil {
		return nil, call, err
	}
	req.Header.Set("Authorization", "Bearer "+e.Key)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	hc := e.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 120 * time.Second}
	}
	t0 := time.Now()
	resp, err := hc.Do(req)
	if err != nil {
		call.MS, call.Error = time.Since(t0).Milliseconds(), err.Error()
		return nil, call, fmt.Errorf("image edit: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 40<<20))
	call.MS, call.Status = time.Since(t0).Milliseconds(), resp.StatusCode
	if err != nil {
		call.Error = err.Error()
		return nil, call, fmt.Errorf("image edit read: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		call.Error = fmt.Sprintf("HTTP %d %s", resp.StatusCode, clip(string(raw), 300))
		return nil, call, fmt.Errorf("image edit: %s", call.Error)
	}
	var out struct {
		ID   string `json:"id"`
		Data []struct {
			B64 string `json:"b64_json"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || len(out.Data) == 0 || out.Data[0].B64 == "" {
		call.Error = "no image in the answer"
		return nil, call, errors.New("image edit: no image in the answer")
	}
	call.UpstreamID = out.ID
	call.Images = len(out.Data)
	call.Price()
	b, err := base64.StdEncoding.DecodeString(out.Data[0].B64)
	if err != nil || len(b) == 0 {
		call.Error = "image is not base64"
		return nil, call, errors.New("image edit: image is not base64")
	}
	return b, call, nil
}

func extFor(mime string) string {
	switch mime {
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	}
	return ".jpg"
}

// AspectSize is "WxH" of the image, for the edit's size field (it sets the
// aspect ratio only); "" when the size cannot be read (webp: no decoder in
// the standard library), and the API default applies.
func AspectSize(img []byte) string {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(img))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return ""
	}
	return fmt.Sprintf("%dx%d", cfg.Width, cfg.Height)
}

// AsJPEG returns b as JPEG: JPEG unchanged, PNG re-encoded (quality 92);
// any other type is an error (the listings photo_id path reads .jpg).
func AsJPEG(b []byte) ([]byte, error) {
	switch http.DetectContentType(b) {
	case "image/jpeg":
		return b, nil
	case "image/png":
		im, err := png.Decode(bytes.NewReader(b))
		if err != nil {
			return nil, err
		}
		var out bytes.Buffer
		if err := jpeg.Encode(&out, im, &jpeg.Options{Quality: 92}); err != nil {
			return nil, err
		}
		return out.Bytes(), nil
	}
	return nil, fmt.Errorf("edited image is %s, not jpeg or png", http.DetectContentType(b))
}

// ---- photo store -----------------------------------------------------------

var photoIDRe = regexp.MustCompile(`^[0-9a-f]{32}$`)

// ValidPhotoID is the analyze service's photo_id form (32 hex).
func ValidPhotoID(id string) bool { return photoIDRe.MatchString(id) }

// photoMeta is the item-photo sidecar (inference/photos.go fields that the
// listings service reads: photo_id, user_id), plus the touch-up record.
type photoMeta struct {
	PhotoID       string `json:"photo_id"`
	UserID        string `json:"user_id"`
	Kind          string `json:"kind,omitempty"`
	SourcePhotoID string `json:"source_photo_id,omitempty"`
	SourceSHA256  string `json:"source_sha256,omitempty"`
	EditedSHA256  string `json:"edited_sha256,omitempty"`
	Mode          string `json:"mode,omitempty"`
	ImageModel    string `json:"image_model,omitempty"`
	RequestID     string `json:"request_id,omitempty"`
	CreatedAt     string `json:"created_at,omitempty"`
}

// ReadItemPhoto reads an item photo of dir that belongs to userID (one
// error for unknown and not-owned: OWASP API1:2023).
func ReadItemPhoto(dir, id, userID string) ([]byte, error) {
	if userID == "" { // a session user always has an id; "" is the bearer owner
		return nil, errors.New("photo_id not found")
	}
	return readItemPhoto(dir, id, userID)
}

// ReadBearerItemPhoto is an item photo of a bearer caller (/v1/analyze owns
// its photos as user ""); X42e, for /v1/touchup only.
func ReadBearerItemPhoto(dir, id string) ([]byte, error) { return readItemPhoto(dir, id, "") }

func readItemPhoto(dir, id, userID string) ([]byte, error) {
	errNo := errors.New("photo_id not found")
	if dir == "" || !ValidPhotoID(id) {
		return nil, errNo
	}
	mb, err := os.ReadFile(filepath.Join(dir, id+".json"))
	if err != nil {
		return nil, errNo
	}
	var m photoMeta
	if json.Unmarshal(mb, &m) != nil || m.PhotoID != id || m.UserID != userID {
		return nil, errNo
	}
	b, err := os.ReadFile(filepath.Join(dir, id+".jpg"))
	if err != nil || len(b) == 0 || len(b) > MaxImage {
		return nil, errNo
	}
	return b, nil
}

var randReader io.Reader = rand.Reader

func newHex(n int) string {
	b := make([]byte, n)
	if _, err := io.ReadFull(randReader, b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func sha256hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// ---- the service -----------------------------------------------------------

// TouchupConfig is everything the touch-up handler needs (cmd/touchup).
type TouchupConfig struct {
	Editor      *ImageEditor
	Checker     *Muse           // nil = no check: every edit is kept_original (fails closed)
	Session     Authenticator   // the app's session cookie: /api/touchup...
	Auth        *analyze.Bearer // X42e: the bearer token: /v1/touchup... (nil = no bearer route)
	Limiter     *Limiter
	Budget      *Budget
	Events      *Events
	Dir         string // edited photos (a volume), item-photo format
	ItemPhotos  string // the analyze service's photo folder, read-only (the only input)
	MaxImage    int64
	MaxInFlight int
	JobTimeout  time.Duration // one touch-up, both calls included
	Build       string
	// X48o: the touch-up of the X48m page's "auto" column (inference.V2:
	// Muse Image bigger, checked in code by SSIM, then the scene rule's mask
	// and the studio blur). nil = the X35a edit + the Muse Spark check.
	// Photos is the analyze upload folder (read-only), AnalyzeEvents its
	// events (the scan's item names); both may be "" (then the item photo is
	// used as it is, with the general concepts).
	V2            *inference.V2
	Photos        string
	AnalyzeEvents string
	// X44c: batch jobs that wait for a slot (at most MaxQueued at a time,
	// each at most QueueWait; then it fails with nothing paid)
	MaxQueued int
	QueueWait time.Duration

	mu     sync.Mutex
	jobs   map[string]*Job
	slots  chan struct{}
	queued int
	now    func() time.Time
}

// MaxBatch is the most photos in one batch call (X44c): a scan's review list
// rarely has more items, and the queue is shared by every caller.
const MaxBatch = 6

// NewTouchup builds the mux.
func NewTouchup(c *TouchupConfig) http.Handler {
	if c.MaxImage == 0 || c.MaxImage > MaxImage {
		c.MaxImage = MaxImage
	}
	if c.MaxInFlight <= 0 {
		c.MaxInFlight = 2
	}
	if c.JobTimeout == 0 {
		c.JobTimeout = 90 * time.Second
	}
	if c.MaxQueued <= 0 {
		c.MaxQueued = 12
	}
	if c.QueueWait == 0 {
		c.QueueWait = 3 * time.Minute
	}
	if c.now == nil {
		c.now = time.Now
	}
	c.jobs = map[string]*Job{}
	c.slots = make(chan struct{}, c.MaxInFlight)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		WriteJSON(w, 200, map[string]any{"ok": true, "service": "touchup", "build": c.Build,
			"spent_today_usd": c.Budget.Spent(), "cap_usd": c.Budget.Cap, "caller_cap_usd": c.Budget.PerCaller,
			"image_model": c.Editor.Model, "v2": c.V2 != nil, "in_flight": len(c.slots), "queued": c.queuedNow()})
	})
	for _, p := range []string{"/api/touchup", "/v1/touchup"} { // X42e: /v1 = the bearer token
		mux.HandleFunc("POST "+p, c.start)
		mux.HandleFunc("POST "+p+"/batch", c.batch) // X44c: every photo of a scan, queued
		mux.HandleFunc("GET "+p+"/{id}", c.get)
		mux.HandleFunc("GET "+p+"/{id}/photo", c.photo)
		mux.HandleFunc("GET "+p+"/{id}/rejected", c.rejected) // X42d: a refused edit, for review
	}
	return mux
}

func (c *TouchupConfig) queuedNow() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.queued
}

func (c *TouchupConfig) record(ev Event) {
	ev.Service, ev.Build = "touchup", c.Build
	ev.SpentTodayUSD = c.Budget.Spent()
	c.Events.Record(ev)
}

func (c *TouchupConfig) start(w http.ResponseWriter, r *http.Request) {
	t0 := c.now()
	id := analyze.NewRequestID()
	w.Header().Set("X-Request-Id", id)
	ev := Event{RequestID: id, TS: t0.UTC().Format(time.RFC3339Nano), Route: r.URL.Path}
	ev.From(r)
	fail := func(status int, code, msg string) {
		ev.Status, ev.Code, ev.Error, ev.LatencyMS = status, code, msg, time.Since(t0).Milliseconds()
		WriteJSON(w, status, ErrorAnswer{RequestID: id, Error: msg, Code: code})
		c.record(ev)
	}
	who, ok := c.identify(r)
	if !ok {
		fail(http.StatusUnauthorized, CodeUnauthorized, "not logged in")
		return
	}
	ev.UserID, ev.CapKey = who.User.ID, who.RateKey
	if c.Limiter != nil && !c.Limiter.Allow(who.RateKey) {
		fail(http.StatusTooManyRequests, CodeRateLimited, "rate limited")
		return
	}
	mode := r.URL.Query().Get("mode")
	if mode == "" {
		mode = ModeLight
	}
	prompt, ok := EditPrompts[mode]
	if !ok {
		fail(http.StatusBadRequest, CodeBadRequest, "mode must be light or background")
		return
	}
	src := strings.TrimSpace(r.URL.Query().Get("photo_id"))
	if src == "" {
		fail(http.StatusBadRequest, CodeBadRequest, "photo_id is required (an item photo from the scan)")
		return
	}
	// X42c: only a real body is refused. A Content-Type with an empty body
	// is allowed: on the public path a request with no body reached the
	// service with "application/x-www-form-urlencoded" and Content-Length 0.
	if hasBody(r) {
		fail(http.StatusBadRequest, CodeBadRequest, "no body: the photo comes from photo_id")
		return
	}
	read := func() ([]byte, error) { return ReadItemPhoto(c.ItemPhotos, src, who.User.ID) }
	if who.Bearer {
		read = func() ([]byte, error) { return ReadBearerItemPhoto(c.ItemPhotos, src) }
	}
	img, err := read()
	if err != nil {
		fail(http.StatusNotFound, CodeNotFound, err.Error())
		return
	}
	mime := "image/jpeg"
	select {
	case c.slots <- struct{}{}:
	default:
		fail(http.StatusServiceUnavailable, "busy", "too many touch-ups at the same time")
		return
	}
	hold, why := c.Budget.ReserveFor(who.RateKey) // X44a: the service cap and this caller's cap
	if hold == nil {
		<-c.slots
		fail(CapRefusal(why))
		return
	}
	job := &Job{TouchupID: newHex(16), RequestID: id, Status: JobRunning, Mode: mode, SourcePhotoID: src,
		ImageModel: c.Editor.Model, userID: who.User.ID, bearer: who.Bearer, remoteIP: ev.RemoteIP, forwardedIP: ev.ForwardedIP,
		capKey: who.RateKey}
	if c.V2 != nil {
		job.CheckModel = "ssim" // X48o: the check is in code
	} else if c.Checker != nil {
		job.CheckModel = c.Checker.Model
	}
	c.mu.Lock()
	c.pruneLocked()
	c.jobs[job.TouchupID] = job
	c.mu.Unlock()
	snapshot := *job
	go c.run(job, img, mime, prompt, hold, t0)
	WriteJSON(w, http.StatusAccepted, map[string]any{"request_id": id, "touchup_id": snapshot.TouchupID,
		"status": snapshot.Status, "mode": mode})
}

// batch is POST .../batch (X44c, F1 "no visible waits": the app starts the
// touch-up of every item when the scan ends, while the donor reads the
// list). The checks are the ones of start, for every photo, before any job:
// one unknown or not-owned photo_id refuses the whole call (one 404, so an id
// cannot be probed). The rate limit counts the call once; the daily caps
// count every job. A job that finds no free slot waits in the queue.
func (c *TouchupConfig) batch(w http.ResponseWriter, r *http.Request) {
	t0 := c.now()
	id := analyze.NewRequestID()
	w.Header().Set("X-Request-Id", id)
	ev := Event{RequestID: id, TS: t0.UTC().Format(time.RFC3339Nano), Route: r.URL.Path}
	ev.From(r)
	fail := func(status int, code, msg string) {
		ev.Status, ev.Code, ev.Error, ev.LatencyMS = status, code, msg, time.Since(t0).Milliseconds()
		WriteJSON(w, status, ErrorAnswer{RequestID: id, Error: msg, Code: code})
		c.record(ev)
	}
	who, ok := c.identify(r)
	if !ok {
		fail(http.StatusUnauthorized, CodeUnauthorized, "not logged in")
		return
	}
	ev.UserID, ev.CapKey = who.User.ID, who.RateKey
	if c.Limiter != nil && !c.Limiter.Allow(who.RateKey) {
		fail(http.StatusTooManyRequests, CodeRateLimited, "rate limited")
		return
	}
	mode := r.URL.Query().Get("mode")
	if mode == "" {
		mode = ModeLight
	}
	prompt, ok := EditPrompts[mode]
	if !ok {
		fail(http.StatusBadRequest, CodeBadRequest, "mode must be light or background")
		return
	}
	var ids []string
	seen := map[string]bool{}
	for _, p := range strings.Split(r.URL.Query().Get("photo_ids"), ",") {
		if p = strings.TrimSpace(p); p == "" {
			continue
		}
		if seen[p] {
			fail(http.StatusBadRequest, CodeBadRequest, "a photo_id is twice in photo_ids")
			return
		}
		seen[p] = true
		ids = append(ids, p)
	}
	if len(ids) == 0 || len(ids) > MaxBatch {
		fail(http.StatusBadRequest, CodeBadRequest, fmt.Sprintf("photo_ids must have 1 to %d item photos from the scan", MaxBatch))
		return
	}
	if hasBody(r) {
		fail(http.StatusBadRequest, CodeBadRequest, "no body: the photos come from photo_ids")
		return
	}
	imgs := make([][]byte, len(ids))
	for i, src := range ids {
		var err error
		if who.Bearer {
			imgs[i], err = ReadBearerItemPhoto(c.ItemPhotos, src)
		} else {
			imgs[i], err = ReadItemPhoto(c.ItemPhotos, src, who.User.ID)
		}
		if err != nil {
			fail(http.StatusNotFound, CodeNotFound, err.Error())
			return
		}
	}
	type started struct {
		PhotoID   string `json:"photo_id"`
		TouchupID string `json:"touchup_id"`
		Status    string `json:"status"`
	}
	type skip struct {
		PhotoID string `json:"photo_id"`
		Code    string `json:"code"`
		Error   string `json:"error"`
	}
	jobs, skipped := []started{}, []skip{}
	firstSkip := 0
	for i, src := range ids {
		c.mu.Lock()
		full := c.queued >= c.MaxQueued
		if !full {
			c.queued++
		}
		c.mu.Unlock()
		if full {
			skipped = append(skipped, skip{src, "busy", "the touch-up queue is full; try again soon"})
			if firstSkip == 0 {
				firstSkip = http.StatusServiceUnavailable
			}
			continue
		}
		hold, why := c.Budget.ReserveFor(who.RateKey)
		if hold == nil {
			c.mu.Lock()
			c.queued--
			c.mu.Unlock()
			st, code, msg := CapRefusal(why)
			skipped = append(skipped, skip{src, code, msg})
			if firstSkip == 0 {
				firstSkip = st
			}
			continue
		}
		job := &Job{TouchupID: newHex(16), RequestID: id, Status: JobQueued, Mode: mode, SourcePhotoID: src,
			ImageModel: c.Editor.Model, userID: who.User.ID, bearer: who.Bearer, remoteIP: ev.RemoteIP,
			forwardedIP: ev.ForwardedIP, capKey: who.RateKey}
		if c.V2 != nil {
			job.CheckModel = "ssim" // X48o: the check is in code
		} else if c.Checker != nil {
			job.CheckModel = c.Checker.Model
		}
		c.mu.Lock()
		c.pruneLocked()
		c.jobs[job.TouchupID] = job
		c.mu.Unlock()
		jobs = append(jobs, started{src, job.TouchupID, JobQueued})
		go c.queue(job, imgs[i], "image/jpeg", prompt, hold, t0)
	}
	ev.Detail = map[string]any{"batch": len(ids), "touchup_ids": jobs, "skipped": skipped, "mode": mode}
	if len(jobs) == 0 { // nothing started: the answer of the first refusal
		code, msg := skipped[0].Code, skipped[0].Error
		ev.Status, ev.Code, ev.Error, ev.LatencyMS = firstSkip, code, msg, time.Since(t0).Milliseconds()
		WriteJSON(w, firstSkip, map[string]any{"request_id": id, "error": msg, "code": code, "skipped": skipped})
		c.record(ev)
		return
	}
	ev.Status, ev.LatencyMS = http.StatusAccepted, time.Since(t0).Milliseconds()
	WriteJSON(w, http.StatusAccepted, map[string]any{"request_id": id, "mode": mode, "jobs": jobs, "skipped": skipped})
	c.record(ev)
}

// queue waits for a free slot (at most QueueWait), then runs the job.
func (c *TouchupConfig) queue(job *Job, img []byte, mime, prompt string, hold *Hold, t0 time.Time) {
	timer := time.NewTimer(c.QueueWait)
	defer timer.Stop()
	select {
	case c.slots <- struct{}{}:
		c.mu.Lock()
		c.queued--
		job.Status = JobRunning
		c.mu.Unlock()
		c.run(job, img, mime, prompt, hold, t0)
	case <-timer.C:
		hold.Settle(0)
		ms := time.Since(t0).Milliseconds()
		c.mu.Lock()
		c.queued--
		job.Status, job.Reason, job.LatencyMS, job.ended = JobFailed, "waited too long for a free slot", ms, c.now()
		snap := *job
		c.mu.Unlock()
		c.record(Event{RequestID: job.RequestID, TS: t0.UTC().Format(time.RFC3339Nano), Route: "touchup.job",
			UserID: job.userID, RemoteIP: job.remoteIP, ForwardedIP: job.forwardedIP, CapKey: job.capKey,
			Status: 200, LatencyMS: ms, Detail: map[string]any{"touchup_id": snap.TouchupID, "status": snap.Status,
				"mode": snap.Mode, "reason": snap.Reason, "source_photo_id": snap.SourcePhotoID}})
	}
}

// run does the edit and the check, then stores the result. It owns one slot.
func (c *TouchupConfig) run(job *Job, img []byte, mime, prompt string, hold *Hold, t0 time.Time) {
	defer func() { <-c.slots }()
	ctx, cancel := context.WithTimeout(context.Background(), c.JobTimeout)
	defer cancel()
	if c.V2 != nil {
		c.runV2(ctx, job, img, hold, t0)
		return
	}
	var calls []Call
	status, reason, photoID := JobFailed, "", ""
	edited, call, err := c.Editor.Edit(ctx, img, mime, prompt, AspectSize(img))
	calls = append(calls, call)
	switch {
	case err != nil:
		reason = "the edit failed"
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			reason = "the edit took too long"
		}
	default:
		edited, err = AsJPEG(edited)
		if err != nil {
			status, reason = JobKeptOriginal, "the edit is not a JPEG or PNG"
			break
		}
		if c.Checker == nil {
			status, reason = JobKeptOriginal, "no check configured"
			break
		}
		obj, ccall, cerr := c.Checker.AskJSON(ctx, "touchup:check", VerifyPrompt,
			Image{Bytes: img, Mime: mime}, Image{Bytes: edited, Mime: "image/jpeg"})
		calls = append(calls, ccall)
		same, isBool := obj["same_food"].(bool)
		switch {
		case cerr != nil || !isBool:
			status, reason = JobKeptOriginal, "the check could not run"
		case !same:
			status, reason = JobKeptOriginal, clip(Str(obj, "reason"), 160)
			if reason == "" {
				reason = "the food changed"
			}
			if c.storeRejected(job, img, edited, reason) == nil {
				c.mu.Lock()
				job.RejectedEdit = true
				c.mu.Unlock()
			}
		default:
			photoID = newHex(16)
			if err := c.store(job, photoID, img, edited); err != nil {
				status, reason, photoID = JobKeptOriginal, "could not store the edit", ""
			} else {
				status, reason = JobDone, clip(Str(obj, "reason"), 160)
			}
		}
	}
	cost := Sum(calls)
	hold.Settle(cost)
	ms := time.Since(t0).Milliseconds()
	c.mu.Lock()
	job.Status, job.Reason, job.PhotoID, job.CostUSD, job.LatencyMS = status, reason, photoID, cost, ms
	job.ended = c.now()
	snap := *job
	c.mu.Unlock()
	c.record(Event{RequestID: job.RequestID, TS: t0.UTC().Format(time.RFC3339Nano), Route: "touchup.job",
		UserID: job.userID, RemoteIP: job.remoteIP, ForwardedIP: job.forwardedIP, CapKey: job.capKey, Status: 200, LatencyMS: ms, CostUSD: cost, Calls: calls,
		Detail: map[string]any{"touchup_id": snap.TouchupID, "status": snap.Status, "mode": snap.Mode,
			"reason": snap.Reason, "source_photo_id": snap.SourcePhotoID, "photo_id": snap.PhotoID,
			"source_sha256": sha256hex(img)}})
}

// runV2 is run for X48o: the item is cut again from the upload (LoadItem),
// then V2.Touchup. Its result is always a code-made picture with every food
// pixel of the photo or of an edit that kept the photo's structure (SSIM), so
// it is stored as done; the reason says which mask the scene rule chose.
// "light" is the Muse bigger edit only (no mask, no new background).
func (c *TouchupConfig) runV2(ctx context.Context, job *Job, img []byte, hold *Hold, t0 time.Time) {
	it := inference.LoadItem(c.Photos, c.AnalyzeEvents, c.ItemPhotos, job.SourcePhotoID, img, WiderUnder, WiderMargin)
	it.Light = job.Mode == ModeLight
	r, err := c.V2.Touchup(ctx, it)
	status, reason, photoID := JobFailed, "", ""
	switch {
	case err != nil:
		reason = "the edit failed"
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			reason = "the edit took too long"
		}
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		status, reason = JobKeptOriginal, "the edit took too long"
	default:
		photoID = newHex(16)
		if err := c.store(job, photoID, img, r.JPEG); err != nil {
			status, reason, photoID = JobKeptOriginal, "could not store the edit", ""
		} else {
			status, reason = JobDone, clip(r.Mask, 160)
		}
	}
	cost := r.CostUSD
	hold.Settle(cost)
	ms := time.Since(t0).Milliseconds()
	c.mu.Lock()
	job.Status, job.Reason, job.PhotoID, job.CostUSD, job.LatencyMS = status, reason, photoID, cost, ms
	job.ended = c.now()
	snap := *job
	c.mu.Unlock()
	c.record(Event{RequestID: job.RequestID, TS: t0.UTC().Format(time.RFC3339Nano), Route: "touchup.job",
		UserID: job.userID, RemoteIP: job.remoteIP, ForwardedIP: job.forwardedIP, CapKey: job.capKey, Status: 200, LatencyMS: ms, CostUSD: cost,
		Detail: map[string]any{"touchup_id": snap.TouchupID, "status": snap.Status, "mode": snap.Mode,
			"reason": snap.Reason, "source_photo_id": snap.SourcePhotoID, "photo_id": snap.PhotoID,
			"source_sha256": sha256hex(img), "v2": true, "mask": r.Mask, "ssim": r.SSIM, "muse": r.Muse, "note": clip(r.Note, 240)}})
}

// WiderUnder, WiderMargin: an item photo with the long edge under WiderUnder
// px is cut again with the margin WiderMargin (the X48j-X48m pages: -wider-under
// 500, -wider-margin 0.35).
const (
	WiderUnder  = 500
	WiderMargin = 0.35
)

// JobKeep is how long a finished job stays readable (the edited photo file
// stays on the volume).
const JobKeep = 24 * time.Hour

func (c *TouchupConfig) pruneLocked() {
	now := c.now()
	for id, j := range c.jobs {
		if j.Status != JobRunning && !j.ended.IsZero() && now.Sub(j.ended) > JobKeep {
			delete(c.jobs, id)
		}
	}
}

func (c *TouchupConfig) store(job *Job, photoID string, src, edited []byte) error {
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return err
	}
	if err := WriteFileSync(filepath.Join(c.Dir, photoID+".jpg"), edited, 0o644); err != nil {
		return err
	}
	m := photoMeta{PhotoID: photoID, UserID: job.userID, Kind: "touchup",
		SourcePhotoID: job.SourcePhotoID, SourceSHA256: sha256hex(src), EditedSHA256: sha256hex(edited),
		Mode: job.Mode, ImageModel: job.ImageModel, RequestID: job.RequestID,
		CreatedAt: c.now().UTC().Format(time.RFC3339Nano)}
	b, _ := json.Marshal(m)
	return WriteFileSync(filepath.Join(c.Dir, photoID+".json"), b, 0o644)
}

// storeRejected keeps an edit that the check refused, for review only:
// <Dir>/rejected/<touchup_id>.jpg + .json. It is not an item photo (not in
// <Dir> itself), so a listing can never use it.
func (c *TouchupConfig) storeRejected(job *Job, src, edited []byte, reason string) error {
	dir := filepath.Join(c.Dir, "rejected")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := WriteFileSync(filepath.Join(dir, job.TouchupID+".jpg"), edited, 0o644); err != nil {
		return err
	}
	m := map[string]any{"touchup_id": job.TouchupID, "user_id": job.userID, "source_photo_id": job.SourcePhotoID,
		"source_sha256": sha256hex(src), "edited_sha256": sha256hex(edited), "mode": job.Mode,
		"image_model": job.ImageModel, "reason": reason, "created_at": c.now().UTC().Format(time.RFC3339Nano)}
	b, _ := json.Marshal(m)
	return WriteFileSync(filepath.Join(dir, job.TouchupID+".json"), b, 0o644)
}

// rejected serves a refused edit to the owner of the job, for review.
func (c *TouchupConfig) rejected(w http.ResponseWriter, r *http.Request) {
	j, ok := c.owned(w, r)
	if !ok {
		return
	}
	if !j.RejectedEdit {
		WriteJSON(w, http.StatusConflict, ErrorAnswer{RequestID: analyze.NewRequestID(), Error: "no refused edit (status " + j.Status + ")", Code: CodeConflict})
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeFile(w, r, filepath.Join(c.Dir, "rejected", j.TouchupID+".jpg"))
}

// owned finds job id for the caller (one 404 for unknown and not-owned).
// identify: /v1/... takes only the bearer token, /api/... only the session
// cookie (X42e, as the voice service does).
func (c *TouchupConfig) identify(r *http.Request) (Caller, bool) {
	if strings.HasPrefix(r.URL.Path, "/v1/") {
		if c.Auth == nil {
			return Caller{}, false
		}
		return Identify(r, nil, c.Auth)
	}
	return Identify(r, c.Session, nil)
}

func (c *TouchupConfig) owned(w http.ResponseWriter, r *http.Request) (Job, bool) {
	who, ok := c.identify(r)
	if !ok {
		WriteJSON(w, http.StatusUnauthorized, ErrorAnswer{RequestID: analyze.NewRequestID(), Error: "not logged in", Code: CodeUnauthorized})
		return Job{}, false
	}
	c.mu.Lock()
	j, found := c.jobs[r.PathValue("id")]
	var snap Job
	if found {
		snap = *j
	}
	c.mu.Unlock()
	if !found || snap.userID != who.User.ID || snap.bearer != who.Bearer {
		WriteJSON(w, http.StatusNotFound, ErrorAnswer{RequestID: analyze.NewRequestID(), Error: "touch-up not found", Code: CodeNotFound})
		return Job{}, false
	}
	return snap, true
}

func (c *TouchupConfig) get(w http.ResponseWriter, r *http.Request) {
	if j, ok := c.owned(w, r); ok {
		WriteJSON(w, http.StatusOK, j)
	}
}

func (c *TouchupConfig) photo(w http.ResponseWriter, r *http.Request) {
	j, ok := c.owned(w, r)
	if !ok {
		return
	}
	if j.Status != JobDone || j.PhotoID == "" {
		WriteJSON(w, http.StatusConflict, ErrorAnswer{RequestID: analyze.NewRequestID(), Error: "no edited photo (status " + j.Status + ")", Code: CodeConflict})
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeFile(w, r, filepath.Join(c.Dir, j.PhotoID+".jpg"))
}

// hasBody reports whether the request carries at least one byte of body
// (a known length over 0, or a chunked body that is not empty).
func hasBody(r *http.Request) bool {
	if r.ContentLength > 0 {
		return true
	}
	if r.ContentLength == 0 || r.Body == nil || r.Body == http.NoBody {
		return false
	}
	var b [1]byte
	n, _ := io.ReadFull(r.Body, b[:])
	return n > 0
}
