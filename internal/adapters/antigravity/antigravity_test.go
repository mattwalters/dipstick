package antigravity_test

import (
	"context"
	"testing"

	"github.com/mattwalters/dipstick"
	"github.com/mattwalters/dipstick/internal/adapters/antigravity"
)

func TestAdapter(t *testing.T) {
	a := antigravity.New()
	if a == nil {
		t.Fatalf("expected non-nil adapter")
	}
	if a.Name() != "antigravity" {
		t.Errorf("expected name 'antigravity', got %q", a.Name())
	}
}

func TestAdapter_CollectResolvesNotSupported(t *testing.T) {
	ctx := context.Background()
	report, err := dipstick.Collect(ctx, dipstick.WithProviders(dipstick.ProviderAntigravity))
	if err != nil {
		t.Fatalf("unexpected error from Collect: %v", err)
	}
	if report == nil {
		t.Fatal("expected non-nil report")
	}
	if len(report.Providers) != 0 {
		t.Errorf("expected 0 provider reports, got %d", len(report.Providers))
	}
	if len(report.Errors) != 1 {
		t.Fatalf("expected 1 error for antigravity, got %d", len(report.Errors))
	}
	pe := report.Errors[0]
	if pe.Provider != dipstick.ProviderAntigravity {
		t.Errorf("expected provider %q, got %q", dipstick.ProviderAntigravity, pe.Provider)
	}
	if pe.Reason != dipstick.ReasonNotSupported {
		t.Errorf("expected reason %q, got %q", dipstick.ReasonNotSupported, pe.Reason)
	}
}
