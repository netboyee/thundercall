package nwws

import (
	"strings"
	"time"

	"thundercall-go/internal/thundercall"
)

func Normalize(parsed ParsedMessage, sourceMessageID int64, externalMessageID string) []thundercall.IncomingMessageRequest {
	result := make([]thundercall.IncomingMessageRequest, 0, len(parsed.Segments))
	for i, segment := range parsed.Segments {
		body := buildMessageBody(parsed, segment)
		eventCode := strings.ToUpper(strings.TrimSpace(parsed.AWIPSIdentifier.ProductCategory))
		messageType := classifyMessageType(eventCode, body)
		if messageType == "" {
			messageType = eventCode
		}

		request := thundercall.IncomingMessageRequest{
			MessageSource: "NWWS",
			MessageEvent:  eventCode,
			MessageType:   messageType,
			Title:         "[ThunderCall] National Weather Wire Service Message",
			Body:          body,
			Timestamp:     parsed.WMOHeader.IssuedAt,
			Original:      parsed.Original,
		}
		request.PrimaryVTECCount = len(segment.Header.PrimaryVTECs)
		if request.Timestamp.IsZero() {
			request.Timestamp = time.Now().UTC()
		}
		if len(segment.Header.PrimaryVTECs) == 1 {
			primary := segment.Header.PrimaryVTEC
			request.PrimaryVTECRaw = strings.TrimSpace(primary.Raw)
			request.VTECAction = strings.ToUpper(strings.TrimSpace(primary.Action))
			request.VTECProductClass = strings.ToUpper(strings.TrimSpace(primary.ProductClass))
			request.VTECOfficeID = strings.ToUpper(strings.TrimSpace(primary.OfficeID))
			request.VTECPhenomenon = strings.ToUpper(strings.TrimSpace(primary.Phenomenon))
			request.VTECSignificance = strings.ToUpper(strings.TrimSpace(primary.Significance))
			request.VTECETN = strings.ToUpper(strings.TrimSpace(primary.ETN))
			request.VTECBeginsAtRaw = strings.ToUpper(strings.TrimSpace(primary.BeginsAtRaw))
			request.VTECBeginsAt = primary.BeginsAt
			request.VTECEndsAtRaw = strings.ToUpper(strings.TrimSpace(primary.EndsAtRaw))
			request.VTECEndsAt = primary.EndsAt
		}
		if len(segment.Polygon) > 0 {
			request.Polygon = wellKnownPolygon(segment.Polygon)
		}

		for _, ugcCode := range segment.Header.UGCCodes {
			switch ugcCode.Format {
			case "C":
				request.FIPSCodes = append(request.FIPSCodes, ugcCode.State+ugcCode.Format+ugcCode.Code)
			case "Z":
				request.NWSZones = append(request.NWSZones, ugcCode.State+ugcCode.Format+ugcCode.Code)
			}
		}

		request.ExternalID = externalMessageID
		request.SourceMessageID = sourceMessageID
		request.SourceSegmentIndex = i
		result = append(result, request)
	}
	return result
}

func buildMessageBody(parsed ParsedMessage, segment Segment) string {
	parts := make([]string, 0, 4)
	if parsed.MNDHeader.BroadcastInstruction != "" {
		parts = append(parts, parsed.MNDHeader.BroadcastInstruction)
	}
	if parsed.ProductHeadlineOverview != "" {
		parts = append(parts, parsed.ProductHeadlineOverview)
	}
	if segment.Message != "" {
		parts = append(parts, segment.Message)
	}
	if parsed.Footer != "" {
		parts = append(parts, parsed.Footer)
	}
	return trimWhitespace(strings.Join(parts, "\n\n"))
}

func classifyMessageType(eventCode string, body string) string {
	switch eventCode {
	case "TOA":
		return "Tornado Watch"
	case "TOR":
		return "Tornado Warning"
	case "SVA":
		return "Severe Weather Watch"
	case "SVR":
		return "Severe Weather Warning"
	case "SVS":
		return "Severe Weather Statement"
	case "WSA":
		return "Winter Storm Watch"
	case "WSW":
		return "Winter Storm Warning"
	case "TSA":
		return "Tsunami Watch"
	case "TSW":
		return "Tsunami Warning"
	case "FFA":
		return "Flash Flood Watch"
	case "FFW":
		return "Flash Flood Warning"
	case "FFS":
		return "Flash Flood Statement"
	case "FLA":
		return "Flood Watch"
	case "FLW":
		return "Flood Warning"
	case "FLS":
		return "Flood Statement"
	case "NPW":
		return classifyNonPrecipitation(body)
	default:
		return ""
	}
}

func classifyNonPrecipitation(body string) string {
	switch {
	case strings.Contains(strings.ToLower(body), strings.ToLower("Blowing Dust Advisory")):
		return "Blowing Dust Advisory"
	case strings.Contains(strings.ToLower(body), strings.ToLower("Dust Storm Warning")):
		return "Dust Storm Warning"
	case strings.Contains(strings.ToLower(body), strings.ToLower("Dense Fog Advisory")):
		return "Dense Fog Advisory"
	case strings.Contains(strings.ToLower(body), strings.ToLower("Excessive Heat Warning")):
		return "Excessive Heat Warning"
	case strings.Contains(strings.ToLower(body), strings.ToLower("Freeze Warning")):
		return "Freeze Warning"
	case strings.Contains(strings.ToLower(body), strings.ToLower("Frost Warning")):
		return "Frost Warning"
	case strings.Contains(strings.ToLower(body), strings.ToLower("Heat Advisory")):
		return "Heat Advisory"
	case strings.Contains(strings.ToLower(body), strings.ToLower("Smoke Advisory")):
		return "Smoke Advisory"
	case strings.Contains(strings.ToLower(body), strings.ToLower("Ashfall Advisory")):
		return "Ashfall Advisory"
	case strings.Contains(strings.ToLower(body), strings.ToLower("Wind Advisory")):
		return "Wind Advisory"
	case strings.Contains(strings.ToLower(body), strings.ToLower("Wind Chill Warning")):
		return "Wind Chill Warning"
	default:
		return "Non-Precipitation"
	}
}
