package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	loginWindow   = 15 * time.Minute
	loginMaxFails = 5
	loginBlock    = 1 * time.Hour
	cleanupPeriod = 5 * time.Minute
)

type ipEntry struct {
	failures     []time.Time
	blockedUntil time.Time
}

// LoginLimiter tracks failed login attempts per IP using a sliding window.
// After loginMaxFails failures within loginWindow, the IP is blocked for loginBlock.
// Safe for concurrent use.
type LoginLimiter struct {
	mu      sync.Mutex
	entries map[string]*ipEntry
}

// NewLoginLimiter creates a LoginLimiter and starts a background goroutine that
// periodically removes expired entries to prevent unbounded memory growth.
func NewLoginLimiter() *LoginLimiter {
	l := &LoginLimiter{entries: make(map[string]*ipEntry)}
	go l.cleanup()
	return l
}

// IsBlocked reports whether the IP is currently blocked.
func (l *LoginLimiter) IsBlocked(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[ip]
	return ok && time.Now().Before(e.blockedUntil)
}

// RecordFailure records a failed login attempt for the IP. If the number of
// failures within the sliding window reaches loginMaxFails, the IP is blocked.
// Returns true if this call triggered a new block.
func (l *LoginLimiter) RecordFailure(ip string) (newlyBlocked bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	e, ok := l.entries[ip]
	if !ok {
		e = &ipEntry{}
		l.entries[ip] = e
	}

	// Trim attempts that have fallen outside the sliding window.
	cutoff := now.Add(-loginWindow)
	filtered := e.failures[:0]
	for _, t := range e.failures {
		if t.After(cutoff) {
			filtered = append(filtered, t)
		}
	}
	e.failures = append(filtered, now)

	if len(e.failures) >= loginMaxFails && !now.Before(e.blockedUntil) {
		e.blockedUntil = now.Add(loginBlock)
		return true
	}
	return false
}

// notifyLoginBlock posts a Discord message if SIDEKICK_DISCORD_WEBHOOK is set.
// Runs in a goroutine — never blocks the request path.
func notifyLoginBlock(ip string, until time.Time) {
	webhookURL := os.Getenv("SIDEKICK_DISCORD_WEBHOOK")
	if webhookURL == "" {
		return
	}

	msg := fmt.Sprintf(
		"[sidekick] Login blocked: IP %s exceeded %d failed attempts. Blocked until %s.",
		ip, loginMaxFails, until.UTC().Format(time.RFC3339),
	)
	body, err := json.Marshal(map[string]string{"content": msg})
	if err != nil {
		return
	}

	resp, err := http.Post(webhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return
	}
	resp.Body.Close()
}

// clientIP extracts the real client IP from the request. It checks
// X-Forwarded-For and X-Real-IP (set by reverse proxies such as Caddy)
// before falling back to RemoteAddr.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// X-Forwarded-For may be a comma-separated list; the leftmost is the
		// original client.
		if i := strings.IndexByte(xff, ','); i != -1 {
			xff = xff[:i]
		}
		if ip := strings.TrimSpace(xff); ip != "" {
			return ip
		}
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// cleanup removes entries that are no longer blocked and have no recent
// failures. Runs on a fixed ticker for the lifetime of the process.
func (l *LoginLimiter) cleanup() {
	ticker := time.NewTicker(cleanupPeriod)
	defer ticker.Stop()
	for range ticker.C {
		l.mu.Lock()
		now := time.Now()
		cutoff := now.Add(-loginWindow)
		for ip, e := range l.entries {
			if now.Before(e.blockedUntil) {
				continue // still blocked, keep
			}
			hasRecent := false
			for _, t := range e.failures {
				if t.After(cutoff) {
					hasRecent = true
					break
				}
			}
			if !hasRecent {
				delete(l.entries, ip)
			}
		}
		l.mu.Unlock()
	}
}
