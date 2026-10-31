package auth

// N5 (T9: "we need to wire in the email link to work"): password reset by an
// email link, the contract of the reset brief (2026-10-29) that the app (U9)
// already calls.
//
//	POST /api/auth/reset          {email}            -> 202 {} (also for an unknown email) | 400 | 429
//	POST /api/auth/reset/confirm  {token, password}  -> 200 user + Set-Cookie | 400 "reset link expired" | 400 "password too short"
//
// The token is 32 random bytes (unpadded base64url, 43 characters); the data
// file keeps only its SHA-256, with the user id and an expiry of 30 minutes. A
// new request replaces the earlier token of that user. The link is
// <ResetLinkBase>/reset#token=<token>: after "#", so the browser never sends
// it to a server, a log or a Referer header. The answer, its time and the
// limits are the same for an email with an account and one without, so the
// route does not tell which accounts exist (the work for an account is done
// after the answer). Logs have the user id only, never an email, a token or a
// password. https://cheatsheetseries.owasp.org/cheatsheets/Forgot_Password_Cheat_Sheet.html

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ResetTTL is how long a reset link works.
const ResetTTL = 30 * time.Minute

// The reset errors; the text is what the app reads (mobile/src/signin.ts EXPIRED_ERROR).
var (
	ErrResetExpired = errors.New("reset link expired")
	ErrResetShort   = errors.New("password too short")
)

// resetRec is one reset token in the data file (by the SHA-256 of the token).
type resetRec struct {
	TokenHash string `json:"token_hash"`
	UserID    string `json:"user_id"`
	CreatedAt string `json:"created_at"`
	ExpiresAt string `json:"expires_at"`
}

// Mailer sends one email. OutboxMailer writes it to a folder (no sender yet);
// a real sender (Amazon SES) comes when it is set up.
type Mailer interface {
	Send(to, subject, body string) error
}

// OutboxMailer (T9 2026-10-31: no sender is set up yet) writes each email as a
// file, mode 0600, in Dir on the server: an operator can open a test link from
// there. It is a mail queue, not a log. N5 review: a file holds a live link, so
// each Send first removes the files older than ResetTTL (their links are dead).
type OutboxMailer struct{ Dir string }

// Send removes the old files, then writes <Dir>/<UTC time>-<random>.eml.
func (m OutboxMailer) Send(to, subject, body string) error {
	if err := os.MkdirAll(m.Dir, 0o700); err != nil {
		return err
	}
	if old, err := filepath.Glob(filepath.Join(m.Dir, "*.eml")); err == nil {
		for _, f := range old {
			if st, err := os.Stat(f); err == nil && time.Since(st.ModTime()) > ResetTTL {
				os.Remove(f)
			}
		}
	}
	var b [6]byte
	_, _ = rand.Read(b[:])
	name := time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + fmt.Sprintf("%x", b) + ".eml"
	msg := "To: " + to + "\r\nSubject: " + subject + "\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" + body + "\r\n"
	return os.WriteFile(filepath.Join(m.Dir, name), []byte(msg), 0o600)
}

// ResetSubject and ResetBody are the words of the email (the brief, ASD-STE100).
const ResetSubject = "Reset your V3RDE password"

// ResetBody is the text with the link.
func ResetBody(link string) string {
	return "Open this link to set a new password:\r\n" + link + "\r\n\r\n" +
		"The link works for 30 minutes, one time. If you did not ask for this, do nothing: your password stays the same."
}

// NewReset makes a reset token for the account of email, replacing the
// earlier token of that user. It returns "" and no error when no account has
// the email (the caller answers the same way).
func (s *Store) NewReset(email string) (token, userID, to string, err error) {
	email = NormalizeEmail(email)
	var b [32]byte
	_, _ = rand.Read(b[:])
	token = base64.RawURLEncoding.EncodeToString(b[:])
	now := s.now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.data.Emails[email]
	old := map[string]resetRec{}
	for k, r := range s.data.Resets {
		if (ok && r.UserID == id) || !resetLive(r, now) { // the user's earlier token, and every expired one
			old[k] = r
			delete(s.data.Resets, k)
		}
	}
	if !ok { // N5 review: the same file write as for an account, so the work takes about the same time
		if err := s.saveLocked(); err != nil {
			for k, r := range old {
				s.data.Resets[k] = r
			}
			return "", "", "", err
		}
		return "", "", "", nil
	}
	key := hashToken(token)
	s.data.Resets[key] = resetRec{TokenHash: key, UserID: id, CreatedAt: now.Format(time.RFC3339),
		ExpiresAt: now.Add(ResetTTL).Format(time.RFC3339)}
	if err := s.saveLocked(); err != nil {
		delete(s.data.Resets, key)
		for k, r := range old {
			s.data.Resets[k] = r
		}
		return "", "", "", err
	}
	return token, id, s.data.Users[id].Email, nil
}

