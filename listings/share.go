package listings

// The share hook (N3, T9 2026-10-31: the Facebook post starts with "Every new
// listing (server)", and the post has the claim link only). After a listing
// is created, the listings service asks the fan-out service to post it to the
// Facebook Page: POST <URL><listing_id> with the giver's own session cookie
// (only that cookie), the same route and owner check as a tap in the app
// (post/fanout.go, POST /api/fanout/{listing_id}). It runs in the background,
// so the create answer does not wait for Muse or Facebook. It tries again
// (N3 review) on no answer, 429 and a 5xx other than 503 (fan-out restarts,
// listings or the photo not there): fan-out's Begin makes a second post
// impossible (409 already_posted, or conflict while one runs), and those
// answers come before the model calls. 503 (no Page: before any model call,
// no cost), 409, 404 and any other answer end it. The test accounts (X44h)
// are never posted.

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// ShareHook posts a new listing to the fan-out service.
type ShareHook struct {
	URL  string // for example http://fanout:8090/api/fanout/ ; "" = off
	HTTP *http.Client
	// Backoff is the wait before each new try (the first try is at once); nil = ShareBackoff.
	Backoff []time.Duration
	// Done, when set, gets the result (tests, logs).
	Done func(listingID string, status int, err error)
}

// ShareBackoff: 3 more tries in 85 s.
var ShareBackoff = []time.Duration{5 * time.Second, 20 * time.Second, 60 * time.Second}

// shareAgain reports whether a try with this result is followed by another.
func shareAgain(status int, err error) bool {
	return err != nil || status == http.StatusTooManyRequests || (status >= 500 && status != http.StatusServiceUnavailable)
}

// Changed (X60) asks fan-out to change the post of listingID after the owner
// changed the listing (PUT <URL><listing_id>, the same cookie and tries as
// Share). Fan-out reads the listing again, so a later change wins.
func (h *ShareHook) Changed(listingID string, session *http.Cookie) {
	h.send(http.MethodPut, listingID, session)
}

// ChangeBackoff (X60 review): 4 more tries in 145 s, longer than the longest
// share (DefaultBackoff note: 95 s), so a change while the first post is
// still being published (409 "pending") is not lost.
var ChangeBackoff = []time.Duration{5 * time.Second, 20 * time.Second, 60 * time.Second, 60 * time.Second}

// pendingCode is fan-out's code for "the post is being published now: retry".
const pendingCode = "pending"

// Share starts the post of listingID in the background with the giver's
// session cookie. A nil hook, a hook with no URL, or no cookie does nothing.
func (h *ShareHook) Share(listingID string, session *http.Cookie) {
	h.send(http.MethodPost, listingID, session)
}

func (h *ShareHook) send(method, listingID string, session *http.Cookie) {
	if h == nil || h.URL == "" || session == nil || !listingIDForm.MatchString(listingID) {
		return
	}
	ck := &http.Cookie{Name: session.Name, Value: session.Value}
	waits := h.Backoff
	if waits == nil {
		waits = ShareBackoff
		if method == http.MethodPut {
			waits = ChangeBackoff
		}
	}
	go func() {
		var status, tries int
		var err error
		for tries = 1; ; tries++ {
			var code string
			status, code, err = h.once(method, listingID, ck)
			retry := shareAgain(status, err) || (method == http.MethodPut && status == http.StatusConflict && code == pendingCode)
			if !retry || tries > len(waits) {
				break
			}
			time.Sleep(waits[tries-1])
		}
		log.Printf("share hook: %s listing %s -> fanout %d after %d tries (err=%v)", method, listingID, status, tries, err)
		if h.Done != nil {
			h.Done(listingID, status, err)
		}
	}()
}

func (h *ShareHook) once(method, listingID string, ck *http.Cookie) (int, string, error) {
	client := h.HTTP
	if client == nil {
		client = &http.Client{Timeout: 120 * time.Second} // longer than a share (DefaultBackoff note: 95 s)
	}
	req, err := http.NewRequest(method, strings.TrimRight(h.URL, "/")+"/"+listingID, nil)
	if err != nil {
		return 0, "", err
	}
	req.AddCookie(ck)
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	var body struct {
		Code string `json:"code"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&body)
	return resp.StatusCode, body.Code, nil
}

// listingIDForm is the form of a listing id (newID("l_")): it goes into a URL path.
var listingIDForm = regexp.MustCompile(`^l_[0-9a-f]{16}$`)
