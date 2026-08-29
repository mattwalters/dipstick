// Package cliexec provides a defensive, hermetic subprocess execution runner
// with hard timeouts, context cancellation termination, size-capped output buffers,
// binary path resolution with relative path rejection, child environment scrubbing,
// permitted argv allowlisting, and cached version probing.
package cliexec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DefaultMaxOutputBytes is the default maximum bytes captured per stream (stdout/stderr) (1MB).
const DefaultMaxOutputBytes = 1024 * 1024

var (
	// ErrRelativeBinary indicates a binary was specified or resolved to a relative path.
	ErrRelativeBinary = errors.New("cliexec: relative binary path is disallowed")

	// ErrEmptyBinary indicates no executable name or path was provided.
	ErrEmptyBinary = errors.New("cliexec: binary name cannot be empty")

	// ErrDisallowedArgv indicates an argv invocation does not match any permitted safe pattern.
	ErrDisallowedArgv = errors.New("cliexec: argv shape is not permitted by allowlist")
)

// DefaultAllowedEnvKeys contains the standard system environment variable names
// permitted in child processes to ensure safe execution without leaking ambient secrets.
var DefaultAllowedEnvKeys = []string{
	"APPDATA",
	"COLORTERM",
	"COMMONPROGRAMFILES",
	"COMSPEC",
	"HOME",
	"HOMEDRIVE",
	"HOMEPATH",
	"LANG",
	"LC_ALL",
	"LC_COLLATE",
	"LC_CTYPE",
	"LC_MESSAGES",
	"LC_MONETARY",
	"LC_NUMERIC",
	"LC_TIME",
	"LOCALAPPDATA",
	"LOGNAME",
	"PATH",
	"PATHEXT",
	"PROGRAMDATA",
	"PROGRAMFILES",
	"PROGRAMFILES(X86)",
	"SHELL",
	"SSH_AUTH_SOCK",
	"SYSTEMDRIVE",
	"SYSTEMROOT",
	"TEMP",
	"TERM",
	"TMP",
	"TMPDIR",
	"USER",
	"USERNAME",
	"USERPROFILE",
	"WINDIR",
	"XDG_CACHE_HOME",
	"XDG_CONFIG_HOME",
	"XDG_DATA_HOME",
	"XDG_RUNTIME_DIR",
}

// SensitiveKeySubstrings is retained for backward compatibility.
var SensitiveKeySubstrings = []string{
	"KEY",
	"SECRET",
	"TOKEN",
	"PASSWORD",
	"AUTH",
	"CREDENTIAL",
	"PRIVATE",
}

