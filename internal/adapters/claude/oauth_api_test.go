package claude_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mattwalters/dipstick"
	"github.com/mattwalters/dipstick/internal/adapters/claude"
	"github.com/mattwalters/dipstick/internal/localstate"
)

func TestOAuthAPISource_Available(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	future := now.Add(24 * time.Hour)
	past := now.Add(-1 * time.Hour)

	t.Run("returns true when valid unexpired token present", func(t *testing.T) {
		src := claude.NewOAuthAPISource(
			claude.WithNow(func() time.Time { return now }),
			claude.WithCredentialResolver(func(ctx context.Context) (*localstate.ClaudeCredentials, error) {
				return &localstate.ClaudeCredentials{
					AccessToken: "sk-ant-test-token",
					ExpiresAt:   &future,
				}, nil
			}),
		)

		if !src.Available(ctx) {
			t.Errorf("expected Available() = true")
		}
	})

	t.Run("returns true when credentials expired to allow reporting credential_expired in Fetch", func(t *testing.T) {
		src := claude.NewOAuthAPISource(
			claude.WithNow(func() time.Time { return now }),
			claude.WithCredentialResolver(func(ctx context.Context) (*localstate.ClaudeCredentials, error) {
				return &localstate.ClaudeCredentials{
					AccessToken: "sk-ant-test-token",
					ExpiresAt:   &past,
				}, localstate.ErrCredentialExpired
			}),
		)

		if !src.Available(ctx) {
			t.Errorf("expected Available() = true for expired credentials")
		}
	})

	t.Run("returns false when token empty", func(t *testing.T) {
		src := claude.NewOAuthAPISource(
			claude.WithNow(func() time.Time { return now }),
			claude.WithCredentialResolver(func(ctx context.Context) (*localstate.ClaudeCredentials, error) {
				return &localstate.ClaudeCredentials{
					AccessToken: "",
				}, nil
			}),
		)

		if src.Available(ctx) {
			t.Errorf("expected Available() = false for empty token")
		}
	})

	t.Run("returns false when credentials not found", func(t *testing.T) {
		src := claude.NewOAuthAPISource(
			claude.WithNow(func() time.Time { return now }),
			claude.WithCredentialResolver(func(ctx context.Context) (*localstate.ClaudeCredentials, error) {
				return nil, localstate.ErrCredentialNotFound
			}),
		)

		if src.Available(ctx) {
			t.Errorf("expected Available() = false when credentials not found")
		}
	})
}

