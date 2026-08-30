package dipstick_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mattwalters/dipstick"
)

type doctorFakeSource struct {
	id          dipstick.SourceID
	tier        dipstick.SourceTier
	available   bool
	fetchReport *dipstick.ProviderReport
	fetchErr    error
	fetchHook   func()
}

func (s *doctorFakeSource) ID() dipstick.SourceID     { return s.id }
func (s *doctorFakeSource) Tier() dipstick.SourceTier { return s.tier }
func (s *doctorFakeSource) Available(ctx context.Context) bool {
	return s.available
}
func (s *doctorFakeSource) Fetch(ctx context.Context) (*dipstick.ProviderReport, error) {
	if s.fetchHook != nil {
		s.fetchHook()
	}
	if s.fetchErr != nil {
		return nil, s.fetchErr
	}
	return s.fetchReport, nil
}

type doctorFakeAdapter struct {
	id        dipstick.ProviderID
	detection dipstick.Detection
	sources   []dipstick.Source
	compat    dipstick.Compat
}

func (a *doctorFakeAdapter) ID() dipstick.ProviderID { return a.id }
func (a *doctorFakeAdapter) Detect(ctx context.Context) (dipstick.Detection, error) {
	return a.detection, nil
}
func (a *doctorFakeAdapter) Sources() []dipstick.Source {
	return a.sources
}
func (a *doctorFakeAdapter) Compat() dipstick.Compat {
	return a.compat
}

func TestDoctor_FullyWorkingProvider(t *testing.T) {
	tier1Source := &doctorFakeSource{
		id:        dipstick.SourceOAuthAPI,
		tier:      dipstick.TierAPI,
		available: true,
		fetchReport: &dipstick.ProviderReport{
			Windows: []dipstick.RateWindow{
				{
					Label:       "session",
					UsedPercent: dipstick.Ptr(68.0),
				},
				{
					Label:       "weekly",
					UsedPercent: dipstick.Ptr(31.0),
				},
			},
		},
	}
	tier4Source := &doctorFakeSource{
		id:        dipstick.SourceTranscript,
		tier:      dipstick.TierTranscripts,
		available: true,
	}

	adapter := &doctorFakeAdapter{
		id: dipstick.ProviderClaude,
		detection: dipstick.Detection{
			Installed:     true,
			Authenticated: true,
			Version:       "2.1.246",
			BinaryPath:    "/usr/local/bin/claude",
		},
		sources: []dipstick.Source{tier1Source, tier4Source},
	}

	ctx := context.Background()
	rep, err := dipstick.Doctor(ctx, dipstick.WithAdapter(adapter), dipstick.WithProviders(dipstick.ProviderClaude))
	if err != nil {
		t.Fatalf("unexpected Doctor error: %v", err)
	}

	if len(rep.Providers) != 1 {
		t.Fatalf("expected 1 provider report, got %d", len(rep.Providers))
	}

	pr := rep.Providers[0]
	if pr.Provider != dipstick.ProviderClaude {
		t.Errorf("expected provider claude, got %s", pr.Provider)
	}
	if !pr.Installed {
		t.Errorf("expected installed true")
	}
	if pr.Version != "2.1.246" {
		t.Errorf("expected version 2.1.246, got %s", pr.Version)
	}
	if pr.CompatVerdict != dipstick.CompatVerified {
		t.Errorf("expected compat verdict verified, got %s", pr.CompatVerdict)
	}

	if len(pr.Sources) != 2 {
		t.Fatalf("expected 2 source reports, got %d", len(pr.Sources))
	}

	// Tier 1 succeeded
	if pr.Sources[0].Status != dipstick.AttemptStatusSuccess {
		t.Errorf("expected Tier 1 status success, got %s", pr.Sources[0].Status)
	}
	if pr.Sources[0].Summary != "ok (session 68%, weekly 31%)" {
		t.Errorf("expected Tier 1 summary 'ok (session 68%%, weekly 31%%)', got %q", pr.Sources[0].Summary)
	}

	// Tier 4 skipped because higher tier succeeded
	if pr.Sources[1].Status != dipstick.AttemptStatusSkipped {
		t.Errorf("expected Tier 4 status skipped, got %s", pr.Sources[1].Status)
	}
	if !strings.Contains(pr.Sources[1].Summary, "higher tier succeeded") {
		t.Errorf("expected Tier 4 summary to mention higher tier succeeded, got %q", pr.Sources[1].Summary)
	}

	var buf bytes.Buffer
	if err := rep.RenderText(&buf); err != nil {
		t.Fatalf("failed rendering text: %v", err)
	}
	text := buf.String()
	if !strings.Contains(text, "claude   2.1.246  ✓ verified range") {
		t.Errorf("expected header with verified range, got:\n%s", text)
	}
	if !strings.Contains(text, "oauth-api") || !strings.Contains(text, "ok (session 68%, weekly 31%)") {
		t.Errorf("expected Tier 1 output in text, got:\n%s", text)
	}
	if !strings.Contains(text, "transcripts") || !strings.Contains(text, "skipped — higher tier succeeded") {
		t.Errorf("expected Tier 4 output in text, got:\n%s", text)
	}
}

