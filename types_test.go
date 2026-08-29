package dipstick_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v5"

	"github.com/mattwalters/dipstick"
)

func fullReportFixture() dipstick.Report {
	genTime, _ := time.Parse(time.RFC3339, "2026-08-29T12:00:00Z")
	obsTimeClaude, _ := time.Parse(time.RFC3339, "2026-08-29T12:00:00Z")
	obsTimeCodex, _ := time.Parse(time.RFC3339, "2026-08-29T11:59:30Z")
	resetTimeSession, _ := time.Parse(time.RFC3339, "2026-08-29T17:00:00Z")
	resetTimeWeekly, _ := time.Parse(time.RFC3339, "2026-09-01T00:00:00Z")
	resetTimeCodex, _ := time.Parse(time.RFC3339, "2026-08-29T13:00:00Z")

	return dipstick.Report{
		SchemaVersion: dipstick.SchemaVersion,
		GeneratedAt:   genTime,
		Providers: []dipstick.ProviderReport{
			{
				Provider:   dipstick.ProviderClaude,
				Source:     dipstick.SourceOAuthAPI,
				Confidence: dipstick.ConfidenceExact,
				CLIVersion: "2.1.246",
				Identity: &dipstick.Identity{
					Email:        "user@example.com",
					Organization: "Acme Corp",
					AccountID:    "acc_123456",
					Plan:         "Pro",
				},
				Windows: []dipstick.RateWindow{
					{
						Label:                 "session",
						UsedPercent:           dipstick.Ptr(15.5),
						Limit:                 dipstick.Ptr(100.0),
						Used:                  dipstick.Ptr(15.5),
						ResetsAt:              &resetTimeSession,
						WindowDurationSeconds: dipstick.Ptr[int64](18000),
					},
					{
						Label:                 "weekly",
						UsedPercent:           dipstick.Ptr(42.0),
						Limit:                 dipstick.Ptr(1000.0),
						Used:                  dipstick.Ptr(420.0),
						ResetsAt:              &resetTimeWeekly,
						WindowDurationSeconds: dipstick.Ptr[int64](604800),
					},
				},
				Tokens: &dipstick.TokenUsage{
					InputTokens:      dipstick.Ptr[int64](125000),
					OutputTokens:     dipstick.Ptr[int64](34000),
					CacheReadTokens:  dipstick.Ptr[int64](80000),
					CacheWriteTokens: dipstick.Ptr[int64](15000),
					TotalTokens:      dipstick.Ptr[int64](159000),
				},
				ObservedAt: obsTimeClaude,
			},
			{
				Provider:   dipstick.ProviderCodex,
				Source:     dipstick.SourceLocalState,
				Confidence: dipstick.ConfidenceDerived,
				CLIVersion: "0.148.0",
				Identity: &dipstick.Identity{
					Email:     "dev@example.com",
					AccountID: "user_7890",
				},
				Windows: []dipstick.RateWindow{
					{
						Label:       "primary",
						UsedPercent: dipstick.Ptr(0.0),
						Limit:       dipstick.Ptr(500.0),
						Used:        dipstick.Ptr(0.0),
						ResetsAt:    &resetTimeCodex,
					},
				},
				ObservedAt: obsTimeCodex,
			},
		},
		Errors: []dipstick.ProviderError{
			{
				Provider:  dipstick.ProviderAntigravity,
				Reason:    dipstick.ReasonNotSupported,
				Source:    dipstick.SourceCLIStdout,
				Detail:    "antigravity exposes no usage or quota surface",
				Retryable: false,
			},
		},
	}
}

func emptyReportFixture() dipstick.Report {
	genTime, _ := time.Parse(time.RFC3339, "2026-08-29T12:00:00Z")
	return dipstick.Report{
		SchemaVersion: dipstick.SchemaVersion,
		GeneratedAt:   genTime,
		Providers:     []dipstick.ProviderReport{},
	}
}

func TestReport_Golden(t *testing.T) {
	tests := []struct {
		name       string
		report     dipstick.Report
		goldenFile string
	}{
		{
			name:       "empty report matches report_empty.golden.json",
			report:     emptyReportFixture(),
			goldenFile: filepath.Join("testdata", "report_empty.golden.json"),
		},
		{
			name:       "full report matches report_full.golden.json",
			report:     fullReportFixture(),
			goldenFile: filepath.Join("testdata", "report_full.golden.json"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotBytes, err := json.MarshalIndent(tt.report, "", "  ")
			if err != nil {
				t.Fatalf("json.MarshalIndent failed: %v", err)
			}
			gotBytes = append(gotBytes, '\n')

			wantBytes, err := os.ReadFile(tt.goldenFile)
			if err != nil {
				t.Fatalf("failed reading golden file %s: %v", tt.goldenFile, err)
			}

			if !bytes.Equal(gotBytes, wantBytes) {
				t.Errorf("marshaled json mismatch with %s\nGot:\n%s\nWant:\n%s", tt.goldenFile, string(gotBytes), string(wantBytes))
			}
		})
	}
}

