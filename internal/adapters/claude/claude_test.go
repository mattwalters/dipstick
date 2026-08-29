package claude_test

import (
	"testing"

	"github.com/mattwalters/dipstick/internal/adapters/claude"
)

func TestAdapter(t *testing.T) {
	a := claude.New()
	if a == nil {
		t.Fatalf("expected non-nil adapter")
	}
	if a.Name() != "claude" {
		t.Errorf("expected name 'claude', got %q", a.Name())
	}
}
