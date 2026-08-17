package httpapi

import (
	"testing"

	"thundercall-go/internal/geocode"
)

func TestResolveLookupRequestRejectsAmbiguousInput(t *testing.T) {
	server := &Server{}

	_, err := server.resolveLookupRequest(t.Context(), locationLookupRequest{})
	if err == nil {
		t.Fatal("expected empty lookup request to fail")
	}
}

func TestLocationMatchReasons(t *testing.T) {
	polygon := "POLYGON ((39.71 -85.1,39.83 -85.12,39.83 -85.22,39.87 -85.22,39.71 -85.1))"
	item := locationMessageLookupItem{
		FIPSCodes:  []string{"INC041"},
		NWSZones:   []string{"INZ047"},
		PolygonWKT: &polygon,
	}

	reasons := locationMatchReasons(item, geocode.ResolvedLocation{
		Latitude:   39.80,
		Longitude:  -85.16,
		CountyFIPS: "INC041",
		NWSZone:    "INZ047",
	})

	if len(reasons) != 3 {
		t.Fatalf("expected 3 match reasons, got %v", reasons)
	}
}
