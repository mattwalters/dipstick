// Package dipstick defines the public domain types and stable data contract
// for coding agent usage and quota metering.
//
// Compatibility Promise:
// Within the dipstick.v1 schema, all modifications are strictly additive.
// New optional fields may be introduced in minor or patch versions, but
// existing fields, types, and semantics will not be deleted, renamed, or altered.
// Consumers may safely parse and inspect dipstick.v1 JSON output with jq or
// standard JSON parsers under this contract. Breaking schema changes will be
// published under a new SchemaVersion.
package dipstick

import (
	"github.com/mattwalters/dipstick/internal/types"
)

// SchemaVersion is the public schema identifier for version 1 of the dipstick JSON report.
const SchemaVersion = types.SchemaVersion

// ProviderID identifies an AI coding agent or vendor.
type ProviderID = types.ProviderID

const (
	ProviderClaude      = types.ProviderClaude
	ProviderCodex       = types.ProviderCodex
	ProviderOpenCode    = types.ProviderOpenCode
	ProviderAntigravity = types.ProviderAntigravity
)

// AllProviders lists every supported ProviderID.
var AllProviders = types.AllProviders

// SourceID identifies the detection or retrieval tier that produced usage or error data.
type SourceID = types.SourceID

const (
	SourceOAuthAPI   = types.SourceOAuthAPI
	SourceLocalState = types.SourceLocalState
	SourceAppServer  = types.SourceAppServer
	SourceTranscript = types.SourceTranscript
	SourceCLIStdout  = types.SourceCLIStdout
)

// Confidence indicates the reliability and freshness level of a provider report.
type Confidence = types.Confidence

const (
	ConfidenceExact   = types.ConfidenceExact
	ConfidenceDerived = types.ConfidenceDerived
	ConfidenceStale   = types.ConfidenceStale
	ConfidenceUnknown = types.ConfidenceUnknown
)

// Reason categorizes non-fatal provider collection failures and error conditions.
type Reason = types.Reason

const (
	ReasonNotInstalled       = types.ReasonNotInstalled
	ReasonNotAuthenticated   = types.ReasonNotAuthenticated
	ReasonCredentialExpired  = types.ReasonCredentialExpired
	ReasonUnsupportedVersion = types.ReasonUnsupportedVersion
	ReasonParseFailed        = types.ReasonParseFailed
	ReasonUpstreamError      = types.ReasonUpstreamError
	ReasonTimeout            = types.ReasonTimeout
	ReasonNotSupported       = types.ReasonNotSupported
)

// AttemptStatus represents the outcome of evaluating one source in the ladder.
type AttemptStatus = types.AttemptStatus

const (
	// AttemptStatusSuccess indicates the source returned usable data.
	AttemptStatusSuccess = types.AttemptStatusSuccess
	// AttemptStatusError indicates the source was attempted but returned an error.
	AttemptStatusError = types.AttemptStatusError
	// AttemptStatusUnavailable indicates the source's prerequisites were not met.
	AttemptStatusUnavailable = types.AttemptStatusUnavailable
	// AttemptStatusSkipped indicates the source was excluded by the source policy.
	AttemptStatusSkipped = types.AttemptStatusSkipped
	// AttemptStatusTimeout indicates the source exceeded its per-source timeout.
	AttemptStatusTimeout = types.AttemptStatusTimeout
)

// SourceAttempt records one rung of a ladder walk: which source was tried,
// how it ended, and how long it took.
type SourceAttempt = types.SourceAttempt

// Report is the top-level container for a complete usage collection run.
type Report = types.Report

// ProviderReport contains usage, quota, and metadata for a single agent provider.
type ProviderReport = types.ProviderReport

// RateWindow represents a vendor rate limit or quota window (e.g. session, weekly).
type RateWindow = types.RateWindow

// TokenUsage records token consumption counters when exposed by the vendor.
type TokenUsage = types.TokenUsage

// Identity holds account and authentication identity metadata.
type Identity = types.Identity

// ProviderError captures non-fatal, provider-specific failures during collection.
type ProviderError = types.ProviderError

// Ptr returns a pointer to the provided value.
func Ptr[T any](v T) *T {
	return types.Ptr(v)
}
