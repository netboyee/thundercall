package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"thundercall-go/internal/geocode"
	"thundercall-go/internal/logging"
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

var signupLogger = logging.New("api.signup")

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

	if !s.allowPublicSignupRequest(w, r) {
		return
	}

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

	var (
		resolved          *geocode.ResolvedLocation
		enrichmentPending bool
		geocodeStatus     = "resolved"
	)

	resolvedLocation, err := s.resolver.ResolveAddress(r.Context(), input.Address.toGeocodeAddress())
	if err != nil {
		if errors.Is(err, geocode.ErrNoMatch) {
			writePublicSignupError(w, http.StatusUnprocessableEntity, "Address could not be geocoded.")
			return
		}
		enrichmentPending = true
		geocodeStatus = "pending"
		signupLogger.Warnf(
			"event=user_signup_geocode_pending account_id=%d address=%q error=%v",
			account.ID,
			input.Address.toGeocodeAddress().OneLine(),
			err,
		)
	} else {
		resolved = &resolvedLocation
		if strings.TrimSpace(resolvedLocation.CountyFIPS) == "" || strings.TrimSpace(resolvedLocation.NWSZone) == "" {
			enrichmentPending = true
			geocodeStatus = "partial"
			signupLogger.Warnf(
				"event=user_signup_geocode_partial account_id=%d address=%q county_fips=%s nws_zone=%s",
				account.ID,
				input.Address.toGeocodeAddress().OneLine(),
				resolvedLocation.CountyFIPS,
				resolvedLocation.NWSZone,
			)
		}
	}

	user, location, subscription, methods, err := s.createUserWithSignupLocation(r.Context(), account.ID, input, resolved)
	if err != nil {
		writePublicSignupError(w, http.StatusInternalServerError, "Failed to create record.")
		return
	}
	resolvedCountyFIPS := ""
	resolvedNWSZone := ""
	if resolved != nil {
		resolvedCountyFIPS = strings.TrimSpace(resolved.CountyFIPS)
		resolvedNWSZone = strings.TrimSpace(resolved.NWSZone)
	}
	signupLogger.Infof(
		"event=user_signup account_id=%d user_id=%d location_id=%d external_id=%s geocode_status=%s county_fips=%s nws_zone=%s contact_methods=%d",
		account.ID,
		user.ID,
		location.ID,
		stringValue(user.ExternalID),
		geocodeStatus,
		resolvedCountyFIPS,
		resolvedNWSZone,
		len(methods),
	)

	response := map[string]any{
		"message":           "Record created.",
		"enrichmentPending": enrichmentPending,
		"user":              userResponse(user),
		"location":          locationResponse(location),
		"subscription":      subscriptionResponse(subscription),
		"contactMethods":    contactMethodResponses(methods),
	}
	if enrichmentPending {
		response["message"] = "Record created; location enrichment pending."
	}
	if resolved != nil {
		response["resolved"] = resolvedLocationPayload(*resolved)
	}

	writeJSON(w, http.StatusCreated, response)
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

func (s *Server) createUserWithSignupLocation(ctx context.Context, accountID int64, input createResolvedUserInput, resolved *geocode.ResolvedLocation) (*models.User, *models.Location, *models.UserLocation, []models.UserContactMethod, error) {
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
	canonicalAddress := canonicalSignupAddress(input.Address, resolved)

	user, err := findOrCreateSignupUser(ctx, users, contactMethods, accountID, input, displayName)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	subscriptionType := strings.TrimSpace(input.SubscriptionType)
	if subscriptionType == "" {
		subscriptionType = "address"
	}
	existingSubscriptions, err := userLocations.ListByUserID(ctx, user.ID)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	location, err := findMatchingSignupLocation(ctx, locations, existingSubscriptions, canonicalAddress)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	locationName := strings.TrimSpace(input.LocationName)
	if location == nil {
		if locationName == "" {
			locationName = defaultLocationName(user, canonicalAddress)
		}
		location = buildSignupLocation(accountID, locationName, canonicalAddress, resolved)
		if err := locations.Create(ctx, location); err != nil {
			return nil, nil, nil, nil, err
		}
	} else {
		if locationName != "" {
			location.Name = locationName
		}
		applySignupLocationUpdate(location, canonicalAddress, resolved)
		if err := locations.Update(ctx, location); err != nil {
			return nil, nil, nil, nil, err
		}
	}

	isPrimary := resolveSignupPrimary(input.IsPrimaryLocation, existingSubscriptions, location.ID, subscriptionType)
	subscription := &models.UserLocation{
		UserID:               user.ID,
		LocationID:           location.ID,
		SubscriptionType:     subscriptionType,
		IsPrimary:            isPrimary,
		IsThunderCallEnabled: true,
	}
	if err := userLocations.Upsert(ctx, subscription); err != nil {
		return nil, nil, nil, nil, err
	}

	if err := upsertSignupContactMethods(ctx, contactMethods, user.ID, input); err != nil {
		return nil, nil, nil, nil, err
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
	methods, err := contactMethods.ListByUserID(ctx, user.ID)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, nil, nil, err
	}
	return user, location, subscription, methods, nil
}

