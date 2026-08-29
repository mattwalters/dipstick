package codex_test

import (
	"testing"

	"github.com/mattwalters/dipstick/internal/adapters/codex"
)

func TestAdapter(t *testing.T) {
	a := codex.New()
	if a == nil {
		t.Fatalf("expected non-nil adapter")
	}
	if a.Name() != "codex" {
		t.Errorf("expected name 'codex', got %q", a.Name())
	}
}