func TestReport_RoundTrip(t *testing.T) {
	files := []string{
		filepath.Join("testdata", "report_empty.golden.json"),
		filepath.Join("testdata", "report_full.golden.json"),
	}

	for _, file := range files {
		t.Run(file, func(t *testing.T) {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("failed reading %s: %v", file, err)
			}

			var rep dipstick.Report
			if err := json.Unmarshal(data, &rep); err != nil {
				t.Fatalf("unmarshal %s failed: %v", file, err)
			}

			remarshaled, err := json.MarshalIndent(rep, "", "  ")
			if err != nil {
				t.Fatalf("marshal %s failed: %v", file, err)
			}
			remarshaled = append(remarshaled, '\n')

			if !bytes.Equal(data, remarshaled) {
				t.Errorf("round trip mismatch for %s\nGot:\n%s\nWant:\n%s", file, string(remarshaled), string(data))
			}
		})
	}
}

func TestReport_PointerNumerics(t *testing.T) {
	t.Run("nil numeric pointers are omitted from JSON", func(t *testing.T) {
		rw := dipstick.RateWindow{
			Label: "session",
		}
		data, err := json.Marshal(rw)
		if err != nil {
			t.Fatalf("marshal failed: %v", err)
		}
		expected := `{"label":"session"}`
		if string(data) != expected {
			t.Errorf("got %s, want %s", string(data), expected)
		}
	})

	t.Run("zero-valued numeric pointers are explicitly preserved", func(t *testing.T) {
		rw := dipstick.RateWindow{
			Label:       "session",
			UsedPercent: dipstick.Ptr(0.0),
			Used:        dipstick.Ptr(0.0),
			Limit:       dipstick.Ptr(100.0),
		}
		data, err := json.Marshal(rw)
		if err != nil {
			t.Fatalf("marshal failed: %v", err)
		}

		var parsed dipstick.RateWindow
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("unmarshal failed: %v", err)
		}

		if parsed.UsedPercent == nil || *parsed.UsedPercent != 0.0 {
			t.Errorf("UsedPercent: got %v, want pointer to 0.0", parsed.UsedPercent)
		}
		if parsed.Used == nil || *parsed.Used != 0.0 {
			t.Errorf("Used: got %v, want pointer to 0.0", parsed.Used)
		}
		if parsed.Limit == nil || *parsed.Limit != 100.0 {
			t.Errorf("Limit: got %v, want pointer to 100.0", parsed.Limit)
		}
		if parsed.WindowDurationSeconds != nil {
			t.Errorf("WindowDurationSeconds: got %v, want nil", parsed.WindowDurationSeconds)
		}
	})

	t.Run("token usage distinguishes nil from 0", func(t *testing.T) {
		tokens := dipstick.TokenUsage{
			InputTokens: dipstick.Ptr[int64](0),
			TotalTokens: dipstick.Ptr[int64](0),
		}
		data, err := json.Marshal(tokens)
		if err != nil {
			t.Fatalf("marshal failed: %v", err)
		}

		var parsed dipstick.TokenUsage
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("unmarshal failed: %v", err)
		}

		if parsed.InputTokens == nil || *parsed.InputTokens != 0 {
			t.Errorf("InputTokens: got %v, want pointer to 0", parsed.InputTokens)
		}
		if parsed.OutputTokens != nil {
			t.Errorf("OutputTokens: got %v, want nil", parsed.OutputTokens)
		}
		if parsed.CacheReadTokens != nil {
			t.Errorf("CacheReadTokens: got %v, want nil", parsed.CacheReadTokens)
		}
		if parsed.TotalTokens == nil || *parsed.TotalTokens != 0 {
			t.Errorf("TotalTokens: got %v, want pointer to 0", parsed.TotalTokens)
		}
	})
}

