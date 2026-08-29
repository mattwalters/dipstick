package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/mattwalters/dipstick/internal/adapters/claude"
	"github.com/mattwalters/dipstick/internal/cliexec"
	"github.com/mattwalters/dipstick/internal/localstate"
	"github.com/mattwalters/dipstick/internal/scrub"
	"github.com/mattwalters/dipstick/internal/types"
)

type Manifest struct {
	Provider      string   `json:"provider"`
	VendorVersion string   `json:"vendor_version"`
	CapturedAt    string   `json:"captured_at"`
	OS            string   `json:"os"`
	Arch          string   `json:"arch"`
	Sources       []string `json:"sources"`
}

func main() {
	outDir := flag.String("out", filepath.Join("testdata", "fixtures"), "Output directory for captured fixtures")
	providerFlag := flag.String("provider", "all", "Provider to capture (all, claude, codex, opencode, antigravity)")
	dryRun := flag.Bool("dry-run", false, "Simulate capture and print actions without writing files")
	verbose := flag.Bool("v", false, "Verbose logging")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fmt.Println("==> Dipstick Fixture Capture Harness")
	fmt.Printf("    Destination: %s\n", *outDir)
	fmt.Printf("    Provider:    %s\n", *providerFlag)
	fmt.Printf("    Platform:    %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Printf("    Dry-run:     %v\n\n", *dryRun)

	target := strings.ToLower(strings.TrimSpace(*providerFlag))
	capturedAny := false

	if target == "all" || target == "claude" {
		captured, err := captureClaude(ctx, *outDir, *dryRun, *verbose)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error capturing Claude fixtures: %v\n", err)
		}
		if captured {
			capturedAny = true
		}
	}

	if target == "all" || target == "codex" {
		captured, err := captureCodex(ctx, *outDir, *dryRun, *verbose)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error capturing Codex fixtures: %v\n", err)
		}
		if captured {
			capturedAny = true
		}
	}

	if target == "all" || target == "opencode" {
		captureOpenCode(ctx, *outDir, *dryRun, *verbose)
	}

	if target == "all" || target == "antigravity" {
		captureAntigravity(ctx, *outDir, *dryRun, *verbose)
	}

	fmt.Println("\n==> Validating committed fixture tree for unredacted secrets...")
	findings, err := validateFixtureDirectory(*outDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning validating fixture tree: %v\n", err)
	} else if len(findings) > 0 {
		fmt.Fprintf(os.Stderr, "FATAL: Detected %d unredacted credential or PII leaks in fixtures!\n", len(findings))
		for _, f := range findings {
			fmt.Fprintf(os.Stderr, "  - [%s] %s (matched: %s)\n", f.Rule, f.Message, f.Match)
		}
		os.Exit(1)
	} else {
		fmt.Println("    [OK] All fixtures passed secret scanning validation.")
	}

	if !capturedAny {
		fmt.Println("\nNote: No live credentials were available on host to capture fresh payloads.")
		fmt.Println("Existing committed fixtures remain intact and tested.")
	} else {
		fmt.Println("\nCapture run completed successfully.")
	}
}