// DefaultPermittedArgvPatterns defines safe invocation arguments allowed by default.
var DefaultPermittedArgvPatterns = [][]string{
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

// Result holds the outcome of an executed command.
type Result struct {
	BinaryPath string
	Stdout     []byte
	Stderr     []byte
	ExitCode   int
	Duration   time.Duration
}

// StdoutString returns stdout as a trimmed string.
func (r *Result) StdoutString() string {
	return strings.TrimSpace(string(r.Stdout))
}

// StderrString returns stderr as a trimmed string.
func (r *Result) StderrString() string {
	return strings.TrimSpace(string(r.Stderr))
}

// Runner executes external commands with timeouts, environment scrubbing, and safety constraints.
type Runner struct {
	Dir                   string
	Env                   []string
	AllowedEnvKeys        map[string]struct{}
	ExtraEnv              []string
	Timeout               time.Duration
	MaxOutputBytes        int
	ScrubEnv              bool
	StrictArgv            bool
	PermittedArgvPatterns [][]string
	VersionCache          *VersionCache
}

// Option configures command execution.
type Option func(*Runner)

// WithDir sets the working directory for command execution.
func WithDir(dir string) Option {
	return func(r *Runner) {
		r.Dir = dir
	}
}

// WithEnv sets the base environment variables (defaults to os.Environ()).
func WithEnv(env []string) Option {
	return func(r *Runner) {
		r.Env = env
	}
}

// WithExtraEnv adds specific KEY=VALUE variables to the child environment.
func WithExtraEnv(env ...string) Option {
	return func(r *Runner) {
		r.ExtraEnv = append(r.ExtraEnv, env...)
	}
}

// WithAllowedEnvKeys overrides the allowlisted environment variable keys.
func WithAllowedEnvKeys(keys ...string) Option {
	return func(r *Runner) {
		r.AllowedEnvKeys = make(map[string]struct{}, len(keys))
		for _, k := range keys {
			r.AllowedEnvKeys[strings.ToUpper(strings.TrimSpace(k))] = struct{}{}
		}
	}
}

// WithExtraAllowedEnvKeys adds additional allowed environment variable keys.
func WithExtraAllowedEnvKeys(keys ...string) Option {
	return func(r *Runner) {
		if r.AllowedEnvKeys == nil {
			r.AllowedEnvKeys = make(map[string]struct{})
		}
		for _, k := range keys {
			r.AllowedEnvKeys[strings.ToUpper(strings.TrimSpace(k))] = struct{}{}
		}
	}
}

// WithTimeout sets a hard timeout for command execution.
func WithTimeout(d time.Duration) Option {
	return func(r *Runner) {
		r.Timeout = d
	}
}

// WithMaxOutputBytes sets the maximum byte size captured for stdout and stderr separately.
// A negative value denotes unbounded output. A value of 0 enforces zero-byte capture.
func WithMaxOutputBytes(n int) Option {
	return func(r *Runner) {
		r.MaxOutputBytes = n
	}
}

// WithScrubEnv enables or disables environment allowlist scrubbing (default: true).
func WithScrubEnv(scrub bool) Option {
	return func(r *Runner) {
		r.ScrubEnv = scrub
	}
}

// WithScrubSecrets is an alias for WithScrubEnv for backwards compatibility.
func WithScrubSecrets(scrub bool) Option {
	return WithScrubEnv(scrub)
}

// WithStrictArgv enables or disables argv pattern enforcement (default: true).
func WithStrictArgv(strict bool) Option {
	return func(r *Runner) {
		r.StrictArgv = strict
	}
}

// WithPermittedArgv sets the permitted argv patterns.
func WithPermittedArgv(patterns ...[]string) Option {
	return func(r *Runner) {
		r.PermittedArgvPatterns = patterns
	}
}

// WithExtraPermittedArgv adds extra permitted argv patterns.
func WithExtraPermittedArgv(patterns ...[]string) Option {
	return func(r *Runner) {
		r.PermittedArgvPatterns = append(r.PermittedArgvPatterns, patterns...)
	}
}

// WithVersionCache sets a custom VersionCache for the runner.
func WithVersionCache(vc *VersionCache) Option {
	return func(r *Runner) {
		r.VersionCache = vc
	}
}

// New creates a new command runner with default defensive settings.
func New(opts ...Option) *Runner {
	allowedKeys := make(map[string]struct{}, len(DefaultAllowedEnvKeys))
	for _, k := range DefaultAllowedEnvKeys {
		allowedKeys[strings.ToUpper(k)] = struct{}{}
	}

	patterns := make([][]string, len(DefaultPermittedArgvPatterns))
	for i, p := range DefaultPermittedArgvPatterns {
		copied := make([]string, len(p))
		copy(copied, p)
		patterns[i] = copied
	}

	r := &Runner{
		AllowedEnvKeys:        allowedKeys,
		MaxOutputBytes:        DefaultMaxOutputBytes,
		ScrubEnv:              true,
		StrictArgv:            true,
		PermittedArgvPatterns: patterns,
		VersionCache:          defaultVersionCache,
	}

	for _, opt := range opts {
		if opt != nil {
			opt(r)
		}
	}
	return r
}

// ResolveBinary finds the executable using exec.LookPath and rejects relative resolutions.
func ResolveBinary(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", ErrEmptyBinary
	}

	// Reject explicit relative paths with directory separators before or after LookPath.
	if strings.ContainsRune(trimmed, filepath.Separator) || strings.ContainsRune(trimmed, '/') {
		if !filepath.IsAbs(trimmed) {
			return "", fmt.Errorf("%w: path %q is relative", ErrRelativeBinary, name)
		}
	}

	resolved, err := exec.LookPath(trimmed)
	if err != nil {
		if errors.Is(err, exec.ErrDot) {
			return "", fmt.Errorf("%w: binary %q resolved to current directory: %w", ErrRelativeBinary, name, err)
		}
		return "", fmt.Errorf("cliexec: resolving binary %q: %w", name, err)
	}

	if !filepath.IsAbs(resolved) {
		return "", fmt.Errorf("%w: binary %q resolved to relative path %q", ErrRelativeBinary, name, resolved)
	}

	return filepath.Clean(resolved), nil
}

