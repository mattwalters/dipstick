package claude

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mattwalters/dipstick/internal/localstate"
	"github.com/mattwalters/dipstick/internal/types"
)

type fixtureManifest struct {
	Provider      string   `json:"provider"`
	VendorVersion string   `json:"vendor_version"`
	CapturedAt    string   `json:"captured_at"`
	OS            string   `json:"os"`
	Arch          string   `json:"arch"`
	Sources       []string `json:"sources"`
}

func TestClaudeFixtures_ReplayGoldenContracts(t *testing.T) {
	fixturesRoot := filepath.Join("..", "..", "..", "testdata", "fixtures", "claude")
	entries, err := os.ReadDir(fixturesRoot)
	if err != nil {
		t.Fatalf("reading claude fixtures root: %v", err)
	}

	replayedCount := 0
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "malformed" {
			continue
		}

		versionDir := filepath.Join(fixturesRoot, entry.Name())
		manifestPath := filepath.Join(versionDir, "manifest.json")
		payloadPath := filepath.Join(versionDir, "oauth_api.json")
		goldenPath := filepath.Join(versionDir, "golden_report.json")

		if _, err := os.Stat(payloadPath); os.IsNotExist(err) {
			continue
		}

		t.Run("version_"+entry.Name(), func(t *testing.T) {
			manifestData, err := os.ReadFile(manifestPath)
			if err != nil {
				t.Fatalf("reading manifest: %v", err)
			}
			var manifest fixtureManifest
			if err := json.Unmarshal(manifestData, &manifest); err != nil {
				t.Fatalf("unmarshaling manifest: %v", err)
			}

			payloadBytes, err := os.ReadFile(payloadPath)
			if err != nil {
				t.Fatalf("reading payload fixture: %v", err)
			}

			goldenBytes, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("reading golden report: %v", err)
			}
			var expectedReport types.ProviderReport
			if err := json.Unmarshal(goldenBytes, &expectedReport); err != nil {
				t.Fatalf("unmarshaling golden report: %v", err)
			}

			// 1. Direct parser replay
			windows, err := parseOAuthUsageResponse(payloadBytes)
			if err != nil {
				t.Fatalf("parseOAuthUsageResponse failed on valid fixture: %v", err)
			}
			if len(windows) != len(expectedReport.Windows) {
				t.Fatalf("expected %d windows, got %d", len(expectedReport.Windows), len(windows))
			}
			for i, expWin := range expectedReport.Windows {
				gotWin := windows[i]
				if gotWin.Label != expWin.Label {
					t.Errorf("window[%d] label mismatch: expected %s, got %s", i, expWin.Label, gotWin.Label)
				}
				if expWin.UsedPercent != nil {
					if gotWin.UsedPercent == nil || *gotWin.UsedPercent != *expWin.UsedPercent {
						t.Errorf("window[%d] UsedPercent mismatch: expected %v, got %v", i, expWin.UsedPercent, gotWin.UsedPercent)
					}
				}
				if expWin.WindowDurationSeconds != nil {
					if gotWin.WindowDurationSeconds == nil || *gotWin.WindowDurationSeconds != *expWin.WindowDurationSeconds {
						t.Errorf("window[%d] duration mismatch: expected %v, got %v", i, expWin.WindowDurationSeconds, gotWin.WindowDurationSeconds)
					}
				}
			}

			// 2. Full HTTP adapter replay hermetically
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/oauth/usage" {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(payloadBytes)
			}))
			defer server.Close()

			nowTime := expectedReport.ObservedAt
			futureTime := nowTime.Add(24 * time.Hour)

			src := NewOAuthAPISource(
				WithBaseURL(server.URL),
				WithHTTPClient(server.Client()),
				WithNow(func() time.Time { return nowTime }),
				WithVersionProbe(func(ctx context.Context) (string, error) {
					return manifest.VendorVersion, nil
				}),
				WithCredentialResolver(func(ctx context.Context) (*localstate.ClaudeCredentials, error) {
					var idEmail, idAccount, idPlan string
					if expectedReport.Identity != nil {
						idEmail = expectedReport.Identity.Email
						idAccount = expectedReport.Identity.AccountID
						idPlan = expectedReport.Identity.Plan
					}
					return &localstate.ClaudeCredentials{
						AccessToken:  "sk-ant-test-token",
						AccountID:    idAccount,
						Email:        idEmail,
						Subscription: idPlan,
						ExpiresAt:    &futureTime,
					}, nil
				}),
			)

			rep, err := src.Fetch(context.Background())
			if err != nil {
				t.Fatalf("Fetch failed: %v", err)
			}

			if rep.Provider != types.ProviderClaude {
				t.Errorf("Provider: got %s, want %s", rep.Provider, types.ProviderClaude)
			}
			if rep.Source != types.SourceOAuthAPI {
				t.Errorf("Source: got %s, want %s", rep.Source, types.SourceOAuthAPI)
			}
			if rep.Confidence != types.ConfidenceExact {
				t.Errorf("Confidence: got %s, want %s", rep.Confidence, types.ConfidenceExact)
			}
			if rep.CLIVersion != manifest.VendorVersion {
				t.Errorf("CLIVersion: got %s, want %s", rep.CLIVersion, manifest.VendorVersion)
			}
			if len(rep.Windows) != len(expectedReport.Windows) {
				t.Errorf("Windows length: got %d, want %d", len(rep.Windows), len(expectedReport.Windows))
			}
		})
		replayedCount++
	}

	if replayedCount == 0 {
		t.Fatalf("no Claude golden fixtures were replayed from %s", fixturesRoot)
	}
}

