package sendgrid

import (
	"context"
	"fmt"
	"html"
	"strings"

	sendgridapi "github.com/sendgrid/sendgrid-go"
	"github.com/sendgrid/sendgrid-go/helpers/mail"

	"thundercall-go/internal/config"
)

type Provider struct {
	client    *sendgridapi.Client
	fromEmail string
	fromName  string
}

type Result struct {
	Provider          string
	ProviderMessageID string
	Status            string
}

func New(cfg config.SendGridConfig) *Provider {
	if !cfg.Enabled() {
		return &Provider{}
	}

	return &Provider{
		client:    sendgridapi.NewSendClient(cfg.APIKey),
		fromEmail: cfg.FromEmail,
		fromName:  cfg.FromName,
	}
}

func (p *Provider) SendEmail(_ context.Context, to string, subject string, plainTextBody string) (Result, error) {
	if p.client == nil {
		return Result{}, fmt.Errorf("sendgrid is not configured")
	}

	from := mail.NewEmail(p.fromName, p.fromEmail)
	recipient := mail.NewEmail("", to)
	htmlBody := "<pre>" + html.EscapeString(strings.TrimSpace(plainTextBody)) + "</pre>"
	message := mail.NewSingleEmail(from, subject, recipient, plainTextBody, htmlBody)

	resp, err := p.client.Send(message)
	if err != nil {
		return Result{}, err
	}

	return Result{
		Provider:          "sendgrid_email",
		ProviderMessageID: firstHeader(resp.Headers, "X-Message-Id"),
		Status:            fmt.Sprintf("%d", resp.StatusCode),
	}, nil
}

func firstHeader(headers map[string][]string, key string) string {
	if headers == nil {
		return ""
	}
	values := headers[key]
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