func TestOAuthAPISource_Fetch_Success(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	future := now.Add(24 * time.Hour)

	var authHeader, betaHeader, acceptHeader string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/oauth/usage" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		authHeader = r.Header.Get("Authorization")
		betaHeader = r.Header.Get("anthropic-beta")
		acceptHeader = r.Header.Get("Accept")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"five_hour": {
				"utilization": 14.5,
				"resets_at": "2026-08-29T17:00:00Z"
			},
			"seven_day": {
				"utilization": 62.0,
				"resets_at": "2026-09-05T12:00:00Z"
			}
		}`))
	}))
	defer server.Close()

	src := claude.NewOAuthAPISource(
		claude.WithBaseURL(server.URL),
		claude.WithHTTPClient(server.Client()),
		claude.WithNow(func() time.Time { return now }),
		claude.WithVersionProbe(func(ctx context.Context) (string, error) {
			return "2.1.246", nil
		}),
		claude.WithCredentialResolver(func(ctx context.Context) (*localstate.ClaudeCredentials, error) {
			return &localstate.ClaudeCredentials{
				AccessToken:  "sk-ant-test-token-value",
				AccountID:    "acc-12345",
				Email:        "user@example.com",
				Subscription: "pro",
				ExpiresAt:    &future,
			}, nil
		}),
	)

	report, err := src.Fetch(ctx)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if authHeader != "Bearer sk-ant-test-token-value" {
		t.Errorf("Authorization header: got %q", authHeader)
	}
	if betaHeader != "oauth-2025-04-20" {
		t.Errorf("anthropic-beta header: got %q", betaHeader)
	}
	if acceptHeader != "application/json" {
		t.Errorf("Accept header: got %q", acceptHeader)
	}

	if report.Provider != dipstick.ProviderClaude {
		t.Errorf("Provider: expected %q, got %q", dipstick.ProviderClaude, report.Provider)
	}
	if report.Source != dipstick.SourceOAuthAPI {
		t.Errorf("Source: expected %q, got %q", dipstick.SourceOAuthAPI, report.Source)
	}
	if report.Confidence != dipstick.ConfidenceExact {
		t.Errorf("Confidence: expected %q, got %q", dipstick.ConfidenceExact, report.Confidence)
	}
	if report.CLIVersion != "2.1.246" {
		t.Errorf("CLIVersion: expected 2.1.246, got %q", report.CLIVersion)
	}

	if report.Identity == nil {
		t.Fatalf("expected non-nil Identity")
	}
	if report.Identity.Email != "user@example.com" {
		t.Errorf("Identity.Email: expected user@example.com, got %q", report.Identity.Email)
	}
	if report.Identity.AccountID != "acc-12345" {
		t.Errorf("Identity.AccountID: expected acc-12345, got %q", report.Identity.AccountID)
	}
	if report.Identity.Plan != "pro" {
		t.Errorf("Identity.Plan: expected pro, got %q", report.Identity.Plan)
	}

	if len(report.Windows) != 2 {
		t.Fatalf("expected 2 windows, got %d", len(report.Windows))
	}

	sessionWin := report.Windows[0]
	if sessionWin.Label != "session" {
		t.Errorf("window[0] label: expected session, got %q", sessionWin.Label)
	}
	if sessionWin.UsedPercent == nil || *sessionWin.UsedPercent != 14.5 {
		t.Errorf("session UsedPercent: expected 14.5, got %v", sessionWin.UsedPercent)
	}
	if sessionWin.WindowDurationSeconds == nil || *sessionWin.WindowDurationSeconds != 18000 {
		t.Errorf("session WindowDurationSeconds: expected 18000, got %v", sessionWin.WindowDurationSeconds)
	}
	if sessionWin.ResetsAt == nil {
		t.Fatalf("session ResetsAt is nil")
	}
	expectedSessionReset := time.Date(2026, 8, 29, 17, 0, 0, 0, time.UTC)
	if !sessionWin.ResetsAt.Equal(expectedSessionReset) {
		t.Errorf("session ResetsAt: expected %v, got %v", expectedSessionReset, *sessionWin.ResetsAt)
	}

	weeklyWin := report.Windows[1]
	if weeklyWin.Label != "weekly" {
		t.Errorf("window[1] label: expected weekly, got %q", weeklyWin.Label)
	}
	if weeklyWin.UsedPercent == nil || *weeklyWin.UsedPercent != 62.0 {
		t.Errorf("weekly UsedPercent: expected 62.0, got %v", weeklyWin.UsedPercent)
	}
	if weeklyWin.WindowDurationSeconds == nil || *weeklyWin.WindowDurationSeconds != 604800 {
		t.Errorf("weekly WindowDurationSeconds: expected 604800, got %v", weeklyWin.WindowDurationSeconds)
	}
	if weeklyWin.ResetsAt == nil {
		t.Fatalf("weekly ResetsAt is nil")
	}
	expectedWeeklyReset := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	if !weeklyWin.ResetsAt.Equal(expectedWeeklyReset) {
		t.Errorf("weekly ResetsAt: expected %v, got %v", expectedWeeklyReset, *weeklyWin.ResetsAt)
	}
}

func TestOAuthAPISource_Fetch_DynamicWindows(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	future := now.Add(24 * time.Hour)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"five_hour": {
				"utilization": 0.0,
				"resets_at": "2026-08-29T17:00:00Z"
			},
			"seven_day": {
				"utilization": 5.0,
				"resets_at": "2026-09-05T12:00:00Z"
			},
			"one_hour": {
				"utilization": 0.0,
				"resets_at": "2026-08-29T13:00:00Z"
			},
			"custom_extra_window": {
				"utilization": 10.0,
				"resets_at": "2026-08-30T00:00:00Z"
			}
		}`))
	}))
	defer server.Close()

	src := claude.NewOAuthAPISource(
		claude.WithBaseURL(server.URL),
		claude.WithHTTPClient(server.Client()),
		claude.WithNow(func() time.Time { return now }),
		claude.WithCredentialResolver(func(ctx context.Context) (*localstate.ClaudeCredentials, error) {
			return &localstate.ClaudeCredentials{
				AccessToken: "sk-ant-test-token",
				ExpiresAt:   &future,
			}, nil
		}),
	)

	report, err := src.Fetch(ctx)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if len(report.Windows) != 4 {
		t.Fatalf("expected 4 dynamically discovered windows, got %d", len(report.Windows))
	}

	labels := []string{
		report.Windows[0].Label,
		report.Windows[1].Label,
		report.Windows[2].Label,
		report.Windows[3].Label,
	}

	expectedLabels := []string{"session", "weekly", "hourly", "custom_extra_window"}
	for i, exp := range expectedLabels {
		if labels[i] != exp {
			t.Errorf("window[%d] label: expected %s, got %s", i, exp, labels[i])
		}
	}
}