func captureClaude(ctx context.Context, outDir string, dryRun bool, verbose bool) (bool, error) {
	fmt.Println("--> Inspecting Claude (Tier 1 OAuth API)...")

	creds, err := localstate.ReadClaudeCredentials(ctx)
	if err != nil || creds == nil || creds.AccessToken == "" {
		fmt.Println("    Claude: No active credentials found in keychain or credentials file; skipping live capture.")
		return false, nil
	}
	if creds.IsExpired(time.Now()) {
		fmt.Println("    Claude: Credentials have expired; skipping live capture.")
		return false, nil
	}

	fmt.Println("    Claude: Active credentials detected. Fetching live OAuth usage payload...")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.anthropic.com/api/oauth/usage", nil)
	if err != nil {
		return false, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+creds.AccessToken)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "dipstick-capture/1.0")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("http request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return false, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("    Claude: Upstream usage endpoint returned HTTP %d: %s; skipping capture.\n", resp.StatusCode, scrub.Scrub(string(bodyBytes)))
		return false, nil
	}

	// Sanitize and format JSON
	scrubbedBody := scrub.Scrub(string(bodyBytes))
	var rawJSON any
	if err := json.Unmarshal([]byte(scrubbedBody), &rawJSON); err != nil {
		if err := json.Unmarshal(bodyBytes, &rawJSON); err != nil {
			return false, fmt.Errorf("parsing response JSON: %w", err)
		}
	}

	formattedJSON, err := json.MarshalIndent(rawJSON, "", "  ")
	if err != nil {
		return false, fmt.Errorf("formatting JSON: %w", err)
	}

	// Probe version
	version := "2.1.246"
	runner := cliexec.New()
	if v, err := runner.ProbeVersion(ctx, "claude"); err == nil && v != "" {
		version = normalizeVersion(v)
	}

	findings := scrub.FindSecrets(string(formattedJSON))
	if len(findings) > 0 {
		return false, fmt.Errorf("sanitization failed: payload contains unredacted secrets")
	}

	windows, err := claude.ParseOAuthUsageResponse(bodyBytes)
	if err != nil {
		return false, fmt.Errorf("parsing rate windows: %w", err)
	}

	verDir := filepath.Join(outDir, "claude", "v"+version)
	payloadFile := filepath.Join(verDir, "oauth_api.json")
	manifestFile := filepath.Join(verDir, "manifest.json")
	goldenFile := filepath.Join(verDir, "golden_report.json")

	now := time.Now().UTC()
	manifest := Manifest{
		Provider:      "claude",
		VendorVersion: version,
		CapturedAt:    now.Format(time.RFC3339),
		OS:            runtime.GOOS,
		Arch:          runtime.GOARCH,
		Sources:       []string{"oauth_api.json"},
	}
	manifestBytes, _ := json.MarshalIndent(manifest, "", "  ")

	goldenReport := types.ProviderReport{
		Provider:   types.ProviderClaude,
		Source:     types.SourceOAuthAPI,
		Confidence: types.ConfidenceExact,
		CLIVersion: version,
		Identity: &types.Identity{
			Email:     "developer@example.com",
			AccountID: "acc-claude-test",
			Plan:      "pro",
		},
		Windows:    windows,
		ObservedAt: now,
	}
	goldenBytes, _ := json.MarshalIndent(goldenReport, "", "  ")

	if dryRun {
		fmt.Printf("    [Dry-Run] Would write %s, %s, and %s\n", payloadFile, manifestFile, goldenFile)
		return true, nil
	}

	if err := os.MkdirAll(verDir, 0o755); err != nil {
		return false, fmt.Errorf("creating dir %s: %w", verDir, err)
	}
	if err := os.WriteFile(payloadFile, append(formattedJSON, '\n'), 0o644); err != nil {
		return false, fmt.Errorf("writing %s: %w", payloadFile, err)
	}
	if err := os.WriteFile(manifestFile, append(manifestBytes, '\n'), 0o644); err != nil {
		return false, fmt.Errorf("writing %s: %w", manifestFile, err)
	}
	if err := os.WriteFile(goldenFile, append(goldenBytes, '\n'), 0o644); err != nil {
		return false, fmt.Errorf("writing %s: %w", goldenFile, err)
	}

	fmt.Printf("    Claude: Successfully captured and sanitized fixture to %s\n", verDir)
	return true, nil
}

