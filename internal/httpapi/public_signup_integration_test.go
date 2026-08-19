package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"thundercall-go/internal/geocode"
	"thundercall-go/internal/models"
	accountsrepo "thundercall-go/internal/repositories/accounts"
	locationsrepo "thundercall-go/internal/repositories/locations"
	usercontactmethodsrepo "thundercall-go/internal/repositories/usercontactmethods"
	userlocationsrepo "thundercall-go/internal/repositories/userlocations"
	usersrepo "thundercall-go/internal/repositories/users"
	usersettingsrepo "thundercall-go/internal/repositories/usersettings"
	"thundercall-go/internal/testmysql"
)

func TestHandlePublicSignupCreatesUserLocationContactsAndSettings(t *testing.T) {
	harness := testmysql.Open(t)
	ctx := context.Background()

	account := &models.Account{
		Name:   "KLTV",
		Active: true,
	}
	if err := accountsrepo.New(harness.DB).Create(ctx, account); err != nil {
		t.Fatalf("Create(account) error = %v", err)
	}

	server := NewServer(harness.DB, time.Hour, stubPublicSignupResolver{
		resolved: geocode.ResolvedLocation{
			MatchedAddress: "123 MAIN ST, TYLER, TX, 75701",
			Latitude:       32.3513,
			Longitude:      -95.3011,
			CountyFIPS:     "TXC423",
			NWSZone:        "TXZ149",
		},
	})

	payload := legacyPublicSignupRequest{
		ExternalID: "RD1234567",
		AccountID:  account.ID,
		FirstName:  "Pat",
		LastName:   "Smith",
		Emails: []legacyPublicSignupEmail{{
			EmailAddress: "PAT@example.com",
			EmailType:    "Home",
		}},
		Phones: []legacyPublicSignupPhone{{
			PhoneNumber: "(407) 353-0340",
			PhoneType:   "Home",
		}},
		Addresses: []legacyPublicSignupAddress{{
			Address:       "123 Main St",
			City:          "Tyler",
			StateProvince: "TX",
			ZipPostalCode: "75701",
			Country:       "US",
			AddressType:   "Home",
			ThunderCall: legacyPublicSignupThunderCall{
				WarningTypes: []int{0, 2},
			},
		}},
		LocationIDs: []int64{64623},
		TCall:       true,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal(payload) error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/users/signup", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected 201 response, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	users, err := usersrepo.New(harness.DB).ListByAccountID(ctx, account.ID)
	if err != nil {
		t.Fatalf("ListByAccountID(users) error = %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(users))
	}
	user := users[0]
	if got := stringValue(user.DisplayName); got != "Pat Smith" {
		t.Fatalf("DisplayName = %q, want %q", got, "Pat Smith")
	}

	locations, err := locationsrepo.New(harness.DB).ListByAccountID(ctx, account.ID)
	if err != nil {
		t.Fatalf("ListByAccountID(locations) error = %v", err)
	}
	if len(locations) != 1 {
		t.Fatalf("expected 1 location, got %d", len(locations))
	}
	location := locations[0]
	if got := stringValue(location.CountyFIPS); got != "TXC423" {
		t.Fatalf("CountyFIPS = %q, want TXC423", got)
	}
	if got := stringValue(location.NWSZone); got != "TXZ149" {
		t.Fatalf("NWSZone = %q, want TXZ149", got)
	}

	subscriptions, err := userlocationsrepo.New(harness.DB).ListByUserID(ctx, user.ID)
	if err != nil {
		t.Fatalf("ListByUserID(subscriptions) error = %v", err)
	}
	if len(subscriptions) != 1 {
		t.Fatalf("expected 1 subscription, got %d", len(subscriptions))
	}

	methods, err := usercontactmethodsrepo.New(harness.DB).ListByUserID(ctx, user.ID)
	if err != nil {
		t.Fatalf("ListByUserID(contact methods) error = %v", err)
	}
	if len(methods) != 2 {
		t.Fatalf("expected 2 contact methods, got %d", len(methods))
	}
	destinations := map[models.Channel]string{}
	for _, method := range methods {
		destinations[method.Channel] = method.Destination
	}
	if got := destinations[models.ChannelVoice]; got != "+14073530340" {
		t.Fatalf("voice destination = %q, want +14073530340", got)
	}
	if got := destinations[models.ChannelEmail]; got != "pat@example.com" {
		t.Fatalf("email destination = %q, want pat@example.com", got)
	}

	assertUserSetting(t, harness.DB, user.ID, "tornado_warning", true)
	assertUserSetting(t, harness.DB, user.ID, "severe_thunderstorm_warning", true)
	assertUserSetting(t, harness.DB, user.ID, "flash_flood_warning", false)
	assertUserSetting(t, harness.DB, user.ID, "winter_storm_warning", false)
}

func TestHandlePublicSignupReusesUserAndLocationForSamePhoneAndGeocodedAddress(t *testing.T) {
	harness := testmysql.Open(t)
	ctx := context.Background()

	account := &models.Account{Name: "KWTX", Active: true}
	if err := accountsrepo.New(harness.DB).Create(ctx, account); err != nil {
		t.Fatalf("Create(account) error = %v", err)
	}

	resolver := mappedPublicSignupResolver{
		byAddress: map[string]geocode.ResolvedLocation{
			"123 First St, Waco, TX, 76701": {
				MatchedAddress: "123 FIRST STREET, WACO, TX, 76701",
				Latitude:       31.5493,
				Longitude:      -97.1467,
				CountyFIPS:     "TXC309",
				NWSZone:        "TXZ111",
			},
			"123 First Street, Waco, TX, 76701": {
				MatchedAddress: "123 FIRST STREET, WACO, TX, 76701",
				Latitude:       31.5493,
				Longitude:      -97.1467,
				CountyFIPS:     "TXC309",
				NWSZone:        "TXZ111",
			},
		},
	}
	server := NewServer(harness.DB, time.Hour, resolver)

	postPublicSignup(t, server, legacyPublicSignupRequest{
		ExternalID: "RD1000001",
		AccountID:  account.ID,
		FirstName:  "Ernie",
		LastName:   "Lyon",
		Emails: []legacyPublicSignupEmail{{
			EmailAddress: "ernie@example.com",
		}},
		Phones: []legacyPublicSignupPhone{{
			PhoneNumber: "4073530340",
		}},
		Addresses: []legacyPublicSignupAddress{{
			Address:       "123 First St",
			City:          "Waco",
			StateProvince: "TX",
			ZipPostalCode: "76701",
			ThunderCall:   legacyPublicSignupThunderCall{WarningTypes: []int{0}},
		}},
	})

	postPublicSignup(t, server, legacyPublicSignupRequest{
		ExternalID: "RD1000002",
		AccountID:  account.ID,
		FirstName:  "Ernie",
		LastName:   "Lyon",
		Emails: []legacyPublicSignupEmail{{
			EmailAddress: "ernie@example.com",
		}},
		Phones: []legacyPublicSignupPhone{{
			PhoneNumber: "(407) 353-0340",
		}},
		Addresses: []legacyPublicSignupAddress{{
			Address:       "123 First Street",
			City:          "Waco",
			StateProvince: "TX",
			ZipPostalCode: "76701",
			ThunderCall:   legacyPublicSignupThunderCall{WarningTypes: []int{1}},
		}},
	})

	users, err := usersrepo.New(harness.DB).ListByAccountID(ctx, account.ID)
	if err != nil {
		t.Fatalf("ListByAccountID(users) error = %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected 1 user after repeat signup, got %d", len(users))
	}

	locations, err := locationsrepo.New(harness.DB).ListByAccountID(ctx, account.ID)
	if err != nil {
		t.Fatalf("ListByAccountID(locations) error = %v", err)
	}
	if len(locations) != 1 {
		t.Fatalf("expected 1 location after equivalent geocoded signup, got %d", len(locations))
	}
	if got := stringValue(locations[0].AddressLine1); got != "123 FIRST STREET" {
		t.Fatalf("AddressLine1 = %q, want canonical matched address", got)
	}

	subscriptions, err := userlocationsrepo.New(harness.DB).ListByUserID(ctx, users[0].ID)
	if err != nil {
		t.Fatalf("ListByUserID(subscriptions) error = %v", err)
	}
	if len(subscriptions) != 1 {
		t.Fatalf("expected 1 subscription after equivalent geocoded signup, got %d", len(subscriptions))
	}

	methods, err := usercontactmethodsrepo.New(harness.DB).ListByUserID(ctx, users[0].ID)
	if err != nil {
		t.Fatalf("ListByUserID(contact methods) error = %v", err)
	}
	if len(methods) != 2 {
		t.Fatalf("expected 2 contact methods after repeat signup, got %d", len(methods))
	}

	assertUserSetting(t, harness.DB, users[0].ID, "tornado_warning", false)
	assertUserSetting(t, harness.DB, users[0].ID, "flash_flood_warning", true)
}

func TestHandlePublicSignupReusesUserAndAddsLocationForDifferentGeocodedAddress(t *testing.T) {
	harness := testmysql.Open(t)
	ctx := context.Background()

	account := &models.Account{Name: "KTRE", Active: true}
	if err := accountsrepo.New(harness.DB).Create(ctx, account); err != nil {
		t.Fatalf("Create(account) error = %v", err)
	}

	resolver := mappedPublicSignupResolver{
		byAddress: map[string]geocode.ResolvedLocation{
			"100 Oak St, Tyler, TX, 75701": {
				MatchedAddress: "100 OAK STREET, TYLER, TX, 75701",
				Latitude:       32.3513,
				Longitude:      -95.3011,
				CountyFIPS:     "TXC423",
				NWSZone:        "TXZ149",
			},
			"200 Pine St, Tyler, TX, 75703": {
				MatchedAddress: "200 PINE STREET, TYLER, TX, 75703",
				Latitude:       32.2869,
				Longitude:      -95.3034,
				CountyFIPS:     "TXC423",
				NWSZone:        "TXZ149",
			},
		},
	}
	server := NewServer(harness.DB, time.Hour, resolver)

	postPublicSignup(t, server, legacyPublicSignupRequest{
		ExternalID: "RD2000001",
		AccountID:  account.ID,
		FirstName:  "Pat",
		LastName:   "Smith",
		Emails: []legacyPublicSignupEmail{{
			EmailAddress: "pat@example.com",
		}},
		Phones: []legacyPublicSignupPhone{{
			PhoneNumber: "4073530340",
		}},
		Addresses: []legacyPublicSignupAddress{{
			Address:       "100 Oak St",
			City:          "Tyler",
			StateProvince: "TX",
			ZipPostalCode: "75701",
			ThunderCall:   legacyPublicSignupThunderCall{WarningTypes: []int{0}},
		}},
	})

	postPublicSignup(t, server, legacyPublicSignupRequest{
		ExternalID: "RD2000002",
		AccountID:  account.ID,
		FirstName:  "Pat",
		LastName:   "Smith",
		Emails: []legacyPublicSignupEmail{{
			EmailAddress: "pat@example.com",
		}},
		Phones: []legacyPublicSignupPhone{{
			PhoneNumber: "1-407-353-0340",
		}},
		Addresses: []legacyPublicSignupAddress{{
			Address:       "200 Pine St",
			City:          "Tyler",
			StateProvince: "TX",
			ZipPostalCode: "75703",
			ThunderCall:   legacyPublicSignupThunderCall{WarningTypes: []int{0, 2}},
		}},
	})

	users, err := usersrepo.New(harness.DB).ListByAccountID(ctx, account.ID)
	if err != nil {
		t.Fatalf("ListByAccountID(users) error = %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected 1 user after second-address signup, got %d", len(users))
	}

	locations, err := locationsrepo.New(harness.DB).ListByAccountID(ctx, account.ID)
	if err != nil {
		t.Fatalf("ListByAccountID(locations) error = %v", err)
	}
	if len(locations) != 2 {
		t.Fatalf("expected 2 locations after second-address signup, got %d", len(locations))
	}

	subscriptions, err := userlocationsrepo.New(harness.DB).ListByUserID(ctx, users[0].ID)
	if err != nil {
		t.Fatalf("ListByUserID(subscriptions) error = %v", err)
	}
	if len(subscriptions) != 2 {
		t.Fatalf("expected 2 subscriptions after second-address signup, got %d", len(subscriptions))
	}
	if !subscriptions[0].IsPrimary {
		t.Fatalf("expected first subscription to remain primary")
	}
	if subscriptions[1].IsPrimary {
		t.Fatalf("expected second subscription to default to non-primary")
	}

	assertUserSetting(t, harness.DB, users[0].ID, "tornado_warning", true)
	assertUserSetting(t, harness.DB, users[0].ID, "severe_thunderstorm_warning", true)
}

func TestHandlePublicSignupCreatesPendingLocationWhenResolverFailsUpstream(t *testing.T) {
	harness := testmysql.Open(t)
	ctx := context.Background()

	account := &models.Account{Name: "KWTX", Active: true}
	if err := accountsrepo.New(harness.DB).Create(ctx, account); err != nil {
		t.Fatalf("Create(account) error = %v", err)
	}

	server := NewServer(harness.DB, time.Hour, errorPublicSignupResolver{
		err: errors.New("upstream geocoder timeout"),
	})

	recorder := postPublicSignupExpect(t, server, legacyPublicSignupRequest{
		ExternalID: "RD3000001",
		AccountID:  account.ID,
		FirstName:  "Ernie",
		LastName:   "Lyon",
		Emails: []legacyPublicSignupEmail{{
			EmailAddress: "ernie@example.com",
		}},
		Phones: []legacyPublicSignupPhone{{
			PhoneNumber: "4073530340",
		}},
		Addresses: []legacyPublicSignupAddress{{
			Address:       "4368 Berry Oak Dr",
			City:          "Apopka",
			StateProvince: "FL",
			ZipPostalCode: "32712",
			ThunderCall:   legacyPublicSignupThunderCall{WarningTypes: []int{0, 2}},
		}},
	}, http.StatusCreated)

	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("Unmarshal(response) error = %v", err)
	}
	if got := response["message"]; got != "Record created; location enrichment pending." {
		t.Fatalf("message = %#v, want pending enrichment message", got)
	}
	if got := response["enrichmentPending"]; got != true {
		t.Fatalf("enrichmentPending = %#v, want true", got)
	}
	if _, ok := response["resolved"]; ok {
		t.Fatalf("resolved payload should be omitted when geocoding fails upstream")
	}

	users, err := usersrepo.New(harness.DB).ListByAccountID(ctx, account.ID)
	if err != nil {
		t.Fatalf("ListByAccountID(users) error = %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(users))
	}

	locations, err := locationsrepo.New(harness.DB).ListByAccountID(ctx, account.ID)
	if err != nil {
		t.Fatalf("ListByAccountID(locations) error = %v", err)
	}
	if len(locations) != 1 {
		t.Fatalf("expected 1 location, got %d", len(locations))
	}
	location := locations[0]
	if got := stringValue(location.AddressLine1); got != "4368 Berry Oak Dr" {
		t.Fatalf("AddressLine1 = %q, want raw signup address", got)
	}
	if location.Latitude != nil || location.Longitude != nil {
		t.Fatalf("expected nil coordinates for pending enrichment location")
	}
	if location.CountyFIPS != nil || location.NWSZone != nil {
		t.Fatalf("expected nil county_fips and nws_zone for pending enrichment location")
	}
	if location.CoverageWKT != nil {
		t.Fatalf("expected nil coverage geometry for pending enrichment location")
	}
}

func TestHandlePublicSignupRejectsAddressNoMatch(t *testing.T) {
	harness := testmysql.Open(t)
	ctx := context.Background()

	account := &models.Account{Name: "KLTV", Active: true}
	if err := accountsrepo.New(harness.DB).Create(ctx, account); err != nil {
		t.Fatalf("Create(account) error = %v", err)
	}

	server := NewServer(harness.DB, time.Hour, errorPublicSignupResolver{
		err: geocode.ErrNoMatch,
	})

	recorder := postPublicSignupExpect(t, server, legacyPublicSignupRequest{
		ExternalID: "RD3000002",
		AccountID:  account.ID,
		FirstName:  "Pat",
		LastName:   "Smith",
		Emails: []legacyPublicSignupEmail{{
			EmailAddress: "pat@example.com",
		}},
		Phones: []legacyPublicSignupPhone{{
			PhoneNumber: "9035551212",
		}},
		Addresses: []legacyPublicSignupAddress{{
			Address:       "999 Unknown Rd",
			City:          "Tyler",
			StateProvince: "TX",
			ZipPostalCode: "75701",
			ThunderCall:   legacyPublicSignupThunderCall{WarningTypes: []int{2}},
		}},
	}, http.StatusUnprocessableEntity)

	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("Unmarshal(response) error = %v", err)
	}
	if got := response["message"]; got != "Address could not be geocoded." {
		t.Fatalf("message = %#v, want address could not be geocoded", got)
	}

	users, err := usersrepo.New(harness.DB).ListByAccountID(ctx, account.ID)
	if err != nil {
		t.Fatalf("ListByAccountID(users) error = %v", err)
	}
	if len(users) != 0 {
		t.Fatalf("expected 0 users after no-match rejection, got %d", len(users))
	}

	locations, err := locationsrepo.New(harness.DB).ListByAccountID(ctx, account.ID)
	if err != nil {
		t.Fatalf("ListByAccountID(locations) error = %v", err)
	}
	if len(locations) != 0 {
		t.Fatalf("expected 0 locations after no-match rejection, got %d", len(locations))
	}
}

func TestHandlePublicSignupEnrichesPendingLocationWhenResolverRecovers(t *testing.T) {
	harness := testmysql.Open(t)
	ctx := context.Background()

	account := &models.Account{Name: "KTRE", Active: true}
	if err := accountsrepo.New(harness.DB).Create(ctx, account); err != nil {
		t.Fatalf("Create(account) error = %v", err)
	}

	pendingServer := NewServer(harness.DB, time.Hour, errorPublicSignupResolver{
		err: errors.New("weather.gov unavailable"),
	})
	postPublicSignup(t, pendingServer, legacyPublicSignupRequest{
		ExternalID: "RD3000003",
		AccountID:  account.ID,
		FirstName:  "Ernie",
		LastName:   "Lyon",
		Emails: []legacyPublicSignupEmail{{
			EmailAddress: "ernie@example.com",
		}},
		Phones: []legacyPublicSignupPhone{{
			PhoneNumber: "4073530340",
		}},
		Addresses: []legacyPublicSignupAddress{{
			Address:       "123 First St",
			City:          "Waco",
			StateProvince: "TX",
			ZipPostalCode: "76701",
			ThunderCall:   legacyPublicSignupThunderCall{WarningTypes: []int{0}},
		}},
	})

	resolvedServer := NewServer(harness.DB, time.Hour, mappedPublicSignupResolver{
		byAddress: map[string]geocode.ResolvedLocation{
			"123 First St, Waco, TX, 76701": {
				MatchedAddress: "123 FIRST STREET, WACO, TX, 76701",
				Latitude:       31.5493,
				Longitude:      -97.1467,
				CountyFIPS:     "TXC309",
				NWSZone:        "TXZ111",
			},
		},
	})
	postPublicSignup(t, resolvedServer, legacyPublicSignupRequest{
		ExternalID: "RD3000004",
		AccountID:  account.ID,
		FirstName:  "Ernie",
		LastName:   "Lyon",
		Emails: []legacyPublicSignupEmail{{
			EmailAddress: "ernie@example.com",
		}},
		Phones: []legacyPublicSignupPhone{{
			PhoneNumber: "(407) 353-0340",
		}},
		Addresses: []legacyPublicSignupAddress{{
			Address:       "123 First St",
			City:          "Waco",
			StateProvince: "TX",
			ZipPostalCode: "76701",
			ThunderCall:   legacyPublicSignupThunderCall{WarningTypes: []int{1}},
		}},
	})

	users, err := usersrepo.New(harness.DB).ListByAccountID(ctx, account.ID)
	if err != nil {
		t.Fatalf("ListByAccountID(users) error = %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected 1 user after resolver recovery, got %d", len(users))
	}

	locations, err := locationsrepo.New(harness.DB).ListByAccountID(ctx, account.ID)
	if err != nil {
		t.Fatalf("ListByAccountID(locations) error = %v", err)
	}
	if len(locations) != 1 {
		t.Fatalf("expected 1 location after resolver recovery, got %d", len(locations))
	}
	location := locations[0]
	if got := stringValue(location.AddressLine1); got != "123 FIRST STREET" {
		t.Fatalf("AddressLine1 = %q, want canonical matched address after enrichment", got)
	}
	if got := stringValue(location.CountyFIPS); got != "TXC309" {
		t.Fatalf("CountyFIPS = %q, want TXC309", got)
	}
	if got := stringValue(location.NWSZone); got != "TXZ111" {
		t.Fatalf("NWSZone = %q, want TXZ111", got)
	}
	if location.Latitude == nil || location.Longitude == nil {
		t.Fatalf("expected coordinates after resolver recovery")
	}
}

func TestHandlePublicSignupCreatesLocationWhenEnrichmentIsPartial(t *testing.T) {
	harness := testmysql.Open(t)
	ctx := context.Background()

	account := &models.Account{Name: "KWTX", Active: true}
	if err := accountsrepo.New(harness.DB).Create(ctx, account); err != nil {
		t.Fatalf("Create(account) error = %v", err)
	}

	server := NewServer(harness.DB, time.Hour, stubPublicSignupResolver{
		resolved: geocode.ResolvedLocation{
			MatchedAddress: "500 MARKET ST, WACO, TX, 76701",
			Latitude:       31.5562,
			Longitude:      -97.1335,
			CountyFIPS:     "TXC309",
		},
	})

	recorder := postPublicSignupExpect(t, server, legacyPublicSignupRequest{
		ExternalID: "RD3000005",
		AccountID:  account.ID,
		FirstName:  "Jane",
		LastName:   "Doe",
		Emails: []legacyPublicSignupEmail{{
			EmailAddress: "jane@example.com",
		}},
		Phones: []legacyPublicSignupPhone{{
			PhoneNumber: "2545550101",
		}},
		Addresses: []legacyPublicSignupAddress{{
			Address:       "500 Market St",
			City:          "Waco",
			StateProvince: "TX",
			ZipPostalCode: "76701",
			ThunderCall:   legacyPublicSignupThunderCall{WarningTypes: []int{0}},
		}},
	}, http.StatusCreated)

	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("Unmarshal(response) error = %v", err)
	}
	if got := response["enrichmentPending"]; got != true {
		t.Fatalf("enrichmentPending = %#v, want true for partial enrichment", got)
	}

	locations, err := locationsrepo.New(harness.DB).ListByAccountID(ctx, account.ID)
	if err != nil {
		t.Fatalf("ListByAccountID(locations) error = %v", err)
	}
	if len(locations) != 1 {
		t.Fatalf("expected 1 location, got %d", len(locations))
	}
	location := locations[0]
	if got := stringValue(location.CountyFIPS); got != "TXC309" {
		t.Fatalf("CountyFIPS = %q, want TXC309", got)
	}
	if location.NWSZone != nil {
		t.Fatalf("expected nil nws_zone when only partial enrichment is available")
	}
	if location.Latitude == nil || location.Longitude == nil {
		t.Fatalf("expected coordinates to be stored when address geocoding succeeds")
	}
}

