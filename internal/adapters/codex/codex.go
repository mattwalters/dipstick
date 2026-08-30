package codex

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/mattwalters/dipstick/internal/cliexec"
	"github.com/mattwalters/dipstick/internal/localstate"
	"github.com/mattwalters/dipstick/internal/types"
)

var _ types.Adapter = (*Adapter)(nil)

// Option configures a Codex Adapter instance.
type Option func(*Adapter)

// WithResolver sets the localstate Resolver used for finding Codex configuration files.
func WithResolver(r *localstate.Resolver) Option {
	return func(a *Adapter) {
		if r != nil {
			a.resolver = r
		}
	}
}

// WithRunner sets the cliexec Runner used for binary detection and version probing.
func WithRunner(r *cliexec.Runner) Option {
	return func(a *Adapter) {
		if r != nil {
			a.runner = r
		}
	}
}

// WithAppServerRunner sets the AppServerRunner used to launch codex app-server sessions.
func WithAppServerRunner(runner AppServerRunner) Option {
	return func(a *Adapter) {
		if runner != nil {
			a.appServerRunner = runner
		}
	}
}

// WithAppServerTimeout sets the execution timeout for codex app-server queries.
func WithAppServerTimeout(d time.Duration) Option {
	return func(a *Adapter) {
		if d > 0 {
			a.appServerTimeout = d
		}
	}
}

// WithNow sets the time provider function for the Codex adapter.
func WithNow(fn func() time.Time) Option {
	return func(a *Adapter) {
		if fn != nil {
			a.now = fn
		}
	}
}

// Adapter provides usage and metering collection for the Codex coding agent.
type Adapter struct {
	resolver         *localstate.Resolver
	runner           *cliexec.Runner
	appServerRunner  AppServerRunner
	appServerTimeout time.Duration
	now              func() time.Time
}

// New creates a new Codex adapter instance.
func New(opts ...Option) *Adapter {
	a := &Adapter{
		resolver:         localstate.New(),
		runner:           cliexec.New(),
		appServerTimeout: DefaultAppServerTimeout,
		now:              time.Now,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(a)
		}
	}
	return a
}

// ID returns the unique provider identifier.
func (a *Adapter) ID() types.ProviderID {
	return types.ProviderCodex
}

// Name returns the provider identifier string for backwards compatibility.
func (a *Adapter) Name() string {
	return string(types.ProviderCodex)
}

// Detect probes the local environment to determine if the Codex CLI is installed
// and authenticated.
func (a *Adapter) Detect(ctx context.Context) (types.Detection, error) {
	var d types.Detection

	// 1. Probe binary installation and version
	resolved, err := cliexec.ResolveBinary("codex")
	if err == nil {
		d.Installed = true
		d.BinaryPath = resolved
		if a.runner != nil {
			if v, err := a.runner.ProbeVersion(ctx, "codex"); err == nil {
				d.Version = v
			}
		}
	}

	// 2. Probe authentication state via auth.json
	if a.resolver != nil {
		paths, err := a.resolver.CodexPaths()
		if err == nil {
			if fi, err := os.Stat(paths.AuthFile); err == nil && !fi.IsDir() {
				if auth, err := a.resolver.ReadCodexAuth(ctx); err == nil && auth != nil {
					d.Authenticated = true
				}
			}
		}
	}

	return d, nil
}

// Sources returns the ordered ladder of sources for Codex (Tier 3 local RPC, Tier 2 local state).
func (a *Adapter) Sources() []types.Source {
	return []types.Source{
		newAppServerSource(a),
		newLocalStateSource(a),
	}
}

// Compat returns the verified compatibility range declaration for Codex.
func (a *Adapter) Compat() types.Compat {
	return types.Compat{
		VerifiedRange: ">=0.148.0 <0.150.0",
		LastCheck:     "2026-08-29",
		Notes:         "Supported",
	}
}

var _ types.Source = (*appServerSource)(nil)

type appServerSource struct {
	adapter *Adapter
}

func newAppServerSource(a *Adapter) *appServerSource {
	return &appServerSource{adapter: a}
}

// ID returns the identifier for the app-server source rung.
func (s *appServerSource) ID() types.SourceID {
	return types.SourceAppServer
}

// Tier returns TierLocalRPC (Tier 3).
func (s *appServerSource) Tier() types.SourceTier {
	return types.TierLocalRPC
}

// Available checks whether codex binary is available or custom runner is injected.
func (s *appServerSource) Available(ctx context.Context) bool {
	if s.adapter == nil {
		return false
	}
	if s.adapter.appServerRunner != nil {
		return true
	}
	_, err := cliexec.ResolveBinary("codex")
	return err == nil
}

