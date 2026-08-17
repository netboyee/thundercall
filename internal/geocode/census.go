package geocode

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type censusAddressMatch struct {
	MatchedAddress string                      `json:"matchedAddress"`
	Coordinates    censusCoordinates           `json:"coordinates"`
	Geographies    map[string][]map[string]any `json:"geographies"`
}

type censusCoordinates struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type censusAddressResponse struct {
	Result struct {
		AddressMatches []censusAddressMatch `json:"addressMatches"`
	} `json:"result"`
}

type censusCoordinatesResponse struct {
	Result struct {
		Geographies map[string][]map[string]any `json:"geographies"`
	} `json:"result"`
}

func (s *Service) lookupAddress(ctx context.Context, address Address) (ResolvedLocation, error) {
	if s == nil || s.client == nil {
		return ResolvedLocation{}, fmt.Errorf("geocoder is not configured")
	}

	query := url.Values{}
	query.Set("address", address.OneLine())
	query.Set("benchmark", s.cfg.CensusBenchmark)
	query.Set("vintage", s.cfg.CensusVintage)
	query.Set("format", "json")

	endpoint := strings.TrimRight(s.cfg.CensusBaseURL, "/") + "/geographies/onelineaddress?" + query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ResolvedLocation{}, err
	}

	var response censusAddressResponse
	if err := s.doJSON(request, &response); err != nil {
		return ResolvedLocation{}, err
	}
	if len(response.Result.AddressMatches) == 0 {
		return ResolvedLocation{}, ErrNoMatch
	}

	match := response.Result.AddressMatches[0]
	countyFIPS, err := countyFIPSFromGeographies(match.Geographies)
	if err != nil && !errorsIsNoMatch(err) {
		return ResolvedLocation{}, err
	}

	return ResolvedLocation{
		MatchedAddress: strings.TrimSpace(match.MatchedAddress),
		Latitude:       match.Coordinates.Y,
		Longitude:      match.Coordinates.X,
		CountyFIPS:     countyFIPS,
	}, nil
}

func (s *Service) lookupCountyByCoordinates(ctx context.Context, latitude float64, longitude float64) (string, error) {
	if s == nil || s.client == nil {
		return "", fmt.Errorf("geocoder is not configured")
	}

	query := url.Values{}
	query.Set("x", strconv.FormatFloat(longitude, 'f', 7, 64))
	query.Set("y", strconv.FormatFloat(latitude, 'f', 7, 64))
	query.Set("benchmark", s.cfg.CensusBenchmark)
	query.Set("vintage", s.cfg.CensusVintage)
	query.Set("format", "json")

	endpoint := strings.TrimRight(s.cfg.CensusBaseURL, "/") + "/geographies/coordinates?" + query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}

	var response censusCoordinatesResponse
	if err := s.doJSON(request, &response); err != nil {
		return "", err
	}
	return countyFIPSFromGeographies(response.Result.Geographies)
}

func (s *Service) doJSON(request *http.Request, dest any) error {
	response, err := s.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusNotFound {
		return ErrNoMatch
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("request %s returned status %d", request.URL.String(), response.StatusCode)
	}

	return json.NewDecoder(response.Body).Decode(dest)
}

func countyFIPSFromGeographies(geographies map[string][]map[string]any) (string, error) {
	for key, entries := range geographies {
		if !strings.EqualFold(strings.TrimSpace(key), "Counties") {
			continue
		}
		for _, entry := range entries {
			state := strings.TrimSpace(anyString(entry["STATE"]))
			county := strings.TrimSpace(anyString(entry["COUNTY"]))
			if state == "" || county == "" {
				continue
			}

			stateCode, ok := stateFIPSToUSPS[state]
			if !ok {
				return "", fmt.Errorf("unknown state FIPS code %q", state)
			}
			return strings.ToUpper(stateCode + "C" + county), nil
		}
	}
	return "", ErrNoMatch
}

func anyString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return fmt.Sprint(v)
	}
}

func errorsIsNoMatch(err error) bool {
	return err != nil && err == ErrNoMatch
}