func TestDoctor_PartlyDegradedProvider(t *testing.T) {
	tier1Source := &doctorFakeSource{
		id:        dipstick.SourceOAuthAPI,
		tier:      dipstick.TierAPI,
		available: true,
		fetchErr:  fmt.Errorf("%w: auth_mode is API key", dipstick.ErrNotAuthenticated),
	}
	tier2Source := &doctorFakeSource{
		id:        dipstick.SourceLocalState,
		tier:      dipstick.TierLocalState,
		available: true,
		fetchReport: &dipstick.ProviderReport{
			Identity: &dipstick.Identity{
				Email: "dev@example.com",
			},
		},
	}
	tier3Source := &doctorFakeSource{
		id:        dipstick.SourceAppServer,
		tier:      dipstick.TierLocalRPC,
		available: true,
		fetchErr:  fmt.Errorf("%w: no usage method in protocol", dipstick.ErrNotSupported),
	}

	adapter := &doctorFakeAdapter{
		id: dipstick.ProviderCodex,
		detection: dipstick.Detection{
			Installed:     true,
			Authenticated: true,
			Version:       "0.155.0",
			BinaryPath:    "/usr/local/bin/codex",
		},
		sources: []dipstick.Source{tier1Source, tier2Source, tier3Source},
	}

	ctx := context.Background()
	rep, err := dipstick.Doctor(ctx, dipstick.WithAdapter(adapter), dipstick.WithProviders(dipstick.ProviderCodex))
	if err != nil {
		t.Fatalf("unexpected Doctor error: %v", err)
	}

	if len(rep.Providers) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(rep.Providers))
	}
	pr := rep.Providers[0]
	if pr.CompatVerdict != dipstick.CompatNewerThanVerified {
		t.Errorf("expected newer_than_verified, got %s", pr.CompatVerdict)
	}
	if pr.CompatRange != ">=0.148.0 <0.150.0" {
		t.Errorf("expected compat range '>=0.148.0 <0.150.0', got %q", pr.CompatRange)
	}

	if len(pr.Sources) != 3 {
		t.Fatalf("expected 3 sources, got %d", len(pr.Sources))
	}

	// Tier 1 error
	if pr.Sources[0].Status != dipstick.AttemptStatusError {
		t.Errorf("expected tier 1 error, got %s", pr.Sources[0].Status)
	}
	if pr.Sources[0].Reason != dipstick.ReasonNotAuthenticated {
		t.Errorf("expected tier 1 reason not_authenticated, got %s", pr.Sources[0].Reason)
	}
	if pr.Sources[0].NextStep != "run `codex auth login`" {
		t.Errorf("expected next step 'run `codex auth login`', got %q", pr.Sources[0].NextStep)
	}

	// Tier 2 success (identity only)
	if pr.Sources[1].Status != dipstick.AttemptStatusSuccess {
		t.Errorf("expected tier 2 success, got %s", pr.Sources[1].Status)
	}
	if pr.Sources[1].Summary != "ok (identity only)" {
		t.Errorf("expected tier 2 summary 'ok (identity only)', got %q", pr.Sources[1].Summary)
	}

	// Tier 3 skipped because higher tier (tier 2) succeeded
	if pr.Sources[2].Status != dipstick.AttemptStatusSkipped {
		t.Errorf("expected tier 3 skipped, got %s", pr.Sources[2].Status)
	}

	var buf bytes.Buffer
	if err := rep.RenderText(&buf); err != nil {
		t.Fatalf("failed rendering text: %v", err)
	}
	text := buf.String()
	if !strings.Contains(text, "codex    0.155.0  ⚠ newer than verified (>=0.148.0 <0.150.0)") {
		t.Errorf("expected warning header, got:\n%s", text)
	}
	if !strings.Contains(text, "usage-api") || !strings.Contains(text, "not_authenticated — auth_mode is API key") {
		t.Errorf("expected tier 1 auth error in text, got:\n%s", text)
	}
	if !strings.Contains(text, "auth-json") || !strings.Contains(text, "ok (identity only)") {
		t.Errorf("expected tier 2 identity only in text, got:\n%s", text)
	}
}

