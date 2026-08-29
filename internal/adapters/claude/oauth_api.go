package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/mattwalters/dipstick/internal/localstate"
	"github.com/mattwalters/dipstick/internal/scrub"
	"github.com/mattwalters/dipstick/internal/types"
)

var _ types.Source = (*OAuthAPISource)(nil)

const (
	// DefaultOAuthUsageURL is the default Anthropic OAuth usage API endpoint.
	DefaultOAuthUsageURL = "https://api.anthropic.com"

	// AnthropicBetaHeader is the beta header required for Anthropic OAuth endpoints.
	AnthropicBetaHeader = "oauth-2025-04-20"

	// DefaultHTTPTimeout is the default bounded timeout for OAuth API calls.
	DefaultHTTPTimeout = 5 * time.Second

	// FiveHourDurationSeconds is the duration of Claude's session rate window in seconds (5h).
	FiveHourDurationSeconds int64 = 5 * 3600

	// SevenDayDurationSeconds is the duration of Claude's weekly rate window in seconds (7d).
	SevenDayDurationSeconds int64 = 7 * 24 * 3600
)

// OAuthAPISource implements Tier 1 usage collection via Anthropic's OAuth usage endpoint.
type OAuthAPISource struct {
	baseURL            string
	httpClient         *http.Client
	credentialResolver func(context.Context) (*localstate.ClaudeCredentials, error)
	timeout            time.Duration
	versionProbe       func(context.Context) (string, error)
	now                func() time.Time
}

// OAuthOption configures an OAuthAPISource instance.
type OAuthOption func(*OAuthAPISource)

// WithBaseURL sets the base URL for the Anthropic API.
func WithBaseURL(url string) OAuthOption {
	return func(s *OAuthAPISource) {
		if url != "" {
			s.baseURL = strings.TrimRight(url, "/")
		}
	}
}

// WithHTTPClient sets a custom http.Client.
func WithHTTPClient(client *http.Client) OAuthOption {
	return func(s *OAuthAPISource) {
		if client != nil {
			s.httpClient = client
		}
	}
}

// WithCredentialResolver sets a custom credential resolution function.
func WithCredentialResolver(fn func(context.Context) (*localstate.ClaudeCredentials, error)) OAuthOption {
	return func(s *OAuthAPISource) {
		if fn != nil {
			s.credentialResolver = fn
		}
	}
}

// WithTimeout sets a custom HTTP execution timeout.
func WithTimeout(d time.Duration) OAuthOption {
	return func(s *OAuthAPISource) {
		if d > 0 {
			s.timeout = d
		}
	}
}

// WithVersionProbe sets a custom CLI version probe function.
func WithVersionProbe(fn func(context.Context) (string, error)) OAuthOption {
	return func(s *OAuthAPISource) {
		s.versionProbe = fn
	}
}

// WithNow sets the time provider function.
func WithNow(fn func() time.Time) OAuthOption {
	return func(s *OAuthAPISource) {
		if fn != nil {
			s.now = fn
		}
	}
}

// NewOAuthAPISource creates a new Tier 1 OAuth API source.
func NewOAuthAPISource(opts ...OAuthOption) *OAuthAPISource {
	s := &OAuthAPISource{
		baseURL:            DefaultOAuthUsageURL,
		credentialResolver: localstate.ReadClaudeCredentials,
		timeout:            DefaultHTTPTimeout,
		now:                time.Now,
		httpClient: &http.Client{
			Transport: http.DefaultTransport,
		},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}
	return s
}

// ID returns the source identifier for Tier 1 OAuth API.
func (s *OAuthAPISource) ID() types.SourceID {
	return types.SourceOAuthAPI
}

// Tier returns the source robustness tier (Tier 1: API).
func (s *OAuthAPISource) Tier() types.SourceTier {
	return types.TierAPI
}

// Available checks whether prerequisites for this source are satisfied without making network requests.
func (s *OAuthAPISource) Available(ctx context.Context) bool {
	if s.credentialResolver == nil {
		return false
	}
	creds, err := s.credentialResolver(ctx)
	if err != nil {
		// If credentials exist but are expired, the source is available to run and report ReasonCredentialExpired.
		return errors.Is(err, localstate.ErrCredentialExpired)
	}
	return creds != nil && creds.AccessToken != ""
}

