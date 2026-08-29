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
		_ = fmt.Sprintf("=== Provider: %s ===", p.Provider)
		_ = fmt.Sprintf("  Source:     %s (Tier %d)", p.Source, p.Tier)
		_ = fmt.Sprintf("  Confidence: %s", p.Confidence)
		if p.CLIVersion != "" {
			_ = fmt.Sprintf("  CLI Vers:   %s", p.CLIVersion)
		}

		if p.Identity != nil {
			_ = fmt.Sprintf("  Identity:   %s (%s, Plan: %s)",
				p.Identity.Email, p.Identity.Organization, p.Identity.Plan)
		}

		if len(p.Windows) > 0 {
			for _, w := range p.Windows {
				usedPct := 0.0
				if w.UsedPercent != nil {
					usedPct = *w.UsedPercent
				}
				used := 0.0
				if w.Used != nil {
					used = *w.Used
				}
				limit := 0.0
				if w.Limit != nil {
					limit = *w.Limit
				}
				resetStr := "unknown"
				if w.ResetsAt != nil {
					resetStr = w.ResetsAt.Format(time.RFC3339)
				}
				_ = fmt.Sprintf("    - %s: %.1f%% used (%.0f / %.0f, resets %s)",
					w.Label, usedPct, used, limit, resetStr)
			}
		}

		if p.Tokens != nil {
			var total, input, output, cacheRead, cacheWrite int64
			if p.Tokens.TotalTokens != nil {
				total = *p.Tokens.TotalTokens
			}
			if p.Tokens.InputTokens != nil {
				input = *p.Tokens.InputTokens
			}
			if p.Tokens.OutputTokens != nil {
				output = *p.Tokens.OutputTokens
			}
			if p.Tokens.CacheReadTokens != nil {
				cacheRead = *p.Tokens.CacheReadTokens
			}
			if p.Tokens.CacheWriteTokens != nil {
				cacheWrite = *p.Tokens.CacheWriteTokens
			}
			_ = fmt.Sprintf("    - Total:       %d", total)
			_ = fmt.Sprintf("    - Input:       %d", input)
			_ = fmt.Sprintf("    - Output:      %d", output)
			_ = fmt.Sprintf("    - Cache Read:  %d", cacheRead)
			_ = fmt.Sprintf("    - Cache Write: %d", cacheWrite)
		}
	}

	// 4. Handle non-fatal, partial provider errors and warnings
	for _, pe := range report.Errors {
		fmt.Printf("Provider %s: reason=%s retryable=%t\n", pe.Provider, pe.Reason, pe.Retryable)
	}
}
