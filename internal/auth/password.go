// Package auth handles password hashing and credential checks.
//
// Hashing choice: bcrypt (golang.org/x/crypto/bcrypt).
//
// Why bcrypt and not argon2id? For a teaching reference app the deciding factor
// is a small, hard-to-misuse surface:
//   - One tuning knob (cost). argon2id needs three (memory, time, parallelism)
//     that you must choose, store, and keep in sync — more to get wrong.
//   - The cost is embedded in the hash string, so verification needs no external
//     parameters and rehashing on cost bumps is trivial.
//   - It ships in the Go team's golang.org/x/crypto with a decade of scrutiny.
//
// The honest tradeoff: argon2id is memory-hard and is OWASP's first
// recommendation for new systems; bcrypt is GPU-friendlier and truncates input
// at 72 bytes. We accept that by capping password length (see MaxPasswordLen) so
// nothing is silently truncated, and we document argon2id as the upgrade path.
package auth

import (
	"errors"

	"golang.org/x/crypto/bcrypt"
)

const (
	// bcryptCost of 12 is ~250ms/hash on typical 2020s hardware — a sensible
	// balance of user-facing latency and brute-force resistance.
	bcryptCost = 12

	// MinPasswordLen is a floor; NIST guidance favours length over complexity.
	MinPasswordLen = 8
	// MaxPasswordLen guards bcrypt's 72-byte input limit so passwords are never
	// silently truncated. (A byte, not a rune, but 64 chars is plenty.)
	MaxPasswordLen = 64
)

var ErrPasswordLength = errors.New("password must be between 8 and 64 characters")

// HashPassword returns a bcrypt hash suitable for storage.
func HashPassword(plain string) (string, error) {
	if len(plain) < MinPasswordLen || len(plain) > MaxPasswordLen {
		return "", ErrPasswordLength
	}
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// CheckPassword reports whether plain matches the stored bcrypt hash. It runs in
// constant time relative to the hash, so it is safe against timing attacks.
func CheckPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}
