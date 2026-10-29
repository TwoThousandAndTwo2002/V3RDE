package listings

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---- helpers --------------------------------------------------------------

// fakeAuth: the session cookie value is the user id ("" = no session).
type fakeAuth struct{}

func (fakeAuth) Who(r *http.Request) (User, bool) {
	ck, err := r.Cookie(sessionCookie)
	if err != nil || ck.Value == "" {
		return User{}, false
	}
	return User{ID: ck.Value, Email: ck.Value + "@example.org"}, true
}

var jpeg = []byte("\xff\xd8\xff\xe0\x00\x10JFIF\x00\x01 a small photo")

type env struct {
	st    *Store
	h     http.Handler
	dir   string
	items string // item photo folder
	touch string // touch-up folder
	clock *time.Time
}

func newEnv(t *testing.T, claim *ClaimHook) *env {
	t.Helper()
	dir := t.TempDir()
	st, err := Open(dir, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 28, 18, 0, 0, 0, time.UTC)
	e := &env{st: st, dir: dir, items: t.TempDir(), touch: t.TempDir(), clock: &now}
	st.now = func() time.Time { return *e.clock }
	e.h = New(Config{Store: st, Auth: fakeAuth{}, ItemPhotos: e.items, TouchupPhotos: e.touch, Claim: claim})
	return e
}

func (e *env) tick(d time.Duration) { *e.clock = e.clock.Add(d) }

// form builds a create request; fields with a "photo" key add a file part.
func form(t *testing.T, user string, fields map[string]string, photo []byte, photoType string) *http.Request {
	t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		mw.WriteField(k, fields[k])
	}
	if photo != nil {
		hdr := map[string][]string{"Content-Disposition": {`form-data; name="photo"; filename="p.jpg"`}}
		if photoType != "" {
			hdr["Content-Type"] = []string{photoType}
		}
		pw, _ := mw.CreatePart(hdr)
		pw.Write(photo)
	}
	mw.Close()
	r := httptest.NewRequest(http.MethodPost, "/api/listings", &b)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	if user != "" {
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: user})
	}
	return r
}

func okFields() map[string]string {
	return map[string]string{"title": " Lasagna tray ", "number_of_serving": "6", "pickup_text": "Side door, ring twice",
		"request_id": "0123456789abcdef0123456789abcdef"}
}

func get(user, path string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	if user != "" {
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: user})
	}
	return r
}

func post(user, path string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, path, nil)
	if user != "" {
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: user})
	}
	return r
}

func call(h http.Handler, r *http.Request) (*httptest.ResponseRecorder, map[string]any) {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var m map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &m)
	return w, m
}

func (e *env) create(t *testing.T, user string) Listing {
	t.Helper()
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, form(t, user, okFields(), jpeg, "image/jpeg"))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	var l Listing
	if err := json.Unmarshal(w.Body.Bytes(), &l); err != nil {
		t.Fatal(err)
	}
	return l
}

// itemPhotoFiles writes <id>.jpg and <id>.json into dir.
func itemPhotoFiles(t *testing.T, dir, id, owner string, img []byte) {
	t.Helper()
	meta, _ := json.Marshal(map[string]string{"photo_id": id, "user_id": owner, "label": "salad"})
	if err := os.WriteFile(filepath.Join(dir, id+".json"), meta, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".jpg"), img, 0o600); err != nil {
		t.Fatal(err)
	}
}

// ---- the input rules ------------------------------------------------------

func TestValidateRulesInOrder(t *testing.T) {
	good := func() Input {
		return Input{Title: "Soup", NumberOfServing: 2, PickupText: "Front desk", Photo: jpeg, Mime: "image/jpeg"}
	}
	for _, tc := range []struct {
		edit func(*Input)
		want string
	}{
		{func(in *Input) { in.Title = "   " }, "title must be 1-80 characters"},
		{func(in *Input) { in.Title = strings.Repeat("é", 41) }, "title must be 1-80 characters"}, // 82 bytes
		{func(in *Input) { in.NumberOfServing = 0 }, "number_of_serving must be 1-99"},
		{func(in *Input) { in.NumberOfServing = 100 }, "number_of_serving must be 1-99"},
		{func(in *Input) { in.PickupText = "" }, "pickup_text must be 1-200 characters"},
		{func(in *Input) { in.PickupText = strings.Repeat("x", 201) }, "pickup_text must be 1-200 characters"},
		{func(in *Input) { in.RequestID = "XYZ" }, "request_id must be the analyze request_id (32 hex)"},
		{func(in *Input) { in.Photo = nil }, "photo is required"},
		{func(in *Input) { in.Photo = make([]byte, MaxPhotoBytes+1) }, "photo over 5 MB"},
		{func(in *Input) { in.Mime = "image/gif" }, "photo must be jpeg, png or webp"},
		{func(in *Input) { in.Title, in.NumberOfServing = "", 0 }, "title must be 1-80 characters"}, // the first rule wins
	} {
		in := good()
		tc.edit(&in)
		if err := in.Validate(); err == nil || err.Error() != tc.want {
			t.Errorf("got %v, want %q", err, tc.want)
		}
	}
	in := good()
	in.Title, in.PickupText, in.RequestID = "  Soup  ", " Front desk\n", " 0123456789abcdef0123456789abcdef "
	if err := in.Validate(); err != nil || in.Title != "Soup" || in.PickupText != "Front desk" || len(in.RequestID) != 32 {
		t.Fatalf("trim: %v %+v", err, in)
	}
	in.RequestID = ""
	if err := in.Validate(); err != nil {
		t.Fatalf("request_id is optional: %v", err)
	}
}

