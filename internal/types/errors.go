package types

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/mattwalters/dipstick/internal/scrub"
)

var (
	// ErrNotInstalled indicates the provider CLI or prerequisite binary is not installed.
	ErrNotInstalled = errors.New("provider not installed")

	// ErrNotAuthenticated indicates the provider is installed but not authenticated.
	ErrNotAuthenticated = errors.New("not authenticated")

	// ErrCredentialExpired indicates provider credentials have expired and need renewal.
	ErrCredentialExpired = errors.New("credential expired")

	// ErrUnsupportedVersion indicates the installed provider version is unsupported.
	ErrUnsupportedVersion = errors.New("unsupported version")

	// ErrParseFailed indicates output could not be parsed (vendor drift signal).
	ErrParseFailed = errors.New("parse failed")

	// ErrUpstreamError indicates a vendor API 4xx/5xx or upstream service failure.
	ErrUpstreamError = errors.New("upstream error")

	// ErrTimeout indicates an operation exceeded its timeout.
	ErrTimeout = errors.New("timeout")

	// ErrNotSupported indicates the vendor exposes no usage surface or feature is unsupported.
	ErrNotSupported = errors.New("not supported")

	// ErrSourceTimeout is returned when a source fetch or availability check exceeds its allocated timeout.
	ErrSourceTimeout = errors.New("source fetch timeout")
)

// Sentinel returns the standard sentinel error corresponding to the given Reason.
func Sentinel(r Reason) error {
	switch r {
	case ReasonNotInstalled:
		return ErrNotInstalled
	case ReasonNotAuthenticated:
		return ErrNotAuthenticated
	case ReasonCredentialExpired:
		return ErrCredentialExpired
	case ReasonUnsupportedVersion:
		return ErrUnsupportedVersion
	case ReasonParseFailed:
		return ErrParseFailed
	case ReasonUpstreamError:
		return ErrUpstreamError
	case ReasonTimeout:
		return ErrTimeout
	case ReasonNotSupported:
		return ErrNotSupported
	default:
		return nil
	}
}

// Sentinel returns the standard sentinel error corresponding to this Reason.
func (r Reason) Sentinel() error {
	return Sentinel(r)
}

// Retryable returns whether failures of this reason are typically retryable.
func (r Reason) Retryable() bool {
	switch r {
	case ReasonUpstreamError, ReasonTimeout:
		return true
	default:
		return false
	}
}

// ReasonForError maps an error (sentinel, ProviderError, or wrapped error) to its corresponding Reason.
func ReasonForError(err error) Reason {
	if err == nil {
		return ""
	}
	var pe ProviderError
	if errors.As(err, &pe) {
		return pe.Reason
	}
	var pErr *ProviderError
	if errors.As(err, &pErr) && pErr != nil {
		return pErr.Reason
	}
	switch {
	case errors.Is(err, ErrParseFailed):
		return ReasonParseFailed
	case errors.Is(err, ErrUnsupportedVersion):
		return ReasonUnsupportedVersion
	case errors.Is(err, ErrCredentialExpired):
		return ReasonCredentialExpired
	case errors.Is(err, ErrNotAuthenticated):
		return ReasonNotAuthenticated
	case errors.Is(err, ErrNotInstalled):
		return ReasonNotInstalled
	case errors.Is(err, ErrNotSupported):
		return ReasonNotSupported
	case errors.Is(err, ErrTimeout), errors.Is(err, ErrSourceTimeout), errors.Is(err, context.DeadlineExceeded):
		return ReasonTimeout
	case errors.Is(err, ErrUpstreamError):
		return ReasonUpstreamError
	default:
		return ""
	}
}

// ScrubSecrets removes sensitive credentials, tokens, cookies, and authorization headers from a string.
func ScrubSecrets(s string) string {
	return scrub.Scrub(s)
}

// Is implements standard library errors.Is matching for ProviderError.
func (e ProviderError) Is(target error) bool {
	if target == nil {
		return false
	}
	if target == Sentinel(e.Reason) {
		return true
	}
	if e.Reason == ReasonTimeout && (target == ErrSourceTimeout || target == context.DeadlineExceeded) {
		return true
	}
	if pe, ok := target.(ProviderError); ok {
		return (pe.Reason == "" || pe.Reason == e.Reason) &&
			(pe.Provider == "" || pe.Provider == e.Provider) &&
			(pe.Source == "" || pe.Source == e.Source)
	}
	if pe, ok := target.(*ProviderError); ok && pe != nil {
		return (pe.Reason == "" || pe.Reason == e.Reason) &&
			(pe.Provider == "" || pe.Provider == e.Provider) &&
			(pe.Source == "" || pe.Source == e.Source)
	}
	return false
}

type providerErrorAlias ProviderError

// MarshalJSON implements json.Marshaler to guarantee that sensitive tokens and credentials
// are scrubbed from ProviderError.Detail before JSON serialization.
func (e ProviderError) MarshalJSON() ([]byte, error) {
	alias := providerErrorAlias(e)
	alias.Detail = ScrubSecrets(alias.Detail)
	return json.Marshal(alias)
}
