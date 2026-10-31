// Package listings is the V3RDE listings service: a donor posts a meal
// (title, servings, pickup text, a photo, the analyze request_id), a
// receiver sees what is active and grabs one. It has its own data, and it
// asks the auth service who the caller is (with the forwarded session
// cookie); it never sees a password or the session table.
//
// V3RDE #25 (Oct 28 2026): this package is written new for V3RDE. The
// routes, the status codes, the error texts, the JSON of a listing and the
// data file format are kept the same as the listings service the app used
// before the Hackathon, so the app and the data on the volume do not change.
// The photo_id input (an analyze item photo or a touched-up photo) and the
// claim hook to fan-out were added in the Hackathon (entries 9 and 22).
//
// Routes (all under /api/listings; every route but healthz needs a session):
//
//	POST /api/listings               multipart: title, number_of_serving, pickup_text, request_id,
//	                                 and photo (a file) or photo_id (an item photo of the caller) -> 201 listing
//	                                 X44h: ttl_seconds (30-1800), test accounts only (-test-ttl-emails), else 403
//	                                 X48d: details (optional JSON, details.go), kept as "details"
//	GET  /api/listings?after=<id>    active and not expired, newest first, 20 per page -> {items, next}
//	GET  /api/listings/mine          the caller's listings, any status, newest first -> {items}
//	POST /api/listings/{id}/grab     -> 200 listing | 409 collected or expired | 403 own listing | 404
//	                                 (then the claim hook tells fan-out, in the background: hook.go)
//	DELETE /api/listings/{id}        X48t: the owner deletes a listing nobody has grabbed -> 204 |
//	                                 409 collected | 404 unknown or another's (then the hook tells fan-out "expired")
//	GET  /api/listings/{id}/photo    the image bytes
//	GET  /api/listings/healthz
//
// Active means status "active" and expires_at after now, decided here, never
// only in the app. expires_at = created_at + the TTL (30 minutes). A grab
// sets status "collected", collected_by and collected_at.
package listings

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// The limits of a listing. post/voice.go uses MaxTitle, MaxPickup and
// MaxServings for its own checks.
const (
	DefaultTTL    = 30 * time.Minute
	PageSize      = 20
	MaxPhotoBytes = 5 << 20
	MaxTitle      = 80  // bytes
	MaxPickup     = 200 // bytes
	MaxServings   = 99
)

// The store errors; the handler maps them to status codes.
var (
	ErrNotFound  = errors.New("listing not found")
	ErrCollected = errors.New("listing already collected")
	ErrExpired   = errors.New("listing expired")
	ErrOwn       = errors.New("cannot grab your own listing")
	// X48t: a grabbed listing is a promise to a receiver: it cannot be deleted
	ErrGrabbed = errors.New("someone is picking this up: it cannot be deleted")
	// X49e: a release of a listing that the caller did not grab
	ErrNotGrabbed = errors.New("you did not grab this listing")
)

// photoExt is the file extension of each accepted photo type.
var photoExt = map[string]string{"image/jpeg": ".jpg", "image/jpg": ".jpg", "image/png": ".png", "image/webp": ".webp"}

// hex32 is the form of an analyze request_id and of a photo_id.
var hex32 = regexp.MustCompile(`^[0-9a-f]{32}$`)

// Listing is the JSON the app gets.
type Listing struct {
	ID              string  `json:"id"`
	OwnerID         string  `json:"owner_id"`
	Title           string  `json:"title"`
	NumberOfServing int     `json:"number_of_serving"`
	PhotoURL        string  `json:"photo_url"`
	PickupText      string  `json:"pickup_text"`
	Status          string  `json:"status"` // active | collected
	CreatedAt       string  `json:"created_at"`
	ExpiresAt       string  `json:"expires_at"`
	CollectedBy     *string `json:"collected_by"`
	CollectedAt     *string `json:"collected_at"`
	RequestID       string  `json:"request_id"`
	// Details (X48d): the scanner's fields of the item; left out when none.
	Details *Details `json:"details,omitempty"`
	// DistanceMi (X49e): miles from the caller's place, 2 decimals; only in
	// an answer to a request with a place, never in the data file.
	DistanceMi *float64 `json:"distance_mi,omitempty"`
}

// Input is one create request before and after Validate.
type Input struct {
	Title           string
	NumberOfServing int
	PickupText      string
	RequestID       string
	Photo           []byte
	Mime            string
	// TTL (X44h): a test account's own timer, MinTestTTL..DefaultTTL; 0 = the
	// store's TTL. Only the handler sets it, and only for a test account.
	TTL time.Duration
	// Details (X48d): the handler parsed and checked them (details.go).
	Details *Details
	// Place (X49e): the pickup place; the store keeps it snapped (place.go).
	Place *Place
}

// MinTestTTL is the shortest timer a test account can ask for (X44h, T9
// 2026-10-29: the rescue test must not wait 30 minutes).
const MinTestTTL = 30 * time.Second

// Validate trims the text fields and applies the rules in this order; the
// error text is what the app shows.
func (in *Input) Validate() error {
	in.Title = strings.TrimSpace(in.Title)
	in.PickupText = strings.TrimSpace(in.PickupText)
	in.RequestID = strings.TrimSpace(in.RequestID)
	if in.Title == "" || len(in.Title) > MaxTitle {
		return fmt.Errorf("title must be 1-%d characters", MaxTitle)
	}
	if in.NumberOfServing < 1 || in.NumberOfServing > MaxServings {
		return fmt.Errorf("number_of_serving must be 1-%d", MaxServings)
	}
	if in.PickupText == "" || len(in.PickupText) > MaxPickup {
		return fmt.Errorf("pickup_text must be 1-%d characters", MaxPickup)
	}
	if in.RequestID != "" && !hex32.MatchString(in.RequestID) {
		return errors.New("request_id must be the analyze request_id (32 hex)")
	}
	if len(in.Photo) == 0 {
		return errors.New("photo is required")
	}
	if len(in.Photo) > MaxPhotoBytes {
		return errors.New("photo over 5 MB")
	}
	if photoExt[in.Mime] == "" {
		return errors.New("photo must be jpeg, png or webp")
	}
	return nil
}
