package dipstick

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

var (
	// ErrSourceTimeout is returned when a source fetch or availability check exceeds its allocated timeout.
	ErrSourceTimeout = errors.New("source fetch timeout")
)

// ResolverConfig contains configuration parameters for the Resolver engine.
type ResolverConfig struct {
	SourcePolicy  SourcePolicy
	SourceTimeout time.Duration
}

// Resolver orchestrates tiered source-ladder resolution across configured adapters concurrently.
type Resolver struct {
	adapters      map[ProviderID]Adapter
	sourcePolicy  SourcePolicy
	sourceTimeout time.Duration
}

// NewResolver creates a new Resolver instance.
func NewResolver(adapters map[ProviderID]Adapter, cfg ResolverConfig) *Resolver {
	sourceTimeout := cfg.SourceTimeout
	if sourceTimeout <= 0 {
		sourceTimeout = 5 * time.Second
	}
	sourcePolicy := cfg.SourcePolicy
	if sourcePolicy == "" {
		sourcePolicy = SourcePolicyDefault
	}
	adps := make(map[ProviderID]Adapter, len(adapters))
	for k, v := range adapters {
		if v != nil {
			adps[k] = v
		}
	}
	return &Resolver{
		adapters:      adps,
		sourcePolicy:  sourcePolicy,
		sourceTimeout: sourceTimeout,
	}
}

// providerResolutionResult carries one adapter's outcome back from its
// goroutine. Exactly one of report and pErr is set: a provider whose ladder
// yielded nothing produces no ProviderReport at all, because dipstick.v1
// requires a real source and confidence on every entry in Report.Providers
// and an exhausted ladder can honestly supply neither.
type providerResolutionResult struct {
	id     ProviderID
	report *ProviderReport
	pErr   *ProviderError
}

// Resolve concurrently executes ladder resolution across the requested provider IDs.
func (r *Resolver) Resolve(ctx context.Context, providerIDs []ProviderID) (*Report, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Non-nil so a run that resolves nothing still marshals "providers": [],
	// which the schema requires present.
	report := &Report{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   time.Now().UTC(),
		Providers:     make([]ProviderReport, 0, len(providerIDs)),
	}

	if len(providerIDs) == 0 {
		return report, nil
	}

	// Validate all provider IDs exist before starting concurrent workers
	for _, id := range providerIDs {
		if _, ok := r.adapters[id]; !ok {
			return nil, fmt.Errorf("unknown provider: %q", id)
		}
	}

	resultsChan := make(chan providerResolutionResult, len(providerIDs))
	var wg sync.WaitGroup

	for _, id := range providerIDs {
		adapter := r.adapters[id]
		wg.Add(1)
		go func(pID ProviderID, adp Adapter) {
			defer wg.Done()
			pr, pErr := r.resolveAdapter(ctx, adp)
			resultsChan <- providerResolutionResult{
				id:     pID,
				report: pr,
				pErr:   pErr,
			}
		}(id, adapter)
	}

	wg.Wait()
	close(resultsChan)

	// Context check in case cancellation occurred during execution
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Providers is a slice in dipstick.v1, so the report has an order and the
	// order has to be the caller's, not whichever goroutine finished first.
	// Drain into a map, then emit in the requested sequence.
	byID := make(map[ProviderID]providerResolutionResult, len(providerIDs))
	for res := range resultsChan {
		byID[res.id] = res
	}
	for _, id := range providerIDs {
		res, ok := byID[id]
		if !ok {
			continue
		}
		if res.report != nil {
			report.Providers = append(report.Providers, *res.report)
		}
		if res.pErr != nil {
			report.Errors = append(report.Errors, *res.pErr)
		}
	}

	return report, nil
}

