//go:build integration

package twilio

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"

	"thundercall-go/internal/config"
)

const (
	runLiveVoiceFunctionTestEnv   = "THUNDERCALL_RUN_LIVE_TWILIO_TEST"
	liveVoiceFunctionToEnv        = "THUNDERCALL_LIVE_TWILIO_TEST_TO"
	liveVoiceFunctionEventEnv     = "THUNDERCALL_LIVE_TWILIO_TEST_EVENT"
	liveVoiceFunctionAccountIDEnv = "THUNDERCALL_LIVE_TWILIO_TEST_ACCOUNT_ID"
)

func TestLiveVoiceFunctionCall(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live Twilio voice function test in short mode")
	}
	if !strings.EqualFold(strings.TrimSpace(os.Getenv(runLiveVoiceFunctionTestEnv)), "1") &&
		!strings.EqualFold(strings.TrimSpace(os.Getenv(runLiveVoiceFunctionTestEnv)), "true") {
		t.Skipf("%s is not enabled; this test places one real Twilio voice call", runLiveVoiceFunctionTestEnv)
	}

	to := normalizeLiveVoiceFunctionDestination(t, requiredEnvOrSkip(t, liveVoiceFunctionToEnv))
	eventCode := validateLiveVoiceFunctionEvent(t, requiredEnvOrSkip(t, liveVoiceFunctionEventEnv))
	accountID := validateLiveVoiceFunctionAccountID(t, requiredEnvOrSkip(t, liveVoiceFunctionAccountIDEnv))
	accountSID := requiredEnvOrSkip(t, "TWILIO_ACCOUNT_SID")
	authToken := requiredEnvOrSkip(t, "TWILIO_AUTH_TOKEN")
	voiceFrom := requiredEnvOrSkip(t, "TWILIO_VOICE_FROM")
	voiceURL := requiredEnvOrSkip(t, "TWILIO_VOICE_URL")

	provider := New(config.TwilioConfig{
		AccountSID:   accountSID,
		AuthToken:    authToken,
		VoiceFrom:    voiceFrom,
		VoiceURL:     voiceURL,
		VoiceLogOnly: false,
	})

	expectedVoiceURL, err := BuildVoiceFunctionURL(voiceURL, eventCode, accountID)
	if err != nil {
		t.Fatalf("BuildVoiceFunctionURL() error = %v", err)
	}

	t.Logf(
		"placing live Twilio function-backed call to %s for event=%s account_id=%d via %s",
		to,
		eventCode,
		accountID,
		expectedVoiceURL,
	)

	result, err := provider.SendVoice(context.Background(), VoiceRequest{
		To:        to,
		Body:      "ThunderCall live Twilio function integration test.",
		EventCode: eventCode,
		AccountID: accountID,
	})
	if err != nil {
		t.Fatalf("SendVoice() error = %v", err)
	}

	if result.Provider != "twilio_voice" {
		t.Fatalf("Provider = %q, want twilio_voice", result.Provider)
	}
	if strings.TrimSpace(result.ProviderMessageID) == "" {
		t.Fatal("ProviderMessageID is empty, want Twilio call SID")
	}
	if !strings.HasPrefix(strings.TrimSpace(result.ProviderMessageID), "CA") {
		t.Fatalf("ProviderMessageID = %q, want Twilio call SID", result.ProviderMessageID)
	}

	t.Logf(
		"live Twilio call placed successfully sid=%s event=%s account_id=%d to=%s",
		result.ProviderMessageID,
		eventCode,
		accountID,
		to,
	)
}

func requiredEnvOrSkip(t *testing.T, key string) string {
	t.Helper()

	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		t.Skipf("%s is required; this test places one real Twilio voice call", key)
	}
	return value
}

func validateLiveVoiceFunctionEvent(t *testing.T, raw string) string {
	t.Helper()

	value := strings.ToUpper(strings.TrimSpace(raw))
	switch value {
	case "WSW", "FFW", "TOR", "SVR":
		return value
	default:
		t.Fatalf("%s = %q, want one of WSW, FFW, TOR, SVR", liveVoiceFunctionEventEnv, raw)
		return ""
	}
}

func validateLiveVoiceFunctionAccountID(t *testing.T, raw string) int64 {
	t.Helper()

	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		t.Fatalf("parse %s: %v", liveVoiceFunctionAccountIDEnv, err)
	}

	switch value {
	case 2, 3, 4:
		return value
	default:
		t.Fatalf("%s = %d, want one of 2, 3, 4", liveVoiceFunctionAccountIDEnv, value)
		return 0
	}
}

func normalizeLiveVoiceFunctionDestination(t *testing.T, raw string) string {
	t.Helper()

	value := strings.TrimSpace(raw)
	digits := make([]rune, 0, len(value))
	for _, r := range value {
		if r >= '0' && r <= '9' {
			digits = append(digits, r)
		}
	}

	switch {
	case len(digits) == 10:
		return "+1" + string(digits)
	case len(digits) == 11 && digits[0] == '1':
		return "+" + string(digits)
	case strings.HasPrefix(value, "+") && len(digits) >= 10:
		return "+" + string(digits)
	default:
		t.Fatalf("%s = %q, want a 10-digit US number or E.164 number", liveVoiceFunctionToEnv, raw)
		return ""
	}
}
