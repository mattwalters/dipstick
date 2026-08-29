package dipstick_test

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/mattwalters/dipstick"
)

// ExampleCollect demonstrates the primary Go library usage pattern described in README.md.
func ExampleCollect() {
	// 1. Create a context with an execution timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 2. Collect usage metrics from specified providers
	report, err := dipstick.Collect(
		ctx,
		dipstick.WithProviders(
			dipstick.ProviderClaude,
			dipstick.ProviderCodex,
			dipstick.ProviderOpenCode,
		),
		dipstick.WithTimeout(8*time.Second),
		dipstick.WithSourcePolicy(dipstick.SourcePolicyDefault),
	)
	if err != nil {
		// Whole-run execution failures: context cancellations, invalid configuration
		log.Fatalf("Fatal collection error: %v", err)
	}

	_ = report.SchemaVersion
	_ = report.GeneratedAt

	// 3. Inspect successful provider reports
	for _, p := range report.Providers {
		_ = p.Provider
		_ = p.Source
		_ = p.Tier
		_ = p.Confidence
		if p.Identity != nil {
			_ = p.Identity.Email
		}
		for _, w := range p.Windows {
			_ = w.Label
			_ = w.UsedPercent
		}
		if p.Tokens != nil {
			_ = p.Tokens.TotalTokens
		}
	}

	// 4. Handle non-fatal, partial provider errors and warnings
	for _, pe := range report.Errors {
		fmt.Printf("Provider %s: reason=%s retryable=%t\n", pe.Provider, pe.Reason, pe.Retryable)
	}
}
