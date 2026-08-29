package dipstick

import (
	"github.com/mattwalters/dipstick/internal/types"
)

// SourceTier represents the robustness and fidelity tier of a data source.
type SourceTier = types.SourceTier

const (
	TierAPI         = types.TierAPI
	TierLocalState  = types.TierLocalState
	TierLocalRPC    = types.TierLocalRPC
	TierTranscripts = types.TierTranscripts
	TierCLIScrape   = types.TierCLIScrape
)

// Detection captures provider installation status, authentication status, discovered version, and binary path.
type Detection = types.Detection

// Adapter defines the contract for provider-specific integrations.
type Adapter = types.Adapter

// Source defines a single data collection strategy within an adapter's source ladder.
type Source = types.Source

// SourcePolicy specifies filtering rules for which sources in the ladder are eligible.
type SourcePolicy = types.SourcePolicy

const (
	SourcePolicyDefault     = types.SourcePolicyDefault
	SourcePolicyLocal       = types.SourcePolicyLocal
	SourcePolicyRemote      = types.SourcePolicyRemote
	SourcePolicyAPI         = types.SourcePolicyAPI
	SourcePolicyLocalState  = types.SourcePolicyLocalState
	SourcePolicyLocalRPC    = types.SourcePolicyLocalRPC
	SourcePolicyTranscripts = types.SourcePolicyTranscripts
	SourcePolicyCLI         = types.SourcePolicyCLI
	SourcePolicyOffline     = types.SourcePolicyOffline
	SourcePolicyAll         = types.SourcePolicyAll
)

// PinTierPolicy creates a SourcePolicy that only allows the specified tier.
func PinTierPolicy(tier SourceTier) SourcePolicy {
	return types.PinTierPolicy(tier)
}

// TierFloorPolicy creates a SourcePolicy that requires tier >= floor.
func TierFloorPolicy(floor SourceTier) SourcePolicy {
	return types.TierFloorPolicy(floor)
}
