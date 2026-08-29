package localstate

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var (
	// ErrCredentialExpired is returned when an auth token or credential timestamp is in the past.
	ErrCredentialExpired = errors.New("credential has expired")

	// ErrCredentialNotFound is returned when expected credentials are missing from disk or keychain.
	ErrCredentialNotFound = errors.New("credential not found")

	// ErrCredentialMalformed is returned when credentials cannot be parsed.
	ErrCredentialMalformed = errors.New("credential is malformed")
)

// ClaudeCredentials represents parsed OAuth authentication state for Claude.
type ClaudeCredentials struct {
	AccessToken  string     `json:"-"`
	RefreshToken string     `json:"-"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	AccountID    string     `json:"account_id,omitempty"`
	Email        string     `json:"email,omitempty"`
	Subscription string     `json:"subscription,omitempty"`
}

// String redacts sensitive tokens to guarantee zero logging of credentials.
func (c *ClaudeCredentials) String() string {
	if c == nil {
		return "<nil>"
	}
	expStr := "none"
	if c.ExpiresAt != nil {
		expStr = c.ExpiresAt.Format(time.RFC3339)
	}
	return fmt.Sprintf("ClaudeCredentials{AccountID: %q, Email: %q, ExpiresAt: %s, AccessToken: [REDACTED], RefreshToken: [REDACTED]}",
		c.AccountID, c.Email, expStr)
}

// GoString redacts sensitive tokens for formatted printing.
func (c *ClaudeCredentials) GoString() string {
	return c.String()
}

// IsExpired checks if the credentials have expired relative to the provided time.
func (c *ClaudeCredentials) IsExpired(now time.Time) bool {
	if c == nil || c.ExpiresAt == nil {
		return false
	}
	return !now.Before(*c.ExpiresAt)
}

// Validate checks whether the credential contains tokens and is not expired.
func (c *ClaudeCredentials) Validate(now time.Time) error {
	if c == nil || (c.AccessToken == "" && c.RefreshToken == "") {
		return ErrCredentialNotFound
	}
	if c.IsExpired(now) {
		return ErrCredentialExpired
	}
	return nil
}

// CodexAuth represents parsed authentication state from Codex's auth.json.
type CodexAuth struct {
	AuthMode    string       `json:"auth_mode,omitempty"`
	APIKey      string       `json:"-"`
	Tokens      *CodexTokens `json:"tokens,omitempty"`
	LastRefresh *time.Time   `json:"last_refresh,omitempty"`
}

// CodexTokens holds OAuth tokens for Codex.
type CodexTokens struct {
	IDToken      string     `json:"-"`
	AccessToken  string     `json:"-"`
	RefreshToken string     `json:"-"`
	AccountID    string     `json:"account_id,omitempty"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
}

// String redacts sensitive tokens to guarantee zero logging of credentials.
func (a *CodexAuth) String() string {
	if a == nil {
		return "<nil>"
	}
	hasKey := a.APIKey != ""
	hasTokens := a.Tokens != nil && (a.Tokens.AccessToken != "" || a.Tokens.IDToken != "")
	return fmt.Sprintf("CodexAuth{AuthMode: %q, HasAPIKey: %t, HasTokens: %t}", a.AuthMode, hasKey, hasTokens)
}

// GoString redacts sensitive tokens for formatted printing.
func (a *CodexAuth) GoString() string {
	return a.String()
}

// String redacts sensitive tokens to guarantee zero logging of credentials.
func (t *CodexTokens) String() string {
	if t == nil {
		return "<nil>"
	}
	expStr := "none"
	if t.ExpiresAt != nil {
		expStr = t.ExpiresAt.Format(time.RFC3339)
	}
	return fmt.Sprintf("CodexTokens{AccountID: %q, ExpiresAt: %s, AccessToken: [REDACTED]}", t.AccountID, expStr)
}

// GoString redacts sensitive tokens for formatted printing.
func (t *CodexTokens) GoString() string {
	return t.String()
}

