package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"thundercall-go/internal/geocode"
	"thundercall-go/internal/models"
)

type addressRequest struct {
	Line1      string `json:"line1"`
	Line2      string `json:"line2"`
	City       string `json:"city"`
	StateCode  string `json:"stateCode"`
	PostalCode string `json:"postalCode"`
}

func (a addressRequest) validate() error {
	switch {
	case strings.TrimSpace(a.Line1) == "":
		return errors.New("address.line1 is required")
	case strings.TrimSpace(a.StateCode) == "":
		return errors.New("address.stateCode is required")
	case strings.TrimSpace(a.City) == "" && strings.TrimSpace(a.PostalCode) == "":
		return errors.New("address.city or address.postalCode is required")
	default:
		return nil
	}
}

func (a addressRequest) toGeocodeAddress() geocode.Address {
	return geocode.Address{
		Line1:      strings.TrimSpace(a.Line1),
		Line2:      strings.TrimSpace(a.Line2),
		City:       strings.TrimSpace(a.City),
		StateCode:  strings.ToUpper(strings.TrimSpace(a.StateCode)),
		PostalCode: strings.TrimSpace(a.PostalCode),
	}
}

type createUserRequest struct {
	ExternalID        string         `json:"externalId"`
	FirstName         string         `json:"firstName"`
	LastName          string         `json:"lastName"`
	DisplayName       string         `json:"displayName"`
	Title             string         `json:"title"`
	VoicePhone        string         `json:"voicePhone"`
	LocationName      string         `json:"locationName"`
	SubscriptionType  string         `json:"subscriptionType"`
	IsPrimaryLocation *bool          `json:"isPrimaryLocation"`
	Address           addressRequest `json:"address"`
}

type locationLookupRequest struct {
	Address   *addressRequest `json:"address,omitempty"`
	Latitude  *float64        `json:"latitude,omitempty"`
	Longitude *float64        `json:"longitude,omitempty"`
	Limit     *int            `json:"limit,omitempty"`
	Offset    *int            `json:"offset,omitempty"`
}

type locationLookupResponse struct {
	Location resolvedLocationResponse    `json:"location"`
	Items    []locationMessageLookupItem `json:"items"`
	Page     pagination                  `json:"page"`
}

type resolvedLocationResponse struct {
	MatchedAddress *string `json:"matchedAddress,omitempty"`
	Latitude       float64 `json:"latitude"`
	Longitude      float64 `json:"longitude"`
	CountyFIPS     *string `json:"countyFips,omitempty"`
	NWSZone        *string `json:"nwsZone,omitempty"`
}

type locationMessageLookupItem struct {
	ID                 int64      `json:"id"`
	Source             string     `json:"source"`
	EventCode          string     `json:"eventCode"`
	MessageType        string     `json:"messageType"`
	AlertTypeCode      string     `json:"alertTypeCode"`
	Title              *string    `json:"title,omitempty"`
	Status             string     `json:"status"`
	IssuedAt           *time.Time `json:"issuedAt,omitempty"`
	ReceivedAt         time.Time  `json:"receivedAt"`
	ProcessedAt        *time.Time `json:"processedAt,omitempty"`
	SourceMessageID    *int64     `json:"sourceMessageId,omitempty"`
	ExternalMessageID  *string    `json:"externalMessageId,omitempty"`
	SourceSegmentIndex *int       `json:"sourceSegmentIndex,omitempty"`
	PolygonWKT         *string    `json:"polygonWKT,omitempty"`
	FIPSCodes          []string   `json:"fipsCodes,omitempty"`
	NWSZones           []string   `json:"nwsZones,omitempty"`
	MatchReasons       []string   `json:"matchReasons"`
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	current := actorFromContext(r.Context())
	if current == nil {
		writeError(w, http.StatusUnauthorized, "session is required")
		return
	}
	if s.db == nil || s.resolver == nil {
		writeError(w, http.StatusInternalServerError, "user creation is not configured")
		return
	}

	var request createUserRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := validateCreateUserRequest(request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	resolved, err := s.resolver.ResolveAddress(r.Context(), request.Address.toGeocodeAddress())
	if err != nil {
		if errors.Is(err, geocode.ErrNoMatch) {
			writeError(w, http.StatusUnprocessableEntity, "address could not be geocoded")
			return
		}
		writeError(w, http.StatusBadGateway, "failed to geocode address")
		return
	}
	if strings.TrimSpace(resolved.CountyFIPS) == "" || strings.TrimSpace(resolved.NWSZone) == "" {
		writeError(w, http.StatusUnprocessableEntity, "address could not be enriched with county FIPS and NWS zone")
		return
	}

	user, location, subscription, methods, err := s.createUserWithLocation(r.Context(), current.Account.ID, request, resolved)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create user")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"user":           userResponse(user),
		"location":       locationResponse(location),
		"subscription":   subscriptionResponse(subscription),
		"contactMethods": contactMethodResponses(methods),
		"resolved":       resolvedLocationPayload(resolved),
	})
}

