package post

// Rescue (X35b). A listing that nobody claimed before its timer ran out can
// still go somewhere: the donor asks a food bank (shelf-stable food) or a
// meal program (perishable food) near them. Muse Spark 1.3 looks at the
// listing photo, decides shelf-stable or not, and writes the inquiry; the
// Feed America directory gives the nearest places; the donor reads the
// draft and sends it (mailto / phone, or the server's mail relay when one is
// configured). Nothing is sent without the donor's action.
//
//	POST /api/rescue/{listing_id}   {"zip":"10001"} or {"lat":40.7,"lng":-74.0}
//	     -> 200 Rescue (the draft, the places)            session cookie, owner only
//	GET  /api/rescue/{rescue_id}    -> 200 Rescue
//	POST /api/rescue/{rescue_id}/send {"place_id","to_email"[,"subject","body"]}
//	     -> 200 {sent:true}  (503 disabled when no mail relay is configured)
//	GET  /healthz
//
// Rules: the listing must be the caller's, still "active" (not collected)
// and past expires_at. The directory has no email field that we could
// confirm, so the donor gives the address (from the place's site or a call)
// and a bulk answer always has mailto: and tel: links.
//
// R3 (T9 2026-10-31: "instead of an email it suggests that the user can
// transport the food to one of 3 locations near them based on the pickup
// instructions. the emails should only be for bulk items"; bulk = 30
// servings or the scanner says bulk, "Either one"; "Map directions link"):
//   - bulk (number_of_serving >= MinServings, or details.serving_type "bulk"
//     from a vision scan): mode "email", the X35b flow (draft, mailto, /send);
//   - else: mode "dropoff": Muse decides only shelf_stable (no draft), and
//     each place has a Google Maps directions link; /send answers 409.
//   - the search location: the ZIP at the end of the listing's pickup address
//     ("12 Main St, Denver, CO 80219, front desk"), else the body's zip or
//     lat/lng. The body is optional when the pickup address has a ZIP. The
//     pickup text is never written to the events line (X48u rule).

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	analyze "github.com/TwoThousandAndTwo2002/Icarus/server"
)

// Kinds of place a rescue looks for.
const (
	KindFoodBank    = "food_bank"    // shelf-stable food: pantries and food banks
	KindMealProgram = "meal_program" // perishable food: soup kitchens, meal programs, shelters
)

// DirectoryAttribution is the CC BY 4.0 credit the answer carries.
const DirectoryAttribution = "Feed America (feedam.org), CC BY 4.0"

// RescuePrompt is the Muse Spark instruction. %s are the title and the
// servings; the listing photo follows as an image.
const RescuePrompt = `A donor listed this food on a food-sharing app and nobody picked it up in time.
Title: %s
Servings: %d
Look at the photo. Return ONLY valid JSON:
{"shelf_stable": true or false, "reason": "one sentence, 12 words maximum",
 "subject": "an email subject, 8 words maximum",
 "body": "an email body, 150 words maximum"}
shelf_stable is true only for sealed food that keeps without a refrigerator (cans, dry goods, sealed jars).
The email asks a local food program whether it can accept this donation, says what the food is and how much,
and asks when the donor can drop it off. Do not say the food is safe, do not give dates or temperatures,
do not invent facts about the food. Do not add a greeting name or a signature.`

// R1 (T9 2026-10-30, option A): the food scanner already says how the food
// was packed when it was listed (the contract field meal_condition, kept with
// the listing as details, X48d). Rescue gives it to Muse Spark as a hint, with
// the scanner's description; Muse still decides shelf_stable from the photo,
// because "sealed" is not the same as "keeps without a refrigerator" (sealed
// yogurt). A listing without the field gets the prompt of X35b, byte for byte.
// A barcode answer's meal_condition is a fixed value (barcode/product.go), not
// something the scanner saw, so the hint is used only when the scan's own
// events line (the analyze events, read-only) says the route was "vision" for
// the same user: see scanSource. Any other case gives no hint.

// RescueHint is the line rescuePrompt puts after "Servings:". %s are the
// condition words and the description part.
const RescueHint = "Scanner note, made when the food was listed (it can be wrong; the photo decides): %s%s.\n"

// hintWords are the words of each meal_condition value (server/contract.go).
var hintWords = map[string]string{
	"sealed":           "the food is in a sealed package",
	"ready_to_eat":     "the food is ready to eat",
	"cooking_required": "the food needs cooking",
}

// MaxHintDescription is the most characters (runes) of the description in the
// note: the listings service keeps at most 200 (listings/details.go).
const MaxHintDescription = 200

// rescuePrompt is the Muse instruction for listing l, whose scan took route
// source (scanSource), and the meal_condition value it used as a hint (""
// when it used none: no field, a value not in the contract, or a route that
// is not "vision").
func rescuePrompt(l Listing, source string) (string, string) {
	return promptFor(RescuePrompt, l, source)
}

// dropoffPrompt (R3) is rescuePrompt with DropoffPrompt.
func dropoffPrompt(l Listing, source string) (string, string) {
	return promptFor(DropoffPrompt, l, source)
}

// promptFor is tmpl (RescuePrompt or DropoffPrompt) for listing l, with the
// scanner note when there is one.
func promptFor(tmpl string, l Listing, source string) (string, string) {
	note, cond := "", ""
	if l.Details != nil && source == ScanVision {
		if words, ok := hintWords[l.Details.MealCondition]; ok {
			cond = l.Details.MealCondition
			desc := ""
			if d := oneLine(l.Details.Description, MaxHintDescription); d != "" {
				desc = "; the scanner described it as: " + d
			}
			note = fmt.Sprintf(RescueHint, words, desc)
		}
	}
	if note == "" {
		return fmt.Sprintf(tmpl, l.Title, l.NumberOfServing), ""
	}
	// the note goes in as an argument, so a "%" in the description stays text
	t := strings.Replace(tmpl, "Servings: %d\n", "Servings: %d\n%s", 1)
	return fmt.Sprintf(t, l.Title, l.NumberOfServing, note), cond
}

// oneLine is s with control and format characters (new lines, and bidi or
// zero-width marks too) as spaces, runs of spaces as one, trimmed, and at
// most n runes.
func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return ' '
		}
		return r
	}, s)), " ")
	if r := []rune(s); len(r) > n {
		s = strings.TrimSpace(string(r[:n]))
	}
	return s
}

