package cliexec_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mattwalters/dipstick/internal/cliexec"
)

var fakeCliBin string

func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "cliexec-test-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir for fakecli: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	binName := "fakecli"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	fakeCliBin = filepath.Join(tmpDir, binName)

	cmd := exec.Command("go", "build", "-o", fakeCliBin, "./testdata/fakecli")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to build fakecli: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()
	os.Exit(code)
}

func TestResolveBinary_Absolute(t *testing.T) {
	resolved, err := cliexec.ResolveBinary(fakeCliBin)
	if err != nil {
		t.Fatalf("expected resolving absolute binary to succeed, got: %v", err)
	}
	if resolved != fakeCliBin {
		t.Errorf("expected %q, got %q", fakeCliBin, resolved)
	}
}

func TestResolveBinary_PathLookup(t *testing.T) {
	binDir := filepath.Dir(fakeCliBin)
	binName := filepath.Base(fakeCliBin)

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+origPath)

	resolved, err := cliexec.ResolveBinary(binName)
	if err != nil {
		t.Fatalf("expected PATH resolution to succeed, got: %v", err)
	}
	if resolved != fakeCliBin {
		t.Errorf("expected %q, got %q", fakeCliBin, resolved)
	}
}

func TestResolveBinary_RelativeRejected(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "dot slash", input: "./fakecli"},
		{name: "parent slash", input: "../fakecli"},
		{name: "relative subpath", input: "subdir/fakecli"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := cliexec.ResolveBinary(tt.input)
			if err == nil {
				t.Fatalf("expected error for relative path %q, got nil", tt.input)
			}
			if !errors.Is(err, cliexec.ErrRelativeBinary) {
				t.Errorf("expected ErrRelativeBinary, got: %v", err)
			}
		})
	}
}

func TestResolveBinary_Empty(t *testing.T) {
	_, err := cliexec.ResolveBinary("   ")
	if err == nil {
		t.Fatalf("expected error for empty binary name, got nil")
	}
	if !errors.Is(err, cliexec.ErrEmptyBinary) {
		t.Errorf("expected ErrEmptyBinary, got: %v", err)
	}
}

func TestResolveBinary_NotFound(t *testing.T) {
	_, err := cliexec.ResolveBinary("definitely-nonexistent-binary-xyz-12345")
	if err == nil {
		t.Fatalf("expected error for nonexistent binary, got nil")
	}
}

func TestValidateArgv(t *testing.T) {
	allowedCases := [][]string{
		{"--version"},
		{"-v"},
		{"-V"},
		{"-version"},
		{"version"},
		{"--version", "--json"},
		{"version", "--json"},
		{"--help"},
		{"-h"},
		{"help"},
	}

	for _, argv := range allowedCases {
		t.Run(strings.Join(argv, "_"), func(t *testing.T) {
			if err := cliexec.ValidateArgv(argv, cliexec.DefaultPermittedArgvPatterns); err != nil {
				t.Errorf("expected argv %v to be allowed, got error: %v", argv, err)
			}
		})
	}

	disallowedCases := [][]string{
		{},
		{"run"},
		{"prompt", "hello world"},
		{"--interactive"},
		{"chat"},
		{"-c", "rm -rf /"},
		{"auth", "login"},
	}

	for _, argv := range disallowedCases {
		t.Run("disallowed_"+strings.Join(argv, "_"), func(t *testing.T) {
			err := cliexec.ValidateArgv(argv, cliexec.DefaultPermittedArgvPatterns)
			if err == nil {
				t.Errorf("expected argv %v to be rejected, got nil", argv)
			}
			if !errors.Is(err, cliexec.ErrDisallowedArgv) {
				t.Errorf("expected ErrDisallowedArgv, got: %v", err)
			}
		})
	}
}

func TestRun_StrictArgvEnforcement(t *testing.T) {
	runner := cliexec.New()
	ctx := context.Background()

	_, err := runner.Run(ctx, fakeCliBin, "prompt", "say something")
	if err == nil {
		t.Fatalf("expected strict argv rejection for 'prompt', got nil")
	}
	if !errors.Is(err, cliexec.ErrDisallowedArgv) {
		t.Errorf("expected ErrDisallowedArgv, got: %v", err)
	}

	// Permitting via custom option
	customRunner := cliexec.New(
		cliexec.WithExtraPermittedArgv([]string{"exit", "0"}),
	)
	res, err := customRunner.Run(ctx, fakeCliBin, "exit", "0")
	if err != nil {
		t.Fatalf("expected custom permitted argv to succeed, got: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", res.ExitCode)
	}
}

