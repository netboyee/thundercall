package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"thundercall-go/internal/geocode"
	"thundercall-go/internal/models"
	locationsrepo "thundercall-go/internal/repositories/locations"
	usercontactmethodsrepo "thundercall-go/internal/repositories/usercontactmethods"
	userlocationsrepo "thundercall-go/internal/repositories/userlocations"
	usersrepo "thundercall-go/internal/repositories/users"
	usersettingsrepo "thundercall-go/internal/repositories/usersettings"
)

var legacyPublicSignupWarningTypeMap = map[int]string{
	0: "tornado_warning",
	1: "flash_flood_warning",
	2: "severe_thunderstorm_warning",
	3: "winter_storm_warning",
	4: "tropical_storm",
	5: "special_weather_statement",
	6: "freeze_warning",
}

var legacyPublicSignupSupportedMessageTypes = []string{
	"tornado_warning",
	"flash_flood_warning",
	"severe_thunderstorm_warning",
	"winter_storm_warning",
	"tropical_storm",
	"special_weather_statement",
	"freeze_warning",
}

type legacyPublicSignupRequest struct {
	ExternalID  string                      `json:"externalId"`
	AccountID   int64                       `json:"accountId"`
	FirstName   string                      `json:"firstName"`
	LastName    string                      `json:"lastName"`
	DisplayName string                      `json:"displayName"`
	Title       string                      `json:"title"`
	TCall       bool                        `json:"tcall"`
	Emails      []legacyPublicSignupEmail   `json:"emails"`
	Phones      []legacyPublicSignupPhone   `json:"phones"`
	Addresses   []legacyPublicSignupAddress `json:"addresses"`
	LocationIDs []int64                     `json:"locationIds"`
}

type legacyPublicSignupEmail struct {
	EmailAddress string `json:"emailAddress"`
	EmailType    string `json:"emailType"`
}

type legacyPublicSignupPhone struct {
	PhoneNumber string `json:"phoneNumber"`
	Extension   string `json:"extension"`
	PhoneType   string `json:"phoneType"`
}

type legacyPublicSignupAddress struct {
	Address       string                        `json:"address"`
	Address2      string                        `json:"address2"`
	City          string                        `json:"city"`
	StateProvince string                        `json:"stateProvince"`
	ZipPostalCode string                        `json:"zipPostalCode"`
	Country       string                        `json:"country"`
	AddressType   string                        `json:"addressType"`
	ThunderCall   legacyPublicSignupThunderCall `json:"thundercall"`
}

type legacyPublicSignupThunderCall struct {
	PhoneSetting legacyPublicSignupPhoneSetting `json:"phoneSetting"`
	WarningTypes []int                          `json:"warningTypes"`
}

type legacyPublicSignupPhoneSetting struct {
	Name       string `json:"name"`
	PhoneType  string `json:"phoneType"`
	Email      int    `json:"email"`
	EnableText bool   `json:"enableText"`
}

type createResolvedUserInput struct {
	ExternalID        string
	FirstName         string
	LastName          string
	DisplayName       string
	Title             string
	VoicePhone        string
	EmailAddress      string
	LocationName      string
	SubscriptionType  string
	IsPrimaryLocation *bool
	Address           addressRequest
	VoiceSettings     map[string]bool
}

