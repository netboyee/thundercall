package nwws

import (
	"fmt"
	"math"
	"strings"
	"time"
)

func trimWhitespace(input string) string {
	return strings.Trim(input, "\n ")
}

func normalizeNewlines(input string) string {
	input = strings.ReplaceAll(input, "\r\n", "\n")
	input = strings.ReplaceAll(input, "\r", "\n")
	return input
}

func parseLine(message *string) string {
	newLine := strings.Index(*message, "\n")
	if newLine < 0 {
		line := *message
		*message = ""
		return line
	}

	line := (*message)[:newLine]
	*message = (*message)[newLine+1:]
	return line
}

func peekLine(message string) string {
	newLine := strings.Index(message, "\n")
	if newLine < 0 {
		return message
	}
	return message[:newLine]
}

func consumeMatchedPrefix(message *string, length int) {
	if length <= 0 {
		return
	}
	if length > len(*message) {
		length = len(*message)
	}
	*message = (*message)[length:]
	if strings.HasPrefix(*message, "\n") {
		*message = (*message)[1:]
	}
}

func parseDayHourMinute(raw string, reference time.Time) (time.Time, error) {
	if len(raw) != 6 {
		return time.Time{}, fmt.Errorf("invalid day-hour-minute %q", raw)
	}

	reference = reference.UTC()
	var day, hour, minute int
	if _, err := fmt.Sscanf(raw, "%2d%2d%2d", &day, &hour, &minute); err != nil {
		return time.Time{}, err
	}

	candidates := candidateDayHourMinuteTimes(reference, day, hour, minute)
	if len(candidates) == 0 {
		return time.Time{}, fmt.Errorf("day %02d is invalid around %s", day, reference.Format(time.RFC3339))
	}

	best := candidates[0]
	bestDistance := absDuration(best.Sub(reference))
	for _, candidate := range candidates[1:] {
		candidateDistance := absDuration(candidate.Sub(reference))
		if candidateDistance < bestDistance || (candidateDistance == bestDistance && candidate.After(reference) && !best.After(reference)) {
			best = candidate
			bestDistance = candidateDistance
		}
	}

	return best, nil
}

func parseVTECTime(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}

	value, err := time.Parse("060102T1504Z", raw)
	if err != nil {
		return time.Time{}
	}
	return value.UTC()
}

func wellKnownPolygon(points []Coordinate) string {
	if len(points) == 0 {
		return ""
	}

	points = closePolygonRing(points)

	var builder strings.Builder
	builder.WriteString("POLYGON ((")
	for i, point := range points {
		if i > 0 {
			builder.WriteString(",")
		}
		builder.WriteString(strconvFloat(point.Latitude))
		builder.WriteString(" ")
		builder.WriteString(strconvFloat(point.Longitude))
	}
	builder.WriteString("))")
	return builder.String()
}

func closePolygonRing(points []Coordinate) []Coordinate {
	if len(points) < 3 {
		return points
	}

	first := points[0]
	last := points[len(points)-1]
	if first == last {
		return points
	}

	closed := make([]Coordinate, 0, len(points)+1)
	closed = append(closed, points...)
	closed = append(closed, first)
	return closed
}

func strconvFloat(value float64) string {
	if math.Mod(value, 1) == 0 {
		return fmt.Sprintf("%.0f", value)
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.4f", value), "0"), ".")
}

func candidateDayHourMinuteTimes(reference time.Time, day int, hour int, minute int) []time.Time {
	baseMonth := time.Date(reference.Year(), reference.Month(), 1, 0, 0, 0, 0, time.UTC)
	monthReferences := []time.Time{
		baseMonth.AddDate(0, -1, 0),
		baseMonth,
		baseMonth.AddDate(0, 1, 0),
	}

	candidates := make([]time.Time, 0, len(monthReferences))
	for _, monthReference := range monthReferences {
		year, month := monthReference.Year(), monthReference.Month()
		if day < 1 || day > daysInMonth(year, month) {
			continue
		}
		candidates = append(candidates, time.Date(year, month, day, hour, minute, 0, 0, time.UTC))
	}

	return candidates
}

func daysInMonth(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

func absDuration(duration time.Duration) time.Duration {
	if duration < 0 {
		return -duration
	}
	return duration
}
