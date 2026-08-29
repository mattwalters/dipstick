package dipstick_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v5"

	"github.com/mattwalters/dipstick"
	"github.com/mattwalters/dipstick/internal/adapters/claude"
	"github.com/mattwalters/dipstick/internal/adapters/codex"
	"github.com/mattwalters/dipstick/internal/adapters/opencode"
)

// findError returns the ProviderError recorded for id, if any.
func findError(report *dipstick.Report, id dipstick.ProviderID) (dipstick.ProviderError, bool) {
	for _, pe := range report.Errors {
		if pe.Provider == id {
			return pe, true
		}
	}
	return dipstick.ProviderError{}, false
}

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

	if report.SchemaVersion != dipstick.SchemaVersion {
		t.Errorf("expected schema version %q, got %q", dipstick.SchemaVersion, report.SchemaVersion)
	}

	if report.GeneratedAt.IsZero() {
		t.Errorf("expected non-zero GeneratedAt")
	}

	for _, id := range dipstick.Providers() {
		pe, inErr := findError(report, id)
		pr, inProv := findProvider(report, id)
		if !inErr && !inProv {
			t.Errorf("missing provider %s in both report errors and providers", id)
			continue
		}
		if id == dipstick.ProviderCodex {
			if inProv {
				if pr.Source != dipstick.SourceAppServer && pr.Source != dipstick.SourceLocalState {
					t.Errorf("codex provider: expected source %s or %s, got %s", dipstick.SourceAppServer, dipstick.SourceLocalState, pr.Source)
				}
			}
		} else if id == dipstick.ProviderClaude {
			if inProv {
				if pr.Source != dipstick.SourceOAuthAPI {
					t.Errorf("claude provider: expected source %s, got %s", dipstick.SourceOAuthAPI, pr.Source)
				}
			}
		} else if id == dipstick.ProviderOpenCode {
			if inProv {
				if pr.Confidence != dipstick.ConfidenceDerived {
					t.Errorf("opencode provider: expected confidence %s, got %s", dipstick.ConfidenceDerived, pr.Confidence)
				}
			}
		} else {
			if !inErr {
				t.Errorf("expected stub provider %s in report errors", id)
			} else if pe.Reason != dipstick.ReasonNotSupported {
				t.Errorf("provider %s: expected reason %s, got %s", id, dipstick.ReasonNotSupported, pe.Reason)
			}
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

	if len(report.Errors)+len(report.Providers) != 2 {
		t.Fatalf("expected 2 unique providers total, got %d errors and %d providers", len(report.Errors), len(report.Providers))
	}

	_, hasClaudeErr := findError(report, dipstick.ProviderClaude)
	_, hasClaudeProv := findProvider(report, dipstick.ProviderClaude)
	if !hasClaudeErr && !hasClaudeProv {
		t.Errorf("expected claude provider in report")
	}
	if _, ok := findError(report, dipstick.ProviderAntigravity); !ok {
		t.Errorf("expected antigravity provider in report")
	}
	if _, ok := findError(report, dipstick.ProviderCodex); ok {
		t.Errorf("did not expect codex provider in report")
	}
	if _, ok := findProvider(report, dipstick.ProviderCodex); ok {
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

func TestCollect_NegativeSourceTimeout(t *testing.T) {
	ctx := context.Background()
	_, err := dipstick.Collect(ctx, dipstick.WithSourceTimeout(-1*time.Second))
	if err == nil {
		t.Fatalf("expected error for negative source timeout, got nil")
	}
}

func TestCollect_WithTimeout(t *testing.T) {
	ctx := context.Background()
	report, err := dipstick.Collect(ctx, dipstick.WithTimeout(5*time.Second), dipstick.WithSourceTimeout(2*time.Second))
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

func TestCollect_WithStrict(t *testing.T) {
	ctx := context.Background()
	report, err := dipstick.Collect(ctx, dipstick.WithStrict(true))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report == nil {
		t.Fatal("expected non-nil report")
		return
	}
}

func TestDefaultTimeout(t *testing.T) {
	if dipstick.DefaultTimeout != 30*time.Second {
		t.Errorf("expected DefaultTimeout to be 30s, got %v", dipstick.DefaultTimeout)
	}
}

func TestCollect_SingleProviderCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := dipstick.Collect(ctx, dipstick.WithProviders(dipstick.ProviderClaude))
	if err == nil {
		t.Fatalf("expected context cancellation error for single provider, got nil")
	}
}

// TestCollect_SchemaValidation ties the two halves of this package together:
// types_test.go proves hand-built reports match dipstick.v1, but nothing
// otherwise checks that the report Collect actually emits does. That gap is
// what let the collection path and the schema drift apart in the first place.
func TestCollect_SchemaValidation(t *testing.T) {
	schemaPath := filepath.Join("schema", "dipstick.v1.json")
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft2020
	schema, err := compiler.Compile(schemaPath)
	if err != nil {
		t.Fatalf("failed compiling schema %s: %v", schemaPath, err)
	}

	report, err := dipstick.Collect(context.Background())
	if err != nil {
		t.Fatalf("unexpected error from Collect: %v", err)
	}

	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("failed marshalling report: %v", err)
	}

	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("failed parsing marshalled report: %v", err)
	}

	if err := schema.Validate(v); err != nil {
		t.Errorf("Collect output failed dipstick.v1 validation: %v\nreport: %s", err, data)
	}
}

func TestCollect_OpenCode(t *testing.T) {
	schemaPath := filepath.Join("schema", "dipstick.v1.json")
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft2020
	schema, err := compiler.Compile(schemaPath)
	if err != nil {
		t.Fatalf("failed compiling schema %s: %v", schemaPath, err)
	}

	ctx := context.Background()
	report, err := dipstick.Collect(ctx, dipstick.WithProviders(dipstick.ProviderOpenCode))
	if err != nil {
		t.Fatalf("unexpected error collecting OpenCode: %v", err)
	}

	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("failed marshalling report: %v", err)
	}

	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("failed unmarshalling report: %v", err)
	}

	if err := schema.Validate(v); err != nil {
		t.Errorf("OpenCode report failed schema validation: %v\nreport: %s", err, string(data))
	}

	if len(report.Providers) > 0 {
		pr := report.Providers[0]
		if pr.Provider != dipstick.ProviderOpenCode {
			t.Errorf("expected ProviderOpenCode, got %s", pr.Provider)
		}
		if pr.Confidence != dipstick.ConfidenceDerived {
			t.Errorf("expected ConfidenceDerived, got %s", pr.Confidence)
		}
		if pr.Tokens == nil {
			t.Errorf("expected non-nil Tokens")
		}
	}
}