func TestDoctor_AbsentProvider(t *testing.T) {
	adapter := &doctorFakeAdapter{
		id: dipstick.ProviderAntigravity,
		detection: dipstick.Detection{
			Installed: false,
		},
		sources: nil,
	}

	ctx := context.Background()
	rep, err := dipstick.Doctor(ctx, dipstick.WithAdapter(adapter), dipstick.WithProviders(dipstick.ProviderAntigravity))
	if err != nil {
		t.Fatalf("unexpected Doctor error: %v", err)
	}

	if len(rep.Providers) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(rep.Providers))
	}
	pr := rep.Providers[0]
	if pr.Installed {
		t.Errorf("expected installed false")
	}
	if pr.CompatVerdict != dipstick.CompatNotInstalled {
		t.Errorf("expected not_installed verdict, got %s", pr.CompatVerdict)
	}

	var buf bytes.Buffer
	if err := rep.RenderText(&buf); err != nil {
		t.Fatalf("failed rendering text: %v", err)
	}
	text := buf.String()
	if !strings.Contains(text, "antigravity  —  ✗ not_installed") {
		t.Errorf("expected absent provider text, got:\n%s", text)
	}
}

func TestDoctor_ConfigDirEnvOverrideDetection(t *testing.T) {
	origEnv := os.Getenv("CLAUDE_CONFIG_DIR")
	defer func() {
		if origEnv != "" {
			_ = os.Setenv("CLAUDE_CONFIG_DIR", origEnv)
		} else {
			_ = os.Unsetenv("CLAUDE_CONFIG_DIR")
		}
	}()

	customDir := "/tmp/custom-claude-dir"
	_ = os.Setenv("CLAUDE_CONFIG_DIR", customDir)

	rep, err := dipstick.Doctor(context.Background(), dipstick.WithProviders(dipstick.ProviderClaude))
	if err != nil {
		t.Fatalf("Doctor error: %v", err)
	}

	if len(rep.Providers) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(rep.Providers))
	}

	cfgInfo := rep.Providers[0].ConfigDir
	if !cfgInfo.Overridden {
		t.Errorf("expected config dir to be marked overridden")
	}
	if cfgInfo.EnvVar != "CLAUDE_CONFIG_DIR" {
		t.Errorf("expected env var CLAUDE_CONFIG_DIR, got %s", cfgInfo.EnvVar)
	}
	if cfgInfo.Path != customDir {
		t.Errorf("expected path %s, got %s", customDir, cfgInfo.Path)
	}
}

