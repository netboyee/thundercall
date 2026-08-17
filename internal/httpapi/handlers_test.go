package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestParseMessageListFilterDefaults(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/messages", nil)

	filter, err := parseMessageListFilter(req)
	if err != nil {
		t.Fatalf("parseMessageListFilter returned error: %v", err)
	}

	if filter.Limit != 50 {
		t.Fatalf("expected default limit 50, got %d", filter.Limit)
	}
	if filter.Offset != 0 {
		t.Fatalf("expected default offset 0, got %d", filter.Offset)
	}
	if filter.From != nil || filter.To != nil {
		t.Fatal("expected no time filters by default")
	}
}

func TestParseMessageListFilterWithAllParams(t *testing.T) {
	req := httptest.NewRequest(
		http.MethodGet,
		"/v1/messages?search=%20severe%20&eventCode=SVR&messageType=alert&status=processed&source=nwws&from=2026-07-01&to=2026-07-31&limit=25&offset=75",
		nil,
	)

	filter, err := parseMessageListFilter(req)
	if err != nil {
		t.Fatalf("parseMessageListFilter returned error: %v", err)
	}

	if filter.Search != "severe" {
		t.Fatalf("expected trimmed search, got %q", filter.Search)
	}
	if filter.EventCode != "SVR" {
		t.Fatalf("expected event code SVR, got %q", filter.EventCode)
	}
	if filter.MessageType != "alert" {
		t.Fatalf("expected message type alert, got %q", filter.MessageType)
	}
	if filter.Status != "processed" {
		t.Fatalf("expected status processed, got %q", filter.Status)
	}
	if filter.Source != "nwws" {
		t.Fatalf("expected source nwws, got %q", filter.Source)
	}
	if filter.Limit != 25 {
		t.Fatalf("expected limit 25, got %d", filter.Limit)
	}
	if filter.Offset != 75 {
		t.Fatalf("expected offset 75, got %d", filter.Offset)
	}

	expectedFrom := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	expectedTo := time.Date(2026, time.July, 31, 23, 59, 59, int(time.Second-time.Nanosecond), time.UTC)
	if filter.From == nil || !filter.From.Equal(expectedFrom) {
		t.Fatalf("expected from=%s, got %v", expectedFrom, filter.From)
	}
	if filter.To == nil || !filter.To.Equal(expectedTo) {
		t.Fatalf("expected to=%s, got %v", expectedTo, filter.To)
	}
}

func TestParseMessageListFilterRejectsBadInput(t *testing.T) {
	tests := []struct {
		name   string
		target string
	}{
		{name: "invalid from", target: "/v1/messages?from=07-31-2026"},
		{name: "invalid to", target: "/v1/messages?to=nope"},
		{name: "limit not numeric", target: "/v1/messages?limit=abc"},
		{name: "limit too large", target: "/v1/messages?limit=201"},
		{name: "offset negative", target: "/v1/messages?offset=-1"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.target, nil)
			if _, err := parseMessageListFilter(req); err == nil {
				t.Fatalf("expected an error for %q", tc.target)
			}
		})
	}
}

func TestParseLocationListFilter(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/locations?search=%20north%20&activeOnly=true&limit=10&offset=20", nil)

	filter, err := parseLocationListFilter(req)
	if err != nil {
		t.Fatalf("parseLocationListFilter returned error: %v", err)
	}

	if filter.Search != "north" {
		t.Fatalf("expected trimmed search, got %q", filter.Search)
	}
	if filter.ActiveOnly == nil || !*filter.ActiveOnly {
		t.Fatalf("expected ActiveOnly=true, got %v", filter.ActiveOnly)
	}
	if filter.Limit != 10 {
		t.Fatalf("expected limit 10, got %d", filter.Limit)
	}
	if filter.Offset != 20 {
		t.Fatalf("expected offset 20, got %d", filter.Offset)
	}
}

func TestParseLocationListFilterRejectsBadActiveOnly(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/locations?activeOnly=maybe", nil)
	if _, err := parseLocationListFilter(req); err == nil {
		t.Fatal("expected invalid activeOnly value to fail")
	}
}

func TestParseDeliveryListFilter(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/messages/1/deliveries?search=%20smith%20&status=sent&limit=15&offset=30", nil)

	filter, err := parseDeliveryListFilter(req)
	if err != nil {
		t.Fatalf("parseDeliveryListFilter returned error: %v", err)
	}

	if filter.Search != "smith" {
		t.Fatalf("expected trimmed search, got %q", filter.Search)
	}
	if filter.Status != "sent" {
		t.Fatalf("expected status sent, got %q", filter.Status)
	}
	if filter.Limit != 15 {
		t.Fatalf("expected limit 15, got %d", filter.Limit)
	}
	if filter.Offset != 30 {
		t.Fatalf("expected offset 30, got %d", filter.Offset)
	}
}

func TestParseTimestampParam(t *testing.T) {
	t.Run("rfc3339", func(t *testing.T) {
		parsed, err := parseTimestampParam("2026-07-31T14:05:00Z", false)
		if err != nil {
			t.Fatalf("parseTimestampParam returned error: %v", err)
		}

		expected := time.Date(2026, time.July, 31, 14, 5, 0, 0, time.UTC)
		if !parsed.Equal(expected) {
			t.Fatalf("expected %s, got %s", expected, parsed)
		}
	})

	t.Run("date end of day", func(t *testing.T) {
		parsed, err := parseTimestampParam("2026-07-31", true)
		if err != nil {
			t.Fatalf("parseTimestampParam returned error: %v", err)
		}

		expected := time.Date(2026, time.July, 31, 23, 59, 59, int(time.Second-time.Nanosecond), time.UTC)
		if !parsed.Equal(expected) {
			t.Fatalf("expected %s, got %s", expected, parsed)
		}
	})

	t.Run("invalid", func(t *testing.T) {
		if _, err := parseTimestampParam("31/07/2026", false); err == nil {
			t.Fatal("expected invalid timestamp to fail")
		}
	})
}
