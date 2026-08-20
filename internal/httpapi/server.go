package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"thundercall-go/internal/config"
	"thundercall-go/internal/geocode"
	twilioprovider "thundercall-go/internal/providers/twilio"
	accountsrepo "thundercall-go/internal/repositories/accounts"
	apisessionsrepo "thundercall-go/internal/repositories/apisessions"
	apiusersrepo "thundercall-go/internal/repositories/apiusers"
	deliveryattemptsrepo "thundercall-go/internal/repositories/deliveryattempts"
	locationsrepo "thundercall-go/internal/repositories/locations"
	notificationsrepo "thundercall-go/internal/repositories/notifications"
	usercontactmethodsrepo "thundercall-go/internal/repositories/usercontactmethods"
	userlocationsrepo "thundercall-go/internal/repositories/userlocations"
	usersrepo "thundercall-go/internal/repositories/users"
	usersmessagesrepo "thundercall-go/internal/repositories/usersmessages"
)

type twilioVoiceLookup interface {
	LookupVoiceCall(ctx context.Context, sid string) (twilioprovider.VoiceCallDetails, error)
}

type Server struct {
	db                       *sql.DB
	accounts                 *accountsrepo.Repository
	apiUsers                 *apiusersrepo.Repository
	apiSessions              *apisessionsrepo.Repository
	users                    *usersrepo.Repository
	locations                *locationsrepo.Repository
	userLocations            *userlocationsrepo.Repository
	contactMethods           *usercontactmethodsrepo.Repository
	userMessages             *usersmessagesrepo.Repository
	notifications            *notificationsrepo.Repository
	deliveryAttempts         *deliveryattemptsrepo.Repository
	resolver                 geocode.Resolver
	twilioVoice              twilioVoiceLookup
	twilioAuthToken          string
	sessionTTL               time.Duration
	publicSignupLimiter      *requestRateLimiter
	publicSignupProxySecret  string
	publicSignupProxyMaxSkew time.Duration
	now                      func() time.Time
	pingDB                   func(context.Context) error
}

func NewServer(db *sql.DB, sessionTTL time.Duration, resolver geocode.Resolver) *Server {
	return NewServerWithTwilio(db, sessionTTL, resolver, config.TwilioConfig{})
}

func NewServerWithTwilio(db *sql.DB, sessionTTL time.Duration, resolver geocode.Resolver, twilioCfg config.TwilioConfig) *Server {
	var twilioVoice twilioVoiceLookup
	if twilioCfg.Enabled() {
		twilioVoice = twilioprovider.New(twilioCfg)
	}

	server := &Server{
		db:                       db,
		accounts:                 accountsrepo.New(db),
		apiUsers:                 apiusersrepo.New(db),
		apiSessions:              apisessionsrepo.New(db),
		users:                    usersrepo.New(db),
		locations:                locationsrepo.New(db),
		userLocations:            userlocationsrepo.New(db),
		contactMethods:           usercontactmethodsrepo.New(db),
		userMessages:             usersmessagesrepo.New(db),
		notifications:            notificationsrepo.New(db),
		deliveryAttempts:         deliveryattemptsrepo.New(db),
		resolver:                 resolver,
		twilioVoice:              twilioVoice,
		twilioAuthToken:          strings.TrimSpace(twilioCfg.AuthToken),
		sessionTTL:               sessionTTL,
		publicSignupProxyMaxSkew: 5 * time.Minute,
		now:                      func() time.Time { return time.Now().UTC() },
	}
	server.configurePublicSignupRateLimit(defaultPublicSignupRateLimitCount, defaultPublicSignupRateLimitWindow)
	if db != nil {
		server.pingDB = db.PingContext
	}
	return server
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /api/users/messages/last", s.handleGetLastPublicVoiceMessage)
	mux.HandleFunc("OPTIONS /api/users/signup", s.handlePublicSignupOptions)
	mux.HandleFunc("POST /api/users/signup", s.handlePublicSignup)
	mux.HandleFunc("GET /api/users/voice/opt-out", s.handlePublicVoiceOptOut)
	mux.HandleFunc("OPTIONS /api/products/{productId}/records", s.handlePublicSignupOptions)
	mux.HandleFunc("POST /api/products/{productId}/records", s.handleLegacyPublicSignup)
	mux.HandleFunc("POST /api/providers/twilio/voice/status", s.handleTwilioVoiceStatusCallback)
	mux.HandleFunc("POST /v1/auth/login", s.handleLogin)
	mux.HandleFunc("OPTIONS /v1/public/signups", s.handlePublicSignupOptions)
	mux.HandleFunc("POST /v1/public/signups", s.handleLegacyPublicSignup)
	mux.Handle("POST /v1/auth/logout", s.withAuth(http.HandlerFunc(s.handleLogout)))
	mux.Handle("GET /v1/auth/me", s.withAuth(http.HandlerFunc(s.handleMe)))
	mux.Handle("GET /v1/dashboard/summary", s.withAuth(http.HandlerFunc(s.handleDashboardSummary)))
	mux.Handle("GET /v1/messages", s.withAuth(http.HandlerFunc(s.handleListMessages)))
	mux.Handle("POST /v1/messages/lookup", s.withAuth(http.HandlerFunc(s.handleLookupMessagesByLocation)))
	mux.Handle("GET /v1/messages/{id}/locations", s.withAuth(http.HandlerFunc(s.handleMessageLocations)))
	mux.Handle("GET /v1/messages/{id}/deliveries", s.withAuth(http.HandlerFunc(s.handleMessageDeliveries)))
	mux.Handle("GET /v1/messages/{id}", s.withAuth(http.HandlerFunc(s.handleGetMessage)))
	mux.Handle("GET /v1/locations", s.withAuth(http.HandlerFunc(s.handleListLocations)))
	mux.Handle("GET /v1/locations/{id}", s.withAuth(http.HandlerFunc(s.handleGetLocation)))
	mux.Handle("POST /v1/users", s.withAuth(http.HandlerFunc(s.handleCreateUser)))
	return mux
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if s.pingDB != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		if err := s.pingDB(ctx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"ok":    false,
				"error": "mysql unavailable",
			})
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true,
	})
}

func decodeJSON(r *http.Request, dest any) error {
	defer r.Body.Close()

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(dest)
}

func decodeJSONBytes(data []byte, dest any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(dest)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{
		"error": message,
	})
}

func optionalString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func optionalTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC()
}

func parsePathInt64(r *http.Request, name string) (int64, error) {
	return strconv.ParseInt(strings.TrimSpace(r.PathValue(name)), 10, 64)
}
