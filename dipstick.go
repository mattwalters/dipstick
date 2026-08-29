package dipstick

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/mattwalters/dipstick/internal/adapters/antigravity"
	"github.com/mattwalters/dipstick/internal/adapters/claude"
	"github.com/mattwalters/dipstick/internal/adapters/codex"
	"github.com/mattwalters/dipstick/internal/adapters/opencode"
)

type config struct {
	providers    []ProviderID
	timeout      time.Duration
	sourcePolicy SourcePolicy
}

// Option configures the collection run.
type Option func(*config)

// WithProviders specifies which providers to collect data from.
func WithProviders(providers ...ProviderID) Option {
	return func(c *config) {
		c.providers = append(c.providers, providers...)
	}
}

// WithTimeout sets a timeout for the collection run.
func WithTimeout(d time.Duration) Option {
	return func(c *config) {
		c.timeout = d
	}
}

// WithSourcePolicy sets the data source policy.
func WithSourcePolicy(policy SourcePolicy) Option {
	return func(c *config) {
		c.sourcePolicy = policy
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

type providerWrapper struct {
	id      ProviderID
	collect func(ctx context.Context, cfg Config) (ProviderReport, error)
}

func (pw *providerWrapper) ID() ProviderID {
	return pw.id
}

func (pw *providerWrapper) Collect(ctx context.Context, cfg Config) (ProviderReport, error) {
	return pw.collect(ctx, cfg)
}

var adapterRegistry = map[ProviderID]func() Provider{
	ProviderAntigravity: func() Provider {
		_ = antigravity.New()
		return &providerWrapper{
			id: ProviderAntigravity,
			collect: func(ctx context.Context, cfg Config) (ProviderReport, error) {
				if err := ctx.Err(); err != nil {
					return ProviderReport{}, err
				}
				return ProviderReport{
					ProviderID: ProviderAntigravity,
					Usage:      Usage{},
				}, nil
			},
		}
	},
	ProviderClaude: func() Provider {
		_ = claude.New()
		return &providerWrapper{
			id: ProviderClaude,
			collect: func(ctx context.Context, cfg Config) (ProviderReport, error) {
				if err := ctx.Err(); err != nil {
					return ProviderReport{}, err
				}
				return ProviderReport{
					ProviderID: ProviderClaude,
					Usage:      Usage{},
				}, nil
			},
		}
	},
	ProviderCodex: func() Provider {
		_ = codex.New()
		return &providerWrapper{
			id: ProviderCodex,
			collect: func(ctx context.Context, cfg Config) (ProviderReport, error) {
				if err := ctx.Err(); err != nil {
					return ProviderReport{}, err
				}
				return ProviderReport{
					ProviderID: ProviderCodex,
					Usage:      Usage{},
				}, nil
			},
		}
	},
	ProviderOpenCode: func() Provider {
		_ = opencode.New()
		return &providerWrapper{
			id: ProviderOpenCode,
			collect: func(ctx context.Context, cfg Config) (ProviderReport, error) {
				if err := ctx.Err(); err != nil {
					return ProviderReport{}, err
				}
				return ProviderReport{
					ProviderID: ProviderOpenCode,
					Usage:      Usage{},
				}, nil
			},
		}
	},
}

// Collect gathers usage reports from configured providers.
// Single provider failures are recorded in the Report under the respective ProviderReport.
// Whole-run failures (such as invalid configuration or cancelled context) return an error.
func Collect(ctx context.Context, opts ...Option) (*Report, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	cfg := &config{
		sourcePolicy: SourcePolicyDefault,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}

	if cfg.timeout < 0 {
		return nil, fmt.Errorf("invalid timeout: %v", cfg.timeout)
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
	}

	seen := make(map[ProviderID]bool)
	var ordered []ProviderID
	for _, p := range targetProviders {
		if _, ok := adapterRegistry[p]; !ok {
			return nil, fmt.Errorf("unknown provider: %q", p)
		}
		if !seen[p] {
			seen[p] = true
			ordered = append(ordered, p)
		}
	}

	report := &Report{
		CollectedAt: time.Now().UTC(),
		Providers:   make(map[ProviderID]ProviderReport, len(ordered)),
	}

	providerCfg := Config{
		SourcePolicy: cfg.sourcePolicy,
	}

	for _, id := range ordered {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		factory := adapterRegistry[id]
		adapter := factory()

		pr, err := adapter.Collect(ctx, providerCfg)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}

		if err != nil {
			report.Providers[id] = ProviderReport{
				ProviderID: id,
				Err:        err,
			}
		} else {
			if pr.ProviderID == "" {
				pr.ProviderID = id
			}
			report.Providers[id] = pr
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return report, nil
}
