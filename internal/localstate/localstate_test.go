package localstate_test

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
	"time"

	"github.com/mattwalters/dipstick/internal/localstate"
)

type mockKeychainReader struct {
	passwords map[string][]byte
	err       error
}

func (m *mockKeychainReader) GetGenericPassword(ctx context.Context, service string, account string) ([]byte, error) {
	if m.err != nil {
		return nil, m.err
	}
	if val, ok := m.passwords[service]; ok {
		return val, nil
	}
	return nil, localstate.ErrKeychainItemNotFound
}

func makeTestJWT(claims map[string]any) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	claimsBytes, _ := json.Marshal(claims)
	payload := base64.RawURLEncoding.EncodeToString(claimsBytes)
	sig := base64.RawURLEncoding.EncodeToString([]byte("signature"))
	return fmt.Sprintf("%s.%s.%s", header, payload, sig)
}

func TestResolver_DipstickPaths(t *testing.T) {
	home := "/test/home/user"

	t.Run("default paths", func(t *testing.T) {
		r := localstate.New(
			localstate.WithHomeDir(home),
			localstate.WithEnvMap(map[string]string{}),
			localstate.WithUserConfigDirFunc(func() (string, error) { return "", errors.New("none") }),
			localstate.WithUserCacheDirFunc(func() (string, error) { return "", errors.New("none") }),
		)

		config, err := r.ConfigDir()
		if err != nil {
			t.Fatalf("ConfigDir failed: %v", err)
		}
		if expected := filepath.Join(home, ".config", "dipstick"); config != expected {
			t.Errorf("ConfigDir: expected %s, got %s", expected, config)
		}

		data, err := r.DataDir()
		if err != nil {
			t.Fatalf("DataDir failed: %v", err)
		}
		if expected := filepath.Join(home, ".local", "share", "dipstick"); data != expected {
			t.Errorf("DataDir: expected %s, got %s", expected, data)
		}

		cache, err := r.CacheDir()
		if err != nil {
			t.Fatalf("CacheDir failed: %v", err)
		}
		if expected := filepath.Join(home, ".cache", "dipstick"); cache != expected {
			t.Errorf("CacheDir: expected %s, got %s", expected, cache)
		}
	})

	t.Run("env overrides", func(t *testing.T) {
		env := map[string]string{
			"DIPSTICK_CONFIG_DIR": "/custom/dipstick/config",
			"DIPSTICK_DATA_DIR":   "/custom/dipstick/data",
			"DIPSTICK_CACHE_DIR":  "/custom/dipstick/cache",
		}
		r := localstate.New(
			localstate.WithHomeDir(home),
			localstate.WithEnvMap(env),
		)

		config, err := r.ConfigDir()
		if err != nil || config != "/custom/dipstick/config" {
			t.Errorf("ConfigDir env override: expected /custom/dipstick/config, got %s (err: %v)", config, err)
		}

		data, err := r.DataDir()
		if err != nil || data != "/custom/dipstick/data" {
			t.Errorf("DataDir env override: expected /custom/dipstick/data, got %s (err: %v)", data, err)
		}

		cache, err := r.CacheDir()
		if err != nil || cache != "/custom/dipstick/cache" {
			t.Errorf("CacheDir env override: expected /custom/dipstick/cache, got %s (err: %v)", cache, err)
		}
	})

	t.Run("xdg overrides", func(t *testing.T) {
		env := map[string]string{
			"XDG_CONFIG_HOME": "/custom/xdg/config",
			"XDG_DATA_HOME":   "/custom/xdg/data",
			"XDG_CACHE_HOME":  "/custom/xdg/cache",
		}
		r := localstate.New(
			localstate.WithHomeDir(home),
			localstate.WithEnvMap(env),
		)

		config, err := r.ConfigDir()
		if err != nil || config != "/custom/xdg/config/dipstick" {
			t.Errorf("ConfigDir XDG override: expected /custom/xdg/config/dipstick, got %s", config)
		}

		data, err := r.DataDir()
		if err != nil || data != "/custom/xdg/data/dipstick" {
			t.Errorf("DataDir XDG override: expected /custom/xdg/data/dipstick, got %s", data)
		}

		cache, err := r.CacheDir()
		if err != nil || cache != "/custom/xdg/cache/dipstick" {
			t.Errorf("CacheDir XDG override: expected /custom/xdg/cache/dipstick, got %s", cache)
		}
	})
}

