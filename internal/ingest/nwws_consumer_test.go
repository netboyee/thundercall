package ingest

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSelectEnvelopePayload(t *testing.T) {
	t.Run("prefers extension payload when present", func(t *testing.T) {
		got := selectEnvelopePayload("body text", "xhtml text", " extension payload ")
		if got != " extension payload " {
			t.Fatalf("selectEnvelopePayload() = %q, want extension payload", got)
		}
	})

	t.Run("falls back to xhtml body before summary body", func(t *testing.T) {
		got := selectEnvelopePayload("summary body", "xhtml text", "   ")
		if got != "xhtml text" {
			t.Fatalf("selectEnvelopePayload() = %q, want xhtml text", got)
		}
	})

	t.Run("falls back to body when extension payload is empty", func(t *testing.T) {
		got := selectEnvelopePayload("body text", "", "   ")
		if got != "body text" {
			t.Fatalf("selectEnvelopePayload() = %q, want body text", got)
		}
	})
}

func TestSelectEnvelopePayloadSource(t *testing.T) {
	t.Run("reports extension source", func(t *testing.T) {
		source, payload := selectEnvelopePayloadSource("body text", "xhtml text", "extension payload")
		if source != "extension" || payload != "extension payload" {
			t.Fatalf("selectEnvelopePayloadSource() = (%q, %q), want (%q, %q)", source, payload, "extension", "extension payload")
		}
	})

	t.Run("reports xhtml source", func(t *testing.T) {
		source, payload := selectEnvelopePayloadSource("body text", "xhtml text", "")
		if source != "xhtml" || payload != "xhtml text" {
			t.Fatalf("selectEnvelopePayloadSource() = (%q, %q), want (%q, %q)", source, payload, "xhtml", "xhtml text")
		}
	})

	t.Run("reports body source", func(t *testing.T) {
		source, payload := selectEnvelopePayloadSource("body text", "", "")
		if source != "body" || payload != "body text" {
			t.Fatalf("selectEnvelopePayloadSource() = (%q, %q), want (%q, %q)", source, payload, "body", "body text")
		}
	})
}

func TestExtractTextFromXHTML(t *testing.T) {
	input := `<p>Line 1</p><p>Line 2<br/>Line 3</p>`
	got := extractTextFromXHTML(input)
	want := "Line 1\nLine 2\nLine 3"
	if got != want {
		t.Fatalf("extractTextFromXHTML() = %q, want %q", got, want)
	}
}

func TestExtractXHTMLBodyFromRawMessage(t *testing.T) {
	raw := `<message xmlns="jabber:client"><body>summary</body><html xmlns="http://jabber.org/protocol/xhtml-im"><body xmlns="http://www.w3.org/1999/xhtml"><p>Line 1</p><p>Line 2<br/>Line 3</p></body></html></message>`
	got := extractXHTMLBodyFromRawMessage(raw)
	want := "Line 1\nLine 2\nLine 3"
	if got != want {
		t.Fatalf("extractXHTMLBodyFromRawMessage() = %q, want %q", got, want)
	}
}

func TestParseRawNWWSGroupChat(t *testing.T) {
	raw := `<message xmlns="jabber:client" type="groupchat"><body>KRLX issues Severe Thunderstorm Warning (SVR) valid 2026-08-11T17:54:00Z</body><html xmlns="http://jabber.org/protocol/xhtml-im"><body xmlns="http://www.w3.org/1999/xhtml">KRLX issues Severe Thunderstorm Warning (SVR) valid 2026-08-11T17:54:00Z</body></html><x xmlns="nwws-oi" cccc="KRLX" ttaaii="WUUS51" issue="2026-08-11T17:54:00Z" awipsid="SVRRLX" id="9707.1"><![CDATA[
454

WUUS51 KRLX 111754
SVRRLX
VAC027-WVC047-059-081-109-111900-
/O.NEW.KRLX.SV.W.0309.260811T1754Z-260811T1900Z/
]]></x></message>`

	payload, err := parseRawNWWSGroupChat(raw)
	if err != nil {
		t.Fatalf("parseRawNWWSGroupChat() error = %v", err)
	}
	if payload.Extension.AWIPSID != "SVRRLX" {
		t.Fatalf("AWIPSID = %q, want SVRRLX", payload.Extension.AWIPSID)
	}
	if !strings.Contains(payload.Extension.RawPayload, "WUUS51 KRLX 111754") {
		t.Fatalf("RawPayload missing expected bulletin text: %q", payload.Extension.RawPayload)
	}
	source, selected := selectEnvelopePayloadSource(payload.Body, extractXHTMLBodyFromRawMessage(raw), payload.Extension.RawPayload)
	if source != "extension" {
		t.Fatalf("selectEnvelopePayloadSource() source = %q, want extension", source)
	}
	if !strings.Contains(selected, "SVRRLX") {
		t.Fatalf("selected payload missing expected bulletin text: %q", selected)
	}
}

func TestMonitorNWWSIdleSessionEmitsReconnectError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := monitorNWWSIdleSession(ctx, 20*time.Millisecond, make(chan struct{}, 1), nil)

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected idle timeout error, got nil")
		}
		if !strings.Contains(err.Error(), "idle") {
			t.Fatalf("expected idle timeout error, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for idle reconnect signal")
	}
}

func TestCloseIOAsyncReturnsWithoutWaitingForClose(t *testing.T) {
	var cleanup sync.Once
	closer := &blockingCloser{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}

	done := make(chan struct{})
	go func() {
		closeIOAsync(&cleanup, "test closer", closer, nil)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("closeIOAsync blocked waiting for Close")
	}

	select {
	case <-closer.started:
	case <-time.After(time.Second):
		t.Fatal("closeIOAsync did not invoke Close")
	}

	close(closer.release)
}

type blockingCloser struct {
	started chan struct{}
	release chan struct{}
}

func (c *blockingCloser) Close() error {
	close(c.started)
	<-c.release
	return nil
}