// IsExpired checks if the credentials have expired relative to the provided time.
func (a *CodexAuth) IsExpired(now time.Time) bool {
	if a == nil {
		return false
	}
	// Direct API key auth does not expire via token timestamps.
	if a.APIKey != "" && (a.AuthMode == "api_key" || a.Tokens == nil) {
		return false
	}
	if a.Tokens != nil && a.Tokens.ExpiresAt != nil {
		return !now.Before(*a.Tokens.ExpiresAt)
	}
	return false
}

// Validate checks whether the auth state is valid and not expired.
func (a *CodexAuth) Validate(now time.Time) error {
	if a == nil {
		return ErrCredentialNotFound
	}
	hasKey := a.APIKey != ""
	hasTokens := a.Tokens != nil && (a.Tokens.AccessToken != "" || a.Tokens.IDToken != "" || a.Tokens.RefreshToken != "")
	if !hasKey && !hasTokens {
		return ErrCredentialNotFound
	}
	if a.IsExpired(now) {
		return ErrCredentialExpired
	}
	return nil
}

// ParseClaudeCredentials parses a JSON payload containing Claude OAuth credentials.
func ParseClaudeCredentials(data []byte, now time.Time) (*ClaudeCredentials, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, ErrCredentialNotFound
	}

	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCredentialMalformed, err)
	}

	creds := &ClaudeCredentials{}

	// Check if nested inside claudeAiOauth
	var oauthMap map[string]any
	if v, ok := root["claudeAiOauth"].(map[string]any); ok {
		oauthMap = v
	} else {
		oauthMap = root
	}

	if token, ok := oauthMap["accessToken"].(string); ok && token != "" {
		creds.AccessToken = token
	} else if token, ok := oauthMap["access_token"].(string); ok && token != "" {
		creds.AccessToken = token
	} else if token, ok := oauthMap["token"].(string); ok && token != "" {
		creds.AccessToken = token
	}

	if refresh, ok := oauthMap["refreshToken"].(string); ok && refresh != "" {
		creds.RefreshToken = refresh
	} else if refresh, ok := oauthMap["refresh_token"].(string); ok && refresh != "" {
		creds.RefreshToken = refresh
	}

	if expVal, ok := oauthMap["expiresAt"]; ok && expVal != nil {
		if exp, err := parseExpiryValue(expVal); err == nil {
			creds.ExpiresAt = exp
		}
	} else if expVal, ok := oauthMap["expires_at"]; ok && expVal != nil {
		if exp, err := parseExpiryValue(expVal); err == nil {
			creds.ExpiresAt = exp
		}
	}

	if acc, ok := oauthMap["account"].(map[string]any); ok {
		if id, ok := acc["uuid"].(string); ok {
			creds.AccountID = id
		} else if id, ok := acc["id"].(string); ok {
			creds.AccountID = id
		} else if id, ok := acc["account_id"].(string); ok {
			creds.AccountID = id
		}
		if email, ok := acc["emailAddress"].(string); ok {
			creds.Email = email
		} else if email, ok := acc["email"].(string); ok {
			creds.Email = email
		}
	}

	if sub, ok := oauthMap["subscriptionType"].(string); ok {
		creds.Subscription = sub
	} else if sub, ok := oauthMap["subscription"].(string); ok {
		creds.Subscription = sub
	}

	// If expires_at is not explicitly provided, try extracting exp from JWT access token
	if creds.ExpiresAt == nil && creds.AccessToken != "" {
		if jwtExp, err := parseJWTExpiry(creds.AccessToken); err == nil && jwtExp != nil {
			creds.ExpiresAt = jwtExp
		}
	}

	if err := creds.Validate(now); err != nil {
		return creds, err
	}

	return creds, nil
}

