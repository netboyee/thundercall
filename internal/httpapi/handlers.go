package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"thundercall-go/internal/models"
)

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	user, err := s.apiUsers.GetByEmail(r.Context(), request.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}
	if user == nil || !user.Active || CheckPassword(user.PasswordHash, request.Password) != nil {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	account, err := s.accounts.GetByID(r.Context(), user.AccountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load account")
		return
	}
	if account == nil || !account.Active {
		writeError(w, http.StatusUnauthorized, "account is inactive")
		return
	}

	token, tokenHash, err := newSessionToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create session token")
		return
	}

	expiresAt := s.sessionExpiry()
	now := s.now()
	session := &models.APISession{
		APIUserID:  user.ID,
		TokenHash:  tokenHash,
		ExpiresAt:  expiresAt,
		LastSeenAt: &now,
	}
	if err := s.apiSessions.Create(r.Context(), session); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create session")
		return
	}
	if err := s.apiUsers.UpdateLastLogin(r.Context(), user.ID, now); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update last login")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"token":     token,
		"expiresAt": expiresAt.UTC(),
		"user":      authUserResponse(user, account),
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	current := actorFromContext(r.Context())
	if current == nil || current.Session == nil {
		writeError(w, http.StatusUnauthorized, "session is required")
		return
	}

	if err := s.apiSessions.RevokeByTokenHash(r.Context(), current.Session.TokenHash, s.now()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke session")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true,
	})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	current := actorFromContext(r.Context())
	if current == nil {
		writeError(w, http.StatusUnauthorized, "session is required")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"user": authUserResponse(current.User, current.Account),
	})
}

func (s *Server) handleDashboardSummary(w http.ResponseWriter, r *http.Request) {
	current := actorFromContext(r.Context())
	filter, err := parseMessageListFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	summary, err := s.dashboardSummary(r.Context(), current.Account.ID, filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load dashboard summary")
		return
	}

	writeJSON(w, http.StatusOK, summary)
}

func (s *Server) handleListMessages(w http.ResponseWriter, r *http.Request) {
	current := actorFromContext(r.Context())
	filter, err := parseMessageListFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	items, total, err := s.listMessages(r.Context(), current.Account.ID, filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load messages")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"page": pagination{
			Total:  total,
			Limit:  filter.Limit,
			Offset: filter.Offset,
		},
	})
}

func (s *Server) handleGetMessage(w http.ResponseWriter, r *http.Request) {
	current := actorFromContext(r.Context())
	messageID, err := parsePathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "message id must be a number")
		return
	}

	item, err := s.getMessageDetail(r.Context(), current.Account.ID, messageID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load message")
		return
	}
	if item == nil {
		writeError(w, http.StatusNotFound, "message not found")
		return
	}

	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleMessageLocations(w http.ResponseWriter, r *http.Request) {
	current := actorFromContext(r.Context())
	messageID, err := parsePathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "message id must be a number")
		return
	}

	visible, err := s.messageVisibleToAccount(r.Context(), current.Account.ID, messageID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to validate message access")
		return
	}
	if !visible {
		writeError(w, http.StatusNotFound, "message not found")
		return
	}

	items, err := s.listMessageLocations(r.Context(), current.Account.ID, messageID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load message locations")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
	})
}

func (s *Server) handleMessageDeliveries(w http.ResponseWriter, r *http.Request) {
	current := actorFromContext(r.Context())
	messageID, err := parsePathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "message id must be a number")
		return
	}

	visible, err := s.messageVisibleToAccount(r.Context(), current.Account.ID, messageID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to validate message access")
		return
	}
	if !visible {
		writeError(w, http.StatusNotFound, "message not found")
		return
	}

	filter, err := parseDeliveryListFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	items, total, err := s.listMessageDeliveries(r.Context(), current.Account.ID, messageID, filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load message deliveries")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"page": pagination{
			Total:  total,
			Limit:  filter.Limit,
			Offset: filter.Offset,
		},
	})
}

func (s *Server) handleListLocations(w http.ResponseWriter, r *http.Request) {
	current := actorFromContext(r.Context())
	filter, err := parseLocationListFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	items, total, err := s.listLocations(r.Context(), current.Account.ID, filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load locations")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"page": pagination{
			Total:  total,
			Limit:  filter.Limit,
			Offset: filter.Offset,
		},
	})
}