// DropoffPrompt (R3) is the Muse instruction when the donor takes the food to
// a place (not bulk): no email, only the decision. %s, %d as RescuePrompt;
// the scanner note goes after "Servings:" as in RescuePrompt.
const DropoffPrompt = `A donor listed this food on a food-sharing app and nobody picked it up in time.
The donor will take it to a local food program.
Title: %s
Servings: %d
Look at the photo. Return ONLY valid JSON:
{"shelf_stable": true or false, "reason": "one sentence, 12 words maximum"}
shelf_stable is true only for sealed food that keeps without a refrigerator (cans, dry goods, sealed jars).
Do not say the food is safe, do not give dates or temperatures, do not invent facts about the food.`

// The modes of a rescue (R3).
const (
	ModeEmail   = "email"   // bulk: the donor asks the place first (draft, mailto, /send)
	ModeDropoff = "dropoff" // the donor takes the food to one of the places
)

// Place is one directory result as the app shows it.
type Place struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Type          string  `json:"type,omitempty"`
	Address       string  `json:"address,omitempty"`
	Phone         string  `json:"phone,omitempty"`
	DistanceMiles float64 `json:"distance_miles,omitempty"`
	Website       string  `json:"website,omitempty"`
	Email         string  `json:"email,omitempty"`      // only when the directory gives one
	Source        string  `json:"source,omitempty"`     // X44l: the record's data_source (the directory names it per record)
	Tel           string  `json:"tel,omitempty"`        // tel: link
	Mailto        string  `json:"mailto,omitempty"`     // R3: bulk (mode email) only
	Directions    string  `json:"directions,omitempty"` // R3: a Google Maps directions link to the place
	SourceURL     string  `json:"source_url,omitempty"` // R4: the web page that shows a place the web search found
}

// SourceWebSearch is Place.Source for a place the web search agent found (R4).
const SourceWebSearch = "web_search"

// The states of a rescue (R4). A rescue made without ?async=1, or stored
// before R4, has no status: it is done.
const (
	StatusRunning = "running"
	StatusDone    = "done"
	StatusFailed  = "failed"
)

// Draft is the inquiry Muse wrote.
type Draft struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// Rescue is one rescue (the GET answer; stored).
type Rescue struct {
	RescueID    string  `json:"rescue_id"`
	RequestID   string  `json:"request_id"`
	ListingID   string  `json:"listing_id"`
	OwnerID     string  `json:"owner_id"`
	Title       string  `json:"title"`
	Servings    int     `json:"number_of_serving"`
	Kind        string  `json:"kind"`
	Mode        string  `json:"mode,omitempty"` // R3: email | dropoff ("" = a rescue from before R3, email)
	ShelfStable bool    `json:"shelf_stable"`
	Reason      string  `json:"reason"`
	Draft       Draft   `json:"draft"`
	Places      []Place `json:"places"`
	Attribution string  `json:"attribution"`
	CreatedAt   string  `json:"created_at"`
	CostUSD     float64 `json:"cost_usd"`
	Sent        []Sent  `json:"sent"`
	// R4: the job. Status running | done | failed; Code and Error tell why it failed.
	Status  string `json:"status,omitempty"`
	Code    string `json:"code,omitempty"`
	Error   string `json:"error,omitempty"`
	FoundBy string `json:"found_by,omitempty"` // directory | web_search | none
}

// Sent is one email the relay sent for the donor.
type Sent struct {
	PlaceID string `json:"place_id"`
	To      string `json:"to"`
	At      string `json:"at"`
}

// ---- the directory ---------------------------------------------------------

// Directory is the Feed America public API (feedam.org/developer-portal):
// GET /api/resources/search?zip=&mode=free and /api/resources/nearby?lat=&lng=&mode=free,
// no auth, results in "results".
type Directory struct {
	Base string // https://feedam.org
	HTTP *http.Client
}

