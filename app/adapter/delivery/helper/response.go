package helper

import (
	"encoding/json"
	"errors"
	"net/http"

	node_error "github.com/celpung/bangkusekolah_exam_node/app/domain/error"
)

// WriteJSON is the single response encoder: every handler writes through it so
// content types and status handling stay uniform.
func WriteJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func Success(w http.ResponseWriter, status int, message string, data interface{}) {
	WriteJSON(w, status, map[string]interface{}{"message": message, "data": data})
}

// DecodeJSON parses the request body into dst. On failure it has already
// written the error response; the caller just stops.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst interface{}) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		Error(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

func Error(w http.ResponseWriter, status int, message string) {
	ErrorWithCode(w, status, "", message)
}

func ErrorWithCode(w http.ResponseWriter, status int, code, message string) {
	payload := map[string]interface{}{"error": message}
	if code != "" {
		payload["code"] = code
	}
	WriteJSON(w, status, payload)
}

// HandleError maps domain errors to HTTP statuses. Anything unrecognized is a
// 500 with a generic message — internals never leak to the client.
func HandleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, node_error.ErrUnauthorized):
		ErrorWithCode(w, http.StatusUnauthorized, errorCode(err), "unauthorized")
	case errors.Is(err, node_error.ErrForbidden),
		errors.Is(err, node_error.ErrAttemptLocked):
		ErrorWithCode(w, http.StatusForbidden, errorCode(err), "forbidden")
	case errors.Is(err, node_error.ErrAttemptDeviceMismatch):
		ErrorWithCode(w, http.StatusConflict, errorCode(err), "exam attempt belongs to another device")
	case errors.Is(err, node_error.ErrInvalidAccessCode):
		ErrorWithCode(w, http.StatusUnauthorized, errorCode(err), "invalid access code")
	case errors.Is(err, node_error.ErrTooManyAttempts),
		errors.Is(err, node_error.ErrIntegrityFlood):
		ErrorWithCode(w, http.StatusTooManyRequests, errorCode(err), "too many requests")
	case errors.Is(err, node_error.ErrExamContentNotReady):
		ErrorWithCode(w, http.StatusServiceUnavailable, errorCode(err), "exam content is not ready")
	case errors.Is(err, node_error.ErrExamNotLoaded),
		errors.Is(err, node_error.ErrExamNotOpen),
		errors.Is(err, node_error.ErrMaxAttemptsReached),
		errors.Is(err, node_error.ErrAttemptNotFound),
		errors.Is(err, node_error.ErrAttemptExpired),
		errors.Is(err, node_error.ErrAttemptDeviceIDInvalid),
		errors.Is(err, node_error.ErrStaleAnswerWrite),
		errors.Is(err, node_error.ErrItemNotFound),
		errors.Is(err, node_error.ErrParticipantNotFound),
		errors.Is(err, node_error.ErrResultNotAvailable):
		ErrorWithCode(w, http.StatusBadRequest, errorCode(err), "request rejected")
	default:
		Error(w, http.StatusInternalServerError, "internal server error")
	}
}

func errorCode(err error) string {
	switch {
	case errors.Is(err, node_error.ErrUnauthorized):
		return "unauthorized"
	case errors.Is(err, node_error.ErrForbidden):
		return "forbidden"
	case errors.Is(err, node_error.ErrInvalidAccessCode):
		return "invalid_access_code"
	case errors.Is(err, node_error.ErrTooManyAttempts):
		return "too_many_login_attempts"
	case errors.Is(err, node_error.ErrIntegrityFlood):
		return "integrity_flood"
	case errors.Is(err, node_error.ErrExamNotLoaded):
		return "exam_not_loaded"
	case errors.Is(err, node_error.ErrExamContentNotReady):
		return "exam_content_not_ready"
	case errors.Is(err, node_error.ErrExamNotOpen):
		return "exam_not_open"
	case errors.Is(err, node_error.ErrMaxAttemptsReached):
		return "max_attempts_reached"
	case errors.Is(err, node_error.ErrAttemptNotFound):
		return "attempt_not_found"
	case errors.Is(err, node_error.ErrAttemptExpired):
		return "attempt_expired"
	case errors.Is(err, node_error.ErrAttemptLocked):
		return "attempt_locked"
	case errors.Is(err, node_error.ErrAttemptDeviceMismatch):
		return "attempt_device_mismatch"
	case errors.Is(err, node_error.ErrAttemptDeviceIDInvalid):
		return "attempt_device_id_invalid"
	case errors.Is(err, node_error.ErrStaleAnswerWrite):
		return "stale_answer_write"
	case errors.Is(err, node_error.ErrItemNotFound):
		return "item_not_found"
	case errors.Is(err, node_error.ErrParticipantNotFound):
		return "participant_not_found"
	case errors.Is(err, node_error.ErrResultNotAvailable):
		return "result_not_available"
	default:
		return ""
	}
}