func (s *Server) handlePublicSignup(w http.ResponseWriter, r *http.Request) {
	writePublicSignupCORSHeaders(w)

	if s.db == nil || s.resolver == nil || s.accounts == nil {
		writePublicSignupError(w, http.StatusInternalServerError, "Public signup is not configured.")
		return
	}

	var request legacyPublicSignupRequest
	if err := decodeJSON(r, &request); err != nil {
		writePublicSignupError(w, http.StatusBadRequest, "Invalid JSON body.")
		return
	}
	if request.AccountID <= 0 {
		writePublicSignupError(w, http.StatusBadRequest, "Account ID required")
		return
	}

	input, err := request.toCreateResolvedUserInput()
	if err != nil {
		writePublicSignupError(w, http.StatusBadRequest, err.Error())
		return
	}

	account, err := s.resolvePublicSignupAccount(r.Context(), request.AccountID)
	if err != nil {
		writePublicSignupError(w, http.StatusInternalServerError, "Failed to resolve account.")
		return
	}
	if account == nil {
		writePublicSignupError(w, http.StatusNotFound, "Account not found.")
		return
	}

	resolved, err := s.resolver.ResolveAddress(r.Context(), input.Address.toGeocodeAddress())
	if err != nil {
		if errors.Is(err, geocode.ErrNoMatch) {
			writePublicSignupError(w, http.StatusUnprocessableEntity, "Address could not be geocoded.")
			return
		}
		writePublicSignupError(w, http.StatusBadGateway, "Failed to geocode address.")
		return
	}
	if strings.TrimSpace(resolved.CountyFIPS) == "" || strings.TrimSpace(resolved.NWSZone) == "" {
		writePublicSignupError(w, http.StatusUnprocessableEntity, "Address could not be enriched with county FIPS and NWS zone.")
		return
	}

	user, location, subscription, methods, err := s.createUserWithResolvedLocation(r.Context(), account.ID, input, resolved)
	if err != nil {
		writePublicSignupError(w, http.StatusInternalServerError, "Failed to create record.")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"message":        "Record created.",
		"user":           userResponse(user),
		"location":       locationResponse(location),
		"subscription":   subscriptionResponse(subscription),
		"contactMethods": contactMethodResponses(methods),
		"resolved":       resolvedLocationPayload(resolved),
	})
}

func (s *Server) handlePublicSignupOptions(w http.ResponseWriter, _ *http.Request) {
	writePublicSignupCORSHeaders(w)
	w.WriteHeader(http.StatusNoContent)
}

func writePublicSignupCORSHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type")
	w.Header().Set("Access-Control-Max-Age", "86400")
}

func writePublicSignupError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{
		"message": message,
	})
}

func (r legacyPublicSignupRequest) toCreateResolvedUserInput() (createResolvedUserInput, error) {
	if strings.TrimSpace(r.FirstName) == "" {
		return createResolvedUserInput{}, errors.New("First name required")
	}
	if strings.TrimSpace(r.LastName) == "" {
		return createResolvedUserInput{}, errors.New("Last name required")
	}

	email := firstNonBlankEmail(r.Emails)
	if email == "" {
		return createResolvedUserInput{}, errors.New("Email address required")
	}

	voicePhone, err := normalizePublicSignupPhone(firstNonBlankPhone(r.Phones))
	if err != nil {
		return createResolvedUserInput{}, err
	}

	address, warningTypes, err := firstSignupAddress(r.Addresses)
	if err != nil {
		return createResolvedUserInput{}, err
	}

	voiceSettings, err := legacyVoiceSettingsFromWarningTypes(warningTypes)
	if err != nil {
		return createResolvedUserInput{}, err
	}

	displayName := strings.TrimSpace(r.DisplayName)
	if displayName == "" {
		displayName = strings.TrimSpace(strings.Join([]string{
			strings.TrimSpace(r.FirstName),
			strings.TrimSpace(r.LastName),
		}, " "))
	}

	return createResolvedUserInput{
		ExternalID:        strings.TrimSpace(r.ExternalID),
		FirstName:         strings.TrimSpace(r.FirstName),
		LastName:          strings.TrimSpace(r.LastName),
		DisplayName:       displayName,
		Title:             strings.TrimSpace(r.Title),
		VoicePhone:        voicePhone,
		EmailAddress:      strings.ToLower(strings.TrimSpace(email)),
		SubscriptionType:  "address",
		IsPrimaryLocation: boolPtr(true),
		Address:           address,
		VoiceSettings:     voiceSettings,
	}, nil
}

