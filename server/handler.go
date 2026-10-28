package analyze

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// Config is everything the handler needs; cmd/analyze fills it from flags.
type Config struct {
	Upstream   *Upstream
	Auth       *Bearer
	Limiter    *Limiter
	Recorder   *Recorder
	Origin     string        // CORS Access-Control-Allow-Origin; "" = no CORS headers
	MaxImage   int64         // bytes
	Timeout    time.Duration // per request, the API call included
	ServingDef float64       // the number_of_serving of every answer
}

// AllowedMimes are the image types https://dev.meta.ai/docs/image-understanding
// accepts.
var AllowedMimes = map[string]bool{
	"image/jpeg": true, "image/jpg": true, "image/png": true, "image/gif": true, "image/webp": true,
}

// Response is the 200 body: the contract and the ids the app keeps.
type Response struct {
	RequestID string `json:"request_id"`
	Result
	Model     string `json:"model"`
	LatencyMS int64  `json:"latency_ms"`
}

// ErrorBody is every body that is not 200.
type ErrorBody struct {
	RequestID string `json:"request_id"`
	Error     string `json:"error"`
}

// New makes the routes: GET /healthz (no auth) and POST /v1/analyze.
func New(c Config) http.Handler {
	if c.MaxImage == 0 {
		c.MaxImage = 5 << 20
	}
	if c.Timeout == 0 {
		c.Timeout = 75 * time.Second
	}
	if c.ServingDef == 0 {
		c.ServingDef = DefaultServings
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "model": c.Upstream.Model})
	})
	mux.HandleFunc("/v1/analyze", c.analyze)
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// cors sets the CORS headers for the one allowed origin.
func (c Config) cors(w http.ResponseWriter) {
	if c.Origin == "" {
		return
	}
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", c.Origin)
	h.Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
	h.Set("Access-Control-Max-Age", "600")
	h.Set("Vary", "Origin")
}

// imageError is a refused photo: the status and the text for the caller.
type imageError struct {
	status int
	msg    string
}

// ReadImage reads the photo of a request: multipart/form-data with the
// field "image", or JSON {"image_base64","mime"}. The body is capped before
// any decoding (https://pkg.go.dev/net/http#MaxBytesReader); the cap allows
// for base64 (4/3) and the form fields. The inference service uses it, so
// its photos have the same formats and limits as /v1/analyze.
func (c Config) ReadImage(r *http.Request) (img []byte, mimeType string, status int, msg string) {
	r.Body = http.MaxBytesReader(nil, r.Body, c.MaxImage*4/3+4096)
	img, mimeType, e := c.decodeImage(r)
	if e == nil {
		e = c.checkImage(img, &mimeType)
	}
	if e != nil {
		return nil, mimeType, e.status, e.msg
	}
	return img, mimeType, 0, ""
}

