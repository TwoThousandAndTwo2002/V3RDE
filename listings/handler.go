package listings

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ---- who is calling: ask auth ---------------------------------------------

// sessionCookie is the cookie the auth service sets.
const sessionCookie = "morpheus_session"

// User is the answer of the auth service's /api/auth/me.
type User struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

// Authenticator finds the caller. AuthClient asks the auth service; the
// tests use a fake.
type Authenticator interface {
	Who(r *http.Request) (User, bool)
}

// AuthClient calls GET <URL>/api/auth/me with the caller's session cookie
// (only that cookie, no other header of the caller).
type AuthClient struct {
	URL  string // for example http://auth:8081
	HTTP *http.Client
}

// Who is the user of the session cookie, or false.
func (a *AuthClient) Who(r *http.Request) (User, bool) {
	ck, err := r.Cookie(sessionCookie)
	if err != nil || ck.Value == "" {
		return User{}, false
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, strings.TrimRight(a.URL, "/")+"/api/auth/me", nil)
	if err != nil {
		return User{}, false
	}
	req.AddCookie(ck)
	client := a.HTTP
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("auth lookup: %v", err)
		return User{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return User{}, false
	}
	var u User
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&u); err != nil || u.ID == "" {
		return User{}, false
	}
	return u, true
}

// ---- item photos ----------------------------------------------------------

// errNoItemPhoto is one answer for an unknown photo_id and for a photo of
// another user, so a photo_id cannot be probed
// (https://api-security.owasp.org/editions/2023/en/0xa1-broken-object-level-authorization/).
var errNoItemPhoto = errors.New("photo_id not found")

// itemPhoto reads <dir>/<id>.jpg when <dir>/<id>.json says that it belongs
// to userID: {"photo_id": id, "user_id": userID, ...}.
func itemPhoto(dir, id, userID string) ([]byte, photoMeta, error) {
	var m photoMeta
	if dir == "" || !hex32.MatchString(id) {
		return nil, m, errNoItemPhoto
	}
	meta, err := os.ReadFile(filepath.Join(dir, id+".json"))
	if err != nil {
		return nil, m, errNoItemPhoto
	}
	if json.Unmarshal(meta, &m) != nil || m.PhotoID != id || m.UserID == "" || m.UserID != userID {
		return nil, photoMeta{}, errNoItemPhoto
	}
	b, err := os.ReadFile(filepath.Join(dir, id+".jpg"))
	if err != nil || len(b) == 0 || len(b) > MaxPhotoBytes {
		return nil, photoMeta{}, errNoItemPhoto
	}
	return b, m, nil
}

// photoMeta is the part of an item photo's record that listings reads.
type photoMeta struct {
	PhotoID string `json:"photo_id"`
	UserID  string `json:"user_id"`
	Kind    string `json:"kind"` // N1: "stock" = an Open Food Facts product photo; "touchup" = an edit
	// SourcePhotoID: the item photo a touch-up started from (post/touchup.go).
	SourcePhotoID string `json:"source_photo_id"`
}

// stockPhoto (N1 review): the photo is a stock photo, or a touch-up of the
// caller's own stock item photo (an edit of a CC BY-SA photo keeps the credit).
func stockPhoto(itemDir string, m photoMeta, userID string) bool {
	if m.Kind == "stock" {
		return true
	}
	if m.Kind != "touchup" || m.SourcePhotoID == "" {
		return false
	}
	_, src, err := itemPhoto(itemDir, m.SourcePhotoID, userID)
	return err == nil && src.Kind == "stock"
}

// StockCredit (N1) is the credit of a stock photo. Open Food Facts photos are
// CC BY-SA (https://world.openfoodfacts.org/terms-of-use): the listing keeps
// the credit (listing photo_credit), so a client can show it with the photo.
const StockCredit = "Open Food Facts, CC BY-SA"

// ---- HTTP -----------------------------------------------------------------

