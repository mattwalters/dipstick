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

func loadGoldenDoctorReport(t *testing.T, relPath string) (*dipstick.DoctorReport, []byte) {
	t.Helper()
	rootRel := filepath.Join("..", "..", relPath)
	data, err := os.ReadFile(rootRel)
	if err != nil {
		t.Fatalf("failed reading golden file %s: %v", rootRel, err)
	}
	var rep dipstick.DoctorReport
	if err := json.Unmarshal(data, &rep); err != nil {
		t.Fatalf("failed unmarshaling golden file %s: %v", rootRel, err)
	}
	return &rep, data
}

func loadGoldenText(t *testing.T, relPath string) string {
	t.Helper()
	rootRel := filepath.Join("..", "..", relPath)
	data, err := os.ReadFile(rootRel)
	if err != nil {
		t.Fatalf("failed reading golden file %s: %v", rootRel, err)
	}
	return string(data)
}

// newClaudeTranscriptDir builds an isolated CLAUDE_CONFIG_DIR containing a
// single project transcript, so transcript-tier tests do not depend on whatever
// Claude state happens to exist on the host running them.
func newClaudeTranscriptDir(t *testing.T) string {
	t.Helper()
	configDir := t.TempDir()
	projectDir := filepath.Join(configDir, "projects", "project-cli")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("failed creating transcript project dir: %v", err)
	}

	lines := []string{
		`{"type":"user","timestamp":"2026-08-20T10:00:00Z","sessionId":"sess-cli-1","message":{"role":"user","content":"Hello world"}}`,
		`{"type":"assistant","timestamp":"2026-08-20T10:00:05Z","sessionId":"sess-cli-1","message":{"id":"msg-cli-1","model":"claude-3-7-sonnet","role":"assistant","usage":{"input_tokens":100,"output_tokens":50,"cache_creation_input_tokens":20,"cache_read_input_tokens":30}}}`,
	}
	transcript := filepath.Join(projectDir, "session-1.jsonl")
	if err := os.WriteFile(transcript, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("failed writing transcript fixture: %v", err)
	}
	return configDir
}