// ScrubEnv filters an environment variable list down to DefaultAllowedEnvKeys.
// It always returns a non-nil slice.
func ScrubEnv(env []string) []string {
	allowed := make(map[string]struct{}, len(DefaultAllowedEnvKeys))
	for _, k := range DefaultAllowedEnvKeys {
		allowed[strings.ToUpper(k)] = struct{}{}
	}
	return ScrubEnvWithAllowed(env, allowed)
}

// ScrubEnvWithAllowed filters an environment variable list against a given set of uppercase allowed keys.
// It always returns a non-nil slice.
func ScrubEnvWithAllowed(env []string, allowedKeys map[string]struct{}) []string {
	clean := make([]string, 0)
	for _, kv := range env {
		trimmed := strings.TrimSpace(kv)
		if trimmed == "" {
			continue
		}
		parts := strings.SplitN(trimmed, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.ToUpper(strings.TrimSpace(parts[0]))
		if key == "" {
			continue
		}
		if _, ok := allowedKeys[key]; ok {
			clean = append(clean, trimmed)
		}
	}
	return clean
}

// ValidateArgv asserts that args matches at least one permitted argv pattern.
func ValidateArgv(args []string, permittedPatterns [][]string) error {
	for _, pattern := range permittedPatterns {
		if len(pattern) != len(args) {
			continue
		}
		match := true
		for i := range pattern {
			if pattern[i] != args[i] {
				match = false
				break
			}
		}
		if match {
			return nil
		}
	}
	return fmt.Errorf("%w: %v", ErrDisallowedArgv, args)
}

type limitWriter struct {
	buf   bytes.Buffer
	limit int
}

func newLimitWriter(limit int) *limitWriter {
	return &limitWriter{
		limit: limit,
	}
}

func (w *limitWriter) Write(p []byte) (int, error) {
	if w.limit < 0 {
		return w.buf.Write(p)
	}
	remaining := w.limit - w.buf.Len()
	if remaining > 0 {
		if len(p) > remaining {
			w.buf.Write(p[:remaining])
		} else {
			w.buf.Write(p)
		}
	}
	// Always report writing len(p) so child process stream does not fail with EPIPE.
	return len(p), nil
}

func (w *limitWriter) Bytes() []byte {
	return w.buf.Bytes()
}

// Run executes a command with the given arguments using context and runner options.
func (r *Runner) Run(ctx context.Context, name string, args ...string) (*Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	if r.StrictArgv {
		if err := ValidateArgv(args, r.PermittedArgvPatterns); err != nil {
			return nil, err
		}
	}

	resolvedPath, err := ResolveBinary(name)
	if err != nil {
		return nil, err
	}

	if r.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.Timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, resolvedPath, args...)
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
	cmd.WaitDelay = 500 * time.Millisecond

	if r.Dir != "" {
		cmd.Dir = r.Dir
	}

	var baseEnv []string
	if r.Env != nil {
		baseEnv = r.Env
	} else {
		baseEnv = os.Environ()
	}

	var childEnv []string
	if r.ScrubEnv {
		childEnv = ScrubEnvWithAllowed(baseEnv, r.AllowedEnvKeys)
	} else {
		childEnv = make([]string, len(baseEnv))
		copy(childEnv, baseEnv)
	}

	if len(r.ExtraEnv) > 0 {
		childEnv = append(childEnv, r.ExtraEnv...)
	}

	// os/exec interprets cmd.Env == nil as an instruction to inherit the parent process os.Environ().
	// Ensure cmd.Env is always a non-nil slice (even when empty) so that an empty/scrubbed environment
	// is strictly preserved without leaking ambient parent variables.
	if childEnv == nil {
		childEnv = []string{}
	}
	cmd.Env = childEnv

	stdoutWriter := newLimitWriter(r.MaxOutputBytes)
	stderrWriter := newLimitWriter(r.MaxOutputBytes)
	cmd.Stdout = stdoutWriter
	cmd.Stderr = stderrWriter
	cmd.Stdin = nil // Explicitly ensure no PTY / stdin allocation

	start := time.Now()
	runErr := cmd.Run()
	duration := time.Since(start)

	res := &Result{
		BinaryPath: resolvedPath,
		Stdout:     stdoutWriter.Bytes(),
		Stderr:     stderrWriter.Bytes(),
		ExitCode:   0,
		Duration:   duration,
	}

	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			res.ExitCode = exitErr.ExitCode()
		} else {
			res.ExitCode = -1
		}
		return res, runErr
	}

	return res, nil
}