// Near returns the places near zip (or lat/lng), mode free (pantries, soup
// kitchens, mobile pantries).
func (d *Directory) Near(ctx context.Context, zip string, lat, lng *float64) ([]Place, int64, error) {
	base := strings.TrimRight(d.Base, "/")
	if base == "" {
		base = "https://feedam.org"
	}
	q := url.Values{"mode": {"free"}, "limit": {"20"}}
	path := "/api/resources/search"
	if zip != "" {
		q.Set("zip", zip)
	} else {
		path = "/api/resources/nearby"
		q.Set("lat", strconv.FormatFloat(*lat, 'f', 5, 64))
		q.Set("lng", strconv.FormatFloat(*lng, 'f', 5, 64))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path+"?"+q.Encode(), nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	hc := d.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	t0 := time.Now()
	resp, err := hc.Do(req)
	if err != nil {
		return nil, time.Since(t0).Milliseconds(), fmt.Errorf("directory: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	ms := time.Since(t0).Milliseconds()
	if err != nil {
		return nil, ms, fmt.Errorf("directory read: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, ms, fmt.Errorf("directory: HTTP %d", resp.StatusCode)
	}
	places, err := ParsePlaces(raw)
	return places, ms, err
}

// ParsePlaces reads the directory answer. The field names of a result are
// not in the public docs that we could read (the developer portal shows
// "results"; the llm-search result has id, name, type, address, phone,
// distance_miles), so each field accepts the likely names and an address
// may be a string or an object.
func ParsePlaces(raw []byte) ([]Place, error) {
	var top map[string]any
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, fmt.Errorf("directory: bad JSON: %w", err)
	}
	var list []any
	for _, k := range []string{"resources", "results", "items", "data"} { // X44l: the live answer has "resources"
		if l, ok := top[k].([]any); ok {
			list = l
			break
		}
	}
	if list == nil {
		return nil, errors.New("directory: no results array")
	}
	var out []Place
	for i, it := range list {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		p := Place{
			ID:      firstStr(m, "id", "resource_id", "location_id"),
			Name:    firstStr(m, "name", "title", "organization_name"),
			Type:    firstStr(m, "type", "resource_type", "category"),
			Address: addressOf(m),
			Phone:   firstStr(m, "phone", "phone_number", "telephone"),
			Website: firstStr(m, "website", "url", "web"),
			Email:   firstStr(m, "email", "contact_email"),
			Source:  firstStr(m, "data_source", "source"),
		}
		for _, k := range []string{"distance_miles", "distance_mi", "distance"} {
			if v, ok := m[k].(float64); ok {
				p.DistanceMiles = v
				break
			}
		}
		if p.ID == "" {
			if v, ok := m["id"].(float64); ok {
				p.ID = strconv.FormatInt(int64(v), 10)
			} else {
				p.ID = "p" + strconv.Itoa(i)
			}
		}
		if p.Name == "" {
			continue
		}
		if p.Email != "" {
			if a, err := mail.ParseAddress(p.Email); err != nil || a.Address != p.Email {
				p.Email = ""
			}
		}
		if !strings.HasPrefix(p.Website, "https://") && !strings.HasPrefix(p.Website, "http://") {
			p.Website = ""
		}
		out = append(out, p)
	}
	return out, nil
}

func firstStr(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func addressOf(m map[string]any) string {
	for _, k := range []string{"address", "full_address", "location"} {
		switch v := m[k].(type) {
		case string:
			if a := strings.TrimSpace(v); a != "" {
				// X44l: the live answer has the street in "address" and the
				// city, state and zip as their own fields
				for _, f := range []string{"city", "state", "zip"} {
					if s, ok := m[f].(string); ok && strings.TrimSpace(s) != "" && !strings.Contains(a, strings.TrimSpace(s)) {
						a += ", " + strings.TrimSpace(s)
					}
				}
				return a
			}
		case map[string]any:
			var parts []string
			for _, f := range []string{"street", "address_1", "address1", "city", "state", "zip", "postal_code"} {
				if s, ok := v[f].(string); ok && strings.TrimSpace(s) != "" {
					parts = append(parts, strings.TrimSpace(s))
				}
			}
			if len(parts) > 0 {
				return strings.Join(parts, ", ")
			}
		}
	}
	parts := []string{}
	for _, f := range []string{"street", "city", "state", "zip"} {
		if s, ok := m[f].(string); ok && strings.TrimSpace(s) != "" {
			parts = append(parts, strings.TrimSpace(s))
		}
	}
	return strings.Join(parts, ", ")
}

var (
	bankRe = regexp.MustCompile(`(?i)pantry|food[ _-]?bank|mobile`) // X44l: types are like food_bank
	mealRe = regexp.MustCompile(`(?i)soup|kitchen|meal|shelter|congregate|hot food`)
)

// PickPlaces puts the places that match kind first (by type and name),
// nearest first inside each group, and keeps n.
func PickPlaces(all []Place, kind string, n int) []Place {
	re := bankRe
	if kind == KindMealProgram {
		re = mealRe
	}
	type sc struct {
		p     Place
		match bool
		i     int
	}
	var s []sc
	for i, p := range all {
		s = append(s, sc{p, re.MatchString(p.Type + " " + p.Name), i})
	}
	sort.SliceStable(s, func(a, b int) bool {
		if s[a].match != s[b].match {
			return s[a].match
		}
		da, db := s[a].p.DistanceMiles, s[b].p.DistanceMiles
		if da != db && da > 0 && db > 0 {
			return da < db
		}
		return s[a].i < s[b].i
	})
	var out []Place
	for _, x := range s {
		if len(out) == n {
			break
		}
		out = append(out, x.p)
	}
	return out
}

// ---- the mail relay (optional) ---------------------------------------------

// Mailer sends one plain-text email. smtp.SendMail upgrades to TLS when the
// server offers STARTTLS, and PlainAuth refuses to send the password
// without TLS (except to localhost).
type Mailer struct {
	Addr string // host:587
	User string
	Pass string
	From string // the Icarus sender address
	// Timeout bounds the whole SMTP exchange (dial, TLS, auth, data):
	// smtp.SendMail has no deadline, so a silent relay would hold the handler.
	Timeout time.Duration
	send    func(addr string, a smtp.Auth, from string, to []string, msg []byte) error
}

// sendTimeout is smtp.SendMail with a dial timeout and one deadline on the
// connection. TLS (STARTTLS) is required when the relay needs a password.
func (m *Mailer) sendTimeout(addr string, a smtp.Auth, from string, to []string, msg []byte) error {
	t := m.Timeout
	if t <= 0 {
		t = 20 * time.Second
	}
	conn, err := net.DialTimeout("tcp", addr, t)
	if err != nil {
		return err
	}
	if err := conn.SetDeadline(time.Now().Add(t)); err != nil {
		conn.Close()
		return err
	}
	host, _, _ := net.SplitHostPort(addr)
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return err
	}
	defer c.Close()
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			return err
		}
	} else if a != nil {
		return errors.New("smtp: the relay does not offer STARTTLS")
	}
	if a != nil {
		if err := c.Auth(a); err != nil {
			return err
		}
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	for _, rc := range to {
		if err := c.Rcpt(rc); err != nil {
			return err
		}
	}
	wc, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := wc.Write(msg); err != nil {
		return err
	}
	if err := wc.Close(); err != nil {
		return err
	}
	return c.Quit()
}

var crlf = regexp.MustCompile(`[\r\n]+`)

// Message builds the RFC 5322 message. Header values lose CR and LF (no
// header injection); the donor's address is the Reply-To.
func Message(from, to, replyTo, subject, body string) []byte {
	h := func(s string) string { return strings.TrimSpace(crlf.ReplaceAllString(s, " ")) }
	var b strings.Builder
	b.WriteString("From: " + h(from) + "\r\n")
	b.WriteString("To: " + h(to) + "\r\n")
	if replyTo != "" {
		b.WriteString("Reply-To: " + h(replyTo) + "\r\n")
	}
	b.WriteString("Subject: " + h(subject) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n")
	b.WriteString(strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\n", "\r\n"))
	b.WriteString("\r\n")
	return []byte(b.String())
}

// Send sends the message.
func (m *Mailer) Send(to, replyTo, subject, body string) error {
	host := m.Addr
	if i := strings.LastIndex(host, ":"); i > 0 {
		host = host[:i]
	}
	var auth smtp.Auth
	if m.User != "" {
		auth = smtp.PlainAuth("", m.User, m.Pass, host)
	}
	send := m.send
	if send == nil {
		send = m.sendTimeout
	}
	return send(m.Addr, auth, m.From, []string{to}, Message(m.From, to, replyTo, subject, body))
}

// ---- the store --------------------------------------------------------------

// RescueStore is one JSON file rewritten atomically (listings/listings.go).
type RescueStore struct {
	path    string
	mu      sync.Mutex
	items   map[string]Rescue
	pending map[string]bool // listing ids with a rescue being made
}

// OpenRescues opens <dir>/rescues.json.
func OpenRescues(dir string) (*RescueStore, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	s := &RescueStore{path: filepath.Join(dir, "rescues.json"), items: map[string]Rescue{}, pending: map[string]bool{}}
	b, err := os.ReadFile(s.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, err
	case len(b) > 0:
		if err := json.Unmarshal(b, &s.items); err != nil {
			return nil, fmt.Errorf("%s: %w", s.path, err)
		}
	}
	// R4: a job that was running when the service stopped does not run now
	stopped := false
	for id, r := range s.items {
		if r.Status == StatusRunning {
			r.Status, r.Code, r.Error = StatusFailed, "interrupted", "the service restarted: start the rescue again"
			s.items[id], stopped = r, true
		}
	}
	if stopped {
		if err := s.flushLocked(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *RescueStore) flushLocked() error {
	b, err := json.MarshalIndent(s.items, "", " ")
	if err != nil {
		return err
	}
	return WriteFileSync(s.path, b, 0o600)
}

// Put stores r.
func (s *RescueStore) Put(r Rescue) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, had := s.items[r.RescueID]
	s.items[r.RescueID] = r
	if err := s.flushLocked(); err != nil {
		if had {
			s.items[r.RescueID] = old
		} else {
			delete(s.items, r.RescueID)
		}
		return err
	}
	return nil
}

// Done (N1) is the end of a job, under one lock: it stores r and clears the
// mark of Begin, so a client that sees the end can start a new rescue at once
// (before, the mark was cleared after the events line, and a POST in that
// moment got 409 busy). When the file write fails it keeps failed in memory
// (R4 review: a job never stays "running") and returns the error.
func (s *RescueStore) Done(r, failed Rescue) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pending, r.ListingID)
	s.items[r.RescueID] = r
	err := s.flushLocked()
	if err != nil {
		s.items[failed.RescueID] = failed
		_ = s.flushLocked()
	}
	return err
}

// Get returns rescue id if owner owns it (one error for both cases).
func (s *RescueStore) Get(id, owner string) (Rescue, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.items[id]
	if !ok || r.OwnerID != owner {
		return Rescue{}, false
	}
	return r, true
}

// Begin returns the owner's rescue of listing lid when one exists; else it
// marks lid as being made and returns ok = true (End clears the mark). Two
// requests for one listing at the same time: the second gets busy = true.
func (s *RescueStore) Begin(lid, owner string) (existing Rescue, found, busy bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.items {
		if r.ListingID == lid && r.OwnerID == owner && r.Status != StatusFailed { // R4: a failed job can be run again
			return r, true, false
		}
	}
	if s.pending[lid] {
		return Rescue{}, false, true
	}
	s.pending[lid] = true
	return Rescue{}, false, false
}

// Existing returns the owner's rescue of listing lid that is not failed (the one Begin would give), with no mark.
// X50b: a second open of the rescue gets it before any check of where to search.
func (s *RescueStore) Existing(lid, owner string) (Rescue, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.items {
		if r.ListingID == lid && r.OwnerID == owner && r.Status != StatusFailed {
			return r, true
		}
	}
	return Rescue{}, false
}

// End clears the mark of Begin.
func (s *RescueStore) End(lid string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pending, lid)
}

// SentToday is how many emails owner sent on the UTC day of now.
func (s *RescueStore) SentToday(owner string, now time.Time) int {
	day := now.UTC().Format("2006-01-02")
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, r := range s.items {
		if r.OwnerID != owner {
			continue
		}
		for _, x := range r.Sent {
			if strings.HasPrefix(x.At, day) {
				n++
			}
		}
	}
	return n
}

// ---- the service ------------------------------------------------------------

// RescueConfig is everything the rescue handler needs (cmd/rescue).
type RescueConfig struct {
	Push        *PushClient // WS1: a message to the owner's app when a job ends; nil = off
	Muse        *Muse
	Directory   *Directory
	Listings    *ListingsClient
	Session     Authenticator
	Limiter     *Limiter
	SendLimiter *Limiter // /send only; nil = Limiter
	Budget      *Budget
	Events      *Events
	Store       *RescueStore
	Mailer      *Mailer // nil = no relay: /send answers 503 disabled
	MinServings int     // the bulk rule (T9 decides); 1 = every listing
	Places      int     // places in the answer (3)
	SendPerDay  int     // emails per donor per UTC day (3)
	Timeout     time.Duration
	Build       string
	// AnalyzeEvents (R1) is the analyze service's events folder, read-only:
	// the scan's route (barcode | vision) for the hint. "" = no hint ever.
	AnalyzeEvents string
	// R4: Search is the web search agent (nil = no fallback; only a job uses
	// it); JobTimeout is the limit of one job (?async=1), default 90 s.
	Search     *PlaceSearch
	JobTimeout time.Duration
	// X50d: the store of every place found (nil = none): used before the web search, and filled after each search.
	PlaceStore *PlaceStore
	now        func() time.Time
	sendMu     sync.Mutex // one /send at a time: the checks, the email and the record are one step
}

// zipRe is a US ZIP (5 digits, optional +4).
var zipRe = regexp.MustCompile(`^\d{5}(-\d{4})?$`)

// NewRescue builds the mux.
func NewRescue(c *RescueConfig) http.Handler {
	if c.Places <= 0 {
		c.Places = 3
	}
	if c.SendPerDay <= 0 {
		c.SendPerDay = 3
	}
	if c.MinServings <= 0 {
		c.MinServings = 1
	}
	if c.Timeout == 0 {
		c.Timeout = 25 * time.Second
	}
	if c.JobTimeout == 0 {
		c.JobTimeout = 90 * time.Second
	}
	if c.now == nil {
		c.now = time.Now
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		WriteJSON(w, 200, map[string]any{"ok": true, "service": "rescue", "build": c.Build,
			"spent_today_usd": c.Budget.Spent(), "cap_usd": c.Budget.Cap, "caller_cap_usd": c.Budget.PerCaller, "mail_relay": c.Mailer != nil, "min_servings": c.MinServings})
	})
	mux.HandleFunc("POST /api/rescue/{id}", c.create)
	mux.HandleFunc("GET /api/rescue/{id}", c.get)
	mux.HandleFunc("POST /api/rescue/{id}/send", c.send)
	return mux
}

type rescueReq struct {
	Zip string   `json:"zip"`
	Lat *float64 `json:"lat"`
	Lng *float64 `json:"lng"`
}

func (c *RescueConfig) record(ev Event) {
	ev.Service, ev.Build = "rescue", c.Build
	ev.SpentTodayUSD = c.Budget.Spent()
	c.Events.Record(ev)
}

func (c *RescueConfig) create(w http.ResponseWriter, r *http.Request) {
	t0 := c.now()
	id := analyze.NewRequestID()
	w.Header().Set("X-Request-Id", id)
	lid := r.PathValue("id")
	ev := Event{RequestID: id, TS: t0.UTC().Format(time.RFC3339Nano), Route: "rescue.create", ListingID: lid}
	ev.From(r)
	var calls []Call
	fail := func(status int, code, msg string) {
		ev.Status, ev.Code, ev.Error, ev.LatencyMS = status, code, msg, time.Since(t0).Milliseconds()
		ev.Calls, ev.CostUSD = calls, Sum(calls)
		WriteJSON(w, status, ErrorAnswer{RequestID: id, Error: msg, Code: code})
		c.record(ev)
	}
	who, ok := Identify(r, c.Session, nil)
	if !ok {
		fail(http.StatusUnauthorized, CodeUnauthorized, "not logged in")
		return
	}
	ev.UserID, ev.CapKey = who.User.ID, who.RateKey
	if c.Limiter != nil && !c.Limiter.Allow(who.RateKey) {
		fail(http.StatusTooManyRequests, CodeRateLimited, "rate limited")
		return
	}
	var in rescueReq
	// R3: the body is optional (the pickup address can give the ZIP); an empty body is {}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&in); err != nil && !errors.Is(err, io.EOF) {
		fail(http.StatusBadRequest, CodeBadRequest, "send JSON {\"zip\"} or {\"lat\",\"lng\"}")
		return
	}
	in.Zip = strings.TrimSpace(in.Zip)
	hasLatLng := in.Lat != nil && in.Lng != nil
	switch {
	case in.Zip != "" && !zipRe.MatchString(in.Zip):
		fail(http.StatusBadRequest, CodeBadRequest, "zip must be a US ZIP code")
		return
	case in.Zip == "" && (in.Lat != nil || in.Lng != nil) &&
		(!hasLatLng || *in.Lat < -90 || *in.Lat > 90 || *in.Lng < -180 || *in.Lng > 180):
		fail(http.StatusBadRequest, CodeBadRequest, "send zip, or lat and lng")
		return
	}
	l, err := c.Listings.Mine(r, lid)
	switch {
	case errors.Is(err, ErrNotOwned):
		fail(http.StatusNotFound, CodeNotFound, err.Error())
		return
	case err != nil:
		fail(http.StatusBadGateway, CodeUpstream, "listings unavailable")
		return
	case l.Status != "active":
		fail(http.StatusConflict, "collected", "the listing was collected")
		return
	case !l.Expired(c.now()):
		fail(http.StatusConflict, "not_expired", "the listing timer has not run out")
		return
	}
	// X50b (T9 Nov 1: "after user selects rescue and backs out the results should be cached so that they dont have
	// to run again"): the stored rescue of this listing answers at once, before the place checks. Before, a listing
	// with no pickup place got 400 (no place) on each open, so the giver had to give the location again.
	if prev, ok := c.Store.Existing(lid, who.User.ID); ok && (prev.Status != StatusRunning || r.URL.Query().Get("async") == "1") {
		ev.Status, ev.LatencyMS = 200, time.Since(t0).Milliseconds()
		ev.Detail = map[string]any{"rescue_id": prev.RescueID, "existing": true}
		WriteJSON(w, http.StatusOK, prev)
		c.record(ev)
		return
	}
	// R3: where to search. The pickup address's ZIP first (where the food is),
	// then the body's zip, then its lat/lng.
	addr, pzip := pickupAddress(l.PickupText)
	lat, lng := in.Lat, in.Lng
	var apoint bool // X49j: the chosen address has no ZIP: its point is where the food is
	if a := l.PickupAddress; a != nil {
		// X49j: the address the giver chose: its text, and its ZIP or else its point (the pickup text is only
		// the instructions now: never a ZIP or a directions origin from it)
		addr, pzip = oneLine(a.Text, MaxPickupAddress), ""
		if a.Zip != "00000" && zipRe.MatchString(a.Zip) {
			pzip = a.Zip
		} else if a.Lat >= -90 && a.Lat <= 90 && a.Lng >= -180 && a.Lng <= 180 && (a.Lat != 0 || a.Lng != 0) {
			alat, alng := a.Lat, a.Lng
			lat, lng, apoint = &alat, &alng, true
		}
	}
	zip, from := in.Zip, "request"
	switch {
	case pzip != "":
		zip, from = pzip, "pickup"
	case apoint:
		zip, from = "", "pickup"
	case in.Zip == "" && !hasLatLng:
		fail(http.StatusBadRequest, CodeBadRequest, "the pickup address has no ZIP: send zip, or lat and lng")
		return
	}
	// one rescue per listing: a second request gets the first one (no new call)
	prev, found, busy := c.Store.Begin(lid, who.User.ID)
	switch {
	case found && prev.Status == StatusRunning && r.URL.Query().Get("async") != "1":
		// R4 review: the one-request route does not answer a job that still runs
		fail(http.StatusConflict, CodeConflict, "a rescue for this listing is being made")
		return
	case found:
		ev.Status, ev.LatencyMS = 200, time.Since(t0).Milliseconds()
		ev.Detail = map[string]any{"rescue_id": prev.RescueID, "existing": true}
		WriteJSON(w, http.StatusOK, prev)
		c.record(ev)
		return
	case busy:
		fail(http.StatusConflict, CodeConflict, "a rescue for this listing is being made")
		return
	}
	handoff := false // R4: a job ends the mark itself, when it is done
	defer func() {
		if !handoff {
			c.Store.End(lid)
		}
	}()
	hold, why := c.Budget.ReserveFor(who.RateKey) // X44a: the service cap and this caller's cap
	if hold == nil {
		fail(CapRefusal(why))
		return
	}
	// The photo is read now, with the caller's cookie (R4: a job runs after the answer).
	photo, mime, perr := c.Listings.Photo(r, lid)
	var images []Image
	if perr == nil {
		images = append(images, Image{Bytes: photo, Mime: mime})
	}
	source := scanSource(c.AnalyzeEvents, l) // R1: the scan's route, from its own events line
	mode := ModeDropoff                      // R3: the bulk rule
	if isBulk(l, source, c.MinServings) {
		mode = ModeEmail
	}
	prompt, hint := dropoffPrompt(l, source)
	if mode == ModeEmail {
		prompt, hint = rescuePrompt(l, source)
	}
	base := map[string]any{"hint": hint, "scan_source": source, "mode": mode, "location_from": from, "photo": perr == nil} // never the pickup text
	ev.Detail = base
	j := rescueJob{id: id, lid: lid, owner: who.User.ID, l: l, zip: zip, lat: lat, lng: lng, addr: addr,
		mode: mode, prompt: prompt, images: images, base: base}
	if r.URL.Query().Get("async") != "1" { // the X35b route: one request, no web search (the 30 s gateway)
		ctx, cancel := context.WithTimeout(r.Context(), c.Timeout)
		defer cancel()
		res, calls_, status, code, msg := c.runRescue(ctx, j, nil)
		calls = calls_
		hold.Settle(Sum(calls))
		if status != http.StatusOK {
			fail(status, code, msg)
			return
		}
		res.Status, res.FoundBy = "", "" // the X35b answer shape (R4 review)
		if err := c.Store.Put(res); err != nil {
			fail(http.StatusInternalServerError, CodeInternal, "could not store the rescue")
			return
		}
		ev.Status, ev.LatencyMS, ev.Calls, ev.CostUSD = 200, time.Since(t0).Milliseconds(), calls, Sum(calls)
		WriteJSON(w, http.StatusOK, res)
		c.record(ev)
		return
	}
	// R4: the job. The answer is 202 with the rescue (status running); the app
	// polls GET /api/rescue/{rescue_id} until the status is done or failed.
	run := Rescue{RescueID: newHex(16), RequestID: id, ListingID: lid, OwnerID: who.User.ID, Title: l.Title,
		Servings: l.NumberOfServing, Mode: mode, Places: []Place{}, Sent: []Sent{}, Status: StatusRunning,
		CreatedAt: c.now().UTC().Format(time.RFC3339Nano)}
	if err := c.Store.Put(run); err != nil {
		hold.Settle(0)
		fail(http.StatusInternalServerError, CodeInternal, "could not store the rescue")
		return
	}
	handoff = true
	WriteJSON(w, http.StatusAccepted, run)
	go func() {
		defer c.Store.End(lid)
		ctx, cancel := context.WithTimeout(context.Background(), c.JobTimeout)
		defer cancel()
		res, calls, status, code, msg := c.runRescue(ctx, j, c.Search)
		hold.Settle(Sum(calls))
		res.RescueID, res.CreatedAt, res.Status = run.RescueID, run.CreatedAt, StatusDone
		if status != http.StatusOK {
			res = run
			res.Status, res.Code, res.Error, res.CostUSD = StatusFailed, code, msg, Sum(calls)
		}
		bad := run // R4 review: never stays "running"; a new POST may run again
		bad.Status, bad.Code, bad.Error, bad.CostUSD = StatusFailed, CodeInternal, "could not store the rescue", Sum(calls)
		if err := c.Store.Done(res, bad); err != nil { // N1: the end and the free listing in one step
			status, code, msg = http.StatusInternalServerError, CodeInternal, "could not store the rescue"
		}
		ev.Status, ev.Code, ev.Error = status, "", ""
		if status != http.StatusOK {
			ev.Code, ev.Error = code, msg
		}
		ev.LatencyMS, ev.Calls, ev.CostUSD = time.Since(t0).Milliseconds(), calls, Sum(calls)
		ev.Detail = j.detail(res)
		ev.Detail["async"] = true
		c.record(ev)
		c.Push.Notify(PushMessage{UserID: j.owner, Kind: "rescue", ID: res.RescueID, Status: res.Status}) // WS1
	}()
}