func (s *Server) handleGetLocation(w http.ResponseWriter, r *http.Request) {
	current := actorFromContext(r.Context())
	locationID, err := parsePathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "location id must be a number")
		return
	}

	item, err := s.getLocationDetail(r.Context(), current.Account.ID, locationID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load location")
		return
	}
	if item == nil {
		writeError(w, http.StatusNotFound, "location not found")
		return
	}

	writeJSON(w, http.StatusOK, item)
}

func authUserResponse(user *models.APIUser, account *models.Account) map[string]any {
	return map[string]any{
		"id":          user.ID,
		"accountId":   user.AccountID,
		"email":       user.Email,
		"displayName": optionalString(user.DisplayName),
		"lastLoginAt": optionalTime(user.LastLoginAt),
		"account": map[string]any{
			"id":   account.ID,
			"name": account.Name,
		},
	}
}

func parseMessageListFilter(r *http.Request) (messageListFilter, error) {
	filter := messageListFilter{
		Search:      strings.TrimSpace(r.URL.Query().Get("search")),
		EventCode:   strings.TrimSpace(r.URL.Query().Get("eventCode")),
		MessageType: strings.TrimSpace(r.URL.Query().Get("messageType")),
		Status:      strings.TrimSpace(r.URL.Query().Get("status")),
		Source:      strings.TrimSpace(r.URL.Query().Get("source")),
		Limit:       50,
		Offset:      0,
	}

	if from := strings.TrimSpace(r.URL.Query().Get("from")); from != "" {
		value, err := parseTimestampParam(from, false)
		if err != nil {
			return filter, fmt.Errorf("invalid from parameter: %w", err)
		}
		filter.From = &value
	}
	if to := strings.TrimSpace(r.URL.Query().Get("to")); to != "" {
		value, err := parseTimestampParam(to, true)
		if err != nil {
			return filter, fmt.Errorf("invalid to parameter: %w", err)
		}
		filter.To = &value
	}

	limit, err := parseIntQueryParam(r, "limit", 50, 1, 200)
	if err != nil {
		return filter, err
	}
	offset, err := parseIntQueryParam(r, "offset", 0, 0, 1000000)
	if err != nil {
		return filter, err
	}
	filter.Limit = limit
	filter.Offset = offset
	return filter, nil
}

func parseLocationListFilter(r *http.Request) (locationListFilter, error) {
	filter := locationListFilter{
		Search: strings.TrimSpace(r.URL.Query().Get("search")),
		Limit:  50,
		Offset: 0,
	}

	if activeOnly := strings.TrimSpace(r.URL.Query().Get("activeOnly")); activeOnly != "" {
		value, err := strconv.ParseBool(activeOnly)
		if err != nil {
			return filter, errors.New("activeOnly must be true or false")
		}
		filter.ActiveOnly = &value
	}

	limit, err := parseIntQueryParam(r, "limit", 50, 1, 200)
	if err != nil {
		return filter, err
	}
	offset, err := parseIntQueryParam(r, "offset", 0, 0, 1000000)
	if err != nil {
		return filter, err
	}
	filter.Limit = limit
	filter.Offset = offset
	return filter, nil
}

func parseDeliveryListFilter(r *http.Request) (deliveryListFilter, error) {
	filter := deliveryListFilter{
		Search: strings.TrimSpace(r.URL.Query().Get("search")),
		Status: strings.TrimSpace(r.URL.Query().Get("status")),
		Limit:  50,
		Offset: 0,
	}

	limit, err := parseIntQueryParam(r, "limit", 50, 1, 200)
	if err != nil {
		return filter, err
	}
	offset, err := parseIntQueryParam(r, "offset", 0, 0, 1000000)
	if err != nil {
		return filter, err
	}
	filter.Limit = limit
	filter.Offset = offset
	return filter, nil
}

func parseIntQueryParam(r *http.Request, name string, fallback int, min int, max int) (int, error) {
	value := strings.TrimSpace(r.URL.Query().Get(name))
	if value == "" {
		return fallback, nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a number", name)
	}
	if parsed < min || parsed > max {
		return 0, fmt.Errorf("%s must be between %d and %d", name, min, max)
	}
	return parsed, nil
}

func parseTimestampParam(raw string, endOfDay bool) (time.Time, error) {
	layouts := []string{
		time.RFC3339,
		"2006-01-02",
	}

	for _, layout := range layouts {
		parsed, err := time.Parse(layout, raw)
		if err != nil {
			continue
		}

		parsed = parsed.UTC()
		if layout == "2006-01-02" && endOfDay {
			return parsed.Add(24*time.Hour - time.Nanosecond), nil
		}
		return parsed, nil
	}

	return time.Time{}, fmt.Errorf("expected RFC3339 or YYYY-MM-DD")
}