// decodeImage takes the photo bytes and the declared type out of the body.
func (c Config) decodeImage(r *http.Request) ([]byte, string, *imageError) {
	// A prefix match: a boundary or a charset can follow the type.
	ctype := r.Header.Get("Content-Type")
	switch {
	case strings.HasPrefix(ctype, "multipart/form-data"):
		if err := r.ParseMultipartForm(c.MaxImage); err != nil {
			return nil, "", &imageError{http.StatusRequestEntityTooLarge, "image too large or bad multipart"}
		}
		f, hdr, err := r.FormFile("image")
		if err != nil {
			return nil, "", &imageError{http.StatusBadRequest, "multipart field 'image' missing"}
		}
		defer f.Close()
		img, err := io.ReadAll(io.LimitReader(f, c.MaxImage+1))
		if err != nil {
			return nil, "", &imageError{http.StatusBadRequest, "image unreadable"}
		}
		part, _, _ := strings.Cut(hdr.Header.Get("Content-Type"), ";")
		return img, strings.ToLower(strings.TrimSpace(part)), nil
	case strings.HasPrefix(ctype, "application/json"):
		var body struct {
			ImageBase64 string `json:"image_base64"`
			Mime        string `json:"mime"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			return nil, "", &imageError{http.StatusBadRequest, "bad JSON body"}
		}
		img, err := base64.StdEncoding.DecodeString(strings.TrimSpace(body.ImageBase64))
		if err != nil {
			return nil, "", &imageError{http.StatusBadRequest, "image_base64 is not base64"}
		}
		return img, strings.ToLower(strings.TrimSpace(body.Mime)), nil
	}
	return nil, "", &imageError{http.StatusUnsupportedMediaType, "send multipart/form-data or application/json"}
}

// checkImage finds the type when none was declared, then applies the type,
// size and empty checks, in this order.
func (c Config) checkImage(img []byte, mimeType *string) *imageError {
	if *mimeType == "" {
		*mimeType = http.DetectContentType(img)
	}
	switch {
	case !AllowedMimes[*mimeType]:
		return &imageError{http.StatusUnsupportedMediaType, "unsupported image type " + *mimeType}
	case int64(len(img)) > c.MaxImage:
		return &imageError{http.StatusRequestEntityTooLarge, "image too large"}
	case len(img) == 0:
		return &imageError{http.StatusBadRequest, "empty image"}
	}
	return nil
}

// analyze is POST /v1/analyze: auth, rate limit, photo, one Meta call, the
// checked contract with number_of_serving set. Every request writes one
// metrics line, also a refused one.
func (c Config) analyze(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	id := NewRequestID()
	w.Header().Set("X-Request-Id", id)
	c.cors(w)
	remote, fwd := ClientIPs(r)
	line := Line{RequestID: id, TS: start.UTC().Format(time.RFC3339Nano), RemoteIP: remote,
		ForwardedIP: fwd, Method: r.Method, Path: r.URL.Path, Model: c.Upstream.Model}
	fail := func(status int, msg string) {
		line.Status, line.Error = status, msg
		line.LatencyMS = time.Since(start).Milliseconds()
		writeJSON(w, status, ErrorBody{RequestID: id, Error: msg})
		c.record(line)
	}

	switch {
	case r.Method == http.MethodOptions:
		w.WriteHeader(http.StatusNoContent)
		return
	case r.Method != http.MethodPost:
		fail(http.StatusMethodNotAllowed, "POST only")
		return
	case !c.Auth.OK(r):
		fail(http.StatusUnauthorized, "missing or bad bearer token")
		return
	}
	key := remote
	if fwd != "" {
		key = fwd
	}
	if c.Limiter != nil && !c.Limiter.Allow(key) {
		fail(http.StatusTooManyRequests, "rate limited")
		return
	}
	img, mimeType, status, msg := c.ReadImage(r)
	line.ImageBytes, line.Mime = len(img), mimeType
	if status != 0 {
		fail(status, msg)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), c.Timeout)
	defer cancel()
	ans, err := c.Upstream.Analyze(ctx, img, mimeType)
	line.UpstreamMS = ans.Elapsed.Milliseconds()
	line.UpstreamStatus = ans.Status
	line.UpstreamID = ans.RequestID
	line.InputTokens = ans.Usage.InputTokens
	line.OutputTokens = ans.Usage.OutputTokens
	line.ReasoningTokens = ans.Usage.ReasoningTokens
	line.SchemaValid = ans.Valid
	if err != nil {
		fail(http.StatusBadGateway, err.Error())
		return
	}
	res := ans.Result
	res.NumberOfServing = c.ServingDef
	line.Status, line.Name = http.StatusOK, res.Name
	line.LatencyMS = time.Since(start).Milliseconds()
	writeJSON(w, http.StatusOK, Response{RequestID: id, Result: res, Model: c.Upstream.Model, LatencyMS: line.LatencyMS})
	c.record(line)
}

// record writes the metrics line; a failure is a log line, not a failed request.
func (c Config) record(l Line) {
	if c.Recorder == nil {
		return
	}
	if err := c.Recorder.Write(l); err != nil {
		log.Printf("metrics write failed: %v (request %s)", err, l.RequestID)
	}
}