// Fetch gathers usage data from the Anthropic OAuth usage API endpoint.
func (s *OAuthAPISource) Fetch(ctx context.Context) (*types.ProviderReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if s.credentialResolver == nil {
		return nil, fmt.Errorf("%w: no credential resolver configured", types.ErrNotAuthenticated)
	}

	creds, err := s.credentialResolver(ctx)
	if err != nil {
		if errors.Is(err, localstate.ErrCredentialExpired) {
			return nil, fmt.Errorf("%w: Claude credentials have expired", types.ErrCredentialExpired)
		}
		if errors.Is(err, localstate.ErrCredentialNotFound) {
			return nil, fmt.Errorf("%w: Claude OAuth credentials not found in keychain or .credentials.json", types.ErrNotAuthenticated)
		}
		if errors.Is(err, localstate.ErrCredentialMalformed) {
			return nil, fmt.Errorf("%w: Claude credentials payload is malformed: %v", types.ErrParseFailed, err)
		}
		return nil, fmt.Errorf("%w: failed to resolve Claude credentials: %v", types.ErrNotAuthenticated, err)
	}

	if creds == nil || creds.AccessToken == "" {
		return nil, fmt.Errorf("%w: Claude OAuth access token is missing", types.ErrNotAuthenticated)
	}

	now := time.Now()
	if s.now != nil {
		now = s.now()
	}
	if creds.IsExpired(now) {
		return nil, fmt.Errorf("%w: Claude credentials expired at %v", types.ErrCredentialExpired, creds.ExpiresAt)
	}

	endpoint := fmt.Sprintf("%s/api/oauth/usage", strings.TrimRight(s.baseURL, "/"))

	reqTimeout := s.timeout
	if reqTimeout <= 0 {
		reqTimeout = DefaultHTTPTimeout
	}

	reqCtx, reqCancel := context.WithTimeout(ctx, reqTimeout)
	defer reqCancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: creating usage request: %v", types.ErrUpstreamError, err)
	}

	req.Header.Set("Authorization", "Bearer "+creds.AccessToken)
	req.Header.Set("anthropic-beta", AnthropicBetaHeader)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "dipstick/1.0")

	client := s.httpClient
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if reqCtx.Err() != nil {
			if errors.Is(reqCtx.Err(), context.DeadlineExceeded) {
				return nil, fmt.Errorf("%w: request timed out: %v", types.ErrTimeout, reqCtx.Err())
			}
			if errors.Is(reqCtx.Err(), context.Canceled) {
				return nil, reqCtx.Err()
			}
		}
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return nil, fmt.Errorf("%w: request timed out: %v", types.ErrTimeout, err)
		}
		return nil, fmt.Errorf("%w: upstream HTTP request failed: %s", types.ErrUpstreamError, scrub.Scrub(err.Error()))
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if reqCtx.Err() != nil {
			if errors.Is(reqCtx.Err(), context.DeadlineExceeded) {
				return nil, fmt.Errorf("%w: reading response body timed out: %v", types.ErrTimeout, reqCtx.Err())
			}
			if errors.Is(reqCtx.Err(), context.Canceled) {
				return nil, reqCtx.Err()
			}
		}
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return nil, fmt.Errorf("%w: reading response body timed out: %v", types.ErrTimeout, err)
		}
		return nil, fmt.Errorf("%w: reading response body: %v", types.ErrUpstreamError, scrub.Scrub(err.Error()))
	}

	switch resp.StatusCode {
	case http.StatusOK:
		// Process 200 OK below
	case http.StatusUnauthorized:
		return nil, fmt.Errorf("%w: authentication failed (HTTP 401); re-login with 'claude'", types.ErrNotAuthenticated)
	case http.StatusForbidden:
		return nil, fmt.Errorf("%w: access forbidden by upstream (HTTP 403)", types.ErrUpstreamError)
	case http.StatusTooManyRequests:
		return nil, fmt.Errorf("%w: rate limit exceeded on upstream usage API (HTTP 429)", types.ErrUpstreamError)
	default:
		if resp.StatusCode >= 500 {
			return nil, fmt.Errorf("%w: upstream server error (HTTP %d)", types.ErrUpstreamError, resp.StatusCode)
		}
		return nil, fmt.Errorf("%w: unexpected HTTP status %d: %s", types.ErrUpstreamError, resp.StatusCode, scrub.Scrub(string(bodyBytes)))
	}

	windows, err := parseOAuthUsageResponse(bodyBytes)
	if err != nil {
		return nil, err
	}

	var identity *types.Identity
	if creds.Email != "" || creds.AccountID != "" || creds.Subscription != "" {
		identity = &types.Identity{
			Email:     creds.Email,
			AccountID: creds.AccountID,
			Plan:      creds.Subscription,
		}
	}

	var cliVersion string
	if s.versionProbe != nil {
		if ver, err := s.versionProbe(ctx); err == nil && ver != "" {
			cliVersion = ver
		}
	}

	return &types.ProviderReport{
		Provider:   types.ProviderClaude,
		Source:     types.SourceOAuthAPI,
		Confidence: types.ConfidenceExact,
		CLIVersion: cliVersion,
		Identity:   identity,
		Windows:    windows,
		ObservedAt: now.UTC(),
	}, nil
}

