package claude_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/mattwalters/dipstick"
	"github.com/mattwalters/dipstick/internal/adapters/claude"
	"github.com/mattwalters/dipstick/internal/localstate"
)

func TestAdapter_Interface(t *testing.T) {
	a := claude.New()
	if a == nil {
		t.Fatalf("expected non-nil adapter")
	}

	if a.ID() != dipstick.ProviderClaude {
		t.Errorf("expected ID %q, got %q", dipstick.ProviderClaude, a.ID())
	}

	sources := a.Sources()
	if len(sources) != 2 {
		t.Fatalf("expected 2 sources in ladder, got %d", len(sources))
	}

	primary := sources[0]
	if primary.ID() != dipstick.SourceOAuthAPI {
		t.Errorf("expected primary source ID %q, got %q", dipstick.SourceOAuthAPI, primary.ID())
	}
	if primary.Tier() != dipstick.TierAPI {
		t.Errorf("expected primary source tier %v, got %v", dipstick.TierAPI, primary.Tier())
	}

	secondary := sources[1]
	if secondary.ID() != dipstick.SourceTranscript {
		t.Errorf("expected secondary source ID %q, got %q", dipstick.SourceTranscript, secondary.ID())
	}
	if secondary.Tier() != dipstick.TierTranscripts {
		t.Errorf("expected secondary source tier %v, got %v", dipstick.TierTranscripts, secondary.Tier())
	}
}

func TestAdapter_Detect(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	future := now.Add(24 * time.Hour)
	past := now.Add(-1 * time.Hour)

	t.Run("authenticated with valid credentials", func(t *testing.T) {
		a := claude.New(
			claude.WithAdapterNow(func() time.Time { return now }),
			claude.WithAdapterCredentialResolver(func(ctx context.Context) (*localstate.ClaudeCredentials, error) {
				return &localstate.ClaudeCredentials{
					AccessToken: "sk-ant-test-token",
					ExpiresAt:   &future,
				}, nil
			}),
		)

		detection, err := a.Detect(ctx)
		if err != nil {
			t.Fatalf("Detect failed: %v", err)
		}
		if !detection.Authenticated {
			t.Errorf("expected Authenticated = true")
		}
	})

	t.Run("unauthenticated when credentials expired", func(t *testing.T) {
		a := claude.New(
			claude.WithAdapterNow(func() time.Time { return now }),
			claude.WithAdapterCredentialResolver(func(ctx context.Context) (*localstate.ClaudeCredentials, error) {
				return &localstate.ClaudeCredentials{
					AccessToken: "sk-ant-test-token",
					ExpiresAt:   &past,
				}, localstate.ErrCredentialExpired
			}),
		)

		detection, err := a.Detect(ctx)
		if err != nil {
			t.Fatalf("Detect failed: %v", err)
		}
		if detection.Authenticated {
			t.Errorf("expected Authenticated = false for expired credentials")
		}
	})

	t.Run("unauthenticated when credentials not found", func(t *testing.T) {
		a := claude.New(
			claude.WithAdapterNow(func() time.Time { return now }),
			claude.WithAdapterCredentialResolver(func(ctx context.Context) (*localstate.ClaudeCredentials, error) {
				return nil, localstate.ErrCredentialNotFound
			}),
		)

		detection, err := a.Detect(ctx)
		if err != nil {
			t.Fatalf("Detect failed: %v", err)
		}
		if detection.Authenticated {
			t.Errorf("expected Authenticated = false when credentials not found")
		}
	})

	t.Run("detect respects context cancellation", func(t *testing.T) {
		cancelCtx, cancel := context.WithCancel(ctx)
		cancel()

		a := claude.New()
		_, err := a.Detect(cancelCtx)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled error, got %v", err)
		}
	})
}

func TestAdapter_CollectLadderFallback(t *testing.T) {
	ctx := context.Background()
	fixturesDir := filepath.Join("testdata", "transcripts")

	// Adapter where OAuth is unauthenticated -> fallback to TranscriptSource
	a := claude.New(
		claude.WithAdapterCredentialResolver(func(ctx context.Context) (*localstate.ClaudeCredentials, error) {
			return nil, localstate.ErrCredentialNotFound
		}),
		claude.WithAdapterProjectsDir(fixturesDir),
	)

	rep, err := dipstick.Collect(ctx, dipstick.WithAdapter(a))
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	if len(rep.Providers) != 1 {
		t.Fatalf("expected 1 provider report, got %d", len(rep.Providers))
	}

	p := rep.Providers[0]
	if p.Provider != dipstick.ProviderClaude {
		t.Errorf("expected Provider %q, got %q", dipstick.ProviderClaude, p.Provider)
	}
	if p.Source != dipstick.SourceTranscript {
		t.Errorf("expected Source %q, got %q", dipstick.SourceTranscript, p.Source)
	}
	if p.Confidence != dipstick.ConfidenceDerived {
		t.Errorf("expected Confidence %q, got %q", dipstick.ConfidenceDerived, p.Confidence)
	}
	if len(p.Windows) != 0 {
		t.Errorf("expected empty Windows from transcript fallback, got %d", len(p.Windows))
	}
	if p.Tokens == nil || *p.Tokens.TotalTokens != 1795 {
		t.Errorf("expected TotalTokens = 1795, got %v", p.Tokens)
	}
}

func TestAdapter_CollectOfflinePolicy(t *testing.T) {
	ctx := context.Background()
	fixturesDir := filepath.Join("testdata", "transcripts")
	future := time.Now().Add(24 * time.Hour)

	// Even with valid OAuth credentials available, --policy offline should bypass OAuth and use TranscriptSource
	a := claude.New(
		claude.WithAdapterCredentialResolver(func(ctx context.Context) (*localstate.ClaudeCredentials, error) {
			return &localstate.ClaudeCredentials{
				AccessToken: "sk-ant-test-token",
				ExpiresAt:   &future,
			}, nil
		}),
		claude.WithAdapterProjectsDir(fixturesDir),
	)

	rep, err := dipstick.Collect(ctx,
		dipstick.WithAdapter(a),
		dipstick.WithSourcePolicy(dipstick.SourcePolicyOffline),
	)
	if err != nil {
		t.Fatalf("Collect failed with offline policy: %v", err)
	}

	if len(rep.Providers) != 1 {
		t.Fatalf("expected 1 provider report, got %d", len(rep.Providers))
	}

	p := rep.Providers[0]
	if p.Source != dipstick.SourceTranscript {
		t.Errorf("expected Source %q under offline policy, got %q", dipstick.SourceTranscript, p.Source)
	}
	if p.Confidence != dipstick.ConfidenceDerived {
		t.Errorf("expected Confidence %q, got %q", dipstick.ConfidenceDerived, p.Confidence)
	}
}
