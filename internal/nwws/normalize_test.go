package nwws

import (
	"testing"
	"time"
)

func TestNormalizeCarriesSinglePrimaryVTECMetadata(t *testing.T) {
	t.Parallel()

	parser := NewParser()
	raw := readFixture(t, "examples_01_SVRDMX.txt")

	parsed, err := parser.Parse(raw, time.Date(2026, 8, 15, 21, 18, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	requests := Normalize(parsed, 44, "source-44")
	if len(requests) != 1 {
		t.Fatalf("Normalize() count = %d, want 1", len(requests))
	}

	request := requests[0]
	if request.PrimaryVTECCount != 1 {
		t.Fatalf("PrimaryVTECCount = %d, want 1", request.PrimaryVTECCount)
	}
	if request.VTECProductClass != "O" {
		t.Fatalf("VTECProductClass = %q, want O", request.VTECProductClass)
	}
	if request.VTECAction != "NEW" {
		t.Fatalf("VTECAction = %q, want NEW", request.VTECAction)
	}
	if request.VTECOfficeID != "KDMX" {
		t.Fatalf("VTECOfficeID = %q, want KDMX", request.VTECOfficeID)
	}
	if request.VTECPhenomenon != "SV" || request.VTECSignificance != "W" || request.VTECETN != "0123" {
		t.Fatalf("unexpected VTEC identity %#v", request)
	}
	if request.AlertEventCode() != "SVW" {
		t.Fatalf("AlertEventCode() = %q, want SVW", request.AlertEventCode())
	}
	if request.ConfiguredProductCode() != "SVR" {
		t.Fatalf("ConfiguredProductCode() = %q, want SVR", request.ConfiguredProductCode())
	}
	if request.PrimaryVTECRaw == "" {
		t.Fatal("PrimaryVTECRaw = empty, want populated raw VTEC")
	}
}

func TestNormalizeLeavesSingleEventFieldsEmptyForMultiplePrimaryVTECs(t *testing.T) {
	t.Parallel()

	parsed := ParsedMessage{
		WMOHeader:       WMOHeader{IssuedAt: time.Date(2026, 6, 24, 12, 30, 0, 0, time.UTC)},
		AWIPSIdentifier: AWIPSIdentifier{ProductCategory: "FFA"},
		Segments: []Segment{
			{
				Header: SegmentHeader{
					PrimaryVTECs: []PrimaryVTEC{
						{Raw: "/O.CON.KSLC.FF.A.0007.110922T1200Z-110923T0000Z/"},
						{Raw: "/O.NEW.KSLC.FA.Y.0030.110922T2305Z-110923T0200Z/"},
					},
					PrimaryVTEC: PrimaryVTEC{Raw: "/O.CON.KSLC.FF.A.0007.110922T1200Z-110923T0000Z/"},
				},
				Message: "Body",
			},
		},
		Original: "raw",
	}

	requests := Normalize(parsed, 88, "source-88")
	if len(requests) != 1 {
		t.Fatalf("Normalize() count = %d, want 1", len(requests))
	}

	request := requests[0]
	if request.PrimaryVTECCount != 2 {
		t.Fatalf("PrimaryVTECCount = %d, want 2", request.PrimaryVTECCount)
	}
	if request.PrimaryVTECRaw != "" {
		t.Fatalf("PrimaryVTECRaw = %q, want empty for multi-VTEC segment", request.PrimaryVTECRaw)
	}
	if request.VTECOfficeID != "" || request.VTECETN != "" {
		t.Fatalf("unexpected single-event VTEC metadata %#v", request)
	}
}
