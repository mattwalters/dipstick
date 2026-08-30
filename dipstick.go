package dipstick

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/mattwalters/dipstick/internal/adapters/antigravity"
	"github.com/mattwalters/dipstick/internal/adapters/claude"
	"github.com/mattwalters/dipstick/internal/adapters/codex"
)

// DefaultTimeout is the default execution timeout for a collection run.
const DefaultTimeout = 30 * time.Second

type config struct {
	providers     []ProviderID
	timeout       time.Duration
	sourceTimeout time.Duration
	sourcePolicy  SourcePolicy
	strict        bool
	adapters      map[ProviderID]Adapter
}

// Option configures the collection run.
type Option func(*config)

// WithProviders specifies which providers to collect data from.
func WithProviders(providers ...ProviderID) Option {
	return func(c *config) {
		c.providers = append(c.providers, providers...)
	}
}

// WithTimeout sets an overall timeout for the collection run.
func WithTimeout(d time.Duration) Option {
	return func(c *config) {
		c.timeout = d
	}
}

// WithSourceTimeout sets a per-source timeout for individual source execution.
func WithSourceTimeout(d time.Duration) Option {
	return func(c *config) {
		c.sourceTimeout = d
	}
}

// WithSourcePolicy sets the data source policy.
func WithSourcePolicy(policy SourcePolicy) Option {
	return func(c *config) {
		c.sourcePolicy = policy
	}
}

// WithStrict toggles strict mode: when enabled, drift warnings are treated as failures.
func WithStrict(strict bool) Option {
	return func(c *config) {
		c.strict = strict
	}
}

// WithAdapter registers or overrides an adapter implementation for a provider.
func WithAdapter(adapter Adapter) Option {
	return func(c *config) {
		if adapter == nil {
			return
		}
		if c.adapters == nil {
			c.adapters = make(map[ProviderID]Adapter)
		}
		c.adapters[adapter.ID()] = adapter
	}
}

// Providers returns all supported provider IDs in sorted order.
func Providers() []ProviderID {
	list := make([]ProviderID, len(AllProviders))
	copy(list, AllProviders)
	sort.Slice(list, func(i, j int) bool {
		return list[i] < list[j]
	})
	return list
}

var defaultAdapterRegistry = map[ProviderID]func() Adapter{
	ProviderAntigravity: func() Adapter {
		return antigravity.New()
	},
	ProviderClaude: func() Adapter {
		return claude.New()
	},
	ProviderCodex: func() Adapter {
		return codex.New()
	},
	ProviderOpenCode: func() Adapter {
		return newOpenCodeAdapter()
	},
}

// Collect gathers usage reports from configured providers by walking each
// adapter's tiered source ladder.
// Single provider failures are recorded in Report.Errors.
// Whole-run failures (such as invalid configuration or cancelled context) return an error.
func Collect(ctx context.Context, opts ...Option) (*Report, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	cfg := &config{
		sourcePolicy:  SourcePolicyDefault,
		sourceTimeout: 5 * time.Second,
		adapters:      make(map[ProviderID]Adapter),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}

	if cfg.timeout < 0 {
		return nil, fmt.Errorf("invalid timeout: %v", cfg.timeout)
	}

	if cfg.sourceTimeout < 0 {
		return nil, fmt.Errorf("invalid source timeout: %v", cfg.sourceTimeout)
	}

	if cfg.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.timeout)
		defer cancel()
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	targetProviders := cfg.providers
	if len(targetProviders) == 0 {
		targetProviders = Providers()
		// A caller who registered adapters but named no providers means those
		// adapters, not the built-in roster plus them.
		if len(cfg.adapters) > 0 {
			targetProviders = nil
			for id := range cfg.adapters {
				targetProviders = append(targetProviders, id)
			}
			sort.Slice(targetProviders, func(i, j int) bool {
				return targetProviders[i] < targetProviders[j]
			})
		}
	}

	seen := make(map[ProviderID]bool)
	var ordered []ProviderID
	for _, p := range targetProviders {
		if _, ok := cfg.adapters[p]; !ok {
			if _, ok := defaultAdapterRegistry[p]; !ok {
				return nil, fmt.Errorf("unknown provider: %q", p)
			}
		}
		if !seen[p] {
			seen[p] = true
			ordered = append(ordered, p)
		}
	}

	activeAdapters := make(map[ProviderID]Adapter, len(ordered))
	for _, p := range ordered {
		if custom, ok := cfg.adapters[p]; ok {
			activeAdapters[p] = custom
		} else {
			activeAdapters[p] = defaultAdapterRegistry[p]()
		}
	}

	resolver := NewResolver(activeAdapters, ResolverConfig{
		SourcePolicy:  cfg.sourcePolicy,
		SourceTimeout: cfg.sourceTimeout,
		Strict:        cfg.strict,
	})

	return resolver.Resolve(ctx, ordered)
}
