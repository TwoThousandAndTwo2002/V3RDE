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
//	POST /api/auth/reset   {email}            -> 202 {} (N5, reset.go: an email link, also 202 for no account)
//	POST /api/auth/reset/confirm {token, password} -> 200 user + Set-Cookie | 400
//	GET  /api/auth/healthz                    -> 200
//
// Passwords: PBKDF2-HMAC-SHA256, 600,000 iterations, at least 8 characters,
// no other rules. Sessions: 32 random bytes in the cookie; the data file has
// only their SHA-256, so a copied file cannot be used to sign in. 7 days,
// HttpOnly, SameSite=Lax, Secure unless -cookie-secure=false (plain http).
// Signup and login are rate limited per address. Logs have user ids, never
// emails or passwords.
package auth

import (
	"errors"
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

// emailForm is a loose check: something@something.something, no spaces.
var emailForm = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

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
	ErrBadLogin   = errors.New("email or password is wrong")
	ErrNoSession  = errors.New("no session")
)
