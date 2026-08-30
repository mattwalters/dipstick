package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

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

// Adapter provides usage and metering collection for the Codex coding agent.
type Adapter struct {
	resolver *localstate.Resolver
	runner   *cliexec.Runner
}

// New creates a new Codex adapter instance.
func New(opts ...Option) *Adapter {
	a := &Adapter{
		resolver: localstate.New(),
		runner:   cliexec.New(),
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

// Sources returns the ordered ladder of sources for Codex (Tier 2 local state).
func (a *Adapter) Sources() []types.Source {
	return []types.Source{
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
// and constructs a ProviderReport.
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

	report := &types.ProviderReport{
		Provider:   types.ProviderCodex,
		Source:     types.SourceLocalState,
		Tier:       types.TierLocalState,
		Confidence: types.ConfidenceDerived,
		Identity:   identity,
		Windows:    nil,
		Tokens:     nil,
		ObservedAt: time.Now().UTC(),
	}

	return report, nil
}
