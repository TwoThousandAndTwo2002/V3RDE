package auth

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

// The password hash: PBKDF2-HMAC-SHA256 with 600,000 iterations (the OWASP
// value for PBKDF2-HMAC-SHA256), a 16-byte salt and a 32-byte key, stored as
//
//	pbkdf2$sha256$<iterations>$<salt, base64 raw>$<key, base64 raw>
//
// The iterations are in the text, so a later change of the cost can live
// next to the hashes of today.
const (
	pbkdfIter = 600_000
	saltLen   = 16
	keyLen    = 32
	minIter   = 1000           // a stored hash with fewer iterations is refused
	maxIter   = 10 * pbkdfIter // and one with more (one login must not take 30 s)
)

var b64 = base64.RawStdEncoding

// hashPassword makes the stored text for password.
func hashPassword(password string, iter int) (string, error) {
	salt := make([]byte, saltLen)
	_, _ = rand.Read(salt) // never fails since Go 1.24 (docs/sources/X38a.md D119)
	key, err := derive(password, salt, iter, keyLen)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2$sha256$%d$%s$%s", iter, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// verifyPassword reports whether password matches the stored text. A text
// in another form is never a match, and it takes the same time as a real
// check (a check against missHash), so the time does not show which
// accounts have a damaged hash. The key must be keyLen bytes and the salt
// at least saltLen: a short stored key would let wrong passwords match
// (the X38c review: a 1-byte key matched 4 of 2000 wrong passwords).
func verifyPassword(stored, password string) bool {
	salt, want, iter, ok := parseHash(stored)
	if !ok {
		if stored != missHash && missHash != "" {
			verifyPassword(missHash, password)
		}
		return false
	}
	got, err := derive(password, salt, iter, len(want))
	return err == nil && hmac.Equal(got, want)
}

// derive is PBKDF2-HMAC-SHA256.
func derive(password string, salt []byte, iter, n int) ([]byte, error) {
	return pbkdf2.Key(sha256.New, password, salt, iter, n)
}

// parseHash reads pbkdf2$sha256$<iter>$<salt>$<key>.
func parseHash(stored string) (salt, key []byte, iter int, ok bool) {
	f := strings.Split(stored, "$")
	if len(f) != 5 || f[0] != "pbkdf2" || f[1] != "sha256" {
		return nil, nil, 0, false
	}
	iter, err := strconv.Atoi(f[2])
	if err != nil || iter < minIter || iter > maxIter {
		return nil, nil, 0, false
	}
	salt, err = b64.DecodeString(f[3])
	if err != nil || len(salt) < saltLen {
		return nil, nil, 0, false
	}
	key, err = b64.DecodeString(f[4])
	if err != nil || len(key) != keyLen {
		return nil, nil, 0, false
	}
	return salt, key, iter, true
}

// missHash makes a login with an unknown email cost the same time as one
// with a wrong password, so the time does not tell which emails exist.
var missHash, _ = hashPassword("v3rde-no-such-user", pbkdfIter)
