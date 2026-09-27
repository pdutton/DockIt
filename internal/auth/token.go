package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
)

// TokenPrefix starts every API token, so a leaked token is easy to recognize
// (and for secret scanners to find).
const TokenPrefix = "dockit_"

// NewToken returns a new random API token (256 bits) and its stored hash.
// The token itself is shown to the user once and never stored.
func NewToken() (token, hash string) {
	b := make([]byte, 32)
	rand.Read(b)
	token = TokenPrefix + base64.RawURLEncoding.EncodeToString(b)
	return token, HashToken(token)
}

// HashToken returns the stored form of a token.  Tokens are high-entropy
// random values, so a fast hash is enough; no salt or stretching is needed.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// NewTokenID returns a short random ID by which a user can name a token, for
// example to revoke it.  It reveals nothing about the token.
func NewTokenID() string {
	return "tok_" + strings.ToLower(randomString(10, "abcdefghijkmnpqrstuvwxyz23456789"))
}