func findOrCreateSignupUser(ctx context.Context, users *usersrepo.Repository, contactMethods *usercontactmethodsrepo.Repository, accountID int64, input createResolvedUserInput, displayName string) (*models.User, error) {
	if voicePhone := strings.TrimSpace(input.VoicePhone); voicePhone != "" {
		userID, err := contactMethods.FindActiveUserIDByAccountAndChannelDestination(ctx, accountID, models.ChannelVoice, voicePhone)
		if err != nil {
			return nil, err
		}
		if userID != 0 {
			user, err := users.GetByID(ctx, userID)
			if err != nil {
				return nil, err
			}
			if user != nil {
				if mergeSignupUser(user, input, displayName) {
					if err := users.Update(ctx, user); err != nil {
						return nil, err
					}
				}
				return user, nil
			}
		}
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
		return nil, err
	}
	return user, nil
}

func mergeSignupUser(user *models.User, input createResolvedUserInput, displayName string) bool {
	changed := false

	if value := nullableTrimmedString(input.FirstName); value != nil && stringValue(user.FirstName) != *value {
		user.FirstName = value
		changed = true
	}
	if value := nullableTrimmedString(input.LastName); value != nil && stringValue(user.LastName) != *value {
		user.LastName = value
		changed = true
	}
	if value := nullableTrimmedString(displayName); value != nil && stringValue(user.DisplayName) != *value {
		user.DisplayName = value
		changed = true
	}
	if value := nullableTrimmedString(input.Title); value != nil && stringValue(user.Title) != *value {
		user.Title = value
		changed = true
	}
	if !user.Active {
		user.Active = true
		changed = true
	}

	return changed
}

func findMatchingSignupLocation(ctx context.Context, locations *locationsrepo.Repository, subscriptions []models.UserLocation, address addressRequest) (*models.Location, error) {
	targetKey := signupLocationKey(address)
	for _, subscription := range subscriptions {
		location, err := locations.GetByID(ctx, subscription.LocationID)
		if err != nil {
			return nil, err
		}
		if location == nil {
			continue
		}
		if locationSignupKey(location) == targetKey {
			return location, nil
		}
	}
	return nil, nil
}

func buildSignupLocation(accountID int64, name string, address addressRequest, resolved *geocode.ResolvedLocation) *models.Location {
	location := &models.Location{
		AccountID:            accountID,
		Name:                 name,
		IsThunderCallEnabled: true,
		Active:               true,
	}
	applySignupLocationUpdate(location, address, resolved)
	return location
}

func applySignupLocationUpdate(location *models.Location, address addressRequest, resolved *geocode.ResolvedLocation) {
	location.AddressLine1 = nullableTrimmedString(address.Line1)
	location.AddressLine2 = nullableTrimmedString(address.Line2)
	location.City = nullableTrimmedString(address.City)
	location.StateCode = nullableTrimmedString(strings.ToUpper(address.StateCode))
	location.PostalCode = nullableTrimmedString(address.PostalCode)
	location.IsThunderCallEnabled = true
	location.Active = true

	if resolved == nil {
		return
	}

	if countyFIPS := nullableTrimmedString(resolved.CountyFIPS); countyFIPS != nil {
		location.CountyFIPS = countyFIPS
	}
	if nwsZone := nullableTrimmedString(resolved.NWSZone); nwsZone != nil {
		location.NWSZone = nwsZone
	}
	if resolvedLocationHasCoordinates(resolved) {
		coverageWKT := geocode.PointWKT(resolved.Latitude, resolved.Longitude)
		latitude := resolved.Latitude
		longitude := resolved.Longitude
		location.Latitude = &latitude
		location.Longitude = &longitude
		location.CoverageWKT = &coverageWKT
	}
}

