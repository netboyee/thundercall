package geocode

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

type weatherGovPointsResponse struct {
	Properties struct {
		ForecastZone string `json:"forecastZone"`
	} `json:"properties"`
}

func (s *Service) lookupForecastZone(ctx context.Context, latitude float64, longitude float64) (string, error) {
	if s == nil || s.client == nil {
		return "", fmt.Errorf("weather.gov client is not configured")
	}

	endpoint := fmt.Sprintf(
		"%s/points/%s,%s",
		strings.TrimRight(s.cfg.WeatherGovBaseURL, "/"),
		strconv.FormatFloat(latitude, 'f', 4, 64),
		strconv.FormatFloat(longitude, 'f', 4, 64),
	)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", s.cfg.UserAgent)
	request.Header.Set("Accept", "application/geo+json")

	response, err := s.client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusNotFound {
		return "", ErrNoMatch
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("request %s returned status %d", request.URL.String(), response.StatusCode)
	}

	var payload weatherGovPointsResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return "", err
	}

	zone := strings.TrimSpace(payload.Properties.ForecastZone)
	if zone == "" {
		return "", ErrNoMatch
	}
	parts := strings.Split(zone, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		value := strings.TrimSpace(parts[i])
		if value != "" {
			return strings.ToUpper(value), nil
		}
	}
	return "", ErrNoMatch
}
