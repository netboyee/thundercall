package nwws

import (
	"encoding/xml"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	localIssuanceDateTimePattern = `[0-9]{3,4} (?:AM|PM) [A-Z]{2,4} [A-Z]{3} [A-Z]{3} [0-9]{1,2} [0-9]{4}`
	utcIssuanceDateTimePattern   = `[0-9]{4} UTC [A-Z]{3} [A-Z]{3} [0-9]{1,2} [0-9]{4}`
)

var (
	broadcastInstructions = []string{"BULLETIN", "URGENT", "FLASH", "REGULAR", "HOLD"}
	ugcExpression         = regexp.MustCompile(`(?i)^([A-Z]{2}[CZ](?:[0-9]{3}|ALL)(>|-|-\n)|(?:[0-9]{3}|ALL)(>|-|-\n))*[0-9]{6}-`)
	plainLanguageNamesExp = regexp.MustCompile(`(?is)^(([A-Za-z0-9 ,]*-)*)(\n{1,2})(([A-Za-z0-9 ,]*-)*)*`)
	includingCitiesExp    = regexp.MustCompile(`(?is)^INCLUDING THE (CITY|CITIES) OF((\.\.\.[A-Za-z0-9 ,]*)|(\.\.\.\n[A-Za-z0-9 ,]*)|([A-Za-z0-9 ,]*\.\.\.))*(\.\.\.[A-Za-z0-9 ,]*)`)
	primaryVTECExp        = regexp.MustCompile(`(?i)^\/([OTEX]{1}).([A-Z0-9]{3}).([A-Z0-9]{4}).([A-Z0-9]{2}).([A-Z0-9]{1}).([A-Z0-9]{4}).([0-9]{6}T[0-9]{4}Z)-([0-9]{6}T[0-9]{4}Z)\/`)
	hydrologicVTECExp     = regexp.MustCompile(`(?i)^\/([A-Z0-9]{5}).([A-Z0-9]{1}).([A-Z0-9]{2}).([0-9]{6}T[0-9]{4}Z).([0-9]{6}T[0-9]{4}Z).([0-9]{6}T[0-9]{4}Z).([A-Z0-9]{2})\/`)
	issuanceDateTimeExp   = regexp.MustCompile(`(?i)^(?:` + localIssuanceDateTimePattern + `|` + utcIssuanceDateTimePattern + `)(?:/(?:` + localIssuanceDateTimePattern + `|` + utcIssuanceDateTimePattern + `))?$`)
)

type Parser struct {
	referenceTime func() time.Time
}

func NewParser() *Parser {
	return &Parser{
		referenceTime: func() time.Time { return time.Now().UTC() },
	}
}

func (p *Parser) ParseEnvelope(rawXML string) (StanzaEnvelope, error) {
	type extension struct {
		CCCCode    string `xml:"cccc,attr"`
		WMOCode    string `xml:"ttaaii,attr"`
		Issue      string `xml:"issue,attr"`
		AWIPSID    string `xml:"awipsid,attr"`
		ExternalID string `xml:"id,attr"`
		Payload    string `xml:",innerxml"`
	}
	type message struct {
		Extension extension `xml:"x"`
	}

	rawXML = normalizeNewlines(rawXML)

	var decoded message
	if err := xml.Unmarshal([]byte(rawXML), &decoded); err != nil {
		return StanzaEnvelope{}, err
	}

	return decodeEnvelopeExtension(
		decoded.Extension.CCCCode,
		decoded.Extension.WMOCode,
		decoded.Extension.Issue,
		decoded.Extension.AWIPSID,
		decoded.Extension.ExternalID,
		decoded.Extension.Payload,
	), nil
}

func DecodeEnvelopeExtension(cccCode string, wmoCode string, issue string, awipsID string, externalID string, payload string) (StanzaEnvelope, error) {
	return decodeEnvelopeExtension(cccCode, wmoCode, issue, awipsID, externalID, payload), nil
}

