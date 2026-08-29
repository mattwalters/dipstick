package dipstick

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// SourceTier represents the robustness and fidelity tier of a data source.
// Lower numerical values represent higher fidelity and reliability (Tier 1 is most robust).
type SourceTier int

const (
	// TierAPI represents local credentials calling vendor HTTP usage endpoints (Tier 1).
	TierAPI SourceTier = 1
	// TierLocalState represents structured local state files (auth.json, JWT claims, config) (Tier 2).
	TierLocalState SourceTier = 2
	// TierLocalRPC represents local RPC/server surfaces (codex app-server, opencode server) (Tier 3).
	TierLocalRPC SourceTier = 3
	// TierTranscripts represents session transcripts on disk (Tier 4).
	TierTranscripts SourceTier = 4
	// TierCLIScrape represents CLI stdout scraping or PTY capture (Tier 5).
	TierCLIScrape SourceTier = 5
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
// Each adapter declares an ordered ladder of sources, ordered highest tier first.
type Adapter interface {
	// ID returns the unique identifier for the provider.
	ID() ProviderID
	// Detect inspects local environment to determine installation, auth, and version state.
	Detect(ctx context.Context) (Detection, error)
	// Sources returns the ordered ladder of sources, highest tier first.
	Sources() []Source
}

// Source defines a single data collection strategy within an adapter's source ladder.
type Source interface {
	// ID returns the identifier for this source.
	ID() SourceID
	// Tier returns the source robustness tier.
	Tier() SourceTier
	// Available checks whether prerequisites for this source are satisfied.
	Available(ctx context.Context) bool
	// Fetch retrieves usage report from this source.
	Fetch(ctx context.Context) (*ProviderReport, error)
}

// SourcePolicy specifies filtering rules for which sources in the ladder are
// eligible. It is an input to a collection run and is never serialized;
// SourceID is the output counterpart, naming the rung a datum came from and
// carrying values the dipstick.v1 schema enum fixes.
type SourcePolicy string

const (
	// SourcePolicyDefault allows all available sources in ladder order.
	SourcePolicyDefault SourcePolicy = "default"
	// SourcePolicyLocal restricts sources to offline/local state only (Tier >= 2).
	SourcePolicyLocal SourcePolicy = "local"
	// SourcePolicyRemote restricts sources to remote API endpoints only (Tier 1).
	SourcePolicyRemote SourcePolicy = "remote"
	// SourcePolicyAPI restricts sources to Tier 1 API endpoints.
	SourcePolicyAPI SourcePolicy = "api"
	// SourcePolicyLocalState restricts sources to Tier 2 local state files.
	SourcePolicyLocalState SourcePolicy = "local_state"
	// SourcePolicyLocalRPC restricts sources to Tier 3 local RPC/app-server.
	SourcePolicyLocalRPC SourcePolicy = "local_rpc"
	// SourcePolicyTranscripts restricts sources to Tier 4 transcript files.
	SourcePolicyTranscripts SourcePolicy = "transcripts"
	// SourcePolicyCLI restricts sources to Tier 5 CLI scraping.
	SourcePolicyCLI SourcePolicy = "cli"
	// SourcePolicyOffline restricts sources to local/offline only (Tier >= 2).
	SourcePolicyOffline SourcePolicy = "offline"
	// SourcePolicyAll allows all available sources in ladder order.
	SourcePolicyAll SourcePolicy = "all"
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

	// Direct tier pinning by name
	switch policyStr {
	case "local", "offline", "no-network", "no_network":
		// Disallow Tier 1 (API network calls)
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

	// Pinned tier syntax: "tier:1", "tier:2", etc.
	if strings.HasPrefix(policyStr, "tier:") {
		tStr := strings.TrimPrefix(policyStr, "tier:")
		if t, err := strconv.Atoi(tStr); err == nil {
			return int(tier) == t
		}
		return false
	}

	// Floor syntax: "floor:2", "min-tier:2" (tier >= floor)
	if strings.HasPrefix(policyStr, "floor:") || strings.HasPrefix(policyStr, "min-tier:") || strings.HasPrefix(policyStr, "min_tier:") {
		idx := strings.Index(policyStr, ":")
		tStr := policyStr[idx+1:]
		if t, err := strconv.Atoi(tStr); err == nil {
			return int(tier) >= t
		}
		return false
	}

	// Max tier syntax: "max-tier:3", "max_tier:3" (tier <= max)
	if strings.HasPrefix(policyStr, "max-tier:") || strings.HasPrefix(policyStr, "max_tier:") {
		idx := strings.Index(policyStr, ":")
		tStr := policyStr[idx+1:]
		if t, err := strconv.Atoi(tStr); err == nil {
			return int(tier) <= t
		}
		return false
	}

	// Match exact ID
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
