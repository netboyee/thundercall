package nwws

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"thundercall-go/internal/thundercall"
)

func TestParseEnvelope(t *testing.T) {
	parser := NewParser()
	raw := xmppEnvelopeFixture

	envelope, err := parser.ParseEnvelope(raw)
	if err != nil {
		t.Fatalf("ParseEnvelope() error = %v", err)
	}

	if envelope.CCCCode != "KCYS" {
		t.Fatalf("CCCCode = %q, want KCYS", envelope.CCCCode)
	}
	if envelope.WMOCode != "ASUS45" {
		t.Fatalf("WMOCode = %q, want ASUS45", envelope.WMOCode)
	}
	if envelope.AWIPSID != "RWRWY" {
		t.Fatalf("AWIPSID = %q, want RWRWY", envelope.AWIPSID)
	}
	if envelope.ExternalID != "4497.11905" {
		t.Fatalf("ExternalID = %q, want 4497.11905", envelope.ExternalID)
	}
	if !strings.Contains(envelope.Body, "WYOMING REGIONAL WEATHER ROUNDUP") {
		t.Fatalf("Body did not contain expected payload header")
	}
}

func TestParseSegmentedMessage(t *testing.T) {
	parser := NewParser()
	raw := readFixture(t, "multisegment_01_WSW_MultiSegment.txt")

	parsed, err := parser.Parse(raw, time.Date(2026, 2, 11, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if parsed.WMOHeader.DataType != "WWUS43" {
		t.Fatalf("WMO header type = %q, want WWUS43", parsed.WMOHeader.DataType)
	}
	if parsed.AWIPSIdentifier.ProductCategory != "WSW" {
		t.Fatalf("product category = %q, want WSW", parsed.AWIPSIdentifier.ProductCategory)
	}
	if parsed.MNDHeader.ProductName != "Winter Weather Message" {
		t.Fatalf("unexpected MND product name %q", parsed.MNDHeader.ProductName)
	}
	if parsed.MNDHeader.IssuingOffice != "National Weather Service Green Bay WI" {
		t.Fatalf("unexpected issuing office %q", parsed.MNDHeader.IssuingOffice)
	}
	if len(parsed.Segments) < 2 {
		t.Fatalf("segment count = %d, want at least 2", len(parsed.Segments))
	}
	if len(parsed.Segments[0].Header.UGCCodes) == 0 {
		t.Fatalf("first segment missing UGC codes")
	}
	if !strings.Contains(parsed.Segments[0].Message, "Heavy snow") {
		t.Fatalf("first segment body missing expected content")
	}
}

func TestParseNonSegmentedMessage(t *testing.T) {
	parser := NewParser()
	raw := readFixture(t, "examples_06_FFADVN.txt")

	parsed, err := parser.Parse(raw, time.Date(2026, 6, 24, 9, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if parsed.AWIPSIdentifier.ProductCategory != "FFA" {
		t.Fatalf("product category = %q, want FFA", parsed.AWIPSIdentifier.ProductCategory)
	}
	if len(parsed.Segments) != 1 {
		t.Fatalf("segment count = %d, want 1", len(parsed.Segments))
	}
	if parsed.MNDHeader.ProductName != "Flood Watch" {
		t.Fatalf("unexpected MND product name %q", parsed.MNDHeader.ProductName)
	}
	if parsed.MNDHeader.IssuingOffice != "National Weather Service Quad Cities IA IL" {
		t.Fatalf("unexpected issuing office %q", parsed.MNDHeader.IssuingOffice)
	}
	if len(parsed.Segments[0].Header.UGCCodes) < 6 {
		t.Fatalf("UGC count = %d, want many", len(parsed.Segments[0].Header.UGCCodes))
	}
	if !strings.Contains(parsed.Segments[0].Message, "Heavy rainfall may produce flash flooding") {
		t.Fatalf("segment body missing expected content")
	}
}

func TestParseSevereWeatherMessage(t *testing.T) {
	parser := NewParser()
	raw := readFixture(t, "examples_01_SVRDMX.txt")

	parsed, err := parser.Parse(raw, time.Date(2026, 8, 15, 21, 18, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(parsed.Segments) != 1 {
		t.Fatalf("segment count = %d, want 1", len(parsed.Segments))
	}
	segment := parsed.Segments[0]
	if len(segment.Header.PrimaryVTECs) != 1 {
		t.Fatalf("primary VTEC count = %d, want 1", len(segment.Header.PrimaryVTECs))
	}
	if segment.Header.PrimaryVTEC.Phenomenon != "SV" {
		t.Fatalf("phenomenon = %q, want SV", segment.Header.PrimaryVTEC.Phenomenon)
	}
	if segment.Header.PrimaryVTEC.Significance != "W" {
		t.Fatalf("significance = %q, want W", segment.Header.PrimaryVTEC.Significance)
	}
	if len(segment.Polygon) == 0 {
		t.Fatalf("polygon point count = %d, want at least 1", len(segment.Polygon))
	}
}

func TestNormalizeSevereWeatherMessage(t *testing.T) {
	parser := NewParser()
	raw := readFixture(t, "examples_01_SVRDMX.txt")
	parsed, err := parser.Parse(raw, time.Date(2026, 8, 15, 21, 18, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	requests := Normalize(parsed, 42, "external-1")
	if len(requests) != 1 {
		t.Fatalf("Normalize() count = %d, want 1", len(requests))
	}
	request := requests[0]
	if request.MessageSource != "NWWS" {
		t.Fatalf("MessageSource = %q, want NWWS", request.MessageSource)
	}
	if request.MessageEvent != "SVR" {
		t.Fatalf("MessageEvent = %q, want SVR", request.MessageEvent)
	}
	if request.MessageType != "Severe Weather Warning" {
		t.Fatalf("MessageType = %q, want Severe Weather Warning", request.MessageType)
	}
	if !strings.Contains(request.Polygon, "POLYGON ((") {
		t.Fatalf("Polygon = %q, want WKT polygon", request.Polygon)
	}
	if len(request.FIPSCodes) != 3 {
		t.Fatalf("FIPS code count = %d, want 3", len(request.FIPSCodes))
	}
}

func TestDecodeEnvelopeExtensionStripsByteCount(t *testing.T) {
	raw := xmppEnvelopeFixture
	parser := NewParser()
	envelope, err := parser.ParseEnvelope(raw)
	if err != nil {
		t.Fatalf("ParseEnvelope() error = %v", err)
	}

	decoded, err := DecodeEnvelopeExtension(
		envelope.CCCCode,
		envelope.WMOCode,
		envelope.IssueTime.Format(time.RFC3339),
		envelope.AWIPSID,
		envelope.ExternalID,
		"<![CDATA[\n454\n\n"+envelope.Body+"\n]]>",
	)
	if err != nil {
		t.Fatalf("DecodeEnvelopeExtension() error = %v", err)
	}

	if strings.HasPrefix(strings.TrimSpace(decoded.Body), "454") {
		t.Fatalf("decoded body still contained byte count prefix")
	}
	if !strings.Contains(decoded.Body, "WYOMING REGIONAL WEATHER ROUNDUP") {
		t.Fatalf("decoded body missing expected payload contents")
	}
}

func TestParseAWIPSIdentifierKeepsNextLineForMNDHeader(t *testing.T) {
	message := "SVRMKX\n\nBULLETIN - IMMEDIATE BROADCAST REQUESTED\nSevere Thunderstorm Warning"

	identifier := parseAWIPSIdentifier(&message)
	if identifier.ProductCategory != "SVR" {
		t.Fatalf("ProductCategory = %q, want SVR", identifier.ProductCategory)
	}
	if identifier.OriginatingOffice != "MKX" {
		t.Fatalf("OriginatingOffice = %q, want MKX", identifier.OriginatingOffice)
	}

	remaining := trimWhitespace(message)
	if !strings.HasPrefix(remaining, "BULLETIN - IMMEDIATE BROADCAST REQUESTED") {
		t.Fatalf("remaining message = %q, want MND header to remain intact", remaining)
	}
}

func TestParseUGCExpandsRangesAndExpiration(t *testing.T) {
	message := "WIZ001-002-006>008-080200-\nNORTHWEST WISCONSIN"
	reference := time.Date(2022, 3, 7, 1, 10, 0, 0, time.UTC)

	codes := parseUGC(&message, reference)
	if len(codes) != 5 {
		t.Fatalf("UGC count = %d, want 5", len(codes))
	}

	wantCodes := []string{"001", "002", "006", "007", "008"}
	for i, want := range wantCodes {
		if codes[i].State != "WI" {
			t.Fatalf("codes[%d].State = %q, want WI", i, codes[i].State)
		}
		if codes[i].Format != "Z" {
			t.Fatalf("codes[%d].Format = %q, want Z", i, codes[i].Format)
		}
		if codes[i].Code != want {
			t.Fatalf("codes[%d].Code = %q, want %q", i, codes[i].Code, want)
		}
		if !codes[i].ExpiresAt.Equal(time.Date(2022, 3, 8, 2, 0, 0, 0, time.UTC)) {
			t.Fatalf("codes[%d].ExpiresAt = %v, want 2022-03-08 02:00:00Z", i, codes[i].ExpiresAt)
		}
	}
}

func TestParseUGCSupportsALLCodes(t *testing.T) {
	message := "WIZALL-080200-\nNORTHWEST WISCONSIN"
	reference := time.Date(2022, 3, 7, 1, 10, 0, 0, time.UTC)

	codes := parseUGC(&message, reference)
	if len(codes) != 1000 {
		t.Fatalf("UGC count = %d, want 1000", len(codes))
	}
	if codes[0].Code != "000" || codes[len(codes)-1].Code != "999" {
		t.Fatalf("unexpected ALL expansion bounds %q..%q", codes[0].Code, codes[len(codes)-1].Code)
	}
	if codes[0].State != "WI" || codes[0].Format != "Z" {
		t.Fatalf("unexpected ALL expansion metadata %+v", codes[0])
	}
}

func TestParsePrimaryVTEC(t *testing.T) {
	message := "/O.NEW.KMKX.SV.W.0049.200718T1757Z-200718T1830Z/\nBULLETIN"

	vtec := parsePrimaryVTEC(&message)
	if vtec.ProductClass != "O" {
		t.Fatalf("ProductClass = %q, want O", vtec.ProductClass)
	}
	if vtec.Action != "NEW" {
		t.Fatalf("Action = %q, want NEW", vtec.Action)
	}
	if vtec.OfficeID != "KMKX" {
		t.Fatalf("OfficeID = %q, want KMKX", vtec.OfficeID)
	}
	if vtec.EventCode() != "SVW" {
		t.Fatalf("EventCode() = %q, want SVW", vtec.EventCode())
	}
	if !vtec.BeginsAt.Equal(time.Date(2020, 7, 18, 17, 57, 0, 0, time.UTC)) {
		t.Fatalf("BeginsAt = %v, want 2020-07-18 17:57:00Z", vtec.BeginsAt)
	}
	if !vtec.EndsAt.Equal(time.Date(2020, 7, 18, 18, 30, 0, 0, time.UTC)) {
		t.Fatalf("EndsAt = %v, want 2020-07-18 18:30:00Z", vtec.EndsAt)
	}
	if vtec.BeginsAtRaw != "200718T1757Z" || vtec.EndsAtRaw != "200718T1830Z" {
		t.Fatalf("unexpected raw VTEC times %q..%q", vtec.BeginsAtRaw, vtec.EndsAtRaw)
	}
}

func TestParsePrimaryVTECPreservesZeroTimestampSentinel(t *testing.T) {
	message := "/O.CON.KDMX.SV.W.0123.000000T0000Z-260815T2200Z/\nBULLETIN"

	vtec := parsePrimaryVTEC(&message)
	if !vtec.HasZeroBeginTime() {
		t.Fatalf("expected zero begin time sentinel to be preserved")
	}
	if vtec.BeginsAtRaw != zeroVTECTimestamp {
		t.Fatalf("BeginsAtRaw = %q, want %q", vtec.BeginsAtRaw, zeroVTECTimestamp)
	}
	if !vtec.BeginsAt.IsZero() {
		t.Fatalf("BeginsAt = %v, want zero time when sentinel is used", vtec.BeginsAt)
	}
}

func TestParseHydrologicVTECPreservesZeroTimestampSentinel(t *testing.T) {
	message := "/00000.0.ER.000000T0000Z.000000T0000Z.000000T0000Z.OO/\nBULLETIN"

	vtec := parseHydrologicVTEC(&message)
	if !vtec.HasZeroBeginTime() || !vtec.HasZeroCrestTime() || !vtec.HasZeroEndTime() {
		t.Fatalf("expected hydrologic zero-time sentinels to be preserved")
	}
	if vtec.BeginsAtRaw != zeroVTECTimestamp || vtec.CrestAtRaw != zeroVTECTimestamp || vtec.EndsAtRaw != zeroVTECTimestamp {
		t.Fatalf("unexpected hydrologic raw times %q %q %q", vtec.BeginsAtRaw, vtec.CrestAtRaw, vtec.EndsAtRaw)
	}
	if !vtec.BeginsAt.IsZero() || !vtec.CrestAt.IsZero() || !vtec.EndsAt.IsZero() {
		t.Fatalf("expected zero-valued parsed hydrologic times for sentinel values")
	}
}

func TestParseWMOHeaderCapturesBBBDesignator(t *testing.T) {
	message := "WWUS43 KGRB 111000 CCA\nWSWGRB"

	header := parseWMOHeader(&message, time.Date(2026, 2, 11, 10, 0, 0, 0, time.UTC))
	if header.BBBDesignator != "CCA" {
		t.Fatalf("BBBDesignator = %q, want CCA", header.BBBDesignator)
	}
	if !strings.HasPrefix(message, "WSWGRB") {
		t.Fatalf("unexpected remaining message %q", message)
	}
}

func TestParseDayHourMinuteHandlesMonthRollover(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		reference time.Time
		want      time.Time
	}{
		{
			name:      "next month",
			raw:       "010200",
			reference: time.Date(2026, 3, 31, 23, 55, 0, 0, time.UTC),
			want:      time.Date(2026, 4, 1, 2, 0, 0, 0, time.UTC),
		},
		{
			name:      "previous month",
			raw:       "312359",
			reference: time.Date(2026, 1, 1, 0, 5, 0, 0, time.UTC),
			want:      time.Date(2025, 12, 31, 23, 59, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDayHourMinute(tt.raw, tt.reference)
			if err != nil {
				t.Fatalf("parseDayHourMinute(%q) error = %v", tt.raw, err)
			}
			if !got.Equal(tt.want) {
				t.Fatalf("parseDayHourMinute(%q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestParseMNDHeaderSeparatesIssuingOffice(t *testing.T) {
	message := "Winter Weather Message\nNational Weather Service Green Bay WI\n400 AM CST Wed Feb 11 2026\n\nBody"

	header := parseMNDHeader(&message)
	if header.ProductName != "Winter Weather Message" {
		t.Fatalf("ProductName = %q, want Winter Weather Message", header.ProductName)
	}
	if len(header.IssuingOfficeLines) != 1 {
		t.Fatalf("IssuingOfficeLines count = %d, want 1", len(header.IssuingOfficeLines))
	}
	if header.IssuingOffice != "National Weather Service Green Bay WI" {
		t.Fatalf("IssuingOffice = %q, want National Weather Service Green Bay WI", header.IssuingOffice)
	}
	if !strings.HasPrefix(trimWhitespace(message), "Body") {
		t.Fatalf("unexpected remaining message %q", message)
	}
}

func TestParseMNDHeaderSupportsUTCIssuance(t *testing.T) {
	message := "Hydrologic Outlook\nNational Weather Service Chicago IL\n1455 UTC Tue Apr 28 2026\n\nBody"

	header := parseMNDHeader(&message)
	if header.IssuanceDateTime != "1455 UTC Tue Apr 28 2026" {
		t.Fatalf("IssuanceDateTime = %q, want UTC issuance line", header.IssuanceDateTime)
	}
	if header.ProductName != "Hydrologic Outlook" {
		t.Fatalf("ProductName = %q, want Hydrologic Outlook", header.ProductName)
	}
	if header.IssuingOffice != "National Weather Service Chicago IL" {
		t.Fatalf("IssuingOffice = %q, want National Weather Service Chicago IL", header.IssuingOffice)
	}
}

func TestParseSegmentHeaderSupportsDualTimeZoneIssuance(t *testing.T) {
	message := "WIZ012-020-111800-\n/O.NEW.KGRB.WS.W.0005.260211T1000Z-260211T1800Z/\nVilas-Oneida-\n400 AM CST Wed Feb 11 2026/500 AM EST Wed Feb 11 2026\n\nBody"

	header := parseSegmentHeader(&message, time.Date(2026, 2, 11, 10, 0, 0, 0, time.UTC))
	if header.IssuanceDateTime != "400 AM CST Wed Feb 11 2026/500 AM EST Wed Feb 11 2026" {
		t.Fatalf("IssuanceDateTime = %q, want dual-zone issuance line", header.IssuanceDateTime)
	}
	if !strings.HasPrefix(trimWhitespace(message), "Body") {
		t.Fatalf("unexpected remaining message %q", message)
	}
}

func TestParseSegmentHeaderSupportsMultipleVTECPairs(t *testing.T) {
	message := "UTZ015-016-230300-\n/O.CON.KSLC.FF.A.0007.110922T1200Z-110923T0000Z/\n/00000.0.ER.000000T0000Z.000000T0000Z.000000T0000Z.OO/\n/O.NEW.KSLC.FA.Y.0030.110922T2305Z-110923T0200Z/\n/00000.0.ER.000000T0000Z.000000T0000Z.000000T0000Z.OO/\n730 AM CDT Wed Jun 24 2026\nBody"

	header := parseSegmentHeader(&message, time.Date(2026, 6, 24, 12, 30, 0, 0, time.UTC))
	if len(header.PrimaryVTECs) != 2 {
		t.Fatalf("PrimaryVTECs count = %d, want 2", len(header.PrimaryVTECs))
	}
	if len(header.HydrologicVTECs) != 2 {
		t.Fatalf("HydrologicVTECs count = %d, want 2", len(header.HydrologicVTECs))
	}
	if len(header.VTECPairs) != 2 {
		t.Fatalf("VTECPairs count = %d, want 2", len(header.VTECPairs))
	}
	if header.PrimaryVTECs[0].EventCode() != "FFA" || header.PrimaryVTECs[1].EventCode() != "FAY" {
		t.Fatalf("unexpected P-VTEC event codes %q and %q", header.PrimaryVTECs[0].EventCode(), header.PrimaryVTECs[1].EventCode())
	}
	for i, pair := range header.VTECPairs {
		if pair.Hydrologic == nil {
			t.Fatalf("VTECPairs[%d] missing corresponding H-VTEC", i)
		}
		if !pair.Hydrologic.HasZeroBeginTime() || !pair.Hydrologic.HasZeroCrestTime() || !pair.Hydrologic.HasZeroEndTime() {
			t.Fatalf("VTECPairs[%d] missing preserved hydrologic zero-time sentinels", i)
		}
	}
	if !strings.HasPrefix(trimWhitespace(message), "Body") {
		t.Fatalf("unexpected remaining message %q", message)
	}
}

func TestFindPolygonStopsAtTimeMotLoc(t *testing.T) {
	points := findPolygon(readFixture(t, "examples_12_FFWLSX.txt"))
	if len(points) != 5 {
		t.Fatalf("polygon point count = %d, want 5", len(points))
	}
	if points[len(points)-1].Latitude != 38.42 || points[len(points)-1].Longitude != -90.77 {
		t.Fatalf("unexpected last polygon point %+v", points[len(points)-1])
	}
}

func TestFindPolygonContinuesAcrossBlankLines(t *testing.T) {
	message := strings.Join([]string{
		"BULLETIN - IMMEDIATE BROADCAST REQUESTED",
		"",
		"LAT...LON 4137 9173 4103 9172 4108 9218 4116 9218",
		"",
		"      4116 9229 4125 9230",
		"",
		"TIME...MOT...LOC 1410Z 266DEG 34KT 4116 9219",
	}, "\n")

	points := findPolygon(message)
	if len(points) != 6 {
		t.Fatalf("polygon point count = %d, want 6", len(points))
	}
	if points[len(points)-1].Latitude != 41.25 || points[len(points)-1].Longitude != -92.30 {
		t.Fatalf("unexpected last polygon point %+v", points[len(points)-1])
	}
}

func TestNormalizeCarriesSourceIdentifiers(t *testing.T) {
	parser := NewParser()
	raw := readFixture(t, "examples_12_FFWLSX.txt")

	parsed, err := parser.Parse(raw, time.Date(2026, 7, 21, 21, 45, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	requests := Normalize(parsed, 77, "nwws-source-77")
	if len(requests) != 1 {
		t.Fatalf("Normalize() count = %d, want 1", len(requests))
	}

	request := requests[0]
	if request.SourceMessageID != 77 {
		t.Fatalf("SourceMessageID = %d, want 77", request.SourceMessageID)
	}
	if request.SourceSegmentIndex != 0 {
		t.Fatalf("SourceSegmentIndex = %d, want 0", request.SourceSegmentIndex)
	}
	if request.ExternalID != "nwws-source-77" {
		t.Fatalf("ExternalID = %q, want nwws-source-77", request.ExternalID)
	}
}

func TestNormalizeSegmentedFixtureUsesSequentialSegmentIndexes(t *testing.T) {
	parser := NewParser()
	raw := readFixture(t, "multisegment_01_WSW_MultiSegment.txt")

	parsed, err := parser.Parse(raw, time.Date(2026, 2, 11, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	requests := Normalize(parsed, 501, "segmented-source")
	if len(requests) != len(parsed.Segments) {
		t.Fatalf("Normalize() count = %d, want %d", len(requests), len(parsed.Segments))
	}
	for i, request := range requests {
		if request.SourceSegmentIndex != i {
			t.Fatalf("requests[%d].SourceSegmentIndex = %d, want %d", i, request.SourceSegmentIndex, i)
		}
		if request.SourceMessageID != 501 {
			t.Fatalf("requests[%d].SourceMessageID = %d, want 501", i, request.SourceMessageID)
		}
	}
}

func TestClassifyNonPrecipitation(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "freeze warning",
			body: "A Freeze Warning remains in effect overnight.",
			want: "Freeze Warning",
		},
		{
			name: "wind advisory",
			body: "This Wind Advisory is in effect until evening.",
			want: "Wind Advisory",
		},
		{
			name: "fallback",
			body: "General non-precipitation statement.",
			want: "Non-Precipitation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyNonPrecipitation(tt.body); got != tt.want {
				t.Fatalf("classifyNonPrecipitation(%q) = %q, want %q", tt.body, got, tt.want)
			}
		})
	}
}

func TestAllExampleFixturesParseAndNormalize(t *testing.T) {
	parser := NewParser()

	for _, path := range listFixturePathsWithPrefix(t, "testdata", "examples_") {
		if filepath.Ext(path) != ".txt" {
			continue
		}

		t.Run(filepath.Base(path), func(t *testing.T) {
			raw := readFixturePath(t, path)
			parsed, err := parser.Parse(raw, corpusReferenceTime(filepath.Base(path)))
			if err != nil {
				t.Fatalf("Parse(%s) error = %v", filepath.Base(path), err)
			}
			if parsed.AWIPSIdentifier.ProductCategory == "" {
				t.Fatalf("Parse(%s) missing AWIPS product category", filepath.Base(path))
			}
			if len(parsed.Segments) == 0 {
				t.Fatalf("Parse(%s) returned no segments", filepath.Base(path))
			}

			requests := Normalize(parsed, 9001, filepath.Base(path))
			if len(requests) != len(parsed.Segments) {
				t.Fatalf("Normalize(%s) count = %d, want %d", filepath.Base(path), len(requests), len(parsed.Segments))
			}
			for i, request := range requests {
				assertNormalizedRequest(t, filepath.Base(path), i, request)
			}
		})
	}
}

func TestAllMultiSegmentFixturesProduceMultipleSegments(t *testing.T) {
	parser := NewParser()

	for _, path := range listFixturePathsWithPrefix(t, "testdata", "multisegment_") {
		if filepath.Ext(path) != ".txt" {
			continue
		}

		t.Run(filepath.Base(path), func(t *testing.T) {
			raw := readFixturePath(t, path)
			parsed, err := parser.Parse(raw, corpusReferenceTime(filepath.Base(path)))
			if err != nil {
				t.Fatalf("Parse(%s) error = %v", filepath.Base(path), err)
			}
			if len(parsed.Segments) < 2 {
				t.Fatalf("Parse(%s) segment count = %d, want at least 2", filepath.Base(path), len(parsed.Segments))
			}

			requests := Normalize(parsed, 9101, filepath.Base(path))
			if len(requests) != len(parsed.Segments) {
				t.Fatalf("Normalize(%s) count = %d, want %d", filepath.Base(path), len(requests), len(parsed.Segments))
			}
			for i, request := range requests {
				assertNormalizedRequest(t, filepath.Base(path), i, request)
			}
		})
	}
}

func TestAllMalformedFixturesAreExercised(t *testing.T) {
	parser := NewParser()

	for _, path := range listFixturePathsWithPrefix(t, "testdata", "malformed_") {
		if filepath.Ext(path) != ".txt" {
			continue
		}

		t.Run(filepath.Base(path), func(t *testing.T) {
			raw := readFixturePath(t, path)

			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatalf("Parse(%s) panicked: %v", filepath.Base(path), recovered)
				}
			}()

			parsed, err := parser.Parse(raw, corpusReferenceTime(filepath.Base(path)))
			if filepath.Base(path) == "malformed_12_empty_message.txt" {
				if err == nil {
					t.Fatalf("Parse(%s) error = nil, want error", filepath.Base(path))
				}
				return
			}

			if err != nil {
				return
			}

			requests := Normalize(parsed, 9201, filepath.Base(path))
			for i, request := range requests {
				if request.MessageSource == "" || request.MessageEvent == "" || request.MessageType == "" {
					t.Fatalf("Normalize(%s)[%d] missing core message fields", filepath.Base(path), i)
				}
			}
		})
	}
}

func readFixture(t *testing.T, name string) string {
	t.Helper()

	path := filepath.Join("testdata", name)
	if _, err := os.Stat(path); err != nil {
		path = resolveFixturePath(t, name)
	}
	bytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return strings.TrimPrefix(string(bytes), "\ufeff")
}

func readFixturePath(t *testing.T, path string) string {
	t.Helper()

	bytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture path %s: %v", path, err)
	}
	return strings.TrimPrefix(string(bytes), "\ufeff")
}

func resolveFixturePath(t *testing.T, name string) string {
	t.Helper()

	fixtureAliases := map[string]string{
		"nwws_segmented.txt":          filepath.Join("testdata", "multisegment_01_WSW_MultiSegment.txt"),
		"nwws_non_segmented.txt":      filepath.Join("testdata", "examples_06_FFADVN.txt"),
		"nwws_severe_weather.rtf.txt": filepath.Join("testdata", "examples_01_SVRDMX.txt"),
		"severe_weather_1.txt":        filepath.Join("testdata", "examples_01_SVRDMX.txt"),
		"severe_weather_2.txt":        filepath.Join("testdata", "examples_12_FFWLSX.txt"),
	}

	if path, ok := fixtureAliases[name]; ok {
		return path
	}

	var resolved string
	walkErr := filepath.Walk("testdata", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Base(path) == name {
			resolved = path
			return filepath.SkipAll
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("resolve fixture %s: %v", name, walkErr)
	}
	if resolved == "" {
		t.Fatalf("resolve fixture %s: not found", name)
	}
	return resolved
}

func listFixturePathsWithPrefix(t *testing.T, dir string, prefix string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read fixture directory %s: %v", dir, err)
	}

	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		paths = append(paths, filepath.Join(dir, entry.Name()))
	}
	sort.Strings(paths)
	return paths
}

func corpusReferenceTime(name string) time.Time {
	switch name {
	case "examples_02_TORLZK.txt":
		return time.Date(2026, 5, 3, 16, 42, 0, 0, time.UTC)
	case "examples_04_SMWTBW.txt":
		return time.Date(2026, 9, 4, 19, 25, 0, 0, time.UTC)
	case "examples_05_WSWGRB.txt", "multisegment_01_WSW_MultiSegment.txt":
		return time.Date(2026, 2, 11, 10, 0, 0, 0, time.UTC)
	case "examples_06_FFADVN.txt", "multisegment_02_FLS_MultiSegment.txt":
		return time.Date(2026, 6, 24, 12, 30, 0, 0, time.UTC)
	case "examples_07_SPSABR.txt":
		return time.Date(2026, 9, 17, 12, 30, 0, 0, time.UTC)
	case "examples_08_ESFLOT.txt":
		return time.Date(2026, 4, 28, 14, 55, 0, 0, time.UTC)
	case "examples_10_RFWREV.txt":
		return time.Date(2026, 6, 28, 16, 30, 0, 0, time.UTC)
	case "examples_11_CFWMLB.txt":
		return time.Date(2026, 9, 9, 10, 15, 0, 0, time.UTC)
	case "examples_12_FFWLSX.txt":
		return time.Date(2026, 7, 21, 21, 45, 0, 0, time.UTC)
	default:
		return time.Date(2026, 8, 15, 21, 18, 0, 0, time.UTC)
	}
}

func assertNormalizedRequest(t *testing.T, fixture string, index int, request thundercall.IncomingMessageRequest) {
	t.Helper()

	if request.MessageSource == "" || request.MessageEvent == "" || request.MessageType == "" || request.Body == "" {
		t.Fatalf("Normalize(%s)[%d] missing core message fields", fixture, index)
	}
	if len(request.FIPSCodes) == 0 && len(request.NWSZones) == 0 && strings.TrimSpace(request.Polygon) == "" {
		return
	}
	if err := request.Validate(); err != nil {
		t.Fatalf("Normalize(%s)[%d] validation error = %v", fixture, index, err)
	}
}

const xmppEnvelopeFixture = `<message xmlns="jabber:client" type="groupchat" from="nwws@conference.nwws-oi.weather.gov/nwws-oi"><body>KCYS issues RWR valid 2022-02-17T19:00:00Z</body><x xmlns="nwws-oi" cccc="KCYS" ttaaii="ASUS45" issue="2022-02-17T19:00:00Z" awipsid="RWRWY" id="4497.11905"><![CDATA[
454

ASUS45 KCYS 171910

RWRWY

WYOMING REGIONAL WEATHER ROUNDUP
NATIONAL WEATHER SERVICE CHEYENNE WY
]]></x></message>`
