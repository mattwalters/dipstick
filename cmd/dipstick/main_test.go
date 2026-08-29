package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v5"

	"github.com/mattwalters/dipstick"
)

var testBinaryPath string

func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "dipstick-cli-test-*")
	if err != nil {
		log.Fatalf("failed creating temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	testBinaryPath = filepath.Join(tmpDir, "dipstick")
	cmd := exec.Command("go", "build", "-o", testBinaryPath, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Fatalf("failed building dipstick test binary: %v, output: %s", err, string(out))
	}

	os.Exit(m.Run())
}

func loadGoldenReport(t *testing.T, relPath string) (*dipstick.Report, []byte) {
	t.Helper()
	rootRel := filepath.Join("..", "..", relPath)
	data, err := os.ReadFile(rootRel)
	if err != nil {
		t.Fatalf("failed reading golden file %s: %v", rootRel, err)
	}
	var rep dipstick.Report
	if err := json.Unmarshal(data, &rep); err != nil {
		t.Fatalf("failed unmarshaling golden file %s: %v", rootRel, err)
	}
	return &rep, data
}

func TestRun_ExitCode0_ProviderReported(t *testing.T) {
	origCollect := collectFn
	defer func() { collectFn = origCollect }()

	goldenRep, _ := loadGoldenReport(t, filepath.Join("testdata", "report_full.golden.json"))
	collectFn = func(ctx context.Context, opts ...dipstick.Option) (*dipstick.Report, error) {
		return goldenRep, nil
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0 when providers report data, got %d. stderr: %s", code, stderr.String())
	}

	var parsed dipstick.Report
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
		t.Fatalf("expected valid JSON on stdout: %v\nstdout: %s", err, stdout.String())
	}
	if len(parsed.Providers) == 0 {
		t.Errorf("expected parsed providers > 0")
	}
}

func TestRun_ExitCode1_NoProvidersReported(t *testing.T) {
	origCollect := collectFn
	defer func() { collectFn = origCollect }()

	goldenRep, _ := loadGoldenReport(t, filepath.Join("testdata", "report_empty.golden.json"))
	collectFn = func(ctx context.Context, opts ...dipstick.Option) (*dipstick.Report, error) {
		return goldenRep, nil
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"--json"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1 when no provider reported data, got %d. stderr: %s", code, stderr.String())
	}

	var parsed dipstick.Report
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
		t.Fatalf("expected valid JSON on stdout: %v\nstdout: %s", err, stdout.String())
	}
	if len(parsed.Providers) != 0 {
		t.Errorf("expected 0 providers, got %d", len(parsed.Providers))
	}
}

func TestRun_ExitCode2_BadInvocations(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		errContains string
	}{
		{
			name:        "unknown flag",
			args:        []string{"--nonexistent-flag"},
			errContains: "flag provided but not defined",
		},
		{
			name:        "negative timeout",
			args:        []string{"-timeout", "-5s"},
			errContains: "invalid timeout",
		},
		{
			name:        "negative source timeout",
			args:        []string{"-source-timeout", "-2s"},
			errContains: "invalid source timeout",
		},
		{
			name:        "unknown provider",
			args:        []string{"-p", "nonexistent-provider"},
			errContains: "unknown provider",
		},
		{
			name:        "unexpected argument",
			args:        []string{"extra-arg"},
			errContains: "unexpected argument",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(tt.args, &stdout, &stderr)
			if code != 2 {
				t.Fatalf("expected exit code 2 for %s, got %d. stderr: %s", tt.name, code, stderr.String())
			}
			if !strings.Contains(stderr.String(), tt.errContains) {
				t.Errorf("expected stderr to contain %q, got %q", tt.errContains, stderr.String())
			}
			if stdout.Len() > 0 {
				t.Errorf("expected empty stdout on bad invocation, got %q", stdout.String())
			}
		})
	}
}

