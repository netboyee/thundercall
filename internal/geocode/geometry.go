package geocode

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

type point struct {
	latitude  float64
	longitude float64
}

func PointWKT(latitude float64, longitude float64) string {
	return fmt.Sprintf("POINT (%s %s)", formatFloat(latitude), formatFloat(longitude))
}

func PolygonContainsPoint(wkt string, latitude float64, longitude float64) (bool, error) {
	points, err := parsePolygonWKT(wkt)
	if err != nil {
		return false, err
	}
	if len(points) < 4 {
		return false, fmt.Errorf("polygon must have at least four points")
	}

	target := point{latitude: latitude, longitude: longitude}
	if pointOnAnySegment(points, target) {
		return true, nil
	}

	inside := false
	j := len(points) - 1
	for i := 0; i < len(points); i++ {
		a := points[i]
		b := points[j]
		intersects := ((a.longitude > target.longitude) != (b.longitude > target.longitude)) &&
			(target.latitude < (b.latitude-a.latitude)*(target.longitude-a.longitude)/(b.longitude-a.longitude)+a.latitude)
		if intersects {
			inside = !inside
		}
		j = i
	}
	return inside, nil
}

func parsePolygonWKT(wkt string) ([]point, error) {
	raw := strings.TrimSpace(wkt)
	if raw == "" {
		return nil, fmt.Errorf("polygon WKT is required")
	}

	upper := strings.ToUpper(raw)
	if !strings.HasPrefix(upper, "POLYGON") {
		return nil, fmt.Errorf("unsupported geometry %q", raw)
	}

	start := strings.Index(raw, "((")
	end := strings.LastIndex(raw, "))")
	if start < 0 || end <= start+1 {
		return nil, fmt.Errorf("invalid polygon WKT %q", raw)
	}

	ring := raw[start+2 : end]
	if hole := strings.Index(ring, "),("); hole >= 0 {
		ring = ring[:hole]
	}

	parts := strings.Split(ring, ",")
	points := make([]point, 0, len(parts)+1)
	for _, part := range parts {
		fields := strings.Fields(strings.TrimSpace(part))
		if len(fields) < 2 {
			return nil, fmt.Errorf("invalid polygon point %q", part)
		}

		latitude, err := strconv.ParseFloat(fields[0], 64)
		if err != nil {
			return nil, fmt.Errorf("parse polygon latitude %q: %w", fields[0], err)
		}
		longitude, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			return nil, fmt.Errorf("parse polygon longitude %q: %w", fields[1], err)
		}

		points = append(points, point{latitude: latitude, longitude: longitude})
	}

	if len(points) >= 3 && !samePoint(points[0], points[len(points)-1]) {
		points = append(points, points[0])
	}
	return points, nil
}

func pointOnAnySegment(points []point, target point) bool {
	for i := 0; i < len(points)-1; i++ {
		if pointOnSegment(points[i], points[i+1], target) {
			return true
		}
	}
	return false
}

func pointOnSegment(a point, b point, target point) bool {
	cross := (target.longitude-a.longitude)*(b.latitude-a.latitude) - (target.latitude-a.latitude)*(b.longitude-a.longitude)
	if math.Abs(cross) > 1e-9 {
		return false
	}

	dot := (target.latitude-a.latitude)*(b.latitude-a.latitude) + (target.longitude-a.longitude)*(b.longitude-a.longitude)
	if dot < 0 {
		return false
	}

	lengthSquared := (b.latitude-a.latitude)*(b.latitude-a.latitude) + (b.longitude-a.longitude)*(b.longitude-a.longitude)
	return dot <= lengthSquared
}

func samePoint(a point, b point) bool {
	return math.Abs(a.latitude-b.latitude) < 1e-9 && math.Abs(a.longitude-b.longitude) < 1e-9
}

func formatFloat(value float64) string {
	if math.Mod(value, 1) == 0 {
		return fmt.Sprintf("%.0f", value)
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.7f", value), "0"), ".")
}