// envWithClaudeConfigDir returns the current environment with CLAUDE_CONFIG_DIR
// replaced by dir. Existing entries are dropped rather than shadowed, since
// duplicate keys in an exec environment resolve inconsistently across platforms.
func envWithClaudeConfigDir(dir string) []string {
	const key = "CLAUDE_CONFIG_DIR="
	base := os.Environ()
	out := make([]string, 0, len(base)+1)
	for _, kv := range base {
		if strings.HasPrefix(kv, key) {
			continue
		}
		out = append(out, kv)
	}
	return append(out, key+dir)
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
		{
			name:        "invalid since flag",
			args:        []string{"--since", "not-a-date"},
			errContains: "invalid --since value",
		},
		{
			name:        "negative since duration",
			args:        []string{"--since", "-24h"},
			errContains: "invalid --since value",
		},
		{
			name:        "doctor invalid since flag",
			args:        []string{"doctor", "--since", "bad-date"},
			errContains: "invalid --since value",
		},
		{
			name:        "doctor unknown flag",
			args:        []string{"doctor", "--nonexistent-flag"},
			errContains: "flag provided but not defined",
		},
		{
			name:        "doctor negative timeout",
			args:        []string{"doctor", "-timeout", "-5s"},
			errContains: "invalid timeout",
		},
		{
			name:        "doctor negative source timeout",
			args:        []string{"doctor", "-source-timeout", "-2s"},
			errContains: "invalid source timeout",
		},
		{
			name:        "doctor unexpected argument",
			args:        []string{"doctor", "unexpected-arg"},
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

func TestRun_Doctor_GoldenOutput(t *testing.T) {
	origDoctor := doctorFn
	defer func() { doctorFn = origDoctor }()

	goldenDoctorRep, goldenDoctorJSON := loadGoldenDoctorReport(t, filepath.Join("testdata", "doctor_full.golden.json"))
	goldenDoctorText := loadGoldenText(t, filepath.Join("testdata", "doctor_full.golden.txt"))

	doctorFn = func(ctx context.Context, opts ...dipstick.Option) (*dipstick.DoctorReport, error) {
		return goldenDoctorRep, nil
	}

	t.Run("doctor human readable text full", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run([]string{"doctor"}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d. stderr: %s", code, stderr.String())
		}
		if stdout.String() != goldenDoctorText {
			t.Errorf("doctor text output mismatch.\nGot:\n%s\nWant:\n%s", stdout.String(), goldenDoctorText)
		}
	})

	t.Run("doctor --json flag full", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run([]string{"doctor", "--json"}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d. stderr: %s", code, stderr.String())
		}
		if !bytes.Equal(stdout.Bytes(), goldenDoctorJSON) {
			t.Errorf("doctor json output mismatch.\nGot:\n%s\nWant:\n%s", stdout.String(), string(goldenDoctorJSON))
		}
	})

	t.Run("--doctor flag alias", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run([]string{"--doctor"}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("expected exit code 0 on --doctor, got %d. stderr: %s", code, stderr.String())
		}
		if stdout.String() != goldenDoctorText {
			t.Errorf("doctor text output mismatch on --doctor flag.\nGot:\n%s\nWant:\n%s", stdout.String(), goldenDoctorText)
		}
	})

	t.Run("--doctor flag with --json", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run([]string{"--doctor", "--json"}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("expected exit code 0 on --doctor --json, got %d. stderr: %s", code, stderr.String())
		}
		if !bytes.Equal(stdout.Bytes(), goldenDoctorJSON) {
			t.Errorf("doctor json mismatch on --doctor --json.\nGot:\n%s\nWant:\n%s", stdout.String(), string(goldenDoctorJSON))
		}
	})

	t.Run("--doctor flag with options", func(t *testing.T) {
		var capturedOpts []dipstick.Option
		doctorFn = func(ctx context.Context, opts ...dipstick.Option) (*dipstick.DoctorReport, error) {
			capturedOpts = opts
			return goldenDoctorRep, nil
		}

		var stdout, stderr bytes.Buffer
		code := run([]string{"--doctor", "-p", "claude", "-timeout", "10s", "-source-timeout", "2s", "--policy", "local", "--strict"}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d. stderr: %s", code, stderr.String())
		}
		if len(capturedOpts) == 0 {
			t.Fatalf("expected options captured from --doctor with flags")
		}
	})

	fixtures := []struct {
		name     string
		jsonPath string
		txtPath  string
	}{
		{
			name:     "working",
			jsonPath: filepath.Join("testdata", "doctor_working.golden.json"),
			txtPath:  filepath.Join("testdata", "doctor_working.golden.txt"),
		},
		{
			name:     "degraded",
			jsonPath: filepath.Join("testdata", "doctor_degraded.golden.json"),
			txtPath:  filepath.Join("testdata", "doctor_degraded.golden.txt"),
		},
		{
			name:     "absent",
			jsonPath: filepath.Join("testdata", "doctor_absent.golden.json"),
			txtPath:  filepath.Join("testdata", "doctor_absent.golden.txt"),
		},
	}

	for _, fix := range fixtures {
		t.Run("fixture_"+fix.name, func(t *testing.T) {
			fixRep, fixJSON := loadGoldenDoctorReport(t, fix.jsonPath)
			fixText := loadGoldenText(t, fix.txtPath)

			doctorFn = func(ctx context.Context, opts ...dipstick.Option) (*dipstick.DoctorReport, error) {
				return fixRep, nil
			}

			var txtStdout, txtStderr bytes.Buffer
			if code := run([]string{"doctor"}, &txtStdout, &txtStderr); code != 0 {
				t.Fatalf("expected exit code 0, got %d", code)
			}
			if txtStdout.String() != fixText {
				t.Errorf("%s text mismatch.\nGot:\n%s\nWant:\n%s", fix.name, txtStdout.String(), fixText)
			}

			var jsonStdout, jsonStderr bytes.Buffer
			if code := run([]string{"doctor", "--json"}, &jsonStdout, &jsonStderr); code != 0 {
				t.Fatalf("expected exit code 0, got %d", code)
			}
			if !bytes.Equal(jsonStdout.Bytes(), fixJSON) {
				t.Errorf("%s json mismatch.\nGot:\n%s\nWant:\n%s", fix.name, jsonStdout.String(), string(fixJSON))
			}
		})
	}
}

