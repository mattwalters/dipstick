package main

import (
	"context"
	"path/filepath"
	"testing"
)

func TestNormalizeVersion(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"codex-cli 0.148.0", "0.148.0"},
		{"claude 2.1.246", "2.1.246"},
		{"v2.1.0", "2.1.0"},
		{"1.0.0", "1.0.0"},
		{"", "0.1.0"},
		{"   ", "0.1.0"},
		{"  v0.5.2  ", "0.5.2"},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := normalizeVersion(tc.input)
			if got != tc.expected {
				t.Errorf("normalizeVersion(%q) = %q, want %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestValidateFixtureDirectory(t *testing.T) {
	fixtureDir := filepath.Join("..", "..", "..", "testdata", "fixtures")
	findings, err := validateFixtureDirectory(fixtureDir)
	if err != nil {
		t.Fatalf("validateFixtureDirectory failed: %v", err)
	}
	if len(findings) > 0 {
		t.Errorf("unexpected secret findings in %s: %+v", fixtureDir, findings)
	}
}

func TestCaptureDryRun(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	captureOpenCode(ctx, tmpDir, true, false)
	captureAntigravity(ctx, tmpDir, true, false)

	_, _ = captureClaude(ctx, tmpDir, true, false)
	_, _ = captureCodex(ctx, tmpDir, true, false)
}
