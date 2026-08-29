package opencode

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/mattwalters/dipstick/internal/cliexec"
	"github.com/mattwalters/dipstick/internal/localstate"
)

var (
	// ErrParseFailed indicates output could not be parsed.
	ErrParseFailed = errors.New("parse failed")
	// ErrUpstreamError indicates an upstream error or unexpected status code.
	ErrUpstreamError = errors.New("upstream error")
)

var semverRegex = regexp.MustCompile(`([0-9]+\.[0-9]+\.[0-9]+(?:-[a-zA-Z0-9.]+)?(?:\+[a-zA-Z0-9.]+)?)`)

// Detection captures OpenCode installation and authentication status.
type Detection struct {
	Installed     bool   `json:"installed"`
	Authenticated bool   `json:"authenticated"`
	Version       string `json:"version,omitempty"`
	BinaryPath    string `json:"binary_path,omitempty"`
}

// TokenUsage records token consumption counters for OpenCode.
type TokenUsage struct {
	InputTokens      int64
	OutputTokens     int64
	ReasoningTokens  int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	TotalTokens      int64
	ObservedAt       time.Time
	CLIVersion       string
}

// Adapter provides usage collection for the OpenCode coding agent.
type Adapter struct {
	resolver   *localstate.Resolver
	runner     *cliexec.Runner
	httpClient *http.Client
	serverURL  string
	now        func() time.Time
}

// Option configures an OpenCode Adapter.
type Option func(*Adapter)

// WithResolver sets the localstate resolver.
func WithResolver(r *localstate.Resolver) Option {
	return func(a *Adapter) {
		if r != nil {
			a.resolver = r
		}
	}
}

// WithRunner sets the command execution runner.
func WithRunner(r *cliexec.Runner) Option {
	return func(a *Adapter) {
		if r != nil {
			a.runner = r
		}
	}
}

// WithHTTPClient sets the HTTP client for local RPC queries.
func WithHTTPClient(client *http.Client) Option {
	return func(a *Adapter) {
		if client != nil {
			a.httpClient = client
		}
	}
}

// WithServerURL sets the base URL for local RPC queries.
func WithServerURL(url string) Option {
	return func(a *Adapter) {
		if url != "" {
			a.serverURL = strings.TrimRight(url, "/")
		}
	}
}

// WithNow sets the time provider function.
func WithNow(fn func() time.Time) Option {
	return func(a *Adapter) {
		if fn != nil {
			a.now = fn
		}
	}
}

// New creates a new OpenCode adapter instance.
func New(opts ...Option) *Adapter {
	a := &Adapter{
		resolver: localstate.New(),
		runner: cliexec.New(
			cliexec.WithStrictArgv(false),
		),
		httpClient: &http.Client{Timeout: 5 * time.Second},
		serverURL:  "http://127.0.0.1:4096",
		now:        time.Now,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(a)
		}
	}
	return a
}

// Name returns the provider identifier string.
func (a *Adapter) Name() string {
	return "opencode"
}

// Detect inspects the local environment to determine OpenCode installation, auth, and version state.
func (a *Adapter) Detect(ctx context.Context) (Detection, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	var detection Detection

	// 1. Probe binary
	binPath, err := cliexec.ResolveBinary("opencode")
	if err == nil {
		detection.Installed = true
		detection.BinaryPath = binPath
		vStr, vErr := a.runner.ProbeVersion(ctx, binPath)
		if vErr == nil {
			detection.Version = parseVersion(vStr)
		}
	}

	// 2. Check local database/filesystem state
	paths, pathsErr := a.resolver.OpenCodePaths()
	if pathsErr == nil {
		if fi, statErr := os.Stat(paths.DBFile); statErr == nil && !fi.IsDir() {
			detection.Installed = true
		}
		if a.checkAuthenticated(ctx, paths) {
			detection.Authenticated = true
		}
	}

	return detection, nil
}

func (a *Adapter) getenv(key string) string {
	if a.resolver != nil {
		return a.resolver.Getenv(key)
	}
	return os.Getenv(key)
}

func (a *Adapter) checkAuthenticated(ctx context.Context, paths *localstate.OpenCodePaths) bool {
	if paths != nil && paths.AuthFile != "" {
		if fi, err := os.Stat(paths.AuthFile); err == nil && !fi.IsDir() && fi.Size() > 0 {
			return true
		}
	}

	envKeys := []string{
		"OPENCODE_AUTH_CONTENT",
		"OPENAI_API_KEY",
		"ANTHROPIC_API_KEY",
		"OPENROUTER_API_KEY",
	}
	for _, key := range envKeys {
		if val := a.getenv(key); strings.TrimSpace(val) != "" {
			return true
		}
	}

	return false
}

func parseVersion(output string) string {
	match := semverRegex.FindString(output)
	if match != "" {
		return match
	}
	return strings.TrimSpace(output)
}

// AvailableLocalState checks if the SQLite database is present and non-empty.
func (a *Adapter) AvailableLocalState(ctx context.Context) bool {
	paths, err := a.resolver.OpenCodePaths()
	if err != nil {
		return false
	}
	fi, err := os.Stat(paths.DBFile)
	return err == nil && !fi.IsDir() && fi.Size() > 0
}

