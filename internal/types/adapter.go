package types

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// SourceTier represents the robustness and fidelity tier of a data source.
type SourceTier int

const (
	TierAPI         SourceTier = 1
	TierLocalState  SourceTier = 2
	TierLocalRPC    SourceTier = 3
	TierTranscripts SourceTier = 4
	TierCLIScrape   SourceTier = 5
)

// String returns a human-readable name for the source tier.
func (t SourceTier) String() string {
	switch t {
	case TierAPI:
		return "api"
	case TierLocalState:
		return "local_state"
	case TierLocalRPC:
		return "local_rpc"
	case TierTranscripts:
		return "transcripts"
	case TierCLIScrape:
		return "cli_scrape"
	default:
		return fmt.Sprintf("tier_%d", int(t))
	}
}

// Detection captures provider installation status, authentication status, discovered version, and binary path.
type Detection struct {
	Installed     bool   `json:"installed"`
	Authenticated bool   `json:"authenticated"`
	Version       string `json:"version,omitempty"`
	BinaryPath    string `json:"binary_path,omitempty"`
}

// Adapter defines the contract for provider-specific integrations.
type Adapter interface {
	ID() ProviderID
	Detect(ctx context.Context) (Detection, error)
	Sources() []Source
	Compat() Compat
}

// Source defines a single data collection strategy within an adapter's source ladder.
type Source interface {
	ID() SourceID
	Tier() SourceTier
	Available(ctx context.Context) bool
	Fetch(ctx context.Context) (*ProviderReport, error)
}

// SourcePolicy specifies filtering rules for which sources in the ladder are eligible.
type SourcePolicy string

const (
	SourcePolicyDefault     SourcePolicy = "default"
	SourcePolicyLocal       SourcePolicy = "local"
	SourcePolicyRemote      SourcePolicy = "remote"
	SourcePolicyAPI         SourcePolicy = "api"
	SourcePolicyLocalState  SourcePolicy = "local_state"
	SourcePolicyLocalRPC    SourcePolicy = "local_rpc"
	SourcePolicyTranscripts SourcePolicy = "transcripts"
	SourcePolicyCLI         SourcePolicy = "cli"
	SourcePolicyOffline     SourcePolicy = "offline"
	SourcePolicyAll         SourcePolicy = "all"
)

// Allows checks if a source is permitted under this source policy.
func (p SourcePolicy) Allows(s Source) bool {
	if s == nil {
		return false
	}
	return p.AllowsTierAndID(s.Tier(), s.ID())
}

// AllowsTierAndID checks if a tier and source ID are permitted under this policy.
func (p SourcePolicy) AllowsTierAndID(tier SourceTier, id SourceID) bool {
	policyStr := strings.TrimSpace(strings.ToLower(string(p)))
	if policyStr == "" || policyStr == "default" || policyStr == "all" {
		return true
	}

	switch policyStr {
	case "local", "offline", "no-network", "no_network":
		return tier >= TierLocalState
	case "remote", "api":
		return tier == TierAPI || id == SourceOAuthAPI
	case "local_state", "local-state", "localstate":
		return tier == TierLocalState || id == SourceLocalState
	case "rpc", "local_rpc", "local-rpc":
		return tier == TierLocalRPC || id == SourceAppServer
	case "transcripts", "transcript":
		return tier == TierTranscripts || id == SourceTranscript
	case "cli", "cli_scrape", "cli-scrape":
		return tier == TierCLIScrape || id == SourceCLIStdout
	}

	if strings.HasPrefix(policyStr, "tier:") {
		tStr := strings.TrimPrefix(policyStr, "tier:")
		if t, err := strconv.Atoi(tStr); err == nil {
			return int(tier) == t
		}
		return false
	}

	if strings.HasPrefix(policyStr, "floor:") || strings.HasPrefix(policyStr, "min-tier:") || strings.HasPrefix(policyStr, "min_tier:") {
		idx := strings.Index(policyStr, ":")
		tStr := policyStr[idx+1:]
		if t, err := strconv.Atoi(tStr); err == nil {
			return int(tier) >= t
		}
		return false
	}

	if strings.HasPrefix(policyStr, "max-tier:") || strings.HasPrefix(policyStr, "max_tier:") {
		idx := strings.Index(policyStr, ":")
		tStr := policyStr[idx+1:]
		if t, err := strconv.Atoi(tStr); err == nil {
			return int(tier) <= t
		}
		return false
	}

	if strings.EqualFold(string(id), policyStr) {
		return true
	}

	return false
}

// PinTierPolicy creates a SourcePolicy that only allows the specified tier.
func PinTierPolicy(tier SourceTier) SourcePolicy {
	return SourcePolicy(fmt.Sprintf("tier:%d", int(tier)))
}

// TierFloorPolicy creates a SourcePolicy that requires tier >= floor.
func TierFloorPolicy(floor SourceTier) SourcePolicy {
	return SourcePolicy(fmt.Sprintf("floor:%d", int(floor)))
}
