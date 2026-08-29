package dipstick_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/mattwalters/dipstick"
)

func TestProviders(t *testing.T) {
	providers := dipstick.Providers()
	if len(providers) == 0 {
		t.Fatalf("expected non-empty providers list")
	}

	expected := []dipstick.ProviderID{
		dipstick.ProviderAntigravity,
		dipstick.ProviderClaude,
		dipstick.ProviderCodex,
		dipstick.ProviderOpenCode,
	}

	if len(providers) != len(expected) {
		t.Fatalf("expected %d providers, got %d", len(expected), len(providers))
	}

	for i, id := range expected {
		if providers[i] != id {
			t.Errorf("at index %d: expected %s, got %s", i, id, providers[i])
		}
	}
}

func TestCollect_Default(t *testing.T) {
	ctx := context.Background()
	report, err := dipstick.Collect(ctx)
	if err != nil {
		t.Fatalf("unexpected error from Collect: %v", err)
	}

	if report == nil {
		t.Fatal("expected non-nil report")
		return
	}

	if report.CollectedAt.IsZero() {
		t.Errorf("expected non-zero CollectedAt")
	}

	all := dipstick.Providers()
	for _, id := range all {
		pr, ok := report.Providers[id]
		if !ok {
			t.Errorf("missing provider %s in report", id)
			continue
		}
		if pr.ProviderID != id {
			t.Errorf("expected ProviderID %s, got %s", id, pr.ProviderID)
		}
		if pr.Err != nil {
			t.Errorf("unexpected error for provider %s: %v", id, pr.Err)
		}
	}
}

func TestCollect_NilContextAndOptions(t *testing.T) {
	var ctx context.Context
	report, err := dipstick.Collect(ctx, nil)
	if err != nil {
		t.Fatalf("unexpected error from Collect with nil context and options: %v", err)
	}
	if report == nil {
		t.Fatal("expected non-nil report")
		return
	}
}

func TestCollect_WithProviders(t *testing.T) {
	ctx := context.Background()
	report, err := dipstick.Collect(ctx, dipstick.WithProviders(dipstick.ProviderClaude, dipstick.ProviderAntigravity, dipstick.ProviderClaude))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report == nil {
		t.Fatal("expected non-nil report")
		return
	}

	if len(report.Providers) != 2 {
		t.Fatalf("expected 2 unique providers, got %d", len(report.Providers))
	}

	if _, ok := report.Providers[dipstick.ProviderClaude]; !ok {
		t.Errorf("expected claude provider in report")
	}
	if _, ok := report.Providers[dipstick.ProviderAntigravity]; !ok {
		t.Errorf("expected antigravity provider in report")
	}
	if _, ok := report.Providers[dipstick.ProviderCodex]; ok {
		t.Errorf("did not expect codex provider in report")
	}
}

func TestCollect_UnknownProvider(t *testing.T) {
	ctx := context.Background()
	_, err := dipstick.Collect(ctx, dipstick.WithProviders(dipstick.ProviderID("unknown-provider")))
	if err == nil {
		t.Fatalf("expected error for unknown provider, got nil")
	}
}

func TestCollect_NegativeTimeout(t *testing.T) {
	ctx := context.Background()
	_, err := dipstick.Collect(ctx, dipstick.WithTimeout(-1*time.Second))
	if err == nil {
		t.Fatalf("expected error for negative timeout, got nil")
	}
}

func TestCollect_WithTimeout(t *testing.T) {
	ctx := context.Background()
	report, err := dipstick.Collect(ctx, dipstick.WithTimeout(5*time.Second))
	if err != nil {
		t.Fatalf("unexpected error with timeout: %v", err)
	}
	if report == nil {
		t.Fatal("expected non-nil report")
		return
	}
}

func TestCollect_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := dipstick.Collect(ctx)
	if err == nil {
		t.Fatalf("expected context cancellation error, got nil")
	}
}

func TestCollect_WithSourcePolicy(t *testing.T) {
	ctx := context.Background()
	report, err := dipstick.Collect(ctx, dipstick.WithSourcePolicy(dipstick.SourcePolicyLocal))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report == nil {
		t.Fatal("expected non-nil report")
		return
	}
}

func TestProviderReport_JSON(t *testing.T) {
	pr := dipstick.ProviderReport{
		ProviderID: dipstick.ProviderClaude,
		Usage: dipstick.Usage{
			Sessions:     3,
			InputTokens:  1000,
			OutputTokens: 200,
			TotalTokens:  1200,
		},
		Err: errors.New("rate limited"),
	}

	data, err := json.Marshal(pr)
	if err != nil {
		t.Fatalf("failed to marshal ProviderReport: %v", err)
	}

	var unmarshaled dipstick.ProviderReport
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("failed to unmarshal ProviderReport: %v", err)
	}

	if unmarshaled.ProviderID != pr.ProviderID {
		t.Errorf("expected provider ID %s, got %s", pr.ProviderID, unmarshaled.ProviderID)
	}
	if unmarshaled.Usage.TotalTokens != 1200 {
		t.Errorf("expected 1200 total tokens, got %d", unmarshaled.Usage.TotalTokens)
	}
	if unmarshaled.Err == nil || unmarshaled.Err.Error() != "rate limited" {
		t.Errorf("expected error 'rate limited', got %v", unmarshaled.Err)
	}

	// Test without error
	prNoError := dipstick.ProviderReport{
		ProviderID: dipstick.ProviderAntigravity,
	}
	dataNoError, err := json.Marshal(prNoError)
	if err != nil {
		t.Fatalf("failed to marshal ProviderReport: %v", err)
	}
	var unmarshaledNoError dipstick.ProviderReport
	if err := json.Unmarshal(dataNoError, &unmarshaledNoError); err != nil {
		t.Fatalf("failed to unmarshal ProviderReport: %v", err)
	}
	if unmarshaledNoError.Err != nil {
		t.Errorf("expected nil error, got %v", unmarshaledNoError.Err)
	}
}