// ---- create, the JSON shape and the data file -----------------------------

func TestCreateShapeAndDataFile(t *testing.T) {
	e := newEnv(t, nil)
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, form(t, "u_donor", okFields(), jpeg, "image/jpeg"))
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var raw map[string]any
	json.Unmarshal(w.Body.Bytes(), &raw)
	var keys []string
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	want := "collected_at collected_by created_at expires_at id number_of_serving owner_id photo_url pickup_text request_id status title"
	if strings.Join(keys, " ") != want {
		t.Fatalf("keys %v", keys)
	}
	id := raw["id"].(string)
	if !strings.HasPrefix(id, "l_") || len(id) != 18 || raw["photo_url"] != "/api/listings/"+id+"/photo" ||
		raw["title"] != "Lasagna tray" || raw["status"] != "active" || raw["collected_by"] != nil ||
		raw["created_at"] != "2026-10-28T18:00:00Z" || raw["expires_at"] != "2026-10-28T18:30:00Z" {
		t.Fatalf("listing %v", raw)
	}
	// the data file: id -> listing + photo_file, the photo next to it
	b, err := os.ReadFile(filepath.Join(e.dir, "listings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var file map[string]map[string]any
	if err := json.Unmarshal(b, &file); err != nil || file[id]["photo_file"] != id+".jpg" || file[id]["owner_id"] != "u_donor" {
		t.Fatalf("data file %s %v", b, err)
	}
	if p, _ := os.ReadFile(filepath.Join(e.dir, "photos", id+".jpg")); !bytes.Equal(p, jpeg) {
		t.Fatal("photo not stored")
	}
	if _, err := os.Stat(filepath.Join(e.dir, "listings.json.tmp")); !os.IsNotExist(err) {
		t.Fatal("a temp file was left")
	}
}

func TestReopenKeepsListings(t *testing.T) {
	e := newEnv(t, nil)
	l := e.create(t, "u_donor")
	if _, err := e.st.Grab(l.ID, "u_receiver"); err != nil {
		t.Fatal(err)
	}
	st, err := Open(e.dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	mine := st.Mine("u_donor")
	if len(mine) != 1 || mine[0].Status != "collected" || *mine[0].CollectedBy != "u_receiver" {
		t.Fatalf("after reopen: %+v", mine)
	}
	if st.ttl != DefaultTTL {
		t.Fatalf("ttl 0 must be the default, got %s", st.ttl)
	}
	os.WriteFile(filepath.Join(e.dir, "listings.json"), []byte("{broken"), 0o600)
	if _, err := Open(e.dir, 0); err == nil || !strings.Contains(err.Error(), "listings.json") {
		t.Fatalf("a broken file must stop the start: %v", err)
	}
}

// oldDataFile was written by the listings service of the app before the
// Hackathon (a listing collected, a listing active): V3RDE must load it.
const oldDataFile = `{
 "l_2eff9ecbf6f9d992": {
  "id": "l_2eff9ecbf6f9d992",
  "owner_id": "u_donor",
  "title": "Bread rolls",
  "number_of_serving": 12,
  "photo_url": "/api/listings/l_2eff9ecbf6f9d992/photo",
  "pickup_text": "Back door",
  "status": "collected",
  "created_at": "2026-10-25T17:00:00Z",
  "expires_at": "2026-10-25T17:30:00Z",
  "collected_by": "u_receiver",
  "collected_at": "2026-10-25T17:02:00Z",
  "request_id": "0123456789abcdef0123456789abcdef",
  "photo_file": "l_2eff9ecbf6f9d992.jpg"
 },
 "l_9f32bbd59d843fbc": {
  "id": "l_9f32bbd59d843fbc",
  "owner_id": "u_donor",
  "title": "Soup",
  "number_of_serving": 4,
  "photo_url": "/api/listings/l_9f32bbd59d843fbc/photo",
  "pickup_text": "Front desk",
  "status": "active",
  "created_at": "2026-10-25T17:01:00Z",
  "expires_at": "2026-10-25T17:31:00Z",
  "collected_by": null,
  "collected_at": null,
  "request_id": "",
  "photo_file": "l_9f32bbd59d843fbc.png"
 }
}`

func TestLoadsTheOldDataFile(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "photos"), 0o750)
	os.WriteFile(filepath.Join(dir, "listings.json"), []byte(oldDataFile), 0o600)
	os.WriteFile(filepath.Join(dir, "photos", "l_9f32bbd59d843fbc.png"), []byte("\x89PNG"), 0o640)
	st, err := Open(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	mine := st.Mine("u_donor")
	if len(mine) != 2 || mine[0].ID != "l_9f32bbd59d843fbc" || mine[0].Status != "active" ||
		mine[1].Status != "collected" || *mine[1].CollectedBy != "u_receiver" || *mine[1].CollectedAt != "2026-10-25T17:02:00Z" {
		t.Fatalf("mine %+v", mine)
	}
	path, mime, err := st.Photo("l_9f32bbd59d843fbc")
	if err != nil || mime != "image/png" || path != filepath.Join(dir, "photos", "l_9f32bbd59d843fbc.png") {
		t.Fatalf("photo %s %s %v", path, mime, err)
	}
	// a write keeps the old records as they were
	st.now = func() time.Time { return time.Date(2026, 10, 25, 17, 10, 0, 0, time.UTC) }
	if _, err := st.Grab("l_9f32bbd59d843fbc", "u_receiver"); err != nil {
		t.Fatal(err)
	}
	var before, after map[string]map[string]any
	json.Unmarshal([]byte(oldDataFile), &before)
	b, _ := os.ReadFile(filepath.Join(dir, "listings.json"))
	json.Unmarshal(b, &after)
	if fmt.Sprint(before["l_2eff9ecbf6f9d992"]) != fmt.Sprint(after["l_2eff9ecbf6f9d992"]) {
		t.Fatalf("an untouched record changed:\n%v\n%v", before["l_2eff9ecbf6f9d992"], after["l_2eff9ecbf6f9d992"])
	}
}

func TestNoTempFilesAndRelativePhotoPath(t *testing.T) {
	e := newEnv(t, nil)
	e.create(t, "u_donor")
	if tmps, _ := filepath.Glob(filepath.Join(e.dir, "photos", "*.tmp")); len(tmps) != 0 {
		t.Fatalf("temp photos left: %v", tmps)
	}
	b, _ := os.ReadFile(filepath.Join(e.dir, "listings.json"))
	if bytes.Contains(b, []byte(e.dir)) {
		t.Fatal("the data file has an absolute path")
	}
	// a crash during a write leaves a temp file; the next start removes it
	os.WriteFile(filepath.Join(e.dir, "photos", "l_crash.jpg.tmp"), []byte("half"), 0o640)
	os.WriteFile(filepath.Join(e.dir, "listings.json.tmp"), []byte("{half"), 0o600)
	if _, err := Open(e.dir, 0); err != nil {
		t.Fatal(err)
	}
	if tmps, _ := filepath.Glob(filepath.Join(e.dir, "photos", "*.tmp")); len(tmps) != 0 {
		t.Fatalf("temp photos after a start: %v", tmps)
	}
	if _, err := os.Stat(filepath.Join(e.dir, "listings.json.tmp")); !os.IsNotExist(err) {
		t.Fatal("the temp data file is still there")
	}
}

// A failed photo write names the photo file, not its temp file.
func TestPhotoWriteErrorNamesThePhoto(t *testing.T) {
	e := newEnv(t, nil)
	os.RemoveAll(e.st.photos)
	_, err := e.st.Create("u", Input{Title: "a", NumberOfServing: 1, PickupText: "b", Photo: jpeg, Mime: "image/jpeg"})
	if err == nil || strings.Contains(err.Error(), ".tmp") || !strings.Contains(err.Error(), ".jpg") {
		t.Fatalf("error %v", err)
	}
}

// Two listings in one second: the order is the time order, not the text
// order of created_at (RFC 3339 drops zeros at the end).
func TestSameSecondOrderIsTimeOrder(t *testing.T) {
	e := newEnv(t, nil)
	*e.clock = time.Date(2026, 10, 28, 18, 0, 57, 520000000, time.UTC) // "...:57.52Z"
	first := e.create(t, "u_donor")
	*e.clock = time.Date(2026, 10, 28, 18, 0, 57, 521569942, time.UTC) // "...:57.521569942Z"
	second := e.create(t, "u_donor")
	if first.CreatedAt != "2026-10-28T18:00:57.52Z" {
		t.Fatalf("created_at %s", first.CreatedAt)
	}
	items, _ := e.st.Active("", 0)
	if len(items) != 2 || items[0].ID != second.ID || items[1].ID != first.ID {
		t.Fatalf("order %s %s", items[0].CreatedAt, items[1].CreatedAt)
	}
}

func TestCreateRefusals(t *testing.T) {
	e := newEnv(t, nil)
	bad := okFields()
	bad["number_of_serving"] = "six"
	for _, tc := range []struct {
		name string
		r    *http.Request
		code int
		msg  string
	}{
		{"no session", form(t, "", okFields(), jpeg, "image/jpeg"), 401, "not logged in"},
		{"servings not a number", form(t, "u1", bad, jpeg, "image/jpeg"), 400, "number_of_serving must be 1-99"},
		{"no photo", form(t, "u1", okFields(), nil, ""), 400, "photo is required"},
		{"gif", form(t, "u1", okFields(), []byte("GIF89a...."), "image/gif"), 400, "photo must be jpeg, png or webp"},
		{"type found from bytes", form(t, "u1", okFields(), []byte("GIF89a...."), ""), 400, "photo must be jpeg, png or webp"},
		{"too large", form(t, "u1", okFields(), make([]byte, MaxPhotoBytes+70<<10), "image/jpeg"), 413, "photo too large or bad multipart"},
	} {
		w, body := call(e.h, tc.r)
		if w.Code != tc.code || body["error"] != tc.msg {
			t.Errorf("%s: %d %v", tc.name, w.Code, body)
		}
	}
	if n := len(e.st.Mine("u1")); n != 0 {
		t.Fatalf("%d listings from refused requests", n)
	}
	if fs, _ := os.ReadDir(filepath.Join(e.dir, "photos")); len(fs) != 0 {
		t.Fatalf("photos from refused requests: %d", len(fs))
	}
}

func TestCreateFromPNGKeepsItsType(t *testing.T) {
	e := newEnv(t, nil)
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	w, body := call(e.h, form(t, "u1", okFields(), png, ""))
	if w.Code != 201 {
		t.Fatalf("%d %v", w.Code, body)
	}
	pw, _ := call(e.h, get("u2", body["photo_url"].(string)))
	if pw.Header().Get("Content-Type") != "image/png" || !bytes.Equal(pw.Body.Bytes(), png) {
		t.Fatalf("photo %s %q", pw.Header().Get("Content-Type"), pw.Body.Bytes())
	}
}

// ---- photo_id ------------------------------------------------------------

func TestCreateWithPhotoIDOwnerOnly(t *testing.T) {
	e := newEnv(t, nil)
	const pid = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	itemPhotoFiles(t, e.items, pid, "u_donor", jpeg)
	f := okFields()
	f["photo_id"] = pid
	for _, tc := range []struct {
		user string
		pid  string
		code int
	}{
		{"u_other", pid, 400},                                // another user's photo
		{"u_donor", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", 400}, // unknown
		{"u_donor", "../../etc/passwd", 400},                 // not the id form
	} {
		g := okFields()
		g["photo_id"] = tc.pid
		w, body := call(e.h, form(t, tc.user, g, nil, ""))
		if w.Code != tc.code || body["error"] != "photo_id not found" {
			t.Errorf("%s %s: %d %v", tc.user, tc.pid, w.Code, body)
		}
	}
	w, body := call(e.h, form(t, "u_donor", f, nil, ""))
	if w.Code != 201 {
		t.Fatalf("owner: %d %v", w.Code, body)
	}
	pw, _ := call(e.h, get("u_x", body["photo_url"].(string)))
	if !bytes.Equal(pw.Body.Bytes(), jpeg) || pw.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatal("the item photo is not the listing photo")
	}
	w, body = call(e.h, form(t, "u_donor", f, jpeg, "image/jpeg"))
	if w.Code != 400 || body["error"] != "send photo or photo_id, not both" {
		t.Fatalf("both: %d %v", w.Code, body)
	}
}

func TestCreateWithTouchedUpPhoto(t *testing.T) {
	e := newEnv(t, nil)
	const pid = "cccccccccccccccccccccccccccccccc"
	edited := append([]byte{}, jpeg...)
	edited = append(edited, " edited"...)
	itemPhotoFiles(t, e.touch, pid, "u_donor", edited)
	f := okFields()
	f["photo_id"] = pid
	w, body := call(e.h, form(t, "u_donor", f, nil, ""))
	if w.Code != 201 {
		t.Fatalf("%d %v", w.Code, body)
	}
	pw, _ := call(e.h, get("u_x", body["photo_url"].(string)))
	if !bytes.Equal(pw.Body.Bytes(), edited) {
		t.Fatal("not the edited photo")
	}
	// the meta file must name the same id
	os.WriteFile(filepath.Join(e.touch, pid+".json"), []byte(`{"photo_id":"dddddddddddddddddddddddddddddddd","user_id":"u_donor"}`), 0o600)
	if w, _ := call(e.h, form(t, "u_donor", f, nil, "")); w.Code != 400 {
		t.Fatalf("a meta file of another id: %d", w.Code)
	}
}

// ---- browse, mine, expiry -------------------------------------------------

func TestActivePagesAndExpiry(t *testing.T) {
	e := newEnv(t, nil)
	var ids []string
	for i := 0; i < 23; i++ {
		ids = append(ids, e.create(t, "u_donor").ID)
		e.tick(time.Second)
	}
	page := func(after string) ([]any, string) {
		w, body := call(e.h, get("u_r", "/api/listings?after="+after))
		if w.Code != 200 {
			t.Fatalf("%d", w.Code)
		}
		return body["items"].([]any), body["next"].(string)
	}
	p1, next := page("")
	if len(p1) != PageSize || next != ids[3] || p1[0].(map[string]any)["id"] != ids[22] {
		t.Fatalf("page 1: %d next %s", len(p1), next)
	}
	p2, next2 := page(next)
	if len(p2) != 3 || next2 != "" || p2[2].(map[string]any)["id"] != ids[0] {
		t.Fatalf("page 2: %d next %q", len(p2), next2)
	}
	if p, n := page("l_unknown"); len(p) != 0 || n != "" {
		t.Fatalf("unknown cursor: %d %q", len(p), n)
	}
	if items, _ := e.st.Active("", 5); len(items) != 5 {
		t.Fatalf("limit 5: %d", len(items))
	}
	// the server decides expiry: a listing is active until 30 minutes after
	// it was made; at 30:00 it is gone (ids[0] was made at 18:00:00)
	*e.clock = time.Date(2026, 10, 28, 18, 29, 59, 0, time.UTC)
	if _, err := e.st.Grab(ids[1], "u_probe"); err != nil {
		t.Fatalf("at 29:59 a listing of 18:00:01 is still active: %v", err)
	}
	*e.clock = time.Date(2026, 10, 28, 18, 30, 0, 0, time.UTC)
	if _, err := e.st.Grab(ids[0], "u_probe"); err != ErrExpired {
		t.Fatalf("at 30:00 the listing of 18:00:00 must be expired: %v", err)
	}
	*e.clock = e.clock.Add(time.Hour)
	if p, _ := page(""); len(p) != 0 {
		t.Fatalf("all expired: %d", len(p))
	}
	w, body := call(e.h, get("u_donor", "/api/listings/mine"))
	if w.Code != 200 || len(body["items"].([]any)) != 23 {
		t.Fatalf("mine keeps expired ones: %d", len(body["items"].([]any)))
	}
	w, body = call(e.h, get("u_nobody", "/api/listings/mine"))
	if items, ok := body["items"].([]any); w.Code != 200 || !ok || len(items) != 0 {
		t.Fatalf("mine is an empty list, not null: %s", w.Body)
	}
}

func TestEmptyListIsAnArray(t *testing.T) {
	e := newEnv(t, nil)
	w, _ := call(e.h, get("u", "/api/listings"))
	if strings.TrimSpace(w.Body.String()) != `{"items":[],"next":""}` {
		t.Fatalf("body %s", w.Body)
	}
}

// ---- grab -----------------------------------------------------------------

func TestGrabRules(t *testing.T) {
	e := newEnv(t, nil)
	l := e.create(t, "u_donor")
	for _, tc := range []struct {
		user, id string
		code     int
		msg      string
	}{
		{"", l.ID, 401, "not logged in"},
		{"u_r", "l_0000000000000000", 404, "listing not found"},
		{"u_donor", l.ID, 403, "cannot grab your own listing"},
	} {
		w, body := call(e.h, post(tc.user, "/api/listings/"+tc.id+"/grab"))
		if w.Code != tc.code || body["error"] != tc.msg {
			t.Errorf("%s: %d %v", tc.user, w.Code, body)
		}
	}
	e.tick(time.Minute)
	w, body := call(e.h, post("u_r", "/api/listings/"+l.ID+"/grab"))
	if w.Code != 200 || body["status"] != "collected" || body["collected_by"] != "u_r" || body["collected_at"] != "2026-10-28T18:01:00Z" {
		t.Fatalf("grab: %d %v", w.Code, body)
	}
	w, body = call(e.h, post("u_s", "/api/listings/"+l.ID+"/grab"))
	if w.Code != 409 || body["error"] != "listing already collected" {
		t.Fatalf("second grab: %d %v", w.Code, body)
	}
	old := e.create(t, "u_donor")
	e.tick(31 * time.Minute)
	w, body = call(e.h, post("u_r", "/api/listings/"+old.ID+"/grab"))
	if w.Code != 409 || body["error"] != "listing expired" {
		t.Fatalf("expired: %d %v", w.Code, body)
	}
}

func TestGrabIsOneWinner(t *testing.T) {
	e := newEnv(t, nil)
	l := e.create(t, "u_donor")
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := e.st.Grab(l.ID, fmt.Sprintf("u_%d", i)); err == nil {
				wins.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("%d winners", wins.Load())
	}
}

// A grab that cannot be written changes nothing: the listing can be grabbed
// again when the disk is back (new in V3RDE: before, the memory said
// "collected" and the file did not). A folder where the temp file must go
// makes the write fail, also as root.
func TestGrabMemoryRestoredWhenSaveFails(t *testing.T) {
	e := newEnv(t, nil)
	l := e.create(t, "u_donor")
	os.Mkdir(filepath.Join(e.dir, "listings.json.tmp"), 0o700)
	if _, err := e.st.Grab(l.ID, "u_r"); err == nil {
		t.Fatal("the write must fail")
	}
	if got := e.st.Mine("u_donor")[0]; got.Status != "active" {
		t.Fatalf("memory changed: %+v", got)
	}
	w, body := call(e.h, post("u_r", "/api/listings/"+l.ID+"/grab"))
	if w.Code != 500 || body["error"] != "could not grab" {
		t.Fatalf("%d %v", w.Code, body)
	}
	os.Remove(filepath.Join(e.dir, "listings.json.tmp"))
	if w, _ := call(e.h, post("u_r", "/api/listings/"+l.ID+"/grab")); w.Code != 200 {
		t.Fatalf("after the fix: %d", w.Code)
	}
}

func TestCreateWriteFailureLeavesNoPhoto(t *testing.T) {
	e := newEnv(t, nil)
	os.Mkdir(filepath.Join(e.dir, "listings.json.tmp"), 0o700)
	if _, err := e.st.Create("u", Input{Title: "a", NumberOfServing: 1, PickupText: "b", Photo: jpeg, Mime: "image/jpeg"}); err == nil {
		t.Fatal("the write must fail")
	}
	if fs, _ := os.ReadDir(e.st.photos); len(fs) != 0 {
		t.Fatalf("photo left: %v", fs)
	}
	if len(e.st.Mine("u")) != 0 {
		t.Fatal("record left in memory")
	}
}

// ---- photo, auth, healthz -------------------------------------------------

func TestPhotoRoute(t *testing.T) {
	e := newEnv(t, nil)
	l := e.create(t, "u_donor")
	w, _ := call(e.h, get("u_r", l.PhotoURL))
	if w.Code != 200 || w.Header().Get("Cache-Control") != "private, max-age=3600" || !bytes.Equal(w.Body.Bytes(), jpeg) {
		t.Fatalf("%d %v", w.Code, w.Header())
	}
	if w, _ := call(e.h, get("", l.PhotoURL)); w.Code != 401 {
		t.Fatalf("photo without a session: %d", w.Code)
	}
	if w, body := call(e.h, get("u_r", "/api/listings/l_nope/photo")); w.Code != 404 || body["error"] != "listing not found" {
		t.Fatalf("unknown photo: %d", w.Code)
	}
}

func TestEveryRouteButHealthzNeedsASession(t *testing.T) {
	e := newEnv(t, nil)
	for _, r := range []*http.Request{get("", "/api/listings"), get("", "/api/listings/mine"),
		post("", "/api/listings/l_x/grab"), get("", "/api/listings/l_x/photo"), post("", "/api/listings")} {
		if w, body := call(e.h, r); w.Code != 401 || body["error"] != "not logged in" {
			t.Errorf("%s %s: %d", r.Method, r.URL.Path, w.Code)
		}
	}
	w, body := call(e.h, get("", "/api/listings/healthz"))
	if w.Code != 200 || body["ok"] != true || body["service"] != "listings" {
		t.Fatalf("healthz %d %v", w.Code, body)
	}
}

func TestAuthClientSendsOnlyTheSessionCookie(t *testing.T) {
	var seen *http.Request
	auth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r
		if r.URL.Path != "/api/auth/me" {
			w.WriteHeader(404)
			return
		}
		if ck, err := r.Cookie(sessionCookie); err != nil || ck.Value != "s-good" {
			w.WriteHeader(401)
			return
		}
		fmt.Fprint(w, `{"id":"u_7","email":"seven@example.org"}`)
	}))
	defer auth.Close()
	a := &AuthClient{URL: auth.URL + "/"}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: "s-good"})
	r.AddCookie(&http.Cookie{Name: "other", Value: "secret"})
	r.Header.Set("Authorization", "Bearer app-token")
	u, ok := a.Who(r)
	if !ok || u.ID != "u_7" || u.Email != "seven@example.org" {
		t.Fatalf("who %+v %v", u, ok)
	}
	if seen.Header.Get("Authorization") != "" || strings.Contains(seen.Header.Get("Cookie"), "other") {
		t.Fatalf("forwarded more than the session cookie: %v", seen.Header)
	}
	r2 := httptest.NewRequest(http.MethodGet, "/", nil)
	r2.AddCookie(&http.Cookie{Name: sessionCookie, Value: "s-bad"})
	if _, ok := a.Who(r2); ok {
		t.Fatal("a 401 from auth is no user")
	}
	if _, ok := a.Who(httptest.NewRequest(http.MethodGet, "/", nil)); ok {
		t.Fatal("no cookie is no user")
	}
	down := &AuthClient{URL: "http://127.0.0.1:1"}
	if _, ok := down.Who(r); ok {
		t.Fatal("auth down is no user")
	}
}