func (s *Server) handleLookupMessagesByLocation(w http.ResponseWriter, r *http.Request) {
	if s.db == nil || s.resolver == nil {
		writeError(w, http.StatusInternalServerError, "location lookup is not configured")
		return
	}

	var request locationLookupRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	limit := 50
	if request.Limit != nil {
		limit = *request.Limit
	}
	offset := 0
	if request.Offset != nil {
		offset = *request.Offset
	}
	if limit < 1 || limit > 200 {
		writeError(w, http.StatusBadRequest, "limit must be between 1 and 200")
		return
	}
	if offset < 0 {
		writeError(w, http.StatusBadRequest, "offset must be zero or greater")
		return
	}

	resolved, err := s.resolveLookupRequest(r.Context(), request)
	if err != nil {
		switch {
		case errors.Is(err, geocode.ErrNoMatch):
			writeError(w, http.StatusUnprocessableEntity, "location could not be resolved")
		case strings.HasPrefix(err.Error(), "address.") || strings.Contains(err.Error(), "latitude") || strings.Contains(err.Error(), "longitude") || strings.Contains(err.Error(), "exactly one"):
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			writeError(w, http.StatusBadGateway, "failed to resolve location")
		}
		return
	}

	items, total, err := s.lookupMessagesByLocation(r.Context(), resolved, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load messages")
		return
	}

	writeJSON(w, http.StatusOK, locationLookupResponse{
		Location: resolvedLocationPayload(resolved),
		Items:    items,
		Page: pagination{
			Total:  total,
			Limit:  limit,
			Offset: offset,
		},
	})
}

func validateCreateUserRequest(request createUserRequest) error {
	if err := request.Address.validate(); err != nil {
		return err
	}
	if strings.TrimSpace(request.DisplayName) == "" && strings.TrimSpace(request.FirstName) == "" && strings.TrimSpace(request.LastName) == "" {
		return errors.New("displayName or firstName/lastName is required")
	}
	if value := strings.TrimSpace(request.SubscriptionType); value != "" {
		for _, r := range value {
			if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' {
				return errors.New("subscriptionType may only contain letters, numbers, and underscores")
			}
		}
	}
	return nil
}

func (s *Server) resolveLookupRequest(ctx context.Context, request locationLookupRequest) (geocode.ResolvedLocation, error) {
	hasAddress := request.Address != nil
	hasCoordinates := request.Latitude != nil || request.Longitude != nil
	if hasAddress == hasCoordinates {
		return geocode.ResolvedLocation{}, errors.New("exactly one of address or latitude/longitude is required")
	}

	if hasAddress {
		if err := request.Address.validate(); err != nil {
			return geocode.ResolvedLocation{}, err
		}
		return s.resolver.ResolveAddress(ctx, request.Address.toGeocodeAddress())
	}

	if request.Latitude == nil || request.Longitude == nil {
		return geocode.ResolvedLocation{}, errors.New("latitude and longitude are both required")
	}
	if *request.Latitude < -90 || *request.Latitude > 90 {
		return geocode.ResolvedLocation{}, errors.New("latitude must be between -90 and 90")
	}
	if *request.Longitude < -180 || *request.Longitude > 180 {
		return geocode.ResolvedLocation{}, errors.New("longitude must be between -180 and 180")
	}
	return s.resolver.ResolveCoordinates(ctx, *request.Latitude, *request.Longitude)
}

