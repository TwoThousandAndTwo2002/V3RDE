package inference

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// X32g (T9, 2026-10-27): the app reports what the user did with an answer --
// the final title and servings of each item, the items removed, posted or
// not -- and the service logs it next to the answer's own event (same
// request_id), so thresholds can be tuned on real corrections. The report
// never changes a listing; it is bounded (16 KB, 24 items, 120-character
// titles) and logged as sent after these checks.
//
//	POST /api/analyze/labels (session) | /v1/analyze/labels (bearer)
//	{"request_id": "<32 hex>", "outcome": "posted|retake|closed", "locale": "en-US",
//	 "items": [{"item":1, "photo_id":"…", "model_title":"…", "final_title":"…",
//	            "model_servings":3, "final_servings":2, "kept":true}]}  -> 204

// CodeLabelsTooLarge: a labels report over 16 KB (413).
const CodeLabelsTooLarge = "labels_too_large"

const (
	maxLabelBody  = 16 << 10
	maxLabelItems = 24
	maxLabelText  = 120
)

var (
	labelReqRe    = regexp.MustCompile(`^[0-9a-f]{32}$`)
	labelLocaleRe = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8}){0,3}$`)
)

type labelsIn struct {
	RequestID string      `json:"request_id"`
	Outcome   string      `json:"outcome"`
	Locale    string      `json:"locale"`
	Items     []LabelItem `json:"items"`
}

func clip(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxLabelText {
		return s
	}
	s = s[:maxLabelText]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

func servings(n int) int {
	if n < 0 {
		return 0
	}
	if n > 99 {
		return 99
	}
	return n
}

// CheckLabels validates and bounds a report; "" = ok, else the reason.
func CheckLabels(in *labelsIn) string {
	switch {
	case !labelReqRe.MatchString(in.RequestID):
		return "request_id must be the analyze request_id (32 hex)"
	case in.Outcome != "posted" && in.Outcome != "retake" && in.Outcome != "closed":
		return "outcome must be posted, retake or closed"
	case len(in.Items) > maxLabelItems:
		return "at most 24 items"
	case in.Locale != "" && !labelLocaleRe.MatchString(in.Locale):
		return "locale must be a language tag like en-US"
	}
	for i := range in.Items {
		it := &in.Items[i]
		if it.Item < 1 || it.Item > 99 || (it.PhotoID != "" && !labelReqRe.MatchString(it.PhotoID)) {
			return "each item needs its number (1-99) and a 32-hex photo_id or none"
		}
		it.ModelTitle, it.FinalTitle = clip(it.ModelTitle), clip(it.FinalTitle)
		it.ModelServings, it.FinalServings = servings(it.ModelServings), servings(it.FinalServings)
	}
	return ""
}

func (c *ServiceConfig) labels(w http.ResponseWriter, r *http.Request) {
	c.cors(w)
	kind, u, ok := c.caller(r)
	if !ok {
		writeJSONResp(w, http.StatusUnauthorized, ErrorAnswer{Error: "sign in, or send the bearer token", Code: CodeUnauthorized})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxLabelBody+1))
	if err != nil || len(body) > maxLabelBody {
		writeJSONResp(w, http.StatusRequestEntityTooLarge, ErrorAnswer{Error: "labels over 16 KB", Code: CodeLabelsTooLarge})
		return
	}
	var in labelsIn
	if json.Unmarshal(body, &in) != nil {
		writeJSONResp(w, http.StatusBadRequest, ErrorAnswer{Error: "labels must be JSON", Code: CodeBadRequest})
		return
	}
	if why := CheckLabels(&in); why != "" {
		writeJSONResp(w, http.StatusBadRequest, ErrorAnswer{Error: why, Code: CodeBadRequest})
		return
	}
	if in.Items == nil {
		in.Items = []LabelItem{}
	}
	c.Events.Record(Event{TS: time.Now().UTC().Format(time.RFC3339Nano), Service: "analyze", Step: "labels",
		RequestID: in.RequestID, Status: http.StatusNoContent, Route: "labels", Caller: kind, UserID: u.ID,
		Model: c.Engine.Muse, Build: c.Build, Labels: &LabelSet{Outcome: in.Outcome, Locale: in.Locale, Items: in.Items}})
	w.WriteHeader(http.StatusNoContent)
}
