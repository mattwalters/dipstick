package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mattwalters/dipstick/internal/compat"
	"github.com/mattwalters/dipstick/internal/types"
)

func TestCanary_MockDrift(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	report, err := Run(ctx, Config{MockDrift: true})
	if err != nil {
		t.Fatalf("Run with MockDrift failed: %v", err)
	}

	if !report.DriftDetected {
		t.Errorf("expected DriftDetected to be true in mock drift mode")
	}

	if len(report.Vendors) == 0 {
		t.Fatalf("expected vendor reports, got none")
	}

	// Verify markdown generation
	md := GenerateMarkdownReport(report)
	if !strings.Contains(md, "Vendor CLI Drift Canary Report") {
		t.Errorf("expected markdown report title, got: %s", md)
	}
	if !strings.Contains(md, "Drift Detected") {
		t.Errorf("expected drift warning in markdown, got: %s", md)
	}
	if !strings.Contains(md, "claude") || !strings.Contains(md, "codex") || !strings.Contains(md, "opencode") {
		t.Errorf("expected all providers in markdown table, got: %s", md)
	}

	// Verify JSON serialization
	jsonData, err := GenerateJSONReport(report)
	if err != nil {
		t.Fatalf("GenerateJSONReport failed: %v", err)
	}

	var parsedReport CanaryReport
	if err := json.Unmarshal(jsonData, &parsedReport); err != nil {
		t.Fatalf("unmarshaling JSON report failed: %v", err)
	}

	if !parsedReport.DriftDetected {
		t.Errorf("parsed JSON report should have DriftDetected = true")
	}
}