func TestResolver_ClaudePaths(t *testing.T) {
	home := "/test/home/user"

	t.Run("default paths", func(t *testing.T) {
		r := localstate.New(
			localstate.WithHomeDir(home),
			localstate.WithEnvMap(map[string]string{}),
		)

		paths, err := r.ClaudePaths()
		if err != nil {
			t.Fatalf("ClaudePaths failed: %v", err)
		}

		expectedConfig := filepath.Join(home, ".claude")
		if paths.ConfigDir != expectedConfig {
			t.Errorf("ConfigDir: expected %s, got %s", expectedConfig, paths.ConfigDir)
		}
		if paths.SettingsFile != filepath.Join(expectedConfig, "settings.json") {
			t.Errorf("SettingsFile: expected %s, got %s", filepath.Join(expectedConfig, "settings.json"), paths.SettingsFile)
		}
		if paths.ProjectsDir != filepath.Join(expectedConfig, "projects") {
			t.Errorf("ProjectsDir: expected %s, got %s", filepath.Join(expectedConfig, "projects"), paths.ProjectsDir)
		}
		if paths.SessionsDir != filepath.Join(expectedConfig, "sessions") {
			t.Errorf("SessionsDir: expected %s, got %s", filepath.Join(expectedConfig, "sessions"), paths.SessionsDir)
		}
		if paths.HistoryFile != filepath.Join(expectedConfig, "history.jsonl") {
			t.Errorf("HistoryFile: expected %s, got %s", filepath.Join(expectedConfig, "history.jsonl"), paths.HistoryFile)
		}
		if paths.CredentialsFile != filepath.Join(expectedConfig, ".credentials.json") {
			t.Errorf("CredentialsFile: expected %s, got %s", filepath.Join(expectedConfig, ".credentials.json"), paths.CredentialsFile)
		}
	})

	t.Run("CLAUDE_CONFIG_DIR override", func(t *testing.T) {
		custom := "/custom/claude/config"
		r := localstate.New(
			localstate.WithHomeDir(home),
			localstate.WithEnvMap(map[string]string{
				"CLAUDE_CONFIG_DIR": custom,
			}),
		)

		paths, err := r.ClaudePaths()
		if err != nil {
			t.Fatalf("ClaudePaths failed: %v", err)
		}
		if paths.ConfigDir != custom {
			t.Errorf("ConfigDir override: expected %s, got %s", custom, paths.ConfigDir)
		}
		if paths.CredentialsFile != filepath.Join(custom, ".credentials.json") {
			t.Errorf("CredentialsFile override: expected %s, got %s", filepath.Join(custom, ".credentials.json"), paths.CredentialsFile)
		}
	})
}

func TestResolver_CodexPaths(t *testing.T) {
	home := "/test/home/user"

	t.Run("default paths", func(t *testing.T) {
		r := localstate.New(
			localstate.WithHomeDir(home),
			localstate.WithEnvMap(map[string]string{}),
		)

		paths, err := r.CodexPaths()
		if err != nil {
			t.Fatalf("CodexPaths failed: %v", err)
		}

		expectedHome := filepath.Join(home, ".codex")
		if paths.HomeDir != expectedHome {
			t.Errorf("HomeDir: expected %s, got %s", expectedHome, paths.HomeDir)
		}
		if paths.AuthFile != filepath.Join(expectedHome, "auth.json") {
			t.Errorf("AuthFile: expected %s, got %s", filepath.Join(expectedHome, "auth.json"), paths.AuthFile)
		}
		if paths.ConfigFile != filepath.Join(expectedHome, "config.toml") {
			t.Errorf("ConfigFile: expected %s, got %s", filepath.Join(expectedHome, "config.toml"), paths.ConfigFile)
		}
		if paths.SessionsDir != filepath.Join(expectedHome, "sessions") {
			t.Errorf("SessionsDir: expected %s, got %s", filepath.Join(expectedHome, "sessions"), paths.SessionsDir)
		}
		if paths.HistoryFile != filepath.Join(expectedHome, "history.jsonl") {
			t.Errorf("HistoryFile: expected %s, got %s", filepath.Join(expectedHome, "history.jsonl"), paths.HistoryFile)
		}
	})

	t.Run("CODEX_HOME override", func(t *testing.T) {
		custom := "/custom/codex/home"
		r := localstate.New(
			localstate.WithHomeDir(home),
			localstate.WithEnvMap(map[string]string{
				"CODEX_HOME": custom,
			}),
		)

		paths, err := r.CodexPaths()
		if err != nil {
			t.Fatalf("CodexPaths failed: %v", err)
		}
		if paths.HomeDir != custom {
			t.Errorf("HomeDir CODEX_HOME: expected %s, got %s", custom, paths.HomeDir)
		}
		if paths.AuthFile != filepath.Join(custom, "auth.json") {
			t.Errorf("AuthFile: expected %s, got %s", filepath.Join(custom, "auth.json"), paths.AuthFile)
		}
	})

	t.Run("CODEX_CONFIG_DIR fallback override", func(t *testing.T) {
		custom := "/custom/codex/config"
		r := localstate.New(
			localstate.WithHomeDir(home),
			localstate.WithEnvMap(map[string]string{
				"CODEX_CONFIG_DIR": custom,
			}),
		)

		paths, err := r.CodexPaths()
		if err != nil {
			t.Fatalf("CodexPaths failed: %v", err)
		}
		if paths.HomeDir != custom {
			t.Errorf("HomeDir CODEX_CONFIG_DIR: expected %s, got %s", custom, paths.HomeDir)
		}
	})
}

