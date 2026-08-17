package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLegacyVoiceSettingsFromWarningTypes(t *testing.T) {
	settings, err := legacyVoiceSettingsFromWarningTypes([]int{0, 2, 3})
	if err != nil {
		t.Fatalf("legacyVoiceSettingsFromWarningTypes() error = %v", err)
	}

	tests := map[string]bool{
		"tornado_warning":             true,
		"severe_thunderstorm_warning": true,
		"winter_storm_warning":        true,
		"flash_flood_warning":         false,
		"tropical_storm":              false,
		"special_weather_statement":   false,
		"freeze_warning":              false,
	}

	for messageType, want := range tests {
		if got := settings[messageType]; got != want {
			t.Fatalf("settings[%q] = %t, want %t", messageType, got, want)
		}
	}
}

func TestLegacyVoiceSettingsFromWarningTypesRejectsUnknownCode(t *testing.T) {
	if _, err := legacyVoiceSettingsFromWarningTypes([]int{99}); err == nil {
		t.Fatal("expected unsupported warning type to fail")
	}
}

func TestNormalizePublicSignupPhone(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr string
	}{
		{name: "ten digits", input: "4073530340", want: "+14073530340"},
		{name: "formatted ten digits", input: "(407) 353-0340", want: "+14073530340"},
		{name: "eleven digits", input: "14073530340", want: "+14073530340"},
		{name: "blank", input: "  ", wantErr: "Phone number required"},
		{name: "too short", input: "407353034", wantErr: "Phone number must contain 10 digits"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizePublicSignupPhone(tc.input)
			if tc.wantErr != "" {
				if err == nil || err.Error() != tc.wantErr {
					t.Fatalf("expected error %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizePublicSignupPhone() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("normalizePublicSignupPhone() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLegacyPublicSignupRequestToCreateResolvedUserInput(t *testing.T) {
	request := legacyPublicSignupRequest{
		ExternalID: "RD1234567",
		AccountID:  2,
		FirstName:  "Pat",
		LastName:   "Smith",
		Title:      "Parent",
		Emails: []legacyPublicSignupEmail{{
			EmailAddress: "PAT@example.com",
		}},
		Phones: []legacyPublicSignupPhone{{
			PhoneNumber: "407-353-0340",
		}},
		Addresses: []legacyPublicSignupAddress{{
			Address:       "123 Main St",
			City:          "Tyler",
			StateProvince: "tx",
			ZipPostalCode: "75701",
			ThunderCall: legacyPublicSignupThunderCall{
				WarningTypes: []int{0, 1},
			},
		}},
	}

	input, err := request.toCreateResolvedUserInput()
	if err != nil {
		t.Fatalf("toCreateResolvedUserInput() error = %v", err)
	}

	if input.DisplayName != "Pat Smith" {
		t.Fatalf("DisplayName = %q, want %q", input.DisplayName, "Pat Smith")
	}
	if input.EmailAddress != "pat@example.com" {
		t.Fatalf("EmailAddress = %q, want pat@example.com", input.EmailAddress)
	}
	if input.VoicePhone != "+14073530340" {
		t.Fatalf("VoicePhone = %q, want +14073530340", input.VoicePhone)
	}
	if input.Address.StateCode != "TX" {
		t.Fatalf("Address.StateCode = %q, want TX", input.Address.StateCode)
	}
	if !input.VoiceSettings["tornado_warning"] || !input.VoiceSettings["flash_flood_warning"] {
		t.Fatalf("expected selected warning settings to be enabled: %#v", input.VoiceSettings)
	}
	if input.VoiceSettings["severe_thunderstorm_warning"] {
		t.Fatalf("expected unselected warning setting to be disabled: %#v", input.VoiceSettings)
	}
}

func TestPublicSignupOptionsHandler(t *testing.T) {
	server := NewServer(nil, time.Hour, nil)

	req := httptest.NewRequest(http.MethodOptions, "/api/users/signup", nil)
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("expected 204 response, got %d", recorder.Code)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("expected wildcard CORS origin, got %q", got)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Methods"); got != "POST, OPTIONS" {
		t.Fatalf("unexpected allow methods header %q", got)
	}
}