// ParseCodexAuth parses a JSON payload containing Codex auth.json data.
func ParseCodexAuth(data []byte, now time.Time) (*CodexAuth, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, ErrCredentialNotFound
	}

	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCredentialMalformed, err)
	}

	auth := &CodexAuth{}

	if mode, ok := root["auth_mode"].(string); ok {
		auth.AuthMode = mode
	}

	if key, ok := root["OPENAI_API_KEY"].(string); ok && key != "" {
		auth.APIKey = key
	} else if key, ok := root["openai_api_key"].(string); ok && key != "" {
		auth.APIKey = key
	}

	if refreshVal, ok := root["last_refresh"]; ok && refreshVal != nil {
		if lr, err := parseExpiryValue(refreshVal); err == nil {
			auth.LastRefresh = lr
		}
	}

	if tokensMap, ok := root["tokens"].(map[string]any); ok {
		tokens := &CodexTokens{}
		if id, ok := tokensMap["id_token"].(string); ok {
			tokens.IDToken = id
		}
		if access, ok := tokensMap["access_token"].(string); ok {
			tokens.AccessToken = access
		}
		if refresh, ok := tokensMap["refresh_token"].(string); ok {
			tokens.RefreshToken = refresh
		}
		if acc, ok := tokensMap["account_id"].(string); ok {
			tokens.AccountID = acc
		}
		if expVal, ok := tokensMap["expires_at"]; ok && expVal != nil {
			if exp, err := parseExpiryValue(expVal); err == nil {
				tokens.ExpiresAt = exp
			}
		}

		// Extract exp from JWT tokens if not already specified
		if tokens.ExpiresAt == nil {
			if tokens.AccessToken != "" {
				if jwtExp, err := parseJWTExpiry(tokens.AccessToken); err == nil && jwtExp != nil {
					tokens.ExpiresAt = jwtExp
				}
			}
			if tokens.ExpiresAt == nil && tokens.IDToken != "" {
				if jwtExp, err := parseJWTExpiry(tokens.IDToken); err == nil && jwtExp != nil {
					tokens.ExpiresAt = jwtExp
				}
			}
		}

		auth.Tokens = tokens
	}

	if err := auth.Validate(now); err != nil {
		return auth, err
	}

	return auth, nil
}

func parseExpiryValue(v any) (*time.Time, error) {
	switch val := v.(type) {
	case float64:
		return parseNumericEpoch(int64(val)), nil
	case int64:
		return parseNumericEpoch(val), nil
	case int:
		return parseNumericEpoch(int64(val)), nil
	case json.Number:
		if i, err := val.Int64(); err == nil {
			return parseNumericEpoch(i), nil
		}
		if f, err := val.Float64(); err == nil {
			return parseNumericEpoch(int64(f)), nil
		}
	case string:
		valStr := strings.TrimSpace(val)
		if valStr == "" {
			return nil, fmt.Errorf("empty timestamp string")
		}
		if i, err := strconv.ParseInt(valStr, 10, 64); err == nil {
			return parseNumericEpoch(i), nil
		}
		formats := []string{
			time.RFC3339Nano,
			time.RFC3339,
			"2006-01-02T15:04:05Z07:00",
			"2006-01-02T15:04:05",
			"2006-01-02 15:04:05",
		}
		for _, f := range formats {
			if t, err := time.Parse(f, valStr); err == nil {
				utc := t.UTC()
				return &utc, nil
			}
		}
	}
	return nil, fmt.Errorf("unrecognized expiry format: %v", v)
}

func parseNumericEpoch(epoch int64) *time.Time {
	if epoch <= 0 {
		return nil
	}
	var t time.Time
	// Timestamps in milliseconds are larger than 1e11 (approx year 1973 in ms)
	if epoch > 100000000000 {
		t = time.UnixMilli(epoch).UTC()
	} else {
		t = time.Unix(epoch, 0).UTC()
	}
	return &t
}

func parseJWTExpiry(token string) (*time.Time, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("not a valid JWT structure")
	}

	payloadSegment := parts[1]
	// Handle raw URL decoding and padded URL decoding
	payloadBytes, err := base64.RawURLEncoding.DecodeString(payloadSegment)
	if err != nil {
		payloadBytes, err = base64.URLEncoding.DecodeString(payloadSegment)
		if err != nil {
			return nil, fmt.Errorf("decoding JWT payload: %w", err)
		}
	}

	var claims map[string]any
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return nil, fmt.Errorf("unmarshaling JWT claims: %w", err)
	}

	if exp, ok := claims["exp"]; ok && exp != nil {
		return parseExpiryValue(exp)
	}

	return nil, nil
}
