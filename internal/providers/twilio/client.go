package twilio

import (
	"context"
	"encoding/xml"
	"fmt"
	"log"
	"strings"
	"time"

	twiliogo "github.com/twilio/twilio-go"
	api "github.com/twilio/twilio-go/rest/api/v2010"

	"thundercall-go/internal/config"
)

type Provider struct {
	client              *twiliogo.RestClient
	messagingServiceSID string
	smsFrom             string
	voiceFrom           string
	voiceStatusCallback string
	voiceLogOnly        bool
	logf                func(string, ...any)
	now                 func() time.Time
}

type Result struct {
	Provider          string
	ProviderMessageID string
	Status            string
}

func New(cfg config.TwilioConfig) *Provider {
	provider := &Provider{
		messagingServiceSID: cfg.MessagingServiceSID,
		smsFrom:             cfg.SMSFrom,
		voiceFrom:           cfg.VoiceFrom,
		voiceStatusCallback: cfg.VoiceStatusCallback,
		voiceLogOnly:        cfg.VoiceLogOnly,
		logf:                log.Printf,
		now:                 func() time.Time { return time.Now().UTC() },
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

func (p *Provider) SendVoice(_ context.Context, to string, body string) (Result, error) {
	if p.voiceLogOnly {
		providerMessageID := fmt.Sprintf("dryrun-voice-%d", p.now().UnixNano())
		p.logf(
			"twilio voice dry-run: placing call to=%s from=%s provider_message_id=%s body=%q",
			to,
			p.voiceFrom,
			providerMessageID,
			truncateLogBody(body, 120),
		)
		return Result{
			Provider:          "twilio_voice",
			ProviderMessageID: providerMessageID,
			Status:            "sent",
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
	params.SetTwiml(buildVoiceTwiml(body))
	if p.voiceStatusCallback != "" {
		params.SetStatusCallback(p.voiceStatusCallback)
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

func truncateLogBody(body string, maxLength int) string {
	body = strings.TrimSpace(body)
	if maxLength <= 0 || len(body) <= maxLength {
		return body
	}
	return body[:maxLength-3] + "..."
}
