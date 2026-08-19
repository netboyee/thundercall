package ingest

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"thundercall-go/internal/nwws"
)

func TestProcessEnvelopeSkipsUnconfiguredProductsBeforePersistence(t *testing.T) {
	service := NewService(nil, "thundercall:messages", []string{"SVR", "FFW"})

	result, err := service.ProcessEnvelope(context.Background(), nwws.StanzaEnvelope{
		AWIPSID:    "RWRMO",
		ExternalID: "skip-me",
		Body:       "summary only",
	})
	if err != nil {
		t.Fatalf("ProcessEnvelope() error = %v", err)
	}
	if result.IgnoredCount != 1 {
		t.Fatalf("IgnoredCount = %d, want 1", result.IgnoredCount)
	}
	if result.SourceMessageID != 0 {
		t.Fatalf("SourceMessageID = %d, want 0", result.SourceMessageID)
	}
	if result.Duplicate {
		t.Fatalf("Duplicate = true, want false")
	}
}

func TestProcessEnvelopeSkipsUncategorizedSummaryMessagesBeforePersistence(t *testing.T) {
	service := NewService(nil, "thundercall:messages", []string{"SVR", "FFW", "TOR"})

	result, err := service.ProcessEnvelope(context.Background(), nwws.StanzaEnvelope{
		WMOCode:    "WOUS99",
		CCCCode:    "KNCF",
		ExternalID: "summary-only",
		Body:       "KNCF issued, valid 2026-08-11T17:21:00Z",
	})
	if err != nil {
		t.Fatalf("ProcessEnvelope() error = %v", err)
	}
	if result.IgnoredCount != 1 {
		t.Fatalf("IgnoredCount = %d, want 1", result.IgnoredCount)
	}
	if result.SourceMessageID != 0 {
		t.Fatalf("SourceMessageID = %d, want 0", result.SourceMessageID)
	}
}

func TestProcessEnvelopeSkipsConfiguredSummaryMessagesWithoutSegmentsBeforePersistence(t *testing.T) {
	service := NewService(nil, "thundercall:messages", []string{"SVR"})

	result, err := service.ProcessEnvelope(context.Background(), nwws.StanzaEnvelope{
		AWIPSID:    "SVRDMX",
		ExternalID: "summary-only-configured",
		Body:       "KDMX issues Severe Thunderstorm Warning (SVR) valid 2026-08-11T17:54:00Z",
	})
	if err != nil {
		t.Fatalf("ProcessEnvelope() error = %v", err)
	}
	if result.IgnoredCount != 1 {
		t.Fatalf("IgnoredCount = %d, want 1", result.IgnoredCount)
	}
	if result.SourceMessageID != 0 {
		t.Fatalf("SourceMessageID = %d, want 0", result.SourceMessageID)
	}
}

func TestProcessEnvelopeStillRequiresDatabaseForConfiguredBulletins(t *testing.T) {
	service := NewService(nil, "thundercall:messages", []string{"SVR"})

	bodyBytes, err := os.ReadFile(filepath.Join("..", "nwws", "testdata", "examples_01_SVRDMX.txt"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	_, err = service.ProcessEnvelope(context.Background(), nwws.StanzaEnvelope{
		AWIPSID:    "SVRDMX",
		ExternalID: "allowed",
		Body:       string(bodyBytes),
	})
	if err == nil {
		t.Fatalf("ProcessEnvelope() error = nil, want database is required")
	}
}

func TestProcessEnvelopeSkipsStatementUpdatesWhenRawProductIsUnconfigured(t *testing.T) {
	service := NewService(nil, "thundercall:messages", []string{"SVR"})

	bodyBytes, err := os.ReadFile(filepath.Join("..", "nwws", "testdata", "examples_03_SVSDMX.txt"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	result, err := service.ProcessEnvelope(context.Background(), nwws.StanzaEnvelope{
		AWIPSID:    "SVSDMX",
		ExternalID: "statement-update",
		Body:       string(bodyBytes),
	})
	if err != nil {
		t.Fatalf("ProcessEnvelope() error = %v", err)
	}
	if result.IgnoredCount != 1 {
		t.Fatalf("IgnoredCount = %d, want 1", result.IgnoredCount)
	}
	if result.SourceMessageID != 0 {
		t.Fatalf("SourceMessageID = %d, want 0", result.SourceMessageID)
	}
}

func TestRequestAllowedMatchesOnlyRawConfiguredProduct(t *testing.T) {
	service := NewService(nil, "thundercall:messages", []string{"SVR"})

	if !service.requestAllowed("SVR") {
		t.Fatal("requestAllowed(SVR) = false, want true")
	}
	if service.requestAllowed("SVS") {
		t.Fatal("requestAllowed(SVS) = true, want false when only the raw SVR product is configured")
	}
	if service.requestAllowed("FLS") {
		t.Fatal("requestAllowed(FLS) = true, want false")
	}
}

func TestSkipUnconfiguredProduct(t *testing.T) {
	service := NewService(nil, "thundercall:messages", []string{"SVR", "FFW"})

	if !service.skipUnconfiguredProduct("RWR") {
		t.Fatalf("skipUnconfiguredProduct(RWR) = false, want true")
	}
	if service.skipUnconfiguredProduct("SVR") {
		t.Fatalf("skipUnconfiguredProduct(SVR) = true, want false")
	}
	if service.skipUnconfiguredProduct("") {
		t.Fatalf("skipUnconfiguredProduct(\"\") = true, want false")
	}
}

func TestFirstNonEmpty(t *testing.T) {
	got := firstNonEmpty("", "  ", "SVR", "FFW")
	if got != "SVR" {
		t.Fatalf("firstNonEmpty() = %q, want SVR", got)
	}
}