func TestResolver_AntigravityPaths(t *testing.T) {
	home := "/test/home/user"

	t.Run("macOS layout", func(t *testing.T) {
		r := localstate.New(
			localstate.WithHomeDir(home),
			localstate.WithGOOS("darwin"),
			localstate.WithEnvMap(map[string]string{}),
		)

		paths, err := r.AntigravityPaths()
		if err != nil {
			t.Fatalf("AntigravityPaths failed: %v", err)
		}

		expectedCLI := filepath.Join(home, ".gemini", "antigravity-cli")
		if paths.CLIConfigDir != expectedCLI {
			t.Errorf("CLIConfigDir: expected %s, got %s", expectedCLI, paths.CLIConfigDir)
		}
		if paths.CLIOAuthTokenFile != filepath.Join(expectedCLI, "antigravity-oauth-token") {
			t.Errorf("CLIOAuthTokenFile: expected %s, got %s", filepath.Join(expectedCLI, "antigravity-oauth-token"), paths.CLIOAuthTokenFile)
		}

		expectedDesktop := filepath.Join(home, "Library", "Application Support", "Antigravity")
		if paths.DesktopConfigDir != expectedDesktop {
			t.Errorf("DesktopConfigDir: expected %s, got %s", expectedDesktop, paths.DesktopConfigDir)
		}
		if paths.DesktopDataDir != expectedDesktop {
			t.Errorf("DesktopDataDir: expected %s, got %s", expectedDesktop, paths.DesktopDataDir)
		}
		if paths.DesktopLegacyDir != filepath.Join(home, ".antigravity") {
			t.Errorf("DesktopLegacyDir: expected %s, got %s", filepath.Join(home, ".antigravity"), paths.DesktopLegacyDir)
		}
	})

	t.Run("Linux layout with XDG", func(t *testing.T) {
		r := localstate.New(
			localstate.WithHomeDir(home),
			localstate.WithGOOS("linux"),
			localstate.WithEnvMap(map[string]string{
				"XDG_CONFIG_HOME": "/custom/xdg/config",
				"XDG_DATA_HOME":   "/custom/xdg/data",
			}),
		)

		paths, err := r.AntigravityPaths()
		if err != nil {
			t.Fatalf("AntigravityPaths failed: %v", err)
		}

		expectedDesktopConfig := "/custom/xdg/config/Antigravity"
		if paths.DesktopConfigDir != expectedDesktopConfig {
			t.Errorf("DesktopConfigDir: expected %s, got %s", expectedDesktopConfig, paths.DesktopConfigDir)
		}

		expectedDesktopData := "/custom/xdg/data/Antigravity"
		if paths.DesktopDataDir != expectedDesktopData {
			t.Errorf("DesktopDataDir: expected %s, got %s", expectedDesktopData, paths.DesktopDataDir)
		}
	})

	t.Run("ANTIGRAVITY_CONFIG_DIR override", func(t *testing.T) {
		custom := "/custom/antigravity/cli"
		r := localstate.New(
			localstate.WithHomeDir(home),
			localstate.WithEnvMap(map[string]string{
				"ANTIGRAVITY_CONFIG_DIR": custom,
			}),
		)

		paths, err := r.AntigravityPaths()
		if err != nil {
			t.Fatalf("AntigravityPaths failed: %v", err)
		}
		if paths.CLIConfigDir != custom {
			t.Errorf("CLIConfigDir override: expected %s, got %s", custom, paths.CLIConfigDir)
		}
	})
}

