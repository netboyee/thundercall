package geocode

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"thundercall-go/internal/config"
)

var ErrNoMatch = errors.New("no matching location found")

type Address struct {
	Line1      string
	Line2      string
	City       string
	StateCode  string
	PostalCode string
}

func (a Address) OneLine() string {
	parts := []string{
		strings.TrimSpace(a.Line1),
		strings.TrimSpace(a.Line2),
		strings.TrimSpace(a.City),
		strings.TrimSpace(a.StateCode),
		strings.TrimSpace(a.PostalCode),
	}

	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return strings.Join(out, ", ")
}

type ResolvedLocation struct {
	MatchedAddress string
	Latitude       float64
	Longitude      float64
	CountyFIPS     string
	NWSZone        string
}

type Resolver interface {
	ResolveAddress(ctx context.Context, address Address) (ResolvedLocation, error)
	ResolveCoordinates(ctx context.Context, latitude float64, longitude float64) (ResolvedLocation, error)
}

type Service struct {
	client *http.Client
	cfg    config.GeocodingConfig
}

func New(cfg config.GeocodingConfig) *Service {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	if strings.TrimSpace(cfg.CensusBaseURL) == "" {
		cfg.CensusBaseURL = "https://geocoding.geo.census.gov/geocoder"
	}
	if strings.TrimSpace(cfg.CensusBenchmark) == "" {
		cfg.CensusBenchmark = "Public_AR_Current"
	}
	if strings.TrimSpace(cfg.CensusVintage) == "" {
		cfg.CensusVintage = "Current_Current"
	}
	if strings.TrimSpace(cfg.WeatherGovBaseURL) == "" {
		cfg.WeatherGovBaseURL = "https://api.weather.gov"
	}
	if strings.TrimSpace(cfg.UserAgent) == "" {
		cfg.UserAgent = "thundercall/0.1"
	}

	return &Service{
		client: &http.Client{Timeout: timeout},
		cfg:    cfg,
	}
}

func (s *Service) ResolveAddress(ctx context.Context, address Address) (ResolvedLocation, error) {
	match, err := s.lookupAddress(ctx, address)
	if err != nil {
		return ResolvedLocation{}, err
	}

	resolved := ResolvedLocation{
		MatchedAddress: match.MatchedAddress,
		Latitude:       match.Latitude,
		Longitude:      match.Longitude,
		CountyFIPS:     match.CountyFIPS,
	}

	zone, err := s.lookupForecastZone(ctx, resolved.Latitude, resolved.Longitude)
	if err == nil {
		resolved.NWSZone = zone
	}
	return resolved, nil
}

func (s *Service) ResolveCoordinates(ctx context.Context, latitude float64, longitude float64) (ResolvedLocation, error) {
	countyFIPS, err := s.lookupCountyByCoordinates(ctx, latitude, longitude)
	if err != nil {
		if !errors.Is(err, ErrNoMatch) {
			return ResolvedLocation{}, err
		}
	}

	resolved := ResolvedLocation{
		Latitude:   latitude,
		Longitude:  longitude,
		CountyFIPS: countyFIPS,
	}

	zone, err := s.lookupForecastZone(ctx, latitude, longitude)
	if err == nil {
		resolved.NWSZone = zone
	}
	return resolved, nil
}
