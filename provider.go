package dipstick

import "context"

// Config contains runtime configuration passed to a provider during collection.
type Config struct {
	SourcePolicy SourcePolicy
}

// Provider defines the interface implemented by coding agent adapters.
type Provider interface {
	// ID returns the unique identifier for the provider.
	ID() ProviderID

	// Collect gathers usage and metering data for the provider.
	Collect(ctx context.Context, cfg Config) (ProviderReport, error)
}
