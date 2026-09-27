// Package auth handles credentials: password hashing and generation.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// argon2id parameters for new hashes.  Verification reads the parameters from
// the stored hash, so these can be raised later without invalidating
// existing passwords.
const (
	argonMemory  = 64 * 1024 // KiB
	argonTime    = 3
	argonThreads = 4
	argonKeyLen  = 32
	argonSaltLen = 16
)

var b64 = base64.RawStdEncoding

// ErrBadHash is returned when a stored password hash cannot be parsed.
var ErrBadHash = errors.New("malformed password hash")

// HashPassword returns an argon2id hash of password in the standard PHC
// string format: $argon2id$v=19$m=...,t=...,p=...$salt$key
func HashPassword(password string) string {
	salt := make([]byte, argonSaltLen)
	rand.Read(salt)
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		b64.EncodeToString(salt), b64.EncodeToString(key))
}

// VerifyPassword reports whether password matches hash.
func VerifyPassword(password, hash string) (bool, error) {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return false, ErrBadHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, ErrBadHash
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil || m == 0 || t == 0 || p == 0 {
		return false, ErrBadHash
	}
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return false, ErrBadHash
	}
	want, err := b64.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false, ErrBadHash
	}
	got := argon2.IDKey([]byte(password), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// passwordAlphabet omits characters that are easily confused when read from
// a terminal: 0/O, 1/l/I.
const passwordAlphabet = "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// GeneratePassword returns a random one-time password of n characters.
func GeneratePassword(n int) string {
	return randomString(n, passwordAlphabet)
}

func randomString(n int, alphabet string) string {
	// Rejection sampling keeps the distribution uniform.
	limit := 256 - 256%len(alphabet)
	out := make([]byte, 0, n)
	buf := make([]byte, 1)
	for len(out) < n {
		rand.Read(buf)
		if int(buf[0]) < limit {
			out = append(out, alphabet[int(buf[0])%len(alphabet)])
		}
	}
	return string(out)
}