// rescueJob (R4) is what a rescue needs once the request checks passed.
type rescueJob struct {
	id, lid, owner string
	l              Listing
	zip            string
	lat, lng       *float64
	addr           string // the address part of the pickup text (directions only; never logged)
	mode, prompt   string
	images         []Image
	base           map[string]any // the events detail so far
	dir            dirOut
}

// dirOut is the directory's answer, and the web search's when it ran.
type dirOut struct {
	places    []Place
	ms        int64
	err       error
	searched  bool
	searchErr error
	call      *Call
	foundBy   string
	dirErr    error // X50d review: the directory's failure when the store answered (for the events line only)
}

// detail is the events detail of a finished job.
func (j rescueJob) detail(res Rescue) map[string]any {
	d := map[string]any{}
	for k, v := range j.base {
		d[k] = v
	}
	d["rescue_id"], d["kind"], d["shelf_stable"], d["places"] = res.RescueID, res.Kind, res.ShelfStable, len(res.Places)
	d["directory_results"], d["directory_ms"], d["found_by"] = len(j.dir.places), j.dir.ms, res.FoundBy
	if j.dir.err == nil && j.dir.dirErr != nil {
		d["directory_error"] = clip(j.dir.dirErr.Error(), 300) // X50d review: the store answered
	}
	if j.dir.err != nil {
		d["directory_error"] = clip(j.dir.err.Error(), 300)
	}
	if j.dir.searched {
		d["web_search"] = true
		if j.dir.searchErr != nil {
			d["web_search_error"] = clip(j.dir.searchErr.Error(), 300)
		}
	}
	return d
}