func TestReport_SchemaValidation(t *testing.T) {
	schemaPath := filepath.Join("schema", "dipstick.v1.json")
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft2020
	schema, err := compiler.Compile(schemaPath)
	if err != nil {
		t.Fatalf("failed compiling schema %s: %v", schemaPath, err)
	}

	t.Run("golden files validate successfully", func(t *testing.T) {
		goldenFiles := []string{
			filepath.Join("testdata", "report_empty.golden.json"),
			filepath.Join("testdata", "report_full.golden.json"),
		}

		for _, gf := range goldenFiles {
			var v any
			data, err := os.ReadFile(gf)
			if err != nil {
				t.Fatalf("reading %s: %v", gf, err)
			}
			if err := json.Unmarshal(data, &v); err != nil {
				t.Fatalf("parsing json %s: %v", gf, err)
			}
			if err := schema.Validate(v); err != nil {
				t.Errorf("schema validation failed for %s: %v", gf, err)
			}
		}
	})

	t.Run("schema rejects invalid payloads", func(t *testing.T) {
		invalidCases := []struct {
			name string
			json string
		}{
			{
				name: "missing schema_version",
				json: `{"generated_at": "2026-08-29T12:00:00Z", "providers": []}`,
			},
			{
				name: "wrong schema_version",
				json: `{"schema_version": "dipstick.v2", "generated_at": "2026-08-29T12:00:00Z", "providers": []}`,
			},
			{
				name: "missing generated_at",
				json: `{"schema_version": "dipstick.v1", "providers": []}`,
			},
			{
				name: "missing providers",
				json: `{"schema_version": "dipstick.v1", "generated_at": "2026-08-29T12:00:00Z"}`,
			},
			{
				name: "invalid provider id",
				json: `{
					"schema_version": "dipstick.v1",
					"generated_at": "2026-08-29T12:00:00Z",
					"providers": [
						{
							"provider": "unknown_ai",
							"source": "oauth_api",
							"confidence": "exact",
							"observed_at": "2026-08-29T12:00:00Z"
						}
					]
				}`,
			},
			{
				name: "invalid source id",
				json: `{
					"schema_version": "dipstick.v1",
					"generated_at": "2026-08-29T12:00:00Z",
					"providers": [
						{
							"provider": "claude",
							"source": "magic_file",
							"confidence": "exact",
							"observed_at": "2026-08-29T12:00:00Z"
						}
					]
				}`,
			},
			{
				name: "invalid confidence",
				json: `{
					"schema_version": "dipstick.v1",
					"generated_at": "2026-08-29T12:00:00Z",
					"providers": [
						{
							"provider": "claude",
							"source": "oauth_api",
							"confidence": "super_exact",
							"observed_at": "2026-08-29T12:00:00Z"
						}
					]
				}`,
			},
			{
				name: "missing observed_at on provider report",
				json: `{
					"schema_version": "dipstick.v1",
					"generated_at": "2026-08-29T12:00:00Z",
					"providers": [
						{
							"provider": "claude",
							"source": "oauth_api",
							"confidence": "exact"
						}
					]
				}`,
			},
			{
				name: "missing label on rate window",
				json: `{
					"schema_version": "dipstick.v1",
					"generated_at": "2026-08-29T12:00:00Z",
					"providers": [
						{
							"provider": "claude",
							"source": "oauth_api",
							"confidence": "exact",
							"observed_at": "2026-08-29T12:00:00Z",
							"windows": [
								{
									"used_percent": 50
								}
							]
						}
					]
				}`,
			},
			{
				name: "invalid error reason",
				json: `{
					"schema_version": "dipstick.v1",
					"generated_at": "2026-08-29T12:00:00Z",
					"providers": [],
					"errors": [
						{
							"provider": "claude",
							"reason": "exploded",
							"detail": "boom",
							"retryable": false
						}
					]
				}`,
			},
			{
				name: "additional unknown root property",
				json: `{
					"schema_version": "dipstick.v1",
					"generated_at": "2026-08-29T12:00:00Z",
					"providers": [],
					"unknown_field": 123
				}`,
			},
		}

		for _, ic := range invalidCases {
			t.Run(ic.name, func(t *testing.T) {
				var v any
				if err := json.Unmarshal([]byte(ic.json), &v); err != nil {
					t.Fatalf("malformed test json: %v", err)
				}
				if err := schema.Validate(v); err == nil {
					t.Errorf("expected schema validation failure for %s, got success", ic.name)
				}
			})
		}
	})
}

