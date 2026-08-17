package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"thundercall-go/internal/models"
)

type actorContextKey struct{}

type actor struct {
	User    *models.APIUser
	Account *models.Account
	Session *models.APISession
}

func HashPassword(password string) (string, error) {
	if strings.TrimSpace(password) == "" {
		return "", errors.New("password is required")
	}

	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func CheckPassword(hash string, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

func newSessionToken() (string, string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", "", err
	}

	token := base64.RawURLEncoding.EncodeToString(bytes)
	return token, hashToken(token), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func actorFromContext(ctx context.Context) *actor {
	value := ctx.Value(actorContextKey{})
	if value == nil {
		return nil
	}

	current, _ := value.(*actor)
	return current
}

func (s *Server) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerToken(r.Header.Get("Authorization"))
		if !ok {
			writeError(w, http.StatusUnauthorized, "authorization token is required")
			return
		}

		session, err := s.apiSessions.GetByTokenHash(r.Context(), hashToken(token))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load session")
			return
		}
		if session == nil || session.RevokedAt != nil || !session.ExpiresAt.After(s.now()) {
			writeError(w, http.StatusUnauthorized, "session is invalid or expired")
			return
		}

		user, err := s.apiUsers.GetByID(r.Context(), session.APIUserID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load user")
			return
		}
		if user == nil || !user.Active {
			writeError(w, http.StatusUnauthorized, "user is inactive")
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

		_ = s.apiSessions.TouchLastSeen(r.Context(), session.ID, s.now())

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), actorContextKey{}, &actor{
			User:    user,
			Account: account,
			Session: session,
		})))
	})
}

func bearerToken(header string) (string, bool) {
	header = strings.TrimSpace(header)
	if header == "" {
		return "", false
	}

	const prefix = "Bearer "
	if !strings.HasPrefix(strings.ToLower(header), strings.ToLower(prefix)) {
		return "", false
	}

	token := strings.TrimSpace(header[len(prefix):])
	return token, token != ""
}

func (s *Server) sessionExpiry() time.Time {
	return s.now().Add(s.sessionTTL).UTC()
}