func TestOAuthAPISource_Fetch_NestedLimits(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	future := now.Add(24 * time.Hour)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"limits": {
				"five_hour": {
					"utilization": 20.0,
					"resets_at": "2026-08-29T17:00:00Z"
				},
				"seven_day": {
					"utilization": 80.0,
					"resets_at": "2026-09-05T12:00:00Z"
				}
			}
		}`))
	}))
	defer server.Close()

	src := claude.NewOAuthAPISource(
		claude.WithBaseURL(server.URL),
		claude.WithHTTPClient(server.Client()),
		claude.WithNow(func() time.Time { return now }),
		claude.WithCredentialResolver(func(ctx context.Context) (*localstate.ClaudeCredentials, error) {
			return &localstate.ClaudeCredentials{
				AccessToken: "sk-ant-test-token",
				ExpiresAt:   &future,
			}, nil
		}),
	)

	report, err := src.Fetch(ctx)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if len(report.Windows) != 2 {
		t.Fatalf("expected 2 windows from nested limits, got %d", len(report.Windows))
	}
}

func TestOAuthAPISource_Fetch_HTTP401(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	future := now.Add(24 * time.Hour)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error": "unauthorized"}`))
	}))
	defer server.Close()

	src := claude.NewOAuthAPISource(
		claude.WithBaseURL(server.URL),
		claude.WithHTTPClient(server.Client()),
		claude.WithNow(func() time.Time { return now }),
		claude.WithCredentialResolver(func(ctx context.Context) (*localstate.ClaudeCredentials, error) {
			return &localstate.ClaudeCredentials{
				AccessToken: "sk-ant-revoked-token",
				ExpiresAt:   &future,
			}, nil
		}),
	)

	_, err := src.Fetch(ctx)
	if err == nil {
		t.Fatalf("expected error on 401 Unauthorized")
	}

	if !errors.Is(err, dipstick.ErrNotAuthenticated) {
		t.Errorf("expected ErrNotAuthenticated, got %v", err)
	}

	reason := dipstick.ReasonForError(err)
	if reason != dipstick.ReasonNotAuthenticated {
		t.Errorf("expected ReasonNotAuthenticated, got %v", reason)
	}
}

func TestOAuthAPISource_Fetch_UpstreamErrors(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	future := now.Add(24 * time.Hour)

	statusCodes := []int{
		http.StatusForbidden,
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
	}

	for _, statusCode := range statusCodes {
		t.Run(fmt.Sprintf("HTTP %d", statusCode), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(statusCode)
				_, _ = w.Write([]byte(`{"error": "upstream failure"}`))
			}))
			defer server.Close()

			src := claude.NewOAuthAPISource(
				claude.WithBaseURL(server.URL),
				claude.WithHTTPClient(server.Client()),
				claude.WithNow(func() time.Time { return now }),
				claude.WithCredentialResolver(func(ctx context.Context) (*localstate.ClaudeCredentials, error) {
					return &localstate.ClaudeCredentials{
						AccessToken: "sk-ant-test-token",
						ExpiresAt:   &future,
					}, nil
				}),
			)

			_, err := src.Fetch(ctx)
			if err == nil {
				t.Fatalf("expected error for HTTP %d", statusCode)
			}

			if !errors.Is(err, dipstick.ErrUpstreamError) {
				t.Errorf("expected ErrUpstreamError for HTTP %d, got %v", statusCode, err)
			}

			reason := dipstick.ReasonForError(err)
			if reason != dipstick.ReasonUpstreamError {
				t.Errorf("expected ReasonUpstreamError, got %v", reason)
			}
		})
	}
}