func upsertSignupContactMethods(ctx context.Context, contactMethods *usercontactmethodsrepo.Repository, userID int64, input createResolvedUserInput) error {
	if voicePhone := strings.TrimSpace(input.VoicePhone); voicePhone != "" {
		method := models.UserContactMethod{
			UserID:      userID,
			Channel:     models.ChannelVoice,
			Destination: voicePhone,
			IsPrimary:   true,
			IsVerified:  false,
			Active:      true,
		}
		if err := contactMethods.Upsert(ctx, &method); err != nil {
			return err
		}
	}

	if emailAddress := strings.TrimSpace(input.EmailAddress); emailAddress != "" {
		method := models.UserContactMethod{
			UserID:      userID,
			Channel:     models.ChannelEmail,
			Destination: strings.ToLower(emailAddress),
			IsPrimary:   true,
			IsVerified:  false,
			Active:      true,
		}
		if err := contactMethods.Upsert(ctx, &method); err != nil {
			return err
		}
	}

	return nil
}

func canonicalSignupAddress(address addressRequest, resolved *geocode.ResolvedLocation) addressRequest {
	canonical := addressRequest{
		Line1:      strings.TrimSpace(address.Line1),
		Line2:      strings.TrimSpace(address.Line2),
		City:       strings.TrimSpace(address.City),
		StateCode:  strings.ToUpper(strings.TrimSpace(address.StateCode)),
		PostalCode: strings.TrimSpace(address.PostalCode),
	}

	if resolved == nil {
		return canonical
	}

	matched := strings.TrimSpace(resolved.MatchedAddress)
	if matched == "" {
		return canonical
	}

	parts := strings.Split(matched, ",")
	if len(parts) < 4 {
		return canonical
	}

	canonical.Line1 = strings.TrimSpace(parts[0])
	canonical.City = strings.TrimSpace(parts[1])
	canonical.StateCode = strings.ToUpper(strings.TrimSpace(parts[2]))
	canonical.PostalCode = strings.TrimSpace(strings.Join(parts[3:], ","))
	return canonical
}

func signupLocationKey(address addressRequest) string {
	return strings.Join([]string{
		normalizeSignupKeyPart(address.Line1),
		normalizeSignupKeyPart(address.Line2),
		normalizeSignupKeyPart(address.City),
		normalizeSignupKeyPart(address.StateCode),
		normalizeSignupKeyPart(address.PostalCode),
	}, "|")
}

func locationSignupKey(location *models.Location) string {
	return strings.Join([]string{
		normalizeSignupKeyPart(stringValue(location.AddressLine1)),
		normalizeSignupKeyPart(stringValue(location.AddressLine2)),
		normalizeSignupKeyPart(stringValue(location.City)),
		normalizeSignupKeyPart(stringValue(location.StateCode)),
		normalizeSignupKeyPart(stringValue(location.PostalCode)),
	}, "|")
}

func normalizeSignupKeyPart(value string) string {
	return strings.Join(strings.Fields(strings.ToUpper(strings.TrimSpace(value))), " ")
}

func resolveSignupPrimary(preferred *bool, subscriptions []models.UserLocation, locationID int64, subscriptionType string) bool {
	if preferred != nil {
		return *preferred
	}
	for _, subscription := range subscriptions {
		if subscription.LocationID == locationID && subscription.SubscriptionType == subscriptionType {
			return subscription.IsPrimary
		}
	}
	return len(subscriptions) == 0
}

func boolPtr(value bool) *bool {
	return &value
}

func resolvedLocationHasCoordinates(resolved *geocode.ResolvedLocation) bool {
	return resolved != nil && (resolved.Latitude != 0 || resolved.Longitude != 0)
}