func decodeEnvelopeExtension(cccCode string, wmoCode string, issue string, awipsID string, externalID string, payload string) StanzaEnvelope {
	issueTime, _ := time.Parse(time.RFC3339, strings.TrimSpace(issue))
	body := trimWhitespace(payload)
	body = strings.ReplaceAll(body, "<![CDATA[", "")
	body = strings.ReplaceAll(body, "]]>", "")
	body = trimWhitespace(body)

	// NWWS wraps the text payload in a leading byte-count line.
	if len(body) > 5 && strings.Index(body, "\n") > 0 {
		firstLine := strings.TrimSpace(strings.SplitN(body, "\n", 2)[0])
		if _, err := strconv.Atoi(firstLine); err == nil {
			body = strings.SplitN(body, "\n", 2)[1]
		}
	}

	return StanzaEnvelope{
		CCCCode:    cccCode,
		WMOCode:    wmoCode,
		IssueTime:  issueTime.UTC(),
		AWIPSID:    awipsID,
		ExternalID: externalID,
		Body:       trimWhitespace(body),
	}
}

func (p *Parser) Parse(raw string, referenceTime time.Time) (ParsedMessage, error) {
	raw = trimWhitespace(normalizeNewlines(raw))
	if raw == "" {
		return ParsedMessage{}, fmt.Errorf("empty NWWS message")
	}
	if referenceTime.IsZero() {
		referenceTime = p.referenceTime()
	}

	message := raw
	parsed := ParsedMessage{Original: raw}

	parsed.WMOHeader = parseWMOHeader(&message, referenceTime)
	message = trimWhitespace(message)
	parsed.AWIPSIdentifier = parseAWIPSIdentifier(&message)
	message = trimWhitespace(message)

	if message == "" {
		return parsed, nil
	}

	if !isUGCCode(message) {
		parsed.MNDHeader = parseMNDHeader(&message)
		message = trimWhitespace(message)
		if message != "" && !isUGCCode(message) {
			parsed.ProductHeadlineOverview = parseProductHeaderOverview(&message)
		}
		message = trimWhitespace(message)

		for message != "" {
			segment := Segment{
				Header: parseSegmentHeader(&message, referenceTime),
			}
			message = trimWhitespace(message)

			segmentEnd := strings.Index(message, "$$")
			if segmentEnd < 0 {
				break
			}

			segment.Message = trimWhitespace(message[:segmentEnd])
			segment.Polygon = findPolygon(segment.Message)
			parsed.Segments = append(parsed.Segments, segment)

			message = trimWhitespace(message[segmentEnd+2:])
		}

		if message != "" {
			parsed.Footer = trimWhitespace(message)
		}
		return parsed, nil
	}

	segment := Segment{}
	segment.Header = parseSegmentHeader(&message, referenceTime)
	message = trimWhitespace(message)

	parsed.MNDHeader = parseMNDHeader(&message)
	message = trimWhitespace(message)

	if segmentEnd := strings.Index(message, "$$"); segmentEnd >= 0 {
		message = message[:segmentEnd]
	}

	segment.Message = trimWhitespace(message)
	segment.Polygon = findPolygon(segment.Message)
	parsed.Segments = append(parsed.Segments, segment)
	return parsed, nil
}

func parseWMOHeader(message *string, referenceTime time.Time) WMOHeader {
	line := parseLine(message)
	header := WMOHeader{}

	parts := strings.Fields(line)
	if len(parts) < 3 {
		return header
	}

	header.DataType = parts[0]
	header.IssuingOffice = parts[1]
	if issuedAt, err := parseDayHourMinute(parts[2], referenceTime); err == nil {
		header.IssuedAt = issuedAt
	}
	if len(parts) >= 4 {
		header.BBBDesignator = parts[3]
	}
	return header
}