// Fetch executes codex app-server --stdio to query rate limits, token usage, and account identity.
func (s *appServerSource) Fetch(ctx context.Context) (*types.ProviderReport, error) {
	if s.adapter == nil {
		return nil, fmt.Errorf("%w: adapter not initialized", types.ErrNotInstalled)
	}

	timeout := s.adapter.appServerTimeout
	if timeout <= 0 {
		timeout = DefaultAppServerTimeout
	}

	fetchCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	runner := s.adapter.appServerRunner
	if runner == nil {
		runner = defaultAppServerRunner("codex")
	}

	transport, err := runner.Start(fetchCtx)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(fetchCtx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("%w: app-server start timeout: %v", types.ErrSourceTimeout, err)
		}
		if errors.Is(err, types.ErrNotInstalled) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: starting app-server: %v", types.ErrUpstreamError, err)
	}
	defer func() { _ = transport.Close() }()

	client := newAppServerClient(transport)

	formatErr := func(msg string, err error) error {
		if carrier, ok := transport.(interface{ Stderr() string }); ok {
			if sErr := carrier.Stderr(); sErr != "" {
				return fmt.Errorf("%w: %s: %v (stderr: %s)", types.ErrUpstreamError, msg, err, sErr)
			}
		}
		return fmt.Errorf("%w: %s: %v", types.ErrUpstreamError, msg, err)
	}

	// 1. Handshake
	if _, err := client.Initialize(fetchCtx); err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(fetchCtx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("%w: initialize timeout: %v", types.ErrSourceTimeout, err)
		}
		return nil, formatErr("handshake initialize failed", err)
	}

	// 2. Read Rate Limits
	rlRes, rlErr := client.ReadRateLimits(fetchCtx)
	if rlErr != nil {
		if errors.Is(rlErr, context.DeadlineExceeded) || errors.Is(fetchCtx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("%w: reading rate limits timeout: %v", types.ErrSourceTimeout, rlErr)
		}
	}

	// 3. Read Usage (Tokens)
	usageRes, usageErr := client.ReadUsage(fetchCtx)
	if usageErr != nil {
		if errors.Is(usageErr, context.DeadlineExceeded) || errors.Is(fetchCtx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("%w: reading usage timeout: %v", types.ErrSourceTimeout, usageErr)
		}
	}

	// 4. Read Account (Identity)
	accountRes, accountErr := client.ReadAccount(fetchCtx)
	if accountErr != nil {
		if errors.Is(accountErr, context.DeadlineExceeded) || errors.Is(fetchCtx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("%w: reading account timeout: %v", types.ErrSourceTimeout, accountErr)
		}
	}

	if rlErr != nil && usageErr != nil && accountErr != nil {
		return nil, formatErr("all app-server queries failed", fmt.Errorf("rateLimits: %v, usage: %v, account: %v", rlErr, usageErr, accountErr))
	}

	var windows []types.RateWindow
	identity := &types.Identity{}
	var tokens *types.TokenUsage

	if rlRes != nil && rlRes.RateLimits != nil {
		rl := rlRes.RateLimits
		if rl.PlanType != "" {
			identity.Plan = rl.PlanType
		}

		if rl.Primary != nil {
			usedPercent := rl.Primary.UsedPercent
			durationSecs := rl.Primary.WindowDurationMins * 60
			var resetsAt *time.Time
			if rl.Primary.ResetsAt > 0 {
				t := time.Unix(rl.Primary.ResetsAt, 0).UTC()
				resetsAt = &t
			}
			windows = append(windows, types.RateWindow{
				Label:                 "primary",
				UsedPercent:           &usedPercent,
				WindowDurationSeconds: &durationSecs,
				ResetsAt:              resetsAt,
			})
		}

		if rl.Secondary != nil {
			usedPercent := rl.Secondary.UsedPercent
			durationSecs := rl.Secondary.WindowDurationMins * 60
			var resetsAt *time.Time
			if rl.Secondary.ResetsAt > 0 {
				t := time.Unix(rl.Secondary.ResetsAt, 0).UTC()
				resetsAt = &t
			}
			windows = append(windows, types.RateWindow{
				Label:                 "secondary",
				UsedPercent:           &usedPercent,
				WindowDurationSeconds: &durationSecs,
				ResetsAt:              resetsAt,
			})
		}
	}

	if usageRes != nil && usageRes.Summary != nil {
		tot := usageRes.Summary.LifetimeTokens
		tokens = &types.TokenUsage{
			TotalTokens: &tot,
		}
	}

	if accountRes != nil {
		if accountRes.Account.Email != "" {
			identity.Email = accountRes.Account.Email
		}
		if identity.Plan == "" && accountRes.Account.PlanType != "" {
			identity.Plan = accountRes.Account.PlanType
		}
	}

	if identity.Email == "" && identity.Plan == "" && identity.AccountID == "" && identity.Organization == "" {
		identity = nil
	}

	nowFn := time.Now
	if s.adapter.now != nil {
		nowFn = s.adapter.now
	}

	report := &types.ProviderReport{
		Provider:   types.ProviderCodex,
		Source:     types.SourceAppServer,
		Tier:       types.TierLocalRPC,
		Confidence: types.ConfidenceExact,
		Identity:   identity,
		Windows:    windows,
		Tokens:     tokens,
		ObservedAt: nowFn().UTC(),
	}

	return report, nil
}

