package listings

import (
	"encoding/json"
	"errors"
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
func itemPhoto(dir, id, userID string) ([]byte, error) {
	if dir == "" || !hex32.MatchString(id) {
		return nil, errNoItemPhoto
	}
	meta, err := os.ReadFile(filepath.Join(dir, id+".json"))
	if err != nil {
		return nil, errNoItemPhoto
	}
	var m struct {
		PhotoID string `json:"photo_id"`
		UserID  string `json:"user_id"`
	}
	if json.Unmarshal(meta, &m) != nil || m.PhotoID != id || m.UserID == "" || m.UserID != userID {
		return nil, errNoItemPhoto
	}
	b, err := os.ReadFile(filepath.Join(dir, id+".jpg"))
	if err != nil || len(b) == 0 || len(b) > MaxPhotoBytes {
		return nil, errNoItemPhoto
	}
	return b, nil
}

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
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "service": "listings"})
	})
	mux.HandleFunc("POST "+p, rt.signedIn(rt.create))
	mux.HandleFunc("GET "+p, rt.signedIn(rt.list))
	mux.HandleFunc("GET "+p+"/mine", rt.signedIn(rt.mine))
	mux.HandleFunc("POST "+p+"/{id}/grab", rt.signedIn(rt.grab))
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
		b, err := itemPhoto(rt.ItemPhotos, photoID, u.ID)
		if err != nil && rt.TouchupPhotos != "" {
			b, err = itemPhoto(rt.TouchupPhotos, photoID, u.ID) // an edited photo
		}
		if err != nil {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
		in.Photo, in.Mime = b, "image/jpeg"
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
	log.Printf("listing %s created by %s request_id=%s", l.ID, u.ID, l.RequestID)
	writeJSON(w, http.StatusCreated, l)
}

// list is GET /api/listings?after=&limit=.
func (rt routes) list(w http.ResponseWriter, r *http.Request, _ User) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, next := rt.Store.Active(r.URL.Query().Get("after"), limit)
	if items == nil {
		items = []Listing{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "next": next})
}

// mine is GET /api/listings/mine.
func (rt routes) mine(w http.ResponseWriter, _ *http.Request, u User) {
	items := rt.Store.Mine(u.ID)
	if items == nil {
		items = []Listing{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// grab is POST /api/listings/{id}/grab.
func (rt routes) grab(w http.ResponseWriter, r *http.Request, u User) {
	l, err := rt.Store.Grab(r.PathValue("id"), u.ID)
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
		rt.Claim.Claimed(l.ID) // in the background: the answer does not wait
		writeJSON(w, http.StatusOK, l)
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
