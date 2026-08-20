package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRequestRateLimiterBlocksAfterLimitAndResetsAfterWindow(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 19, 12, 0, 0, 0, time.UTC)
	limiter := newRequestRateLimiter(2, time.Minute, func() time.Time { return now })

	if allowed, _ := limiter.Allow("198.51.100.10"); !allowed {
		t.Fatal("first request blocked, want allowed")
	}
	if allowed, _ := limiter.Allow("198.51.100.10"); !allowed {
		t.Fatal("second request blocked, want allowed")
	}

	allowed, retryAfter := limiter.Allow("198.51.100.10")
	if allowed {
		t.Fatal("third request allowed, want blocked")
	}
	if retryAfter != time.Minute {
		t.Fatalf("retryAfter = %s, want %s", retryAfter, time.Minute)
	}

	now = now.Add(time.Minute)
	if allowed, _ := limiter.Allow("198.51.100.10"); !allowed {
		t.Fatal("request after window reset blocked, want allowed")
	}
}

func TestPublicSignupClientIDPrefersForwardedHeaders(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "/api/users/signup", nil)
	req.RemoteAddr = "203.0.113.10:4444"
	req.Header.Set("X-Forwarded-For", "198.51.100.11, 203.0.113.10")
	if got := publicSignupClientID(req); got != "198.51.100.11" {
		t.Fatalf("publicSignupClientID(X-Forwarded-For) = %q, want 198.51.100.11", got)
	}

	req.Header.Set("CF-Connecting-IP", "192.0.2.44")
	if got := publicSignupClientID(req); got != "192.0.2.44" {
		t.Fatalf("publicSignupClientID(CF-Connecting-IP) = %q, want 192.0.2.44", got)
	}
}

func TestPublicSignupRateLimitReturnsTooManyRequests(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, time.Hour, nil)
	server.ConfigurePublicSignupRateLimit(2, time.Minute)

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/users/signup", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "198.51.100.25:1234"

		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, req)

		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("request %d status = %d, want 400 before limit is exceeded", i+1, recorder.Code)
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/api/users/signup", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "198.51.100.25:1234"

	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, req)

	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", recorder.Code)
	}
	if got := recorder.Header().Get("Retry-After"); got == "" {
		t.Fatal("expected Retry-After header on rate limited response")
	}

	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if got, _ := body["message"].(string); got != "Too many signup attempts. Please try again soon." {
		t.Fatalf("message = %q, want rate limit message", got)
	}
}

func TestPublicSignupRateLimitIsSharedAcrossAliasRoutesPerClient(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, time.Hour, nil)
	server.ConfigurePublicSignupRateLimit(1, time.Minute)

	first := httptest.NewRequest(http.MethodPost, "/api/users/signup", strings.NewReader(`{}`))
	first.Header.Set("Content-Type", "application/json")
	first.Header.Set("CF-Connecting-IP", "198.51.100.33")
	firstRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(firstRecorder, first)
	if firstRecorder.Code != http.StatusBadRequest {
		t.Fatalf("first request status = %d, want 400", firstRecorder.Code)
	}

	second := httptest.NewRequest(http.MethodPost, "/v1/public/signups", strings.NewReader(`{}`))
	second.Header.Set("Content-Type", "application/json")
	second.Header.Set("CF-Connecting-IP", "198.51.100.33")
	secondRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(secondRecorder, second)
	if secondRecorder.Code != http.StatusTooManyRequests {
		t.Fatalf("second alias request status = %d, want 429", secondRecorder.Code)
	}

	otherClient := httptest.NewRequest(http.MethodPost, "/api/products/ignored/records", strings.NewReader(`{}`))
	otherClient.Header.Set("Content-Type", "application/json")
	otherClient.Header.Set("CF-Connecting-IP", "198.51.100.34")
	otherRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(otherRecorder, otherClient)
	if otherRecorder.Code != http.StatusBadRequest {
		t.Fatalf("other client status = %d, want 400", otherRecorder.Code)
	}
}
