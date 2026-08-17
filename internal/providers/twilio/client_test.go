package twilio

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"thundercall-go/internal/config"
)

func TestSendVoiceLogOnlyWithoutCredentials(t *testing.T) {
	t.Parallel()

	provider := New(config.TwilioConfig{
		VoiceFrom:    "+18005551212",
		VoiceLogOnly: true,
	})

	now := time.Unix(1786547045, 123).UTC()
	provider.now = func() time.Time { return now }

	var logged string
	provider.logf = func(format string, args ...any) {
		logged = fmt.Sprintf(format, args...)
	}

	result, err := provider.SendVoice(context.Background(), "+14085550123", "This is a test ThunderCall voice notification.")
	if err != nil {
		t.Fatalf("SendVoice() error = %v", err)
	}

	if result.Provider != "twilio_voice" {
		t.Fatalf("SendVoice().Provider = %q, want %q", result.Provider, "twilio_voice")
	}
	if result.ProviderMessageID != "dryrun-voice-1786547045000000123" {
		t.Fatalf("SendVoice().ProviderMessageID = %q, want deterministic dry-run id", result.ProviderMessageID)
	}
	if result.Status != "sent" {
		t.Fatalf("SendVoice().Status = %q, want %q", result.Status, "sent")
	}
	if !strings.Contains(logged, "+14085550123") {
		t.Fatalf("SendVoice() log = %q, want destination", logged)
	}
	if !strings.Contains(logged, "dryrun-voice-1786547045000000123") {
		t.Fatalf("SendVoice() log = %q, want dry-run provider id", logged)
	}
}

func TestSendVoiceRequiresCredentialsWhenLogOnlyDisabled(t *testing.T) {
	t.Parallel()

	provider := New(config.TwilioConfig{
		VoiceLogOnly: false,
	})

	if _, err := provider.SendVoice(context.Background(), "+14085550123", "Test"); err == nil {
		t.Fatal("SendVoice() error = nil, want configuration error")
	}
}