func firstNonBlankEmail(values []legacyPublicSignupEmail) string {
	for _, value := range values {
		if email := strings.TrimSpace(value.EmailAddress); email != "" {
			return email
		}
	}
	return ""
}

func firstNonBlankPhone(values []legacyPublicSignupPhone) string {
	for _, value := range values {
		if phone := strings.TrimSpace(value.PhoneNumber); phone != "" {
			return phone
		}
	}
	return ""
}

func firstSignupAddress(values []legacyPublicSignupAddress) (addressRequest, []int, error) {
	if len(values) == 0 {
		return addressRequest{}, nil, errors.New("Address required")
	}

	address := values[0]
	if strings.TrimSpace(address.Address) == "" {
		return addressRequest{}, nil, errors.New("Address required")
	}
	if strings.TrimSpace(address.City) == "" {
		return addressRequest{}, nil, errors.New("City required")
	}
	if strings.TrimSpace(address.StateProvince) == "" {
		return addressRequest{}, nil, errors.New("State Required")
	}
	if strings.TrimSpace(address.ZipPostalCode) == "" {
		return addressRequest{}, nil, errors.New("Zip Code Required")
	}
	if len(address.ThunderCall.WarningTypes) == 0 {
		return addressRequest{}, nil, errors.New("At least one warning must be selected")
	}

	return addressRequest{
		Line1:      strings.TrimSpace(address.Address),
		Line2:      strings.TrimSpace(address.Address2),
		City:       strings.TrimSpace(address.City),
		StateCode:  strings.ToUpper(strings.TrimSpace(address.StateProvince)),
		PostalCode: strings.TrimSpace(address.ZipPostalCode),
	}, address.ThunderCall.WarningTypes, nil
}

func normalizePublicSignupPhone(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("Phone number required")
	}

	digits := make([]rune, 0, len(value))
	for _, r := range value {
		if r >= '0' && r <= '9' {
			digits = append(digits, r)
		}
	}

	switch {
	case len(digits) == 10:
		return "+1" + string(digits), nil
	case len(digits) == 11 && digits[0] == '1':
		return "+" + string(digits), nil
	default:
		return "", errors.New("Phone number must contain 10 digits")
	}
}

func legacyVoiceSettingsFromWarningTypes(warningTypes []int) (map[string]bool, error) {
	selected := make(map[string]struct{}, len(warningTypes))
	for _, value := range warningTypes {
		messageType, ok := legacyPublicSignupWarningTypeMap[value]
		if !ok {
			return nil, fmt.Errorf("Unsupported warning type: %d", value)
		}
		selected[messageType] = struct{}{}
	}

	settings := make(map[string]bool, len(legacyPublicSignupSupportedMessageTypes))
	for _, messageType := range legacyPublicSignupSupportedMessageTypes {
		_, ok := selected[messageType]
		settings[messageType] = ok
	}
	return settings, nil
}

func (s *Server) resolvePublicSignupAccount(ctx context.Context, accountID int64) (*models.Account, error) {
	account, err := s.accounts.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account == nil || !account.Active {
		return nil, nil
	}
	return account, nil
}