// keepPlaces adds places to the store; a failed write goes to the service log (X50d review).
func (c *RescueConfig) keepPlaces(area string, p []Place, foundBy string) {
	if err := c.PlaceStore.Add(area, p, foundBy, c.now()); err != nil {
		log.Printf("rescue: place store: %v", err)
	}
}

// runRescue makes the rescue: the directory (and, with search, the web search
// when the directory finds nothing or fails) at the same time as the Muse
// decision. status is 200, or the refusal with its code and words.
func (c *RescueConfig) runRescue(ctx context.Context, j rescueJob, search *PlaceSearch) (Rescue, []Call, int, string, string) {
	var calls []Call
	dirc := make(chan dirOut, 1)
	go func() { // the directory and Muse do not depend on each other
		p, ms, err := c.Directory.Near(ctx, j.zip, j.lat, j.lng)
		out := dirOut{places: p, ms: ms, err: err, foundBy: "directory"}
		area := AreaKey(j.zip, j.lat, j.lng)
		if err == nil && len(p) > 0 {
			c.keepPlaces(area, p, "directory") // X50d: the store grows with each search
		}
		if (err != nil || len(p) == 0) && ctx.Err() == nil { // X50d: the store before the paid web search
			if sp := c.PlaceStore.Get(area, c.now(), PlaceMaxAge); len(sp) > 0 {
				out.places, out.err, out.dirErr, out.foundBy = sp, nil, err, FoundByStore
				dirc <- out
				return
			}
		}
		if search != nil && (err != nil || len(p) == 0) && ctx.Err() == nil { // R4
			why := "the food directory found no places near this location."
			if err != nil {
				why = "the food directory did not answer (" + clip(err.Error(), 120) + ")."
			}
			sp, call, serr := search.Find(ctx, j.zip, j.lat, j.lng, why)
			out.searched, out.searchErr, out.call = true, serr, &call
			if serr == nil && len(sp) > 0 {
				out.places, out.foundBy = sp, SourceWebSearch
				c.keepPlaces(area, sp, SourceWebSearch) // X50d
			}
		}
		if len(out.places) == 0 {
			out.foundBy = "none"
		}
		dirc <- out
	}()
	obj, call, merr := c.Muse.AskJSON(ctx, "rescue:draft", j.prompt, j.images...)
	calls = append(calls, call)
	d := <-dirc
	if d.call != nil {
		calls = append(calls, *d.call)
	}
	j.dir = d
	res := Rescue{}
	stable, isBool := obj["shelf_stable"].(bool)
	draft := Draft{Subject: clip(Str(obj, "subject"), 120), Body: clip(Str(obj, "body"), 2000)}
	fail := func(status int, code, msg string) (Rescue, []Call, int, string, string) {
		for k, v := range j.detail(res) {
			j.base[k] = v // the failure's events line keeps what is known
		}
		return res, calls, status, code, msg
	}
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return fail(http.StatusGatewayTimeout, CodeTimeout, "took too long")
	case merr != nil || !isBool || (j.mode == ModeEmail && (draft.Subject == "" || draft.Body == "")):
		return fail(http.StatusBadGateway, CodeUpstream, "the draft failed")
	case d.err != nil && !(d.searched && d.searchErr == nil):
		// X44l: the reason goes into the events line (the answer stays short)
		if d.searched {
			return fail(http.StatusBadGateway, CodeUpstream, "the directory and the web search are unavailable")
		}
		return fail(http.StatusBadGateway, CodeUpstream, "the directory is unavailable")
	}
	kind := KindMealProgram
	if stable {
		kind = KindFoodBank
	}
	places := PickPlaces(d.places, kind, c.Places)
	if j.mode == ModeDropoff {
		draft = Draft{} // R3: no email for a drop-off
	}
	for i := range places {
		places[i].Tel = telLink(places[i].Phone)
		places[i].Directions = directionsLink(j.addr, places[i]) // R3
		if j.mode == ModeDropoff {
			continue
		}
		if places[i].Email != "" {
			places[i].Mailto = mailtoLink(places[i].Email, draft)
		} else {
			places[i].Mailto = mailtoLink("", draft)
		}
	}
	if places == nil {
		places = []Place{}
	}
	attr := DirectoryAttribution
	if d.foundBy == SourceWebSearch {
		attr = WebAttribution
	}
	if d.foundBy == FoundByStore { // X50d: the credit of the places as they were found (both when mixed)
		web, dir := false, false
		for _, p := range places {
			if p.Source == SourceWebSearch {
				web = true
			} else {
				dir = true
			}
		}
		switch {
		case web && dir:
			attr = DirectoryAttribution + "; " + WebAttribution
		case web:
			attr = WebAttribution
		}
	}
	res = Rescue{RescueID: newHex(16), RequestID: j.id, ListingID: j.lid, OwnerID: j.owner, Title: j.l.Title,
		Servings: j.l.NumberOfServing, Kind: kind, Mode: j.mode, ShelfStable: stable, Reason: clip(Str(obj, "reason"), 160),
		Draft: draft, Places: places, Attribution: attr, FoundBy: d.foundBy,
		CreatedAt: c.now().UTC().Format(time.RFC3339Nano), CostUSD: Sum(calls), Sent: []Sent{}}
	for k, v := range j.detail(res) {
		j.base[k] = v
	}
	return res, calls, http.StatusOK, "", ""
}