func TestResolver_OpenCodePaths(t *testing.T) {
	home := "/test/home/user"

	t.Run("default paths", func(t *testing.T) {
		r := localstate.New(
			localstate.WithHomeDir(home),
			localstate.WithEnvMap(map[string]string{}),
		)

		paths, err := r.OpenCodePaths()
		if err != nil {
			t.Fatalf("OpenCodePaths failed: %v", err)
		}

		expectedConfig := filepath.Join(home, ".opencode")
		if paths.ConfigDir != expectedConfig {
			t.Errorf("ConfigDir: expected %s, got %s", expectedConfig, paths.ConfigDir)
		}
		if paths.ConfigFile != filepath.Join(expectedConfig, "config.json") {
			t.Errorf("ConfigFile: expected %s, got %s", filepath.Join(expectedConfig, "config.json"), paths.ConfigFile)
		}
		if paths.AuthFile != filepath.Join(expectedConfig, "auth.json") {
			t.Errorf("AuthFile: expected %s, got %s", filepath.Join(expectedConfig, "auth.json"), paths.AuthFile)
		}
	})

	t.Run("OPENCODE_CONFIG_DIR override", func(t *testing.T) {
		custom := "/custom/opencode"
		r := localstate.New(
			localstate.WithHomeDir(home),
			localstate.WithEnvMap(map[string]string{
				"OPENCODE_CONFIG_DIR": custom,
			}),
		)

		paths, err := r.OpenCodePaths()
		if err != nil {
			t.Fatalf("OpenCodePaths failed: %v", err)
		}
		if paths.ConfigDir != custom {
			t.Errorf("ConfigDir override: expected %s, got %s", custom, paths.ConfigDir)
		}
	})
}

func TestResolver_ProviderConfigDir(t *testing.T) {
	home := "/test/home/user"
	r := localstate.New(
		localstate.WithHomeDir(home),
		localstate.WithEnvMap(map[string]string{}),
		localstate.WithUserConfigDirFunc(func() (string, error) { return "", errors.New("none") }),
	)

	tests := []struct {
		provider string
		expected string
	}{
		{"antigravity", filepath.Join(home, ".gemini", "antigravity-cli")},
		{"claude", filepath.Join(home, ".claude")},
		{"codex", filepath.Join(home, ".codex")},
		{"opencode", filepath.Join(home, ".opencode")},
		{"custom", filepath.Join(home, ".config", "dipstick", "providers", "custom")},
	}

	for _, tc := range tests {
		dir, err := r.ProviderConfigDir(tc.provider)
		if err != nil {
			t.Errorf("ProviderConfigDir(%s) failed: %v", tc.provider, err)
		}
		if dir != tc.expected {
			t.Errorf("ProviderConfigDir(%s): expected %s, got %s", tc.provider, tc.expected, dir)
		}
	}
}

func TestResolver_ReadClaudeCredentials_Keychain(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	futureExpiry := now.Add(24 * time.Hour).UnixMilli()

	payload := fmt.Sprintf(`{
		"claudeAiOauth": {
			"accessToken": "sk-ant-test-keychain-token",
			"refreshToken": "refresh-test-keychain",
			"expiresAt": %d,
			"account": {
				"uuid": "acc-keychain-123",
				"emailAddress": "user@keychain.com"
			},
			"subscriptionType": "pro"
		}
	}`, futureExpiry)

	mockKM := &mockKeychainReader{
		passwords: map[string][]byte{
			localstate.ClaudeCredentialService: []byte(payload),
		},
	}

	r := localstate.New(
		localstate.WithKeychain(mockKM),
		localstate.WithNow(func() time.Time { return now }),
	)

	creds, err := r.ReadClaudeCredentials(ctx)
	if err != nil {
		t.Fatalf("ReadClaudeCredentials failed: %v", err)
	}

	if creds.AccessToken != "sk-ant-test-keychain-token" {
		t.Errorf("AccessToken: got %s", creds.AccessToken)
	}
	if creds.AccountID != "acc-keychain-123" {
		t.Errorf("AccountID: got %s", creds.AccountID)
	}
	if creds.Email != "user@keychain.com" {
		t.Errorf("Email: got %s", creds.Email)
	}
	if creds.Subscription != "pro" {
		t.Errorf("Subscription: got %s", creds.Subscription)
	}
	if creds.ExpiresAt == nil {
		t.Errorf("expected non-nil ExpiresAt")
	}
}