func TestRun_Success(t *testing.T) {
	ctx := context.Background()
	res, err := cliexec.Run(ctx, fakeCliBin, "--version")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", res.ExitCode)
	}
	if res.BinaryPath != fakeCliBin {
		t.Errorf("expected BinaryPath %q, got %q", fakeCliBin, res.BinaryPath)
	}
	if !strings.Contains(res.StdoutString(), "fakecli version 2.4.0") {
		t.Errorf("unexpected stdout: %q", res.StdoutString())
	}
	if res.StderrString() != "" {
		t.Errorf("expected empty stderr, got %q", res.StderrString())
	}
	if res.Duration <= 0 {
		t.Errorf("expected positive duration, got %v", res.Duration)
	}
}

func TestRun_NonZeroExit(t *testing.T) {
	runner := cliexec.New(
		cliexec.WithPermittedArgv([]string{"exit", "42", "critical error occurred"}),
	)

	ctx := context.Background()
	res, err := runner.Run(ctx, fakeCliBin, "exit", "42", "critical error occurred")
	if err == nil {
		t.Fatalf("expected error for exit 42, got nil")
	}

	if res == nil {
		t.Fatalf("expected non-nil Result on exit error")
	}
	if res.ExitCode != 42 {
		t.Errorf("expected exit code 42, got %d", res.ExitCode)
	}
	if res.StderrString() != "critical error occurred" {
		t.Errorf("expected stderr 'critical error occurred', got %q", res.StderrString())
	}
}

func TestRun_Timeout(t *testing.T) {
	runner := cliexec.New(
		cliexec.WithTimeout(50*time.Millisecond),
		cliexec.WithPermittedArgv([]string{"sleep", "2s"}),
	)

	ctx := context.Background()
	start := time.Now()
	_, err := runner.Run(ctx, fakeCliBin, "sleep", "2s")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("expected timeout error, got nil")
	}
	if elapsed > 1500*time.Millisecond {
		t.Errorf("execution took %v; hard timeout was not respected promptly", elapsed)
	}
}

func TestRun_ContextCancellation(t *testing.T) {
	runner := cliexec.New(
		cliexec.WithPermittedArgv([]string{"sleep", "5s"}),
	)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := runner.Run(ctx, fakeCliBin, "sleep", "5s")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("expected cancellation error, got nil")
	}
	if elapsed > 1500*time.Millisecond {
		t.Errorf("cancellation took %v; child process was not killed promptly", elapsed)
	}
}

func TestRun_OversizedOutput(t *testing.T) {
	capBytes := 64 * 1024 // 64 KB cap for fast test
	runner := cliexec.New(
		cliexec.WithMaxOutputBytes(capBytes),
		cliexec.WithPermittedArgv([]string{"oversized"}),
	)

	ctx := context.Background()
	res, err := runner.Run(ctx, fakeCliBin, "oversized")
	if err != nil {
		t.Fatalf("expected oversized output execution to succeed without error, got: %v", err)
	}

	if len(res.Stdout) != capBytes {
		t.Errorf("expected stdout capped at %d bytes, got %d", capBytes, len(res.Stdout))
	}
	if len(res.Stderr) != capBytes {
		t.Errorf("expected stderr capped at %d bytes, got %d", capBytes, len(res.Stderr))
	}
}

func TestRun_Default1MBOutputCap(t *testing.T) {
	runner := cliexec.New(
		cliexec.WithPermittedArgv([]string{"oversized"}),
	)

	ctx := context.Background()
	res, err := runner.Run(ctx, fakeCliBin, "oversized")
	if err != nil {
		t.Fatalf("expected execution to succeed, got: %v", err)
	}

	if len(res.Stdout) > cliexec.DefaultMaxOutputBytes {
		t.Errorf("expected stdout within 1MB cap, got %d bytes", len(res.Stdout))
	}
	if len(res.Stderr) > cliexec.DefaultMaxOutputBytes {
		t.Errorf("expected stderr within 1MB cap, got %d bytes", len(res.Stderr))
	}
}

