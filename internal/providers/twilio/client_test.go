package twilio

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	twilioclient "github.com/twilio/twilio-go/client"

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

	result, err := provider.SendVoice(context.Background(), VoiceRequest{
		To:   "+14085550123",
		Body: "This is a test ThunderCall voice notification.",
	})
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

	if _, err := provider.SendVoice(context.Background(), VoiceRequest{
		To:   "+14085550123",
		Body: "Test",
	}); err == nil {
		t.Fatal("SendVoice() error = nil, want configuration error")
	}
}

func TestResolveVoiceDestinationUsesOverride(t *testing.T) {
	t.Parallel()

	provider := New(config.TwilioConfig{
		VoiceToOverride: "+14073530340",
	})

	got, overridden := provider.ResolveVoiceDestination("+15550000010")
	if !overridden {
		t.Fatal("ResolveVoiceDestination() overridden = false, want true")
	}
	if got != "+14073530340" {
		t.Fatalf("ResolveVoiceDestination() = %q, want override", got)
	}
}

func TestBuildTestVoiceBodyPrefixesIntendedRecipient(t *testing.T) {
	t.Parallel()

	provider := New(config.TwilioConfig{
		VoiceToOverride: "+14073530340",
	})

	got := provider.BuildTestVoiceBody("+15550000010", "Severe weather warning in your area.")
	if !strings.Contains(got, "This call is meant for 1 5 5 5 0 0 0 0 0 1 0.") {
		t.Fatalf("BuildTestVoiceBody() = %q, want intended phone prefix", got)
	}
	if !strings.Contains(got, "Severe weather warning in your area.") {
		t.Fatalf("BuildTestVoiceBody() = %q, want original body", got)
	}
}

func TestBuildCollapsedTestVoiceBodyPrefixesFirstRecipient(t *testing.T) {
	t.Parallel()

	provider := New(config.TwilioConfig{
		VoiceToOverride:         "+14073530340",
		VoiceOverrideSingleCall: true,
	})

	got := provider.BuildCollapsedTestVoiceBody("+15550000010", "Severe weather warning in your area.")
	if !strings.Contains(got, "This test call stands in for one or more intended recipients.") {
		t.Fatalf("BuildCollapsedTestVoiceBody() = %q, want collapsed-call prefix", got)
	}
	if !strings.Contains(got, "The first intended recipient is 1 5 5 5 0 0 0 0 0 1 0.") {
		t.Fatalf("BuildCollapsedTestVoiceBody() = %q, want first intended phone", got)
	}
}

func TestCollapseVoiceOverrideCallsRequiresOverrideAndFlag(t *testing.T) {
	t.Parallel()

	withOverride := New(config.TwilioConfig{
		VoiceToOverride:         "+14073530340",
		VoiceOverrideSingleCall: true,
	})
	if !withOverride.CollapseVoiceOverrideCalls() {
		t.Fatal("CollapseVoiceOverrideCalls() = false, want true")
	}

	withoutFlag := New(config.TwilioConfig{
		VoiceToOverride:         "+14073530340",
		VoiceOverrideSingleCall: false,
	})
	if withoutFlag.CollapseVoiceOverrideCalls() {
		t.Fatal("CollapseVoiceOverrideCalls() = true, want false when flag disabled")
	}
}

func TestVoiceFunctionAudioCodeMapsStatementUpdateToWarningFamily(t *testing.T) {
	t.Parallel()

	if got := VoiceFunctionAudioCode("SVS", "severe_thunderstorm_warning"); got != "SVR" {
		t.Fatalf("VoiceFunctionAudioCode(SVS, severe_thunderstorm_warning) = %q, want SVR", got)
	}
	if got := VoiceFunctionAudioCode("FLS", "flash_flood_warning"); got != "FFW" {
		t.Fatalf("VoiceFunctionAudioCode(FLS, flash_flood_warning) = %q, want FFW", got)
	}
	if got := VoiceFunctionAudioCode("TEST", ""); got != "TEST" {
		t.Fatalf("VoiceFunctionAudioCode(TEST, \"\") = %q, want TEST", got)
	}
}

func TestBuildVoiceFunctionURLUsesAccountIDAndAudio(t *testing.T) {
	t.Parallel()

	got, err := BuildVoiceFunctionURL("https://thundercall-2287.twil.io/welcome", "SVR", 42)
	if err != nil {
		t.Fatalf("BuildVoiceFunctionURL() error = %v", err)
	}
	want := "https://thundercall-2287.twil.io/welcome?audio=SVR&id=42"
	if got != want {
		t.Fatalf("BuildVoiceFunctionURL() = %q, want %q", got, want)
	}
}

func TestSendVoiceLogOnlyLogsFunctionBackedCallWhenConfigured(t *testing.T) {
	t.Parallel()

	provider := New(config.TwilioConfig{
		VoiceFrom:    "+18005551212",
		VoiceURL:     "https://thundercall-2287.twil.io/welcome",
		VoiceLogOnly: true,
	})

	var logged string
	provider.logf = func(format string, args ...any) {
		logged = fmt.Sprintf(format, args...)
	}

	_, err := provider.SendVoice(context.Background(), VoiceRequest{
		To:            "+14085550123",
		EventCode:     "SVS",
		AlertTypeCode: "severe_thunderstorm_warning",
		AccountID:     690,
	})
	if err != nil {
		t.Fatalf("SendVoice() error = %v", err)
	}

	if !strings.Contains(logged, "voice_url=\"https://thundercall-2287.twil.io/welcome?audio=SVR&id=690\"") {
		t.Fatalf("SendVoice() log = %q, want function URL with account id", logged)
	}
}

func TestIsRetryableErrorClassifiesTwilioRateLimitsAndServerErrors(t *testing.T) {
	t.Parallel()

	if !IsRetryableError(&twilioclient.TwilioRestError{Status: 429, Code: 20429, Message: "Too many requests"}) {
		t.Fatal("IsRetryableError(429) = false, want true")
	}
	if !IsRetryableError(&twilioclient.RestErrorV1{HttpStatusCode: 503, Code: 12345, Message: "Service unavailable"}) {
		t.Fatal("IsRetryableError(503) = false, want true")
	}
	if IsRetryableError(&twilioclient.TwilioRestError{Status: 400, Code: 21211, Message: "Invalid To phone number"}) {
		t.Fatal("IsRetryableError(400 invalid number) = true, want false")
	}
}
