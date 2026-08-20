package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"thundercall-go/internal/models"
	twilioprovider "thundercall-go/internal/providers/twilio"
	"thundercall-go/internal/repositories/sqlutil"
	usercontactmethodsrepo "thundercall-go/internal/repositories/usercontactmethods"
	userlocationsrepo "thundercall-go/internal/repositories/userlocations"
	usersrepo "thundercall-go/internal/repositories/users"
)

type publicLastVoiceMessageRecord struct {
	AccountID     int64
	EventCode     string
	AlertTypeCode string
}

type publicLastVoiceMessageResponse struct {
	Found     bool   `json:"found"`
	AccountID *int64 `json:"account_id,omitempty"`
	Type      string `json:"type,omitempty"`
}

type publicVoiceOptOutResponse struct {
	Found                 bool    `json:"found"`
	PhoneNumber           string  `json:"phoneNumber"`
	MatchedUsersCount     int     `json:"matchedUsersCount"`
	DeactivatedUsersCount int     `json:"deactivatedUsersCount"`
	UserIDs               []int64 `json:"userIds,omitempty"`
	Message               string  `json:"message"`
}

func (s *Server) handleGetLastPublicVoiceMessage(w http.ResponseWriter, r *http.Request) {
	_, variants, err := publicPhoneLookupVariants(r.URL.Query().Get("phoneNumber"))
	if err != nil {
		writePublicVoiceCompatibilityError(w, http.StatusBadRequest, err.Error())
		return
	}

	record, err := s.lookupLatestVoiceMessageByPhone(r.Context(), variants)
	if err != nil {
		writePublicVoiceCompatibilityError(w, http.StatusInternalServerError, "Failed to load last voice message.")
		return
	}
	if record == nil {
		writeJSON(w, http.StatusOK, publicLastVoiceMessageResponse{
			Found: false,
		})
		return
	}

	response := publicLastVoiceMessageResponse{
		Found:     true,
		AccountID: &record.AccountID,
		Type:      twilioprovider.VoiceFunctionAudioCode(record.EventCode, record.AlertTypeCode),
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handlePublicVoiceOptOut(w http.ResponseWriter, r *http.Request) {
	normalizedPhone, variants, err := publicPhoneLookupVariants(r.URL.Query().Get("phoneNumber"))
	if err != nil {
		writePublicVoiceCompatibilityError(w, http.StatusBadRequest, err.Error())
		return
	}

	userIDs, err := s.deactivateVoiceRecipientsByPhone(r.Context(), variants)
	if err != nil {
		writePublicVoiceCompatibilityError(w, http.StatusInternalServerError, "Failed to deactivate voice recipients.")
		return
	}

	response := publicVoiceOptOutResponse{
		Found:                 len(userIDs) > 0,
		PhoneNumber:           normalizedPhone,
		MatchedUsersCount:     len(userIDs),
		DeactivatedUsersCount: len(userIDs),
		UserIDs:               userIDs,
		Message:               "Voice recipients deactivated.",
	}
	if len(userIDs) == 0 {
		response.Message = "No active voice recipients found."
	}

	writeJSON(w, http.StatusOK, response)
}

func (s *Server) lookupLatestVoiceMessageByPhone(ctx context.Context, destinations []string) (*publicLastVoiceMessageRecord, error) {
	if s.db == nil || len(destinations) == 0 {
		return nil, nil
	}

	args := make([]any, 0, 2+len(destinations))
	args = append(args, "voice", "sent")
	for _, destination := range destinations {
		args = append(args, destination)
	}

	query := fmt.Sprintf(`
		SELECT
			COALESCE(m.account_id, u.account_id) AS account_id,
			m.event_code,
			m.alert_type_code
		FROM delivery_attempts da
		INNER JOIN users_messages um
			ON um.id = da.users_message_id
		INNER JOIN messages m
			ON m.id = um.message_id
		INNER JOIN users u
			ON u.id = um.user_id
		WHERE da.channel = ?
		  AND da.status = ?
		  AND da.destination IN (%s)
		ORDER BY COALESCE(da.delivered_at, da.sent_at, da.requested_at, um.delivered_at, um.queued_at, m.received_at) DESC,
		         da.id DESC
		LIMIT 1`, sqlutil.Placeholders(len(destinations)))

	row := s.db.QueryRowContext(ctx, query, args...)

	var (
		record publicLastVoiceMessageRecord
	)
	if err := row.Scan(
		&record.AccountID,
		&record.EventCode,
		&record.AlertTypeCode,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	return &record, nil
}

func (s *Server) deactivateVoiceRecipientsByPhone(ctx context.Context, destinations []string) ([]int64, error) {
	if s.db == nil || len(destinations) == 0 {
		return nil, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	contactMethods := usercontactmethodsrepo.NewWithDBTX(tx)
	users := usersrepo.NewWithDBTX(tx)
	userLocations := userlocationsrepo.NewWithDBTX(tx)

	methods, err := contactMethods.ListActiveByChannelAndDestinations(ctx, models.ChannelVoice, destinations)
	if err != nil {
		return nil, err
	}

	userIDs := uniqueInt64ValuesFromMethods(methods)
	if len(userIDs) == 0 {
		return nil, nil
	}

	if err := users.DeactivateByIDs(ctx, userIDs); err != nil {
		return nil, err
	}
	if err := contactMethods.DeactivateByUserIDsAndChannel(ctx, userIDs, models.ChannelVoice); err != nil {
		return nil, err
	}
	if err := userLocations.DisableThunderCallByUserIDs(ctx, userIDs); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return userIDs, nil
}

func publicPhoneLookupVariants(value string) (string, []string, error) {
	normalized, err := normalizePublicSignupPhone(value)
	if err != nil {
		return "", nil, err
	}

	digits := signupDigitsOnly(value)
	if len(digits) == 11 && strings.HasPrefix(digits, "1") {
		digits = digits[1:]
	}

	seen := map[string]struct{}{}
	var variants []string
	for _, candidate := range []string{
		strings.TrimSpace(value),
		digits,
		"1" + digits,
		"+" + "1" + digits,
		normalized,
	} {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		variants = append(variants, candidate)
	}

	return normalized, variants, nil
}

func signupDigitsOnly(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	digits := make([]rune, 0, len(value))
	for _, r := range value {
		if r >= '0' && r <= '9' {
			digits = append(digits, r)
		}
	}
	return string(digits)
}

func uniqueInt64ValuesFromMethods(methods []models.UserContactMethod) []int64 {
	if len(methods) == 0 {
		return nil
	}

	seen := make(map[int64]struct{}, len(methods))
	userIDs := make([]int64, 0, len(methods))
	for _, method := range methods {
		if _, ok := seen[method.UserID]; ok {
			continue
		}
		seen[method.UserID] = struct{}{}
		userIDs = append(userIDs, method.UserID)
	}
	return userIDs
}

func writePublicVoiceCompatibilityError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{
		"message": message,
	})
}
