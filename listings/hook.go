package listings

// The claim hook (Hackathon entry 22). After a grab, the fan-out service
// edits the listing's Facebook post to [CLAIMED] (post/fanout.go, POST
// /v1/fanout/status). The call is internal only (no Caddy route) and has its
// own secret, never the analyze token (that one is in the app bundle, so it
// is public). It runs in the background, so the grab answer does not wait
// for Facebook. It tries again on no answer, 409 (the listing is being
// shared now), 429 and 5xx other than 503; any other answer ends it (200
// done, 404 no post for this listing, 503 posting is off).

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// ClaimHook calls the fan-out status route after a grab.
type ClaimHook struct {
	URL   string // for example http://fanout:8090/v1/fanout/status
	Token string // from /run/secrets/fanout_status_token
	HTTP  *http.Client
	// Backoff is the wait before each new try (the first try is at once);
	// nil = DefaultBackoff.
	Backoff []time.Duration
	// Done, when set, gets the result of each claim (tests, logs).
	Done func(listingID string, status int, tries int, err error)

	// X49e: the calls for one listing go one after the other, in the order
	// of the requests (a release right after a grab must not reach fan-out
	// before the claim). tail is the end of each listing's queue.
	mu   sync.Mutex
	tail map[string]chan struct{}
}

// DefaultBackoff waits 137 s in all: longer than the longest share in
// fan-out (the listing photo 10 s + two Muse calls 25 s + the Facebook
// upload 60 s = 95 s), so a claim during a share is not lost.
var DefaultBackoff = []time.Duration{2 * time.Second, 5 * time.Second, 10 * time.Second,
	20 * time.Second, 40 * time.Second, 60 * time.Second}

// ReadToken reads a secret file; an empty file is an error.
func ReadToken(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	t := strings.TrimSpace(string(b))
	if t == "" {
		return "", errors.New(path + " is empty")
	}
	return t, nil
}

// Claimed sends {"listing_id", "status":"claimed"} in the background. A nil
// hook or a hook with no URL does nothing.
func (h *ClaimHook) Claimed(listingID string) {
	h.start(listingID, "claimed")
}

// Released sends {"listing_id", "status":"released"} in the background (X49e:
// the receiver gave the grab back; fan-out removes [CLAIMED]).
func (h *ClaimHook) Released(listingID string) {
	h.start(listingID, "released")
}

// start queues the call before it returns (the answer to the app does not
// wait), so the calls of one listing keep the order of the requests.
func (h *ClaimHook) start(listingID, status string) {
	if h == nil || h.URL == "" {
		return
	}
	mine := make(chan struct{})
	h.mu.Lock()
	if h.tail == nil {
		h.tail = map[string]chan struct{}{}
	}
	prev := h.tail[listingID]
	h.tail[listingID] = mine
	h.mu.Unlock()
	go func() {
		if prev != nil {
			<-prev
		}
		h.run(listingID, status)
		close(mine)
		h.mu.Lock()
		if h.tail[listingID] == mine {
			delete(h.tail, listingID)
		}
		h.mu.Unlock()
	}()
}

// Ended sends {"listing_id", "status":"expired"} in the background (X48t: the
// owner deleted the listing, so the food is no longer there).
func (h *ClaimHook) Ended(listingID string) {
	h.start(listingID, "expired")
}

// again reports whether a try with this result is followed by another one.
// 429 is included: every claim comes from this one container, so all
// claims share one rate-limit key in fan-out.
func again(status int, err error) bool {
	switch {
	case err != nil:
		return true
	case status == http.StatusConflict, status == http.StatusTooManyRequests:
		return true
	case status >= 500 && status != http.StatusServiceUnavailable:
		return true
	}
	return false
}

func (h *ClaimHook) run(listingID, status string) {
	client := h.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	waits := h.Backoff
	if waits == nil {
		waits = DefaultBackoff
	}
	body, _ := json.Marshal(map[string]string{"listing_id": listingID, "status": status})
	// At most len(waits)+1 tries; tries is the number of calls made.
	var code, tries int
	var err error
	for tries = 1; ; tries++ {
		code, err = h.once(client, body)
		if !again(code, err) || tries > len(waits) {
			break
		}
		time.Sleep(waits[tries-1])
	}
	if err == nil && code != http.StatusOK && code != http.StatusNotFound && code != http.StatusServiceUnavailable {
		err = fmt.Errorf("fanout answered %d", code)
	}
	log.Printf("claim hook: listing %s %s -> fanout %d after %d tries (err=%v)", listingID, status, code, tries, err)
	if h.Done != nil {
		h.Done(listingID, code, tries, err)
	}
}

// once is one POST to the status route; it returns the status code.
func (h *ClaimHook) once(client *http.Client, body []byte) (int, error) {
	req, err := http.NewRequest(http.MethodPost, h.URL, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+h.Token)
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return resp.StatusCode, nil
}