// FetchLocalState reads token usage directly from the SQLite database in read-only mode.
func (a *Adapter) FetchLocalState(ctx context.Context) (*TokenUsage, error) {
	paths, err := a.resolver.OpenCodePaths()
	if err != nil {
		return nil, fmt.Errorf("resolving opencode paths: %w", err)
	}

	dsn := fmt.Sprintf("file:%s?mode=ro&_busy_timeout=5000", paths.DBFile)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite database: %w", err)
	}
	defer func() { _ = db.Close() }()

	query := `SELECT 
		COALESCE(SUM(tokens_input), 0),
		COALESCE(SUM(tokens_output), 0),
		COALESCE(SUM(tokens_reasoning), 0),
		COALESCE(SUM(tokens_cache_read), 0),
		COALESCE(SUM(tokens_cache_write), 0),
		COALESCE(MAX(time_updated), 0)
	FROM session;`

	var input, output, reasoning, cacheRead, cacheWrite, maxTimeUpdated int64
	row := db.QueryRowContext(ctx, query)
	if err := row.Scan(&input, &output, &reasoning, &cacheRead, &cacheWrite, &maxTimeUpdated); err != nil {
		return nil, fmt.Errorf("querying session table: %w", err)
	}

	total := input + output + reasoning + cacheRead + cacheWrite

	var observedAt time.Time
	if maxTimeUpdated > 0 {
		observedAt = time.UnixMilli(maxTimeUpdated).UTC()
	} else {
		observedAt = a.now().UTC()
	}

	var cliVersion string
	if det, err := a.Detect(ctx); err == nil && det.Version != "" {
		cliVersion = det.Version
	}

	return &TokenUsage{
		InputTokens:      input,
		OutputTokens:     output,
		ReasoningTokens:  reasoning,
		CacheReadTokens:  cacheRead,
		CacheWriteTokens: cacheWrite,
		TotalTokens:      total,
		ObservedAt:       observedAt,
		CLIVersion:       cliVersion,
	}, nil
}

// AvailableRPC checks if the local RPC server endpoint is active.
func (a *Adapter) AvailableRPC(ctx context.Context) bool {
	client := a.httpClient
	if client == nil {
		client = &http.Client{Timeout: 500 * time.Millisecond}
	}
	endpoint := strings.TrimRight(a.serverURL, "/") + "/session"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode == http.StatusOK
}

type rawSession struct {
	ID               string          `json:"id"`
	TokensInput      *int64          `json:"tokens_input,omitempty"`
	TokensOutput     *int64          `json:"tokens_output,omitempty"`
	TokensReasoning  *int64          `json:"tokens_reasoning,omitempty"`
	TokensCacheRead  *int64          `json:"tokens_cache_read,omitempty"`
	TokensCacheWrite *int64          `json:"tokens_cache_write,omitempty"`
	TimeUpdated      *int64          `json:"time_updated,omitempty"`
	Tokens           *rawNestedToken `json:"tokens,omitempty"`
	Time             *rawNestedTime  `json:"time,omitempty"`
}

type rawNestedToken struct {
	Input     *int64          `json:"input,omitempty"`
	Output    *int64          `json:"output,omitempty"`
	Reasoning *int64          `json:"reasoning,omitempty"`
	Cache     *rawNestedCache `json:"cache,omitempty"`
}

type rawNestedCache struct {
	Read  *int64 `json:"read,omitempty"`
	Write *int64 `json:"write,omitempty"`
}

type rawNestedTime struct {
	Updated *int64 `json:"updated,omitempty"`
	Created *int64 `json:"created,omitempty"`
}

func (s *rawSession) extractTokens() (input, output, reasoning, cacheRead, cacheWrite, updated int64) {
	if s.TokensInput != nil {
		input = *s.TokensInput
	}
	if s.TokensOutput != nil {
		output = *s.TokensOutput
	}
	if s.TokensReasoning != nil {
		reasoning = *s.TokensReasoning
	}
	if s.TokensCacheRead != nil {
		cacheRead = *s.TokensCacheRead
	}
	if s.TokensCacheWrite != nil {
		cacheWrite = *s.TokensCacheWrite
	}
	if s.TimeUpdated != nil {
		updated = *s.TimeUpdated
	}

	if s.Tokens != nil {
		if s.Tokens.Input != nil {
			input = *s.Tokens.Input
		}
		if s.Tokens.Output != nil {
			output = *s.Tokens.Output
		}
		if s.Tokens.Reasoning != nil {
			reasoning = *s.Tokens.Reasoning
		}
		if s.Tokens.Cache != nil {
			if s.Tokens.Cache.Read != nil {
				cacheRead = *s.Tokens.Cache.Read
			}
			if s.Tokens.Cache.Write != nil {
				cacheWrite = *s.Tokens.Cache.Write
			}
		}
	}
	if s.Time != nil && s.Time.Updated != nil {
		updated = *s.Time.Updated
	}
	return
}

