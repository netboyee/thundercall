package twilio

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	twiliogo "github.com/twilio/twilio-go"
	api "github.com/twilio/twilio-go/rest/api/v2010"

	"thundercall-go/internal/config"
	"thundercall-go/internal/logging"
)

type Provider struct {
	client                  *twiliogo.RestClient
	messagingServiceSID     string
	smsFrom                 string
	voiceFrom               string
	voiceURL                string
	voiceToOverride         string
	voiceOverrideSingleCall bool
	voiceStatusCallback     string
	voiceLogOnly            bool
	debugf                  func(string, ...any)
	warnf                   func(string, ...any)
	now                     func() time.Time
}

type Result struct {
	Provider          string
	ProviderMessageID string
	Status            string
}

type VoiceRequest struct {
	To            string
	Body          string
	EventCode     string
	AlertTypeCode string
	AccountID     int64
}

type VoiceCallDetails struct {
	SID             string
	Status          string
	AnsweredBy      string
	DurationSeconds *int
}

func New(cfg config.TwilioConfig) *Provider {
	logger := logging.New("twilio")
	provider := &Provider{
		messagingServiceSID:     cfg.MessagingServiceSID,
		smsFrom:                 cfg.SMSFrom,
		voiceFrom:               cfg.VoiceFrom,
		voiceURL:                strings.TrimSpace(cfg.VoiceURL),
		voiceToOverride:         strings.TrimSpace(cfg.VoiceToOverride),
		voiceOverrideSingleCall: cfg.VoiceOverrideSingleCall,
		voiceStatusCallback:     cfg.VoiceStatusCallback,
		voiceLogOnly:            cfg.VoiceLogOnly,
		debugf:                  logger.Debugf,
		warnf:                   logger.Warnf,
		now:                     func() time.Time { return time.Now().UTC() },
	}
	if !cfg.Enabled() {
		return provider
	}

	provider.client = twiliogo.NewRestClientWithParams(twiliogo.ClientParams{
		Username: cfg.AccountSID,
		Password: cfg.AuthToken,
	})
	return provider
}

func (p *Provider) SendSMS(_ context.Context, to string, body string) (Result, error) {
	if p.client == nil {
		return Result{}, fmt.Errorf("twilio is not configured")
	}
	if p.messagingServiceSID == "" && p.smsFrom == "" {
		return Result{}, fmt.Errorf("twilio sms sender is not configured")
	}

	params := &api.CreateMessageParams{}
	params.SetTo(to)
	params.SetBody(body)
	if p.messagingServiceSID != "" {
		params.SetMessagingServiceSid(p.messagingServiceSID)
	} else {
		params.SetFrom(p.smsFrom)
	}

	resp, err := p.client.Api.CreateMessage(params)
	if err != nil {
		return Result{}, err
	}

	return Result{
		Provider:          "twilio_sms",
		ProviderMessageID: deref(resp.Sid),
		Status:            deref(resp.Status),
	}, nil
}

func (p *Provider) SendVoice(_ context.Context, request VoiceRequest) (Result, error) {
	to := strings.TrimSpace(request.To)
	if p.voiceLogOnly {
		providerMessageID := fmt.Sprintf("dryrun-voice-%d", p.now().UnixNano())
		if voiceURL, ok := p.voiceFunctionURL(request); ok {
			p.debugf(
				"event=twilio_voice_dry_run mode=function to=%s from=%s provider_message_id=%s voice_url=%q audio=%s account_id=%d",
				to,
				p.voiceFrom,
				providerMessageID,
				voiceURL,
				VoiceFunctionAudioCode(request.EventCode, request.AlertTypeCode),
				request.AccountID,
			)
		} else {
			p.debugf(
				"event=twilio_voice_dry_run mode=twiml to=%s from=%s provider_message_id=%s",
				to,
				p.voiceFrom,
				providerMessageID,
			)
		}
		return Result{
			Provider:          "twilio_voice",
			ProviderMessageID: providerMessageID,
			Status:            "completed",
		}, nil
	}
	if p.client == nil {
		return Result{}, fmt.Errorf("twilio is not configured")
	}
	if p.voiceFrom == "" {
		return Result{}, fmt.Errorf("twilio voice sender is not configured")
	}

	params := &api.CreateCallParams{}
	params.SetTo(to)
	params.SetFrom(p.voiceFrom)
	params.SetMachineDetection("DetectMessageEnd")
	if voiceURL, ok := p.voiceFunctionURL(request); ok {
		params.SetUrl(voiceURL)
	} else {
		params.SetTwiml(buildVoiceTwiml(request.Body))
	}
	if p.voiceStatusCallback != "" {
		params.SetStatusCallback(p.voiceStatusCallback)
		params.SetStatusCallbackMethod("POST")
		params.SetStatusCallbackEvent([]string{"completed"})
	}

	resp, err := p.client.Api.CreateCall(params)
	if err != nil {
		return Result{}, err
	}

	return Result{
		Provider:          "twilio_voice",
		ProviderMessageID: deref(resp.Sid),
		Status:            deref(resp.Status),
	}, nil
}

