package geocode

import "testing"

func TestPolygonContainsPointClosesOpenRing(t *testing.T) {
	inside, err := PolygonContainsPoint("POLYGON ((39.71 -85.1,39.83 -85.12,39.83 -85.22,39.87 -85.22))", 39.80, -85.16)
	if err != nil {
		t.Fatalf("PolygonContainsPoint() error = %v", err)
	}
	if !inside {
		t.Fatal("expected point to be inside polygon")
	}
}

func TestPointWKT(t *testing.T) {
	if got := PointWKT(29.6516, -82.3248); got != "POINT (29.6516 -82.3248)" {
		t.Fatalf("PointWKT() = %q", got)
	}
}
