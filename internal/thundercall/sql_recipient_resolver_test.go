package thundercall

import (
	"context"
	"sort"
	"testing"

	"thundercall-go/internal/models"
)

func TestSQLRecipientResolverResolveRecipientsByPolygon(t *testing.T) {
	t.Parallel()

	resolver := &SQLRecipientResolver{
		locations: &fakeLocationsMatcher{
			byPolygon: map[string][]models.Location{
				"polygon-a": {
					{ID: 1001, AccountID: 1, Name: "Loc 1", Active: true, IsThunderCallEnabled: true},
					{ID: 1002, AccountID: 1, Name: "Loc 2", Active: true, IsThunderCallEnabled: true},
				},
			},
		},
		userLocations: &fakeUserLocationsRepository{
			byLocationID: map[int64][]models.UserLocation{
				1001: {{UserID: 10, LocationID: 1001, IsThunderCallEnabled: true}},
				1002: {{UserID: 20, LocationID: 1002, IsThunderCallEnabled: true}},
			},
		},
		accountSettings: &fakeAccountSettingsRepository{},
		userSettings:    &fakeUserSettingsRepository{},
	}

	message := &models.Message{
		ID:            1,
		AlertTypeCode: "severe_thunderstorm_warning",
		PolygonWKT:    stringPtr("polygon-a"),
	}

	matches, err := resolver.ResolveRecipients(context.Background(), message)
	if err != nil {
		t.Fatalf("ResolveRecipients() error = %v", err)
	}

	if got := sortedUserIDs(matches); len(got) != 2 || got[0] != 10 || got[1] != 20 {
		t.Fatalf("resolved user IDs = %v, want [10 20]", got)
	}
	for _, match := range matches {
		if !containsChannel(match.Channels, models.ChannelVoice) {
			t.Fatalf("user %d channels = %v, want voice enabled", match.UserID, match.Channels)
		}
	}
}

func TestResolverAndDispatcherUpdateCallsOnlyNetNewPolygonRecipientsForSameEvent(t *testing.T) {
	t.Parallel()

	resolver := &SQLRecipientResolver{
		locations: &fakeLocationsMatcher{
			byPolygon: map[string][]models.Location{
				"polygon-initial": {
					{ID: 1001, AccountID: 1, Name: "Loc 1", Active: true, IsThunderCallEnabled: true},
					{ID: 1002, AccountID: 1, Name: "Loc 2", Active: true, IsThunderCallEnabled: true},
				},
				"polygon-updated": {
					{ID: 1002, AccountID: 1, Name: "Loc 2", Active: true, IsThunderCallEnabled: true},
					{ID: 1003, AccountID: 1, Name: "Loc 3", Active: true, IsThunderCallEnabled: true},
				},
			},
		},
		userLocations: &fakeUserLocationsRepository{
			byLocationID: map[int64][]models.UserLocation{
				1001: {{UserID: 10, LocationID: 1001, IsThunderCallEnabled: true}},
				1002: {{UserID: 20, LocationID: 1002, IsThunderCallEnabled: true}},
				1003: {{UserID: 30, LocationID: 1003, IsThunderCallEnabled: true}},
			},
		},
		accountSettings: &fakeAccountSettingsRepository{},
		userSettings:    &fakeUserSettingsRepository{},
	}

	fixture := newDispatcherFixture()
	fixture.contacts.byUser[30] = []models.UserContactMethod{
		{UserID: 30, Channel: models.ChannelVoice, Destination: "+15550000030", Active: true},
	}

	eventID := int64(500)
	initial := &models.Message{
		ID:            100,
		NWSEventID:    &eventID,
		AlertTypeCode: "severe_thunderstorm_warning",
		MessageType:   "Severe Weather Warning",
		Body:          "initial",
		PolygonWKT:    stringPtr("polygon-initial"),
	}
	updated := &models.Message{
		ID:            101,
		NWSEventID:    &eventID,
		AlertTypeCode: "severe_thunderstorm_warning",
		MessageType:   "Severe Weather Warning",
		Body:          "updated",
		PolygonWKT:    stringPtr("polygon-updated"),
	}

	initialMatches, err := resolver.ResolveRecipients(context.Background(), initial)
	if err != nil {
		t.Fatalf("ResolveRecipients(initial) error = %v", err)
	}
	if got := sortedUserIDs(initialMatches); len(got) != 2 || got[0] != 10 || got[1] != 20 {
		t.Fatalf("initial resolved user IDs = %v, want [10 20]", got)
	}
	if err := fixture.dispatcher.Dispatch(context.Background(), initial, initialMatches); err != nil {
		t.Fatalf("Dispatch(initial) error = %v", err)
	}

	updatedMatches, err := resolver.ResolveRecipients(context.Background(), updated)
	if err != nil {
		t.Fatalf("ResolveRecipients(updated) error = %v", err)
	}
	if got := sortedUserIDs(updatedMatches); len(got) != 2 || got[0] != 20 || got[1] != 30 {
		t.Fatalf("updated resolved user IDs = %v, want [20 30]", got)
	}
	if err := fixture.dispatcher.Dispatch(context.Background(), updated, updatedMatches); err != nil {
		t.Fatalf("Dispatch(updated) error = %v", err)
	}

	if got := len(fixture.sendCalls); got != 3 {
		t.Fatalf("send call count = %d, want 3", got)
	}
	if got := countSendCalls(fixture.sendCalls, "voice:+15550000020"); got != 1 {
		t.Fatalf("user 20 send count = %d, want 1", got)
	}
	if got := countSendCalls(fixture.sendCalls, "voice:+15550000030"); got != 1 {
		t.Fatalf("user 30 send count = %d, want 1", got)
	}
	if got := fixture.usersMessages.statusByMessageUser(101, 20); got != "suppressed" {
		t.Fatalf("updated status for overlapping user = %q, want suppressed", got)
	}
	if got := fixture.usersMessages.statusByMessageUser(101, 30); got != "sent" {
		t.Fatalf("updated status for new user = %q, want sent", got)
	}
}