func TestDoctor_CredentialLeakageGuard(t *testing.T) {
	// Assert strictly zero secret tokens, hashes, lengths, or substrings are printed in DoctorReport text or JSON
	mockSecretToken := "sk-ant-api03-VERY-SECRET-TOKEN-12345-DO-NOT-LEAK"
	mockAPIKey := "sk-openai-SECRET-KEY-ABCDEF-987654"

	tier1Source := &doctorFakeSource{
		id:        dipstick.SourceOAuthAPI,
		tier:      dipstick.TierAPI,
		available: true,
		fetchErr:  fmt.Errorf("failed calling upstream with Authorization Bearer %s and key %s", mockSecretToken, mockAPIKey),
	}

	adapter := &doctorFakeAdapter{
		id: dipstick.ProviderClaude,
		detection: dipstick.Detection{
			Installed:  true,
			Version:    "2.1.246",
			BinaryPath: "/usr/bin/claude",
		},
		sources: []dipstick.Source{tier1Source},
	}

	rep, err := dipstick.Doctor(context.Background(), dipstick.WithAdapter(adapter), dipstick.WithProviders(dipstick.ProviderClaude))
	if err != nil {
		t.Fatalf("Doctor error: %v", err)
	}

	// 1. Check JSON serialization
	jsonData, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		t.Fatalf("json marshal error: %v", err)
	}
	jsonStr := string(jsonData)

	if strings.Contains(jsonStr, mockSecretToken) {
		t.Errorf("CRITICAL: DoctorReport JSON leaked secret token:\n%s", jsonStr)
	}
	if strings.Contains(jsonStr, mockAPIKey) {
		t.Errorf("CRITICAL: DoctorReport JSON leaked API key:\n%s", jsonStr)
	}
	if strings.Contains(jsonStr, "VERY-SECRET") || strings.Contains(jsonStr, "ABCDEF-987654") {
		t.Errorf("CRITICAL: DoctorReport JSON leaked secret substrings:\n%s", jsonStr)
	}

	// 2. Check Text output
	var buf bytes.Buffer
	if err := rep.RenderText(&buf); err != nil {
		t.Fatalf("render text error: %v", err)
	}
	textStr := buf.String()

	if strings.Contains(textStr, mockSecretToken) {
		t.Errorf("CRITICAL: DoctorReport Text leaked secret token:\n%s", textStr)
	}
	if strings.Contains(textStr, mockAPIKey) {
		t.Errorf("CRITICAL: DoctorReport Text leaked API key:\n%s", textStr)
	}
	if strings.Contains(textStr, "VERY-SECRET") || strings.Contains(textStr, "ABCDEF-987654") {
		t.Errorf("CRITICAL: DoctorReport Text leaked secret substrings:\n%s", textStr)
	}
}

func TestDoctor_CompatVerdicts(t *testing.T) {
	tests := []struct {
		provider    dipstick.ProviderID
		version     string
		wantVerdict dipstick.CompatVerdict
		wantRange   string
	}{
		{
			provider:    dipstick.ProviderClaude,
			version:     "2.1.246",
			wantVerdict: dipstick.CompatVerified,
			wantRange:   ">=2.1.0 <2.2.0",
		},
		{
			provider:    dipstick.ProviderClaude,
			version:     "1.9.0",
			wantVerdict: dipstick.CompatOlderThanFloor,
			wantRange:   "<2.1.0",
		},
		{
			provider:    dipstick.ProviderClaude,
			version:     "2.3.0",
			wantVerdict: dipstick.CompatNewerThanVerified,
			wantRange:   ">=2.1.0 <2.2.0",
		},
		{
			provider:    dipstick.ProviderCodex,
			version:     "0.149.0",
			wantVerdict: dipstick.CompatVerified,
			wantRange:   ">=0.148.0 <0.150.0",
		},
		{
			provider:    dipstick.ProviderCodex,
			version:     "0.155.0",
			wantVerdict: dipstick.CompatNewerThanVerified,
			wantRange:   ">=0.148.0 <0.150.0",
		},
		{
			provider:    dipstick.ProviderCodex,
			version:     "0.135.0",
			wantVerdict: dipstick.CompatOlderThanFloor,
			wantRange:   "<0.148.0",
		},
		{
			provider:    dipstick.ProviderOpenCode,
			version:     "1.18.5",
			wantVerdict: dipstick.CompatVerified,
			wantRange:   ">=1.18.0",
		},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s_%s", tt.provider, tt.version), func(t *testing.T) {
			adapter := &doctorFakeAdapter{
				id: tt.provider,
				detection: dipstick.Detection{
					Installed:  true,
					Version:    tt.version,
					BinaryPath: "/bin/" + string(tt.provider),
				},
			}
			rep, err := dipstick.Doctor(context.Background(), dipstick.WithAdapter(adapter), dipstick.WithProviders(tt.provider))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(rep.Providers) != 1 {
				t.Fatalf("expected 1 provider, got %d", len(rep.Providers))
			}
			pr := rep.Providers[0]
			if pr.CompatVerdict != tt.wantVerdict {
				t.Errorf("version %s: expected verdict %s, got %s", tt.version, tt.wantVerdict, pr.CompatVerdict)
			}
		})
	}
}