var _ types.Source = (*localStateSource)(nil)

type localStateSource struct {
	adapter *Adapter
}

func newLocalStateSource(a *Adapter) *localStateSource {
	return &localStateSource{adapter: a}
}

// ID returns the identifier for the local state source rung.
func (s *localStateSource) ID() types.SourceID {
	return types.SourceLocalState
}

// Tier returns TierLocalState (Tier 2).
func (s *localStateSource) Tier() types.SourceTier {
	return types.TierLocalState
}

// Available checks whether ~/.codex/auth.json exists and is readable.
func (s *localStateSource) Available(ctx context.Context) bool {
	if s.adapter == nil || s.adapter.resolver == nil {
		return false
	}
	paths, err := s.adapter.resolver.CodexPaths()
	if err != nil {
		return false
	}
	fi, err := os.Stat(paths.AuthFile)
	if err != nil || fi.IsDir() {
		return false
	}
	return true
}

// Fetch reads auth.json, parses identity and plan claims without signature verification,
// queries cumulative tokens from state_5.sqlite if present, and constructs a ProviderReport.
func (s *localStateSource) Fetch(ctx context.Context) (*types.ProviderReport, error) {
	if s.adapter == nil || s.adapter.resolver == nil {
		return nil, fmt.Errorf("%w: resolver not initialized", types.ErrNotInstalled)
	}

	paths, err := s.adapter.resolver.CodexPaths()
	if err != nil {
		return nil, fmt.Errorf("%w: resolving codex paths: %v", types.ErrNotInstalled, err)
	}

	data, err := os.ReadFile(paths.AuthFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: codex auth file not found: %s", types.ErrNotInstalled, paths.AuthFile)
		}
		return nil, fmt.Errorf("reading codex auth file: %w", err)
	}

	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, fmt.Errorf("%w: empty auth.json file", types.ErrParseFailed)
	}

	var root struct {
		AuthMode    string `json:"auth_mode"`
		APIKey      string `json:"OPENAI_API_KEY"`
		AltAPIKey   string `json:"openai_api_key"`
		LastRefresh any    `json:"last_refresh"`
		Tokens      *struct {
			IDToken      string `json:"id_token"`
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			AccountID    string `json:"account_id"`
		} `json:"tokens"`
	}

	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("%w: parsing auth.json: %v", types.ErrParseFailed, err)
	}

	apiKey := strings.TrimSpace(root.APIKey)
	if apiKey == "" {
		apiKey = strings.TrimSpace(root.AltAPIKey)
	}
	authMode := strings.ToLower(strings.TrimSpace(root.AuthMode))

	hasTokens := root.Tokens != nil && strings.TrimSpace(root.Tokens.IDToken) != ""
	hasAPIKey := apiKey != ""

	if !hasTokens && !hasAPIKey {
		return nil, fmt.Errorf("%w: auth.json contains neither valid tokens nor an API key", types.ErrParseFailed)
	}

	identity := &types.Identity{}

	// API Key Auth mode: distinguishes API-key auth from ChatGPT-subscription auth.
	// Users on an API key have no subscription quota windows.
	if authMode == "api_key" || (hasAPIKey && !hasTokens) {
		identity.Plan = "api_key"
		if root.Tokens != nil && root.Tokens.AccountID != "" {
			identity.AccountID = root.Tokens.AccountID
		}
	} else {
		// ChatGPT Subscription Auth mode: decode the id_token JWT payload without signature verification.
		claims, err := decodeJWTUnverified(root.Tokens.IDToken)
		if err != nil {
			return nil, err
		}

		identity.Email = claims.Email
		identity.AccountID = claims.ChatGPTAccountID
		if identity.AccountID == "" && root.Tokens.AccountID != "" {
			identity.AccountID = root.Tokens.AccountID
		}
		identity.Plan = claims.ChatGPTPlanType
	}

	var tokenUsage *types.TokenUsage
	if fi, err := os.Stat(paths.StateFile); err == nil && !fi.IsDir() {
		if total, err := querySQLiteTokens(ctx, paths.StateFile); err == nil {
			tokenUsage = &types.TokenUsage{
				TotalTokens: &total,
			}
		}
	}

	nowFn := time.Now
	if s.adapter != nil && s.adapter.now != nil {
		nowFn = s.adapter.now
	}

	report := &types.ProviderReport{
		Provider:   types.ProviderCodex,
		Source:     types.SourceLocalState,
		Tier:       types.TierLocalState,
		Confidence: types.ConfidenceDerived,
		Identity:   identity,
		Windows:    nil,
		Tokens:     tokenUsage,
		ObservedAt: nowFn().UTC(),
	}

	return report, nil
}

func querySQLiteTokens(ctx context.Context, dbPath string) (int64, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return 0, err
	}
	defer func() { _ = db.Close() }()

	var total int64
	row := db.QueryRowContext(ctx, "SELECT COALESCE(SUM(tokens_used), 0) FROM threads;")
	if err := row.Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}
