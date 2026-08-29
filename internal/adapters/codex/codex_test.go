package codex_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v5"

	"github.com/mattwalters/dipstick"
	"github.com/mattwalters/dipstick/internal/adapters/codex"
	"github.com/mattwalters/dipstick/internal/localstate"
)

func makeTestJWT(header map[string]any, payload map[string]any) string {
	hBytes, _ := json.Marshal(header)
	pBytes, _ := json.Marshal(payload)
	hB64 := base64.RawURLEncoding.EncodeToString(hBytes)
	pB64 := base64.RawURLEncoding.EncodeToString(pBytes)
	sigB64 := base64.RawURLEncoding.EncodeToString([]byte("signature-bytes"))
	return fmt.Sprintf("%s.%s.%s", hB64, pB64, sigB64)
}

func TestAdapter_IDAndName(t *testing.T) {
	a := codex.New()
	if a == nil {
		t.Fatalf("expected non-nil adapter")
	}
	if a.ID() != dipstick.ProviderCodex {
		t.Errorf("expected ID %q, got %q", dipstick.ProviderCodex, a.ID())
	}
	if a.Name() != "codex" {
		t.Errorf("expected name %q, got %q", "codex", a.Name())
	}
	sources := a.Sources()
	if len(sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(sources))
	}
	if sources[0].ID() != dipstick.SourceLocalState {
		t.Errorf("expected source ID %q, got %q", dipstick.SourceLocalState, sources[0].ID())
	}
	if sources[0].Tier() != dipstick.TierLocalState {
		t.Errorf("expected source tier %v, got %v", dipstick.TierLocalState, sources[0].Tier())
	}
}