// Config is everything the routes need; cmd/listings fills it from flags.
type Config struct {
	Store  *Store
	Auth   Authenticator
	Prefix string // "" = /api/listings
	// ItemPhotos is the analyze service's item photo folder, mounted
	// read-only: <id>.jpg + <id>.json {"photo_id","user_id",...}; "" = off.
	ItemPhotos string
	// TouchupPhotos is the touch-up service's folder of edited photos, in
	// the same format, read-only; "" = off.
	TouchupPhotos string
	// Claim tells fan-out after a grab; nil = off.
	Claim *ClaimHook
	// Share (N3) asks fan-out to post each new listing; nil = off.
	Share *ShareHook
	// TestTTLEmails (X44h): the test accounts (lower case) that may send
	// ttl_seconds on create; nil = nobody. Every other caller gets 403.
	TestTTLEmails map[string]bool
	// MapsKey (X49j): the Google Maps browser key (Places autocomplete and the
	// map of the pickup address); "" = the app uses the X49e place row. The key
	// is limited to the app's site and the 3 Maps APIs (Google Cloud).
	MapsKey string
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// fail is every error body: {"error": msg}.
func fail(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// routes holds the config for the route functions.
type routes struct{ Config }

// New makes the routes of the listings service.
func New(c Config) http.Handler {
	if c.Prefix == "" {
		c.Prefix = "/api/listings"
	}
	rt := routes{c}
	p := c.Prefix
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+p+"/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "service": "listings", "zips": ZipCount()})
	})
	mux.HandleFunc("POST "+p, rt.signedIn(rt.create))
	mux.HandleFunc("GET "+p, rt.signedIn(rt.list))
	mux.HandleFunc("GET "+p+"/mine", rt.signedIn(rt.mine))
	mux.HandleFunc("GET "+p+"/config", rt.signedIn(rt.config))   // X49j
	mux.HandleFunc("GET "+p+"/grabbed", rt.signedIn(rt.grabbed)) // X49e
	mux.HandleFunc("POST "+p+"/{id}/grab", rt.signedIn(rt.grab))
	mux.HandleFunc("POST "+p+"/{id}/release", rt.signedIn(rt.release)) // X49e
	mux.HandleFunc("DELETE "+p+"/{id}", rt.signedIn(rt.remove))        // X48t
	mux.HandleFunc("PATCH "+p+"/{id}", rt.signedIn(rt.update))         // X60
	mux.HandleFunc("GET "+p+"/{id}/photo", rt.signedIn(rt.photo))
	return mux
}

// signedIn answers 401 when the caller has no valid session.
func (rt routes) signedIn(next func(http.ResponseWriter, *http.Request, User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := rt.Auth.Who(r)
		if !ok {
			fail(w, http.StatusUnauthorized, "not logged in")
			return
		}
		next(w, r, u)
	}
}

