package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	twilioprovider "thundercall-go/internal/providers/twilio"
)

func TestParseTwilioVoiceStatusCallback(t *testing.T) {
	t.Parallel()

	values := url.Values{}
	values.Set("CallSid", "CA123")
	values.Set("CallStatus", " completed ")
	values.Set("CallDuration", "17")

	callback, err := parseTwilioVoiceStatusCallback(values)
	if err != nil {
		t.Fatalf("parseTwilioVoiceStatusCallback() error = %v", err)
	}

	if callback.CallSID != "CA123" {
		t.Fatalf("CallSID = %q, want CA123", callback.CallSID)
	}
	if callback.CallStatus != "completed" {
		t.Fatalf("CallStatus = %q, want completed", callback.CallStatus)
	}
	if callback.DurationSeconds == nil || *callback.DurationSeconds != 17 {
		t.Fatalf("DurationSeconds = %v, want 17", callback.DurationSeconds)
	}
	if callback.ErrorMessage != nil {
		t.Fatalf("ErrorMessage = %v, want nil", *callback.ErrorMessage)
	}
}

func TestParseTwilioVoiceStatusCallbackBuildsFailureErrorMessage(t *testing.T) {
	t.Parallel()

	values := url.Values{}
	values.Set("CallSid", "CA456")
	values.Set("CallStatus", "busy")
	values.Set("ErrorCode", "486")
	values.Set("SipResponseCode", "486")

	callback, err := parseTwilioVoiceStatusCallback(values)
	if err != nil {
		t.Fatalf("parseTwilioVoiceStatusCallback() error = %v", err)
	}

	if callback.ErrorMessage == nil {
		t.Fatal("ErrorMessage = nil, want failure message")
	}
	if got := *callback.ErrorMessage; !strings.Contains(got, "busy") || !strings.Contains(got, "error_code=486") || !strings.Contains(got, "sip_response_code=486") {
		t.Fatalf("ErrorMessage = %q, want busy/error_code/sip_response_code details", got)
	}
}

func TestParseTwilioVoiceStatusCallbackRejectsInvalidPayload(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		values url.Values
		want   string
	}{
		{
			name:   "missing call sid",
			values: url.Values{"CallStatus": []string{"completed"}},
			want:   "CallSid is required",
		},
		{
			name:   "missing call status",
			values: url.Values{"CallSid": []string{"CA789"}},
			want:   "CallStatus is required",
		},
		{
			name: "bad duration",
			values: url.Values{
				"CallSid":      []string{"CA999"},
				"CallStatus":   []string{"completed"},
				"CallDuration": []string{"abc"},
			},
			want: "CallDuration must be numeric",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := parseTwilioVoiceStatusCallback(tc.values)
			if err == nil {
				t.Fatal("expected parse error")
			}
			if err.Error() != tc.want {
				t.Fatalf("error = %q, want %q", err.Error(), tc.want)
			}
		})
	}
}

func TestTwilioVoiceStatusCallbackApplyCallDetails(t *testing.T) {
	t.Parallel()

	callback := twilioVoiceStatusCallback{
		CallSID:    "CA321",
		CallStatus: "completed",
	}

	duration := 24
	callback.ApplyCallDetails(twilioprovider.VoiceCallDetails{
		SID:             "CA321",
		Status:          "completed",
		AnsweredBy:      "machine_end_beep",
		DurationSeconds: &duration,
	})

	if callback.AnsweredBy != "machine_end_beep" {
		t.Fatalf("AnsweredBy = %q, want machine_end_beep", callback.AnsweredBy)
	}
	if callback.DurationSeconds == nil || *callback.DurationSeconds != 24 {
		t.Fatalf("DurationSeconds = %v, want 24", callback.DurationSeconds)
	}
}

func TestShouldEnrichTwilioVoiceCallback(t *testing.T) {
	t.Parallel()

	if !shouldEnrichTwilioVoiceCallback(twilioVoiceStatusCallback{CallStatus: "completed"}) {
		t.Fatal("expected completed callback without AnsweredBy/Duration to require enrichment")
	}
	if shouldEnrichTwilioVoiceCallback(twilioVoiceStatusCallback{
		CallStatus:      "completed",
		AnsweredBy:      "human",
		DurationSeconds: intPtr(10),
	}) {
		t.Fatal("expected completed callback with AnsweredBy/Duration to skip enrichment")
	}
	if shouldEnrichTwilioVoiceCallback(twilioVoiceStatusCallback{CallStatus: "ringing"}) {
		t.Fatal("expected non-final status to skip enrichment")
	}
}

func TestValidateTwilioSignature(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, time.Hour, nil)
	server.twilioAuthToken = "test-auth-token"

	form := url.Values{}
	form.Set("CallSid", "CA123")
	form.Set("CallStatus", "completed")

	req := httptest.NewRequest(http.MethodPost, "https://example.com/api/providers/twilio/voice/status", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	req.Header.Set("X-Twilio-Signature", "invalid")
	if server.validateTwilioSignature(req, form) {
		t.Fatal("validateTwilioSignature() = true, want false for invalid signature")
	}

	req.Header.Set("X-Twilio-Signature", computeTwilioSignature(server.twilioAuthToken, twilioRequestURL(req), form))
	if !server.validateTwilioSignature(req, form) {
		t.Fatal("validateTwilioSignature() = false, want true for valid signature")
	}
}
