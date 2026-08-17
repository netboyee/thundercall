package ingest

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"thundercall-go/internal/nwws"
	"thundercall-go/internal/thundercall"
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

func TestProcessEnvelopeAllowsConfiguredStatementUpdatesViaPrimaryVTEC(t *testing.T) {
	service := NewService(nil, "thundercall:messages", []string{"SVR"})

	bodyBytes, err := os.ReadFile(filepath.Join("..", "nwws", "testdata", "examples_03_SVSDMX.txt"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	_, err = service.ProcessEnvelope(context.Background(), nwws.StanzaEnvelope{
		AWIPSID:    "SVSDMX",
		ExternalID: "statement-update",
		Body:       string(bodyBytes),
	})
	if err == nil {
		t.Fatalf("ProcessEnvelope() error = nil, want database is required")
	}
}

func TestRequestAllowedUsesUnderlyingAlertFamilyForStatementProducts(t *testing.T) {
	service := NewService(nil, "thundercall:messages", []string{"SVR"})

	allowed := service.requestAllowed(thundercall.IncomingMessageRequest{
		MessageSource:    "NWWS",
		MessageEvent:     "SVS",
		PrimaryVTECCount: 1,
		VTECProductClass: "O",
		VTECOfficeID:     "KDMX",
		VTECPhenomenon:   "SV",
		VTECSignificance: "W",
		VTECETN:          "0123",
	}, "SVS")
	if !allowed {
		t.Fatal("requestAllowed() = false, want true for SVS wrapping an allowed severe thunderstorm warning")
	}

	disallowed := service.requestAllowed(thundercall.IncomingMessageRequest{
		MessageSource:    "NWWS",
		MessageEvent:     "FLS",
		PrimaryVTECCount: 1,
		VTECProductClass: "O",
		VTECOfficeID:     "KDVN",
		VTECPhenomenon:   "FL",
		VTECSignificance: "W",
		VTECETN:          "0012",
	}, "FLS")
	if disallowed {
		t.Fatal("requestAllowed() = true, want false when the underlying alert family is not configured")
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
