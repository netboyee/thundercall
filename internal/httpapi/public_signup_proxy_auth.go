package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	publicSignupProxyTimestampHeader = "X-Thundercall-Signup-Timestamp"
	publicSignupProxySignatureHeader = "X-Thundercall-Signup-Signature"
	publicSignupProxyClientIPHeader  = "X-Thundercall-Client-IP"
)

type publicSignupProxyAuthResult struct {
	clientID string
}

func (s *Server) configurePublicSignupProxyAuth(secret string, maxSkew time.Duration) {
	s.publicSignupProxySecret = strings.TrimSpace(secret)
	if maxSkew <= 0 {
		maxSkew = 5 * time.Minute
	}
	s.publicSignupProxyMaxSkew = maxSkew
}

func (s *Server) ConfigurePublicSignupProxyAuth(secret string, maxSkew time.Duration) {
	s.configurePublicSignupProxyAuth(secret, maxSkew)
}

func (s *Server) validatePublicSignupProxyRequest(r *http.Request, body []byte) (publicSignupProxyAuthResult, error) {
	secret := strings.TrimSpace(s.publicSignupProxySecret)
	if secret == "" {
		return publicSignupProxyAuthResult{clientID: publicSignupClientID(r)}, nil
	}

	timestamp := strings.TrimSpace(r.Header.Get(publicSignupProxyTimestampHeader))
	signature := strings.ToLower(strings.TrimSpace(r.Header.Get(publicSignupProxySignatureHeader)))
	if timestamp == "" || signature == "" {
		return publicSignupProxyAuthResult{}, fmt.Errorf("missing proxy auth headers")
	}

	requestAt, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		return publicSignupProxyAuthResult{}, fmt.Errorf("invalid proxy timestamp")
	}

	now := time.Now().UTC()
	if s.now != nil {
		now = s.now().UTC()
	}

	skew := s.publicSignupProxyMaxSkew
	if skew <= 0 {
		skew = 5 * time.Minute
	}
	delta := now.Sub(requestAt.UTC())
	if delta < 0 {
		delta = -delta
	}
	if delta > skew {
		return publicSignupProxyAuthResult{}, fmt.Errorf("stale proxy timestamp")
	}

	expected := computePublicSignupProxySignature(secret, r.Method, r.URL.Path, timestamp, body)
	if !hmac.Equal([]byte(signature), []byte(expected)) {
		return publicSignupProxyAuthResult{}, fmt.Errorf("invalid proxy signature")
	}

	clientID := strings.TrimSpace(r.Header.Get(publicSignupProxyClientIPHeader))
	if clientID == "" {
		clientID = publicSignupClientID(r)
	}

	return publicSignupProxyAuthResult{clientID: clientID}, nil
}

func computePublicSignupProxySignature(secret string, method string, path string, timestamp string, body []byte) string {
	var builder strings.Builder
	builder.WriteString(strings.ToUpper(strings.TrimSpace(method)))
	builder.WriteString("\n")
	builder.WriteString(strings.TrimSpace(path))
	builder.WriteString("\n")
	builder.WriteString(strings.TrimSpace(timestamp))
	builder.WriteString("\n")
	builder.Write(body)

	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(builder.String()))
	return hex.EncodeToString(mac.Sum(nil))
}
