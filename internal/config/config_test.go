package config

import (
	"testing"
	"time"
)

func TestNormalizedCSVValues(t *testing.T) {
	got := normalizedCSVValues([]string{" svr ", "FFW", "tor", "FFW", "", "npw"})
	want := []string{"SVR", "FFW", "TOR", "NPW"}

	if len(got) != len(want) {
		t.Fatalf("normalizedCSVValues() len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("normalizedCSVValues()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestDurationValueOrDefault(t *testing.T) {
	t.Setenv("THUNDERCALL_TEST_DURATION", "7m")

	got := durationValueOrDefault("THUNDERCALL_TEST_DURATION", time.Minute)
	if got != 7*time.Minute {
		t.Fatalf("durationValueOrDefault() = %v, want %v", got, 7*time.Minute)
	}
}

func TestLoadTwilioVoiceLogOnlyDefaultsTrue(t *testing.T) {
	t.Setenv("THUNDERCALL_TWILIO_VOICE_LOG_ONLY", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.Twilio.VoiceLogOnly {
		t.Fatalf("Load().Twilio.VoiceLogOnly = %v, want true", cfg.Twilio.VoiceLogOnly)
	}
}

func TestLoadTwilioVoiceLogOnlyHonorsEnv(t *testing.T) {
	t.Setenv("THUNDERCALL_TWILIO_VOICE_LOG_ONLY", "false")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Twilio.VoiceLogOnly {
		t.Fatalf("Load().Twilio.VoiceLogOnly = %v, want false", cfg.Twilio.VoiceLogOnly)
	}
}

func TestLoadTwilioVoiceOverrideHonorsEnv(t *testing.T) {
	t.Setenv("THUNDERCALL_TWILIO_VOICE_TO_OVERRIDE", "+14073530340")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Twilio.VoiceToOverride != "+14073530340" {
		t.Fatalf("Load().Twilio.VoiceToOverride = %q, want override number", cfg.Twilio.VoiceToOverride)
	}
}

func TestLoadTwilioVoiceURLHonorsEnv(t *testing.T) {
	t.Setenv("TWILIO_VOICE_URL", "https://thundercall-2287.twil.io/welcome")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Twilio.VoiceURL != "https://thundercall-2287.twil.io/welcome" {
		t.Fatalf("Load().Twilio.VoiceURL = %q, want voice function URL", cfg.Twilio.VoiceURL)
	}
}

func TestLoadTwilioVoiceOverrideSingleCallDefaultsTrue(t *testing.T) {
	t.Setenv("THUNDERCALL_TWILIO_VOICE_OVERRIDE_SINGLE_CALL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.Twilio.VoiceOverrideSingleCall {
		t.Fatalf("Load().Twilio.VoiceOverrideSingleCall = %v, want true", cfg.Twilio.VoiceOverrideSingleCall)
	}
}

func TestLoadTwilioVoiceOverrideSingleCallHonorsEnv(t *testing.T) {
	t.Setenv("THUNDERCALL_TWILIO_VOICE_OVERRIDE_SINGLE_CALL", "false")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Twilio.VoiceOverrideSingleCall {
		t.Fatalf("Load().Twilio.VoiceOverrideSingleCall = %v, want false", cfg.Twilio.VoiceOverrideSingleCall)
	}
}