// ---- the claim hook -------------------------------------------------------

type fanoutFake struct {
	*httptest.Server
	mu      sync.Mutex
	answers []int // one per call; the last one repeats
	calls   int
	bodies  []string
	auth    []string
}

func newFanout(t *testing.T, answers ...int) *fanoutFake {
	t.Helper()
	f := &fanoutFake{answers: answers}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		var b bytes.Buffer
		b.ReadFrom(r.Body)
		f.bodies = append(f.bodies, b.String())
		f.auth = append(f.auth, r.Header.Get("Authorization"))
		i := f.calls
		if i >= len(f.answers) {
			i = len(f.answers) - 1
		}
		f.calls++
		w.WriteHeader(f.answers[i])
	}))
	t.Cleanup(f.Close)
	return f
}

type outcome struct {
	status, tries int
	err           error
}

func hookFor(f *fanoutFake, waits int) (*ClaimHook, chan outcome) {
	done := make(chan outcome, 1)
	h := &ClaimHook{URL: f.URL, Token: "claim-secret", Backoff: make([]time.Duration, waits),
		Done: func(_ string, status, tries int, err error) { done <- outcome{status, tries, err} }}
	return h, done
}

func wait(t *testing.T, done chan outcome) outcome {
	t.Helper()
	select {
	case o := <-done:
		return o
	case <-time.After(5 * time.Second):
		t.Fatal("the hook did not finish")
	}
	return outcome{}
}