func (p *Provider) LookupVoiceCall(_ context.Context, sid string) (VoiceCallDetails, error) {
	if p.client == nil {
		return VoiceCallDetails{}, fmt.Errorf("twilio is not configured")
	}

	call, err := p.client.Api.FetchCall(strings.TrimSpace(sid), nil)
	if err != nil {
		return VoiceCallDetails{}, err
	}

	details := VoiceCallDetails{
		SID:        deref(call.Sid),
		Status:     deref(call.Status),
		AnsweredBy: deref(call.AnsweredBy),
	}
	if duration := strings.TrimSpace(deref(call.Duration)); duration != "" {
		if value, parseErr := strconv.Atoi(duration); parseErr == nil {
			details.DurationSeconds = &value
		}
	}

	return details, nil
}

func (p *Provider) ResolveVoiceDestination(to string) (string, bool) {
	override := strings.TrimSpace(p.voiceToOverride)
	to = strings.TrimSpace(to)
	if override == "" {
		return to, false
	}
	return override, true
}

func (p *Provider) BuildTestVoiceBody(intendedTo string, body string) string {
	if strings.TrimSpace(p.voiceToOverride) == "" {
		return body
	}

	prefix := "This call is meant for " + formatPhoneNumberForVoice(intendedTo) + "."
	body = strings.TrimSpace(body)
	if body == "" {
		return prefix
	}
	return prefix + " " + body
}

func (p *Provider) BuildCollapsedTestVoiceBody(intendedTo string, body string) string {
	if strings.TrimSpace(p.voiceToOverride) == "" {
		return body
	}

	prefix := "This test call stands in for one or more intended recipients. The first intended recipient is " + formatPhoneNumberForVoice(intendedTo) + "."
	body = strings.TrimSpace(body)
	if body == "" {
		return prefix
	}
	return prefix + " " + body
}

func (p *Provider) CollapseVoiceOverrideCalls() bool {
	return strings.TrimSpace(p.voiceToOverride) != "" && p.voiceOverrideSingleCall
}

func (p *Provider) VoiceFrom() string {
	return strings.TrimSpace(p.voiceFrom)
}

func (p *Provider) VoiceURL() string {
	return p.voiceURL
}

func (p *Provider) voiceFunctionURL(request VoiceRequest) (string, bool) {
	if strings.TrimSpace(p.voiceURL) == "" {
		return "", false
	}
	if strings.TrimSpace(p.voiceToOverride) != "" {
		return "", false
	}
	if request.AccountID <= 0 {
		p.warnf(
			"event=twilio_voice_function_disabled reason=missing_account_id event_code=%s alert_type=%s destination=%s",
			request.EventCode,
			request.AlertTypeCode,
			request.To,
		)
		return "", false
	}

	voiceURL, err := BuildVoiceFunctionURL(p.voiceURL, VoiceFunctionAudioCode(request.EventCode, request.AlertTypeCode), request.AccountID)
	if err != nil {
		p.warnf("event=twilio_voice_function_invalid_url base_url=%q error=%q", p.voiceURL, err)
		return "", false
	}
	return voiceURL, true
}

func BuildVoiceFunctionURL(baseURL string, audio string, accountID int64) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "", err
	}

	query := parsed.Query()
	query.Set("audio", strings.TrimSpace(audio))
	query.Set("id", strconv.FormatInt(accountID, 10))
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func VoiceFunctionAudioCode(eventCode string, alertTypeCode string) string {
	event := strings.ToUpper(strings.TrimSpace(eventCode))
	switch event {
	case "WSW", "FFW", "TOR", "SVR", "TEST":
		return event
	}

	switch strings.ToLower(strings.TrimSpace(alertTypeCode)) {
	case "winter_storm_warning":
		return "WSW"
	case "flash_flood_warning":
		return "FFW"
	case "tornado_warning":
		return "TOR"
	case "severe_thunderstorm_warning":
		return "SVR"
	default:
		return event
	}
}

func buildVoiceTwiml(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		body = "You have a new ThunderCall notification."
	}

	var builder strings.Builder
	builder.WriteString("<Response><Say>")
	if err := xml.EscapeText(&builder, []byte(body)); err != nil {
		return "<Response><Say>You have a new ThunderCall notification.</Say></Response>"
	}
	builder.WriteString("</Say></Response>")
	return builder.String()
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func formatPhoneNumberForVoice(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "an unknown number"
	}

	digits := make([]string, 0, len(value))
	for _, r := range value {
		if r >= '0' && r <= '9' {
			digits = append(digits, string(r))
		}
	}
	if len(digits) == 0 {
		return value
	}
	return strings.Join(digits, " ")
}
