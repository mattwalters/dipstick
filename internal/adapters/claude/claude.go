package claude

import (
	"context"
	"net/http"
	"time"

	"github.com/mattwalters/dipstick/internal/cliexec"
	"github.com/mattwalters/dipstick/internal/localstate"
	"github.com/mattwalters/dipstick/internal/types"
)

var _ types.Adapter = (*Adapter)(nil)

// Option configures a Claude Adapter.
type Option func(*Adapter)

// WithAdapterBaseURL sets the base URL for the Claude adapter's OAuth source.
func WithAdapterBaseURL(url string) Option {
	return func(a *Adapter) {
		a.baseURL = url
	}
}

// WithAdapterHTTPClient sets the HTTP client for the Claude adapter.
func WithAdapterHTTPClient(client *http.Client) Option {
	return func(a *Adapter) {
		a.httpClient = client
	}
}

// WithAdapterCredentialResolver sets the credential resolver for the Claude adapter.
func WithAdapterCredentialResolver(fn func(context.Context) (*localstate.ClaudeCredentials, error)) Option {
	return func(a *Adapter) {
		a.credentialResolver = fn
	}
}

// WithAdapterRunner sets the cliexec.Runner used for CLI detection and probing.
func WithAdapterRunner(r *cliexec.Runner) Option {
	return func(a *Adapter) {
		a.runner = r
	}
}

// WithAdapterNow sets the time provider function for the Claude adapter.
func WithAdapterNow(fn func() time.Time) Option {
	return func(a *Adapter) {
		a.now = fn
	}
}

// Adapter provides usage collection for the Claude coding agent.
type Adapter struct {
	baseURL            string
	httpClient         *http.Client
	credentialResolver func(context.Context) (*localstate.ClaudeCredentials, error)
	runner             *cliexec.Runner
	now                func() time.Time
}

// New creates a new Claude adapter instance.
func New(opts ...Option) *Adapter {
	a := &Adapter{
		baseURL:            DefaultOAuthUsageURL,
		credentialResolver: localstate.ReadClaudeCredentials,
		runner:             cliexec.New(),
		now:                time.Now,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(a)
		}
	}
	return a
}

// ID returns the provider identifier for Claude.
func (a *Adapter) ID() types.ProviderID {
	return types.ProviderClaude
}

// Name returns the provider identifier string.
func (a *Adapter) Name() string {
	return string(types.ProviderClaude)
}

// Detect inspects local environment to determine installation, auth, and version state.
func (a *Adapter) Detect(ctx context.Context) (types.Detection, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return types.Detection{}, err
	}

	var detection types.Detection

	// 1. Check binary installation and probe version
	resolvedPath, err := cliexec.ResolveBinary("claude")
	if err == nil && resolvedPath != "" {
		detection.Installed = true
		detection.BinaryPath = resolvedPath

		runner := a.runner
		if runner == nil {
			runner = cliexec.New()
		}
		if ver, probeErr := runner.ProbeVersion(ctx, "claude"); probeErr == nil && ver != "" {
			detection.Version = ver
		}
	}

	// 2. Check authentication status via credential resolver
	resolver := a.credentialResolver
	if resolver == nil {
		resolver = localstate.ReadClaudeCredentials
	}

	creds, credErr := resolver(ctx)
	if credErr == nil && creds != nil && creds.AccessToken != "" {
		now := time.Now()
		if a.now != nil {
			now = a.now()
		}
		if !creds.IsExpired(now) {
			detection.Authenticated = true
		}
	}

	return detection, nil
}

// Sources returns the ordered source ladder for Claude, highest tier first.
func (a *Adapter) Sources() []types.Source {
	oauthOpts := []OAuthOption{
		WithBaseURL(a.baseURL),
		WithHTTPClient(a.httpClient),
		WithCredentialResolver(a.credentialResolver),
		WithNow(a.now),
	}
	if a.runner != nil {
		oauthOpts = append(oauthOpts, WithVersionProbe(func(ctx context.Context) (string, error) {
			return a.runner.ProbeVersion(ctx, "claude")
		}))
	}

	return []types.Source{
		NewOAuthAPISource(oauthOpts...),
	}
}
