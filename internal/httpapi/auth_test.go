package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHashPasswordAndCheckPassword(t *testing.T) {
	hash, err := HashPassword("super-secret")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	if hash == "" {
		t.Fatal("HashPassword returned an empty hash")
	}

	if err := CheckPassword(hash, "super-secret"); err != nil {
		t.Fatalf("CheckPassword rejected the original password: %v", err)
	}
	if err := CheckPassword(hash, "wrong-password"); err == nil {
		t.Fatal("CheckPassword accepted the wrong password")
	}
}

func TestHashPasswordRejectsBlankPassword(t *testing.T) {
	if _, err := HashPassword("   "); err == nil {
		t.Fatal("HashPassword accepted a blank password")
	}
}

func TestNewSessionToken(t *testing.T) {
	tokenOne, hashOne, err := newSessionToken()
	if err != nil {
		t.Fatalf("newSessionToken returned error: %v", err)
	}
	tokenTwo, hashTwo, err := newSessionToken()
	if err != nil {
		t.Fatalf("newSessionToken returned error: %v", err)
	}

	if tokenOne == "" || hashOne == "" {
		t.Fatal("first token or hash was empty")
	}
	if tokenTwo == "" || hashTwo == "" {
		t.Fatal("second token or hash was empty")
	}
	if len(hashOne) != 64 || len(hashTwo) != 64 {
		t.Fatalf("expected 64-character SHA-256 hashes, got %d and %d", len(hashOne), len(hashTwo))
	}
	if hashOne != hashToken(tokenOne) {
		t.Fatal("first token hash did not match hashToken output")
	}
	if hashTwo != hashToken(tokenTwo) {
		t.Fatal("second token hash did not match hashToken output")
	}
	if tokenOne == tokenTwo {
		t.Fatal("expected two generated session tokens to differ")
	}
	if hashOne == hashTwo {
		t.Fatal("expected two generated session hashes to differ")
	}
}

func TestBearerToken(t *testing.T) {
	tests := []struct {
		name     string
		header   string
		expected string
		ok       bool
	}{
		{name: "standard", header: "Bearer abc123", expected: "abc123", ok: true},
		{name: "lowercase prefix", header: "bearer abc123", expected: "abc123", ok: true},
		{name: "trims spaces", header: "  Bearer   abc123   ", expected: "abc123", ok: true},
		{name: "missing token", header: "Bearer   ", ok: false},
		{name: "wrong scheme", header: "Basic abc123", ok: false},
		{name: "empty header", header: "", ok: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			token, ok := bearerToken(tc.header)
			if ok != tc.ok {
				t.Fatalf("expected ok=%t, got %t", tc.ok, ok)
			}
			if token != tc.expected {
				t.Fatalf("expected token %q, got %q", tc.expected, token)
			}
		})
	}
}

func TestSessionExpiry(t *testing.T) {
	now := time.Date(2026, time.July, 31, 14, 15, 0, 0, time.UTC)
	server := &Server{
		sessionTTL: 90 * time.Minute,
		now: func() time.Time {
			return now
		},
	}

	expiresAt := server.sessionExpiry()
	expected := now.Add(90 * time.Minute)
	if !expiresAt.Equal(expected) {
		t.Fatalf("expected expiry %s, got %s", expected, expiresAt)
	}
}

func TestHealthzHandler(t *testing.T) {
	server := NewServer(nil, time.Hour, nil)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200 response, got %d", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("expected application/json content type, got %q", got)
	}
	if body := recorder.Body.String(); body != "{\"ok\":true}\n" {
		t.Fatalf("unexpected healthz body: %q", body)
	}
}
