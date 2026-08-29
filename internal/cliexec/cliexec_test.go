package cliexec_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mattwalters/dipstick/internal/cliexec"
)

func TestRun_Success(t *testing.T) {
	ctx := context.Background()
	res, err := cliexec.Run(ctx, "echo", "hello world")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", res.ExitCode)
	}

	if res.StdoutString() != "hello world" {
		t.Errorf("unexpected stdout string: %q", res.StdoutString())
	}
	if res.StderrString() != "" {
		t.Errorf("unexpected stderr string: %q", res.StderrString())
	}
}

func TestRun_ExitCode(t *testing.T) {
	ctx := context.Background()
	res, err := cliexec.Run(ctx, "sh", "-c", "echo 'err' >&2; exit 42")
	if err == nil {
		t.Fatalf("expected error for exit 42, got nil")
	}

	if res.ExitCode != 42 {
		t.Errorf("expected exit code 42, got %d", res.ExitCode)
	}
	if res.StderrString() != "err" {
		t.Errorf("expected stderr 'err', got %q", res.StderrString())
	}
}

func TestRun_NotFound(t *testing.T) {
	ctx := context.Background()
	res, err := cliexec.Run(ctx, "nonexistent-binary-123456")
	if err == nil {
		t.Fatalf("expected error for non-existent binary, got nil")
	}
	if res.ExitCode != -1 {
		t.Errorf("expected exit code -1, got %d", res.ExitCode)
	}
}

func TestRun_Timeout(t *testing.T) {
	ctx := context.Background()
	runner := cliexec.New(cliexec.WithTimeout(50 * time.Millisecond))
	_, err := runner.Run(ctx, "sleep", "1")
	if err == nil {
		t.Fatalf("expected timeout error, got nil")
	}
}

func TestRun_Options(t *testing.T) {
	tmp := t.TempDir()
	runner := cliexec.New(
		cliexec.WithDir(tmp),
		cliexec.WithEnv([]string{"TEST_VAR=custom_val"}),
		cliexec.WithScrubSecrets(false),
		nil,
	)

	ctx := context.Background()
	res, err := runner.Run(ctx, "sh", "-c", "pwd && echo $TEST_VAR")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := res.StdoutString()
	if !strings.Contains(out, "custom_val") {
		t.Errorf("expected output to contain 'custom_val', got %q", out)
	}
}

func TestScrubEnv(t *testing.T) {
	env := []string{
		"PATH=/usr/bin:/bin",
		"HOME=/home/user",
		"OPENAI_API_KEY=sk-12345",
		"ANTHROPIC_AUTH_TOKEN=auth-secret",
		"LINEAR_API_KEY=lin-key",
		"DB_PASSWORD=supersecret",
		"PRIVATE_KEY_PATH=/tmp/key",
		"USER=developer",
		"",
	}

	scrubbed := cliexec.ScrubEnv(env)

	for _, kv := range scrubbed {
		if strings.Contains(kv, "OPENAI_API_KEY") ||
			strings.Contains(kv, "ANTHROPIC_AUTH_TOKEN") ||
			strings.Contains(kv, "LINEAR_API_KEY") ||
			strings.Contains(kv, "DB_PASSWORD") ||
			strings.Contains(kv, "PRIVATE_KEY_PATH") {
			t.Errorf("found unscrubbed sensitive variable: %s", kv)
		}
	}

	expectedKeys := map[string]bool{
		"PATH": true,
		"HOME": true,
		"USER": true,
	}

	for _, kv := range scrubbed {
		parts := strings.SplitN(kv, "=", 2)
		if !expectedKeys[parts[0]] {
			t.Errorf("unexpected preserved key: %s", parts[0])
		}
	}
}

func TestRun_NilContextHandling(t *testing.T) {
	var ctx context.Context
	res, err := cliexec.Run(ctx, "echo", "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.StdoutString() != "hi" {
		t.Errorf("expected 'hi', got %q", res.StdoutString())
	}
}

func TestRun_ExplicitEmptyEnv(t *testing.T) {
	t.Setenv("HOST_TEST_ENV_VAR", "should_not_leak")
	runner := cliexec.New(
		cliexec.WithEnv([]string{}),
		cliexec.WithScrubSecrets(false),
	)

	ctx := context.Background()
	res, err := runner.Run(ctx, "sh", "-c", "echo $HOST_TEST_ENV_VAR")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.StdoutString() != "" {
		t.Errorf("expected empty output for explicit empty env, got %q", res.StdoutString())
	}
}
