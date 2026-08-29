package claude_test

import (
	"context"
	"errors"
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

	if a.Name() != "claude" {
		t.Errorf("expected Name %q, got %q", "claude", a.Name())
	}

	sources := a.Sources()
	if len(sources) == 0 {
		t.Fatalf("expected non-empty source ladder")
	}

	primary := sources[0]
	if primary.ID() != dipstick.SourceOAuthAPI {
		t.Errorf("expected primary source ID %q, got %q", dipstick.SourceOAuthAPI, primary.ID())
	}
	if primary.Tier() != dipstick.TierAPI {
		t.Errorf("expected primary source tier %v, got %v", dipstick.TierAPI, primary.Tier())
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