type stubPublicSignupResolver struct {
	resolved geocode.ResolvedLocation
}

func (s stubPublicSignupResolver) ResolveAddress(_ context.Context, _ geocode.Address) (geocode.ResolvedLocation, error) {
	return s.resolved, nil
}

func (s stubPublicSignupResolver) ResolveCoordinates(_ context.Context, latitude float64, longitude float64) (geocode.ResolvedLocation, error) {
	return geocode.ResolvedLocation{
		Latitude:   latitude,
		Longitude:  longitude,
		CountyFIPS: s.resolved.CountyFIPS,
		NWSZone:    s.resolved.NWSZone,
	}, nil
}

type mappedPublicSignupResolver struct {
	byAddress map[string]geocode.ResolvedLocation
}

func (m mappedPublicSignupResolver) ResolveAddress(_ context.Context, address geocode.Address) (geocode.ResolvedLocation, error) {
	resolved, ok := m.byAddress[address.OneLine()]
	if !ok {
		return geocode.ResolvedLocation{}, fmt.Errorf("unexpected address lookup %q", address.OneLine())
	}
	return resolved, nil
}

func (m mappedPublicSignupResolver) ResolveCoordinates(_ context.Context, latitude float64, longitude float64) (geocode.ResolvedLocation, error) {
	return geocode.ResolvedLocation{
		Latitude:  latitude,
		Longitude: longitude,
	}, nil
}

