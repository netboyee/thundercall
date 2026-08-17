package ingest

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"time"

	"mellium.im/sasl"
	"mellium.im/xmlstream"
	"mellium.im/xmpp"
	"mellium.im/xmpp/jid"
	"mellium.im/xmpp/muc"
	"mellium.im/xmpp/mux"
	"mellium.im/xmpp/stanza"

	"thundercall-go/internal/config"
	"thundercall-go/internal/nwws"
)

type NWWSConsumer struct {
	cfg            config.NWWSConfig
	handler        func(context.Context, nwws.StanzaEnvelope) error
	reconnectDelay time.Duration
	logf           func(string, ...any)
	run            func(context.Context) error
}

func NewNWWSConsumer(cfg config.NWWSConfig, handler func(context.Context, nwws.StanzaEnvelope) error) *NWWSConsumer {
	consumer := &NWWSConsumer{
		cfg:            cfg,
		handler:        handler,
		reconnectDelay: 10 * time.Second,
		logf:           log.Printf,
	}
	consumer.run = consumer.runSession
	return consumer
}

func (c *NWWSConsumer) Run(ctx context.Context) error {
	return c.run(ctx)
}

func (c *NWWSConsumer) RunForever(ctx context.Context, reconnectDelay time.Duration) error {
	if reconnectDelay <= 0 {
		reconnectDelay = c.reconnectDelay
	}

	for {
		err := c.run(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if err != nil {
			c.logf("NWWS consumer error: %v", err)
		} else {
			c.logf("NWWS consumer disconnected; reconnecting")
		}

		timer := time.NewTimer(reconnectDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (c *NWWSConsumer) runSession(ctx context.Context) error {
	if c.handler == nil {
		return fmt.Errorf("nwws handler is required")
	}
	if !c.cfg.Enabled() {
		return fmt.Errorf("nwws credentials are required")
	}

	accountJID, err := jid.Parse(c.cfg.Username + "@" + c.cfg.Domain)
	if err != nil {
		return fmt.Errorf("parse NWWS account JID: %w", err)
	}

	session, err := xmpp.DialClientSession(
		ctx,
		accountJID,
		xmpp.StartTLS(&tls.Config{ServerName: c.cfg.Domain}),
		xmpp.SASL("", c.cfg.Password, sasl.Plain),
		xmpp.BindResource(),
	)
	if err != nil {
		return fmt.Errorf("dial NWWS XMPP session: %w", err)
	}
	var sessionCleanup sync.Once
	defer closeIOAsync(&sessionCleanup, "NWWS XMPP session", session, c.logf)

	mucClient := &muc.Client{}
	activity := make(chan struct{}, 1)
	router := mux.New(
		stanza.NSClient,
		muc.HandleClient(mucClient),
		mux.MessageFunc(stanza.GroupChatMessage, xml.Name{}, func(_ stanza.Message, r xmlstream.TokenReadEncoder) error {
			if c.cfg.IdleTimeout > 0 {
				select {
				case activity <- struct{}{}:
				default:
				}
			}
			envelope, ok, err := decodeNWWSGroupChat(r, c.cfg.LogFullMessages, c.logf)
			if err != nil {
				c.logf("decode NWWS groupchat payload: %v", err)
				return nil
			}
			if !ok {
				return nil
			}
			if err := c.handler(ctx, envelope); err != nil {
				c.logf("process NWWS envelope %q: %v", envelope.ExternalID, err)
			}
			return nil
		}),
	)

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- session.Serve(router)
	}()

	roomJID, err := jid.Parse(fmt.Sprintf("%s/%s", c.cfg.RoomJID(), c.cfg.Nick()))
	if err != nil {
		return fmt.Errorf("parse NWWS room JID: %w", err)
	}

	options := []muc.Option{
		muc.MaxHistory(0),
	}
	if c.cfg.JoinPassword != "" {
		options = append(options, muc.Password(c.cfg.JoinPassword))
	}

	channel, err := mucClient.Join(ctx, roomJID, session, options...)
	if err != nil {
		return fmt.Errorf("join NWWS room %s: %w", roomJID.Bare().String(), err)
	}
	var channelCleanup sync.Once
	defer leaveNWWSChannelAsync(&channelCleanup, channel, c.logf)
	c.logf("joined NWWS room %s as %s", roomJID.Bare().String(), roomJID.Resourcepart())

	var idleErr <-chan error
	if c.cfg.IdleTimeout > 0 {
		idleErr = monitorNWWSIdleSession(ctx, c.cfg.IdleTimeout, activity, c.logf)
	}

	select {
	case err := <-serveErr:
		if err == nil || ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	case err := <-idleErr:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func decodeNWWSGroupChat(r xmlstream.TokenReadEncoder, logFullMessages bool, logf func(string, ...any)) (nwws.StanzaEnvelope, bool, error) {
	var raw bytes.Buffer
	encoder := xml.NewEncoder(&raw)
	tee := xmlstream.TeeReader(r, encoder)

	var sink struct {
		XMLName xml.Name
	}
	if err := xml.NewTokenDecoder(tee).Decode(&sink); err != nil {
		return nwws.StanzaEnvelope{}, false, err
	}
	if err := encoder.Flush(); err != nil {
		return nwws.StanzaEnvelope{}, false, err
	}

	rawXML := raw.String()
	payload, err := parseRawNWWSGroupChat(rawXML)
	if err != nil {
		return nwws.StanzaEnvelope{}, false, err
	}
	if payload.Extension.XMLName.Local == "" {
		return nwws.StanzaEnvelope{}, false, nil
	}

	xhtmlBody := extractXHTMLBodyFromRawMessage(rawXML)
	payloadSource, selectedPayload := selectEnvelopePayloadSource(payload.Body, xhtmlBody, payload.Extension.RawPayload)
	if logFullMessages && logf != nil {
		logf(
			"received NWWS message id=%s awips=%s source=%s payload:\n%s",
			payload.Extension.ExternalID,
			payload.Extension.AWIPSID,
			payloadSource,
			selectedPayload,
		)
	}
	if strings.TrimSpace(payload.Extension.RawPayload) == "" {
		logf(
			"NWWS stanza missing extension payload awips=%s id=%s xhtml_len=%d inner=%q",
			payload.Extension.AWIPSID,
			payload.Extension.ExternalID,
			len(xhtmlBody),
			truncateForLog(rawXML, 400),
		)
		return nwws.StanzaEnvelope{}, false, nil
	}

	envelope, err := nwws.DecodeEnvelopeExtension(
		payload.Extension.CCCCode,
		payload.Extension.WMOCode,
		payload.Extension.Issue,
		payload.Extension.AWIPSID,
		payload.Extension.ExternalID,
		payload.Extension.RawPayload,
	)
	return envelope, true, err
}

func monitorNWWSIdleSession(ctx context.Context, idleTimeout time.Duration, activity <-chan struct{}, logf func(string, ...any)) <-chan error {
	errCh := make(chan error, 1)

	go func() {
		timer := time.NewTimer(idleTimeout)
		defer timer.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-activity:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(idleTimeout)
			case <-timer.C:
				err := fmt.Errorf("nwws session idle for %s", idleTimeout)
				if logf != nil {
					logf("%v; forcing reconnect", err)
				}
				errCh <- err
				close(errCh)
				return
			}
		}
	}()

	return errCh
}

func closeIOAsync(cleanup *sync.Once, label string, closer io.Closer, logf func(string, ...any)) {
	if cleanup == nil || closer == nil {
		return
	}
	cleanup.Do(func() {
		go func() {
			if err := closer.Close(); err != nil && logf != nil {
				logf("close %s: %v", label, err)
			}
		}()
	})
}

func leaveNWWSChannelAsync(cleanup *sync.Once, channel *muc.Channel, logf func(string, ...any)) {
	if cleanup == nil || channel == nil {
		return
	}
	cleanup.Do(func() {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := channel.Leave(ctx, ""); err != nil && logf != nil {
				logf("leave NWWS room: %v", err)
			}
		}()
	})
}

type rawNWWSGroupChat struct {
	Body      string `xml:"body"`
	Extension struct {
		XMLName    xml.Name `xml:"x"`
		CCCCode    string   `xml:"cccc,attr"`
		WMOCode    string   `xml:"ttaaii,attr"`
		Issue      string   `xml:"issue,attr"`
		AWIPSID    string   `xml:"awipsid,attr"`
		ExternalID string   `xml:"id,attr"`
		RawPayload string   `xml:",innerxml"`
	} `xml:"x"`
}

func parseRawNWWSGroupChat(rawXML string) (rawNWWSGroupChat, error) {
	rawXML = strings.TrimSpace(rawXML)
	if rawXML == "" {
		return rawNWWSGroupChat{}, fmt.Errorf("empty NWWS groupchat stanza")
	}

	var payload rawNWWSGroupChat
	if err := xml.Unmarshal([]byte(rawXML), &payload); err != nil {
		return rawNWWSGroupChat{}, err
	}
	return payload, nil
}

func selectEnvelopePayload(body string, xhtmlBody string, extensionRaw string) string {
	_, payload := selectEnvelopePayloadSource(body, xhtmlBody, extensionRaw)
	return payload
}

func selectEnvelopePayloadSource(body string, xhtmlBody string, extensionRaw string) (string, string) {
	if strings.TrimSpace(extensionRaw) != "" {
		return "extension", extensionRaw
	}
	if strings.TrimSpace(xhtmlBody) != "" {
		return "xhtml", xhtmlBody
	}
	return "body", body
}

func truncateForLog(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}

func extractTextFromXHTML(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}

	decoder := xml.NewDecoder(strings.NewReader("<root>" + input + "</root>"))
	var builder strings.Builder

	appendNewline := func() {
		text := builder.String()
		if text == "" || strings.HasSuffix(text, "\n") {
			return
		}
		builder.WriteByte('\n')
	}

	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}

		switch token := token.(type) {
		case xml.StartElement:
			switch token.Name.Local {
			case "br":
				appendNewline()
			case "p", "div", "pre", "li", "tr":
				if builder.Len() > 0 {
					appendNewline()
				}
			}
		case xml.EndElement:
			switch token.Name.Local {
			case "p", "div", "pre", "li", "tr":
				appendNewline()
			}
		case xml.CharData:
			builder.WriteString(string(token))
		}
	}

	return strings.TrimSpace(builder.String())
}

func extractXHTMLBodyFromRawMessage(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	var payload struct {
		HTML struct {
			Body struct {
				InnerXML string `xml:",innerxml"`
			} `xml:"body"`
		} `xml:"html"`
	}
	if err := xml.Unmarshal([]byte(raw), &payload); err != nil {
		return ""
	}
	return extractTextFromXHTML(payload.HTML.Body.InnerXML)
}