func TestLocalStateSource_SubscriptionFixture(t *testing.T) {
	fixturePath := filepath.Join("testdata", "auth_subscription.json")
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	tmpDir := t.TempDir()
	codexDir := filepath.Join(tmpDir, ".codex")
	if err := os.MkdirAll(codexDir, 0o700); err != nil {
		t.Fatalf("creating test dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "auth.json"), data, 0o600); err != nil {
		t.Fatalf("writing auth.json: %v", err)
	}

	resolver := localstate.New(
		localstate.WithHomeDir(tmpDir),
		localstate.WithEnvMap(map[string]string{}),
	)
	adapter := codex.New(codex.WithResolver(resolver))

	sources := adapter.Sources()
	if len(sources) == 0 {
		t.Fatalf("expected sources from adapter")
	}
	src := sources[0]

	ctx := context.Background()
	if !src.Available(ctx) {
		t.Fatalf("expected source to be available with auth.json present")
	}

	report, err := src.Fetch(ctx)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if report.Provider != dipstick.ProviderCodex {
		t.Errorf("expected provider %s, got %s", dipstick.ProviderCodex, report.Provider)
	}
	if report.Source != dipstick.SourceLocalState {
		t.Errorf("expected source %s, got %s", dipstick.SourceLocalState, report.Source)
	}
	if report.Confidence != dipstick.ConfidenceDerived {
		t.Errorf("expected confidence %s, got %s", dipstick.ConfidenceDerived, report.Confidence)
	}
	if report.Identity == nil {
		t.Fatalf("expected non-nil Identity")
	}
	if report.Identity.Email != "developer@example.com" {
		t.Errorf("expected email 'developer@example.com', got %q", report.Identity.Email)
	}
	if report.Identity.AccountID != "acc-chatgpt-12345" {
		t.Errorf("expected account_id 'acc-chatgpt-12345', got %q", report.Identity.AccountID)
	}
	if report.Identity.Plan != "pro" {
		t.Errorf("expected plan 'pro', got %q", report.Identity.Plan)
	}
	if report.Windows != nil {
		t.Errorf("expected Windows to be nil from Tier 2 source, got %+v", report.Windows)
	}
	if report.Tokens != nil {
		t.Errorf("expected Tokens to be nil from Tier 2 source, got %+v", report.Tokens)
	}
}

func TestLocalStateSource_ApiKeyFixture(t *testing.T) {
	fixturePath := filepath.Join("testdata", "auth_api_key.json")
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	tmpDir := t.TempDir()
	codexDir := filepath.Join(tmpDir, ".codex")
	if err := os.MkdirAll(codexDir, 0o700); err != nil {
		t.Fatalf("creating test dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "auth.json"), data, 0o600); err != nil {
		t.Fatalf("writing auth.json: %v", err)
	}

	resolver := localstate.New(
		localstate.WithHomeDir(tmpDir),
		localstate.WithEnvMap(map[string]string{}),
	)
	adapter := codex.New(codex.WithResolver(resolver))

	sources := adapter.Sources()
	src := sources[0]

	ctx := context.Background()
	if !src.Available(ctx) {
		t.Fatalf("expected source to be available")
	}

	report, err := src.Fetch(ctx)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if report.Identity == nil {
		t.Fatalf("expected non-nil Identity")
	}
	if report.Identity.Plan != "api_key" {
		t.Errorf("expected plan 'api_key', got %q", report.Identity.Plan)
	}
	if report.Windows != nil {
		t.Errorf("expected Windows to be nil for API key mode, got %+v", report.Windows)
	}
	if report.Tokens != nil {
		t.Errorf("expected Tokens to be nil for API key mode, got %+v", report.Tokens)
	}
}

func TestLocalStateSource_PaddedJwtFixture(t *testing.T) {
	fixturePath := filepath.Join("testdata", "auth_padded_jwt.json")
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	tmpDir := t.TempDir()
	codexDir := filepath.Join(tmpDir, ".codex")
	if err := os.MkdirAll(codexDir, 0o700); err != nil {
		t.Fatalf("creating test dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "auth.json"), data, 0o600); err != nil {
		t.Fatalf("writing auth.json: %v", err)
	}

	resolver := localstate.New(
		localstate.WithHomeDir(tmpDir),
		localstate.WithEnvMap(map[string]string{}),
	)
	adapter := codex.New(codex.WithResolver(resolver))
	src := adapter.Sources()[0]

	report, err := src.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch failed on padded JWT fixture: %v", err)
	}

	if report.Identity == nil {
		t.Fatalf("expected non-nil Identity")
	}
	if report.Identity.Email != "padded@example.com" {
		t.Errorf("expected email 'padded@example.com', got %q", report.Identity.Email)
	}
	if report.Identity.AccountID != "acc-padded-999" {
		t.Errorf("expected account_id 'acc-padded-999', got %q", report.Identity.AccountID)
	}
	if report.Identity.Plan != "plus" {
		t.Errorf("expected plan 'plus', got %q", report.Identity.Plan)
	}
}

func TestLocalStateSource_MalformedAndCorruptFixtures(t *testing.T) {
	tests := []struct {
		name        string
		fixtureFile string
	}{
		{"malformed_jwt", "auth_malformed_jwt.json"},
		{"corrupt_json", "auth_corrupt.json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixturePath := filepath.Join("testdata", tt.fixtureFile)
			data, err := os.ReadFile(fixturePath)
			if err != nil {
				t.Fatalf("reading fixture: %v", err)
			}

			tmpDir := t.TempDir()
			codexDir := filepath.Join(tmpDir, ".codex")
			if err := os.MkdirAll(codexDir, 0o700); err != nil {
				t.Fatalf("creating test dir: %v", err)
			}
			if err := os.WriteFile(filepath.Join(codexDir, "auth.json"), data, 0o600); err != nil {
				t.Fatalf("writing auth.json: %v", err)
			}

			resolver := localstate.New(
				localstate.WithHomeDir(tmpDir),
				localstate.WithEnvMap(map[string]string{}),
			)
			adapter := codex.New(codex.WithResolver(resolver))
			src := adapter.Sources()[0]

			report, err := src.Fetch(context.Background())
			if err == nil {
				t.Fatalf("expected error from %s, got report: %+v", tt.name, report)
			}
			if !errors.Is(err, dipstick.ErrParseFailed) {
				t.Errorf("expected ErrParseFailed, got: %v", err)
			}
		})
	}
}

func TestLocalStateSource_MissingAndEmptyAuthFile(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		tmpDir := t.TempDir()
		resolver := localstate.New(
			localstate.WithHomeDir(tmpDir),
			localstate.WithEnvMap(map[string]string{}),
		)
		adapter := codex.New(codex.WithResolver(resolver))
		src := adapter.Sources()[0]

		if src.Available(context.Background()) {
			t.Errorf("expected Available to be false for missing auth.json")
		}

		_, err := src.Fetch(context.Background())
		if err == nil {
			t.Fatalf("expected error for missing auth.json, got nil")
		}
		if !errors.Is(err, dipstick.ErrNotInstalled) {
			t.Errorf("expected ErrNotInstalled for missing file, got %v", err)
		}
	})

	t.Run("empty file", func(t *testing.T) {
		tmpDir := t.TempDir()
		codexDir := filepath.Join(tmpDir, ".codex")
		_ = os.MkdirAll(codexDir, 0o700)
		_ = os.WriteFile(filepath.Join(codexDir, "auth.json"), []byte("   \n"), 0o600)

		resolver := localstate.New(localstate.WithHomeDir(tmpDir), localstate.WithEnvMap(map[string]string{}))
		adapter := codex.New(codex.WithResolver(resolver))
		src := adapter.Sources()[0]

		_, err := src.Fetch(context.Background())
		if err == nil || !errors.Is(err, dipstick.ErrParseFailed) {
			t.Errorf("expected ErrParseFailed for empty file, got: %v", err)
		}
	})

	t.Run("empty json object", func(t *testing.T) {
		tmpDir := t.TempDir()
		codexDir := filepath.Join(tmpDir, ".codex")
		_ = os.MkdirAll(codexDir, 0o700)
		_ = os.WriteFile(filepath.Join(codexDir, "auth.json"), []byte("{}"), 0o600)

		resolver := localstate.New(localstate.WithHomeDir(tmpDir), localstate.WithEnvMap(map[string]string{}))
		adapter := codex.New(codex.WithResolver(resolver))
		src := adapter.Sources()[0]

		_, err := src.Fetch(context.Background())
		if err == nil || !errors.Is(err, dipstick.ErrParseFailed) {
			t.Errorf("expected ErrParseFailed for empty JSON object, got: %v", err)
		}
	})
}

