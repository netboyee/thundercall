package httpapi

import (
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultPublicSignupRateLimitCount  = 10
	defaultPublicSignupRateLimitWindow = time.Minute
)

type requestRateLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	now     func() time.Time
	entries map[string]requestRateLimitEntry
}

type requestRateLimitEntry struct {
	windowStart time.Time
	count       int
}

func newRequestRateLimiter(limit int, window time.Duration, now func() time.Time) *requestRateLimiter {
	if limit <= 0 || window <= 0 {
		return nil
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &requestRateLimiter{
		limit:   limit,
		window:  window,
		now:     now,
		entries: make(map[string]requestRateLimitEntry),
	}
}

func (l *requestRateLimiter) Allow(key string) (bool, time.Duration) {
	if l == nil {
		return true, 0
	}

	now := l.now().UTC()
	if strings.TrimSpace(key) == "" {
		key = "unknown"
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	for existingKey, entry := range l.entries {
		if now.Sub(entry.windowStart) >= l.window {
			delete(l.entries, existingKey)
		}
	}

	entry, ok := l.entries[key]
	if !ok || now.Sub(entry.windowStart) >= l.window {
		l.entries[key] = requestRateLimitEntry{
			windowStart: now,
			count:       1,
		}
		return true, 0
	}

	if entry.count < l.limit {
		entry.count++
		l.entries[key] = entry
		return true, 0
	}

	retryAfter := l.window - now.Sub(entry.windowStart)
	if retryAfter < 0 {
		retryAfter = 0
	}
	return false, retryAfter
}

func publicSignupClientID(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); value != "" {
		return value
	}
	if value := firstForwardedFor(r.Header.Get("X-Forwarded-For")); value != "" {
		return value
	}
	if value := strings.TrimSpace(r.Header.Get("X-Real-IP")); value != "" {
		return value
	}

	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

func firstForwardedFor(value string) string {
	for _, part := range strings.Split(value, ",") {
		if token := strings.TrimSpace(part); token != "" {
			return token
		}
	}
	return ""
}

func (s *Server) configurePublicSignupRateLimit(limit int, window time.Duration) {
	s.publicSignupLimiter = newRequestRateLimiter(limit, window, func() time.Time {
		if s.now != nil {
			return s.now()
		}
		return time.Now().UTC()
	})
}

func (s *Server) ConfigurePublicSignupRateLimit(limit int, window time.Duration) {
	s.configurePublicSignupRateLimit(limit, window)
}

func (s *Server) allowPublicSignupRequest(w http.ResponseWriter, r *http.Request) bool {
	if s.publicSignupLimiter == nil {
		return true
	}

	clientID := publicSignupClientID(r)
	allowed, retryAfter := s.publicSignupLimiter.Allow(clientID)
	if allowed {
		return true
	}

	retryAfterSeconds := int(math.Ceil(retryAfter.Seconds()))
	if retryAfterSeconds < 1 {
		retryAfterSeconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds))
	signupLogger.Warnf(
		"event=signup_rate_limited client_id=%s path=%s retry_after_seconds=%d",
		clientID,
		r.URL.Path,
		retryAfterSeconds,
	)
	writePublicSignupError(w, http.StatusTooManyRequests, "Too many signup attempts. Please try again soon.")
	return false
}