// isBulk (R3) is the bulk rule: min or more servings, or the scanner said
// "bulk" on a vision scan (a barcode answer's serving_type is a fixed value,
// barcode/product.go, as its meal_condition).
func isBulk(l Listing, source string, min int) bool {
	return l.NumberOfServing >= min || (source == ScanVision && l.Details != nil && l.Details.ServingType == "bulk")
}

// usStates are the USPS two-letter codes (Publication 28, Appendix B):
// states, DC, territories and the military codes.
var usStates = map[string]bool{}

func init() {
	for _, s := range strings.Fields("AL AK AS AZ AR CA CO CT DE DC FM FL GA GU HI ID IL IN IA KS KY LA ME MH MD MA MI MN MS MO MT " +
		"NE NV NH NJ NM NY NC ND MP OH OK OR PW PA PR RI SC SD TN TX UT VT VI VA WA WV WI WY AE AP AA") {
		usStates[s] = true
	}
}

// stateZipRe is a state code, then a ZIP (5 digits, optional +4), as the last
// line of a US address ("Denver, CO 80219"): the code is a word of its own and
// the ZIP is not part of a longer number. R3 review: a lone 5-digit number (a
// house, suite or gate code number) is never a ZIP.
var stateZipRe = regexp.MustCompile(`(?:^|[^A-Za-z0-9])([A-Za-z]{2})[ ,.]+(\d{5})(-\d{4})?(?:$|[^0-9-])`)

