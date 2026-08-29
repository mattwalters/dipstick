package main_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestCLI_BuildAndRun(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "run", ".", "--version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run failed: %v, output: %s", err, string(out))
	}

	if !strings.Contains(string(out), "dipstick") {
		t.Errorf("expected version output to contain 'dipstick', got %q", string(out))
	}
}

func TestCLI_JSONOutput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "run", ".", "-p", "claude")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run failed: %v, output: %s", err, string(out))
	}

	if !strings.Contains(string(out), `"claude"`) {
		t.Errorf("expected json output to contain 'claude', got %q", string(out))
	}
}

func TestCLI_NegativeTimeoutError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "run", ".", "-timeout", "-5s")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected command failure for negative timeout, got success. Output: %s", string(out))
	}

	if !strings.Contains(string(out), "invalid timeout") {
		t.Errorf("expected output to contain 'invalid timeout', got %q", string(out))
	}
}