func (s *Server) createUserWithLocation(ctx context.Context, accountID int64, request createUserRequest, resolved geocode.ResolvedLocation) (*models.User, *models.Location, *models.UserLocation, []models.UserContactMethod, error) {
	return s.createUserWithResolvedLocation(ctx, accountID, createResolvedUserInput{
		ExternalID:        request.ExternalID,
		FirstName:         request.FirstName,
		LastName:          request.LastName,
		DisplayName:       request.DisplayName,
		Title:             request.Title,
		VoicePhone:        request.VoicePhone,
		LocationName:      request.LocationName,
		SubscriptionType:  request.SubscriptionType,
		IsPrimaryLocation: request.IsPrimaryLocation,
		Address:           request.Address,
	}, resolved)
}

func resolvedLocationPayload(resolved geocode.ResolvedLocation) resolvedLocationResponse {
	return resolvedLocationResponse{
		MatchedAddress: nullableTrimmedString(resolved.MatchedAddress),
		Latitude:       resolved.Latitude,
		Longitude:      resolved.Longitude,
		CountyFIPS:     nullableTrimmedString(resolved.CountyFIPS),
		NWSZone:        nullableTrimmedString(resolved.NWSZone),
	}
}

func userResponse(user *models.User) map[string]any {
	return map[string]any{
		"id":          user.ID,
		"accountId":   user.AccountID,
		"externalId":  optionalString(user.ExternalID),
		"firstName":   optionalString(user.FirstName),
		"lastName":    optionalString(user.LastName),
		"displayName": optionalString(user.DisplayName),
		"title":       optionalString(user.Title),
		"active":      user.Active,
	}
}

func locationResponse(location *models.Location) map[string]any {
	return map[string]any{
		"id":                   location.ID,
		"accountId":            location.AccountID,
		"name":                 location.Name,
		"addressLine1":         optionalString(location.AddressLine1),
		"addressLine2":         optionalString(location.AddressLine2),
		"city":                 optionalString(location.City),
		"stateCode":            optionalString(location.StateCode),
		"postalCode":           optionalString(location.PostalCode),
		"countyFips":           optionalString(location.CountyFIPS),
		"nwsZone":              optionalString(location.NWSZone),
		"latitude":             location.Latitude,
		"longitude":            location.Longitude,
		"coverageWKT":          optionalString(location.CoverageWKT),
		"isThunderCallEnabled": location.IsThunderCallEnabled,
		"active":               location.Active,
	}
}

func subscriptionResponse(subscription *models.UserLocation) map[string]any {
	return map[string]any{
		"id":                   subscription.ID,
		"userId":               subscription.UserID,
		"locationId":           subscription.LocationID,
		"subscriptionType":     subscription.SubscriptionType,
		"isPrimary":            subscription.IsPrimary,
		"isThunderCallEnabled": subscription.IsThunderCallEnabled,
	}
}

func contactMethodResponses(methods []models.UserContactMethod) []map[string]any {
	out := make([]map[string]any, 0, len(methods))
	for _, method := range methods {
		out = append(out, map[string]any{
			"id":          method.ID,
			"userId":      method.UserID,
			"channel":     method.Channel,
			"destination": method.Destination,
			"isPrimary":   method.IsPrimary,
			"isVerified":  method.IsVerified,
			"active":      method.Active,
		})
	}
	return out
}

func defaultLocationName(user *models.User, address addressRequest) string {
	displayName := preferredDisplayName(user.DisplayName, user.FirstName, user.LastName, user.ID)
	if strings.TrimSpace(displayName) != "" && !strings.HasPrefix(displayName, "User ") {
		return displayName + " Address"
	}
	return strings.TrimSpace(address.Line1)
}

func nullableTrimmedString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}