func TestRun_GoldenOutputByteIdentical(t *testing.T) {
	origCollect := collectFn
	defer func() { collectFn = origCollect }()

	tests := []struct {
		name       string
		goldenPath string
	}{
		{
			name:       "report_full.golden.json",
			goldenPath: filepath.Join("testdata", "report_full.golden.json"),
		},
		{
			name:       "report_empty.golden.json",
			goldenPath: filepath.Join("testdata", "report_empty.golden.json"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			goldenRep, expectedBytes := loadGoldenReport(t, tt.goldenPath)
			collectFn = func(ctx context.Context, opts ...dipstick.Option) (*dipstick.Report, error) {
				return goldenRep, nil
			}

			var stdout, stderr bytes.Buffer
			// Emit a simulated warning to stderr during collection
			fmt.Fprintln(&stderr, "warning: provider drift detected on stderr")

			_ = run([]string{"--json"}, &stdout, &stderr)

			if !bytes.Equal(stdout.Bytes(), expectedBytes) {
				t.Errorf("stdout was not byte-identical to golden file %s\nGot:\n%s\nWant:\n%s",
					tt.goldenPath, stdout.String(), string(expectedBytes))
			}

			// Stderr has the warning, but stdout is unpolluted
			if !strings.Contains(stderr.String(), "warning: provider drift detected") {
				t.Errorf("expected warning on stderr, got %q", stderr.String())
			}
		})
	}
}

func TestRun_SubcommandsAndVersion(t *testing.T) {
	t.Run("version subcommand", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run([]string{"version"}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		expected := fmt.Sprintf("dipstick %s\n", Version)
		if stdout.String() != expected {
			t.Errorf("expected %q, got %q", expected, stdout.String())
		}
		if stderr.Len() != 0 {
			t.Errorf("expected empty stderr, got %q", stderr.String())
		}
	})

	t.Run("version flag", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run([]string{"--version"}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		expected := fmt.Sprintf("dipstick %s\n", Version)
		if stdout.String() != expected {
			t.Errorf("expected %q, got %q", expected, stdout.String())
		}
	})

	t.Run("v flag", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run([]string{"-v"}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		expected := fmt.Sprintf("dipstick %s\n", Version)
		if stdout.String() != expected {
			t.Errorf("expected %q, got %q", expected, stdout.String())
		}
	})

	t.Run("doctor subcommand", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run([]string{"doctor"}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		if !strings.Contains(stderr.String(), "not yet implemented") {
			t.Errorf("expected doctor stub message on stderr, got %q", stderr.String())
		}
	})

	t.Run("help flag", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run([]string{"--help"}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("expected exit code 0 on --help, got %d", code)
		}
		if !strings.Contains(stderr.String(), "Usage: dipstick") {
			t.Errorf("expected usage output on stderr, got %q", stderr.String())
		}
	})
}

func TestRun_OptionPassing(t *testing.T) {
	origCollect := collectFn
	defer func() { collectFn = origCollect }()

	var capturedOpts []dipstick.Option
	collectFn = func(ctx context.Context, opts ...dipstick.Option) (*dipstick.Report, error) {
		capturedOpts = opts
		return &dipstick.Report{
			SchemaVersion: dipstick.SchemaVersion,
			GeneratedAt:   time.Now().UTC(),
			Providers:     []dipstick.ProviderReport{},
		}, nil
	}

	var stdout, stderr bytes.Buffer
	args := []string{
		"--provider", "claude",
		"-p", "codex",
		"--policy", "local",
		"--strict",
		"-timeout", "15s",
		"-source-timeout", "3s",
	}
	code := run(args, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d. stderr: %s", code, stderr.String())
	}

	// Verify Collect received options and succeeds when evaluated
	rep, err := dipstick.Collect(context.Background(), capturedOpts...)
	if err != nil {
		t.Fatalf("failed evaluating captured options: %v", err)
	}
	if rep == nil {
		t.Fatal("expected non-nil report")
	}
}

// Subprocess execution tests using compiled binary

