package codex

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

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

func TestCodexFixtures_ReplayGoldenContracts(t *testing.T) {
	fixturesRoot := filepath.Join("..", "..", "..", "testdata", "fixtures", "codex")
	entries, err := os.ReadDir(fixturesRoot)
	if err != nil {
		t.Fatalf("reading codex fixtures root: %v", err)
	}

	replayedCount := 0
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "malformed" {
			continue
		}

		versionDir := filepath.Join(fixturesRoot, entry.Name())
		manifestPath := filepath.Join(versionDir, "manifest.json")
		payloadPath := filepath.Join(versionDir, "local_state.json")
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
				t.Fatalf("reading local_state fixture: %v", err)
			}

			goldenBytes, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("reading golden report: %v", err)
			}
			var expectedReport types.ProviderReport
			if err := json.Unmarshal(goldenBytes, &expectedReport); err != nil {
				t.Fatalf("unmarshaling golden report: %v", err)
			}

			// Setup hermetic temporary environment for local state resolver
			tmpDir := t.TempDir()
			codexDir := filepath.Join(tmpDir, ".codex")
			if err := os.MkdirAll(codexDir, 0o700); err != nil {
				t.Fatalf("creating test .codex dir: %v", err)
			}
			if err := os.WriteFile(filepath.Join(codexDir, "auth.json"), payloadBytes, 0o600); err != nil {
				t.Fatalf("writing auth.json: %v", err)
			}

			resolver := localstate.New(
				localstate.WithHomeDir(tmpDir),
				localstate.WithEnvMap(map[string]string{}),
			)
			adapter := New(WithResolver(resolver))

			sources := adapter.Sources()
			if len(sources) == 0 {
				t.Fatalf("adapter returned no sources")
			}
			src := sources[0]

			if !src.Available(context.Background()) {
				t.Fatalf("expected source to be available")
			}

			rep, err := src.Fetch(context.Background())
			if err != nil {
				t.Fatalf("Fetch failed: %v", err)
			}

			if rep.Provider != types.ProviderCodex {
				t.Errorf("Provider: got %s, want %s", rep.Provider, types.ProviderCodex)
			}
			if rep.Source != types.SourceLocalState {
				t.Errorf("Source: got %s, want %s", rep.Source, types.SourceLocalState)
			}
			if rep.Confidence != types.ConfidenceDerived {
				t.Errorf("Confidence: got %s, want %s", rep.Confidence, types.ConfidenceDerived)
			}

			if expectedReport.Identity != nil {
				if rep.Identity == nil {
					t.Fatalf("expected non-nil Identity")
				}
				if rep.Identity.Plan != expectedReport.Identity.Plan {
					t.Errorf("Identity.Plan: got %s, want %s", rep.Identity.Plan, expectedReport.Identity.Plan)
				}
				if expectedReport.Identity.Email != "" && rep.Identity.Email != expectedReport.Identity.Email {
					t.Errorf("Identity.Email: got %s, want %s", rep.Identity.Email, expectedReport.Identity.Email)
				}
				if expectedReport.Identity.AccountID != "" && rep.Identity.AccountID != expectedReport.Identity.AccountID {
					t.Errorf("Identity.AccountID: got %s, want %s", rep.Identity.AccountID, expectedReport.Identity.AccountID)
				}
			}
		})
		replayedCount++
	}

	if replayedCount == 0 {
		t.Fatalf("no Codex golden fixtures were replayed from %s", fixturesRoot)
	}
}

func TestCodexFixtures_MalformedErrorHandling(t *testing.T) {
	malformedDir := filepath.Join("..", "..", "..", "testdata", "fixtures", "codex", "malformed")
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

			tmpDir := t.TempDir()
			codexDir := filepath.Join(tmpDir, ".codex")
			_ = os.MkdirAll(codexDir, 0o700)
			_ = os.WriteFile(filepath.Join(codexDir, "auth.json"), data, 0o600)

			resolver := localstate.New(
				localstate.WithHomeDir(tmpDir),
				localstate.WithEnvMap(map[string]string{}),
			)
			adapter := New(WithResolver(resolver))
			src := adapter.Sources()[0]

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

func FuzzDecodeCodexAuth(f *testing.F) {
	seedFiles := []string{
		filepath.Join("..", "..", "..", "testdata", "fixtures", "codex", "v0.1.0", "local_state.json"),
		filepath.Join("..", "..", "..", "testdata", "fixtures", "codex", "v0.1.0-apikey", "local_state.json"),
		filepath.Join("..", "..", "..", "testdata", "fixtures", "codex", "malformed", "corrupted.json"),
		filepath.Join("..", "..", "..", "testdata", "fixtures", "codex", "malformed", "malformed_jwt.json"),
		filepath.Join("..", "..", "..", "testdata", "fixtures", "codex", "malformed", "missing_tokens.json"),
	}

	for _, path := range seedFiles {
		if data, err := os.ReadFile(path); err == nil {
			f.Add(data)
		}
	}

	f.Add([]byte(`{"auth_mode":"api_key","OPENAI_API_KEY":"sk-mock"}`))
	f.Add([]byte(`{"tokens":{"id_token":"a.b.c"}}`))
	f.Add([]byte(``))

	f.Fuzz(func(t *testing.T, data []byte) {
		tmpDir := t.TempDir()
		codexDir := filepath.Join(tmpDir, ".codex")
		_ = os.MkdirAll(codexDir, 0o700)
		_ = os.WriteFile(filepath.Join(codexDir, "auth.json"), data, 0o600)

		resolver := localstate.New(
			localstate.WithHomeDir(tmpDir),
			localstate.WithEnvMap(map[string]string{}),
		)
		adapter := New(WithResolver(resolver))
		src := adapter.Sources()[0]

		_, _ = src.Fetch(context.Background())
	})
}

func FuzzDecodeJWTUnverified(f *testing.F) {
	seeds := []string{
		"eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJlbWFpbCI6ImRldmVsb3BlckBleGFtcGxlLmNvbSIsImh0dHBzOi8vYXBpLm9wZW5haS5jb20vYXV0aCI6eyJjaGF0Z3B0X2FjY291bnRfaWQiOiJhY2MtMTIzIiwiY2hhdGdwdF9wbGFuX3R5cGUiOiJwcm8ifX0.dummy_sig",
		"header.payload.sig",
		"not-a-jwt",
		"",
		"a.b",
		"a.b.c.d",
	}

	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, token string) {
		_, _ = decodeJWTUnverified(token)
	})
}
