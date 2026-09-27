package ratelimit

import (
	"testing"
	"time"
)

func TestLimiter(t *testing.T) {
	now := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)
	l := New(3, time.Minute)
	l.now = func() time.Time { return now }

	for i := range 3 {
		if b, _ := l.Blocked("a"); b {
			t.Fatalf("blocked after %d failures", i)
		}
		l.Fail("a")
		now = now.Add(10 * time.Second)
	}
	b, wait := l.Blocked("a")
	if !b || wait != 30*time.Second {
		t.Errorf("Blocked = %v, %v; want true, 30s", b, wait)
	}
	if b, _ := l.Blocked("b"); b {
		t.Error("other key blocked")
	}

	now = now.Add(31 * time.Second) // first failure ages out
	if b, _ := l.Blocked("a"); b {
		t.Error("still blocked after the window")
	}

	l.Fail("a")
	l.Reset("a")
	if b, _ := l.Blocked("a"); b {
		t.Error("blocked after Reset")
	}
}
