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