func TestGrabSendsTheClaim(t *testing.T) {
	f := newFanout(t, 200)
	h, done := hookFor(f, 3)
	e := newEnv(t, h)
	l := e.create(t, "u_donor")
	if w, _ := call(e.h, post("u_r", "/api/listings/"+l.ID+"/grab")); w.Code != 200 {
		t.Fatalf("grab %d", w.Code)
	}
	o := wait(t, done)
	if o.status != 200 || o.tries != 1 || o.err != nil {
		t.Fatalf("outcome %+v", o)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.bodies[0] != `{"listing_id":"`+l.ID+`","status":"claimed"}` || f.auth[0] != "Bearer claim-secret" {
		t.Fatalf("call %q %q", f.bodies[0], f.auth[0])
	}
}

func TestClaimHookRetryRules(t *testing.T) {
	for _, tc := range []struct {
		name    string
		answers []int
		waits   int
		status  int
		tries   int
		isErr   bool
	}{
		{"pending then done", []int{409, 409, 200}, 6, 200, 3, false},
		{"rate limited then done", []int{429, 200}, 6, 200, 2, false},
		{"5xx then no post", []int{502, 500, 404}, 6, 404, 3, false},
		{"posting off: stop", []int{503}, 6, 503, 1, false},
		{"bad token: stop", []int{401}, 6, 401, 1, true},
		{"always pending: give up", []int{409}, 2, 409, 3, true},
	} {
		f := newFanout(t, tc.answers...)
		h, done := hookFor(f, tc.waits)
		h.Claimed("l_1")
		o := wait(t, done)
		if o.status != tc.status || o.tries != tc.tries || (o.err != nil) != tc.isErr {
			t.Errorf("%s: %+v", tc.name, o)
		}
	}
}

func TestClaimHookNoAnswerRetries(t *testing.T) {
	h := &ClaimHook{URL: "http://127.0.0.1:1/v1/fanout/status", Backoff: []time.Duration{0, 0}}
	done := make(chan outcome, 1)
	h.Done = func(_ string, s, n int, err error) { done <- outcome{s, n, err} }
	h.Claimed("l_1")
	if o := wait(t, done); o.tries != 3 || o.err == nil || o.status != 0 {
		t.Fatalf("%+v", o)
	}
	var none *ClaimHook
	none.Claimed("l_1")                       // nil: nothing
	(&ClaimHook{Done: h.Done}).Claimed("l_1") // no URL: nothing
	select {
	case o := <-done:
		t.Fatalf("an off hook ran: %+v", o)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestDefaultBackoffOutlastsAShare(t *testing.T) {
	var sum time.Duration
	for _, d := range DefaultBackoff {
		sum += d
	}
	if sum < 95*time.Second {
		t.Fatalf("%s is shorter than the longest share (95 s)", sum)
	}
}

func TestReadToken(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "t")
	os.WriteFile(p, []byte("  tok-1\n"), 0o600)
	if s, err := ReadToken(p); err != nil || s != "tok-1" {
		t.Fatalf("%q %v", s, err)
	}
	os.WriteFile(p, []byte(" \n"), 0o600)
	if _, err := ReadToken(p); err == nil || !strings.HasSuffix(err.Error(), "is empty") {
		t.Fatalf("empty: %v", err)
	}
	if _, err := ReadToken(filepath.Join(dir, "none")); err == nil {
		t.Fatal("missing file")
	}
}

// ---- the events log -------------------------------------------------------

// readEvents reads the one events file in dir (a test that crosses
// midnight UTC would have two; it fails then instead of passing empty).
func readEvents(t *testing.T, dir string) ([]Event, []byte) {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(dir, "events-*.jsonl"))
	if len(files) != 1 {
		t.Fatalf("want one events file, got %v", files)
	}
	b, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	var out []Event
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var e Event
		if err := json.Unmarshal([]byte(l), &e); err != nil {
			t.Fatalf("line %q: %v", l, err)
		}
		out = append(out, e)
	}
	return out, b
}

