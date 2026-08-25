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

func Error(w http.ResponseWriter, status int, message string) {
	WriteJSON(w, status, map[string]interface{}{"error": message})
}

// HandleError maps domain errors to HTTP statuses. Anything unrecognized is a
// 500 with a generic message — internals never leak to the client.
func HandleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, node_error.ErrUnauthorized):
		Error(w, http.StatusUnauthorized, "unauthorized")
	case errors.Is(err, node_error.ErrForbidden),
		errors.Is(err, node_error.ErrAttemptLocked):
		Error(w, http.StatusForbidden, "forbidden")
	case errors.Is(err, node_error.ErrInvalidAccessCode):
		Error(w, http.StatusUnauthorized, "invalid access code")
	case errors.Is(err, node_error.ErrTooManyAttempts),
		errors.Is(err, node_error.ErrIntegrityFlood):
		Error(w, http.StatusTooManyRequests, "too many requests")
	case errors.Is(err, node_error.ErrExamNotLoaded),
		errors.Is(err, node_error.ErrExamNotOpen),
		errors.Is(err, node_error.ErrMaxAttemptsReached),
		errors.Is(err, node_error.ErrAttemptNotFound),
		errors.Is(err, node_error.ErrAttemptExpired),
		errors.Is(err, node_error.ErrStaleAnswerWrite),
		errors.Is(err, node_error.ErrItemNotFound),
		errors.Is(err, node_error.ErrParticipantNotFound),
		errors.Is(err, node_error.ErrResultNotAvailable):
		Error(w, http.StatusBadRequest, "request rejected")
	default:
		Error(w, http.StatusInternalServerError, "internal server error")
	}
}
