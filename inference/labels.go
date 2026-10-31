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
// titles) and logged as sent after these checks. X48u: 32 KB, and "edits"
// (at most 64, in order) and "chat" (at most 16 lines of 500 bytes). X49: an
// edit can also be a change of the item details (condition, diet, portions,
// weight), with the old and new values.
//
//	POST /api/analyze/labels (session) | /v1/analyze/labels (bearer)
//	{"request_id": "<32 hex>", "outcome": "posted|retake|closed", "locale": "en-US",
//	 "items": [{"item":1, "photo_id":"…", "model_title":"…", "final_title":"…",
//	            "model_servings":3, "final_servings":2, "kept":true, "photo":"touched_up", "touchup":"done"}],
//	 "edits": [{"item":1, "field":"servings", "from":"3", "to":"2", "how":"voice"}],
//	 "chat":  [{"how":"voice", "text":"two servings of item 1", "changes":1}]}  -> 204

// CodeLabelsTooLarge: a labels report over 16 KB (413).
const CodeLabelsTooLarge = "labels_too_large"

const (
	maxLabelBody  = 32 << 10 // X48u: edits and chat (was 16 KB)
	maxLabelItems = 24
	maxLabelText  = 120
	maxLabelEdits = 64  // X48u
	maxLabelChat  = 16  // X48u
	maxChatText   = 500 // X48u: bytes of one chat line
)

var (
	labelFields = map[string]bool{"title": true, "servings": true, "remove": true, "pickup": true, "photo": true,
		"condition": true, "diet": true, "portions": true, "weight": true} // X49: the item details
	labelHows   = map[string]bool{"hand": true, "voice": true, "text": true, "undo": true}
	labelPhotos = map[string]bool{"": true, "original": true, "touched_up": true}
	labelTouch  = map[string]bool{"": true, "done": true, "kept": true, "failed": true}
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
	Edits     []LabelEdit `json:"edits"`
	Chat      []LabelChat `json:"chat"`
}

func clip(s string) string { return clipTo(s, maxLabelText) }

func clipTo(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	s = s[:n]
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
	case len(in.Edits) > maxLabelEdits:
		return "at most 64 edits"
	case len(in.Chat) > maxLabelChat:
		return "at most 16 chat lines"
	}
	for i := range in.Edits {
		e := &in.Edits[i]
		if e.Item < 0 || e.Item > 99 || !labelFields[e.Field] || !labelHows[e.How] {
			return "each edit needs an item (0-99), a field (title, servings, remove, pickup, photo, condition, diet, portions, weight) and how (hand, voice, text, undo)"
		}
		e.From, e.To = clip(e.From), clip(e.To)
		if e.Field == "pickup" { // never the pickup text (an address): only that it changed and how
			e.From, e.To = "", ""
		}
	}
	for i := range in.Chat {
		c := &in.Chat[i]
		if (c.How != "voice" && c.How != "text") || c.Changes < 0 || c.Changes > 99 {
			return "each chat line needs how (voice, text) and changes (0-99)"
		}
		c.Text = clipTo(c.Text, maxChatText)
		if c.Pickup {
			c.Text = "" // the words of a line that set the pickup may hold an address
		}
	}
	for i := range in.Items {
		it := &in.Items[i]
		if it.Item < 1 || it.Item > 99 || (it.PhotoID != "" && !labelReqRe.MatchString(it.PhotoID)) {
			return "each item needs its number (1-99) and a 32-hex photo_id or none"
		}
		it.ModelTitle, it.FinalTitle = clip(it.ModelTitle), clip(it.FinalTitle)
		it.ModelServings, it.FinalServings = servings(it.ModelServings), servings(it.FinalServings)
		if !labelPhotos[it.Photo] || !labelTouch[it.Touchup] {
			return "photo must be original or touched_up, touchup done, kept or failed"
		}
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
		writeJSONResp(w, http.StatusRequestEntityTooLarge, ErrorAnswer{Error: "labels over 32 KB", Code: CodeLabelsTooLarge})
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
		Model: c.Engine.Muse, Build: c.Build, Labels: &LabelSet{Outcome: in.Outcome, Locale: in.Locale, Items: in.Items, Edits: in.Edits, Chat: in.Chat}})
	w.WriteHeader(http.StatusNoContent)
}