// resolveAdapter walks a single adapter's source ladder in ascending tier
// order, highest fidelity first, and returns the first usable result.
//
// Exactly one of the two returns is non-nil, except under cancellation, where
// both are nil and Resolve's own context check turns it into a run-level
// error. A provider that never yields data produces a ProviderError rather
// than an empty ProviderReport: dipstick.v1 requires a source and a
// confidence naming where a datum came from, and an exhausted ladder has
// neither to give.
func (r *Resolver) resolveAdapter(ctx context.Context, adapter Adapter) (*ProviderReport, *ProviderError) {
	if ctx.Err() != nil {
		return nil, nil
	}

	sources := sortSourcesByTier(adapter.Sources())
	var attempts []SourceAttempt

	for _, src := range sources {
		if src == nil {
			continue
		}

		if ctx.Err() != nil {
			return nil, nil
		}

		// 1. Policy filtering
		if !r.sourcePolicy.Allows(src) {
			attempts = append(attempts, SourceAttempt{
				SourceID: src.ID(),
				Tier:     src.Tier(),
				Status:   AttemptStatusSkipped,
				Error:    "skipped by source policy",
			})
			continue
		}

		// 2. Availability check guarded by per-source timeout
		availStart := time.Now()
		availCtx, availCancel := context.WithTimeout(ctx, r.sourceTimeout)

		availCh := make(chan bool, 1)
		go func(s Source, aCtx context.Context) {
			availCh <- s.Available(aCtx)
		}(src, availCtx)

		var isAvailable bool
		var availTimedOut bool

		select {
		case avail := <-availCh:
			availCancel()
			isAvailable = avail
		case <-availCtx.Done():
			availCancel()
			if ctx.Err() != nil {
				return nil, nil
			}
			availTimedOut = true
		}

		if availTimedOut {
			attempts = append(attempts, SourceAttempt{
				SourceID: src.ID(),
				Tier:     src.Tier(),
				Status:   AttemptStatusTimeout,
				Duration: time.Since(availStart),
				Error:    "availability check timed out",
			})
			continue
		}

		if !isAvailable {
			attempts = append(attempts, SourceAttempt{
				SourceID: src.ID(),
				Tier:     src.Tier(),
				Status:   AttemptStatusUnavailable,
				Duration: time.Since(availStart),
				Error:    "source unavailable",
			})
			continue
		}

		// 3. Execution with per-source timeout
		fetchStart := time.Now()
		fetchCtx, fetchCancel := context.WithTimeout(ctx, r.sourceTimeout)

		type fetchResult struct {
			report *ProviderReport
			err    error
		}
		resCh := make(chan fetchResult, 1)

		go func(s Source, fCtx context.Context) {
			rep, err := s.Fetch(fCtx)
			resCh <- fetchResult{report: rep, err: err}
		}(src, fetchCtx)

		var rep *ProviderReport
		var fetchErr error
		var fetchTimedOut bool

		select {
		case res := <-resCh:
			fetchCancel()
			rep = res.report
			fetchErr = res.err
		case <-fetchCtx.Done():
			fetchCancel()
			if ctx.Err() != nil {
				return nil, nil
			}
			fetchTimedOut = true
		}

		duration := time.Since(fetchStart)

		if fetchTimedOut {
			attempts = append(attempts, SourceAttempt{
				SourceID: src.ID(),
				Tier:     src.Tier(),
				Status:   AttemptStatusTimeout,
				Duration: duration,
				Error:    ErrSourceTimeout.Error(),
			})
			continue
		}

		if fetchErr != nil {
			attempts = append(attempts, SourceAttempt{
				SourceID: src.ID(),
				Tier:     src.Tier(),
				Status:   AttemptStatusError,
				Duration: duration,
				Error:    fetchErr.Error(),
			})
			continue
		}

		// Success! First-tier-wins.
		attempts = append(attempts, SourceAttempt{
			SourceID: src.ID(),
			Tier:     src.Tier(),
			Status:   AttemptStatusSuccess,
			Duration: duration,
		})

		final := ProviderReport{}
		if rep != nil {
			final = *rep
		}
		if final.Provider == "" {
			final.Provider = adapter.ID()
		}
		if final.Source == "" {
			final.Source = src.ID()
		}
		if final.Tier == 0 {
			final.Tier = src.Tier()
		}
		if final.Confidence == "" {
			final.Confidence = defaultConfidenceForTier(src.Tier())
		}
		if final.ObservedAt.IsZero() {
			final.ObservedAt = time.Now().UTC()
		}
		final.Attempts = attempts

		return &final, nil
	}

	// All sources in the ladder exhausted without success.
	reason, detail, retryable := outcomeFromAttempts(attempts)
	return nil, &ProviderError{
		Provider:  adapter.ID(),
		Reason:    reason,
		Source:    lastAttemptedSource(attempts),
		Detail:    detail,
		Retryable: retryable,
		Attempts:  attempts,
	}
}

// outcomeFromAttempts classifies an exhausted ladder into the one Reason the
// dipstick.v1 enum has for it, most informative first: a source that ran and
// failed tells us more than one that never ran, and one that never ran tells
// us more than one policy excluded before it could.
func outcomeFromAttempts(attempts []SourceAttempt) (Reason, string, bool) {
	var sawTimeout, sawUnavailable, sawAny bool
	for _, a := range attempts {
		sawAny = true
		switch a.Status {
		case AttemptStatusError:
			return ReasonUpstreamError, "all sources exhausted without success", true
		case AttemptStatusTimeout:
			sawTimeout = true
		case AttemptStatusUnavailable:
			sawUnavailable = true
		}
	}
	switch {
	case sawTimeout:
		return ReasonTimeout, "all sources timed out", true
	case sawUnavailable:
		// Prerequisites were not met and the source never ran. Whether that is
		// a missing binary or a missing credential is more than the ladder
		// knows here; Detect is what answers that.
		return ReasonNotInstalled, "no source reported itself available", false
	case sawAny:
		// Every rung was excluded by policy: nothing was eligible to run.
		return ReasonNotSupported, "every source was excluded by the source policy", false
	default:
		// The adapter declared no sources at all.
		return ReasonNotSupported, "no sources are implemented for this provider yet", false
	}
}

// lastAttemptedSource names the final rung the ladder actually reached, so a
// ProviderError points at where the walk gave up rather than at nothing.
func lastAttemptedSource(attempts []SourceAttempt) SourceID {
	for i := len(attempts) - 1; i >= 0; i-- {
		if attempts[i].Status != AttemptStatusSkipped {
			return attempts[i].SourceID
		}
	}
	return ""
}

// sortSourcesByTier returns a copy of the sources sorted in ascending order by Tier(), filtering out nil elements.
func sortSourcesByTier(sources []Source) []Source {
	if len(sources) == 0 {
		return sources
	}
	var nonNil []Source
	for _, s := range sources {
		if s != nil {
			nonNil = append(nonNil, s)
		}
	}
	if len(nonNil) <= 1 {
		return nonNil
	}
	sorted := make([]Source, len(nonNil))
	copy(sorted, nonNil)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Tier() < sorted[j].Tier()
	})
	return sorted
}

// defaultConfidenceForTier determines standard confidence level based on source tier.
func defaultConfidenceForTier(tier SourceTier) Confidence {
	switch tier {
	case TierAPI:
		return ConfidenceExact
	case TierLocalState, TierLocalRPC:
		return ConfidenceDerived
	case TierTranscripts:
		return ConfidenceDerived
	case TierCLIScrape:
		return ConfidenceDerived
	default:
		return ConfidenceUnknown
	}
}