type errorPublicSignupResolver struct {
	err error
}

func (e errorPublicSignupResolver) ResolveAddress(_ context.Context, _ geocode.Address) (geocode.ResolvedLocation, error) {
	return geocode.ResolvedLocation{}, e.err
}

func (e errorPublicSignupResolver) ResolveCoordinates(_ context.Context, latitude float64, longitude float64) (geocode.ResolvedLocation, error) {
	return geocode.ResolvedLocation{
		Latitude:  latitude,
		Longitude: longitude,
	}, nil
}

func postPublicSignup(t *testing.T, server *Server, payload legacyPublicSignupRequest) {
	t.Helper()

	postPublicSignupExpect(t, server, payload, http.StatusCreated)
}

func postPublicSignupExpect(t *testing.T, server *Server, payload legacyPublicSignupRequest, wantStatus int) *httptest.ResponseRecorder {
	t.Helper()

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal(payload) error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/users/signup", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, req)

	if recorder.Code != wantStatus {
		t.Fatalf("expected %d response, got %d body=%s", wantStatus, recorder.Code, recorder.Body.String())
	}

	return recorder
}

func assertUserSetting(t *testing.T, db *sql.DB, userID int64, messageType string, want bool) {
	t.Helper()

	setting, err := usersettingsrepo.New(db).GetByUserAndMessageType(t.Context(), userID, messageType)
	if err != nil {
		t.Fatalf("GetByUserAndMessageType(%q) error = %v", messageType, err)
	}
	if setting == nil {
		t.Fatalf("expected user setting for %q", messageType)
	}
	if setting.VoiceEnabled != want {
		t.Fatalf("VoiceEnabled for %q = %t, want %t", messageType, setting.VoiceEnabled, want)
	}
}