func TestCanary_CleanRun(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Only test antigravity which is clean by definition (unsupported)
	report, err := Run(ctx, Config{
		Providers: []types.ProviderID{types.ProviderAntigravity},
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if report.DriftDetected {
		t.Errorf("expected DriftDetected to be false for clean run")
	}

	md := GenerateMarkdownReport(report)
	if !strings.Contains(md, "All Probes In Range (Clean)") {
		t.Errorf("expected clean status in markdown, got: %s", md)
	}
}

func TestCanary_ProbeAntigravity(t *testing.T) {
	ctx := context.Background()
	vr := ProbeAntigravity(ctx, nil, false)

	if vr.Provider != types.ProviderAntigravity {
		t.Errorf("expected provider antigravity, got %s", vr.Provider)
	}
	if vr.DriftDetected {
		t.Errorf("expected DriftDetected = false for antigravity")
	}
	if vr.CompatStatus != compat.StatusInRange {
		t.Errorf("expected CompatStatus StatusInRange, got %s", vr.CompatStatus)
	}
}

func TestCanary_ValidateCodexSchemaDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Empty directory
	valid, desc, err := ValidateCodexSchemaDirectory(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error on empty dir: %v", err)
	}
	if valid {
		t.Errorf("expected empty dir to be invalid")
	}
	if !strings.Contains(desc, "no schema files") {
		t.Errorf("expected 'no schema files' message, got: %s", desc)
	}

	// 2. Valid schema files
	schemaJSON := `{
		"$schema": "http://json-schema.org/draft-07/schema#",
		"title": "CodexAppServerProtocol",
		"definitions": {
			"initialize": {
				"type": "object"
			},
			"account/rateLimits/read": {
				"type": "object",
				"properties": {
					"rateLimits": {
						"properties": {
							"primary": { "properties": { "usedPercent": { "type": "number" }, "resetsAt": { "type": "number" } } },
							"secondary": { "properties": { "usedPercent": { "type": "number" }, "resetsAt": { "type": "number" } } }
						}
					}
				}
			},
			"account/usage/read": {
				"type": "object",
				"properties": {
					"lifetimeTokens": { "type": "integer" }
				}
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(tmpDir, "protocol.json"), []byte(schemaJSON), 0o644); err != nil {
		t.Fatalf("writing test schema file: %v", err)
	}

	valid, desc, err = ValidateCodexSchemaDirectory(tmpDir)
	if err != nil {
		t.Fatalf("validating schema dir: %v", err)
	}
	if !valid {
		t.Errorf("expected valid schema directory, got invalid: %s", desc)
	}
	if !strings.Contains(desc, "verified") {
		t.Errorf("expected success description, got: %s", desc)
	}

	// 3. Schema missing required RPC method
	missingMethodDir := t.TempDir()
	incompleteJSON := `{
		"definitions": {
			"initialize": {},
			"account/usage/read": {},
			"usedPercent": {},
			"resetsAt": {}
		}
	}`
	if err := os.WriteFile(filepath.Join(missingMethodDir, "incomplete.json"), []byte(incompleteJSON), 0o644); err != nil {
		t.Fatalf("writing test schema file: %v", err)
	}

	valid, desc, err = ValidateCodexSchemaDirectory(missingMethodDir)
	if err != nil {
		t.Fatalf("validating incomplete schema dir: %v", err)
	}
	if valid {
		t.Errorf("expected incomplete schema dir to fail validation")
	}
	if !strings.Contains(desc, "account/rateLimits/read") {
		t.Errorf("expected description to mention missing account/rateLimits/read, got: %s", desc)
	}

	// 4. Schema missing rate limit fields
	missingFieldsDir := t.TempDir()
	missingFieldsJSON := `{
		"definitions": {
			"initialize": {},
			"account/rateLimits/read": {},
			"account/usage/read": {}
		}
	}`
	if err := os.WriteFile(filepath.Join(missingFieldsDir, "missing_fields.json"), []byte(missingFieldsJSON), 0o644); err != nil {
		t.Fatalf("writing test schema file: %v", err)
	}

	valid, desc, err = ValidateCodexSchemaDirectory(missingFieldsDir)
	if err != nil {
		t.Fatalf("validating schema dir: %v", err)
	}
	if valid {
		t.Errorf("expected missing fields schema dir to fail validation")
	}
	if !strings.Contains(desc, "usedPercent") {
		t.Errorf("expected description to mention missing usedPercent, got: %s", desc)
	}
}

func TestCanary_ReportFormattingAndSerialization(t *testing.T) {
	now := time.Date(2026, 8, 29, 22, 0, 0, 0, time.UTC)
	report := &CanaryReport{
		GeneratedAt:   now,
		DriftDetected: true,
		Summary:       "1 vendor drift detected",
		Vendors: []VendorReport{
			{
				Provider:          types.ProviderClaude,
				VendorName:        "Claude Code (Anthropic)",
				Installed:         true,
				BinaryPath:        "/usr/local/bin/claude",
				DiscoveredVersion: "2.3.0",
				VerifiedRange:     ">=2.1.0 <2.2.0",
				LastCheck:         "2026-08-29",
				CompatStatus:      compat.StatusNewerThanVerified,
				DriftDetected:     true,
				Probes: []ProbeResult{
					{
						Name:   "version",
						Status: ProbeStatusDrift,
						Passed: false,
						Detail: "installed version 2.3.0 is newer than verified range >=2.1.0 <2.2.0",
					},
					{
						Name:   "help",
						Status: ProbeStatusPassed,
						Passed: true,
						Detail: "help surface intact",
					},
				},
			},
			{
				Provider:      types.ProviderCodex,
				VendorName:    "OpenAI Codex",
				Installed:     false,
				VerifiedRange: ">=0.148.0 <0.150.0",
				LastCheck:     "2026-08-29",
				CompatStatus:  compat.StatusUnknown,
				DriftDetected: false,
				Probes: []ProbeResult{
					{
						Name:   "installation",
						Status: ProbeStatusSkipped,
						Passed: true,
						Detail: "codex not installed on host",
					},
				},
			},
		},
	}

	md := GenerateMarkdownReport(report)
	if !strings.Contains(md, "2.3.0") {
		t.Errorf("expected discovered version 2.3.0 in markdown, got: %s", md)
	}
	if !strings.Contains(md, "newer_than_verified") {
		t.Errorf("expected newer_than_verified status in markdown, got: %s", md)
	}
	if !strings.Contains(md, "Not Installed") {
		t.Errorf("expected Not Installed in markdown, got: %s", md)
	}
	if !strings.Contains(md, "make capture") {
		t.Errorf("expected remediation instructions in markdown, got: %s", md)
	}

	jsonBytes, err := GenerateJSONReport(report)
	if err != nil {
		t.Fatalf("GenerateJSONReport failed: %v", err)
	}

	var roundtrip CanaryReport
	if err := json.Unmarshal(jsonBytes, &roundtrip); err != nil {
		t.Fatalf("failed to unmarshal generated JSON: %v", err)
	}

	if roundtrip.Vendors[0].DiscoveredVersion != "2.3.0" {
		t.Errorf("expected roundtrip DiscoveredVersion = 2.3.0, got %s", roundtrip.Vendors[0].DiscoveredVersion)
	}
}
