package dipstick

import (
	"context"
	"errors"
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

// notCollecting builds a Provider that has no usage surface to read yet and
// says so as a ProviderError rather than as an empty ProviderReport.
//
// This is what the dipstick.v1 contract requires of a stub: every entry in
// Report.Providers must carry a real source and confidence, naming the tier
// its numbers actually came from, so a provider with nothing to say cannot
// appear there honestly. Report.Errors is where it belongs until the
// source-ladder resolver lands and these adapters start reading anything.
func notCollecting(id ProviderID, detail string) Provider {
	return &providerWrapper{
		id: id,
		collect: func(ctx context.Context, cfg Config) (ProviderReport, error) {
			if err := ctx.Err(); err != nil {
				return ProviderReport{}, err
			}
			return ProviderReport{}, ProviderError{
				Provider:  id,
				Reason:    ReasonNotSupported,
				Detail:    detail,
				Retryable: false,
			}
		},
	}
}

var adapterRegistry = map[ProviderID]func() Provider{
	ProviderAntigravity: func() Provider {
		_ = antigravity.New()
		return notCollecting(ProviderAntigravity, "antigravity exposes no usage or quota surface")
	},
	ProviderClaude: func() Provider {
		_ = claude.New()
		return notCollecting(ProviderClaude, "claude usage collection is not implemented yet")
	},
	ProviderCodex: func() Provider {
		_ = codex.New()
		return notCollecting(ProviderCodex, "codex usage collection is not implemented yet")
	},
	ProviderOpenCode: func() Provider {
		_ = opencode.New()
		return notCollecting(ProviderOpenCode, "opencode usage collection is not implemented yet")
	},
}

// providerErrorFor normalizes an adapter's error into the report's error
// model. An adapter that classified its own failure keeps that classification;
// anything else is an uncategorized upstream error, which is the one reason
// that does not claim to know more than we do.
func providerErrorFor(id ProviderID, err error) ProviderError {
	var pe ProviderError
	if errors.As(err, &pe) {
		if pe.Provider == "" {
			pe.Provider = id
		}
		return pe
	}
	return ProviderError{
		Provider:  id,
		Reason:    ReasonUpstreamError,
		Detail:    err.Error(),
		Retryable: false,
	}
}

// Collect gathers usage reports from configured providers.
// Single provider failures are recorded in Report.Errors.
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

	// Non-nil so a run that collects nothing marshals "providers": [], which
	// the schema requires present; a nil slice would encode as null.
	report := &Report{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   time.Now().UTC(),
		Providers:     make([]ProviderReport, 0, len(ordered)),
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
			report.Errors = append(report.Errors, providerErrorFor(id, err))
			continue
		}

		if pr.Provider == "" {
			pr.Provider = id
		}
		if pr.ObservedAt.IsZero() {
			pr.ObservedAt = report.GeneratedAt
		}
		report.Providers = append(report.Providers, pr)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return report, nil
}
