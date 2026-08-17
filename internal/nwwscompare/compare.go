package nwwscompare

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
)

type Message struct {
	System      string
	ID          string
	EventCode   string
	AWIPSID     string
	MessageType string
	VTEC        string
	Coordinate  string
	PolygonWKT  string
	FIPSCodes   []string
	NWSZones    []string
	Body        string
	Original    string
	ReceivedAt  time.Time
}

type Report struct {
	Since        time.Time
	Until        time.Time
	GoCount      int
	LegacyCount  int
	MatchedCount int
	OnlyInGo     []Message
	OnlyInLegacy []Message
}

var awipsPattern = regexp.MustCompile(`(?m)^[A-Z0-9]{6}$`)
var polygonNumberPattern = regexp.MustCompile(`-?\d+(?:\.\d+)?`)

func BuildReport(since, until time.Time, goMessages, legacyMessages []Message) Report {
	normalizedGo := cloneAndNormalize(goMessages)
	normalizedLegacy := cloneAndNormalize(legacyMessages)

	legacyBySignature := make(map[string][]Message, len(normalizedLegacy))
	for _, msg := range normalizedLegacy {
		signature := buildSignature(msg)
		legacyBySignature[signature] = append(legacyBySignature[signature], msg)
	}

	report := Report{
		Since:       since,
		Until:       until,
		GoCount:     len(normalizedGo),
		LegacyCount: len(normalizedLegacy),
	}

	for _, msg := range normalizedGo {
		signature := buildSignature(msg)
		queue := legacyBySignature[signature]
		if len(queue) == 0 {
			report.OnlyInGo = append(report.OnlyInGo, msg)
			continue
		}

		report.MatchedCount++
		if len(queue) == 1 {
			delete(legacyBySignature, signature)
		} else {
			legacyBySignature[signature] = queue[1:]
		}
	}

	for _, queue := range legacyBySignature {
		report.OnlyInLegacy = append(report.OnlyInLegacy, queue...)
	}

	slices.SortFunc(report.OnlyInGo, func(a, b Message) int {
		return a.ReceivedAt.Compare(b.ReceivedAt)
	})
	slices.SortFunc(report.OnlyInLegacy, func(a, b Message) int {
		return a.ReceivedAt.Compare(b.ReceivedAt)
	})

	return report
}

func cloneAndNormalize(messages []Message) []Message {
	out := make([]Message, 0, len(messages))
	for _, msg := range messages {
		normalized := msg
		normalizeMessage(&normalized)
		out = append(out, normalized)
	}
	return out
}

func normalizeMessage(msg *Message) {
	msg.System = strings.TrimSpace(msg.System)
	msg.ID = strings.TrimSpace(msg.ID)
	msg.AWIPSID = strings.ToUpper(strings.TrimSpace(msg.AWIPSID))
	msg.EventCode = strings.ToUpper(strings.TrimSpace(msg.EventCode))
	msg.MessageType = strings.TrimSpace(msg.MessageType)
	msg.Coordinate = squashWhitespace(msg.Coordinate)
	msg.PolygonWKT = squashWhitespace(msg.PolygonWKT)
	msg.Body = strings.TrimSpace(msg.Body)
	msg.Original = strings.TrimSpace(msg.Original)
	msg.FIPSCodes = normalizeList(msg.FIPSCodes)
	msg.NWSZones = normalizeList(msg.NWSZones)

	if msg.AWIPSID == "" {
		msg.AWIPSID = extractAWIPS(msg.Body)
		if msg.AWIPSID == "" {
			msg.AWIPSID = extractAWIPS(msg.Original)
		}
	}

	if msg.EventCode == "" && len(msg.AWIPSID) >= 3 {
		msg.EventCode = msg.AWIPSID[:3]
	}

	msg.VTEC = strings.ToUpper(strings.TrimSpace(msg.VTEC))
	if msg.VTEC == "" {
		msg.VTEC = extractVTEC(msg.Body)
	}
	if msg.VTEC == "" {
		msg.VTEC = extractVTEC(msg.Original)
	}
}

func buildSignature(msg Message) string {
	parts := []string{
		msg.EventCode,
		msg.AWIPSID,
		msg.VTEC,
		normalizePolygon(msg.PolygonWKT),
		strings.Join(msg.FIPSCodes, ","),
		strings.Join(msg.NWSZones, ","),
	}

	if isBlank(parts...) {
		parts = append(parts, shortHash(msg.Original+"\n"+msg.Body))
	}

	return strings.Join(parts, "|")
}

func normalizePolygon(wkt string) string {
	values := polygonNumberPattern.FindAllString(wkt, -1)
	if len(values) == 0 {
		return ""
	}
	return strings.Join(values, ",")
}

func normalizeList(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))

	for _, raw := range values {
		for _, value := range splitListValues(raw) {
			value = strings.ToUpper(strings.TrimSpace(value))
			if value == "" {
				continue
			}
			if _, exists := seen[value]; exists {
				continue
			}
			seen[value] = struct{}{}
			out = append(out, value)
		}
	}

	slices.Sort(out)
	return out
}

func splitListValues(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	var decoded []string
	if strings.HasPrefix(raw, "[") && json.Unmarshal([]byte(raw), &decoded) == nil {
		return decoded
	}

	return strings.FieldsFunc(raw, func(r rune) bool {
		switch {
		case r == ',', r == ';', r == '[', r == ']', r == '"':
			return true
		case unicode.IsSpace(r):
			return true
		default:
			return false
		}
	})
}

func extractAWIPS(text string) string {
	match := awipsPattern.FindString(strings.TrimSpace(text))
	return strings.ToUpper(strings.TrimSpace(match))
}

func extractVTEC(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "/") && strings.HasSuffix(line, "/") && strings.Contains(line, ".") {
			return strings.ToUpper(line)
		}
	}
	return ""
}

func squashWhitespace(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func isBlank(values ...string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:8])
}
