package thundercall

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"sort"
	"strings"
)

var polygonPattern = regexp.MustCompile(`(?i)POLYGON\s*\(\(\s*(.*?)\s*\)\)`)

func GenerateFingerprint(messageType string, polygonWKT string, fipsCodes []string, nwsZones []string) string {
	normalizedType := strings.ToLower(strings.TrimSpace(messageType))
	normalizedPolygon := normalizePolygon(polygonWKT)
	normalizedFIPS := normalizeValues(fipsCodes)
	normalizedZones := normalizeValues(nwsZones)

	payload := normalizedType + "|" + normalizedPolygon + "|" + normalizedFIPS + "|" + normalizedZones
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

func normalizePolygon(wkt string) string {
	wkt = strings.TrimSpace(wkt)
	if wkt == "" {
		return ""
	}

	match := polygonPattern.FindStringSubmatch(wkt)
	if len(match) != 2 {
		return strings.ToLower(wkt)
	}

	points := strings.Split(match[1], ",")
	normalized := make([]string, 0, len(points))
	for _, point := range points {
		normalized = append(normalized, strings.ToLower(strings.TrimSpace(point)))
	}

	sort.Strings(normalized)
	return "polygon((" + strings.Join(normalized, ", ") + "))"
}

func normalizeValues(values []string) string {
	if len(values) == 0 {
		return ""
	}

	normalized := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			normalized = append(normalized, value)
		}
	}

	sort.Strings(normalized)
	return strings.Join(normalized, ",")
}
