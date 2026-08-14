package provider

import "errors"

// Common provider errors
var (
	ErrProviderError = func(msg string) error { return errors.New(msg) }
)