func TestLocalStateSource_EnvironmentOverrides(t *testing.T) {
	jwt := makeTestJWT(
		map[string]any{"alg": "RS256", "typ": "JWT"},
		map[string]any{
			"email": "envtest@example.com",
			"https://api.openai.com/auth": map[string]any{
				"chatgpt_account_id": "acc-env-123",
				"chatgpt_plan_type":  "team",
			},
		},
	)
	authData := fmt.Sprintf(`{"auth_mode":"chatgpt","tokens":{"id_token":%q}}`, jwt)

	t.Run("CODEX_HOME override", func(t *testing.T) {
		customHome := t.TempDir()
		if err := os.WriteFile(filepath.Join(customHome, "auth.json"), []byte(authData), 0o600); err != nil {
			t.Fatal(err)
		}

		resolver := localstate.New(
			localstate.WithHomeDir("/other/home"),
			localstate.WithEnvMap(map[string]string{
				"CODEX_HOME": customHome,
			}),
		)
		adapter := codex.New(codex.WithResolver(resolver))
		src := adapter.Sources()[0]

		if !src.Available(context.Background()) {
			t.Fatalf("expected source available via CODEX_HOME")
		}

		rep, err := src.Fetch(context.Background())
		if err != nil {
			t.Fatalf("Fetch failed: %v", err)
		}
		if rep.Identity.Email != "envtest@example.com" {
			t.Errorf("expected email envtest@example.com, got %s", rep.Identity.Email)
		}
		if rep.Identity.Plan != "team" {
			t.Errorf("expected plan team, got %s", rep.Identity.Plan)
		}
	})

	t.Run("CODEX_CONFIG_DIR fallback override", func(t *testing.T) {
		customConfig := t.TempDir()
		if err := os.WriteFile(filepath.Join(customConfig, "auth.json"), []byte(authData), 0o600); err != nil {
			t.Fatal(err)
		}

		resolver := localstate.New(
			localstate.WithHomeDir("/other/home"),
			localstate.WithEnvMap(map[string]string{
				"CODEX_CONFIG_DIR": customConfig,
			}),
		)
		adapter := codex.New(codex.WithResolver(resolver))
		src := adapter.Sources()[0]

		if !src.Available(context.Background()) {
			t.Fatalf("expected source available via CODEX_CONFIG_DIR")
		}

		rep, err := src.Fetch(context.Background())
		if err != nil {
			t.Fatalf("Fetch failed: %v", err)
		}
		if rep.Identity.AccountID != "acc-env-123" {
			t.Errorf("expected account_id acc-env-123, got %s", rep.Identity.AccountID)
		}
	})
}

