package cliexec

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"
)

// SensitiveKeySubstrings contains substrings that identify sensitive environment variables to scrub.
var SensitiveKeySubstrings = []string{
	"KEY",
	"SECRET",
	"TOKEN",
	"PASSWORD",
	"AUTH",
	"CREDENTIAL",
	"PRIVATE",
}

// Result holds the outcome of an executed command.
type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	Duration time.Duration
}

// StdoutString returns stdout as a trimmed string.
func (r *Result) StdoutString() string {
	return strings.TrimSpace(string(r.Stdout))
}

// StderrString returns stderr as a trimmed string.
func (r *Result) StderrString() string {
	return strings.TrimSpace(string(r.Stderr))
}

// Runner executes external commands with timeouts and environment scrubbing.
type Runner struct {
	Dir          string
	Env          []string
	Timeout      time.Duration
	ScrubSecrets bool
}

// Option configures command execution.
type Option func(*Runner)

// WithDir sets the working directory for command execution.
func WithDir(dir string) Option {
	return func(r *Runner) {
		r.Dir = dir
	}
}

// WithEnv sets the base environment variables.
func WithEnv(env []string) Option {
	return func(r *Runner) {
		r.Env = env
	}
}

// WithTimeout sets a timeout for command execution.
func WithTimeout(d time.Duration) Option {
	return func(r *Runner) {
		r.Timeout = d
	}
}

// WithScrubSecrets enables or disables secret scrubbing.
func WithScrubSecrets(scrub bool) Option {
	return func(r *Runner) {
		r.ScrubSecrets = scrub
	}
}

// New creates a new command runner with default settings (secret scrubbing enabled).
func New(opts ...Option) *Runner {
	r := &Runner{
		ScrubSecrets: true,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(r)
		}
	}
	return r
}

// ScrubEnv filters out environment variables that may contain secrets or credentials.
func ScrubEnv(env []string) []string {
	var clean []string
	for _, kv := range env {
		trimmed := strings.TrimSpace(kv)
		if trimmed == "" {
			continue
		}
		parts := strings.SplitN(trimmed, "=", 2)
		if len(parts) == 0 {
			continue
		}
		key := strings.ToUpper(parts[0])
		sensitive := false
		for _, substr := range SensitiveKeySubstrings {
			if strings.Contains(key, substr) {
				sensitive = true
				break
			}
		}
		if !sensitive {
			clean = append(clean, trimmed)
		}
	}
	return clean
}

// Run executes a command with the given arguments using context and runner options.
func (r *Runner) Run(ctx context.Context, name string, args ...string) (*Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	if r.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.Timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, name, args...)

	if r.Dir != "" {
		cmd.Dir = r.Dir
	}

	var baseEnv []string
	if len(r.Env) > 0 {
		baseEnv = r.Env
	} else {
		baseEnv = os.Environ()
	}

	if r.ScrubSecrets {
		cmd.Env = ScrubEnv(baseEnv)
	} else {
		cmd.Env = baseEnv
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start)

	res := &Result{
		Stdout:   stdoutBuf.Bytes(),
		Stderr:   stderrBuf.Bytes(),
		ExitCode: 0,
		Duration: duration,
	}

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			res.ExitCode = exitErr.ExitCode()
		} else {
			res.ExitCode = -1
		}
		return res, err
	}

	return res, nil
}

// Run is a package-level helper that runs a command with default settings.
func Run(ctx context.Context, name string, args ...string) (*Result, error) {
	return New().Run(ctx, name, args...)
}
