package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"thundercall-go/internal/logging"
	twilioprovider "thundercall-go/internal/providers/twilio"
	deliveryattemptsrepo "thundercall-go/internal/repositories/deliveryattempts"
)

var twilioVoiceCallbackLogger = logging.New("api.twilio-callback")

func (s *Server) handleTwilioVoiceStatusCallback(w http.ResponseWriter, r *http.Request) {
	if s.db == nil || s.deliveryAttempts == nil || s.userMessages == nil || s.notifications == nil {
		writeError(w, http.StatusServiceUnavailable, "callback storage unavailable")
		return
	}

	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form payload")
		return
	}

	if !s.validateTwilioSignature(r, r.PostForm) {
		writeError(w, http.StatusUnauthorized, "invalid twilio signature")
		return
	}

	callback, err := parseTwilioVoiceStatusCallback(r.PostForm)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	payloadBytes, err := json.Marshal(r.PostForm)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "encode callback payload")
		return
	}
	payloadJSON := string(payloadBytes)

	if shouldEnrichTwilioVoiceCallback(callback) && s.twilioVoice != nil {
		details, lookupErr := s.twilioVoice.LookupVoiceCall(r.Context(), callback.CallSID)
		if lookupErr != nil {
			twilioVoiceCallbackLogger.Warnf("event=twilio_callback_lookup_failed call_sid=%s error=%q", callback.CallSID, lookupErr)
		} else {
			callback.ApplyCallDetails(details)
		}
	}

	records, err := s.deliveryAttempts.ListVoiceDispatchRecordsByProviderMessageID(r.Context(), callback.CallSID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "lookup delivery attempts")
		return
	}
	if len(records) == 0 {
		twilioVoiceCallbackLogger.Warnf("event=twilio_callback_unknown_call call_sid=%s status=%s", callback.CallSID, callback.CallStatus)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	now := s.now()
	for _, record := range records {
		if err := s.applyTwilioVoiceCallback(r.Context(), record, callback, payloadJSON, now); err != nil {
			writeError(w, http.StatusInternalServerError, "persist callback outcome")
			return
		}
		twilioVoiceCallbackLogger.Infof(
			"event=twilio_callback_applied call_sid=%s status=%s attempt_id=%d message_id=%d user_id=%d answered_by=%s duration_seconds=%s",
			callback.CallSID,
			callback.CallStatus,
			record.Attempt.ID,
			record.MessageID,
			record.UserID,
			blankDash(strings.TrimSpace(callback.AnsweredBy)),
			intPtrString(callback.DurationSeconds),
		)
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) applyTwilioVoiceCallback(ctx context.Context, record deliveryattemptsrepo.VoiceDispatchRecord, callback twilioVoiceStatusCallback, payloadJSON string, callbackAt time.Time) error {
	providerStatus := callback.CallStatus
	update := deliveryattemptsrepo.VoiceCallbackUpdate{
		Status:                  internalAttemptStatusForTwilio(callback.CallStatus),
		ProviderStatus:          stringPtrIfNotBlank(providerStatus),
		ProviderAnsweredBy:      stringPtrIfNotBlank(callback.AnsweredBy),
		ProviderDurationSeconds: callback.DurationSeconds,
		ErrorMessage:            callback.ErrorMessage,
		ProviderPayloadJSON:     &payloadJSON,
		ProviderLastCallbackAt:  callbackAt,
		SentAt:                  coalesceTime(record.Attempt.SentAt, callbackAt),
		DeliveredAt:             deliveredAtForTwilio(callback.CallStatus, callbackAt),
	}
	if err := s.deliveryAttempts.UpdateVoiceCallback(ctx, record.Attempt.ID, update); err != nil {
		return err
	}

	if !isFinalTwilioCallStatus(callback.CallStatus) {
		return nil
	}

	userMessageStatus := finalUserMessageStatusForTwilio(callback.CallStatus)
	deliveredAt := deliveredAtForTwilio(callback.CallStatus, callbackAt)
	if err := s.userMessages.UpdateStatus(ctx, record.Attempt.UserMessageID, userMessageStatus, deliveredAt); err != nil {
		return err
	}

	if record.Attempt.NotificationID != nil {
		requestedAt := record.Attempt.RequestedAt
		if err := s.notifications.UpdateStatus(
			ctx,
			*record.Attempt.NotificationID,
			userMessageStatus,
			record.MessageID,
			&requestedAt,
			coalesceTime(record.Attempt.SentAt, callbackAt),
			deliveredAt,
		); err != nil {
			return err
		}
	}

	return nil
}

type twilioVoiceStatusCallback struct {
	CallSID         string
	CallStatus      string
	AnsweredBy      string
	DurationSeconds *int
	ErrorMessage    *string
}

func parseTwilioVoiceStatusCallback(values url.Values) (twilioVoiceStatusCallback, error) {
	callSID := strings.TrimSpace(values.Get("CallSid"))
	if callSID == "" {
		return twilioVoiceStatusCallback{}, fmt.Errorf("CallSid is required")
	}

	callStatus := normalizeTwilioCallStatus(values.Get("CallStatus"))
	if callStatus == "" {
		return twilioVoiceStatusCallback{}, fmt.Errorf("CallStatus is required")
	}

	callback := twilioVoiceStatusCallback{
		CallSID:    callSID,
		CallStatus: callStatus,
		AnsweredBy: strings.TrimSpace(values.Get("AnsweredBy")),
	}

	if durationValue := strings.TrimSpace(values.Get("CallDuration")); durationValue != "" {
		duration, err := strconv.Atoi(durationValue)
		if err != nil {
			return twilioVoiceStatusCallback{}, fmt.Errorf("CallDuration must be numeric")
		}
		callback.DurationSeconds = &duration
	}

	callback.ErrorMessage = twilioVoiceCallbackError(values, callStatus)
	return callback, nil
}

func (c *twilioVoiceStatusCallback) ApplyCallDetails(details twilioprovider.VoiceCallDetails) {
	if strings.TrimSpace(details.Status) != "" {
		c.CallStatus = normalizeTwilioCallStatus(details.Status)
	}
	if strings.TrimSpace(details.AnsweredBy) != "" {
		c.AnsweredBy = strings.TrimSpace(details.AnsweredBy)
	}
	if c.DurationSeconds == nil && details.DurationSeconds != nil {
		c.DurationSeconds = details.DurationSeconds
	}
}

func shouldEnrichTwilioVoiceCallback(callback twilioVoiceStatusCallback) bool {
	return isFinalTwilioCallStatus(callback.CallStatus) && (strings.TrimSpace(callback.AnsweredBy) == "" || callback.DurationSeconds == nil)
}

func normalizeTwilioCallStatus(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func isFinalTwilioCallStatus(status string) bool {
	switch normalizeTwilioCallStatus(status) {
	case "completed", "busy", "failed", "no-answer", "canceled":
		return true
	default:
		return false
	}
}

func internalAttemptStatusForTwilio(status string) string {
	switch normalizeTwilioCallStatus(status) {
	case "busy", "failed", "no-answer", "canceled":
		return "failed"
	default:
		return "sent"
	}
}

func finalUserMessageStatusForTwilio(status string) string {
	switch normalizeTwilioCallStatus(status) {
	case "busy", "failed", "no-answer", "canceled":
		return "failed"
	default:
		return "sent"
	}
}

func deliveredAtForTwilio(status string, callbackAt time.Time) *time.Time {
	if normalizeTwilioCallStatus(status) != "completed" {
		return nil
	}
	deliveredAt := callbackAt
	return &deliveredAt
}

func twilioVoiceCallbackError(values url.Values, status string) *string {
	var parts []string
	switch normalizeTwilioCallStatus(status) {
	case "busy", "failed", "no-answer", "canceled":
		parts = append(parts, "twilio call status "+normalizeTwilioCallStatus(status))
	}

	if errorCode := strings.TrimSpace(values.Get("ErrorCode")); errorCode != "" {
		parts = append(parts, "error_code="+errorCode)
	}
	if sipResponseCode := strings.TrimSpace(values.Get("SipResponseCode")); sipResponseCode != "" {
		parts = append(parts, "sip_response_code="+sipResponseCode)
	}

	if len(parts) == 0 {
		return nil
	}
	message := strings.Join(parts, " ")
	return &message
}

func intPtrString(value *int) string {
	if value == nil {
		return "-"
	}
	return strconv.Itoa(*value)
}

func blankDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func (s *Server) validateTwilioSignature(r *http.Request, values url.Values) bool {
	token := strings.TrimSpace(s.twilioAuthToken)
	if token == "" {
		return true
	}

	signature := strings.TrimSpace(r.Header.Get("X-Twilio-Signature"))
	if signature == "" {
		return false
	}

	expected := computeTwilioSignature(token, twilioRequestURL(r), values)
	return hmac.Equal([]byte(signature), []byte(expected))
}

func computeTwilioSignature(authToken string, requestURL string, values url.Values) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var builder strings.Builder
	builder.WriteString(requestURL)
	for _, key := range keys {
		entries := append([]string(nil), values[key]...)
		sort.Strings(entries)
		for _, value := range entries {
			builder.WriteString(key)
			builder.WriteString(value)
		}
	}

	mac := hmac.New(sha1.New, []byte(authToken))
	_, _ = mac.Write([]byte(builder.String()))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func twilioRequestURL(r *http.Request) string {
	scheme := "http"
	if forwardedProto := forwardedHeaderValue(r.Header.Get("X-Forwarded-Proto")); forwardedProto != "" {
		scheme = forwardedProto
	} else if r.TLS != nil {
		scheme = "https"
	}

	host := strings.TrimSpace(r.Host)
	if forwardedHost := forwardedHeaderValue(r.Header.Get("X-Forwarded-Host")); forwardedHost != "" {
		host = forwardedHost
	}

	requestURL := scheme + "://" + host + r.URL.Path
	if rawQuery := strings.TrimSpace(r.URL.RawQuery); rawQuery != "" {
		requestURL += "?" + rawQuery
	}
	return requestURL
}

func forwardedHeaderValue(value string) string {
	parts := strings.Split(value, ",")
	if len(parts) == 0 {
		return ""
	}
	return strings.TrimSpace(parts[0])
}

func stringPtrIfNotBlank(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func coalesceTime(existing *time.Time, fallback time.Time) *time.Time {
	if existing != nil {
		value := existing.UTC()
		return &value
	}
	value := fallback.UTC()
	return &value
}