func TestLocalStateSource_JWTVariousPlansAndFallbacks(t *testing.T) {
	plans := []string{"pro", "plus", "free", "team", "enterprise"}

	for _, plan := range plans {
		t.Run("plan_"+plan, func(t *testing.T) {
			jwt := makeTestJWT(
				map[string]any{"alg": "RS256", "typ": "JWT"},
				map[string]any{
					"email": "user@example.com",
					"https://api.openai.com/auth": map[string]any{
						"chatgpt_account_id": "acc-plan-" + plan,
						"chatgpt_plan_type":  plan,
					},
				},
			)
			authData := fmt.Sprintf(`{"auth_mode":"chatgpt","tokens":{"id_token":%q}}`, jwt)

			tmpDir := t.TempDir()
			codexDir := filepath.Join(tmpDir, ".codex")
			_ = os.MkdirAll(codexDir, 0o700)
			_ = os.WriteFile(filepath.Join(codexDir, "auth.json"), []byte(authData), 0o600)

			resolver := localstate.New(localstate.WithHomeDir(tmpDir), localstate.WithEnvMap(map[string]string{}))
			adapter := codex.New(codex.WithResolver(resolver))

			rep, err := adapter.Sources()[0].Fetch(context.Background())
			if err != nil {
				t.Fatalf("Fetch failed for plan %s: %v", plan, err)
			}
			if rep.Identity.Plan != plan {
				t.Errorf("expected plan %s, got %s", plan, rep.Identity.Plan)
			}
		})
	}

	t.Run("top-level claims fallback", func(t *testing.T) {
		jwt := makeTestJWT(
			map[string]any{"alg": "RS256", "typ": "JWT"},
			map[string]any{
				"email":              "fallback@example.com",
				"chatgpt_account_id": "acc-fallback",
				"chatgpt_plan_type":  "pro",
			},
		)
		authData := fmt.Sprintf(`{"auth_mode":"chatgpt","tokens":{"id_token":%q}}`, jwt)

		tmpDir := t.TempDir()
		codexDir := filepath.Join(tmpDir, ".codex")
		_ = os.MkdirAll(codexDir, 0o700)
		_ = os.WriteFile(filepath.Join(codexDir, "auth.json"), []byte(authData), 0o600)

		resolver := localstate.New(localstate.WithHomeDir(tmpDir), localstate.WithEnvMap(map[string]string{}))
		adapter := codex.New(codex.WithResolver(resolver))

		rep, err := adapter.Sources()[0].Fetch(context.Background())
		if err != nil {
			t.Fatalf("Fetch failed: %v", err)
		}
		if rep.Identity.Email != "fallback@example.com" {
			t.Errorf("expected fallback email, got %s", rep.Identity.Email)
		}
		if rep.Identity.AccountID != "acc-fallback" {
			t.Errorf("expected fallback account ID, got %s", rep.Identity.AccountID)
		}
		if rep.Identity.Plan != "pro" {
			t.Errorf("expected fallback plan pro, got %s", rep.Identity.Plan)
		}
	})

	t.Run("tokens.account_id fallback when omitted in JWT", func(t *testing.T) {
		jwt := makeTestJWT(
			map[string]any{"alg": "RS256", "typ": "JWT"},
			map[string]any{
				"email": "noacc@example.com",
				"https://api.openai.com/auth": map[string]any{
					"chatgpt_plan_type": "plus",
				},
			},
		)
		authData := fmt.Sprintf(`{"auth_mode":"chatgpt","tokens":{"id_token":%q,"account_id":"acc-from-tokens"}}`, jwt)

		tmpDir := t.TempDir()
		codexDir := filepath.Join(tmpDir, ".codex")
		_ = os.MkdirAll(codexDir, 0o700)
		_ = os.WriteFile(filepath.Join(codexDir, "auth.json"), []byte(authData), 0o600)

		resolver := localstate.New(localstate.WithHomeDir(tmpDir), localstate.WithEnvMap(map[string]string{}))
		adapter := codex.New(codex.WithResolver(resolver))

		rep, err := adapter.Sources()[0].Fetch(context.Background())
		if err != nil {
			t.Fatalf("Fetch failed: %v", err)
		}
		if rep.Identity.AccountID != "acc-from-tokens" {
			t.Errorf("expected account ID fallback to tokens.account_id, got %s", rep.Identity.AccountID)
		}
	})
}

func TestLocalStateSource_Detect(t *testing.T) {
	tmpDir := t.TempDir()
	codexDir := filepath.Join(tmpDir, ".codex")
	_ = os.MkdirAll(codexDir, 0o700)

	authData := `{"auth_mode":"api_key","OPENAI_API_KEY":"sk-test-12345"}`
	_ = os.WriteFile(filepath.Join(codexDir, "auth.json"), []byte(authData), 0o600)

	resolver := localstate.New(localstate.WithHomeDir(tmpDir), localstate.WithEnvMap(map[string]string{}))
	adapter := codex.New(codex.WithResolver(resolver))

	det, err := adapter.Detect(context.Background())
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	if !det.Authenticated {
		t.Errorf("expected Authenticated to be true when valid auth.json exists")
	}
}

