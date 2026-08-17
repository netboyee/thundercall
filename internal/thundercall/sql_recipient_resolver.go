package thundercall

import (
	"context"

	"thundercall-go/internal/models"
	accountsettingsrepo "thundercall-go/internal/repositories/accountsettings"
	locationsrepo "thundercall-go/internal/repositories/locations"
	userlocationsrepo "thundercall-go/internal/repositories/userlocations"
	usersettingsrepo "thundercall-go/internal/repositories/usersettings"
)

type locationsMatcher interface {
	MatchForMessage(ctx context.Context, polygonWKT string, fipsCodes []string, nwsZones []string) ([]models.Location, error)
}

type userLocationsRepository interface {
	ListByLocationIDs(ctx context.Context, locationIDs []int64) ([]models.UserLocation, error)
}

type accountSettingsRepository interface {
	ListByAccountIDsAndMessageType(ctx context.Context, accountIDs []int64, messageTypeCode string) ([]models.AccountSetting, error)
}

type userSettingsRepository interface {
	ListByUserIDsAndMessageType(ctx context.Context, userIDs []int64, messageTypeCode string) ([]models.UserSetting, error)
}

type SQLRecipientResolver struct {
	locations       locationsMatcher
	userLocations   userLocationsRepository
	accountSettings accountSettingsRepository
	userSettings    userSettingsRepository
}

func NewSQLRecipientResolver(
	locations *locationsrepo.Repository,
	userLocations *userlocationsrepo.Repository,
	accountSettings *accountsettingsrepo.Repository,
	userSettings *usersettingsrepo.Repository,
) *SQLRecipientResolver {
	return &SQLRecipientResolver{
		locations:       locations,
		userLocations:   userLocations,
		accountSettings: accountSettings,
		userSettings:    userSettings,
	}
}

func (r *SQLRecipientResolver) ResolveRecipients(ctx context.Context, message *models.Message) ([]UserMatch, error) {
	if r.locations == nil || r.userLocations == nil {
		return nil, nil
	}

	locations, err := r.locations.MatchForMessage(ctx, stringValue(message.PolygonWKT), []string(message.FIPSCodes), []string(message.NWSZones))
	if err != nil {
		return nil, err
	}
	if len(locations) == 0 {
		return nil, nil
	}

	locationByID := make(map[int64]models.Location, len(locations))
	locationIDs := make([]int64, 0, len(locations))
	accountIDs := make([]int64, 0, len(locations))
	accountSeen := make(map[int64]struct{}, len(locations))
	for _, location := range locations {
		locationByID[location.ID] = location
		locationIDs = append(locationIDs, location.ID)
		if _, ok := accountSeen[location.AccountID]; !ok {
			accountSeen[location.AccountID] = struct{}{}
			accountIDs = append(accountIDs, location.AccountID)
		}
	}

	subscriptions, err := r.userLocations.ListByLocationIDs(ctx, locationIDs)
	if err != nil {
		return nil, err
	}
	if len(subscriptions) == 0 {
		return nil, nil
	}

	userIDs := uniqueUserIDs(subscriptions)
	accountSettings, err := r.accountSettings.ListByAccountIDsAndMessageType(ctx, accountIDs, message.AlertTypeCode)
	if err != nil {
		return nil, err
	}
	userSettings, err := r.userSettings.ListByUserIDsAndMessageType(ctx, userIDs, message.AlertTypeCode)
	if err != nil {
		return nil, err
	}

	accountSettingsByID := make(map[int64]models.AccountSetting, len(accountSettings))
	for _, setting := range accountSettings {
		accountSettingsByID[setting.AccountID] = setting
	}

	userSettingsByID := make(map[int64]models.UserSetting, len(userSettings))
	for _, setting := range userSettings {
		userSettingsByID[setting.UserID] = setting
	}

	matchesByUser := make(map[int64]*UserMatch, len(subscriptions))
	for _, subscription := range subscriptions {
		if !subscription.IsThunderCallEnabled {
			continue
		}

		location, ok := locationByID[subscription.LocationID]
		if !ok {
			continue
		}

		channels := resolveChannels(accountSettingsByID[location.AccountID], userSettingsByID[subscription.UserID])
		if len(channels) == 0 {
			continue
		}

		match, exists := matchesByUser[subscription.UserID]
		if !exists {
			match = &UserMatch{
				UserID:     subscription.UserID,
				AccountID:  location.AccountID,
				LocationID: &location.ID,
			}
			matchesByUser[subscription.UserID] = match
		}

		for _, channel := range channels {
			if !containsChannel(match.Channels, channel) {
				match.Channels = append(match.Channels, channel)
			}
		}
	}

	matches := make([]UserMatch, 0, len(matchesByUser))
	for _, match := range matchesByUser {
		matches = append(matches, *match)
	}
	return matches, nil
}

func resolveChannels(accountSetting models.AccountSetting, userSetting models.UserSetting) []models.Channel {
	smsEnabled := false
	emailEnabled := false
	voiceEnabled := true

	if accountSetting.AccountID != 0 {
		smsEnabled = accountSetting.SMSEnabled
		emailEnabled = accountSetting.EmailEnabled
		voiceEnabled = accountSetting.VoiceEnabled
	}

	if userSetting.UserID != 0 {
		smsEnabled = userSetting.SMSEnabled
		emailEnabled = userSetting.EmailEnabled
		voiceEnabled = userSetting.VoiceEnabled
	}

	channels := make([]models.Channel, 0, 3)
	if smsEnabled {
		channels = append(channels, models.ChannelSMS)
	}
	if emailEnabled {
		channels = append(channels, models.ChannelEmail)
	}
	if voiceEnabled {
		channels = append(channels, models.ChannelVoice)
	}
	return channels
}

func uniqueUserIDs(subscriptions []models.UserLocation) []int64 {
	seen := make(map[int64]struct{}, len(subscriptions))
	ids := make([]int64, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		if _, ok := seen[subscription.UserID]; ok {
			continue
		}
		seen[subscription.UserID] = struct{}{}
		ids = append(ids, subscription.UserID)
	}
	return ids
}

func containsChannel(channels []models.Channel, target models.Channel) bool {
	for _, channel := range channels {
		if channel == target {
			return true
		}
	}
	return false
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
