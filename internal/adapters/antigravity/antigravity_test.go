package antigravity_test

import (
	"testing"

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
