package codex

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mattwalters/dipstick/internal/types"
)

// OpenAIAuthNamespace is the claims namespace URI in OpenAI JWT tokens.
const OpenAIAuthNamespace = "https://api.openai.com/auth"

// JWTClaims represents parsed claims extracted from a Codex id_token JWT payload.
type JWTClaims struct {
	Email            string `json:"email,omitempty"`
	ChatGPTAccountID string `json:"chatgpt_account_id,omitempty"`
	ChatGPTPlanType  string `json:"chatgpt_plan_type,omitempty"`
	ChatGPTUserID    string `json:"chatgpt_user_id,omitempty"`
}

// decodeJWTUnverified decodes a JWT payload without signature verification.
//
// NOTE: Signature verification is intentionally not performed here: we have no
// verification key and no cryptographic requirement to verify signatures. We are
// reading a local state file (~/.codex/auth.json) already trusted by the user on
// their own machine, parsed strictly for UI and quota reporting metadata.
// Do not treat this function as an authentication check.
func decodeJWTUnverified(token string) (*JWTClaims, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("%w: empty JWT token", types.ErrParseFailed)
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("%w: malformed JWT structure (expected 3 segments, got %d)", types.ErrParseFailed, len(parts))
	}

	payloadSegment := parts[1]
	payloadBytes, err := decodeBase64URL(payloadSegment)
	if err != nil {
		return nil, fmt.Errorf("%w: decoding JWT payload base64: %v", types.ErrParseFailed, err)
	}

	var raw map[string]any
	if err := json.Unmarshal(payloadBytes, &raw); err != nil {
		return nil, fmt.Errorf("%w: unmarshaling JWT payload JSON: %v", types.ErrParseFailed, err)
	}

	claims := &JWTClaims{}

	// Extract top-level email
	if email, ok := raw["email"].(string); ok {
		claims.Email = strings.TrimSpace(email)
	}

	// Extract namespaced OpenAI claims under "https://api.openai.com/auth"
	if nsVal, ok := raw[OpenAIAuthNamespace]; ok {
		if nsMap, ok := nsVal.(map[string]any); ok {
			if accID, ok := nsMap["chatgpt_account_id"].(string); ok {
				claims.ChatGPTAccountID = strings.TrimSpace(accID)
			}
			if plan, ok := nsMap["chatgpt_plan_type"].(string); ok {
				claims.ChatGPTPlanType = strings.TrimSpace(plan)
			}
			if userID, ok := nsMap["chatgpt_user_id"].(string); ok {
				claims.ChatGPTUserID = strings.TrimSpace(userID)
			}
		}
	}

	// Fallbacks if claims were not present in namespace
	if claims.ChatGPTAccountID == "" {
		if accID, ok := raw["chatgpt_account_id"].(string); ok {
			claims.ChatGPTAccountID = strings.TrimSpace(accID)
		} else if accID, ok := raw["account_id"].(string); ok {
			claims.ChatGPTAccountID = strings.TrimSpace(accID)
		}
	}

	if claims.ChatGPTPlanType == "" {
		if plan, ok := raw["chatgpt_plan_type"].(string); ok {
			claims.ChatGPTPlanType = strings.TrimSpace(plan)
		} else if plan, ok := raw["plan"].(string); ok {
			claims.ChatGPTPlanType = strings.TrimSpace(plan)
		} else if plan, ok := raw["plan_type"].(string); ok {
			claims.ChatGPTPlanType = strings.TrimSpace(plan)
		}
	}

	return claims, nil
}

// decodeBase64URL decodes URL-safe base64 data, handling both unpadded (RawURLEncoding)
// and padded (URLEncoding) variants defensively.
func decodeBase64URL(s string) ([]byte, error) {
	if s == "" {
		return nil, fmt.Errorf("empty base64 string")
	}

	// Try standard RawURLEncoding first (most common for JWTs)
	if data, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return data, nil
	}

	// Try standard URLEncoding with padding
	if data, err := base64.URLEncoding.DecodeString(s); err == nil {
		return data, nil
	}

	// Compute required '=' padding if missing
	padded := s
	switch len(padded) % 4 {
	case 2:
		padded += "=="
	case 3:
		padded += "="
	}
	if data, err := base64.URLEncoding.DecodeString(padded); err == nil {
		return data, nil
	}

	// Also fallback to StandardEncoding in case of standard base64 encoding
	if data, err := base64.RawStdEncoding.DecodeString(s); err == nil {
		return data, nil
	}
	return base64.StdEncoding.DecodeString(padded)
}