// Run is a package-level helper that executes a command with default settings.
func Run(ctx context.Context, name string, args ...string) (*Result, error) {
	return New().Run(ctx, name, args...)
}

type versionCall struct {
	done chan struct{}
	val  string
	err  error
}

// VersionCache is a thread-safe in-memory cache for CLI version probe outputs.
type VersionCache struct {
	mu    sync.RWMutex
	items map[string]string
	calls map[string]*versionCall
}

// NewVersionCache creates a new in-memory VersionCache.
func NewVersionCache() *VersionCache {
	return &VersionCache{
		items: make(map[string]string),
		calls: make(map[string]*versionCall),
	}
}

var defaultVersionCache = NewVersionCache()

// Clear removes all cached version entries.
func (c *VersionCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]string)
	c.calls = make(map[string]*versionCall)
}

// Get returns the cached version string for an absolute binary path if present.
func (c *VersionCache) Get(binaryPath string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.items[binaryPath]
	return v, ok
}

// Set explicitly sets the cached version for a binary path.
func (c *VersionCache) Set(binaryPath string, version string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[binaryPath] = version
}

// Probe executes a version check using the provided runner, caching the result thread-safely.
func (c *VersionCache) Probe(ctx context.Context, runner *Runner, binary string) (string, error) {
	if runner == nil {
		runner = New()
	}

	resolvedPath, err := ResolveBinary(binary)
	if err != nil {
		return "", err
	}

	c.mu.RLock()
	if cached, ok := c.items[resolvedPath]; ok {
		c.mu.RUnlock()
		return cached, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	if cached, ok := c.items[resolvedPath]; ok {
		c.mu.Unlock()
		return cached, nil
	}

	call, exists := c.calls[resolvedPath]
	if !exists {
		call = &versionCall{
			done: make(chan struct{}),
		}
		c.calls[resolvedPath] = call
		c.mu.Unlock()

		defer func() {
			c.mu.Lock()
			delete(c.calls, resolvedPath)
			c.mu.Unlock()
		}()

		res, runErr := runner.Run(ctx, resolvedPath, "--version")
		c.mu.Lock()
		if runErr != nil {
			call.err = fmt.Errorf("cliexec: probe version for %q: %w", binary, runErr)
		} else {
			out := res.StdoutString()
			if out == "" {
				out = res.StderrString()
			}
			call.val = out
			c.items[resolvedPath] = call.val
		}
		close(call.done)
		c.mu.Unlock()

		return call.val, call.err
	}

	c.mu.Unlock()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-call.done:
		return call.val, call.err
	}
}

// ProbeVersion probes the binary version using runner configuration and its VersionCache.
func (r *Runner) ProbeVersion(ctx context.Context, binary string) (string, error) {
	cache := r.VersionCache
	if cache == nil {
		cache = defaultVersionCache
	}
	return cache.Probe(ctx, r, binary)
}

// ProbeVersion probes and caches the version of the specified binary.
func ProbeVersion(ctx context.Context, binary string, opts ...Option) (string, error) {
	r := New(opts...)
	return r.ProbeVersion(ctx, binary)
}

// ClearVersionCache clears the default package-level version cache.
func ClearVersionCache() {
	defaultVersionCache.Clear()
}