func TestLoggedLine(t *testing.T) {
	e := newEnv(t, nil)
	dir := t.TempDir()
	rec := NewRecorder(dir, 16)
	h := Logged("listings", "abc1234", rec, e.h)
	l := e.create(t, "u_donor")
	r := post("u_r", "/api/listings/"+l.ID+"/grab?x=secret")
	r.Header.Set("X-Trace-Id", "scan-42")
	r.Header.Set("X-Forwarded-For", " 198.51.100.9 , 10.0.0.1")
	r.RemoteAddr = "172.18.0.4:5000"
	h.ServeHTTP(httptest.NewRecorder(), r)
	bad := get("u_r", "/nope")
	bad.Header.Set("X-Trace-Id", "has spaces")
	h.ServeHTTP(httptest.NewRecorder(), bad)
	rec.Close()
	ev, b := readEvents(t, dir)
	if len(ev) != 2 || rec.Written() != 2 || rec.Dropped() != 0 {
		t.Fatalf("events %d written %d dropped %d", len(ev), rec.Written(), rec.Dropped())
	}
	g := ev[0]
	if g.Route != "POST /api/listings/{id}/grab" || g.Status != 200 || g.TraceID != "scan-42" || g.Service != "listings" ||
		g.Step != "request" || g.Build != "abc1234" || g.RemoteIP != "172.18.0.4" || g.ForwardedIP != "198.51.100.9" ||
		g.Bytes == 0 || len(g.RequestID) != 32 {
		t.Fatalf("event %+v", g)
	}
	if ev[1].Route != "(unmatched)" || ev[1].Status != 404 || ev[1].TraceID != ev[1].RequestID {
		t.Fatalf("unmatched %+v", ev[1])
	}
	for _, s := range []string{"secret", l.ID, "u_r", "@example.org"} {
		if bytes.Contains(b, []byte(s)) {
			t.Fatalf("the events log has %q", s)
		}
	}
}

