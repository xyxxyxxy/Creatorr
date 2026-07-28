package auth

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	loginMaxFails   = 5
	loginLockWindow = 5 * time.Minute
)

type loginBucket struct {
	fails    int
	lockedUntil time.Time
}

// LoginLimiter rate-limits failed login attempts by client IP.
type LoginLimiter struct {
	mu   sync.Mutex
	byIP map[string]*loginBucket
}

// NewLoginLimiter builds an empty limiter.
func NewLoginLimiter() *LoginLimiter {
	return &LoginLimiter{byIP: map[string]*loginBucket{}}
}

// DefaultLoginLimiter is used by the web UI when Handler has no override.
var DefaultLoginLimiter = NewLoginLimiter()

// ClientIP returns the client address for login rate limiting.
// When TrustForwardedProto is off, only RemoteAddr is used (ignore client XFF spoofing).
// When on, prefer X-Real-IP / X-Forwarded-For (first hop), else RemoteAddr.
func ClientIP(r *http.Request) string {
	if trustForwardedProto {
		if ip := strings.TrimSpace(r.Header.Get("X-Real-IP")); ip != "" {
			return stripPort(ip)
		}
		if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
			parts := strings.Split(xff, ",")
			if len(parts) > 0 {
				return stripPort(strings.TrimSpace(parts[0]))
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return stripPort(r.RemoteAddr)
	}
	return host
}

func stripPort(s string) string {
	s = strings.TrimSpace(s)
	if host, _, err := net.SplitHostPort(s); err == nil {
		return host
	}
	return s
}

// Allow reports whether a login attempt from ip may proceed.
func (l *LoginLimiter) Allow(ip string) bool {
	if l == nil {
		return true
	}
	ip = strings.TrimSpace(ip)
	if ip == "" {
		ip = "unknown"
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.byIP[ip]
	if b == nil {
		return true
	}
	if now.Before(b.lockedUntil) {
		return false
	}
	if !b.lockedUntil.IsZero() && now.After(b.lockedUntil) {
		// Lock expired; reset.
		delete(l.byIP, ip)
		return true
	}
	return true
}

// Fail records a failed login. Returns remaining lock if newly locked.
func (l *LoginLimiter) Fail(ip string) {
	if l == nil {
		return
	}
	ip = strings.TrimSpace(ip)
	if ip == "" {
		ip = "unknown"
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.byIP[ip]
	if b == nil {
		b = &loginBucket{}
		l.byIP[ip] = b
	}
	if now.Before(b.lockedUntil) {
		return
	}
	b.fails++
	if b.fails >= loginMaxFails {
		b.lockedUntil = now.Add(loginLockWindow)
		b.fails = 0
	}
}

// Success clears failure state for ip.
func (l *LoginLimiter) Success(ip string) {
	if l == nil {
		return
	}
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return
	}
	l.mu.Lock()
	delete(l.byIP, ip)
	l.mu.Unlock()
}