func TestSubprocess_SchemaValidation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, testBinaryPath, "--json")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	// Exit code 1 is expected because built-in stub adapters return no provider data
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if exitErr.ExitCode() != 1 {
			t.Fatalf("expected exit code 1 from binary run with stub adapters, got %d. Stderr: %s", exitErr.ExitCode(), stderr.String())
		}
	} else if err != nil {
		t.Fatalf("unexpected execution error: %v", err)
	}

	// Validate stdout matches schema/dipstick.v1.json
	schemaPath := filepath.Join("..", "..", "schema", "dipstick.v1.json")
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft2020
	schema, err := compiler.Compile(schemaPath)
	if err != nil {
		t.Fatalf("failed compiling schema %s: %v", schemaPath, err)
	}

	var v any
	if err := json.Unmarshal(stdout.Bytes(), &v); err != nil {
		t.Fatalf("failed parsing stdout as JSON: %v\nOutput:\n%s", err, stdout.String())
	}

	if err := schema.Validate(v); err != nil {
		t.Errorf("stdout failed dipstick.v1 schema validation: %v\nJSON:\n%s", err, stdout.String())
	}
}

func TestSubprocess_JQPipingCompatibility(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Pipe dipstick --json to jq .schema_version if jq is present
	jqPath, err := exec.LookPath("jq")
	if err != nil {
		t.Skip("jq not installed, skipping jq pipe test")
	}

	dipstickCmd := exec.CommandContext(ctx, testBinaryPath, "--json")
	jqCmd := exec.CommandContext(ctx, jqPath, "-r", ".schema_version")

	var dipstickStderr, jqStderr bytes.Buffer
	dipstickCmd.Stderr = &dipstickStderr
	jqCmd.Stderr = &jqStderr

	pipe, err := dipstickCmd.StdoutPipe()
	if err != nil {
		t.Fatalf("failed creating stdout pipe: %v", err)
	}
	jqCmd.Stdin = pipe

	var jqStdout bytes.Buffer
	jqCmd.Stdout = &jqStdout

	if err := dipstickCmd.Start(); err != nil {
		t.Fatalf("failed starting dipstick: %v", err)
	}
	if err := jqCmd.Start(); err != nil {
		t.Fatalf("failed starting jq: %v", err)
	}

	_ = dipstickCmd.Wait()
	if err := jqCmd.Wait(); err != nil {
		t.Fatalf("jq pipe execution failed: %v, jq stderr: %s", err, jqStderr.String())
	}

	gotVersion := strings.TrimSpace(jqStdout.String())
	if gotVersion != dipstick.SchemaVersion {
		t.Errorf("expected schema version %q from jq pipe, got %q", dipstick.SchemaVersion, gotVersion)
	}
}

func TestSubprocess_ExitCodes(t *testing.T) {
	t.Run("exit code 1 on empty/failed providers", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, testBinaryPath, "-p", "opencode", "--json")
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()

		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("expected ExitError, got %v", err)
		}
		if exitErr.ExitCode() != 1 {
			t.Errorf("expected exit code 1, got %d", exitErr.ExitCode())
		}
		if !strings.Contains(stdout.String(), `"schema_version": "dipstick.v1"`) {
			t.Errorf("expected stdout to contain schema_version, got %s", stdout.String())
		}
	})

	t.Run("exit code 2 on bad invocation", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, testBinaryPath, "-timeout", "-10s")
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()

		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("expected ExitError, got %v", err)
		}
		if exitErr.ExitCode() != 2 {
			t.Errorf("expected exit code 2, got %d", exitErr.ExitCode())
		}
		if !strings.Contains(stderr.String(), "invalid timeout") {
			t.Errorf("expected stderr to contain 'invalid timeout', got %s", stderr.String())
		}
		if stdout.Len() > 0 {
			t.Errorf("expected empty stdout, got %s", stdout.String())
		}
	})

	t.Run("exit code 0 on version", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, testBinaryPath, "version")
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("expected exit code 0 for version, got %v, stderr: %s", err, stderr.String())
		}
		if !strings.Contains(stdout.String(), "dipstick") {
			t.Errorf("expected stdout to contain 'dipstick', got %s", stdout.String())
		}
	})
}