func TestOAuthAPISource_Fetch_MalformedJSONAndDrift(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	future := now.Add(24 * time.Hour)

	cases := []struct {
		name string
		body string
	}{
		{"invalid JSON syntax", `{invalid-json-body`},
		{"empty JSON object", `{}`},
		{"no recognized rate windows", `{"status": "ok", "user": "test"}`},
		{"malformed timestamp", `{"five_hour": {"utilization": 10.0, "resets_at": "not-a-timestamp"}}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			src := claude.NewOAuthAPISource(
				claude.WithBaseURL(server.URL),
				claude.WithHTTPClient(server.Client()),
				claude.WithNow(func() time.Time { return now }),
				claude.WithCredentialResolver(func(ctx context.Context) (*localstate.ClaudeCredentials, error) {
					return &localstate.ClaudeCredentials{
						AccessToken: "sk-ant-test-token",
						ExpiresAt:   &future,
					}, nil
				}),
			)

			_, err := src.Fetch(ctx)
			if err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}

			if !errors.Is(err, dipstick.ErrParseFailed) {
				t.Errorf("expected ErrParseFailed for %s, got %v", tc.name, err)
			}

			reason := dipstick.ReasonForError(err)
			if reason != dipstick.ReasonParseFailed {
				t.Errorf("expected ReasonParseFailed, got %v", reason)
			}
		})
	}
}

func TestOAuthAPISource_Fetch_PreRequestExpirationAndMissingCreds(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	past := now.Add(-1 * time.Hour)

	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	t.Run("expired credentials pre-request check", func(t *testing.T) {
		atomic.StoreInt32(&requestCount, 0)
		src := claude.NewOAuthAPISource(
			claude.WithBaseURL(server.URL),
			claude.WithHTTPClient(server.Client()),
			claude.WithNow(func() time.Time { return now }),
			claude.WithCredentialResolver(func(ctx context.Context) (*localstate.ClaudeCredentials, error) {
				return &localstate.ClaudeCredentials{
					AccessToken: "sk-ant-expired-token",
					ExpiresAt:   &past,
				}, localstate.ErrCredentialExpired
			}),
		)

		_, err := src.Fetch(ctx)
		if !errors.Is(err, dipstick.ErrCredentialExpired) {
			t.Errorf("expected ErrCredentialExpired, got %v", err)
		}
		if dipstick.ReasonForError(err) != dipstick.ReasonCredentialExpired {
			t.Errorf("expected ReasonCredentialExpired, got %v", dipstick.ReasonForError(err))
		}
		if atomic.LoadInt32(&requestCount) != 0 {
			t.Errorf("expected zero network requests for expired credentials, got %d", atomic.LoadInt32(&requestCount))
		}
	})

	t.Run("missing credentials check", func(t *testing.T) {
		atomic.StoreInt32(&requestCount, 0)
		src := claude.NewOAuthAPISource(
			claude.WithBaseURL(server.URL),
			claude.WithHTTPClient(server.Client()),
			claude.WithNow(func() time.Time { return now }),
			claude.WithCredentialResolver(func(ctx context.Context) (*localstate.ClaudeCredentials, error) {
				return nil, localstate.ErrCredentialNotFound
			}),
		)

		_, err := src.Fetch(ctx)
		if !errors.Is(err, dipstick.ErrNotAuthenticated) {
			t.Errorf("expected ErrNotAuthenticated, got %v", err)
		}
		if dipstick.ReasonForError(err) != dipstick.ReasonNotAuthenticated {
			t.Errorf("expected ReasonNotAuthenticated, got %v", dipstick.ReasonForError(err))
		}
		if atomic.LoadInt32(&requestCount) != 0 {
			t.Errorf("expected zero network requests for missing credentials, got %d", atomic.LoadInt32(&requestCount))
		}
	})
}

func TestOAuthAPISource_Fetch_TimeoutAndCancellation(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	future := now.Add(24 * time.Hour)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"five_hour": {"utilization": 0, "resets_at": "2026-08-29T17:00:00Z"}}`))
	}))
	defer server.Close()

	t.Run("bounded HTTP timeout", func(t *testing.T) {
		src := claude.NewOAuthAPISource(
			claude.WithBaseURL(server.URL),
			claude.WithHTTPClient(server.Client()),
			claude.WithTimeout(30*time.Millisecond),
			claude.WithNow(func() time.Time { return now }),
			claude.WithCredentialResolver(func(ctx context.Context) (*localstate.ClaudeCredentials, error) {
				return &localstate.ClaudeCredentials{
					AccessToken: "sk-ant-test-token",
					ExpiresAt:   &future,
				}, nil
			}),
		)

		_, err := src.Fetch(context.Background())
		if err == nil {
			t.Fatalf("expected timeout error")
		}
		if !errors.Is(err, dipstick.ErrTimeout) {
			t.Errorf("expected ErrTimeout, got %v", err)
		}
		if dipstick.ReasonForError(err) != dipstick.ReasonTimeout {
			t.Errorf("expected ReasonTimeout, got %v", dipstick.ReasonForError(err))
		}
	})

	t.Run("caller context cancellation", func(t *testing.T) {
		cancelCtx, cancel := context.WithCancel(context.Background())
		cancel()

		src := claude.NewOAuthAPISource(
			claude.WithBaseURL(server.URL),
			claude.WithHTTPClient(server.Client()),
			claude.WithNow(func() time.Time { return now }),
			claude.WithCredentialResolver(func(ctx context.Context) (*localstate.ClaudeCredentials, error) {
				return &localstate.ClaudeCredentials{
					AccessToken: "sk-ant-test-token",
					ExpiresAt:   &future,
				}, nil
			}),
		)

		_, err := src.Fetch(cancelCtx)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	})

	t.Run("slow streaming response body triggers ErrTimeout", func(t *testing.T) {
		slowBodyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			time.Sleep(200 * time.Millisecond)
			_, _ = w.Write([]byte(`{"five_hour": {"utilization": 0}}`))
		}))
		defer slowBodyServer.Close()

		src := claude.NewOAuthAPISource(
			claude.WithBaseURL(slowBodyServer.URL),
			claude.WithHTTPClient(slowBodyServer.Client()),
			claude.WithTimeout(30*time.Millisecond),
			claude.WithNow(func() time.Time { return now }),
			claude.WithCredentialResolver(func(ctx context.Context) (*localstate.ClaudeCredentials, error) {
				return &localstate.ClaudeCredentials{
					AccessToken: "sk-ant-test-token",
					ExpiresAt:   &future,
				}, nil
			}),
		)

		_, err := src.Fetch(context.Background())
		if err == nil {
			t.Fatalf("expected timeout error for slow response body")
		}
		if !errors.Is(err, dipstick.ErrTimeout) {
			t.Errorf("expected ErrTimeout, got %v", err)
		}
	})
}