func TestCollect_SchemaValidation_WithWarnings(t *testing.T) {
	schemaPath := filepath.Join("schema", "dipstick.v1.json")
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft2020
	schema, err := compiler.Compile(schemaPath)
	if err != nil {
		t.Fatalf("failed compiling schema %s: %v", schemaPath, err)
	}

	report := &dipstick.Report{
		SchemaVersion: dipstick.SchemaVersion,
		GeneratedAt:   time.Now().UTC(),
		Providers: []dipstick.ProviderReport{
			{
				Provider:   dipstick.ProviderClaude,
				Source:     dipstick.SourceOAuthAPI,
				Confidence: dipstick.ConfidenceUnknown,
				CLIVersion: "2.3.0",
				Warnings:   []string{"installed version 2.3.0 is newer than verified range >=2.1.0 <2.2.0"},
				ObservedAt: time.Now().UTC(),
			},
		},
		Errors: []dipstick.ProviderError{
			{
				Provider:  dipstick.ProviderCodex,
				Reason:    dipstick.ReasonUnsupportedVersion,
				Detail:    "strict mode: version 0.155.0 is newer than verified range >=0.148.0 <0.150.0",
				Retryable: false,
			},
		},
	}

	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("failed marshalling report: %v", err)
	}

	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("failed unmarshalling report: %v", err)
	}

	if err := schema.Validate(v); err != nil {
		t.Errorf("Report with warnings and confidence:unknown failed schema validation: %v\nreport: %s", err, string(data))
	}
}

func TestReadme_CompatibilityMatrix(t *testing.T) {
	readmePath := "README.md"
	data, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("failed reading %s: %v", readmePath, err)
	}

	content := string(data)
	claudeCompat := claude.New().Compat()
	codexCompat := codex.New().Compat()
	opencodeCompat := opencode.New().Compat()

	if !strings.Contains(content, claudeCompat.VerifiedRange) {
		t.Errorf("README.md missing Claude verified range %q", claudeCompat.VerifiedRange)
	}
	if !strings.Contains(content, codexCompat.VerifiedRange) {
		t.Errorf("README.md missing Codex verified range %q", codexCompat.VerifiedRange)
	}
	if !strings.Contains(content, opencodeCompat.VerifiedRange) {
		t.Errorf("README.md missing OpenCode verified range %q", opencodeCompat.VerifiedRange)
	}
}