func parseAWIPSIdentifier(message *string) AWIPSIdentifier {
	productLine := strings.TrimSpace(parseLine(message))
	identifier := AWIPSIdentifier{}
	if len(productLine) >= 3 {
		identifier.ProductCategory = strings.TrimSpace(productLine[:3])
		identifier.OriginatingOffice = strings.TrimSpace(productLine[3:])
	}
	return identifier
}

func parseMNDHeader(message *string) MNDHeader {
	header := MNDHeader{}
	*message = trimWhitespace(*message)
	var productLines []string
	var issuingOfficeLines []string

	for *message != "" {
		line := strings.TrimSpace(peekLine(*message))

		switch {
		case isBroadcastInstruction(line):
			header.BroadcastInstruction = parseBroadcastInstruction(message)
		case isIssuanceDateTime(line):
			header.IssuanceDateTime = parseIssuanceDateTime(message)
			header.ProductName = strings.Join(productLines, " ")
			header.IssuingOfficeLines = append([]string(nil), issuingOfficeLines...)
			header.IssuingOffice = strings.Join(issuingOfficeLines, "\n")
			return header
		default:
			line = strings.TrimSpace(parseLine(message))
			if line != "" {
				switch {
				case len(productLines) == 0:
					productLines = append(productLines, line)
				case len(issuingOfficeLines) > 0 || isIssuingOfficeLine(line):
					issuingOfficeLines = append(issuingOfficeLines, line)
				default:
					productLines = append(productLines, line)
				}
			}
		}
		*message = trimWhitespace(*message)
	}

	header.ProductName = strings.Join(productLines, " ")
	header.IssuingOfficeLines = append([]string(nil), issuingOfficeLines...)
	header.IssuingOffice = strings.Join(issuingOfficeLines, "\n")
	return header
}

func parseSegmentHeader(message *string, referenceTime time.Time) SegmentHeader {
	header := SegmentHeader{}
	*message = trimWhitespace(*message)

	if isUGCCode(*message) {
		header.UGCCodes = parseUGC(message, referenceTime)
	}
	*message = trimWhitespace(*message)
	header.VTECPairs, header.PrimaryVTECs, header.HydrologicVTECs = parseVTECPairs(message)
	if len(header.PrimaryVTECs) > 0 {
		header.PrimaryVTEC = header.PrimaryVTECs[0]
	}
	if len(header.HydrologicVTECs) > 0 {
		header.HydrologicVTEC = header.HydrologicVTECs[0]
	}
	*message = trimWhitespace(*message)
	if *message != "" && isPlainLanguageGeographicNames(*message) {
		header.PlainLanguageGeographies = parsePlainLanguageGeographicNames(message)
	}
	*message = trimWhitespace(*message)
	if *message != "" && isIncludingCitiesOf(*message) {
		header.IncludingCitiesOf = parseIncludingCitiesOf(message)
	}
	*message = trimWhitespace(*message)
	if *message != "" && isIssuanceDateTime(*message) {
		header.IssuanceDateTime = parseIssuanceDateTime(message)
	}

	return header
}

func isBroadcastInstruction(message string) bool {
	line := strings.TrimSpace(peekLine(message))
	upper := strings.ToUpper(line)
	for _, candidate := range broadcastInstructions {
		if strings.Contains(upper, candidate) {
			return true
		}
	}
	return false
}

func parseBroadcastInstruction(message *string) string {
	return strings.TrimSpace(parseLine(message))
}

func parseProductHeaderOverview(message *string) string {
	var lines []string
	for *message != "" {
		line := parseLine(message)
		if strings.TrimSpace(line) == "" {
			break
		}
		lines = append(lines, line)
	}
	return trimWhitespace(strings.Join(lines, "\n"))
}