func TestClaudeFixtures_MalformedErrorHandling(t *testing.T) {
	malformedDir := filepath.Join("..", "..", "..", "testdata", "fixtures", "claude", "malformed")
	entries, err := os.ReadDir(malformedDir)
	if err != nil {
		t.Fatalf("reading malformed dir: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		t.Run(entry.Name(), func(t *testing.T) {
			path := filepath.Join(malformedDir, entry.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading malformed fixture: %v", err)
			}

			// Assert direct parser failure with typed ErrParseFailed
			_, err = parseOAuthUsageResponse(data)
			if err == nil {
				t.Fatalf("expected parseOAuthUsageResponse to fail for malformed fixture %s", entry.Name())
			}
			if !errors.Is(err, types.ErrParseFailed) {
				t.Errorf("expected ErrParseFailed, got %v", err)
			}

			// Assert HTTP source fetch error handling without panics
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(data)
			}))
			defer server.Close()

			future := time.Now().Add(24 * time.Hour)
			src := NewOAuthAPISource(
				WithBaseURL(server.URL),
				WithHTTPClient(server.Client()),
				WithCredentialResolver(func(ctx context.Context) (*localstate.ClaudeCredentials, error) {
					return &localstate.ClaudeCredentials{
						AccessToken: "sk-ant-test-token",
						ExpiresAt:   &future,
					}, nil
				}),
			)

			_, fetchErr := src.Fetch(context.Background())
			if fetchErr == nil {
				t.Fatalf("expected Fetch to fail for malformed fixture %s", entry.Name())
			}
			if !errors.Is(fetchErr, types.ErrParseFailed) {
				t.Errorf("expected ErrParseFailed from Fetch, got %v", fetchErr)
			}
		})
	}
}

func FuzzParseOAuthUsageResponse(f *testing.F) {
	// Seed with valid and malformed fixtures
	seedFixtures := []string{
		filepath.Join("..", "..", "..", "testdata", "fixtures", "claude", "v2.1.246", "oauth_api.json"),
		filepath.Join("..", "..", "..", "testdata", "fixtures", "claude", "v2.1.0", "oauth_api.json"),
		filepath.Join("..", "..", "..", "testdata", "fixtures", "claude", "malformed", "truncated.json"),
		filepath.Join("..", "..", "..", "testdata", "fixtures", "claude", "malformed", "invalid_syntax.json"),
		filepath.Join("..", "..", "..", "testdata", "fixtures", "claude", "malformed", "empty_windows.json"),
		filepath.Join("..", "..", "..", "testdata", "fixtures", "claude", "malformed", "invalid_resets_at.json"),
	}

	for _, path := range seedFixtures {
		if data, err := os.ReadFile(path); err == nil {
			f.Add(data)
		}
	}

	f.Add([]byte(`{"five_hour": {"utilization": 50.0, "resets_at": "2026-08-29T18:00:00Z"}}`))
	f.Add([]byte(`{"windows": {"session": {"limit": 100, "used": 20}}}`))
	f.Add([]byte(``))

	f.Fuzz(func(t *testing.T, data []byte) {
		// Ensure parseOAuthUsageResponse never panics on arbitrary inputs
		_, _ = parseOAuthUsageResponse(data)
	})
}
