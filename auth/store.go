package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// The data file. Its format is the one the data volume already holds.
type userRec struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	PwHash    string `json:"pw_hash"` // pbkdf2$sha256$<iter>$<salt>$<key> (password.go)
	CreatedAt string `json:"created_at"`
}

type sessionRec struct {
	IDHash    string `json:"id_hash"` // sha256 hex of the cookie value
	UserID    string `json:"user_id"`
	CreatedAt string `json:"created_at"`
	ExpiresAt string `json:"expires_at"`
}

type fileData struct {
	Users    map[string]userRec    `json:"users"`    // by id
	Emails   map[string]string     `json:"emails"`   // normalized email -> id
	Sessions map[string]sessionRec `json:"sessions"` // by id_hash
}

// Store keeps the users and the sessions in memory and in one JSON file,
// written again as a whole after each change (a temp file, a sync, a
// rename), under one lock. For tens of users and hundreds of sessions this
// needs no database. Only one process may use the file at a time (the lock
// is in memory; compose stops the old container before it starts the new one).
type Store struct {
	path string
	mu   sync.Mutex
	data fileData
	now  func() time.Time
}

// Open loads path, or makes it (and its folder) empty.
func Open(path string) (*Store, error) {
	s := &Store{path: path, now: time.Now}
	b, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		s.fill()
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return nil, err
		}
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
		return s, nil
	case err != nil:
		return nil, err
	}
	if len(b) > 0 {
		if err := json.Unmarshal(b, &s.data); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	s.fill()
	return s, nil
}

// fill makes the maps that the file did not have.
func (s *Store) fill() {
	if s.data.Users == nil {
		s.data.Users = map[string]userRec{}
	}
	if s.data.Emails == nil {
		s.data.Emails = map[string]string{}
	}
	if s.data.Sessions == nil {
		s.data.Sessions = map[string]sessionRec{}
	}
}

// Close does nothing: every change is on disk when its call returns.
func (s *Store) Close() error { return nil }

// saveLocked writes the file through path+".tmp"; the caller holds mu. A
// failure removes the temp file.
func (s *Store) saveLocked() error {
	b, err := json.MarshalIndent(s.data, "", " ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, s.path)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}

func newID(prefix string) string {
	var b [8]byte
	_, _ = rand.Read(b[:]) // never fails since Go 1.24
	return prefix + hex.EncodeToString(b[:])
}

// hashToken is the key of a session: the SHA-256 of the cookie value.
func hashToken(tok string) string {
	h := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(h[:])
}

func (u userRec) public() User { return User{ID: u.ID, Email: u.Email, CreatedAt: u.CreatedAt} }

// Signup makes the user. The password is hashed here (outside the lock:
// it takes the PBKDF2 time) and nowhere else.
func (s *Store) Signup(email, password string) (User, error) {
	email = NormalizeEmail(email)
	if !emailForm.MatchString(email) {
		return User{}, ErrBadEmail
	}
	if len(password) < MinPassword {
		return User{}, ErrShortPass
	}
	hash, err := hashPassword(password, pbkdfIter)
	if err != nil {
		return User{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, taken := s.data.Emails[email]; taken {
		return User{}, ErrEmailTaken
	}
	u := userRec{ID: newID("u_"), Email: email, PwHash: hash, CreatedAt: s.now().UTC().Format(time.RFC3339)}
	s.data.Users[u.ID] = u
	s.data.Emails[email] = u.ID
	if err := s.saveLocked(); err != nil {
		delete(s.data.Users, u.ID)
		delete(s.data.Emails, email)
		return User{}, err
	}
	return u.public(), nil
}

// Login checks the password. An unknown email and a wrong password give the
// same error and take the same time (a check against missHash).
func (s *Store) Login(email, password string) (User, error) {
	email = NormalizeEmail(email)
	s.mu.Lock()
	id, ok := s.data.Emails[email]
	u := s.data.Users[id]
	s.mu.Unlock()
	if !ok {
		verifyPassword(missHash, password)
		return User{}, ErrBadLogin
	}
	if !verifyPassword(u.PwHash, password) {
		return User{}, ErrBadLogin
	}
	return u.public(), nil
}

// NewSession makes the cookie value and keeps only its hash. It also
// removes the expired sessions, so the file does not grow without end.
func (s *Store) NewSession(userID string) (token string, expires time.Time, err error) {
	var b [32]byte
	_, _ = rand.Read(b[:])
	token = base64.RawURLEncoding.EncodeToString(b[:])
	key := hashToken(token)
	now := s.now().UTC()
	expires = now.Add(SessionTTL)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Sessions[key] = sessionRec{IDHash: key, UserID: userID,
		CreatedAt: now.Format(time.RFC3339), ExpiresAt: expires.Format(time.RFC3339)}
	s.pruneLocked(now)
	if err = s.saveLocked(); err != nil {
		delete(s.data.Sessions, key) // an expired session that was removed stays removed: it was no session
		return "", time.Time{}, err
	}
	return token, expires, nil
}

// pruneLocked removes every session that has expired at now (or whose
// expiry does not parse). Deleting while ranging is safe in Go
// (https://go.dev/ref/spec#For_range).
func (s *Store) pruneLocked(now time.Time) {
	for k, sess := range s.data.Sessions {
		if t, err := time.Parse(time.RFC3339, sess.ExpiresAt); err != nil || !now.Before(t) {
			delete(s.data.Sessions, k)
		}
	}
}

// UserForSession is the user of a cookie value. An expired session is no
// session, and it is removed.
func (s *Store) UserForSession(token string) (User, error) {
	if token == "" {
		return User{}, ErrNoSession
	}
	key := hashToken(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.data.Sessions[key]
	if !ok {
		return User{}, ErrNoSession
	}
	if t, err := time.Parse(time.RFC3339, sess.ExpiresAt); err != nil || !s.now().UTC().Before(t) {
		delete(s.data.Sessions, key)
		_ = s.saveLocked() // the answer is "no session" also when the file write fails
		return User{}, ErrNoSession
	}
	u, ok := s.data.Users[sess.UserID]
	if !ok {
		return User{}, ErrNoSession
	}
	return u.public(), nil
}

// DeleteSession is the logout: an unknown session is no error. The session
// is gone from memory at once, also when the file write fails (a logout
// must end the session now).
func (s *Store) DeleteSession(token string) error {
	key := hashToken(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data.Sessions[key]; !ok {
		return nil
	}
	delete(s.data.Sessions, key)
	return s.saveLocked()
}

// sessionCount is for the tests.
func (s *Store) sessionCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.data.Sessions)
}