func TestDoctor_SourcePolicySkipping(t *testing.T) {
	tier1Source := &doctorFakeSource{
		id:        dipstick.SourceOAuthAPI,
		tier:      dipstick.TierAPI,
		available: true,
	}
	tier2Source := &doctorFakeSource{
		id:        dipstick.SourceLocalState,
		tier:      dipstick.TierLocalState,
		available: true,
		fetchReport: &dipstick.ProviderReport{
			Identity: &dipstick.Identity{Email: "test@example.com"},
		},
	}

	adapter := &doctorFakeAdapter{
		id: dipstick.ProviderClaude,
		detection: dipstick.Detection{
			Installed:  true,
			Version:    "2.1.246",
			BinaryPath: "/bin/claude",
		},
		sources: []dipstick.Source{tier1Source, tier2Source},
	}

	rep, err := dipstick.Doctor(context.Background(),
		dipstick.WithAdapter(adapter),
		dipstick.WithProviders(dipstick.ProviderClaude),
		dipstick.WithSourcePolicy(dipstick.SourcePolicyLocal),
	)
	if err != nil {
		t.Fatalf("Doctor error: %v", err)
	}

	pr := rep.Providers[0]
	if len(pr.Sources) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(pr.Sources))
	}
	if pr.Sources[0].Status != dipstick.AttemptStatusSkipped {
		t.Errorf("expected tier 1 skipped by policy, got %s", pr.Sources[0].Status)
	}
	if pr.Sources[0].Summary != "skipped by source policy" {
		t.Errorf("expected summary 'skipped by source policy', got %q", pr.Sources[0].Summary)
	}
	if pr.Sources[1].Status != dipstick.AttemptStatusSuccess {
		t.Errorf("expected tier 2 success, got %s", pr.Sources[1].Status)
	}
}

func TestDoctor_InvalidOptionsAndCancellation(t *testing.T) {
	t.Run("negative timeout", func(t *testing.T) {
		_, err := dipstick.Doctor(context.Background(), dipstick.WithTimeout(-1*time.Second))
		if err == nil {
			t.Errorf("expected error for negative timeout")
		}
	})

	t.Run("negative source timeout", func(t *testing.T) {
		_, err := dipstick.Doctor(context.Background(), dipstick.WithSourceTimeout(-1*time.Second))
		if err == nil {
			t.Errorf("expected error for negative source timeout")
		}
	})

	t.Run("cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := dipstick.Doctor(ctx)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	})

	t.Run("mid-run cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		source1 := &doctorFakeSource{
			id:        dipstick.SourceOAuthAPI,
			tier:      dipstick.TierAPI,
			available: true,
			fetchHook: func() {
				cancel()
			},
			fetchReport: &dipstick.ProviderReport{
				Identity: &dipstick.Identity{Email: "test@example.com"},
			},
		}
		adapter := &doctorFakeAdapter{
			id: dipstick.ProviderClaude,
			detection: dipstick.Detection{
				Installed: true,
				Version:   "2.1.246",
			},
			sources: []dipstick.Source{source1},
		}

		_, err := dipstick.Doctor(ctx, dipstick.WithAdapter(adapter), dipstick.WithProviders(dipstick.ProviderClaude, dipstick.ProviderCodex, dipstick.ProviderOpenCode))
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled on mid-run context cancellation, got %v", err)
		}
	})

	t.Run("unknown provider", func(t *testing.T) {
		_, err := dipstick.Doctor(context.Background(), dipstick.WithProviders(dipstick.ProviderID("invalid-provider")))
		if err == nil {
			t.Errorf("expected error for unknown provider")
		}
	})
}
