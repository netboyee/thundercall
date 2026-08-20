package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHandlePublicSignupRejectsUnsignedRequestWhenProxySecretConfigured(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, time.Hour, nil)
	server.ConfigurePublicSignupProxyAuth("signup-secret", 5*time.Minute)

	req := httptest.NewRequest(http.MethodPost, "/api/users/signup", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}

func TestHandlePublicSignupAcceptsSignedRequestWhenProxySecretConfigured(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 19, 19, 0, 0, 0, time.UTC)
	server := NewServer(nil, time.Hour, nil)
	server.now = func() time.Time { return now }
	server.ConfigurePublicSignupProxyAuth("signup-secret", 5*time.Minute)

	body := []byte(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/api/users/signup", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	signPublicSignupProxyRequest(req, "signup-secret", now, body)

	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 after auth passes and request validation runs", recorder.Code)
	}
}

func TestHandlePublicSignupRejectsStaleSignedRequest(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 19, 19, 0, 0, 0, time.UTC)
	server := NewServer(nil, time.Hour, nil)
	server.now = func() time.Time { return now }
	server.ConfigurePublicSignupProxyAuth("signup-secret", time.Minute)

	body := []byte(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/api/users/signup", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	signPublicSignupProxyRequest(req, "signup-secret", now.Add(-2*time.Minute), body)

	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for stale timestamp", recorder.Code)
	}
}

func TestHandlePublicSignupRateLimitUsesTrustedProxyClientIP(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 19, 19, 0, 0, 0, time.UTC)
	server := NewServer(nil, time.Hour, nil)
	server.now = func() time.Time { return now }
	server.ConfigurePublicSignupProxyAuth("signup-secret", 5*time.Minute)
	server.ConfigurePublicSignupRateLimit(1, time.Minute)

	first := signedPublicSignupRequest(now, "198.51.100.10")
	first.RemoteAddr = "203.0.113.10:1234"
	firstRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(firstRecorder, first)
	if firstRecorder.Code != http.StatusBadRequest {
		t.Fatalf("first status = %d, want 400", firstRecorder.Code)
	}

	second := signedPublicSignupRequest(now, "198.51.100.11")
	second.RemoteAddr = "203.0.113.10:1234"
	secondRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(secondRecorder, second)
	if secondRecorder.Code != http.StatusBadRequest {
		t.Fatalf("second status = %d, want 400 for different trusted client", secondRecorder.Code)
	}

	third := signedPublicSignupRequest(now, "198.51.100.10")
	third.RemoteAddr = "203.0.113.10:1234"
	thirdRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(thirdRecorder, third)
	if thirdRecorder.Code != http.StatusTooManyRequests {
		t.Fatalf("third status = %d, want 429 for repeated trusted client", thirdRecorder.Code)
	}
}

func signedPublicSignupRequest(now time.Time, clientIP string) *http.Request {
	body := []byte(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/api/users/signup", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(publicSignupProxyClientIPHeader, clientIP)
	signPublicSignupProxyRequest(req, "signup-secret", now, body)
	return req
}

func signPublicSignupProxyRequest(req *http.Request, secret string, now time.Time, body []byte) {
	timestamp := now.UTC().Format(time.RFC3339)
	signature := computePublicSignupProxySignature(secret, req.Method, req.URL.Path, timestamp, body)
	req.Header.Set(publicSignupProxyTimestampHeader, timestamp)
	req.Header.Set(publicSignupProxySignatureHeader, signature)
}