func captureCodex(ctx context.Context, outDir string, dryRun bool, verbose bool) (bool, error) {
	fmt.Println("--> Inspecting Codex (Tier 2 Local State)...")

	resolver := localstate.New()
	paths, err := resolver.CodexPaths()
	if err != nil {
		fmt.Printf("    Codex: Resolving paths failed: %v\n", err)
		return false, nil
	}

	data, err := os.ReadFile(paths.AuthFile)
	if err != nil {
		fmt.Printf("    Codex: %s not found; skipping live capture.\n", paths.AuthFile)
		return false, nil
	}

	var root struct {
		AuthMode    string `json:"auth_mode"`
		APIKey      string `json:"OPENAI_API_KEY"`
		AltAPIKey   string `json:"openai_api_key"`
		LastRefresh any    `json:"last_refresh"`
		Tokens      *struct {
			IDToken      string `json:"id_token"`
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			AccountID    string `json:"account_id"`
		} `json:"tokens"`
	}

	if err := json.Unmarshal(data, &root); err != nil {
		return false, fmt.Errorf("parsing auth.json: %w", err)
	}

	version := "0.1.0"
	runner := cliexec.New()
	if v, err := runner.ProbeVersion(ctx, "codex"); err == nil && v != "" {
		version = normalizeVersion(v)
	}

	now := time.Now().UTC()
	var sanitizedAuth map[string]any
	var goldenReport types.ProviderReport

	if root.Tokens != nil && strings.TrimSpace(root.Tokens.IDToken) != "" {
		// ChatGPT mode: synthesize sanitized JWT
		mockHeader := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
		mockPayload := base64.RawURLEncoding.EncodeToString([]byte(`{"email":"developer@example.com","https://api.openai.com/auth":{"chatgpt_account_id":"acc-chatgpt-12345","chatgpt_plan_type":"pro","chatgpt_user_id":"user-12345"}}`))
		mockJWT := fmt.Sprintf("%s.%s.dummy_jwt_signature", mockHeader, mockPayload)

		sanitizedAuth = map[string]any{
			"auth_mode": "chatgpt",
			"tokens": map[string]any{
				"id_token":      mockJWT,
				"access_token":  "mock-access-token",
				"refresh_token": "mock-refresh-token",
				"account_id":    "acc-chatgpt-12345",
			},
		}

		goldenReport = types.ProviderReport{
			Provider:   types.ProviderCodex,
			Source:     types.SourceLocalState,
			Confidence: types.ConfidenceDerived,
			Identity: &types.Identity{
				Email:     "developer@example.com",
				AccountID: "acc-chatgpt-12345",
				Plan:      "pro",
			},
			ObservedAt: now,
		}
	} else {
		// API Key mode
		sanitizedAuth = map[string]any{
			"auth_mode":      "api_key",
			"OPENAI_API_KEY": "sk-mock-key-0000000000000000",
			"tokens": map[string]any{
				"account_id": "acc-chatgpt-12345",
			},
		}

		goldenReport = types.ProviderReport{
			Provider:   types.ProviderCodex,
			Source:     types.SourceLocalState,
			Confidence: types.ConfidenceDerived,
			Identity: &types.Identity{
				AccountID: "acc-chatgpt-12345",
				Plan:      "api_key",
			},
			ObservedAt: now,
		}
	}

	sanitizedBytes, _ := json.MarshalIndent(sanitizedAuth, "", "  ")
	findings := scrub.FindSecrets(string(sanitizedBytes))
	if len(findings) > 0 {
		return false, fmt.Errorf("sanitization failed for Codex: unredacted secrets detected")
	}

	verDir := filepath.Join(outDir, "codex", "v"+version)
	payloadFile := filepath.Join(verDir, "local_state.json")
	manifestFile := filepath.Join(verDir, "manifest.json")
	goldenFile := filepath.Join(verDir, "golden_report.json")

	manifest := Manifest{
		Provider:      "codex",
		VendorVersion: version,
		CapturedAt:    now.Format(time.RFC3339),
		OS:            runtime.GOOS,
		Arch:          runtime.GOARCH,
		Sources:       []string{"local_state.json"},
	}
	manifestBytes, _ := json.MarshalIndent(manifest, "", "  ")
	goldenBytes, _ := json.MarshalIndent(goldenReport, "", "  ")

	if dryRun {
		fmt.Printf("    [Dry-Run] Would write %s, %s, and %s\n", payloadFile, manifestFile, goldenFile)
		return true, nil
	}

	if err := os.MkdirAll(verDir, 0o755); err != nil {
		return false, fmt.Errorf("creating dir %s: %w", verDir, err)
	}
	if err := os.WriteFile(payloadFile, append(sanitizedBytes, '\n'), 0o644); err != nil {
		return false, fmt.Errorf("writing %s: %w", payloadFile, err)
	}
	if err := os.WriteFile(manifestFile, append(manifestBytes, '\n'), 0o644); err != nil {
		return false, fmt.Errorf("writing %s: %w", manifestFile, err)
	}
	if err := os.WriteFile(goldenFile, append(goldenBytes, '\n'), 0o644); err != nil {
		return false, fmt.Errorf("writing %s: %w", goldenFile, err)
	}

	fmt.Printf("    Codex: Successfully captured and sanitized fixture to %s\n", verDir)
	return true, nil
}

func captureOpenCode(ctx context.Context, outDir string, dryRun bool, verbose bool) {
	fmt.Println("--> Inspecting OpenCode...")
	fmt.Println("    OpenCode: No active collection ladder declared in v0.1; skipping.")
}

func captureAntigravity(ctx context.Context, outDir string, dryRun bool, verbose bool) {
	fmt.Println("--> Inspecting Antigravity...")
	fmt.Println("    Antigravity: Exposes no standalone CLI usage reporting in v0.1 (unsupported).")
}

func validateFixtureDirectory(root string) ([]scrub.SecretFinding, error) {
	var allFindings []scrub.SecretFinding
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".json" && ext != ".txt" && ext != ".yaml" && ext != ".yml" {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		findings := scrub.FindSecrets(string(data))
		allFindings = append(allFindings, findings...)
		return nil
	})
	return allFindings, err
}

func normalizeVersion(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "0.1.0"
	}
	fields := strings.Fields(raw)
	if len(fields) > 0 {
		raw = fields[len(fields)-1]
	}
	raw = strings.TrimPrefix(raw, "v")
	if raw == "" {
		return "0.1.0"
	}
	return raw
}