func TestOAuthAPISource_Security_ZeroTokenLeakage(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	future := now.Add(24 * time.Hour)
	secretToken := "sk-ant-super-secret-production-token-123456789"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "upstream failure with token %s"}`, secretToken)))
	}))
	defer server.Close()

	src := claude.NewOAuthAPISource(
		claude.WithBaseURL(server.URL),
		claude.WithHTTPClient(server.Client()),
		claude.WithNow(func() time.Time { return now }),
		claude.WithCredentialResolver(func(ctx context.Context) (*localstate.ClaudeCredentials, error) {
			return &localstate.ClaudeCredentials{
				AccessToken: secretToken,
				ExpiresAt:   &future,
			}, nil
		}),
	)

	_, err := src.Fetch(ctx)
	if err == nil {
		t.Fatalf("expected error from bad gateway")
	}

	errStr := err.Error()
	if strings.Contains(errStr, secretToken) {
		t.Errorf("Fetch() error leaked secret token: %s", errStr)
	}
}

func TestClaudeAdapter_EndToEnd_ResolverIntegration(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	future := now.Add(24 * time.Hour)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"five_hour": {
				"utilization": 25.0,
				"resets_at": "2026-08-29T17:00:00Z"
			},
			"seven_day": {
				"utilization": 50.0,
				"resets_at": "2026-09-05T12:00:00Z"
			}
		}`))
	}))
	defer server.Close()

	adapter := claude.New(
		claude.WithAdapterBaseURL(server.URL),
		claude.WithAdapterHTTPClient(server.Client()),
		claude.WithAdapterNow(func() time.Time { return now }),
		claude.WithAdapterCredentialResolver(func(ctx context.Context) (*localstate.ClaudeCredentials, error) {
			return &localstate.ClaudeCredentials{
				AccessToken:  "sk-ant-test-token",
				AccountID:    "acc-e2e-123",
				Email:        "e2e@example.com",
				Subscription: "team",
				ExpiresAt:    &future,
			}, nil
		}),
	)

	report, err := dipstick.Collect(ctx,
		dipstick.WithProviders(dipstick.ProviderClaude),
		dipstick.WithAdapter(adapter),
	)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	if len(report.Errors) > 0 {
		t.Fatalf("unexpected provider errors: %+v", report.Errors)
	}

	if len(report.Providers) != 1 {
		t.Fatalf("expected 1 provider report, got %d", len(report.Providers))
	}

	pReport := report.Providers[0]
	if pReport.Provider != dipstick.ProviderClaude {
		t.Errorf("Provider: got %q", pReport.Provider)
	}
	if pReport.Source != dipstick.SourceOAuthAPI {
		t.Errorf("Source: got %q", pReport.Source)
	}
	if pReport.Confidence != dipstick.ConfidenceExact {
		t.Errorf("Confidence: got %q", pReport.Confidence)
	}
	if len(pReport.Windows) != 2 {
		t.Fatalf("expected 2 windows, got %d", len(pReport.Windows))
	}
	if pReport.Windows[0].Label != "session" || *pReport.Windows[0].UsedPercent != 25.0 {
		t.Errorf("session window: got %+v", pReport.Windows[0])
	}
	if pReport.Windows[1].Label != "weekly" || *pReport.Windows[1].UsedPercent != 50.0 {
		t.Errorf("weekly window: got %+v", pReport.Windows[1])
	}
}