func parseVTECPairs(message *string) ([]VTECPair, []PrimaryVTEC, []HydrologicVTEC) {
	var (
		pairs      []VTECPair
		primarys   []PrimaryVTEC
		hydrologic []HydrologicVTEC
	)

	for {
		*message = trimWhitespace(*message)
		if !isPrimaryVTEC(*message) {
			break
		}

		primary := parsePrimaryVTEC(message)
		pair := VTECPair{Primary: primary}
		primarys = append(primarys, primary)

		*message = trimWhitespace(*message)
		if isHydrologicVTEC(*message) {
			entry := parseHydrologicVTEC(message)
			hydrologic = append(hydrologic, entry)
			pair.Hydrologic = &entry
		}

		pairs = append(pairs, pair)
	}

	return pairs, primarys, hydrologic
}

func isUGCCode(message string) bool {
	return ugcExpression.MatchString(message)
}

func parseUGC(message *string, referenceTime time.Time) []UGCCode {
	match := ugcExpression.FindString(*message)
	if match == "" {
		return nil
	}
	consumeMatchedPrefix(message, len(match))

	ugc := strings.ReplaceAll(match, "\n", "")
	var (
		ugcCodes         []UGCCode
		expirationDate   time.Time
		stateIdentifier  string
		formatIdentifier string
	)

	for len(ugc) >= 7 {
		if len(ugc) >= 3 && !isDigit(ugc[0]) && !isDigit(ugc[1]) {
			stateIdentifier = ugc[:2]
			ugc = ugc[2:]
			formatIdentifier = ugc[:1]
			ugc = ugc[1:]
		}

		if len(ugc) > 7 {
			code := ugc[:3]
			ugc = ugc[3:]
			delimiter := ugc[:1]
			ugc = ugc[1:]

			codeStart, codeEnd := parseUGCCodeRange(code, code)
			if delimiter == ">" {
				lastCode := ugc[:3]
				ugc = ugc[3:]
				_, codeEnd = parseUGCCodeRange(code, lastCode)
				if strings.HasPrefix(ugc, "-") {
					ugc = ugc[1:]
				}
			} else if delimiter != "-" {
				break
			}

			for current := codeStart; current <= codeEnd; current++ {
				ugcCodes = append(ugcCodes, UGCCode{
					Format: formatIdentifier,
					State:  stateIdentifier,
					Code:   fmt.Sprintf("%03d", current),
				})
			}
			continue
		}

		endOfCodeIndex := strings.Index(ugc, "-")
		if endOfCodeIndex < 0 {
			break
		}
		expirationRaw := ugc[:endOfCodeIndex]
		ugc = ugc[endOfCodeIndex+1:]
		if parsed, err := parseDayHourMinute(expirationRaw, referenceTime); err == nil {
			expirationDate = parsed
		}
	}

	for i := range ugcCodes {
		ugcCodes[i].ExpiresAt = expirationDate
	}
	return ugcCodes
}

func parseUGCCodeRange(first string, last string) (int, int) {
	if strings.EqualFold(first, "ALL") || first == "000" {
		return 0, 999
	}
	start, _ := strconv.Atoi(first)
	end, _ := strconv.Atoi(last)
	return start, end
}

func isPlainLanguageGeographicNames(message string) bool {
	return plainLanguageNamesExp.MatchString(message)
}

func parsePlainLanguageGeographicNames(message *string) string {
	match := plainLanguageNamesExp.FindString(*message)
	if match == "" {
		return ""
	}
	consumeMatchedPrefix(message, len(match))
	return strings.ReplaceAll(strings.TrimSpace(match), "\n", " ")
}

func isIncludingCitiesOf(message string) bool {
	return includingCitiesExp.MatchString(message)
}

func parseIncludingCitiesOf(message *string) string {
	match := includingCitiesExp.FindString(*message)
	if match == "" {
		return ""
	}
	consumeMatchedPrefix(message, len(match))
	return strings.ReplaceAll(strings.TrimSpace(match), "\n", "")
}

func isPrimaryVTEC(message string) bool {
	return primaryVTECExp.MatchString(message)
}