func TestRecorderNeverBlocks(t *testing.T) {
	rec := NewRecorder(filepath.Join(t.TempDir(), "missing"), 1) // the folder does not exist: writes fail
	start := time.Now()
	for i := 0; i < 1000; i++ {
		rec.Record(Event{Route: "x"})
	}
	if time.Since(start) > time.Second {
		t.Fatal("Record blocked")
	}
	rec.Close()
	if rec.Written() != 0 || rec.Dropped() != 1000 {
		t.Fatalf("written %d dropped %d", rec.Written(), rec.Dropped())
	}
	var none *Recorder
	none.Record(Event{}) // a nil recorder does nothing
}

// X44h (T9 2026-10-29): a test account may set its listing's timer
// (ttl_seconds, 30 s to 30 min) so the rescue test does not wait 30 minutes;
// any other caller gets 403 and no listing; without the field the timer is
// the store's 30 minutes.
func TestTestAccountTTL(t *testing.T) {
	e := newEnv(t, nil)
	e.h = New(Config{Store: e.st, Auth: fakeAuth{}, TestTTLEmails: map[string]bool{"u_test@example.org": true}})
	with := func(user, ttl string) (*httptest.ResponseRecorder, map[string]any) {
		f := okFields()
		f["ttl_seconds"] = ttl
		return call(e.h, form(t, user, f, jpeg, "image/jpeg"))
	}
	w, body := with("u_test", "30")
	if w.Code != 201 {
		t.Fatalf("test account: %d %v", w.Code, body)
	}
	created, _ := time.Parse(time.RFC3339Nano, body["created_at"].(string))
	expires, _ := time.Parse(time.RFC3339Nano, body["expires_at"].(string))
	if expires.Sub(created) != 30*time.Second {
		t.Fatalf("timer %v", expires.Sub(created))
	}
	id := body["id"].(string)
	e.tick(30 * time.Second)
	if _, err := e.st.Grab(id, "u_r"); err != ErrExpired {
		t.Fatalf("after 30 s: %v", err)
	}
	for _, c := range []struct {
		user, ttl string
		code      int
		msg       string
	}{
		{"u_other", "30", 403, "ttl_seconds is for test accounts only"},
		{"u_test", "29", 400, "ttl_seconds must be 30-1800"},
		{"u_test", "1801", 400, "ttl_seconds must be 30-1800"},
		{"u_test", "soon", 400, "ttl_seconds must be 30-1800"},
	} {
		if w, body := with(c.user, c.ttl); w.Code != c.code || body["error"] != c.msg {
			t.Errorf("%s ttl %s: %d %v", c.user, c.ttl, w.Code, body)
		}
	}
	if n := len(e.st.Mine("u_other")); n != 0 {
		t.Fatal("a refused ttl made a listing")
	}
	l := e.create(t, "u_test") // no field: the store's timer
	c2, _ := time.Parse(time.RFC3339Nano, l.CreatedAt)
	x2, _ := time.Parse(time.RFC3339Nano, l.ExpiresAt)
	if x2.Sub(c2) != 30*time.Minute {
		t.Fatalf("default timer %v", x2.Sub(c2))
	}
}

