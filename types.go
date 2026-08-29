package dipstick

import (
	"encoding/json"
	"errors"
	"time"
)

// ProviderID identifies an AI coding agent provider.
type ProviderID string

const (
	// ProviderAntigravity represents the Google Antigravity coding agent.
	ProviderAntigravity ProviderID = "antigravity"
	// ProviderClaude represents the Anthropic Claude coding agent.
	ProviderClaude ProviderID = "claude"
	// ProviderCodex represents the OpenAI Codex coding agent.
	ProviderCodex ProviderID = "codex"
	// ProviderOpenCode represents the OpenCode coding agent.
	ProviderOpenCode ProviderID = "opencode"
)

// AllProviders returns the list of all supported ProviderIDs.
var AllProviders = []ProviderID{
	ProviderAntigravity,
	ProviderClaude,
	ProviderCodex,
	ProviderOpenCode,
}

// SourcePolicy determines the data source strategy used during collection.
type SourcePolicy string

const (
	// SourcePolicyDefault uses standard provider discovery mechanisms.
	SourcePolicyDefault SourcePolicy = "default"
	// SourcePolicyLocal inspects only local state, config files, and caches.
	SourcePolicyLocal SourcePolicy = "local"
	// SourcePolicyRemote queries remote provider APIs directly.
	SourcePolicyRemote SourcePolicy = "remote"
	// SourcePolicyAll gathers usage data from all available sources.
	SourcePolicyAll SourcePolicy = "all"
)

// Report contains the aggregated results from a collection run.
type Report struct {
	CollectedAt time.Time                     `json:"collected_at"`
	Providers   map[ProviderID]ProviderReport `json:"providers"`
}

// ProviderReport contains usage data and any provider-level errors for a single provider.
type ProviderReport struct {
	ProviderID ProviderID `json:"provider_id"`
	Usage      Usage      `json:"usage,omitempty"`
	Err        error      `json:"error,omitempty"`
}

type providerReportJSON struct {
	ProviderID ProviderID `json:"provider_id"`
	Usage      Usage      `json:"usage,omitempty"`
	Error      *string    `json:"error,omitempty"`
}

// MarshalJSON customizes JSON serialization to encode Err as an error message string.
func (pr ProviderReport) MarshalJSON() ([]byte, error) {
	var errStr *string
	if pr.Err != nil {
		s := pr.Err.Error()
		errStr = &s
	}
	return json.Marshal(providerReportJSON{
		ProviderID: pr.ProviderID,
		Usage:      pr.Usage,
		Error:      errStr,
	})
}

// UnmarshalJSON customizes JSON deserialization to decode Error string back into Err.
func (pr *ProviderReport) UnmarshalJSON(data []byte) error {
	var aux providerReportJSON
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	pr.ProviderID = aux.ProviderID
	pr.Usage = aux.Usage
	if aux.Error != nil {
		pr.Err = errors.New(*aux.Error)
	} else {
		pr.Err = nil
	}
	return nil
}

// Usage represents aggregated token usage, cost, and session metrics for a provider.
type Usage struct {
	Sessions      int            `json:"sessions,omitempty"`
	InputTokens   int64          `json:"input_tokens,omitempty"`
	OutputTokens  int64          `json:"output_tokens,omitempty"`
	TotalTokens   int64          `json:"total_tokens,omitempty"`
	EstimatedCost float64        `json:"estimated_cost,omitempty"`
	Currency      string         `json:"currency,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}