func parsePrimaryVTEC(message *string) PrimaryVTEC {
	match := primaryVTECExp.FindStringSubmatch(*message)
	if len(match) == 0 {
		return PrimaryVTEC{}
	}
	consumeMatchedPrefix(message, len(match[0]))
	return PrimaryVTEC{
		Raw:          match[0],
		ProductClass: match[1],
		Action:       match[2],
		OfficeID:     match[3],
		Phenomenon:   match[4],
		Significance: match[5],
		ETN:          match[6],
		BeginsAtRaw:  match[7],
		EndsAtRaw:    match[8],
		BeginsAt:     parseVTECTime(match[7]),
		EndsAt:       parseVTECTime(match[8]),
	}
}

func isHydrologicVTEC(message string) bool {
	return hydrologicVTECExp.MatchString(message)
}

func parseHydrologicVTEC(message *string) HydrologicVTEC {
	match := hydrologicVTECExp.FindStringSubmatch(*message)
	if len(match) == 0 {
		return HydrologicVTEC{}
	}
	consumeMatchedPrefix(message, len(match[0]))
	return HydrologicVTEC{
		NWSLocationIdentifier: match[1],
		FloodSeverity:         match[2],
		ImmediateCause:        match[3],
		BeginsAtRaw:           match[4],
		CrestAtRaw:            match[5],
		EndsAtRaw:             match[6],
		BeginsAt:              parseVTECTime(match[4]),
		CrestAt:               parseVTECTime(match[5]),
		EndsAt:                parseVTECTime(match[6]),
		FloodRecord:           match[7],
	}
}

func isIssuanceDateTime(message string) bool {
	return issuanceDateTimeExp.MatchString(strings.TrimSpace(peekLine(message)))
}

func parseIssuanceDateTime(message *string) string {
	return strings.TrimSpace(parseLine(message))
}

func isIssuingOfficeLine(line string) bool {
	upper := strings.ToUpper(strings.TrimSpace(line))
	if upper == "" {
		return false
	}

	for _, prefix := range []string{
		"ISSUED BY ",
		"NATIONAL WEATHER SERVICE ",
		"THE NATIONAL WEATHER SERVICE ",
		"NWS ",
		"NATIONAL HURRICANE CENTER ",
		"WEATHER PREDICTION CENTER ",
		"STORM PREDICTION CENTER ",
		"CLIMATE PREDICTION CENTER ",
		"AVIATION WEATHER CENTER ",
		"NATIONAL TSUNAMI WARNING CENTER ",
		"PACIFIC TSUNAMI WARNING CENTER ",
		"SPACE WEATHER PREDICTION CENTER ",
	} {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}

	return false
}

func findPolygon(messageBody string) []Coordinate {
	index := strings.Index(messageBody, "LAT...LON")
	if index < 0 {
		return nil
	}

	lines := strings.Split(messageBody[index+len("LAT...LON"):], "\n")
	var numericFields []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		fields := strings.Fields(trimmed)
		if !allNumeric(fields) {
			break
		}
		numericFields = append(numericFields, fields...)
	}

	polygon := make([]Coordinate, 0, len(numericFields)/2)
	for i := 0; i+1 < len(numericFields); i += 2 {
		lat, lon := numericFields[i], numericFields[i+1]
		if len(lat) <= 3 || len(lon) <= 3 {
			continue
		}
		latValue, err := strconv.ParseFloat(lat[:len(lat)-2]+"."+lat[len(lat)-2:], 64)
		if err != nil {
			continue
		}
		lonValue, err := strconv.ParseFloat(lon[:len(lon)-2]+"."+lon[len(lon)-2:], 64)
		if err != nil {
			continue
		}
		polygon = append(polygon, Coordinate{
			Latitude:  latValue,
			Longitude: -1 * lonValue,
		})
	}

	return polygon
}

func isDigit(value byte) bool {
	return value >= '0' && value <= '9'
}

func allNumeric(values []string) bool {
	if len(values) == 0 {
		return false
	}
	for _, value := range values {
		for i := 0; i < len(value); i++ {
			if !isDigit(value[i]) {
				return false
			}
		}
	}
	return true
}