// X48d: the scanner's details are kept with the listing (the answer, the
// data file, the active list and mine); none: no "details" key; a bad
// details field is a 400 and stores nothing.
func TestCreateKeepsDetails(t *testing.T) {
	e := newEnv(t, nil)
	f := okFields()
	f["details"] = `{"description":" Baked pasta with beef ","is_non_veg":1,"serving_type":"bulk",` +
		`"meal_condition":"ready_to_eat","weight":2.5,"meal_cost":0}`
	w, body := call(e.h, form(t, "u_donor", f, jpeg, "image/jpeg"))
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	want := map[string]any{"description": "Baked pasta with beef", "is_non_veg": 1.0, "serving_type": "bulk",
		"meal_condition": "ready_to_eat", "weight": 2.5, "meal_cost": 0.0}
	if fmt.Sprint(body["details"]) != fmt.Sprint(want) {
		t.Fatalf("details %v", body["details"])
	}
	_, list := call(e.h, get("u_other", "/api/listings"))
	_, mine := call(e.h, get("u_donor", "/api/listings/mine"))
	for name, m := range map[string]map[string]any{"active": list, "mine": mine} {
		items, _ := m["items"].([]any)
		if len(items) != 1 || fmt.Sprint(items[0].(map[string]any)["details"]) != fmt.Sprint(want) {
			t.Fatalf("%s: %v", name, m)
		}
	}
	st, err := Open(e.dir, 30*time.Minute) // the data file keeps them
	if err != nil || len(st.Mine("u_donor")) != 1 || st.Mine("u_donor")[0].Details == nil ||
		*st.Mine("u_donor")[0].Details.Weight != 2.5 {
		t.Fatalf("reopen: %v %+v", err, st.Mine("u_donor"))
	}
	// no details, or an empty object: no key
	for _, d := range []string{"", "{}", ` {"description":"  "} `} {
		g := okFields()
		if d != "" {
			g["details"] = d
		}
		w, b := call(e.h, form(t, "u_donor", g, jpeg, "image/jpeg"))
		if _, has := b["details"]; w.Code != 201 || has {
			t.Fatalf("details %q: %d %v", d, w.Code, b)
		}
	}
	n := len(e.st.Mine("u_donor"))
	for d, msg := range map[string]string{
		`{"name":"x"}`:             "details must be a JSON object of description, is_non_veg, serving_type, meal_condition, weight, meal_cost",
		`[1]`:                      "details must be a JSON object of description, is_non_veg, serving_type, meal_condition, weight, meal_cost",
		`{"weight":"2"}`:           "details must be a JSON object of description, is_non_veg, serving_type, meal_condition, weight, meal_cost",
		`{"weight":1}]`:            "details must be a JSON object of description, is_non_veg, serving_type, meal_condition, weight, meal_cost",
		`{} {}`:                    "details must be a JSON object of description, is_non_veg, serving_type, meal_condition, weight, meal_cost",
		`{"is_non_veg":2}`:         "details.is_non_veg must be 0 or 1",
		`{"serving_type":"tray"}`:  "details.serving_type must be individual or bulk",
		`{"meal_condition":"hot"}`: "details.meal_condition must be ready_to_eat, cooking_required or sealed",
		`{"weight":-1}`:            "details.weight must be 0-1000 kg",
		`{"meal_cost":100001}`:     "details.meal_cost must be 0-100000",
		`{"description":"` + strings.Repeat("a", 201) + `"}`:  "details.description must be at most 200 characters",
		`{"description":"` + strings.Repeat("a", 1100) + `"}`: "details must be at most 1024 bytes",
	} {
		g := okFields()
		g["details"] = d
		w, b := call(e.h, form(t, "u_donor", g, jpeg, "image/jpeg"))
		if w.Code != 400 || b["error"] != msg {
			t.Errorf("details %.40q: %d %v", d, w.Code, b)
		}
	}
	if len(e.st.Mine("u_donor")) != n {
		t.Fatal("a refused details field stored a listing")
	}
	// 200 characters of 4 bytes each are taken (the app counts characters)
	g := okFields()
	g["details"] = `{"description":"` + strings.Repeat("🍕", 200) + `","weight":1}`
	if w, b := call(e.h, form(t, "u_donor", g, jpeg, "image/jpeg")); w.Code != 201 {
		t.Fatalf("200 emoji: %d %v", w.Code, b)
	}
}