type windowRawData struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    *string  `json:"resets_at"`
	Limit       *float64 `json:"limit"`
	Used        *float64 `json:"used"`
}

func parseOAuthUsageResponse(data []byte) ([]types.RateWindow, error) {
	return ParseOAuthUsageResponse(data)
}

// ParseOAuthUsageResponse parses the JSON body returned by Anthropic's OAuth usage endpoint.
// It dynamically extracts rate windows while mapping five_hour -> session and seven_day -> weekly.
func ParseOAuthUsageResponse(data []byte) ([]types.RateWindow, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, fmt.Errorf("%w: empty response payload", types.ErrParseFailed)
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("%w: invalid JSON response: %v", types.ErrParseFailed, err)
	}

	// Check if windows are under a nested "limits" or "windows" or "rate_limits" container
	candidateMaps := []map[string]json.RawMessage{root}
	for _, nestedKey := range []string{"limits", "windows", "rate_limits"} {
		if raw, ok := root[nestedKey]; ok {
			var nested map[string]json.RawMessage
			if err := json.Unmarshal(raw, &nested); err == nil && len(nested) > 0 {
				candidateMaps = append(candidateMaps, nested)
			}
		}
	}

	discoveredWindows := make(map[string]types.RateWindow)

	for _, m := range candidateMaps {
		for key, rawVal := range m {
			if key == "limits" || key == "windows" || key == "rate_limits" {
				continue
			}

			var w windowRawData
			if err := json.Unmarshal(rawVal, &w); err != nil {
				continue
			}

			// A valid rate window must carry at least utilization or resets_at
			if w.Utilization == nil && w.ResetsAt == nil && w.Limit == nil && w.Used == nil {
				continue
			}

			label, duration := normalizeWindowMetadata(key)

			var parsedReset *time.Time
			if w.ResetsAt != nil && strings.TrimSpace(*w.ResetsAt) != "" {
				resetsAtStr := strings.TrimSpace(*w.ResetsAt)
				t, err := time.Parse(time.RFC3339Nano, resetsAtStr)
				if err != nil {
					t, err = time.Parse(time.RFC3339, resetsAtStr)
				}
				if err != nil {
					return nil, fmt.Errorf("%w: malformed resets_at timestamp %q in window %q", types.ErrParseFailed, resetsAtStr, key)
				}
				utc := t.UTC()
				parsedReset = &utc
			}

			discoveredWindows[label] = types.RateWindow{
				Label:                 label,
				UsedPercent:           w.Utilization,
				Limit:                 w.Limit,
				Used:                  w.Used,
				ResetsAt:              parsedReset,
				WindowDurationSeconds: duration,
			}
		}
	}

	if len(discoveredWindows) == 0 {
		return nil, fmt.Errorf("%w: response does not contain any recognized rate windows", types.ErrParseFailed)
	}

	// Sort windows deterministically: session first, weekly second, followed by others sorted by label
	var labels []string
	for lbl := range discoveredWindows {
		labels = append(labels, lbl)
	}

	sort.Slice(labels, func(i, j int) bool {
		priority := func(lbl string) int {
			switch lbl {
			case "session":
				return 1
			case "weekly":
				return 2
			case "daily":
				return 3
			case "hourly":
				return 4
			default:
				return 10
			}
		}
		pI, pJ := priority(labels[i]), priority(labels[j])
		if pI != pJ {
			return pI < pJ
		}
		return labels[i] < labels[j]
	})

	windows := make([]types.RateWindow, 0, len(labels))
	for _, lbl := range labels {
		windows = append(windows, discoveredWindows[lbl])
	}

	return windows, nil
}

func normalizeWindowMetadata(key string) (string, *int64) {
	normKey := strings.ToLower(strings.TrimSpace(key))
	normKey = strings.ReplaceAll(normKey, "-", "_")

	switch normKey {
	case "five_hour", "five_hours", "5h", "fivehour", "session":
		return "session", types.Ptr(FiveHourDurationSeconds)
	case "seven_day", "seven_days", "7d", "sevenday", "weekly":
		return "weekly", types.Ptr(SevenDayDurationSeconds)
	case "one_hour", "1h", "hourly":
		return "hourly", types.Ptr(int64(3600))
	case "twenty_four_hour", "24h", "one_day", "1d", "daily":
		return "daily", types.Ptr(int64(86400))
	case "thirty_day", "30d", "monthly":
		return "monthly", types.Ptr(int64(30 * 86400))
	default:
		return normKey, nil
	}
}
