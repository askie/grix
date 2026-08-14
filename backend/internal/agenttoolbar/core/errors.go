package core

import "errors"

var (
	ErrSessionForbidden      = errors.New("session forbidden")
	ErrPrivateAgentOnly      = errors.New("private agent session required")
	ErrToolbarUnavailable    = errors.New("toolbar unavailable")
	ErrInvalidAction         = errors.New("invalid action")
	ErrRevisionConflict      = errors.New("revision conflict")
	ErrDuplicateClientAction = errors.New("duplicate client action")
)
