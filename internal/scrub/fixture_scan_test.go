package scrub_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mattwalters/dipstick/internal/scrub"
)

func TestCommittedFixturesSecretScan(t *testing.T) {
	fixtureDir := filepath.Join("..", "..", "testdata", "fixtures")
	if _, err := os.Stat(fixtureDir); os.IsNotExist(err) {
		t.Fatalf("fixtures directory does not exist: %s", fixtureDir)
	}

	scannedFiles := 0
	totalFindings := 0

	err := filepath.Walk(fixtureDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		// Only inspect fixture data files
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".json" && ext != ".txt" && ext != ".yaml" && ext != ".yml" {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("failed reading fixture file %s: %v", path, err)
			return nil
		}

		scannedFiles++
		findings := scrub.FindSecrets(string(data))
		if len(findings) > 0 {
			totalFindings += len(findings)
			for _, f := range findings {
				t.Errorf("SECRET SCAN FAILURE in %s: [%s] %s (matched: %q)", path, f.Rule, f.Message, f.Match)
			}
		}
		return nil
	})

	if err != nil {
		t.Fatalf("walking fixtures directory %s: %v", fixtureDir, err)
	}

	if scannedFiles == 0 {
		t.Fatalf("no fixture files scanned in %s", fixtureDir)
	}

	if totalFindings > 0 {
		t.Fatalf("fixture secret scanner found %d unredacted credential/PII leaks across %d files", totalFindings, scannedFiles)
	}
}

func TestSecretScanner_CatchesInjectedSecrets(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		expected string // rule name
	}{
		{
			name:     "Anthropic live API key",
			input:    `{"api_key": "sk-ant-api03-abcdef12345678901234567890"}`,
			expected: "anthropic_api_key",
		},
		{
			name:     "OpenAI live API key",
			input:    `{"key": "sk-proj-1234567890abcdefghijklmnopqrstuvwxyz"}`,
			expected: "openai_api_key",
		},
		{
			name:     "GitHub personal access token",
			input:    `{"gh_token": "ghp_123456789012345678901234567890123456"}`,
			expected: "github_token",
		},
		{
			name:     "Google API key",
			input:    `{"gcp_key": "AIzaSyA1234567890abcdefghijklmnopqrstuvw"}`,
			expected: "google_api_key",
		},
		{
			name:     "AWS Access Key",
			input:    `{"aws_access_key": "AKIAIOSFODNN7EXAMPLE"}`,
			expected: "aws_access_key",
		},
		{
			name:     "Slack token",
			input:    `{"slack_token": "xoxb-1234567890-abcdef12345"}`,
			expected: "slack_token",
		},
		{
			name:     "HuggingFace token",
			input:    `{"hf_token": "hf_123456789012345678901234567890123456"}`,
			expected: "huggingface_token",
		},
		{
			name:     "Unredacted Authorization header",
			input:    `Authorization: Bearer my_secret_live_access_token_12345`,
			expected: "unredacted_auth_header",
		},
		{
			name:     "Real personal email address",
			input:    `{"email": "john.doe@company.corp"}`,
			expected: "real_email_address",
		},
		{
			name:     "Private key block",
			input:    "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA...\n-----END RSA PRIVATE KEY-----",
			expected: "private_key",
		},
		{
			name:     "Live JWT token with real signature",
			input:    `{"id_token": "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.live_real_cryptographic_signature_value_12345"}`,
			expected: "unredacted_jwt",
		},
		{
			name:     "Standalone Bearer token",
			input:    `Bearer ya29.a0AfH6SMCx1234567890abcdefghijklmnopqrstuvwxyz`,
			expected: "unredacted_bearer_token",
		},
		{
			name:     "Standalone Basic auth credentials",
			input:    `Basic dXNlcm5hbWU6c3VwZXJfc2VjcmV0X3Bhc3N3b3JkXzEyMzQ1`,
			expected: "unredacted_basic_auth",
		},
		{
			name:     "Cookie header with live session",
			input:    `Cookie: session_id=live_secret_session_token_12345; user=admin`,
			expected: "unredacted_cookie_header",
		},
		{
			name:     "Quoted credential param",
			input:    `{"client_secret": "super_secret_client_key_12345"}`,
			expected: "unredacted_credential_param",
		},
		{
			name:     "Unquoted credential param",
			input:    `--password=super_secret_cli_password_12345`,
			expected: "unredacted_credential_param",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings := scrub.FindSecrets(tc.input)
			if len(findings) == 0 {
				t.Fatalf("expected scanner to catch secret for %s, got 0 findings", tc.name)
			}
			found := false
			for _, f := range findings {
				if f.Rule == tc.expected {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected rule %s in findings, got: %+v", tc.expected, findings)
			}
		})
	}
}

func TestSecretScanner_AllowsSyntheticPlaceholders(t *testing.T) {
	allowed := []string{
		`{"email": "developer@example.com", "account_id": "acc-chatgpt-12345"}`,
		`{"email": "user@example.org", "account_id": "acc-claude-test"}`,
		`{"email": "test@example.net"}`,
		`{"email": "tester@test.com"}`,
		`{"token": "[REDACTED]"}`,
		`{"token": "mock-access-token", "refresh": "mock-refresh-token"}`,
		`{"key": "sk-mock-key-0000000000000000"}`,
		`{"key": "sk-ant-test-token"}`,
		`{"status": "ok", "utilization": 15.5}`,
	}

	for _, s := range allowed {
		findings := scrub.FindSecrets(s)
		if len(findings) > 0 {
			t.Errorf("unexpected findings for synthetic input %q: %+v", s, findings)
		}
	}
}
