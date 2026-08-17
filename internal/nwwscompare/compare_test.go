package nwwscompare

import (
	"testing"
	"time"
)

func TestNormalizeMessageDerivesAWIPSAndVTEC(t *testing.T) {
	msg := Message{
		Body: "\nWUUS53 KJKL 111745\n\nSVRJKL\n\nKYC159-195-111815-\n\n/O.NEW.KJKL.SV.W.0095.260811T1745Z-260811T1815Z/\n\nBULLETIN - IMMEDIATE BROADCAST REQUESTED\n",
	}

	normalizeMessage(&msg)

	if msg.AWIPSID != "SVRJKL" {
		t.Fatalf("expected AWIPS ID SVRJKL, got %q", msg.AWIPSID)
	}
	if msg.EventCode != "SVR" {
		t.Fatalf("expected event code SVR, got %q", msg.EventCode)
	}
	if msg.VTEC != "/O.NEW.KJKL.SV.W.0095.260811T1745Z-260811T1815Z/" {
		t.Fatalf("unexpected VTEC: %q", msg.VTEC)
	}
}

func TestNormalizeListParsesJSONAndCSV(t *testing.T) {
	values := normalizeList([]string{`["ohc023", "ohc057"]`, "ohc023, ohc113", " OHC057 "})

	expected := []string{"OHC023", "OHC057", "OHC113"}
	if len(values) != len(expected) {
		t.Fatalf("expected %d values, got %d (%v)", len(expected), len(values), values)
	}
	for i := range expected {
		if values[i] != expected[i] {
			t.Fatalf("expected %v, got %v", expected, values)
		}
	}
}

func TestBuildReportMatchesEquivalentMessages(t *testing.T) {
	now := time.Date(2026, time.August, 11, 18, 19, 37, 0, time.UTC)
	goMessages := []Message{{
		System:     "go",
		ID:         "3",
		EventCode:  "SVR",
		AWIPSID:    "SVRJKL",
		VTEC:       "/O.NEW.KJKL.SV.W.0095.260811T1745Z-260811T1815Z/",
		PolygonWKT: "POLYGON ((37.5 -82.7,37.54 -82.55,37.34 -82.41,37.29 -82.7))",
		FIPSCodes:  []string{"KYC071", "KYC195"},
		ReceivedAt: now,
	}}
	legacyMessages := []Message{{
		System:     "legacy",
		ID:         "215833",
		Body:       "SVRJKL\n/O.NEW.KJKL.SV.W.0095.260811T1745Z-260811T1815Z/",
		PolygonWKT: "POLYGON ((37.5  -82.7, 37.54 -82.55, 37.34 -82.41, 37.29 -82.7))",
		FIPSCodes:  []string{"KYC195,KYC071"},
		ReceivedAt: now.Add(5 * time.Second),
	}}

	report := BuildReport(now.Add(-time.Minute), now.Add(time.Minute), goMessages, legacyMessages)

	if report.MatchedCount != 1 {
		t.Fatalf("expected 1 match, got %d", report.MatchedCount)
	}
	if len(report.OnlyInGo) != 0 {
		t.Fatalf("expected no Go-only rows, got %d", len(report.OnlyInGo))
	}
	if len(report.OnlyInLegacy) != 0 {
		t.Fatalf("expected no legacy-only rows, got %d", len(report.OnlyInLegacy))
	}
}