func TestEnums(t *testing.T) {
	// Verify all enum constants match expected string values
	providers := []dipstick.ProviderID{
		dipstick.ProviderClaude,
		dipstick.ProviderCodex,
		dipstick.ProviderOpenCode,
		dipstick.ProviderAntigravity,
	}
	expectedProviders := map[dipstick.ProviderID]string{
		dipstick.ProviderClaude:      "claude",
		dipstick.ProviderCodex:       "codex",
		dipstick.ProviderOpenCode:    "opencode",
		dipstick.ProviderAntigravity: "antigravity",
	}
	for _, p := range providers {
		if string(p) != expectedProviders[p] {
			t.Errorf("Provider %v: got %q, want %q", p, string(p), expectedProviders[p])
		}
	}

	sources := []dipstick.SourceID{
		dipstick.SourceOAuthAPI,
		dipstick.SourceLocalState,
		dipstick.SourceAppServer,
		dipstick.SourceTranscript,
		dipstick.SourceCLIStdout,
	}
	expectedSources := map[dipstick.SourceID]string{
		dipstick.SourceOAuthAPI:   "oauth_api",
		dipstick.SourceLocalState: "local_state",
		dipstick.SourceAppServer:  "app_server",
		dipstick.SourceTranscript: "transcript",
		dipstick.SourceCLIStdout:  "cli_stdout",
	}
	for _, s := range sources {
		if string(s) != expectedSources[s] {
			t.Errorf("Source %v: got %q, want %q", s, string(s), expectedSources[s])
		}
	}

	confidences := []dipstick.Confidence{
		dipstick.ConfidenceExact,
		dipstick.ConfidenceDerived,
		dipstick.ConfidenceStale,
		dipstick.ConfidenceUnknown,
	}
	expectedConfidences := map[dipstick.Confidence]string{
		dipstick.ConfidenceExact:   "exact",
		dipstick.ConfidenceDerived: "derived",
		dipstick.ConfidenceStale:   "stale",
		dipstick.ConfidenceUnknown: "unknown",
	}
	for _, c := range confidences {
		if string(c) != expectedConfidences[c] {
			t.Errorf("Confidence %v: got %q, want %q", c, string(c), expectedConfidences[c])
		}
	}

	reasons := []dipstick.Reason{
		dipstick.ReasonNotInstalled,
		dipstick.ReasonNotAuthenticated,
		dipstick.ReasonCredentialExpired,
		dipstick.ReasonUnsupportedVersion,
		dipstick.ReasonParseFailed,
		dipstick.ReasonUpstreamError,
		dipstick.ReasonTimeout,
		dipstick.ReasonNotSupported,
	}
	expectedReasons := map[dipstick.Reason]string{
		dipstick.ReasonNotInstalled:       "not_installed",
		dipstick.ReasonNotAuthenticated:   "not_authenticated",
		dipstick.ReasonCredentialExpired:  "credential_expired",
		dipstick.ReasonUnsupportedVersion: "unsupported_version",
		dipstick.ReasonParseFailed:        "parse_failed",
		dipstick.ReasonUpstreamError:      "upstream_error",
		dipstick.ReasonTimeout:            "timeout",
		dipstick.ReasonNotSupported:       "not_supported",
	}
	for _, r := range reasons {
		if string(r) != expectedReasons[r] {
			t.Errorf("Reason %v: got %q, want %q", r, string(r), expectedReasons[r])
		}
	}
}

func TestProviderError_Error(t *testing.T) {
	t.Run("with source", func(t *testing.T) {
		pe := dipstick.ProviderError{
			Provider:  dipstick.ProviderClaude,
			Reason:    dipstick.ReasonParseFailed,
			Source:    dipstick.SourceOAuthAPI,
			Detail:    "unexpected response payload",
			Retryable: false,
		}
		got := pe.Error()
		want := "claude (oauth_api): parse_failed: unexpected response payload"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("without source", func(t *testing.T) {
		pe := dipstick.ProviderError{
			Provider:  dipstick.ProviderCodex,
			Reason:    dipstick.ReasonNotInstalled,
			Detail:    "executable not found in PATH",
			Retryable: false,
		}
		got := pe.Error()
		want := "codex: not_installed: executable not found in PATH"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}

func TestRateWindow_Duration(t *testing.T) {
	t.Run("nil duration", func(t *testing.T) {
		rw := dipstick.RateWindow{Label: "session"}
		if d := rw.Duration(); d != 0 {
			t.Errorf("got %v, want 0", d)
		}
	})

	t.Run("set duration", func(t *testing.T) {
		rw := dipstick.RateWindow{
			Label:                 "session",
			WindowDurationSeconds: dipstick.Ptr[int64](3600),
		}
		if d := rw.Duration(); d != 1*time.Hour {
			t.Errorf("got %v, want %v", d, 1*time.Hour)
		}
	})
}
