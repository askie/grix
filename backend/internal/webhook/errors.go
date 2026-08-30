package webhook

import "errors"

var (
	ErrInvalidPayload = errors.New("invalid webhook payload")
	ErrNotFound       = errors.New("webhook not found")
	ErrExpired        = errors.New("webhook expired")
	ErrForbidden      = errors.New("webhook forbidden")
	ErrLimitExceeded  = errors.New("webhook limit exceeded")
	ErrExpiresInPast  = errors.New("webhook expires_at is in the past")
)
