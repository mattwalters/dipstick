package dipstick_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mattwalters/dipstick"
)

func TestSentinels_DefinitionsAndMappings(t *testing.T) {
	cases := []struct {
		reason    dipstick.Reason
		sentinel  error
		retryable bool
	}{
		{
			reason:    dipstick.ReasonNotInstalled,
			sentinel:  dipstick.ErrNotInstalled,
			retryable: false,
		},
		{
			reason:    dipstick.ReasonNotAuthenticated,
			sentinel:  dipstick.ErrNotAuthenticated,
			retryable: false,
		},
		{
			reason:    dipstick.ReasonCredentialExpired,
			sentinel:  dipstick.ErrCredentialExpired,
			retryable: false,
		},
		{
			reason:    dipstick.ReasonUnsupportedVersion,
			sentinel:  dipstick.ErrUnsupportedVersion,
			retryable: false,
		},
		{
			reason:    dipstick.ReasonParseFailed,
			sentinel:  dipstick.ErrParseFailed,
			retryable: false,
		},
		{
			reason:    dipstick.ReasonUpstreamError,
			sentinel:  dipstick.ErrUpstreamError,
			retryable: true,
		},
		{
			reason:    dipstick.ReasonTimeout,
			sentinel:  dipstick.ErrTimeout,
			retryable: true,
		},
		{
			reason:    dipstick.ReasonNotSupported,
			sentinel:  dipstick.ErrNotSupported,
			retryable: false,
		},
	}

	for _, tc := range cases {
		t.Run(string(tc.reason), func(t *testing.T) {
			// Test Sentinel(reason)
			if got := dipstick.Sentinel(tc.reason); got != tc.sentinel {
				t.Errorf("Sentinel(%v): got %v, want %v", tc.reason, got, tc.sentinel)
			}
			// Test reason.Sentinel()
			if got := tc.reason.Sentinel(); got != tc.sentinel {
				t.Errorf("%v.Sentinel(): got %v, want %v", tc.reason, got, tc.sentinel)
			}
			// Test reason.Retryable()
			if got := tc.reason.Retryable(); got != tc.retryable {
				t.Errorf("%v.Retryable(): got %v, want %v", tc.reason, got, tc.retryable)
			}
			// Test ReasonForError(sentinel)
			if got := dipstick.ReasonForError(tc.sentinel); got != tc.reason {
				t.Errorf("ReasonForError(%v): got %v, want %v", tc.sentinel, got, tc.reason)
			}
			// Test ReasonForError(wrapped sentinel)
			wrapped := fmt.Errorf("operation failed: %w", tc.sentinel)
			if got := dipstick.ReasonForError(wrapped); got != tc.reason {
				t.Errorf("ReasonForError(wrapped %v): got %v, want %v", tc.sentinel, got, tc.reason)
			}
		})
	}

	t.Run("unknown reason sentinel mapping", func(t *testing.T) {
		if got := dipstick.Sentinel(dipstick.Reason("unknown_reason")); got != nil {
			t.Errorf("expected nil for unknown reason sentinel, got %v", got)
		}
		if got := dipstick.ReasonForError(errors.New("unrelated error")); got != "" {
			t.Errorf("expected empty Reason for unrelated error, got %v", got)
		}
		if got := dipstick.ReasonForError(nil); got != "" {
			t.Errorf("expected empty Reason for nil error, got %v", got)
		}
	})
}

func TestProviderError_ErrorsIs(t *testing.T) {
	reasons := []struct {
		reason   dipstick.Reason
		sentinel error
	}{
		{dipstick.ReasonNotInstalled, dipstick.ErrNotInstalled},
		{dipstick.ReasonNotAuthenticated, dipstick.ErrNotAuthenticated},
		{dipstick.ReasonCredentialExpired, dipstick.ErrCredentialExpired},
		{dipstick.ReasonUnsupportedVersion, dipstick.ErrUnsupportedVersion},
		{dipstick.ReasonParseFailed, dipstick.ErrParseFailed},
		{dipstick.ReasonUpstreamError, dipstick.ErrUpstreamError},
		{dipstick.ReasonTimeout, dipstick.ErrTimeout},
		{dipstick.ReasonNotSupported, dipstick.ErrNotSupported},
	}

	for _, r := range reasons {
		t.Run(string(r.reason), func(t *testing.T) {
			pe := dipstick.ProviderError{
				Provider: dipstick.ProviderClaude,
				Reason:   r.reason,
				Source:   dipstick.SourceOAuthAPI,
				Detail:   "diagnostic failure detail",
			}

			// Test value receiver with errors.Is
			if !errors.Is(pe, r.sentinel) {
				t.Errorf("expected errors.Is(pe, %v) to be true", r.sentinel)
			}

			// Test pointer receiver with errors.Is
			pePtr := &pe
			if !errors.Is(pePtr, r.sentinel) {
				t.Errorf("expected errors.Is(&pe, %v) to be true", r.sentinel)
			}

			// Test wrapped error with errors.Is
			wrapped := fmt.Errorf("provider execution failed: %w", pe)
			if !errors.Is(wrapped, r.sentinel) {
				t.Errorf("expected errors.Is(wrapped, %v) to be true", r.sentinel)
			}

			// Test that it does NOT match other sentinels
			for _, other := range reasons {
				if other.reason != r.reason {
					if errors.Is(pe, other.sentinel) {
						t.Errorf("errors.Is(pe, %v) should be false for reason %v", other.sentinel, r.reason)
					}
				}
			}
		})
	}

	t.Run("matching ProviderError instances", func(t *testing.T) {
		pe := dipstick.ProviderError{
			Provider: dipstick.ProviderCodex,
			Reason:   dipstick.ReasonParseFailed,
			Source:   dipstick.SourceLocalState,
		}

		// Match by reason only
		if !errors.Is(pe, dipstick.ProviderError{Reason: dipstick.ReasonParseFailed}) {
			t.Errorf("expected match by reason only")
		}

		// Match by provider and reason
		if !errors.Is(pe, dipstick.ProviderError{Provider: dipstick.ProviderCodex, Reason: dipstick.ReasonParseFailed}) {
			t.Errorf("expected match by provider and reason")
		}

		// Mismatched provider should not match
		if errors.Is(pe, dipstick.ProviderError{Provider: dipstick.ProviderClaude, Reason: dipstick.ReasonParseFailed}) {
			t.Errorf("mismatched provider should not match")
		}
	})
}