type fakeLocationsMatcher struct {
	byPolygon map[string][]models.Location
}

func (r *fakeLocationsMatcher) MatchForMessage(_ context.Context, polygonWKT string, _ []string, _ []string) ([]models.Location, error) {
	locations := r.byPolygon[polygonWKT]
	result := make([]models.Location, len(locations))
	copy(result, locations)
	return result, nil
}

type fakeUserLocationsRepository struct {
	byLocationID map[int64][]models.UserLocation
}

func (r *fakeUserLocationsRepository) ListByLocationIDs(_ context.Context, locationIDs []int64) ([]models.UserLocation, error) {
	var result []models.UserLocation
	for _, locationID := range locationIDs {
		result = append(result, r.byLocationID[locationID]...)
	}
	return result, nil
}

type fakeAccountSettingsRepository struct {
	byAccountID map[int64]models.AccountSetting
}

func (r *fakeAccountSettingsRepository) ListByAccountIDsAndMessageType(_ context.Context, accountIDs []int64, _ string) ([]models.AccountSetting, error) {
	var result []models.AccountSetting
	for _, accountID := range accountIDs {
		if setting, ok := r.byAccountID[accountID]; ok {
			result = append(result, setting)
		}
	}
	return result, nil
}

type fakeUserSettingsRepository struct {
	byUserID map[int64]models.UserSetting
}

func (r *fakeUserSettingsRepository) ListByUserIDsAndMessageType(_ context.Context, userIDs []int64, _ string) ([]models.UserSetting, error) {
	var result []models.UserSetting
	for _, userID := range userIDs {
		if setting, ok := r.byUserID[userID]; ok {
			result = append(result, setting)
		}
	}
	return result, nil
}

func sortedUserIDs(matches []UserMatch) []int64 {
	userIDs := make([]int64, 0, len(matches))
	for _, match := range matches {
		userIDs = append(userIDs, match.UserID)
	}
	sort.Slice(userIDs, func(i, j int) bool { return userIDs[i] < userIDs[j] })
	return userIDs
}

func countSendCalls(calls []string, target string) int {
	count := 0
	for _, call := range calls {
		if call == target {
			count++
		}
	}
	return count
}

func stringPtr(value string) *string {
	return &value
}