func TestClaudeAdapter_EndToEnd_ExpiredCredentials(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	past := now.Add(-1 * time.Hour)

	adapter := claude.New(
		claude.WithAdapterNow(func() time.Time { return now }),
		claude.WithAdapterCredentialResolver(func(ctx context.Context) (*localstate.ClaudeCredentials, error) {
			return &localstate.ClaudeCredentials{
				AccessToken: "sk-ant-expired-token",
				ExpiresAt:   &past,
			}, localstate.ErrCredentialExpired
		}),
		claude.WithAdapterProjectsDir(filepath.Join(t.TempDir(), "nonexistent")),
	)

	report, err := dipstick.Collect(ctx,
		dipstick.WithProviders(dipstick.ProviderClaude),
		dipstick.WithAdapter(adapter),
	)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	if len(report.Providers) != 0 {
		t.Fatalf("expected 0 providers in report, got %d", len(report.Providers))
	}

	if len(report.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d: %+v", len(report.Errors), report.Errors)
	}

	pe := report.Errors[0]
	if pe.Provider != dipstick.ProviderClaude {
		t.Errorf("Provider: got %q, want %q", pe.Provider, dipstick.ProviderClaude)
	}
	if pe.Reason != dipstick.ReasonCredentialExpired {
		t.Errorf("Reason: got %q, want %q", pe.Reason, dipstick.ReasonCredentialExpired)
	}
}

func TestLiveHostClaudeUsage(t *testing.T) {
	// Integration test exercising live credential resolution and adapter execution against local system state when available.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	creds, err := localstate.ReadClaudeCredentials(ctx)
	t.Logf("ReadClaudeCredentials result: creds=%s, err=%v", creds, err)
	if err != nil || creds == nil || creds.AccessToken == "" || creds.IsExpired(time.Now()) {
		t.Skip("skipping live Claude usage test: valid unexpired credentials not present on host")
	}

	adapter := claude.New()
	sources := adapter.Sources()
	if len(sources) == 0 {
		t.Fatalf("adapter declared no sources")
	}

	oauthSrc := sources[0]
	if !oauthSrc.Available(ctx) {
		t.Skip("oauth source not available on host")
	}

	rep, err := oauthSrc.Fetch(ctx)
	if err != nil {
		t.Logf("live fetch returned error (may be network/connectivity or rate limit): %v", err)
		return
	}

	if rep.Provider != dipstick.ProviderClaude {
		t.Errorf("expected provider %q, got %q", dipstick.ProviderClaude, rep.Provider)
	}
	if rep.Source != dipstick.SourceOAuthAPI {
		t.Errorf("expected source %q, got %q", dipstick.SourceOAuthAPI, rep.Source)
	}
	if rep.Confidence != dipstick.ConfidenceExact {
		t.Errorf("expected confidence exact, got %q", rep.Confidence)
	}

	t.Logf("Live Claude Report: windows=%d identity=%+v", len(rep.Windows), rep.Identity)
	for _, w := range rep.Windows {
		var pct float64
		if w.UsedPercent != nil {
			pct = *w.UsedPercent
		}
		var dur int64
		if w.WindowDurationSeconds != nil {
			dur = *w.WindowDurationSeconds
		}
		t.Logf("  Window: label=%q used_percent=%.2f%% resets_at=%v duration_s=%d", w.Label, pct, w.ResetsAt, dur)
	}
}
