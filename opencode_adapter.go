package dipstick

import (
	"context"
	"errors"
	"fmt"

	"github.com/mattwalters/dipstick/internal/adapters/opencode"
)

// openCodeAdapter integrates the OpenCode provider adapter into the dipstick resolver ladder.
type openCodeAdapter struct {
	inner *opencode.Adapter
}

// newOpenCodeAdapter creates a new dipstick.Adapter for OpenCode.
func newOpenCodeAdapter(opts ...opencode.Option) Adapter {
	return &openCodeAdapter{
		inner: opencode.New(opts...),
	}
}

// ID returns the provider identifier.
func (a *openCodeAdapter) ID() ProviderID {
	return ProviderOpenCode
}

// Detect returns OpenCode detection information.
func (a *openCodeAdapter) Detect(ctx context.Context) (Detection, error) {
	det, err := a.inner.Detect(ctx)
	if err != nil {
		return Detection{}, err
	}
	return Detection{
		Installed:     det.Installed,
		Authenticated: det.Authenticated,
		Version:       det.Version,
		BinaryPath:    det.BinaryPath,
	}, nil
}

// Sources returns the source ladder for OpenCode (Tier 2 local state, Tier 3 local RPC, Tier 5 CLI).
func (a *openCodeAdapter) Sources() []Source {
	return []Source{
		&openCodeLocalStateSource{adapter: a.inner},
		&openCodeRPCSource{adapter: a.inner},
		&openCodeCLISource{adapter: a.inner},
	}
}

// Compat returns the verified compatibility range for OpenCode.
func (a *openCodeAdapter) Compat() Compat {
	return a.inner.Compat()
}

type openCodeLocalStateSource struct {
	adapter *opencode.Adapter
}

func (s *openCodeLocalStateSource) ID() SourceID {
	return SourceLocalState
}

func (s *openCodeLocalStateSource) Tier() SourceTier {
	return TierLocalState
}

func (s *openCodeLocalStateSource) Available(ctx context.Context) bool {
	return s.adapter.AvailableLocalState(ctx)
}

func (s *openCodeLocalStateSource) Fetch(ctx context.Context) (*ProviderReport, error) {
	usage, err := s.adapter.FetchLocalState(ctx)
	if err != nil {
		return nil, mapOpenCodeError(err)
	}
	return buildOpenCodeReport(SourceLocalState, TierLocalState, usage), nil
}

type openCodeRPCSource struct {
	adapter *opencode.Adapter
}

func (s *openCodeRPCSource) ID() SourceID {
	return SourceAppServer
}

func (s *openCodeRPCSource) Tier() SourceTier {
	return TierLocalRPC
}

func (s *openCodeRPCSource) Available(ctx context.Context) bool {
	return s.adapter.AvailableRPC(ctx)
}

func (s *openCodeRPCSource) Fetch(ctx context.Context) (*ProviderReport, error) {
	usage, err := s.adapter.FetchRPC(ctx)
	if err != nil {
		return nil, mapOpenCodeError(err)
	}
	return buildOpenCodeReport(SourceAppServer, TierLocalRPC, usage), nil
}

type openCodeCLISource struct {
	adapter *opencode.Adapter
}

func (s *openCodeCLISource) ID() SourceID {
	return SourceCLIStdout
}

func (s *openCodeCLISource) Tier() SourceTier {
	return TierCLIScrape
}

func (s *openCodeCLISource) Available(ctx context.Context) bool {
	return s.adapter.AvailableCLI(ctx)
}

func (s *openCodeCLISource) Fetch(ctx context.Context) (*ProviderReport, error) {
	usage, err := s.adapter.FetchCLI(ctx)
	if err != nil {
		return nil, mapOpenCodeError(err)
	}
	return buildOpenCodeReport(SourceCLIStdout, TierCLIScrape, usage), nil
}

func mapOpenCodeError(err error) error {
	if errors.Is(err, opencode.ErrParseFailed) {
		return fmt.Errorf("%w: %v", ErrParseFailed, err)
	}
	if errors.Is(err, opencode.ErrUpstreamError) {
		return fmt.Errorf("%w: %v", ErrUpstreamError, err)
	}
	return err
}

func buildOpenCodeReport(source SourceID, tier SourceTier, usage *opencode.TokenUsage) *ProviderReport {
	if usage == nil {
		return nil
	}
	return &ProviderReport{
		Provider:   ProviderOpenCode,
		Source:     source,
		Tier:       tier,
		Confidence: ConfidenceDerived,
		CLIVersion: usage.CLIVersion,
		Tokens: &TokenUsage{
			InputTokens:      Ptr(usage.InputTokens),
			OutputTokens:     Ptr(usage.OutputTokens),
			CacheReadTokens:  Ptr(usage.CacheReadTokens),
			CacheWriteTokens: Ptr(usage.CacheWriteTokens),
			TotalTokens:      Ptr(usage.TotalTokens),
		},
		ObservedAt: usage.ObservedAt,
	}
}