// create is POST /api/listings.
func (rt routes) create(w http.ResponseWriter, r *http.Request, u User) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxPhotoBytes+64<<10)
	if err := r.ParseMultipartForm(MaxPhotoBytes); err != nil {
		fail(w, http.StatusRequestEntityTooLarge, "photo too large or bad multipart")
		return
	}
	n, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("number_of_serving")))
	in := Input{Title: r.FormValue("title"), NumberOfServing: n, PickupText: r.FormValue("pickup_text"),
		RequestID: r.FormValue("request_id")}
	if v := strings.TrimSpace(r.FormValue("ttl_seconds")); v != "" { // X44h: test accounts only
		if !rt.TestTTLEmails[strings.ToLower(strings.TrimSpace(u.Email))] {
			fail(w, http.StatusForbidden, "ttl_seconds is for test accounts only")
			return
		}
		sec, err := strconv.Atoi(v)
		if err != nil || time.Duration(sec)*time.Second < MinTestTTL || time.Duration(sec)*time.Second > DefaultTTL {
			fail(w, http.StatusBadRequest, fmt.Sprintf("ttl_seconds must be %d-%d", int(MinTestTTL.Seconds()), int(DefaultTTL.Seconds())))
			return
		}
		in.TTL = time.Duration(sec) * time.Second
	}
	d, err := parseDetails(r.FormValue("details")) // X48d
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	in.Details = d
	// X49e: the pickup place is needed (the phone location, or a zip)
	place, err := ParsePlace(r.FormValue("lat"), r.FormValue("lng"), r.FormValue("zip"))
	if err == nil && place == nil {
		err = ErrPlace
	}
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	in.Place = place
	// X49j: the pickup address the giver chose (Places autocomplete): its text, its ZIP and the point of lat/lng
	if text := strings.TrimSpace(r.FormValue("address")); text != "" {
		zip := strings.TrimSpace(r.FormValue("address_zip"))
		switch {
		case len(text) > MaxAddress:
			fail(w, http.StatusBadRequest, fmt.Sprintf("address must be 1-%d characters", MaxAddress))
			return
		case zip != "" && !zipRe.MatchString(zip):
			fail(w, http.StatusBadRequest, "address_zip must be a 5-digit US zip")
			return
		case strings.TrimSpace(r.FormValue("lat")) == "":
			fail(w, http.StatusBadRequest, "send lat and lng with the address")
			return
		}
		in.Address = &PickupAddress{Text: text, Zip: zip, Lat: place.Lat, Lng: place.Lng}
	}
	photoID := strings.TrimSpace(r.FormValue("photo_id"))
	if f, hdr, err := r.FormFile("photo"); err == nil {
		defer f.Close()
		if photoID != "" {
			fail(w, http.StatusBadRequest, "send photo or photo_id, not both")
			return
		}
		in.Photo, _ = io.ReadAll(io.LimitReader(f, MaxPhotoBytes+1)) // a short read fails Validate
		part, _, _ := strings.Cut(hdr.Header.Get("Content-Type"), ";")
		in.Mime = strings.ToLower(strings.TrimSpace(part))
		if in.Mime == "" {
			in.Mime = http.DetectContentType(in.Photo)
		}
	} else if photoID != "" {
		b, meta, err := itemPhoto(rt.ItemPhotos, photoID, u.ID)
		if err != nil && rt.TouchupPhotos != "" {
			b, meta, err = itemPhoto(rt.TouchupPhotos, photoID, u.ID) // an edited photo
		}
		if err != nil {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
		in.Photo, in.Mime = b, "image/jpeg"
		if stockPhoto(rt.ItemPhotos, meta, u.ID) {
			in.PhotoCredit = StockCredit
		}
	}
	l, err := rt.Store.Create(u.ID, in)
	if err != nil {
		// A disk failure is the server's fault; a bad field is the caller's.
		if errors.Is(err, os.ErrPermission) || strings.Contains(err.Error(), "no space") {
			fail(w, http.StatusInternalServerError, "could not store the listing")
			return
		}
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	log.Printf("listing %s created by %s request_id=%s details=%t", l.ID, u.ID, l.RequestID, l.Details != nil)
	writeJSON(w, http.StatusCreated, l)
	// N3: every new listing goes to the Facebook Page (fan-out), never a test account's (X44h)
	if !rt.TestTTLEmails[strings.ToLower(strings.TrimSpace(u.Email))] {
		ck, _ := r.Cookie(sessionCookie)
		rt.Share.Share(l.ID, ck)
	}
}

// list is GET /api/listings?after=&limit=, and with a place (X49e:
// &lat=&lng= or &zip=) the listings within MaxMiles, nearest first, with
// distance_mi.
func (rt routes) list(w http.ResponseWriter, r *http.Request, _ User) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	from, err := ParsePlace(q.Get("lat"), q.Get("lng"), q.Get("zip"))
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	var items []Listing
	var next string
	if from != nil {
		items, next, err = rt.Store.Near(*from, q.Get("after"), limit)
		if err != nil {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
	} else {
		items, next = rt.Store.Active(q.Get("after"), limit)
	}
	if items == nil {
		items = []Listing{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "next": next})
}

// config is GET /api/listings/config (X49j): {"maps_key": ...} for the app.
func (rt routes) config(w http.ResponseWriter, _ *http.Request, _ User) {
	writeJSON(w, http.StatusOK, map[string]string{"maps_key": rt.MapsKey})
}

// mine is GET /api/listings/mine.
func (rt routes) mine(w http.ResponseWriter, _ *http.Request, u User) {
	items := rt.Store.Mine(u.ID)
	if items == nil {
		items = []Listing{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// grabbed is GET /api/listings/grabbed[?lat=&lng=|?zip=] (X49e): the
// caller's grabs whose timer still runs ("Your pickup").
func (rt routes) grabbed(w http.ResponseWriter, r *http.Request, u User) {
	q := r.URL.Query()
	from, err := ParsePlace(q.Get("lat"), q.Get("lng"), q.Get("zip"))
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	items := rt.Store.Grabbed(u.ID, from)
	if items == nil {
		items = []Listing{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// release is POST /api/listings/{id}/release (X49e): the receiver gives the
// grab back while the timer runs; fan-out removes [CLAIMED] in the background.
func (rt routes) release(w http.ResponseWriter, r *http.Request, u User) {
	l, err := rt.Store.ReleaseThen(r.PathValue("id"), u.ID, func(l Listing) { rt.Claim.Released(l.ID) }) // queued in store order
	switch {
	case errors.Is(err, ErrNotFound):
		fail(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrNotGrabbed), errors.Is(err, ErrExpired):
		fail(w, http.StatusConflict, err.Error())
	case err != nil:
		fail(w, http.StatusInternalServerError, "could not release")
	default:
		log.Printf("listing %s released by %s", l.ID, u.ID)
		writeJSON(w, http.StatusOK, l)
	}
}

// grab is POST /api/listings/{id}/grab.
func (rt routes) grab(w http.ResponseWriter, r *http.Request, u User) {
	// the claim is queued while the store is locked: its fan-out call keeps the order of the grabs and releases
	l, err := rt.Store.GrabThen(r.PathValue("id"), u.ID, func(l Listing) { rt.Claim.Claimed(l.ID) })
	switch {
	case errors.Is(err, ErrNotFound):
		fail(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrOwn):
		fail(w, http.StatusForbidden, err.Error())
	case errors.Is(err, ErrCollected), errors.Is(err, ErrExpired):
		fail(w, http.StatusConflict, err.Error())
	case err != nil:
		fail(w, http.StatusInternalServerError, "could not grab")
	default:
		log.Printf("listing %s grabbed by %s", l.ID, u.ID)
		writeJSON(w, http.StatusOK, l)
	}
}

// remove is DELETE /api/listings/{id} (X48t): the owner, before a grab.
func (rt routes) remove(w http.ResponseWriter, r *http.Request, u User) {
	l, err := rt.Store.Delete(r.PathValue("id"), u.ID)
	switch {
	case errors.Is(err, ErrNotFound):
		fail(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrGrabbed):
		fail(w, http.StatusConflict, err.Error())
	case err != nil:
		fail(w, http.StatusInternalServerError, "could not delete")
	default:
		log.Printf("listing %s deleted by its owner %s", l.ID, u.ID)
		rt.Claim.Ended(l.ID) // fan-out: the post says the food is gone (in the background)
		w.WriteHeader(http.StatusNoContent)
	}
}

// update is PATCH /api/listings/{id} (X60): the owner changes the title, the
// servings or the timer of a listing nobody has grabbed; with "reset" an
// ended listing shows again ("post again"). JSON {"title", "number_of_serving",
// "reset"}, each optional, nothing else. Then the share hook asks fan-out to
// change the Facebook post (in the background; never a test account's).
func (rt routes) update(w http.ResponseWriter, r *http.Request, u User) {
	var in struct {
		Title           *string `json:"title"`
		NumberOfServing *int    `json:"number_of_serving"`
		Reset           bool    `json:"reset"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		fail(w, http.StatusBadRequest, `send JSON {"title", "number_of_serving", "reset"}`)
		return
	}
	c := Change{Title: in.Title, Reset: in.Reset}
	if in.NumberOfServing != nil {
		if *in.NumberOfServing == 0 { // 0 means "keep" in Change: a sent 0 is a bad number
			fail(w, http.StatusBadRequest, fmt.Sprintf("number_of_serving must be 1-%d", MaxServings))
			return
		}
		c.NumberOfServing = *in.NumberOfServing
	}
	l, err := rt.Store.Update(r.PathValue("id"), u.ID, c, nil)
	switch {
	case errors.Is(err, ErrNotFound):
		fail(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrGrabbed):
		fail(w, http.StatusConflict, "someone is picking this up: it cannot be changed")
	case errors.Is(err, ErrExpired):
		fail(w, http.StatusConflict, "listing expired: send reset to post it again")
	case errors.Is(err, os.ErrPermission), err != nil && strings.Contains(err.Error(), "no space"):
		fail(w, http.StatusInternalServerError, "could not store the listing")
	case err != nil:
		fail(w, http.StatusBadRequest, err.Error())
	default:
		log.Printf("listing %s changed by its owner %s title=%t servings=%t reset=%t", l.ID, u.ID, in.Title != nil, in.NumberOfServing != nil, in.Reset)
		writeJSON(w, http.StatusOK, l)
		if !rt.TestTTLEmails[strings.ToLower(strings.TrimSpace(u.Email))] {
			ck, _ := r.Cookie(sessionCookie)
			rt.Share.Changed(l.ID, ck)
		}
	}
}

// photo is GET /api/listings/{id}/photo.
func (rt routes) photo(w http.ResponseWriter, r *http.Request, _ User) {
	path, mime, err := rt.Store.Photo(r.PathValue("id"))
	if err != nil {
		fail(w, http.StatusNotFound, err.Error())
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeFile(w, r, path)
}