// FetchRPC retrieves session token metrics via the local HTTP server.
func (a *Adapter) FetchRPC(ctx context.Context) (*TokenUsage, error) {
	client := a.httpClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	endpoint := strings.TrimRight(a.serverURL, "/") + "/session"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("creating http request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUpstreamError, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrUpstreamError, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	var sessions []rawSession
	if err := json.Unmarshal(body, &sessions); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrParseFailed, err)
	}

	var input, output, reasoning, cacheRead, cacheWrite, maxTimeUpdated int64
	for _, sess := range sessions {
		in, out, reas, cRead, cWrite, updated := sess.extractTokens()
		input += in
		output += out
		reasoning += reas
		cacheRead += cRead
		cacheWrite += cWrite
		if updated > maxTimeUpdated {
			maxTimeUpdated = updated
		}
	}

	total := input + output + reasoning + cacheRead + cacheWrite

	var observedAt time.Time
	if maxTimeUpdated > 0 {
		observedAt = time.UnixMilli(maxTimeUpdated).UTC()
	} else {
		observedAt = a.now().UTC()
	}

	var cliVersion string
	if det, err := a.Detect(ctx); err == nil && det.Version != "" {
		cliVersion = det.Version
	}

	return &TokenUsage{
		InputTokens:      input,
		OutputTokens:     output,
		ReasoningTokens:  reasoning,
		CacheReadTokens:  cacheRead,
		CacheWriteTokens: cacheWrite,
		TotalTokens:      total,
		ObservedAt:       observedAt,
		CLIVersion:       cliVersion,
	}, nil
}

// AvailableCLI checks if the opencode binary is present on PATH.
func (a *Adapter) AvailableCLI(ctx context.Context) bool {
	_, err := cliexec.ResolveBinary("opencode")
	return err == nil
}

// FetchCLI queries token consumption via `opencode db` CLI subcommand.
func (a *Adapter) FetchCLI(ctx context.Context) (*TokenUsage, error) {
	sqlQuery := "SELECT COALESCE(SUM(tokens_input), 0) AS input_tokens, COALESCE(SUM(tokens_output), 0) AS output_tokens, COALESCE(SUM(tokens_reasoning), 0) AS reasoning_tokens, COALESCE(SUM(tokens_cache_read), 0) AS cache_read_tokens, COALESCE(SUM(tokens_cache_write), 0) AS cache_write_tokens, COALESCE(MAX(time_updated), 0) AS time_updated FROM session;"

	res, err := a.runner.Run(ctx, "opencode", "db", sqlQuery, "--format", "json")
	if err != nil {
		return nil, fmt.Errorf("executing opencode db query: %w", err)
	}

	stdout := strings.TrimSpace(string(res.Stdout))
	if stdout == "" {
		return nil, fmt.Errorf("%w: empty output from opencode db", ErrParseFailed)
	}

	type dbRow struct {
		InputTokens      json.Number `json:"input_tokens"`
		OutputTokens     json.Number `json:"output_tokens"`
		ReasoningTokens  json.Number `json:"reasoning_tokens"`
		CacheReadTokens  json.Number `json:"cache_read_tokens"`
		CacheWriteTokens json.Number `json:"cache_write_tokens"`
		TimeUpdated      json.Number `json:"time_updated"`
	}

	var rows []dbRow
	if err := json.Unmarshal([]byte(stdout), &rows); err != nil {
		var singleRow dbRow
		if err2 := json.Unmarshal([]byte(stdout), &singleRow); err2 != nil {
			return nil, fmt.Errorf("%w: parsing opencode db json: %v", ErrParseFailed, err)
		}
		rows = []dbRow{singleRow}
	}

	var input, output, reasoning, cacheRead, cacheWrite, maxTimeUpdated int64
	for _, r := range rows {
		if n, err := r.InputTokens.Int64(); err == nil {
			input += n
		}
		if n, err := r.OutputTokens.Int64(); err == nil {
			output += n
		}
		if n, err := r.ReasoningTokens.Int64(); err == nil {
			reasoning += n
		}
		if n, err := r.CacheReadTokens.Int64(); err == nil {
			cacheRead += n
		}
		if n, err := r.CacheWriteTokens.Int64(); err == nil {
			cacheWrite += n
		}
		if n, err := r.TimeUpdated.Int64(); err == nil && n > maxTimeUpdated {
			maxTimeUpdated = n
		}
	}

	total := input + output + reasoning + cacheRead + cacheWrite

	var observedAt time.Time
	if maxTimeUpdated > 0 {
		observedAt = time.UnixMilli(maxTimeUpdated).UTC()
	} else {
		observedAt = a.now().UTC()
	}

	var cliVersion string
	if det, err := a.Detect(ctx); err == nil && det.Version != "" {
		cliVersion = det.Version
	}

	return &TokenUsage{
		InputTokens:      input,
		OutputTokens:     output,
		ReasoningTokens:  reasoning,
		CacheReadTokens:  cacheRead,
		CacheWriteTokens: cacheWrite,
		TotalTokens:      total,
		ObservedAt:       observedAt,
		CLIVersion:       cliVersion,
	}, nil
}