func (s *Server) createUserWithResolvedLocation(ctx context.Context, accountID int64, input createResolvedUserInput, resolved geocode.ResolvedLocation) (*models.User, *models.Location, *models.UserLocation, []models.UserContactMethod, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	defer tx.Rollback()

	users := usersrepo.NewWithDBTX(tx)
	locations := locationsrepo.NewWithDBTX(tx)
	userLocations := userlocationsrepo.NewWithDBTX(tx)
	contactMethods := usercontactmethodsrepo.NewWithDBTX(tx)
	userSettings := usersettingsrepo.NewWithDBTX(tx)

	displayName := strings.TrimSpace(input.DisplayName)
	if displayName == "" {
		displayName = strings.TrimSpace(strings.Join([]string{
			strings.TrimSpace(input.FirstName),
			strings.TrimSpace(input.LastName),
		}, " "))
	}

	user := &models.User{
		AccountID:   accountID,
		ExternalID:  nullableTrimmedString(input.ExternalID),
		FirstName:   nullableTrimmedString(input.FirstName),
		LastName:    nullableTrimmedString(input.LastName),
		DisplayName: nullableTrimmedString(displayName),
		Title:       nullableTrimmedString(input.Title),
		Active:      true,
	}
	if err := users.Create(ctx, user); err != nil {
		return nil, nil, nil, nil, err
	}

	coverageWKT := geocode.PointWKT(resolved.Latitude, resolved.Longitude)
	locationName := strings.TrimSpace(input.LocationName)
	if locationName == "" {
		locationName = defaultLocationName(user, input.Address)
	}

	latitude := resolved.Latitude
	longitude := resolved.Longitude
	location := &models.Location{
		AccountID:            accountID,
		Name:                 locationName,
		AddressLine1:         nullableTrimmedString(input.Address.Line1),
		AddressLine2:         nullableTrimmedString(input.Address.Line2),
		City:                 nullableTrimmedString(input.Address.City),
		StateCode:            nullableTrimmedString(strings.ToUpper(input.Address.StateCode)),
		PostalCode:           nullableTrimmedString(input.Address.PostalCode),
		CountyFIPS:           nullableTrimmedString(resolved.CountyFIPS),
		NWSZone:              nullableTrimmedString(resolved.NWSZone),
		Latitude:             &latitude,
		Longitude:            &longitude,
		CoverageWKT:          &coverageWKT,
		IsThunderCallEnabled: true,
		Active:               true,
	}
	if err := locations.Create(ctx, location); err != nil {
		return nil, nil, nil, nil, err
	}

	subscriptionType := strings.TrimSpace(input.SubscriptionType)
	if subscriptionType == "" {
		subscriptionType = "address"
	}
	isPrimary := input.IsPrimaryLocation == nil || *input.IsPrimaryLocation
	subscription := &models.UserLocation{
		UserID:               user.ID,
		LocationID:           location.ID,
		SubscriptionType:     subscriptionType,
		IsPrimary:            isPrimary,
		IsThunderCallEnabled: true,
	}
	if err := userLocations.Create(ctx, subscription); err != nil {
		return nil, nil, nil, nil, err
	}

	methods := make([]models.UserContactMethod, 0, 2)
	if voicePhone := strings.TrimSpace(input.VoicePhone); voicePhone != "" {
		method := models.UserContactMethod{
			UserID:      user.ID,
			Channel:     models.ChannelVoice,
			Destination: voicePhone,
			IsPrimary:   true,
			IsVerified:  false,
			Active:      true,
		}
		if err := contactMethods.Create(ctx, &method); err != nil {
			return nil, nil, nil, nil, err
		}
		methods = append(methods, method)
	}

	if emailAddress := strings.TrimSpace(input.EmailAddress); emailAddress != "" {
		method := models.UserContactMethod{
			UserID:      user.ID,
			Channel:     models.ChannelEmail,
			Destination: strings.ToLower(emailAddress),
			IsPrimary:   true,
			IsVerified:  false,
			Active:      true,
		}
		if err := contactMethods.Create(ctx, &method); err != nil {
			return nil, nil, nil, nil, err
		}
		methods = append(methods, method)
	}

	if len(input.VoiceSettings) > 0 {
		keys := make([]string, 0, len(input.VoiceSettings))
		for key := range input.VoiceSettings {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		for _, key := range keys {
			if err := userSettings.Upsert(ctx, &models.UserSetting{
				UserID:          user.ID,
				MessageTypeCode: key,
				VoiceEnabled:    input.VoiceSettings[key],
			}); err != nil {
				return nil, nil, nil, nil, err
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, nil, nil, err
	}
	return user, location, subscription, methods, nil
}

func boolPtr(value bool) *bool {
	return &value
}
