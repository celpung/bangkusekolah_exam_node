package node_error

import "errors"

var (
	ErrUnauthorized          = errors.New("unauthorized")
	ErrForbidden             = errors.New("forbidden")
	ErrInvalidAccessCode     = errors.New("access code is not valid")
	ErrTooManyAttempts       = errors.New("too many login attempts, please wait")
	ErrExamNotLoaded         = errors.New("no exam bundle has been loaded on this node")
	ErrExamNotOpen           = errors.New("exam is not open for attempts")
	ErrMaxAttemptsReached    = errors.New("maximum exam attempts reached")
	ErrAttemptNotFound       = errors.New("exam attempt not found")
	ErrAttemptExpired        = errors.New("exam attempt has expired")
	ErrAttemptLocked         = errors.New("exam attempt is no longer accepting answers")
	ErrStaleAnswerWrite      = errors.New("a newer answer for this item has already been saved")
	ErrItemNotFound          = errors.New("exam item not found")
	ErrParticipantNotFound   = errors.New("participant not found")
	ErrResultNotAvailable    = errors.New("exam result is not available yet")
	ErrIntegrityFlood        = errors.New("too many integrity events for this attempt")
	ErrBundleChecksum        = errors.New("bundle checksum does not match its content")
	ErrBundleFileUpload      = errors.New("file_upload items are not supported on the node")
	ErrPreflightFailed       = errors.New("node preflight checks failed")
	ErrRepositoryUnavailable = errors.New("repository unavailable")
)
