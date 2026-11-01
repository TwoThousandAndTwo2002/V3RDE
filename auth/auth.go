// Package auth is the V3RDE sign-in service: an email and a password, a
// session cookie, nothing more. It is its own service with its own data:
// listings asks it "who has this cookie" and never sees a password hash;
// the analyze and inference services never see a user.
//
// V3RDE #26 (Oct 28 2026): this package is written new for V3RDE. The
// routes, the status codes, the error texts, the cookie, the data file
// (auth.json) and the password hash format are kept the same as the auth
// service the app used before the Hackathon, so the app, listings and the
// accounts on the data volume do not change.
//
// Routes (all under /api/auth):
//
//	POST /api/auth/signup  {email, password}  -> 201 user + Set-Cookie | 409 taken | 400
//	POST /api/auth/login   {email, password}  -> 200 user + Set-Cookie | 401
//	POST /api/auth/logout                     -> 204, the session is deleted, the cookie cleared
//	GET  /api/auth/me                         -> 200 user | 401
//	GET  /api/auth/healthz                    -> 200
//	POST /api/auth/reset   {email}            -> 202 {} (also for an unknown email) | 400 | 429 | 503 (X49o, reset.go)
//	POST /api/auth/reset/confirm {token, password} -> 200 user + Set-Cookie | 400 "reset link expired"
//
// Passwords: PBKDF2-HMAC-SHA256, 600,000 iterations, at least 8 characters,
// no other rules. Sessions: 32 random bytes in the cookie; the data file has
// only their SHA-256, so a copied file cannot be used to sign in. 7 days,
// HttpOnly, SameSite=Lax, Secure unless -cookie-secure=false (plain http).
// Signup, login and reset are rate limited per address; a reset request also per email. Logs have user ids, never
// emails or passwords.
package auth

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// The cookie and the rules.
const (
	CookieName   = "morpheus_session" // listings reads this cookie
	SessionTTL   = 7 * 24 * time.Hour
	MinPassword  = 8
	maxBodyBytes = 16 << 10
)

// emailForm (X49w, T9: "emails should be validated more strictly", any country) is the "valid e-mail address" of
// the HTML standard (https://html.spec.whatwg.org/multipage/input.html#valid-e-mail-address) plus a domain with a dot
// and an ending of 2 to 63 letters: name@domain.com, name@company.ng, name@yahoo.co.uk. The app has the same check
// (mobile/src/signin.ts EMAIL_FORM). Only sign-up checks it. Sign-in does not check the form, so an account made
// before this still signs in.
var emailForm = regexp.MustCompile("^[A-Za-z0-9.!#$%&'*+/=?^_`{|}~-]+@[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?(?:\\.[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*\\.[A-Za-z]{2,63}$")

// emailLoose (X49w review) is the check from before the strict form: something@something.something with no space
// and one @. The reset request uses it, so an account made before emailForm still gets its reset link. The app has
// the same check (mobile/src/signin.ts EMAIL_LOOSE).
var emailLoose = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

// User is what a caller sees: never the hash.
type User struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	CreatedAt string `json:"created_at"`
}

// NormalizeEmail is the one form of an email: trimmed, lower case.
func NormalizeEmail(e string) string { return strings.ToLower(strings.TrimSpace(e)) }

// The errors; the text is what the app shows.
var (
	ErrBadEmail   = errors.New("email is not valid")
	ErrShortPass  = errors.New("password must be at least 8 characters")
	ErrEmailTaken = errors.New("email already registered")
	ErrBadLogin   = errors.New("email or password is wrong") // X49w: the wrapper of the two below (errors.Is)
	// X49w (T9 2026-11-01: "if a password is wrong only then do you render forgot password"; "when an email is not
	// recognized ... render create an account"): sign-in tells the two cases apart, with a code for the app. Sign-up
	// already tells that an email is taken (ErrEmailTaken), and the per-address limit stays.
	ErrNoAccount     = fmt.Errorf("%w: no account uses this email", ErrBadLogin)
	ErrWrongPassword = fmt.Errorf("%w: wrong password", ErrBadLogin)
	ErrNoSession     = errors.New("no session")
)