func TestResolver_ReadClaudeCredentials_DiskFallback(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	futureExpiry := now.Add(24 * time.Hour).UnixMilli()

	claudeDir := filepath.Join(tmpDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0o700); err != nil {
		t.Fatal(err)
	}

	payload := fmt.Sprintf(`{
		"claudeAiOauth": {
			"accessToken": "sk-ant-test-disk-token",
			"refreshToken": "refresh-test-disk",
			"expiresAt": %d,
			"account": {
				"uuid": "acc-disk-456",
				"emailAddress": "user@disk.com"
			}
		}
	}`, futureExpiry)

	credsFile := filepath.Join(claudeDir, ".credentials.json")
	if err := os.WriteFile(credsFile, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}

	// Mock keychain returns unsupported, so it falls back to disk
	mockKM := &mockKeychainReader{
		err: localstate.ErrKeychainUnsupported,
	}

	r := localstate.New(
		localstate.WithHomeDir(tmpDir),
		localstate.WithKeychain(mockKM),
		localstate.WithNow(func() time.Time { return now }),
	)

	creds, err := r.ReadClaudeCredentials(ctx)
	if err != nil {
		t.Fatalf("ReadClaudeCredentials disk fallback failed: %v", err)
	}

	if creds.AccessToken != "sk-ant-test-disk-token" {
		t.Errorf("AccessToken: got %s", creds.AccessToken)
	}
	if creds.AccountID != "acc-disk-456" {
		t.Errorf("AccountID: got %s", creds.AccountID)
	}
}

func TestResolver_ReadCodexAuth(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	futureExpiry := now.Add(12 * time.Hour).Unix()

	codexDir := filepath.Join(tmpDir, ".codex")
	if err := os.MkdirAll(codexDir, 0o700); err != nil {
		t.Fatal(err)
	}

	authPayload := fmt.Sprintf(`{
		"auth_mode": "chatgpt",
		"OPENAI_API_KEY": "sk-openai-test-key",
		"tokens": {
			"id_token": "id-token-123",
			"access_token": "access-token-456",
			"refresh_token": "refresh-token-789",
			"account_id": "codex-acc-001",
			"expires_at": %d
		},
		"last_refresh": "2026-08-29T10:00:00Z"
	}`, futureExpiry)

	authFile := filepath.Join(codexDir, "auth.json")
	if err := os.WriteFile(authFile, []byte(authPayload), 0o600); err != nil {
		t.Fatal(err)
	}

	r := localstate.New(
		localstate.WithHomeDir(tmpDir),
		localstate.WithNow(func() time.Time { return now }),
	)

	auth, err := r.ReadCodexAuth(ctx)
	if err != nil {
		t.Fatalf("ReadCodexAuth failed: %v", err)
	}

	if auth.AuthMode != "chatgpt" {
		t.Errorf("AuthMode: got %s", auth.AuthMode)
	}
	if auth.APIKey != "sk-openai-test-key" {
		t.Errorf("APIKey: got %s", auth.APIKey)
	}
	if auth.Tokens == nil {
		t.Fatalf("Tokens is nil")
	}
	if auth.Tokens.AccessToken != "access-token-456" {
		t.Errorf("AccessToken: got %s", auth.Tokens.AccessToken)
	}
	if auth.Tokens.AccountID != "codex-acc-001" {
		t.Errorf("AccountID: got %s", auth.Tokens.AccountID)
	}
}