func TestRun_SubcommandsAndVersion(t *testing.T) {
	expectedVersion := fmt.Sprintf("dipstick %s (commit: %s, built: %s)\n", Version, Commit, Date)

	t.Run("version subcommand", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run([]string{"version"}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		if stdout.String() != expectedVersion {
			t.Errorf("expected %q, got %q", expectedVersion, stdout.String())
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
		if stdout.String() != expectedVersion {
			t.Errorf("expected %q, got %q", expectedVersion, stdout.String())
		}
	})

	t.Run("v flag", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run([]string{"-v"}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		if stdout.String() != expectedVersion {
			t.Errorf("expected %q, got %q", expectedVersion, stdout.String())
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

	t.Run("doctor help flag", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run([]string{"doctor", "--help"}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("expected exit code 0 on doctor --help, got %d", code)
		}
		if !strings.Contains(stderr.String(), "Usage: dipstick doctor") {
			t.Errorf("expected doctor usage on stderr, got %q", stderr.String())
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
		"--since", "24h",
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

func TestRun_StrictFlag(t *testing.T) {
	origCollect := collectFn
	defer func() { collectFn = origCollect }()

	var receivedOpts []dipstick.Option
	collectFn = func(ctx context.Context, opts ...dipstick.Option) (*dipstick.Report, error) {
		receivedOpts = opts
		return &dipstick.Report{
			SchemaVersion: dipstick.SchemaVersion,
			GeneratedAt:   time.Now().UTC(),
			Providers:     []dipstick.ProviderReport{},
		}, nil
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"--strict", "--json"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}

	if len(receivedOpts) == 0 {
		t.Fatalf("expected options to be passed to collectFn")
	}
}

func TestParseSince(t *testing.T) {
	refNow := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		input       string
		expected    time.Time
		expectError bool
	}{
		{
			input:    "",
			expected: time.Time{},
		},
		{
			input:    "24h",
			expected: refNow.Add(-24 * time.Hour),
		},
		{
			input:    "7d",
			expected: refNow.Add(-7 * 24 * time.Hour),
		},
		{
			input:    "2w",
			expected: refNow.Add(-14 * 24 * time.Hour),
		},
		{
			input:    "2026-08-20T10:00:00Z",
			expected: time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC),
		},
		{
			input:    "2026-08-20",
			expected: time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC),
		},
		{
			input:       "-5h",
			expectError: true,
		},
		{
			input:       "invalid-date",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseSince(tt.input, refNow)
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error for input %q, got %v", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for input %q: %v", tt.input, err)
			}
			if !got.Equal(tt.expected) {
				t.Errorf("parseSince(%q): want %v, got %v", tt.input, tt.expected, got)
			}
		})
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
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if exitErr.ExitCode() != 0 && exitErr.ExitCode() != 1 {
			t.Fatalf("expected exit code 0 or 1, got %d. Stderr: %s", exitErr.ExitCode(), stderr.String())
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

func TestSubprocess_DoctorOutput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, testBinaryPath, "doctor")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		t.Fatalf("expected exit code 0 from dipstick doctor, got %v. stderr: %s", err, stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "claude") && !strings.Contains(out, "codex") && !strings.Contains(out, "antigravity") {
		t.Errorf("expected doctor output to contain providers, got:\n%s", out)
	}
}

func TestSubprocess_DoctorJSON(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, testBinaryPath, "doctor", "--json")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		t.Fatalf("expected exit code 0 from dipstick doctor --json, got %v. stderr: %s", err, stderr.String())
	}

	var docReport dipstick.DoctorReport
	if err := json.Unmarshal(stdout.Bytes(), &docReport); err != nil {
		t.Fatalf("expected valid DoctorReport JSON on stdout: %v\nOutput:\n%s", err, stdout.String())
	}
	if len(docReport.Providers) == 0 {
		t.Errorf("expected providers in doctor report")
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

		cmd := exec.CommandContext(ctx, testBinaryPath, "-p", "antigravity", "--json")
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

	t.Run("exit code 0 on version subcommand", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, testBinaryPath, "version")
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("expected exit code 0 for version, got %v, stderr: %s", err, stderr.String())
		}
		expected := "dipstick dev (commit: none, built: unknown)\n"
		if stdout.String() != expected {
			t.Errorf("expected %q, got %q", expected, stdout.String())
		}
	})

	t.Run("exit code 0 on --version flag", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, testBinaryPath, "--version")
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("expected exit code 0 for --version, got %v, stderr: %s", err, stderr.String())
		}
		expected := "dipstick dev (commit: none, built: unknown)\n"
		if stdout.String() != expected {
			t.Errorf("expected %q, got %q", expected, stdout.String())
		}
	})

	t.Run("exit code 0 on injected metadata", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		ldflags := "-X main.Version=v0.1.0 -X main.Commit=abcdef1 -X main.Date=2026-08-29T20:00:00Z"
		cmd := exec.CommandContext(ctx, "go", "run", "-ldflags", ldflags, ".", "version")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("go run failed: %v, output: %s", err, string(out))
		}

		expected := "dipstick v0.1.0 (commit: abcdef1, built: 2026-08-29T20:00:00Z)\n"
		if string(out) != expected {
			t.Errorf("expected version output %q, got %q", expected, string(out))
		}
	})

	t.Run("exit code 1 on pretty empty/failed providers", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, testBinaryPath, "-p", "antigravity", "--pretty")
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
		if !strings.Contains(stdout.String(), "antigravity") {
			t.Errorf("expected stdout to contain provider names in pretty format, got %s", stdout.String())
		}
	})

	t.Run("subprocess offline policy transcript collection", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		configDir := newClaudeTranscriptDir(t)

		cmd := exec.CommandContext(ctx, testBinaryPath, "-p", "claude", "--policy", "offline", "--json")
		cmd.Env = envWithClaudeConfigDir(configDir)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		if err != nil {
			t.Fatalf("expected exit code 0 on offline claude scan, got %v. stderr: %s", err, stderr.String())
		}

		var rep dipstick.Report
		if err := json.Unmarshal(stdout.Bytes(), &rep); err != nil {
			t.Fatalf("failed unmarshaling report: %v\nstdout: %s", err, stdout.String())
		}

		if len(rep.Providers) != 1 {
			t.Fatalf("expected 1 provider in report, got %d", len(rep.Providers))
		}

		p := rep.Providers[0]
		if p.Provider != dipstick.ProviderClaude {
			t.Errorf("Provider: got %q, want claude", p.Provider)
		}
		if p.Source != dipstick.SourceTranscript {
			t.Errorf("Source: got %q, want transcript", p.Source)
		}
		if p.Confidence != dipstick.ConfidenceDerived {
			t.Errorf("Confidence: got %q, want derived", p.Confidence)
		}
		if len(p.Windows) != 0 {
			t.Errorf("Windows: expected empty windows from transcript source, got %d", len(p.Windows))
		}
		if p.Tokens == nil {
			t.Fatalf("Tokens: expected non-nil token usage")
		}

		// Totals are fixed by the fixture written in newClaudeTranscriptDir.
		for _, tc := range []struct {
			name string
			got  *int64
			want int64
		}{
			{"InputTokens", p.Tokens.InputTokens, 100},
			{"OutputTokens", p.Tokens.OutputTokens, 50},
			{"CacheWriteTokens", p.Tokens.CacheWriteTokens, 20},
			{"CacheReadTokens", p.Tokens.CacheReadTokens, 30},
		} {
			if tc.got == nil {
				t.Errorf("%s: got nil, want %d", tc.name, tc.want)
				continue
			}
			if *tc.got != tc.want {
				t.Errorf("%s: got %d, want %d", tc.name, *tc.got, tc.want)
			}
		}
	})
}
