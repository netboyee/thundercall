package thundercall

import (
	"context"
	"strings"
	"time"

	"thundercall-go/internal/models"
)

type ValidationError struct {
	Message string
}

func (e ValidationError) Error() string {
	return e.Message
}

type IncomingMessageRequest struct {
	MessageSource      string    `json:"messageSource"`
	MessageEvent       string    `json:"messageEvent"`
	MessageType        string    `json:"messageType"`
	ExternalID         string    `json:"externalId,omitempty"`
	SourceMessageID    int64     `json:"sourceMessageId,omitempty"`
	SourceSegmentIndex int       `json:"sourceSegmentIndex,omitempty"`
	PrimaryVTECCount   int       `json:"primaryVTECCount,omitempty"`
	PrimaryVTECRaw     string    `json:"primaryVTECRaw,omitempty"`
	VTECAction         string    `json:"vtecAction,omitempty"`
	VTECProductClass   string    `json:"vtecProductClass,omitempty"`
	VTECOfficeID       string    `json:"vtecOfficeId,omitempty"`
	VTECPhenomenon     string    `json:"vtecPhenomenon,omitempty"`
	VTECSignificance   string    `json:"vtecSignificance,omitempty"`
	VTECETN            string    `json:"vtecEtn,omitempty"`
	VTECBeginsAtRaw    string    `json:"vtecBeginsAtRaw,omitempty"`
	VTECBeginsAt       time.Time `json:"vtecBeginsAt,omitempty"`
	VTECEndsAtRaw      string    `json:"vtecEndsAtRaw,omitempty"`
	VTECEndsAt         time.Time `json:"vtecEndsAt,omitempty"`
	Coordinate         string    `json:"coordinate"`
	Polygon            string    `json:"polygon"`
	FIPSCodes          []string  `json:"fipsCodes"`
	NWSZones           []string  `json:"nwsZones"`
	Title              string    `json:"title"`
	Body               string    `json:"body"`
	Timestamp          time.Time `json:"timestamp"`
	Original           string    `json:"original"`
}

func (r IncomingMessageRequest) Validate() error {
	switch {
	case strings.TrimSpace(r.MessageSource) == "":
		return ValidationError{Message: "messageSource is required"}
	case strings.TrimSpace(r.MessageEvent) == "":
		return ValidationError{Message: "messageEvent is required"}
	case strings.TrimSpace(r.MessageType) == "":
		return ValidationError{Message: "messageType is required"}
	case strings.TrimSpace(r.Body) == "":
		return ValidationError{Message: "body is required"}
	case strings.TrimSpace(r.Polygon) == "" && len(r.FIPSCodes) == 0 && len(r.NWSZones) == 0:
		return ValidationError{Message: "one of polygon, fipsCodes, or nwsZones is required"}
	default:
		return nil
	}
}

func (r IncomingMessageRequest) HasSinglePrimaryVTEC() bool {
	return r.PrimaryVTECCount == 1 &&
		strings.TrimSpace(r.VTECProductClass) != "" &&
		strings.TrimSpace(r.VTECOfficeID) != "" &&
		strings.TrimSpace(r.VTECPhenomenon) != "" &&
		strings.TrimSpace(r.VTECSignificance) != "" &&
		strings.TrimSpace(r.VTECETN) != ""
}

func (r IncomingMessageRequest) AlertEventCode() string {
	if r.HasSinglePrimaryVTEC() {
		return strings.ToUpper(strings.TrimSpace(r.VTECPhenomenon + r.VTECSignificance))
	}
	return strings.ToUpper(strings.TrimSpace(r.MessageEvent))
}

func (r IncomingMessageRequest) ConfiguredProductCode() string {
	switch r.AlertEventCode() {
	case "SVW":
		return "SVR"
	case "TOW":
		return "TOR"
	case "TSA", "TSW":
		return "TSU"
	default:
		event := strings.ToUpper(strings.TrimSpace(r.MessageEvent))
		if event != "" {
			return event
		}
		return strings.ToUpper(strings.TrimSpace(r.AlertEventCode()))
	}
}

type UserMatch struct {
	UserID     int64
	AccountID  int64
	LocationID *int64
	Channels   []models.Channel
}

type RecipientResolver interface {
	ResolveRecipients(ctx context.Context, message *models.Message) ([]UserMatch, error)
}

type Dispatcher interface {
	Dispatch(ctx context.Context, message *models.Message, matches []UserMatch) error
}

func ShouldLoadNWWSMessage(req IncomingMessageRequest) bool {
	if !strings.EqualFold(strings.TrimSpace(req.MessageSource), "NWWS") {
		return true
	}

	// Match the legacy ThunderCall behavior: only Dense Fog Advisory
	// messages from NPW are forwarded as actionable alerts.
	if strings.EqualFold(strings.TrimSpace(req.MessageEvent), "NPW") {
		return strings.Contains(strings.ToLower(req.Body), strings.ToLower("Dense Fog Advisory"))
	}

	return true
}

func AlertTypeFromEvent(event string) string {
	event = strings.ToUpper(strings.TrimSpace(event))

	switch {
	case strings.HasPrefix(event, "FFW"):
		return "flash_flood_warning"
	case strings.HasPrefix(event, "NPW"):
		return "special_weather_statement"
	case strings.HasPrefix(event, "FZW"):
		return "freeze_warning"
	case strings.HasPrefix(event, "SVR"):
		return "severe_thunderstorm_warning"
	case strings.HasPrefix(event, "TOR"):
		return "tornado_warning"
	case strings.HasPrefix(event, "TS"):
		return "tropical_storm"
	case strings.HasPrefix(event, "WSW"):
		return "winter_storm_warning"
	default:
		return "unknown"
	}
}