func TestRun_EnvironmentScrubbing(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-sensitive-12345")
	t.Setenv("OPENAI_API_KEY", "sk-proj-secret-67890")
	t.Setenv("CLAUDE_CODE_ENV", "testing")
	t.Setenv("CODEX_SESSION_TOKEN", "sess-token-xyz")
	t.Setenv("SECRET_AUTH_KEY", "super-secret")
	t.Setenv("USER_CUSTOM_SYSTEM_ID", "custom-123")

	runner := cliexec.New(
		cliexec.WithPermittedArgv([]string{"dump-env"}),
	)

	ctx := context.Background()
	res, err := runner.Run(ctx, fakeCliBin, "dump-env")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := res.StdoutString()
	lines := strings.Split(out, "\n")
	envMap := make(map[string]string)
	for _, l := range lines {
		parts := strings.SplitN(l, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}

	// Sensitive / vendor variables must be scrubbed
	forbiddenVars := []string{
		"ANTHROPIC_API_KEY",
		"OPENAI_API_KEY",
		"CLAUDE_CODE_ENV",
		"CODEX_SESSION_TOKEN",
		"SECRET_AUTH_KEY",
		"USER_CUSTOM_SYSTEM_ID",
	}

	for _, k := range forbiddenVars {
		if val, exists := envMap[k]; exists {
			t.Errorf("found ambient unscrubbed variable %s=%s in child environment", k, val)
		}
	}

	// System variables must be preserved if set on host
	for _, k := range []string{"PATH", "HOME", "USER", "SHELL", "TMPDIR"} {
		if hostVal := os.Getenv(k); hostVal != "" {
			if envMap[k] != hostVal {
				t.Errorf("expected system var %s=%q to be preserved, got %q", k, hostVal, envMap[k])
			}
		}
	}
}

func TestRun_ExtraEnvAndAllowedKeys(t *testing.T) {
	runner := cliexec.New(
		cliexec.WithExtraEnv("EXPLICIT_PASSTHROUGH=opt-in-value"),
		cliexec.WithExtraAllowedEnvKeys("CUSTOM_ALLOWED_VAR"),
		cliexec.WithPermittedArgv([]string{"dump-env"}),
	)

	t.Setenv("CUSTOM_ALLOWED_VAR", "preserved-custom")

	ctx := context.Background()
	res, err := runner.Run(ctx, fakeCliBin, "dump-env")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := res.StdoutString()
	if !strings.Contains(out, "EXPLICIT_PASSTHROUGH=opt-in-value") {
		t.Errorf("expected output to contain explicit extra env, got %q", out)
	}
	if !strings.Contains(out, "CUSTOM_ALLOWED_VAR=preserved-custom") {
		t.Errorf("expected output to contain preserved custom allowed var, got %q", out)
	}
}

func TestProbeVersion_Caching(t *testing.T) {
	cliexec.ClearVersionCache()
	defer cliexec.ClearVersionCache()

	recordFile := filepath.Join(t.TempDir(), "invocations.log")
	runner := cliexec.New(
		cliexec.WithExtraEnv("RECORD_FILE=" + recordFile),
	)

	ctx := context.Background()

	// First probe
	v1, err := runner.ProbeVersion(ctx, fakeCliBin)
	if err != nil {
		t.Fatalf("first ProbeVersion failed: %v", err)
	}
	if !strings.Contains(v1, "fakecli version 2.4.0") {
		t.Fatalf("unexpected version output: %q", v1)
	}

	// Verify invocation file was written once
	data, err := os.ReadFile(recordFile)
	if err != nil {
		t.Fatalf("failed to read record file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 invocation recorded, got %d: %v", len(lines), lines)
	}

	// Second and third probes for same binary
	v2, err := runner.ProbeVersion(ctx, fakeCliBin)
	if err != nil {
		t.Fatalf("second ProbeVersion failed: %v", err)
	}
	v3, err := cliexec.ProbeVersion(ctx, fakeCliBin, cliexec.WithExtraEnv("RECORD_FILE="+recordFile))
	if err != nil {
		t.Fatalf("package-level ProbeVersion failed: %v", err)
	}

	if v2 != v1 || v3 != v1 {
		t.Errorf("mismatched version outputs: v1=%q, v2=%q, v3=%q", v1, v2, v3)
	}

	// Verify still only 1 invocation occurred
	dataAfter, err := os.ReadFile(recordFile)
	if err != nil {
		t.Fatalf("failed to read record file after second probe: %v", err)
	}
	linesAfter := strings.Split(strings.TrimSpace(string(dataAfter)), "\n")
	if len(linesAfter) != 1 {
		t.Errorf("expected exactly 1 invocation due to caching, got %d: %v", len(linesAfter), linesAfter)
	}
}

func TestProbeVersion_Concurrent(t *testing.T) {
	cliexec.ClearVersionCache()
	defer cliexec.ClearVersionCache()

	recordFile := filepath.Join(t.TempDir(), "concurrent-invocations.log")
	runner := cliexec.New(
		cliexec.WithExtraEnv("RECORD_FILE=" + recordFile),
	)

	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make(chan error, 20)

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := runner.ProbeVersion(ctx, fakeCliBin)
			if err != nil {
				errs <- err
				return
			}
			if !strings.Contains(v, "fakecli version 2.4.0") {
				errs <- fmt.Errorf("unexpected version: %q", v)
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent probe error: %v", err)
	}

	data, err := os.ReadFile(recordFile)
	if err != nil {
		t.Fatalf("failed to read record file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Errorf("expected single execution under concurrency, got %d: %v", len(lines), lines)
	}
}

func TestScrubEnv_Unit(t *testing.T) {
	rawEnv := []string{
		"PATH=/usr/bin:/bin",
		"HOME=/home/testuser",
		"ANTHROPIC_API_KEY=sk-12345",
		"OPENAI_API_KEY=sk-67890",
		"MY_PASSWORD=password123",
		"USER=testuser",
		"   ",
		"INVALID_FORMAT_WITHOUT_EQUALS",
	}

	scrubbed := cliexec.ScrubEnv(rawEnv)

	expectedMap := map[string]string{
		"PATH": "/usr/bin:/bin",
		"HOME": "/home/testuser",
		"USER": "testuser",
	}

	actualMap := make(map[string]string)
	for _, kv := range scrubbed {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) == 2 {
			actualMap[parts[0]] = parts[1]
		}
	}

	if len(actualMap) != len(expectedMap) {
		t.Fatalf("expected %d entries, got %d (%v)", len(expectedMap), len(actualMap), actualMap)
	}

	for k, v := range expectedMap {
		if actualMap[k] != v {
			t.Errorf("key %s: expected %q, got %q", k, v, actualMap[k])
		}
	}
}

func TestRun_CompletelyScrubbedEnvDoesNotInheritParent(t *testing.T) {
	t.Setenv("SENSITIVE_HOST_VAR", "should_not_leak_to_child")
	runner := cliexec.New(
		cliexec.WithEnv([]string{"UNMATCHED_VAR=123"}),
		cliexec.WithAllowedEnvKeys("NON_EXISTENT_KEY"),
		cliexec.WithPermittedArgv([]string{"dump-env"}),
	)

	ctx := context.Background()
	res, err := runner.Run(ctx, fakeCliBin, "dump-env")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := res.StdoutString()
	if strings.Contains(out, "SENSITIVE_HOST_VAR") {
		t.Errorf("ambient parent environment leaked to child when filtered env was empty: %q", out)
	}
	if strings.Contains(out, "UNMATCHED_VAR") {
		t.Errorf("unmatched variable was not scrubbed: %q", out)
	}
}

func TestRun_ZeroByteOutputCap(t *testing.T) {
	runner := cliexec.New(
		cliexec.WithMaxOutputBytes(0),
		cliexec.WithPermittedArgv([]string{"oversized"}),
	)

	ctx := context.Background()
	res, err := runner.Run(ctx, fakeCliBin, "oversized")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(res.Stdout) != 0 {
		t.Errorf("expected 0 bytes stdout with limit 0, got %d", len(res.Stdout))
	}
	if len(res.Stderr) != 0 {
		t.Errorf("expected 0 bytes stderr with limit 0, got %d", len(res.Stderr))
	}
}

func TestProbeVersion_ContextCancelWaitingCaller(t *testing.T) {
	cliexec.ClearVersionCache()
	defer cliexec.ClearVersionCache()

	cache := cliexec.NewVersionCache()
	runner := cliexec.New(
		cliexec.WithPermittedArgv([]string{"--version"}),
		cliexec.WithVersionCache(cache),
		cliexec.WithExtraEnv("FAKECLI_VERSION_DELAY=300ms"),
	)

	ctxSlow, cancelSlow := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelSlow()

	ctxFast, cancelFast := context.WithCancel(context.Background())

	startedLeader := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	// Leader caller
	go func() {
		defer wg.Done()
		close(startedLeader)
		_, _ = runner.ProbeVersion(ctxSlow, fakeCliBin)
	}()

	// Secondary caller with immediate cancel
	var fastErr error
	go func() {
		defer wg.Done()
		<-startedLeader
		time.Sleep(10 * time.Millisecond)
		cancelFast()
		_, fastErr = runner.ProbeVersion(ctxFast, fakeCliBin)
	}()

	wg.Wait()

	if !errors.Is(fastErr, context.Canceled) {
		t.Errorf("expected secondary caller to return context.Canceled, got: %v", fastErr)
	}
}