func resetLive(r resetRec, now time.Time) bool {
	t, err := time.Parse(time.RFC3339, r.ExpiresAt)
	return err == nil && now.Before(t)
}

// Reset sets the new password of the token's user, removes the token (one
// time) and every session of that user. An unknown, used or expired token is
// ErrResetExpired (one message for all three).
func (s *Store) Reset(token, password string) (User, error) {
	if len(password) < MinPassword {
		return User{}, ErrResetShort
	}
	if token == "" || len(token) > 128 {
		return User{}, ErrResetExpired
	}
	key := hashToken(token)
	s.mu.Lock()
	r, ok := s.data.Resets[key]
	s.mu.Unlock()
	if !ok || !resetLive(r, s.now().UTC()) {
		return User{}, ErrResetExpired
	}
	hash, err := hashPassword(password, pbkdfIter) // outside the lock: it takes the PBKDF2 time
	if err != nil {
		return User{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok = s.data.Resets[key] // again: another confirm may have used it while the hash ran
	u, uok := s.data.Users[r.UserID]
	if !ok || !uok || !resetLive(r, s.now().UTC()) {
		return User{}, ErrResetExpired
	}
	data := s.data
	keepUsers, keepResets, keepSessions := copyMap(data.Users), copyMap(data.Resets), copyMap(data.Sessions)
	u.PwHash = hash
	s.data.Users[u.ID] = u
	delete(s.data.Resets, key)
	for k, sess := range s.data.Sessions {
		if sess.UserID == u.ID {
			delete(s.data.Sessions, k)
		}
	}
	if err := s.saveLocked(); err != nil {
		s.data.Users, s.data.Resets, s.data.Sessions = keepUsers, keepResets, keepSessions
		return User{}, err
	}
	return u.public(), nil
}

func copyMap[K comparable, V any](m map[K]V) map[K]V {
	out := make(map[K]V, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// ---- the routes ----

// resetAsk is POST /api/auth/reset.
func (c Config) resetAsk(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email string `json:"email"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	email := NormalizeEmail(in.Email)
	if !emailForm.MatchString(email) {
		fail(w, http.StatusBadRequest, ErrBadEmail.Error())
		return
	}
	// per email too, so one address cannot be flooded with mails (the key is a hash: no email in memory dumps of the limiter)
	if c.ResetLimiter != nil && !c.ResetLimiter.Allow("e:"+hashToken(email)) {
		fail(w, http.StatusTooManyRequests, "rate limited")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{})
	if c.Mailer == nil || c.ResetLinkBase == "" {
		log.Printf("reset: no mailer or link base; nothing sent")
		return
	}
	go func() { // after the answer: the same time for an email with or without an account
		if c.resetDone != nil {
			defer c.resetDone() // N5 review: also after a mail error
		}
		tok, uid, to, err := c.Store.NewReset(email)
		switch {
		case err != nil:
			log.Printf("reset: store: %v", err)
		case tok == "":
			log.Printf("reset_requested no account")
		default:
			link := strings.TrimRight(c.ResetLinkBase, "/") + "/reset#token=" + tok
			if err := c.Mailer.Send(to, ResetSubject, ResetBody(link)); err != nil {
				log.Printf("reset_requested user=%s mail error: %v", uid, err)
				return
			}
			log.Printf("reset_requested user=%s mail sent", uid)
		}
	}()
}

// resetConfirm is POST /api/auth/reset/confirm: the new password, then a new session (as login).
func (c Config) resetConfirm(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	u, err := c.Store.Reset(strings.TrimSpace(in.Token), in.Password)
	switch {
	case errors.Is(err, ErrResetExpired), errors.Is(err, ErrResetShort):
		fail(w, http.StatusBadRequest, err.Error())
	case err != nil:
		log.Printf("reset confirm: %v", err)
		fail(w, http.StatusInternalServerError, "could not set the password")
	default:
		c.startSession(w, u, http.StatusOK, "reset_done")
	}
}

// readJSON reads a JSON object body of at most 16 KiB into v.
func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		fail(w, http.StatusUnsupportedMediaType, "send application/json")
		return false
	}
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		fail(w, http.StatusBadRequest, "bad JSON body")
		return false
	}
	return true
}
