package dipstick

import (
	"github.com/mattwalters/dipstick/internal/types"
)

var (
	// ErrNotInstalled indicates the provider CLI or prerequisite binary is not installed.
	ErrNotInstalled = types.ErrNotInstalled

	// ErrNotAuthenticated indicates the provider is installed but not authenticated.
	ErrNotAuthenticated = types.ErrNotAuthenticated

	// ErrCredentialExpired indicates provider credentials have expired and need renewal.
	ErrCredentialExpired = types.ErrCredentialExpired

	// ErrUnsupportedVersion indicates the installed provider version is unsupported.
	ErrUnsupportedVersion = types.ErrUnsupportedVersion

	// ErrParseFailed indicates output could not be parsed (vendor drift signal).
	ErrParseFailed = types.ErrParseFailed

	// ErrUpstreamError indicates a vendor API 4xx/5xx or upstream service failure.
	ErrUpstreamError = types.ErrUpstreamError

	// ErrTimeout indicates an operation exceeded its timeout.
	ErrTimeout = types.ErrTimeout

	// ErrNotSupported indicates the vendor exposes no usage surface or feature is unsupported.
	ErrNotSupported = types.ErrNotSupported
)

// Sentinel returns the standard sentinel error corresponding to the given Reason,
// or nil if Reason is unrecognized.
func Sentinel(r Reason) error {
	return types.Sentinel(r)
}

// ReasonForError maps an error (sentinel, ProviderError, or wrapped error) to its corresponding Reason.
// Returns "" if the error cannot be mapped.
func ReasonForError(err error) Reason {
	return types.ReasonForError(err)
}

// ScrubSecrets removes sensitive credentials, tokens, cookies, and authorization headers from a string.
func ScrubSecrets(s string) string {
	return types.ScrubSecrets(s)
}
