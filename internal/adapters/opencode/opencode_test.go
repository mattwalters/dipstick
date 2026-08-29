package opencode_test

import (
	"testing"

	"github.com/mattwalters/dipstick/internal/adapters/opencode"
)

func TestAdapter(t *testing.T) {
	a := opencode.New()
	if a == nil {
		t.Fatalf("expected non-nil adapter")
	}
	if a.Name() != "opencode" {
		t.Errorf("expected name 'opencode', got %q", a.Name())
	}
}
