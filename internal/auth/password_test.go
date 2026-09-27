package auth

import (
	"strings"
	"testing"
)

func TestPasswordRoundTrip(t *testing.T) {
	h := HashPassword("correct horse")
	if !strings.HasPrefix(h, "$argon2id$v=19$m=65536,t=3,p=4$") {
		t.Errorf("unexpected hash format: %s", h)
	}
	if ok, err := VerifyPassword("correct horse", h); err != nil || !ok {
		t.Errorf("correct password: ok=%v err=%v", ok, err)
	}
	if ok, err := VerifyPassword("wrong horse", h); err != nil || ok {
		t.Errorf("wrong password: ok=%v err=%v", ok, err)
	}
	if HashPassword("correct horse") == h {
		t.Error("two hashes of the same password are equal; salt not random")
	}
}

func TestVerifyPasswordBadHash(t *testing.T) {
	for _, bad := range []string{
		"",
		"plaintext",
		"$argon2i$v=19$m=65536,t=3,p=4$c2FsdA$a2V5",
		"$argon2id$v=18$m=65536,t=3,p=4$c2FsdA$a2V5",
		"$argon2id$v=19$m=0,t=3,p=4$c2FsdA$a2V5",
		"$argon2id$v=19$m=65536,t=3,p=4$!!!$a2V5",
		"$argon2id$v=19$m=65536,t=3,p=4$c2FsdA$",
	} {
		if _, err := VerifyPassword("x", bad); err != ErrBadHash {
			t.Errorf("VerifyPassword(%q) err = %v, want ErrBadHash", bad, err)
		}
	}
}

func TestGeneratePassword(t *testing.T) {
	p := GeneratePassword(20)
	if len(p) != 20 {
		t.Fatalf("len = %d", len(p))
	}
	for _, c := range p {
		if !strings.ContainsRune(passwordAlphabet, c) {
			t.Errorf("unexpected character %q", c)
		}
	}
	if GeneratePassword(20) == p {
		t.Error("two generated passwords are equal")
	}
}