// MaxPickupAddress is the most characters (runes) of each address in a
// directions link, before encoding.
const MaxPickupAddress = 150

// MaxLinkPart is the most characters of each of origin and destination once
// percent-encoded (R3 review: a rune can be 12): with the rest of the URL the
// link stays under the Maps limit of 2,048.
const MaxLinkPart = 900

// pickupAddress (R3) splits the pickup text: zip is the last ZIP that comes
// right after a USPS state code (uppercase, or any case after a comma:
// "Denver, co 80219"; so "in" or "or" in a sentence is not a state), and the
// address is the text up to and with it (the instructions come after it). No
// such ZIP, or 00000: "", "".
func pickupAddress(text string) (addr, zip string) {
	text = oneLine(text, 1000)
	ms := stateZipRe.FindAllStringSubmatchIndex(text, -1)
	for k := len(ms) - 1; k >= 0; k-- {
		m := ms[k]
		st, z := text[m[2]:m[3]], text[m[4]:m[5]]
		if !usStates[strings.ToUpper(st)] || z == "00000" {
			continue
		}
		if st != strings.ToUpper(st) && !strings.HasSuffix(strings.TrimRight(text[:m[2]], " "), ",") {
			continue
		}
		end := m[5]
		if m[6] >= 0 {
			end = m[7] // the +4 part stays with the address
		}
		return oneLine(strings.TrimRight(text[:end], " ,;"), MaxPickupAddress), z
	}
	return "", ""
}

// clipEncoded is s without its last runes until it is at most n characters
// once query-encoded.
func clipEncoded(s string, n int) string {
	r := []rune(s)
	for len(r) > 0 && len(url.QueryEscape(string(r))) > n {
		r = r[:len(r)-1]
	}
	return strings.TrimSpace(string(r))
}

