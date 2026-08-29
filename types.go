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
	"fmt"
	"time"
)

// SchemaVersion is the public schema identifier for version 1 of the dipstick JSON report.
const SchemaVersion = "dipstick.v1"

// ProviderID identifies an AI coding agent or vendor.
type ProviderID string

const (
	ProviderClaude      ProviderID = "claude"
	ProviderCodex       ProviderID = "codex"
	ProviderOpenCode    ProviderID = "opencode"
	ProviderAntigravity ProviderID = "antigravity"
)

// AllProviders lists every supported ProviderID.
var AllProviders = []ProviderID{
	ProviderAntigravity,
	ProviderClaude,
	ProviderCodex,
	ProviderOpenCode,
}

// SourcePolicy selects which retrieval tiers a collection run is allowed to
// use. It is an input to Collect and is never serialized into a Report;
// SourceID below is the output counterpart, naming the tier a datum actually
// came from.
type SourcePolicy string

const (
	// SourcePolicyDefault walks the standard source ladder for each provider.
	SourcePolicyDefault SourcePolicy = "default"
	// SourcePolicyLocal restricts collection to local state, config, and caches.
	SourcePolicyLocal SourcePolicy = "local"
	// SourcePolicyRemote restricts collection to remote provider APIs.
	SourcePolicyRemote SourcePolicy = "remote"
	// SourcePolicyAll gathers from every available source.
	SourcePolicyAll SourcePolicy = "all"
)

// SourceID identifies the detection or retrieval tier that produced usage or error data.
type SourceID string

const (
	SourceOAuthAPI   SourceID = "oauth_api"
	SourceLocalState SourceID = "local_state"
	SourceAppServer  SourceID = "app_server"
	SourceTranscript SourceID = "transcript"
	SourceCLIStdout  SourceID = "cli_stdout"
)

// Confidence indicates the reliability and freshness level of a provider report.
type Confidence string

const (
	ConfidenceExact   Confidence = "exact"
	ConfidenceDerived Confidence = "derived"
	ConfidenceStale   Confidence = "stale"
	ConfidenceUnknown Confidence = "unknown"
)

// Reason categorizes non-fatal provider collection failures and error conditions.
type Reason string

const (
	ReasonNotInstalled       Reason = "not_installed"
	ReasonNotAuthenticated   Reason = "not_authenticated"
	ReasonCredentialExpired  Reason = "credential_expired"
	ReasonUnsupportedVersion Reason = "unsupported_version"
	ReasonParseFailed        Reason = "parse_failed"
	ReasonUpstreamError      Reason = "upstream_error"
	ReasonTimeout            Reason = "timeout"
	ReasonNotSupported       Reason = "not_supported"
)

// Report is the top-level container for a complete usage collection run.
type Report struct {
	SchemaVersion string           `json:"schema_version"`
	GeneratedAt   time.Time        `json:"generated_at"`
	Providers     []ProviderReport `json:"providers"`
	Errors        []ProviderError  `json:"errors,omitempty"`
}

// ProviderReport contains usage, quota, and metadata for a single agent provider.
type ProviderReport struct {
	Provider   ProviderID   `json:"provider"`
	Source     SourceID     `json:"source"`
	Confidence Confidence   `json:"confidence"`
	CLIVersion string       `json:"cli_version,omitempty"`
	Identity   *Identity    `json:"identity,omitempty"`
	Windows    []RateWindow `json:"windows,omitempty"`
	Tokens     *TokenUsage  `json:"tokens,omitempty"`
	ObservedAt time.Time    `json:"observed_at"`
}

// RateWindow represents a vendor rate limit or quota window (e.g. session, weekly).
// All numeric metrics are pointers to explicitly distinguish 0 (zero usage) from unknown/unavailable.
type RateWindow struct {
	Label                 string     `json:"label"`
	UsedPercent           *float64   `json:"used_percent,omitempty"`
	Limit                 *float64   `json:"limit,omitempty"`
	Used                  *float64   `json:"used,omitempty"`
	ResetsAt              *time.Time `json:"resets_at,omitempty"`
	WindowDurationSeconds *int64     `json:"window_duration_seconds,omitempty"`
}

// Duration returns WindowDurationSeconds as a time.Duration, or 0 if unset.
func (w RateWindow) Duration() time.Duration {
	if w.WindowDurationSeconds == nil {
		return 0
	}
	return time.Duration(*w.WindowDurationSeconds) * time.Second
}

// TokenUsage records token consumption counters when exposed by the vendor.
// All numeric fields are pointers to explicitly distinguish 0 from missing data.
type TokenUsage struct {
	InputTokens      *int64 `json:"input_tokens,omitempty"`
	OutputTokens     *int64 `json:"output_tokens,omitempty"`
	CacheReadTokens  *int64 `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens *int64 `json:"cache_write_tokens,omitempty"`
	TotalTokens      *int64 `json:"total_tokens,omitempty"`
}

// Identity holds account and authentication identity metadata.
type Identity struct {
	Email        string `json:"email,omitempty"`
	Organization string `json:"organization,omitempty"`
	AccountID    string `json:"account_id,omitempty"`
	Plan         string `json:"plan,omitempty"`
}

// ProviderError captures non-fatal, provider-specific failures during collection.
type ProviderError struct {
	Provider  ProviderID `json:"provider"`
	Reason    Reason     `json:"reason"`
	Source    SourceID   `json:"source,omitempty"`
	Detail    string     `json:"detail"`
	Retryable bool       `json:"retryable"`
}

// Error implements the standard Go error interface for ProviderError.
func (e ProviderError) Error() string {
	if e.Source != "" {
		return fmt.Sprintf("%s (%s): %s: %s", e.Provider, e.Source, e.Reason, e.Detail)
	}
	return fmt.Sprintf("%s: %s: %s", e.Provider, e.Reason, e.Detail)
}

// Ptr returns a pointer to the provided value.
func Ptr[T any](v T) *T {
	return &v
}
