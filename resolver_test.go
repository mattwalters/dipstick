package dipstick_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mattwalters/dipstick"
)

// fakeSource is a controllable mock implementation of dipstick.Source.
type fakeSource struct {
	id             dipstick.SourceID
	tier           dipstick.SourceTier
	available      bool
	availableDelay time.Duration
	fetchReport    *dipstick.ProviderReport
	fetchErr       error
	fetchDelay     time.Duration

	availableCalls int32
	fetchCalls     int32
}

func (s *fakeSource) ID() dipstick.SourceID     { return s.id }
func (s *fakeSource) Tier() dipstick.SourceTier { return s.tier }

func (s *fakeSource) Available(ctx context.Context) bool {
	atomic.AddInt32(&s.availableCalls, 1)
	if s.availableDelay > 0 {
		select {
		case <-time.After(s.availableDelay):
		case <-ctx.Done():
			return false
		}
	}
	return s.available
}

func (s *fakeSource) Fetch(ctx context.Context) (*dipstick.ProviderReport, error) {
	atomic.AddInt32(&s.fetchCalls, 1)
	if s.fetchDelay > 0 {
		select {
		case <-time.After(s.fetchDelay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.fetchErr != nil {
		return nil, s.fetchErr
	}
	return s.fetchReport, nil
}

// findProvider returns the ProviderReport recorded for id, if any.
func findProvider(report *dipstick.Report, id dipstick.ProviderID) (dipstick.ProviderReport, bool) {
	for _, pr := range report.Providers {
		if pr.Provider == id {
			return pr, true
		}
	}
	return dipstick.ProviderReport{}, false
}

// totalTokens reads a report's total token count, or -1 when it carries none.
// dipstick.v1 makes the token counters pointers precisely so that "zero" and
// "never reported" stay distinguishable, so the tests must not collapse them.
func totalTokens(pr dipstick.ProviderReport) int64 {
	if pr.Tokens == nil || pr.Tokens.TotalTokens == nil {
		return -1
	}
	return *pr.Tokens.TotalTokens
}

// fakeAdapter is a mock implementation of dipstick.Adapter.
type fakeAdapter struct {
	id        dipstick.ProviderID
	sources   []dipstick.Source
	compat    dipstick.Compat
	detection *dipstick.Detection
	detectErr error
}

func (a *fakeAdapter) ID() dipstick.ProviderID { return a.id }

func (a *fakeAdapter) Detect(ctx context.Context) (dipstick.Detection, error) {
	if a.detectErr != nil {
		return dipstick.Detection{}, a.detectErr
	}
	if a.detection != nil {
		return *a.detection, nil
	}
	return dipstick.Detection{
		Installed:     true,
		Authenticated: true,
	}, nil
}

func (a *fakeAdapter) Sources() []dipstick.Source {
	return a.sources
}

func (a *fakeAdapter) Compat() dipstick.Compat {
	return a.compat
}

func TestResolver_FirstTierWins(t *testing.T) {
	tier1Source := &fakeSource{
		id:        dipstick.SourceOAuthAPI,
		tier:      dipstick.TierAPI,
		available: true,
		fetchReport: &dipstick.ProviderReport{
			Tokens: &dipstick.TokenUsage{TotalTokens: dipstick.Ptr(int64(1000))},
		},
	}
	tier2Source := &fakeSource{
		id:        dipstick.SourceLocalState,
		tier:      dipstick.TierLocalState,
		available: true,
		fetchReport: &dipstick.ProviderReport{
			Tokens: &dipstick.TokenUsage{TotalTokens: dipstick.Ptr(int64(500))},
		},
	}

	adapter := &fakeAdapter{
		id:      dipstick.ProviderClaude,
		sources: []dipstick.Source{tier1Source, tier2Source},
	}

	resolver := dipstick.NewResolver(map[dipstick.ProviderID]dipstick.Adapter{
		dipstick.ProviderClaude: adapter,
	}, dipstick.ResolverConfig{
		SourcePolicy:  dipstick.SourcePolicyDefault,
		SourceTimeout: time.Second,
	})

	ctx := context.Background()
	report, err := resolver.Resolve(ctx, []dipstick.ProviderID{dipstick.ProviderClaude})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pr, ok := findProvider(report, dipstick.ProviderClaude)
	if !ok {
		t.Fatalf("missing claude report")
	}

	if len(report.Errors) != 0 {
		t.Fatalf("unexpected provider errors: %+v", report.Errors)
	}
	if pr.Source != dipstick.SourceOAuthAPI {
		t.Errorf("expected source %s, got %s", dipstick.SourceOAuthAPI, pr.Source)
	}
	if pr.Tier != dipstick.TierAPI {
		t.Errorf("expected tier %v, got %v", dipstick.TierAPI, pr.Tier)
	}
	if pr.Confidence != dipstick.ConfidenceExact {
		t.Errorf("expected confidence %s, got %s", dipstick.ConfidenceExact, pr.Confidence)
	}
	if totalTokens(pr) != 1000 {
		t.Errorf("expected 1000 tokens, got %d", totalTokens(pr))
	}

	// Verify lower tier was never called
	if atomic.LoadInt32(&tier2Source.availableCalls) != 0 {
		t.Errorf("tier 2 available check should not have been called")
	}
	if atomic.LoadInt32(&tier2Source.fetchCalls) != 0 {
		t.Errorf("tier 2 fetch should not have been called")
	}

	// Verify attempt history
	if len(pr.Attempts) != 1 {
		t.Fatalf("expected 1 attempt, got %d", len(pr.Attempts))
	}
	if pr.Attempts[0].Status != dipstick.AttemptStatusSuccess {
		t.Errorf("expected attempt status success, got %s", pr.Attempts[0].Status)
	}
	if pr.Attempts[0].SourceID != dipstick.SourceOAuthAPI {
		t.Errorf("expected attempt source %s, got %s", dipstick.SourceOAuthAPI, pr.Attempts[0].SourceID)
	}
}

func TestResolver_FallbackOnError(t *testing.T) {
	tier1Source := &fakeSource{
		id:        dipstick.SourceOAuthAPI,
		tier:      dipstick.TierAPI,
		available: true,
		fetchErr:  errors.New("401 Unauthorized"),
	}
	tier2Source := &fakeSource{
		id:        dipstick.SourceLocalState,
		tier:      dipstick.TierLocalState,
		available: true,
		fetchReport: &dipstick.ProviderReport{
			Tokens: &dipstick.TokenUsage{TotalTokens: dipstick.Ptr(int64(750))},
		},
	}

	adapter := &fakeAdapter{
		id:      dipstick.ProviderCodex,
		sources: []dipstick.Source{tier1Source, tier2Source},
	}

	resolver := dipstick.NewResolver(map[dipstick.ProviderID]dipstick.Adapter{
		dipstick.ProviderCodex: adapter,
	}, dipstick.ResolverConfig{
		SourcePolicy:  dipstick.SourcePolicyDefault,
		SourceTimeout: time.Second,
	})

	ctx := context.Background()
	report, err := resolver.Resolve(ctx, []dipstick.ProviderID{dipstick.ProviderCodex})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pr, ok := findProvider(report, dipstick.ProviderCodex)
	if !ok {
		t.Fatalf("missing codex report")
	}

	if len(report.Errors) != 0 {
		t.Fatalf("unexpected provider errors: %+v", report.Errors)
	}
	if pr.Source != dipstick.SourceLocalState {
		t.Errorf("expected source %s, got %s", dipstick.SourceLocalState, pr.Source)
	}
	if pr.Tier != dipstick.TierLocalState {
		t.Errorf("expected tier %v, got %v", dipstick.TierLocalState, pr.Tier)
	}
	if totalTokens(pr) != 750 {
		t.Errorf("expected 750 tokens, got %d", totalTokens(pr))
	}

	if len(pr.Attempts) != 2 {
		t.Fatalf("expected 2 attempts, got %d", len(pr.Attempts))
	}
	if pr.Attempts[0].Status != dipstick.AttemptStatusError || pr.Attempts[0].Error != "401 Unauthorized" {
		t.Errorf("unexpected attempt 0: %+v", pr.Attempts[0])
	}
	if pr.Attempts[1].Status != dipstick.AttemptStatusSuccess {
		t.Errorf("unexpected attempt 1: %+v", pr.Attempts[1])
	}
}

func TestResolver_FallbackOnUnavailable(t *testing.T) {
	tier1Source := &fakeSource{
		id:        dipstick.SourceOAuthAPI,
		tier:      dipstick.TierAPI,
		available: false,
		fetchReport: &dipstick.ProviderReport{
			Tokens: &dipstick.TokenUsage{TotalTokens: dipstick.Ptr(int64(1000))},
		},
	}
	tier2Source := &fakeSource{
		id:        dipstick.SourceAppServer,
		tier:      dipstick.TierLocalRPC,
		available: true,
		fetchReport: &dipstick.ProviderReport{
			Tokens: &dipstick.TokenUsage{TotalTokens: dipstick.Ptr(int64(300))},
		},
	}

	adapter := &fakeAdapter{
		id:      dipstick.ProviderOpenCode,
		sources: []dipstick.Source{tier1Source, tier2Source},
	}

	resolver := dipstick.NewResolver(map[dipstick.ProviderID]dipstick.Adapter{
		dipstick.ProviderOpenCode: adapter,
	}, dipstick.ResolverConfig{})

	ctx := context.Background()
	report, err := resolver.Resolve(ctx, []dipstick.ProviderID{dipstick.ProviderOpenCode})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pr, _ := findProvider(report, dipstick.ProviderOpenCode)
	if len(report.Errors) != 0 {
		t.Fatalf("unexpected provider errors: %+v", report.Errors)
	}
	if pr.Source != dipstick.SourceAppServer {
		t.Errorf("expected source %s, got %s", dipstick.SourceAppServer, pr.Source)
	}
	if atomic.LoadInt32(&tier1Source.fetchCalls) != 0 {
		t.Errorf("tier 1 fetch should not have been called when unavailable")
	}

	if len(pr.Attempts) != 2 {
		t.Fatalf("expected 2 attempts, got %d", len(pr.Attempts))
	}
	if pr.Attempts[0].Status != dipstick.AttemptStatusUnavailable {
		t.Errorf("expected attempt 0 unavailable, got %s", pr.Attempts[0].Status)
	}
	if pr.Attempts[1].Status != dipstick.AttemptStatusSuccess {
		t.Errorf("expected attempt 1 success, got %s", pr.Attempts[1].Status)
	}
}

func TestResolver_AllSourcesUnavailableOrError(t *testing.T) {
	tier1Source := &fakeSource{
		id:        dipstick.SourceOAuthAPI,
		tier:      dipstick.TierAPI,
		available: false,
	}
	tier2Source := &fakeSource{
		id:        dipstick.SourceLocalState,
		tier:      dipstick.TierLocalState,
		available: true,
		fetchErr:  errors.New("corrupt json"),
	}
	tier3Source := &fakeSource{
		id:        dipstick.SourceTranscript,
		tier:      dipstick.TierTranscripts,
		available: false,
	}

	adapter := &fakeAdapter{
		id:      dipstick.ProviderAntigravity,
		sources: []dipstick.Source{tier1Source, tier2Source, tier3Source},
	}

	resolver := dipstick.NewResolver(map[dipstick.ProviderID]dipstick.Adapter{
		dipstick.ProviderAntigravity: adapter,
	}, dipstick.ResolverConfig{})

	ctx := context.Background()
	report, err := resolver.Resolve(ctx, []dipstick.ProviderID{dipstick.ProviderAntigravity})
	if err != nil {
		t.Fatalf("unexpected whole-run error: %v", err)
	}

	// An exhausted ladder produces no ProviderReport: dipstick.v1 requires a
	// source and a confidence on every entry in Providers, and there is
	// nothing truthful to put in either. The failure lives in Errors instead.
	if _, ok := findProvider(report, dipstick.ProviderAntigravity); ok {
		t.Errorf("expected no provider report when every source fails")
	}

	if len(report.Errors) != 1 {
		t.Fatalf("expected 1 provider error in report, got %d", len(report.Errors))
	}
	pe := report.Errors[0]
	if pe.Provider != dipstick.ProviderAntigravity {
		t.Errorf("expected error provider %s, got %s", dipstick.ProviderAntigravity, pe.Provider)
	}
	// One source was available and errored, which outranks the two that never
	// ran, so the run is reported as an upstream failure and is retryable.
	if pe.Reason != dipstick.ReasonUpstreamError {
		t.Errorf("expected reason %s, got %s", dipstick.ReasonUpstreamError, pe.Reason)
	}
	if !pe.Retryable {
		t.Errorf("expected an upstream error to be retryable")
	}

	if len(pe.Attempts) != 3 {
		t.Fatalf("expected 3 attempts, got %d", len(pe.Attempts))
	}
	if pe.Attempts[0].Status != dipstick.AttemptStatusUnavailable {
		t.Errorf("expected attempt 0 unavailable, got %s", pe.Attempts[0].Status)
	}
	if pe.Attempts[1].Status != dipstick.AttemptStatusError {
		t.Errorf("expected attempt 1 error, got %s", pe.Attempts[1].Status)
	}
	if pe.Attempts[2].Status != dipstick.AttemptStatusUnavailable {
		t.Errorf("expected attempt 2 unavailable, got %s", pe.Attempts[2].Status)
	}
}

func TestResolver_PerSourceTimeout(t *testing.T) {
	slowTier1 := &fakeSource{
		id:         dipstick.SourceOAuthAPI,
		tier:       dipstick.TierAPI,
		available:  true,
		fetchDelay: 500 * time.Millisecond,
		fetchReport: &dipstick.ProviderReport{
			Tokens: &dipstick.TokenUsage{TotalTokens: dipstick.Ptr(int64(1000))},
		},
	}
	fastTier2 := &fakeSource{
		id:        dipstick.SourceLocalState,
		tier:      dipstick.TierLocalState,
		available: true,
		fetchReport: &dipstick.ProviderReport{
			Tokens: &dipstick.TokenUsage{TotalTokens: dipstick.Ptr(int64(200))},
		},
	}

	adapter := &fakeAdapter{
		id:      dipstick.ProviderClaude,
		sources: []dipstick.Source{slowTier1, fastTier2},
	}

	resolver := dipstick.NewResolver(map[dipstick.ProviderID]dipstick.Adapter{
		dipstick.ProviderClaude: adapter,
	}, dipstick.ResolverConfig{
		SourceTimeout: 50 * time.Millisecond,
	})

	start := time.Now()
	ctx := context.Background()
	report, err := resolver.Resolve(ctx, []dipstick.ProviderID{dipstick.ProviderClaude})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Ensure execution did not block for 500ms
	if elapsed >= 400*time.Millisecond {
		t.Errorf("collection took too long: %v (timeout should have triggered around 50ms)", elapsed)
	}

	pr, _ := findProvider(report, dipstick.ProviderClaude)
	if len(report.Errors) != 0 {
		t.Fatalf("unexpected provider errors: %+v", report.Errors)
	}
	if pr.Source != dipstick.SourceLocalState {
		t.Errorf("expected fallback to %s, got %s", dipstick.SourceLocalState, pr.Source)
	}
	if totalTokens(pr) != 200 {
		t.Errorf("expected 200 tokens from tier 2, got %d", totalTokens(pr))
	}

	if len(pr.Attempts) != 2 {
		t.Fatalf("expected 2 attempts, got %d", len(pr.Attempts))
	}
	if pr.Attempts[0].Status != dipstick.AttemptStatusTimeout {
		t.Errorf("expected attempt 0 timeout, got %s", pr.Attempts[0].Status)
	}
	if pr.Attempts[1].Status != dipstick.AttemptStatusSuccess {
		t.Errorf("expected attempt 1 success, got %s", pr.Attempts[1].Status)
	}
}

func TestResolver_AvailableTimeout(t *testing.T) {
	hangingAvailTier1 := &fakeSource{
		id:             dipstick.SourceOAuthAPI,
		tier:           dipstick.TierAPI,
		available:      true,
		availableDelay: 500 * time.Millisecond,
	}
	fastTier2 := &fakeSource{
		id:        dipstick.SourceLocalState,
		tier:      dipstick.TierLocalState,
		available: true,
		fetchReport: &dipstick.ProviderReport{
			Tokens: &dipstick.TokenUsage{TotalTokens: dipstick.Ptr(int64(123))},
		},
	}

	adapter := &fakeAdapter{
		id:      dipstick.ProviderClaude,
		sources: []dipstick.Source{hangingAvailTier1, fastTier2},
	}

	resolver := dipstick.NewResolver(map[dipstick.ProviderID]dipstick.Adapter{
		dipstick.ProviderClaude: adapter,
	}, dipstick.ResolverConfig{
		SourceTimeout: 50 * time.Millisecond,
	})

	start := time.Now()
	report, err := resolver.Resolve(context.Background(), []dipstick.ProviderID{dipstick.ProviderClaude})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if elapsed >= 400*time.Millisecond {
		t.Errorf("availability timeout took too long: %v", elapsed)
	}

	pr, _ := findProvider(report, dipstick.ProviderClaude)
	if pr.Source != dipstick.SourceLocalState {
		t.Errorf("expected fallback to %s, got %s", dipstick.SourceLocalState, pr.Source)
	}
	if pr.Attempts[0].Status != dipstick.AttemptStatusTimeout {
		t.Errorf("expected attempt 0 timeout for hanging availability, got %s", pr.Attempts[0].Status)
	}
	if pr.Attempts[1].Status != dipstick.AttemptStatusSuccess {
		t.Errorf("expected attempt 1 success, got %s", pr.Attempts[1].Status)
	}
}

func TestResolver_ConcurrencyAndCancellation(t *testing.T) {
	t.Run("concurrency", func(t *testing.T) {
		adapters := make(map[dipstick.ProviderID]dipstick.Adapter)
		providerIDs := []dipstick.ProviderID{
			dipstick.ProviderClaude,
			dipstick.ProviderCodex,
			dipstick.ProviderAntigravity,
			dipstick.ProviderOpenCode,
		}

		for _, id := range providerIDs {
			src := &fakeSource{
				id:         dipstick.SourceOAuthAPI,
				tier:       dipstick.TierAPI,
				available:  true,
				fetchDelay: 50 * time.Millisecond,
				fetchReport: &dipstick.ProviderReport{
					Tokens: &dipstick.TokenUsage{TotalTokens: dipstick.Ptr(int64(100))},
				},
			}
			adapters[id] = &fakeAdapter{id: id, sources: []dipstick.Source{src}}
		}

		resolver := dipstick.NewResolver(adapters, dipstick.ResolverConfig{
			SourceTimeout: time.Second,
		})

		start := time.Now()
		ctx := context.Background()
		report, err := resolver.Resolve(ctx, providerIDs)
		elapsed := time.Since(start)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(report.Providers) != 4 {
			t.Fatalf("expected 4 provider reports, got %d", len(report.Providers))
		}

		// 4 providers with 50ms delay running concurrently should take well under 150ms
		if elapsed >= 180*time.Millisecond {
			t.Errorf("concurrent collection took %v, expected concurrent execution < 180ms", elapsed)
		}
	})

	t.Run("cancellation", func(t *testing.T) {
		slowSrc := &fakeSource{
			id:         dipstick.SourceOAuthAPI,
			tier:       dipstick.TierAPI,
			available:  true,
			fetchDelay: 500 * time.Millisecond,
		}
		adapter := &fakeAdapter{id: dipstick.ProviderClaude, sources: []dipstick.Source{slowSrc}}

		resolver := dipstick.NewResolver(map[dipstick.ProviderID]dipstick.Adapter{
			dipstick.ProviderClaude: adapter,
		}, dipstick.ResolverConfig{SourceTimeout: 2 * time.Second})

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()

		_, err := resolver.Resolve(ctx, []dipstick.ProviderID{dipstick.ProviderClaude})
		if err == nil {
			t.Fatalf("expected cancellation error, got nil")
		}
	})
}

func TestResolver_SourcePolicyFiltering(t *testing.T) {
	t.Run("offline_policy", func(t *testing.T) {
		tier1Source := &fakeSource{
			id:        dipstick.SourceOAuthAPI,
			tier:      dipstick.TierAPI,
			available: true,
			fetchReport: &dipstick.ProviderReport{
				Tokens: &dipstick.TokenUsage{TotalTokens: dipstick.Ptr(int64(1000))},
			},
		}
		tier2Source := &fakeSource{
			id:        dipstick.SourceLocalState,
			tier:      dipstick.TierLocalState,
			available: true,
			fetchReport: &dipstick.ProviderReport{
				Tokens: &dipstick.TokenUsage{TotalTokens: dipstick.Ptr(int64(500))},
			},
		}

		adapter := &fakeAdapter{
			id:      dipstick.ProviderClaude,
			sources: []dipstick.Source{tier1Source, tier2Source},
		}

		resolver := dipstick.NewResolver(map[dipstick.ProviderID]dipstick.Adapter{
			dipstick.ProviderClaude: adapter,
		}, dipstick.ResolverConfig{
			SourcePolicy: dipstick.SourcePolicyLocal,
		})

		report, err := resolver.Resolve(context.Background(), []dipstick.ProviderID{dipstick.ProviderClaude})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		pr, _ := findProvider(report, dipstick.ProviderClaude)
		if pr.Source != dipstick.SourceLocalState {
			t.Errorf("expected source %s, got %s", dipstick.SourceLocalState, pr.Source)
		}
		if atomic.LoadInt32(&tier1Source.fetchCalls) != 0 {
			t.Errorf("tier 1 should have been skipped by offline policy")
		}

		if len(pr.Attempts) != 2 {
			t.Fatalf("expected 2 attempts, got %d", len(pr.Attempts))
		}
		if pr.Attempts[0].Status != dipstick.AttemptStatusSkipped {
			t.Errorf("expected attempt 0 skipped, got %s", pr.Attempts[0].Status)
		}
		if pr.Attempts[1].Status != dipstick.AttemptStatusSuccess {
			t.Errorf("expected attempt 1 success, got %s", pr.Attempts[1].Status)
		}
	})

	t.Run("pin_tier_policy", func(t *testing.T) {
		tier1Source := &fakeSource{
			id:        dipstick.SourceOAuthAPI,
			tier:      dipstick.TierAPI,
			available: true,
			fetchReport: &dipstick.ProviderReport{
				Tokens: &dipstick.TokenUsage{TotalTokens: dipstick.Ptr(int64(1000))},
			},
		}
		tier3Source := &fakeSource{
			id:        dipstick.SourceAppServer,
			tier:      dipstick.TierLocalRPC,
			available: true,
			fetchReport: &dipstick.ProviderReport{
				Tokens: &dipstick.TokenUsage{TotalTokens: dipstick.Ptr(int64(700))},
			},
		}

		adapter := &fakeAdapter{
			id:      dipstick.ProviderOpenCode,
			sources: []dipstick.Source{tier1Source, tier3Source},
		}

		resolver := dipstick.NewResolver(map[dipstick.ProviderID]dipstick.Adapter{
			dipstick.ProviderOpenCode: adapter,
		}, dipstick.ResolverConfig{
			SourcePolicy: dipstick.PinTierPolicy(dipstick.TierLocalRPC),
		})

		report, err := resolver.Resolve(context.Background(), []dipstick.ProviderID{dipstick.ProviderOpenCode})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		pr, _ := findProvider(report, dipstick.ProviderOpenCode)
		if pr.Source != dipstick.SourceAppServer {
			t.Errorf("expected source %s, got %s", dipstick.SourceAppServer, pr.Source)
		}
		if pr.Attempts[0].Status != dipstick.AttemptStatusSkipped {
			t.Errorf("expected tier 1 attempt skipped, got %s", pr.Attempts[0].Status)
		}
	})

	t.Run("tier_floor_policy", func(t *testing.T) {
		tier1Source := &fakeSource{id: dipstick.SourceOAuthAPI, tier: dipstick.TierAPI, available: true}
		tier2Source := &fakeSource{id: dipstick.SourceLocalState, tier: dipstick.TierLocalState, available: true}
		tier4Source := &fakeSource{
			id:          dipstick.SourceTranscript,
			tier:        dipstick.TierTranscripts,
			available:   true,
			fetchReport: &dipstick.ProviderReport{Tokens: &dipstick.TokenUsage{TotalTokens: dipstick.Ptr(int64(42))}},
		}

		adapter := &fakeAdapter{
			id:      dipstick.ProviderCodex,
			sources: []dipstick.Source{tier1Source, tier2Source, tier4Source},
		}

		resolver := dipstick.NewResolver(map[dipstick.ProviderID]dipstick.Adapter{
			dipstick.ProviderCodex: adapter,
		}, dipstick.ResolverConfig{
			SourcePolicy: dipstick.TierFloorPolicy(dipstick.TierTranscripts),
		})

		report, err := resolver.Resolve(context.Background(), []dipstick.ProviderID{dipstick.ProviderCodex})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		pr, _ := findProvider(report, dipstick.ProviderCodex)
		if pr.Source != dipstick.SourceTranscript {
			t.Errorf("expected source %s, got %s", dipstick.SourceTranscript, pr.Source)
		}
		if len(pr.Attempts) != 3 {
			t.Fatalf("expected 3 attempts, got %d", len(pr.Attempts))
		}
		if pr.Attempts[0].Status != dipstick.AttemptStatusSkipped || pr.Attempts[1].Status != dipstick.AttemptStatusSkipped {
			t.Errorf("expected first 2 attempts skipped")
		}
		if pr.Attempts[2].Status != dipstick.AttemptStatusSuccess {
			t.Errorf("expected attempt 2 success, got %s", pr.Attempts[2].Status)
		}
	})

	t.Run("specific_source_and_unrecognized_rejection", func(t *testing.T) {
		tier1Source := &fakeSource{id: dipstick.SourceOAuthAPI, tier: dipstick.TierAPI, available: true}
		tier2Source := &fakeSource{
			id:          dipstick.SourceLocalState,
			tier:        dipstick.TierLocalState,
			available:   true,
			fetchReport: &dipstick.ProviderReport{Tokens: &dipstick.TokenUsage{TotalTokens: dipstick.Ptr(int64(88))}},
		}

		adapter := &fakeAdapter{
			id:      dipstick.ProviderClaude,
			sources: []dipstick.Source{tier1Source, tier2Source},
		}

		// Test specific "local_state" policy
		resolverState := dipstick.NewResolver(map[dipstick.ProviderID]dipstick.Adapter{
			dipstick.ProviderClaude: adapter,
		}, dipstick.ResolverConfig{
			SourcePolicy: dipstick.SourcePolicyLocalState,
		})

		repState, err := resolverState.Resolve(context.Background(), []dipstick.ProviderID{dipstick.ProviderClaude})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		prState, _ := findProvider(repState, dipstick.ProviderClaude)
		if prState.Source != dipstick.SourceLocalState {
			t.Errorf("expected local_state to win, got %s", prState.Source)
		}
		if prState.Attempts[0].Status != dipstick.AttemptStatusSkipped {
			t.Errorf("expected Tier 1 to be skipped under local_state policy")
		}

		// Test completely unknown policy rejects all sources
		resolverUnknown := dipstick.NewResolver(map[dipstick.ProviderID]dipstick.Adapter{
			dipstick.ProviderClaude: adapter,
		}, dipstick.ResolverConfig{
			SourcePolicy: dipstick.SourcePolicy("unknown_nonexistent_policy"),
		})

		repUnknown, err := resolverUnknown.Resolve(context.Background(), []dipstick.ProviderID{dipstick.ProviderClaude})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := findProvider(repUnknown, dipstick.ProviderClaude); ok {
			t.Errorf("expected no provider report under an unrecognized policy")
		}
		if len(repUnknown.Errors) != 1 {
			t.Fatalf("expected 1 provider error, got %d", len(repUnknown.Errors))
		}
		peUnknown := repUnknown.Errors[0]
		// Nothing was eligible to run, which is not the same as nothing being
		// installed, so the ladder reports it as unsupported and not retryable.
		if peUnknown.Reason != dipstick.ReasonNotSupported {
			t.Errorf("expected reason %s, got %s", dipstick.ReasonNotSupported, peUnknown.Reason)
		}
		if peUnknown.Retryable {
			t.Errorf("a policy that excludes every source is not retryable")
		}
		if peUnknown.Attempts[0].Status != dipstick.AttemptStatusSkipped || peUnknown.Attempts[1].Status != dipstick.AttemptStatusSkipped {
			t.Errorf("expected all attempts skipped under unknown policy: %+v", peUnknown.Attempts)
		}
	})
}

func TestResolver_UnsortedAdapterSources(t *testing.T) {
	// Adapter returns sources in reverse order (Tier 5 first, then Tier 1)
	tier5Source := &fakeSource{
		id:        dipstick.SourceCLIStdout,
		tier:      dipstick.TierCLIScrape,
		available: true,
		fetchReport: &dipstick.ProviderReport{
			Tokens: &dipstick.TokenUsage{TotalTokens: dipstick.Ptr(int64(50))},
		},
	}
	tier1Source := &fakeSource{
		id:        dipstick.SourceOAuthAPI,
		tier:      dipstick.TierAPI,
		available: true,
		fetchReport: &dipstick.ProviderReport{
			Tokens: &dipstick.TokenUsage{TotalTokens: dipstick.Ptr(int64(100))},
		},
	}

	adapter := &fakeAdapter{
		id:      dipstick.ProviderClaude,
		sources: []dipstick.Source{tier5Source, tier1Source},
	}

	resolver := dipstick.NewResolver(map[dipstick.ProviderID]dipstick.Adapter{
		dipstick.ProviderClaude: adapter,
	}, dipstick.ResolverConfig{})

	report, err := resolver.Resolve(context.Background(), []dipstick.ProviderID{dipstick.ProviderClaude})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pr, _ := findProvider(report, dipstick.ProviderClaude)
	if pr.Source != dipstick.SourceOAuthAPI {
		t.Errorf("expected Tier 1 source to win even when unordered, got %s", pr.Source)
	}
	if atomic.LoadInt32(&tier5Source.fetchCalls) != 0 {
		t.Errorf("lower tier should not have been called")
	}
}

func TestResolver_NilSourcesInAdapter(t *testing.T) {
	tier1Source := &fakeSource{
		id:        dipstick.SourceOAuthAPI,
		tier:      dipstick.TierAPI,
		available: true,
		fetchReport: &dipstick.ProviderReport{
			Tokens: &dipstick.TokenUsage{TotalTokens: dipstick.Ptr(int64(999))},
		},
	}

	adapter := &fakeAdapter{
		id:      dipstick.ProviderClaude,
		sources: []dipstick.Source{nil, tier1Source, nil},
	}

	resolver := dipstick.NewResolver(map[dipstick.ProviderID]dipstick.Adapter{
		dipstick.ProviderClaude: adapter,
	}, dipstick.ResolverConfig{})

	report, err := resolver.Resolve(context.Background(), []dipstick.ProviderID{dipstick.ProviderClaude})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pr, _ := findProvider(report, dipstick.ProviderClaude)
	if totalTokens(pr) != 999 {
		t.Errorf("expected 999 tokens, got %d", totalTokens(pr))
	}
}

func TestCollect_WithCustomAdapter(t *testing.T) {
	customSource := &fakeSource{
		id:        dipstick.SourceLocalState,
		tier:      dipstick.TierLocalState,
		available: true,
		fetchReport: &dipstick.ProviderReport{
			Tokens: &dipstick.TokenUsage{TotalTokens: dipstick.Ptr(int64(777))},
		},
	}
	customAdapter := &fakeAdapter{
		id:      dipstick.ProviderClaude,
		sources: []dipstick.Source{customSource},
	}

	report, err := dipstick.Collect(context.Background(),
		dipstick.WithProviders(dipstick.ProviderClaude),
		dipstick.WithAdapter(customAdapter),
		dipstick.WithAdapter(nil), // Verify nil adapter safety
		dipstick.WithSourceTimeout(time.Second),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pr, _ := findProvider(report, dipstick.ProviderClaude)
	// dipstick.v1's TokenUsage carries no session counter, so the fixture's
	// Sessions field went away with DIP-1's Usage type; the token total is
	// what survives and is what this asserts.
	if totalTokens(pr) != 777 {
		t.Errorf("unexpected token total from custom adapter: %d", totalTokens(pr))
	}
	if pr.Source != dipstick.SourceLocalState {
		t.Errorf("expected source %s, got %s", dipstick.SourceLocalState, pr.Source)
	}
}

func TestResolver_ReasonPropagation(t *testing.T) {
	t.Run("drift alarm ReasonParseFailed propagation", func(t *testing.T) {
		// Tier 1 fails with ErrParseFailed (drift signal)
		tier1 := &fakeSource{
			id:        dipstick.SourceOAuthAPI,
			tier:      dipstick.TierAPI,
			available: true,
			fetchErr:  fmt.Errorf("json unmarshal failed: %w", dipstick.ErrParseFailed),
		}
		// Tier 2 fails with generic error
		tier2 := &fakeSource{
			id:        dipstick.SourceLocalState,
			tier:      dipstick.TierLocalState,
			available: true,
			fetchErr:  errors.New("500 internal server error"),
		}

		adapter := &fakeAdapter{
			id:      dipstick.ProviderClaude,
			sources: []dipstick.Source{tier1, tier2},
		}

		resolver := dipstick.NewResolver(map[dipstick.ProviderID]dipstick.Adapter{
			dipstick.ProviderClaude: adapter,
		}, dipstick.ResolverConfig{})

		report, err := resolver.Resolve(context.Background(), []dipstick.ProviderID{dipstick.ProviderClaude})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(report.Errors) != 1 {
			t.Fatalf("expected 1 error, got %d", len(report.Errors))
		}

		pe := report.Errors[0]
		// ReasonParseFailed has priority over ReasonUpstreamError
		if pe.Reason != dipstick.ReasonParseFailed {
			t.Errorf("expected reason %v, got %v", dipstick.ReasonParseFailed, pe.Reason)
		}
		if pe.Retryable {
			t.Errorf("parse_failed should not be retryable")
		}
		if pe.Source != dipstick.SourceOAuthAPI {
			t.Errorf("expected source %v where parse failure happened, got %v", dipstick.SourceOAuthAPI, pe.Source)
		}
		if !errors.Is(pe, dipstick.ErrParseFailed) {
			t.Errorf("expected errors.Is(pe, ErrParseFailed) to be true")
		}
	})

	t.Run("auth failure propagation", func(t *testing.T) {
		tier1 := &fakeSource{
			id:        dipstick.SourceOAuthAPI,
			tier:      dipstick.TierAPI,
			available: true,
			fetchErr:  dipstick.ErrNotAuthenticated,
		}
		tier2 := &fakeSource{
			id:        dipstick.SourceLocalState,
			tier:      dipstick.TierLocalState,
			available: false,
		}

		adapter := &fakeAdapter{
			id:      dipstick.ProviderCodex,
			sources: []dipstick.Source{tier1, tier2},
		}

		resolver := dipstick.NewResolver(map[dipstick.ProviderID]dipstick.Adapter{
			dipstick.ProviderCodex: adapter,
		}, dipstick.ResolverConfig{})

		report, err := resolver.Resolve(context.Background(), []dipstick.ProviderID{dipstick.ProviderCodex})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(report.Errors) != 1 {
			t.Fatalf("expected 1 error, got %d", len(report.Errors))
		}

		pe := report.Errors[0]
		if pe.Reason != dipstick.ReasonNotAuthenticated {
			t.Errorf("expected reason %v, got %v", dipstick.ReasonNotAuthenticated, pe.Reason)
		}
		if !errors.Is(pe, dipstick.ErrNotAuthenticated) {
			t.Errorf("expected errors.Is(pe, ErrNotAuthenticated) to be true")
		}
	})
}

func TestResolver_SecretScrubbingInResolution(t *testing.T) {
	fakeToken := "sk-ant-api03-abcdef1234567890abcdef123456"
	tier1 := &fakeSource{
		id:        dipstick.SourceOAuthAPI,
		tier:      dipstick.TierAPI,
		available: true,
		fetchErr:  fmt.Errorf("401 Unauthorized: Authorization: Bearer %s with token=%s", fakeToken, fakeToken),
	}

	adapter := &fakeAdapter{
		id:      dipstick.ProviderClaude,
		sources: []dipstick.Source{tier1},
	}

	resolver := dipstick.NewResolver(map[dipstick.ProviderID]dipstick.Adapter{
		dipstick.ProviderClaude: adapter,
	}, dipstick.ResolverConfig{})

	report, err := resolver.Resolve(context.Background(), []dipstick.ProviderID{dipstick.ProviderClaude})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(report.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(report.Errors))
	}

	pe := report.Errors[0]
	if strings.Contains(pe.Detail, fakeToken) {
		t.Errorf("pe.Detail leaked secret: %s", pe.Detail)
	}
	if !strings.Contains(pe.Detail, "[REDACTED]") {
		t.Errorf("pe.Detail expected to contain [REDACTED], got: %s", pe.Detail)
	}
}

func TestResolver_Compat_Outcomes(t *testing.T) {
	t.Run("in_range_normal_confidence", func(t *testing.T) {
		src := &fakeSource{
			id:        dipstick.SourceOAuthAPI,
			tier:      dipstick.TierAPI,
			available: true,
			fetchReport: &dipstick.ProviderReport{
				Tokens: &dipstick.TokenUsage{TotalTokens: dipstick.Ptr(int64(500))},
			},
		}
		adapter := &fakeAdapter{
			id:      dipstick.ProviderClaude,
			sources: []dipstick.Source{src},
			compat: dipstick.Compat{
				VerifiedRange: ">=2.1.0 <2.2.0",
				LastCheck:     "2026-08-29",
			},
			detection: &dipstick.Detection{
				Installed:     true,
				Authenticated: true,
				Version:       "2.1.4",
			},
		}

		resolver := dipstick.NewResolver(map[dipstick.ProviderID]dipstick.Adapter{
			dipstick.ProviderClaude: adapter,
		}, dipstick.ResolverConfig{})

		report, err := resolver.Resolve(context.Background(), []dipstick.ProviderID{dipstick.ProviderClaude})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(report.Errors) != 0 {
			t.Fatalf("unexpected errors: %+v", report.Errors)
		}
		pr, ok := findProvider(report, dipstick.ProviderClaude)
		if !ok {
			t.Fatalf("missing claude report")
		}
		if pr.Confidence != dipstick.ConfidenceExact {
			t.Errorf("expected ConfidenceExact for in-range version, got %s", pr.Confidence)
		}
		if len(pr.Warnings) != 0 {
			t.Errorf("expected 0 warnings for in-range version, got %v", pr.Warnings)
		}
		if pr.CLIVersion != "2.1.4" {
			t.Errorf("expected CLIVersion 2.1.4, got %s", pr.CLIVersion)
		}
	})

	t.Run("newer_than_verified_default_mode", func(t *testing.T) {
		src := &fakeSource{
			id:        dipstick.SourceOAuthAPI,
			tier:      dipstick.TierAPI,
			available: true,
			fetchReport: &dipstick.ProviderReport{
				Tokens: &dipstick.TokenUsage{TotalTokens: dipstick.Ptr(int64(500))},
			},
		}
		adapter := &fakeAdapter{
			id:      dipstick.ProviderClaude,
			sources: []dipstick.Source{src},
			compat: dipstick.Compat{
				VerifiedRange: ">=2.1.0 <2.2.0",
				LastCheck:     "2026-08-29",
			},
			detection: &dipstick.Detection{
				Installed:     true,
				Authenticated: true,
				Version:       "2.3.0",
			},
		}

		resolver := dipstick.NewResolver(map[dipstick.ProviderID]dipstick.Adapter{
			dipstick.ProviderClaude: adapter,
		}, dipstick.ResolverConfig{
			Strict: false,
		})

		report, err := resolver.Resolve(context.Background(), []dipstick.ProviderID{dipstick.ProviderClaude})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(report.Errors) != 0 {
			t.Fatalf("unexpected errors: %+v", report.Errors)
		}
		pr, ok := findProvider(report, dipstick.ProviderClaude)
		if !ok {
			t.Fatalf("missing claude report")
		}
		if pr.Confidence != dipstick.ConfidenceUnknown {
			t.Errorf("expected ConfidenceUnknown for newer version, got %s", pr.Confidence)
		}
		if len(pr.Warnings) != 1 {
			t.Fatalf("expected 1 warning, got %d: %v", len(pr.Warnings), pr.Warnings)
		}
		if !strings.Contains(pr.Warnings[0], "2.3.0") || !strings.Contains(pr.Warnings[0], ">=2.1.0 <2.2.0") {
			t.Errorf("warning did not name observed and verified versions: %s", pr.Warnings[0])
		}
	})

	t.Run("newer_than_verified_strict_mode", func(t *testing.T) {
		src := &fakeSource{
			id:        dipstick.SourceOAuthAPI,
			tier:      dipstick.TierAPI,
			available: true,
			fetchReport: &dipstick.ProviderReport{
				Tokens: &dipstick.TokenUsage{TotalTokens: dipstick.Ptr(int64(500))},
			},
		}
		adapter := &fakeAdapter{
			id:      dipstick.ProviderClaude,
			sources: []dipstick.Source{src},
			compat: dipstick.Compat{
				VerifiedRange: ">=2.1.0 <2.2.0",
				LastCheck:     "2026-08-29",
			},
			detection: &dipstick.Detection{
				Installed:     true,
				Authenticated: true,
				Version:       "2.3.0",
			},
		}

		resolver := dipstick.NewResolver(map[dipstick.ProviderID]dipstick.Adapter{
			dipstick.ProviderClaude: adapter,
		}, dipstick.ResolverConfig{
			Strict: true,
		})

		report, err := resolver.Resolve(context.Background(), []dipstick.ProviderID{dipstick.ProviderClaude})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(report.Providers) != 0 {
			t.Errorf("expected 0 providers under strict mode, got %d", len(report.Providers))
		}
		if len(report.Errors) != 1 {
			t.Fatalf("expected 1 error under strict mode, got %d", len(report.Errors))
		}
		pe := report.Errors[0]
		if pe.Reason != dipstick.ReasonUnsupportedVersion {
			t.Errorf("expected ReasonUnsupportedVersion, got %s", pe.Reason)
		}
		if pe.Retryable {
			t.Errorf("unsupported_version error should not be retryable")
		}
		if !strings.Contains(pe.Detail, "strict mode") {
			t.Errorf("expected detail to mention strict mode: %s", pe.Detail)
		}
	})

	t.Run("older_than_floor_aborts_before_fetch", func(t *testing.T) {
		src := &fakeSource{
			id:        dipstick.SourceOAuthAPI,
			tier:      dipstick.TierAPI,
			available: true,
			fetchReport: &dipstick.ProviderReport{
				Tokens: &dipstick.TokenUsage{TotalTokens: dipstick.Ptr(int64(500))},
			},
		}
		adapter := &fakeAdapter{
			id:      dipstick.ProviderClaude,
			sources: []dipstick.Source{src},
			compat: dipstick.Compat{
				VerifiedRange: ">=2.1.0 <2.2.0",
				LastCheck:     "2026-08-29",
			},
			detection: &dipstick.Detection{
				Installed:     true,
				Authenticated: true,
				Version:       "2.0.5",
			},
		}

		resolver := dipstick.NewResolver(map[dipstick.ProviderID]dipstick.Adapter{
			dipstick.ProviderClaude: adapter,
		}, dipstick.ResolverConfig{})

		report, err := resolver.Resolve(context.Background(), []dipstick.ProviderID{dipstick.ProviderClaude})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(report.Providers) != 0 {
			t.Errorf("expected 0 providers for older version, got %d", len(report.Providers))
		}
		if len(report.Errors) != 1 {
			t.Fatalf("expected 1 error, got %d", len(report.Errors))
		}
		pe := report.Errors[0]
		if pe.Reason != dipstick.ReasonUnsupportedVersion {
			t.Errorf("expected ReasonUnsupportedVersion, got %s", pe.Reason)
		}
		if pe.Retryable {
			t.Errorf("older version should not be retryable")
		}
		if !strings.Contains(pe.Detail, "older than supported floor") {
			t.Errorf("expected detail to mention older than supported floor: %s", pe.Detail)
		}
		// Verify source fetch was aborted before running
		if atomic.LoadInt32(&src.fetchCalls) != 0 {
			t.Errorf("source fetch should not have been called when version is older than floor")
		}
	})

	t.Run("source_report_populates_version_and_detects_drift", func(t *testing.T) {
		src := &fakeSource{
			id:        dipstick.SourceLocalState,
			tier:      dipstick.TierLocalState,
			available: true,
			fetchReport: &dipstick.ProviderReport{
				CLIVersion: "0.155.0",
				Tokens:     &dipstick.TokenUsage{TotalTokens: dipstick.Ptr(int64(300))},
			},
		}
		adapter := &fakeAdapter{
			id:      dipstick.ProviderCodex,
			sources: []dipstick.Source{src},
			compat: dipstick.Compat{
				VerifiedRange: ">=0.148.0 <0.150.0",
				LastCheck:     "2026-08-29",
			},
			detection: &dipstick.Detection{
				Installed:     true,
				Authenticated: true,
				Version:       "", // version not available during initial detect
			},
		}

		// Non-strict mode
		resolver := dipstick.NewResolver(map[dipstick.ProviderID]dipstick.Adapter{
			dipstick.ProviderCodex: adapter,
		}, dipstick.ResolverConfig{Strict: false})

		report, err := resolver.Resolve(context.Background(), []dipstick.ProviderID{dipstick.ProviderCodex})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		pr, ok := findProvider(report, dipstick.ProviderCodex)
		if !ok {
			t.Fatalf("missing codex report")
		}
		if pr.Confidence != dipstick.ConfidenceUnknown {
			t.Errorf("expected ConfidenceUnknown, got %s", pr.Confidence)
		}
		if len(pr.Warnings) != 1 {
			t.Fatalf("expected 1 warning, got %d", len(pr.Warnings))
		}

		// Strict mode
		resolverStrict := dipstick.NewResolver(map[dipstick.ProviderID]dipstick.Adapter{
			dipstick.ProviderCodex: adapter,
		}, dipstick.ResolverConfig{Strict: true})

		reportStrict, err := resolverStrict.Resolve(context.Background(), []dipstick.ProviderID{dipstick.ProviderCodex})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(reportStrict.Providers) != 0 {
			t.Errorf("expected 0 providers in strict mode, got %d", len(reportStrict.Providers))
		}
		if len(reportStrict.Errors) != 1 {
			t.Fatalf("expected 1 error in strict mode, got %d", len(reportStrict.Errors))
		}
		if reportStrict.Errors[0].Reason != dipstick.ReasonUnsupportedVersion {
			t.Errorf("expected ReasonUnsupportedVersion, got %s", reportStrict.Errors[0].Reason)
		}
	})
}
