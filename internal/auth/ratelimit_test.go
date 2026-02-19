package auth

import (
	"testing"
	"time"
)

func TestLoginLimiter_BlocksAfterMaxFails(t *testing.T) {
	l := &LoginLimiter{entries: make(map[string]*ipEntry)}
	ip := "1.2.3.4"

	// First 4 failures should not block.
	for i := 0; i < loginMaxFails-1; i++ {
		if blocked := l.RecordFailure(ip); blocked {
			t.Fatalf("attempt %d: unexpectedly blocked", i+1)
		}
		if l.IsBlocked(ip) {
			t.Fatalf("attempt %d: IsBlocked returned true before limit", i+1)
		}
	}

	// 5th failure triggers the block.
	if blocked := l.RecordFailure(ip); !blocked {
		t.Fatal("5th failure should have returned newlyBlocked=true")
	}
	if !l.IsBlocked(ip) {
		t.Fatal("IsBlocked should be true after 5th failure")
	}
}

func TestLoginLimiter_NewlyBlockedOnlyOnce(t *testing.T) {
	l := &LoginLimiter{entries: make(map[string]*ipEntry)}
	ip := "1.2.3.4"

	for i := 0; i < loginMaxFails; i++ {
		l.RecordFailure(ip)
	}

	// Further failures while already blocked must not re-trigger.
	if blocked := l.RecordFailure(ip); blocked {
		t.Fatal("subsequent failure while already blocked should not return newlyBlocked=true")
	}
}

func TestLoginLimiter_SlidingWindowExpiry(t *testing.T) {
	l := &LoginLimiter{entries: make(map[string]*ipEntry)}
	ip := "1.2.3.4"

	// Inject 4 failures with timestamps older than the window.
	old := time.Now().Add(-(loginWindow + time.Second))
	l.mu.Lock()
	l.entries[ip] = &ipEntry{
		failures: []time.Time{old, old, old, old},
	}
	l.mu.Unlock()

	// One fresh failure should not block (old ones get trimmed).
	if blocked := l.RecordFailure(ip); blocked {
		t.Fatal("should not block: old failures are outside the window")
	}
	if l.IsBlocked(ip) {
		t.Fatal("IsBlocked should be false after window expiry")
	}
}

func TestLoginLimiter_DifferentIPsAreIndependent(t *testing.T) {
	l := &LoginLimiter{entries: make(map[string]*ipEntry)}

	for i := 0; i < loginMaxFails; i++ {
		l.RecordFailure("1.1.1.1")
	}
	if !l.IsBlocked("1.1.1.1") {
		t.Fatal("1.1.1.1 should be blocked")
	}
	if l.IsBlocked("2.2.2.2") {
		t.Fatal("2.2.2.2 should not be affected by 1.1.1.1's failures")
	}
}

func TestLoginLimiter_CleanupRemovesExpiredEntries(t *testing.T) {
	l := &LoginLimiter{entries: make(map[string]*ipEntry)}
	ip := "1.2.3.4"

	// Insert a stale entry: block expired, failures outside window.
	l.mu.Lock()
	l.entries[ip] = &ipEntry{
		failures:     []time.Time{time.Now().Add(-(loginWindow + time.Second))},
		blockedUntil: time.Now().Add(-time.Second),
	}
	l.mu.Unlock()

	// Run cleanup inline (not via goroutine).
	l.mu.Lock()
	now := time.Now()
	cutoff := now.Add(-loginWindow)
	for candidate, e := range l.entries {
		if now.Before(e.blockedUntil) {
			continue
		}
		hasRecent := false
		for _, ts := range e.failures {
			if ts.After(cutoff) {
				hasRecent = true
				break
			}
		}
		if !hasRecent {
			delete(l.entries, candidate)
		}
	}
	l.mu.Unlock()

	l.mu.Lock()
	_, exists := l.entries[ip]
	l.mu.Unlock()

	if exists {
		t.Fatal("stale entry should have been removed by cleanup")
	}
}
