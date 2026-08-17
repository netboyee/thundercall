package nwws

import "testing"

func TestWellKnownPolygonClosesOpenRing(t *testing.T) {
	polygon := wellKnownPolygon([]Coordinate{
		{Latitude: 39.79, Longitude: -85.30},
		{Latitude: 39.79, Longitude: -85.22},
		{Latitude: 39.87, Longitude: -85.22},
	})

	const want = "POLYGON ((39.79 -85.3,39.79 -85.22,39.87 -85.22,39.79 -85.3))"
	if polygon != want {
		t.Fatalf("wellKnownPolygon() = %q, want %q", polygon, want)
	}
}

func TestWellKnownPolygonDoesNotDuplicateClosedRing(t *testing.T) {
	polygon := wellKnownPolygon([]Coordinate{
		{Latitude: 39.79, Longitude: -85.30},
		{Latitude: 39.79, Longitude: -85.22},
		{Latitude: 39.87, Longitude: -85.22},
		{Latitude: 39.79, Longitude: -85.30},
	})

	const want = "POLYGON ((39.79 -85.3,39.79 -85.22,39.87 -85.22,39.79 -85.3))"
	if polygon != want {
		t.Fatalf("wellKnownPolygon() = %q, want %q", polygon, want)
	}
}