func TestLocalStateSource_CollectWholeRunAndSchemaValidation(t *testing.T) {
	fixturePath := filepath.Join("testdata", "auth_subscription.json")
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	tmpDir := t.TempDir()
	codexDir := filepath.Join(tmpDir, ".codex")
	_ = os.MkdirAll(codexDir, 0o700)
	_ = os.WriteFile(filepath.Join(codexDir, "auth.json"), data, 0o600)

	resolver := localstate.New(localstate.WithHomeDir(tmpDir), localstate.WithEnvMap(map[string]string{}))
	adapter := codex.New(codex.WithResolver(resolver))

	report, err := dipstick.Collect(context.Background(),
		dipstick.WithProviders(dipstick.ProviderCodex),
		dipstick.WithAdapter(adapter),
	)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	if len(report.Providers) != 1 {
		t.Fatalf("expected 1 provider report, got %d (errors: %+v)", len(report.Providers), report.Errors)
	}
	pr := report.Providers[0]
	if pr.Provider != dipstick.ProviderCodex {
		t.Errorf("expected provider codex, got %s", pr.Provider)
	}
	if pr.Source != dipstick.SourceLocalState {
		t.Errorf("expected source local_state, got %s", pr.Source)
	}
	if pr.Confidence != dipstick.ConfidenceDerived {
		t.Errorf("expected confidence derived, got %s", pr.Confidence)
	}
	if pr.Identity == nil || pr.Identity.Email != "developer@example.com" || pr.Identity.Plan != "pro" {
		t.Errorf("unexpected identity: %+v", pr.Identity)
	}
	if pr.Windows != nil {
		t.Errorf("expected Windows to be nil, got %+v", pr.Windows)
	}
	if pr.Tokens != nil {
		t.Errorf("expected Tokens to be nil, got %+v", pr.Tokens)
	}

	// Validate JSON schema
	schemaPath := filepath.Join("..", "..", "..", "schema", "dipstick.v1.json")
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft2020
	schema, err := compiler.Compile(schemaPath)
	if err != nil {
		t.Fatalf("failed compiling schema %s: %v", schemaPath, err)
	}

	reportBytes, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshalling report: %v", err)
	}

	var unmarshaled any
	if err := json.Unmarshal(reportBytes, &unmarshaled); err != nil {
		t.Fatalf("unmarshalling report JSON: %v", err)
	}

	if err := schema.Validate(unmarshaled); err != nil {
		t.Errorf("report failed dipstick.v1 schema validation: %v\nJSON: %s", err, string(reportBytes))
	}
}

func TestLocalStateSource_ZeroLeakageInvariant(t *testing.T) {
	secretKey := "sk-super-secret-openai-api-key-9999"
	secretToken := "secret-jwt-token-string-xyz"
	authData := fmt.Sprintf(`{
		"auth_mode": "api_key",
		"OPENAI_API_KEY": %q,
		"tokens": {
			"id_token": %q,
			"access_token": "secret-access-token",
			"refresh_token": "secret-refresh-token",
			"account_id": "acc-leak-test"
		}
	}`, secretKey, secretToken)

	tmpDir := t.TempDir()
	codexDir := filepath.Join(tmpDir, ".codex")
	_ = os.MkdirAll(codexDir, 0o700)
	_ = os.WriteFile(filepath.Join(codexDir, "auth.json"), []byte(authData), 0o600)

	resolver := localstate.New(localstate.WithHomeDir(tmpDir), localstate.WithEnvMap(map[string]string{}))
	adapter := codex.New(codex.WithResolver(resolver))

	report, err := adapter.Sources()[0].Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	reportBytes, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshaling report: %v", err)
	}
	reportStr := string(reportBytes)

	if strings.Contains(reportStr, secretKey) {
		t.Errorf("Report JSON leaked secret API key: %s", reportStr)
	}
	if strings.Contains(reportStr, secretToken) {
		t.Errorf("Report JSON leaked secret token: %s", reportStr)
	}
	if strings.Contains(reportStr, "secret-access-token") {
		t.Errorf("Report JSON leaked access token: %s", reportStr)
	}
	if strings.Contains(reportStr, "secret-refresh-token") {
		t.Errorf("Report JSON leaked refresh token: %s", reportStr)
	}
}