func TestProviderError_ErrorsAs(t *testing.T) {
	orig := dipstick.ProviderError{
		Provider:  dipstick.ProviderClaude,
		Reason:    dipstick.ReasonParseFailed,
		Source:    dipstick.SourceOAuthAPI,
		Detail:    "invalid token response structure",
		Retryable: false,
	}

	wrapped := fmt.Errorf("outer wrap: %w", orig)

	var target dipstick.ProviderError
	if !errors.As(wrapped, &target) {
		t.Fatalf("expected errors.As to populate ProviderError")
	}

	if target.Provider != orig.Provider || target.Reason != orig.Reason || target.Source != orig.Source {
		t.Errorf("target mismatch: got %+v, want %+v", target, orig)
	}

	// ReasonForError should also extract from wrapped ProviderError
	if got := dipstick.ReasonForError(wrapped); got != dipstick.ReasonParseFailed {
		t.Errorf("ReasonForError(wrapped ProviderError): got %v, want %v", got, dipstick.ReasonParseFailed)
	}
}

func TestProviderError_SecretScrubbing(t *testing.T) {
	fakeSecrets := []struct {
		name         string
		secret       string
		detailFormat string
	}{
		{
			name:         "Bearer auth token",
			secret:       "sk-ant-api03-abcdef1234567890abcdef123456",
			detailFormat: "HTTP 401 Unauthorized: Authorization: Bearer %s",
		},
		{
			name:         "OpenAI API key",
			secret:       "sk-1234567890abcdef1234567890abcdef",
			detailFormat: "failed calling endpoint with key=%s in query",
		},
		{
			name:         "GitHub personal access token",
			secret:       "ghp_0123456789abcdefghijklmnopqrstuvwxyz01",
			detailFormat: "git credential helper failed for %s",
		},
		{
			name:         "Session cookie",
			secret:       "session_id=abcdef1234567890; token=secret_cookie_token",
			detailFormat: "Cookie: %s",
		},
		{
			name:         "JSON password payload",
			secret:       "super_secret_password_12345!",
			detailFormat: `{"user": "test", "password": "%s"}`,
		},
		{
			name:         "JWT authentication token",
			secret:       "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c",
			detailFormat: "JWT verification failed for token=%s",
		},
	}

	for _, sc := range fakeSecrets {
		t.Run(sc.name, func(t *testing.T) {
			dirtyDetail := fmt.Sprintf(sc.detailFormat, sc.secret)

			pe := dipstick.ProviderError{
				Provider:  dipstick.ProviderClaude,
				Reason:    dipstick.ReasonNotAuthenticated,
				Source:    dipstick.SourceOAuthAPI,
				Detail:    dirtyDetail,
				Retryable: false,
			}

			// 1. Assert ProviderError.Error() does not contain raw secret
			errStr := pe.Error()
			if strings.Contains(errStr, sc.secret) {
				t.Errorf("pe.Error() leaked secret: %s", errStr)
			}
			if !strings.Contains(errStr, "[REDACTED]") {
				t.Errorf("pe.Error() expected to contain [REDACTED], got: %s", errStr)
			}

			// 2. Assert marshaled JSON report does not contain raw secret
			report := dipstick.Report{
				SchemaVersion: dipstick.SchemaVersion,
				GeneratedAt:   time.Now().UTC(),
				Providers:     []dipstick.ProviderReport{},
				Errors:        []dipstick.ProviderError{pe},
			}

			data, err := json.MarshalIndent(report, "", "  ")
			if err != nil {
				t.Fatalf("json.MarshalIndent failed: %v", err)
			}

			jsonStr := string(data)
			if strings.Contains(jsonStr, sc.secret) {
				t.Errorf("marshaled JSON leaked secret: %s", jsonStr)
			}
			if !strings.Contains(jsonStr, "[REDACTED]") {
				t.Errorf("marshaled JSON expected to contain [REDACTED], got: %s", jsonStr)
			}
		})
	}
}