// directionsLink (R3) is a Google Maps directions URL (no API key) from the
// pickup address to the place. With no pickup address the origin is left out:
// Maps then starts at the device's location.
func directionsLink(from string, p Place) string {
	to := strings.TrimSpace(p.Address)
	if to == "" {
		to = strings.TrimSpace(p.Name)
	} else if p.Name != "" {
		to = p.Name + ", " + to
	}
	if to == "" {
		return ""
	}
	q := url.Values{"api": {"1"}, "destination": {clipEncoded(oneLine(to, MaxPickupAddress), MaxLinkPart)}}
	if from = clipEncoded(oneLine(from, MaxPickupAddress), MaxLinkPart); from != "" {
		q.Set("origin", from)
	}
	return "https://www.google.com/maps/dir/?" + q.Encode()
}

func telLink(phone string) string {
	digits := regexp.MustCompile(`[^\d+]`).ReplaceAllString(phone, "")
	if len(strings.TrimPrefix(digits, "+")) < 7 {
		return ""
	}
	return "tel:" + digits
}

// mailtoLink is RFC 6068: the address, then subject and body as query
// fields, percent-encoded (spaces as %20, not +). The "@" of the address
// stays literal (RFC 6068 section 2: addr-spec; some mail apps do not decode
// %40 in the address part).
func mailtoLink(to string, d Draft) string {
	enc := func(s string) string { return strings.ReplaceAll(url.QueryEscape(s), "+", "%20") }
	return "mailto:" + strings.ReplaceAll(enc(to), "%40", "@") + "?subject=" + enc(d.Subject) + "&body=" + enc(d.Body)
}

func (c *RescueConfig) get(w http.ResponseWriter, r *http.Request) {
	who, ok := Identify(r, c.Session, nil)
	if !ok {
		WriteJSON(w, http.StatusUnauthorized, ErrorAnswer{RequestID: analyze.NewRequestID(), Error: "not logged in", Code: CodeUnauthorized})
		return
	}
	res, ok := c.Store.Get(r.PathValue("id"), who.User.ID)
	if !ok {
		WriteJSON(w, http.StatusNotFound, ErrorAnswer{RequestID: analyze.NewRequestID(), Error: "rescue not found", Code: CodeNotFound})
		return
	}
	WriteJSON(w, http.StatusOK, res)
}

type sendReq struct {
	PlaceID string `json:"place_id"`
	ToEmail string `json:"to_email"`
	Subject string `json:"subject"` // optional: the donor's own edit of the draft
	Body    string `json:"body"`
}

func (c *RescueConfig) send(w http.ResponseWriter, r *http.Request) {
	t0 := c.now()
	id := analyze.NewRequestID()
	w.Header().Set("X-Request-Id", id)
	ev := Event{RequestID: id, TS: t0.UTC().Format(time.RFC3339Nano), Route: "rescue.send"}
	ev.From(r)
	fail := func(status int, code, msg string) {
		ev.Status, ev.Code, ev.Error, ev.LatencyMS = status, code, msg, time.Since(t0).Milliseconds()
		WriteJSON(w, status, ErrorAnswer{RequestID: id, Error: msg, Code: code})
		c.record(ev)
	}
	who, ok := Identify(r, c.Session, nil)
	if !ok {
		fail(http.StatusUnauthorized, CodeUnauthorized, "not logged in")
		return
	}
	ev.UserID, ev.CapKey = who.User.ID, who.RateKey
	if c.Mailer == nil {
		fail(http.StatusServiceUnavailable, CodeDisabled, "no mail relay: use the mailto link")
		return
	}
	lim := c.SendLimiter
	if lim == nil {
		lim = c.Limiter
	}
	if !lim.Allow("send:" + who.RateKey) {
		fail(http.StatusTooManyRequests, CodeRateLimited, "rate limited")
		return
	}
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	res, ok := c.Store.Get(r.PathValue("id"), who.User.ID)
	if !ok {
		fail(http.StatusNotFound, CodeNotFound, "rescue not found")
		return
	}
	ev.ListingID = res.ListingID
	if res.Mode == ModeDropoff { // R3: no email for a drop-off
		fail(http.StatusConflict, "dropoff", "this rescue is a drop-off: use the directions link")
		return
	}
	var in sendReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&in); err != nil {
		fail(http.StatusBadRequest, CodeBadRequest, "bad JSON body")
		return
	}
	var place *Place
	for i := range res.Places {
		if res.Places[i].ID == in.PlaceID {
			place = &res.Places[i]
		}
	}
	if place == nil {
		fail(http.StatusBadRequest, CodeBadRequest, "place_id is not one of this rescue's places")
		return
	}
	// X36a (T9 2026-10-28): the relay sends only to the address the directory
	// gives for this place. With no directory address there is no relay
	// email; the donor uses the mailto or tel link (their own mail app), so
	// the Icarus sender cannot carry the donor's text to any address.
	if place.Email == "" {
		fail(http.StatusConflict, CodeConflict, "this place has no email in the directory: use the mailto or tel link")
		return
	}
	to := strings.TrimSpace(in.ToEmail)
	if to != "" && !strings.EqualFold(to, place.Email) {
		fail(http.StatusBadRequest, CodeBadRequest, "the relay sends only to this place's directory address")
		return
	}
	to = place.Email
	if a, err := mail.ParseAddress(to); err != nil || a.Address != to {
		fail(http.StatusBadRequest, CodeBadRequest, "to_email is not one email address")
		return
	}
	for _, s := range res.Sent {
		if s.PlaceID == place.ID {
			fail(http.StatusConflict, CodeConflict, "already sent to this place")
			return
		}
	}
	if c.Store.SentToday(who.User.ID, c.now()) >= c.SendPerDay {
		fail(http.StatusTooManyRequests, CodeRateLimited, fmt.Sprintf("%d emails per day", c.SendPerDay))
		return
	}
	subject, body := res.Draft.Subject, res.Draft.Body
	if s := strings.TrimSpace(in.Subject); s != "" {
		subject = clip(s, 120)
	}
	if b := strings.TrimSpace(in.Body); b != "" {
		body = clip(b, 4000)
	}
	if err := c.Mailer.Send(to, who.User.Email, subject, body); err != nil {
		fail(http.StatusBadGateway, CodeUpstream, "the mail relay refused the email")
		return
	}
	res.Sent = append(res.Sent, Sent{PlaceID: place.ID, To: to, At: c.now().UTC().Format(time.RFC3339Nano)})
	if err := c.Store.Put(res); err != nil {
		fail(http.StatusInternalServerError, CodeInternal, "sent, but could not record it")
		return
	}
	ev.Status, ev.LatencyMS = 200, time.Since(t0).Milliseconds()
	ev.Detail = map[string]any{"rescue_id": res.RescueID, "place_id": place.ID}
	WriteJSON(w, http.StatusOK, map[string]any{"request_id": id, "sent": true, "rescue_id": res.RescueID, "place_id": place.ID})
	c.record(ev)
}