func TestCredentialExpiration(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	pastExpiry := now.Add(-1 * time.Hour).UnixMilli()

	t.Run("Claude expired token returns ErrCredentialExpired", func(t *testing.T) {
		payload := fmt.Sprintf(`{
			"claudeAiOauth": {
				"accessToken": "sk-ant-expired",
				"refreshToken": "refresh-expired",
				"expiresAt": %d
			}
		}`, pastExpiry)

		creds, err := localstate.ParseClaudeCredentials([]byte(payload), now)
		if !errors.Is(err, localstate.ErrCredentialExpired) {
			t.Errorf("expected ErrCredentialExpired, got %v", err)
		}
		if creds == nil || creds.AccessToken != "sk-ant-expired" {
			t.Errorf("expected parsed creds returned with expiration error, got %v", creds)
		}
	})

	t.Run("Claude JWT token expiry check", func(t *testing.T) {
		jwtExp := now.Add(-1 * time.Hour).Unix()
		jwtToken := makeTestJWT(map[string]any{
			"exp":   jwtExp,
			"sub":   "user_123",
			"email": "user@example.com",
		})

		payload := fmt.Sprintf(`{
			"claudeAiOauth": {
				"accessToken": "%s"
			}
		}`, jwtToken)

		_, err := localstate.ParseClaudeCredentials([]byte(payload), now)
		if !errors.Is(err, localstate.ErrCredentialExpired) {
			t.Errorf("expected ErrCredentialExpired for expired JWT, got %v", err)
		}
	})

	t.Run("Codex expired token returns ErrCredentialExpired", func(t *testing.T) {
		payload := fmt.Sprintf(`{
			"auth_mode": "chatgpt",
			"tokens": {
				"access_token": "access-expired",
				"expires_at": %d
			}
		}`, now.Add(-1*time.Hour).Unix())

		auth, err := localstate.ParseCodexAuth([]byte(payload), now)
		if !errors.Is(err, localstate.ErrCredentialExpired) {
			t.Errorf("expected ErrCredentialExpired, got %v", err)
		}
		if auth == nil || auth.Tokens == nil || auth.Tokens.AccessToken != "access-expired" {
			t.Errorf("expected parsed auth returned with expiration error")
		}
	})
}

func TestZeroLoggingAndSecurityInvariants(t *testing.T) {
	claudeCreds := &localstate.ClaudeCredentials{
		AccessToken:  "sk-ant-super-secret-token",
		RefreshToken: "refresh-super-secret-token",
		AccountID:    "acc-123",
		Email:        "user@example.com",
	}

	claudeStr := claudeCreds.String()
	if strings.Contains(claudeStr, "sk-ant-super-secret-token") || strings.Contains(claudeStr, "refresh-super-secret-token") {
		t.Errorf("ClaudeCredentials.String() leaked secret: %s", claudeStr)
	}
	if !strings.Contains(claudeStr, "[REDACTED]") {
		t.Errorf("ClaudeCredentials.String() does not contain [REDACTED]: %s", claudeStr)
	}

	codexAuth := &localstate.CodexAuth{
		AuthMode: "chatgpt",
		APIKey:   "sk-openai-secret-key",
		Tokens: &localstate.CodexTokens{
			AccessToken:  "codex-secret-access-token",
			RefreshToken: "codex-secret-refresh-token",
			IDToken:      "codex-secret-id-token",
			AccountID:    "acc-codex",
		},
	}

	codexStr := codexAuth.String()
	if strings.Contains(codexStr, "sk-openai-secret-key") || strings.Contains(codexStr, "codex-secret-access-token") {
		t.Errorf("CodexAuth.String() leaked secret: %s", codexStr)
	}

	tokensStr := codexAuth.Tokens.String()
	if strings.Contains(tokensStr, "codex-secret-access-token") || strings.Contains(tokensStr, "codex-secret-refresh-token") {
		t.Errorf("CodexTokens.String() leaked secret: %s", tokensStr)
	}
}

func TestReadOnlyGuarantee(t *testing.T) {
	// Verify that reading/resolving never mutates or creates files in vendor directories.
	tmpDir := t.TempDir()
	claudeDir := filepath.Join(tmpDir, ".claude")
	// Note: we intentionally do NOT create claudeDir on disk.

	r := localstate.New(
		localstate.WithHomeDir(tmpDir),
		localstate.WithKeychain(&mockKeychainReader{err: localstate.ErrKeychainUnsupported}),
	)

	_, err := r.ReadClaudeCredentials(context.Background())
	if !errors.Is(err, localstate.ErrCredentialNotFound) {
		t.Errorf("expected ErrCredentialNotFound, got %v", err)
	}

	if _, statErr := os.Stat(claudeDir); !os.IsNotExist(statErr) {
		t.Errorf("directory %s was created, violating read-only guarantee", claudeDir)
	}
}

func TestEnsureDir(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "nested", "dir")
	if err := localstate.EnsureDir(target); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("failed to stat created dir: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("expected %s to be a directory", target)
	}
}
