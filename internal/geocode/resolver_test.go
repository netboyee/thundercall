package geocode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"thundercall-go/internal/config"
)

func TestResolveAddress(t *testing.T) {
	census := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/geographies/onelineaddress") {
			t.Fatalf("unexpected census path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{
			"result": {
				"addressMatches": [{
					"matchedAddress": "4600 SILVER HILL RD, WASHINGTON, DC, 20233",
					"coordinates": {"x": -76.9274, "y": 38.8460},
					"geographies": {
						"Counties": [{
							"STATE": "24",
							"COUNTY": "033"
						}]
					}
				}]
			}
		}`))
	}))
	defer census.Close()

	weatherGov := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "thundercall-test" {
			t.Fatalf("unexpected user agent %q", r.Header.Get("User-Agent"))
		}
		_, _ = w.Write([]byte(`{
			"properties": {
				"forecastZone": "https://api.weather.gov/zones/forecast/MDZ013"
			}
		}`))
	}))
	defer weatherGov.Close()

	resolver := New(config.GeocodingConfig{
		CensusBaseURL:     census.URL,
		CensusBenchmark:   "Public_AR_Current",
		CensusVintage:     "Current_Current",
		WeatherGovBaseURL: weatherGov.URL,
		UserAgent:         "thundercall-test",
	})

	resolved, err := resolver.ResolveAddress(context.Background(), Address{
		Line1:      "4600 Silver Hill Rd",
		City:       "Washington",
		StateCode:  "DC",
		PostalCode: "20233",
	})
	if err != nil {
		t.Fatalf("ResolveAddress() error = %v", err)
	}

	if resolved.Latitude != 38.8460 || resolved.Longitude != -76.9274 {
		t.Fatalf("unexpected coordinates %+v", resolved)
	}
	if resolved.CountyFIPS != "MDC033" {
		t.Fatalf("CountyFIPS = %q, want MDC033", resolved.CountyFIPS)
	}
	if resolved.NWSZone != "MDZ013" {
		t.Fatalf("NWSZone = %q, want MDZ013", resolved.NWSZone)
	}
}

func TestResolveCoordinates(t *testing.T) {
	census := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/geographies/coordinates") {
			t.Fatalf("unexpected census path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{
			"result": {
				"geographies": {
					"Counties": [{
						"STATE": "12",
						"COUNTY": "035"
					}]
				}
			}
		}`))
	}))
	defer census.Close()

	weatherGov := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"properties": {
				"forecastZone": "https://api.weather.gov/zones/forecast/FLZ038"
			}
		}`))
	}))
	defer weatherGov.Close()

	resolver := New(config.GeocodingConfig{
		CensusBaseURL:     census.URL,
		CensusBenchmark:   "Public_AR_Current",
		CensusVintage:     "Current_Current",
		WeatherGovBaseURL: weatherGov.URL,
		UserAgent:         "thundercall-test",
	})

	resolved, err := resolver.ResolveCoordinates(context.Background(), 29.6516, -82.3248)
	if err != nil {
		t.Fatalf("ResolveCoordinates() error = %v", err)
	}

	if resolved.CountyFIPS != "FLC035" {
		t.Fatalf("CountyFIPS = %q, want FLC035", resolved.CountyFIPS)
	}
	if resolved.NWSZone != "FLZ038" {
		t.Fatalf("NWSZone = %q, want FLZ038", resolved.NWSZone)
	}
}
