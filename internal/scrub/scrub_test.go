package scrub_test

import (
	"testing"

	"github.com/mattwalters/dipstick/internal/scrub"
)

func TestScrub_AuthHeaders(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Authorization Bearer header",
			input:    "Authorization: Bearer secret_token_12345",
			expected: "Authorization: [REDACTED]",
		},
		{
			name:     "Authorization Basic header",
			input:    "Authorization: Basic dXNlcjpwYXNzd29yZA==",
			expected: "Authorization: [REDACTED]",
		},
		{
			name:     "Authorization Token header",
			input:    "Authorization: Token 9944b09199c62bcf9418ad846dd0e4bbdfc6ee4b",
			expected: "Authorization: [REDACTED]",
		},
		{
			name:     "Standalone Bearer token",
			input:    "HTTP 401: Bearer ghp_0123456789abcdefghijklmnopqrstuvwxyz invalid",
			expected: "HTTP 401: Bearer [REDACTED] invalid",
		},
		{
			name:     "Standalone Basic credentials",
			input:    "failed with Basic dXNlcjpwYXNzd29yZDEyMw==",
			expected: "failed with Basic [REDACTED]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scrub.Scrub(tt.input)
			if got != tt.expected {
				t.Errorf("got %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestScrub_Cookies(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Cookie header",
			input:    "Cookie: session=xyz123; user_id=456",
			expected: "Cookie: [REDACTED]",
		},
		{
			name:     "Set-Cookie header",
			input:    "Set-Cookie: session=xyz123; Secure; HttpOnly",
			expected: "Set-Cookie: [REDACTED]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scrub.Scrub(tt.input)
			if got != tt.expected {
				t.Errorf("got %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestScrub_APIKeysAndTokens(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Anthropic API key",
			input:    "failed to call anthropic with key sk-ant-api03-abcdef123456",
			expected: "failed to call anthropic with key [REDACTED]",
		},
		{
			name:     "OpenAI API key",
			input:    "invalid response using sk-abcdef1234567890abcdef",
			expected: "invalid response using [REDACTED]",
		},
		{
			name:     "GitHub token",
			input:    "error authenticating ghp_123456789012345678901234567890123456",
			expected: "error authenticating [REDACTED]",
		},
		{
			name:     "GitHub fine-grained PAT",
			input:    "error authenticating github_pat_11AAAAAAA0123456789_abcdefghijklmnopqrstuvwxyz",
			expected: "error authenticating [REDACTED]",
		},
		{
			name:     "Google API key",
			input:    "request failed: AIzaSyA1234567890abcdefghijklmnopqrstuvw",
			expected: "request failed: [REDACTED]",
		},
		{
			name:     "AWS Access Key",
			input:    "failed with user AKIAIOSFODNN7EXAMPLE",
			expected: "failed with user [REDACTED]",
		},
		{
			name:     "Slack token",
			input:    "bot xoxb-1234567890-1234567890-abcdef12345 failed",
			expected: "bot [REDACTED] failed",
		},
		{
			name:     "JWT token",
			input:    "token eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c expired",
			expected: "token [REDACTED] expired",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scrub.Scrub(tt.input)
			if got != tt.expected {
				t.Errorf("got %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestScrub_Parameters(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "URL query password parameter",
			input:    "https://example.com/api?user=alice&password=secret_password123&action=login",
			expected: "https://example.com/api?user=alice&password=[REDACTED]&action=login",
		},
		{
			name:     "URL query token parameter",
			input:    "https://api.vendor.com/v1/usage?token=my_secret_token_abc&env=prod",
			expected: "https://api.vendor.com/v1/usage?token=[REDACTED]&env=prod",
		},
		{
			name:     "JSON token field",
			input:    `{"status": "error", "token": "my_secret_token_123"}`,
			expected: `{"status": "error", "token": "[REDACTED]"}`,
		},
		{
			name:     "JSON password field",
			input:    `{"user": "admin", "password": "super-secret-password"}`,
			expected: `{"user": "admin", "password": "[REDACTED]"}`,
		},
		{
			name:     "CLI flag token",
			input:    "command failed: --token=secret_cli_token_999",
			expected: "command failed: --token=[REDACTED]",
		},
		{
			name:     "key=value format",
			input:    "credentials loaded from config: key=top_secret_key_val",
			expected: "credentials loaded from config: key=[REDACTED]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scrub.Scrub(tt.input)
			if got != tt.expected {
				t.Errorf("got %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestScrub_NonSensitiveStringsPreserved(t *testing.T) {
	nonSensitive := []string{
		"",
		"antigravity exposes no usage or quota surface",
		"all sources exhausted without success",
		"no source reported itself available",
		"every source was excluded by the source policy",
		"no sources are implemented for this provider yet",
		"source unavailable",
		"availability check timed out",
		"401 Unauthorized",
		"500 Internal Server Error",
		"executable not found in PATH",
		"unexpected response payload at byte 42",
		"source fetch timeout",
		"--sort-key=name",
		"turn-key=auto",
		"public-key=allowed",
	}

	for _, s := range nonSensitive {
		got := scrub.Scrub(s)
		if got != s {
			t.Errorf("non-sensitive string modified: got %q, want %q", got, s)
		}
	}
}
