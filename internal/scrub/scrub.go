package scrub

import (
	"regexp"
	"strings"
)

var (
	// authHeaderRegex matches Authorization headers like:
	// Authorization: Bearer <token>, Authorization: Basic <credentials>, Authorization: <raw-token>
	authHeaderRegex   = regexp.MustCompile(`(?i)(authorization\s*:\s*)(?:bearer|basic|token)\s+[^\s,;]+`)
	authHeaderGeneral = regexp.MustCompile(`(?i)(authorization\s*:\s*)[^\r\n,;]+`)

	// bearerTokenRegex matches Bearer tokens standalone: Bearer <token>
	bearerTokenRegex = regexp.MustCompile(`(?i)\b(bearer\s+)[A-Za-z0-9_\-\.~+/=]+`)

	// basicAuthRegex matches Basic auth credentials standalone: Basic <base64>
	basicAuthRegex = regexp.MustCompile(`(?i)\b(basic\s+)[A-Za-z0-9+/=]{8,}`)

	// cookieHeaderRegex matches Cookie: ... or Set-Cookie: ... headers
	cookieHeaderRegex = regexp.MustCompile(`(?i)\b((?:set-)?cookie\s*:\s*)[^\r\n]+`)

	// jwtRegex matches standard 3-part JSON Web Tokens (header.payload.signature)
	jwtRegex = regexp.MustCompile(`\bey[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`)

	// Known vendor API key and token formats:
	anthropicKeyRegex = regexp.MustCompile(`\bsk-ant-[a-zA-Z0-9_\-]{8,}\b`)
	openAIKeyRegex    = regexp.MustCompile(`\bsk-[a-zA-Z0-9_\-]{16,}\b`)
	githubTokenRegex  = regexp.MustCompile(`\b(?:gh[pousr]_[a-zA-Z0-9]{20,}|github_pat_[a-zA-Z0-9_]{20,})\b`)
	googleAPIKeyRegex = regexp.MustCompile(`\bAIza[0-9A-Za-z\-_]{30,}\b`)
	awsAccessKeyRegex = regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)
	slackTokenRegex   = regexp.MustCompile(`\bxox[baprs]-[a-zA-Z0-9\-]{10,}\b`)
	huggingfaceRegex  = regexp.MustCompile(`\bhf_[a-zA-Z0-9]{20,}\b`)

	// paramCredsQuoted matches quoted key-value pairs in JSON, YAML, configs, CLI flags:
	// "token": "secret", 'password': 'secret', --token="secret"
	paramCredsQuoted = regexp.MustCompile(`(?i)(^|[\s?&,;])(--)?(["']?)(password|access_token|refresh_token|auth_token|api_key|apikey|client_secret|client_key|secret_key|private_key|token|secret|key)(["']?)(\s*[:=]\s*)(["'])([^"'\r\n]+)(["'])`)

	// paramCredsUnquoted matches unquoted key-value pairs in URL query strings, CLI flags, plain text:
	// token=secret, password=secret, key=secret
	paramCredsUnquoted = regexp.MustCompile(`(?i)(^|[\s?&,;])(--)?(["']?)(password|access_token|refresh_token|auth_token|api_key|apikey|client_secret|client_key|secret_key|private_key|token|secret|key)(["']?)(\s*[:=]\s*)([^\s,"';&]+)`)
)

// Redacted is the replacement string used for scrubbed sensitive data.
const Redacted = "[REDACTED]"

// Scrub removes sensitive credentials, tokens, cookies, and authorization headers from input text.
func Scrub(s string) string {
	if s == "" {
		return s
	}

	res := s

	// 1. Scrub Authorization headers
	res = authHeaderRegex.ReplaceAllString(res, "${1}"+Redacted)
	res = authHeaderGeneral.ReplaceAllString(res, "${1}"+Redacted)

	// 2. Scrub Bearer and Basic auth prefixes
	res = bearerTokenRegex.ReplaceAllString(res, "${1}"+Redacted)
	res = basicAuthRegex.ReplaceAllString(res, "${1}"+Redacted)

	// 3. Scrub Cookies
	res = cookieHeaderRegex.ReplaceAllString(res, "${1}"+Redacted)

	// 4. Scrub known API key & token patterns
	res = anthropicKeyRegex.ReplaceAllString(res, Redacted)
	res = openAIKeyRegex.ReplaceAllString(res, Redacted)
	res = githubTokenRegex.ReplaceAllString(res, Redacted)
	res = googleAPIKeyRegex.ReplaceAllString(res, Redacted)
	res = awsAccessKeyRegex.ReplaceAllString(res, Redacted)
	res = slackTokenRegex.ReplaceAllString(res, Redacted)
	res = huggingfaceRegex.ReplaceAllString(res, Redacted)

	// 5. Scrub JWT tokens
	res = jwtRegex.ReplaceAllString(res, Redacted)

	// 6. Scrub quoted key-value parameters
	res = paramCredsQuoted.ReplaceAllStringFunc(res, func(match string) string {
		submatches := paramCredsQuoted.FindStringSubmatch(match)
		if len(submatches) >= 10 {
			val := submatches[8]
			if val == Redacted || strings.Contains(val, Redacted) {
				return match
			}
			return submatches[1] + submatches[2] + submatches[3] + submatches[4] + submatches[5] + submatches[6] + submatches[7] + Redacted + submatches[9]
		}
		return match
	})

	// 7. Scrub unquoted key-value parameters
	res = paramCredsUnquoted.ReplaceAllStringFunc(res, func(match string) string {
		submatches := paramCredsUnquoted.FindStringSubmatch(match)
		if len(submatches) >= 8 {
			val := submatches[7]
			if val == Redacted || strings.Contains(val, Redacted) {
				return match
			}
			return submatches[1] + submatches[2] + submatches[3] + submatches[4] + submatches[5] + submatches[6] + Redacted
		}
		return match
	})

	return res
}

// SecretFinding describes a detected secret, credential, or personal identifier.
type SecretFinding struct {
	Rule    string `json:"rule"`
	Match   string `json:"match"`
	Message string `json:"message"`
}

var (
	emailRegex      = regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@([A-Za-z0-9.-]+\.[A-Za-z]{2,})\b`)
	privateKeyRegex = regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)
)

var allowedEmailDomains = map[string]bool{
	"example.com": true,
	"example.org": true,
	"example.net": true,
	"test.com":    true,
	"test":        true,
	"invalid":     true,
	"localhost":   true,
}

// isSyntheticOrRedacted checks if a matched string is a known synthetic placeholder or redacted marker.
func isSyntheticOrRedacted(s string) bool {
	lower := strings.ToLower(s)
	if strings.Contains(lower, "[redacted]") || strings.Contains(lower, "redacted") {
		return true
	}
	if strings.Contains(lower, "mock") || strings.Contains(lower, "placeholder") || strings.Contains(lower, "dummy") {
		return true
	}
	if strings.Contains(lower, "sk-ant-test") || strings.Contains(lower, "sk-test") {
		return true
	}
	return false
}

// FindSecrets scans input text and returns any detected live secrets, credentials, or personal identifiers.
func FindSecrets(content string) []SecretFinding {
	if content == "" {
		return nil
	}

	var findings []SecretFinding

	// 1. Check for unredacted Authorization headers
	for _, match := range authHeaderRegex.FindAllString(content, -1) {
		if !isSyntheticOrRedacted(match) {
			findings = append(findings, SecretFinding{
				Rule:    "unredacted_auth_header",
				Match:   match,
				Message: "detected unredacted Authorization header",
			})
		}
	}

	// 2. Check for private keys
	for _, match := range privateKeyRegex.FindAllString(content, -1) {
		findings = append(findings, SecretFinding{
			Rule:    "private_key",
			Match:   match,
			Message: "detected private key block",
		})
	}

	// 3. Check for Anthropic API keys
	for _, match := range anthropicKeyRegex.FindAllString(content, -1) {
		if !isSyntheticOrRedacted(match) {
			findings = append(findings, SecretFinding{
				Rule:    "anthropic_api_key",
				Match:   match,
				Message: "detected Anthropic API key format",
			})
		}
	}

	// 4. Check for OpenAI API keys
	for _, match := range openAIKeyRegex.FindAllString(content, -1) {
		if !isSyntheticOrRedacted(match) {
			findings = append(findings, SecretFinding{
				Rule:    "openai_api_key",
				Match:   match,
				Message: "detected OpenAI API key format",
			})
		}
	}

	// 5. Check for GitHub tokens
	for _, match := range githubTokenRegex.FindAllString(content, -1) {
		if !isSyntheticOrRedacted(match) {
			findings = append(findings, SecretFinding{
				Rule:    "github_token",
				Match:   match,
				Message: "detected GitHub personal access token",
			})
		}
	}

	// 6. Check for Google API keys
	for _, match := range googleAPIKeyRegex.FindAllString(content, -1) {
		if !isSyntheticOrRedacted(match) {
			findings = append(findings, SecretFinding{
				Rule:    "google_api_key",
				Match:   match,
				Message: "detected Google API key",
			})
		}
	}

	// 7. Check for AWS access keys
	for _, match := range awsAccessKeyRegex.FindAllString(content, -1) {
		if !isSyntheticOrRedacted(match) {
			findings = append(findings, SecretFinding{
				Rule:    "aws_access_key",
				Match:   match,
				Message: "detected AWS access key identifier",
			})
		}
	}

	// 8. Check for Slack tokens
	for _, match := range slackTokenRegex.FindAllString(content, -1) {
		if !isSyntheticOrRedacted(match) {
			findings = append(findings, SecretFinding{
				Rule:    "slack_token",
				Match:   match,
				Message: "detected Slack token",
			})
		}
	}

	// 9. Check for HuggingFace tokens
	for _, match := range huggingfaceRegex.FindAllString(content, -1) {
		if !isSyntheticOrRedacted(match) {
			findings = append(findings, SecretFinding{
				Rule:    "huggingface_token",
				Match:   match,
				Message: "detected HuggingFace token",
			})
		}
	}

	// 10. Check for real email addresses (must use synthetic domains like example.com)
	emailMatches := emailRegex.FindAllStringSubmatch(content, -1)
	for _, sub := range emailMatches {
		if len(sub) >= 2 {
			fullEmail := sub[0]
			domain := strings.ToLower(sub[1])
			if !allowedEmailDomains[domain] && !isSyntheticOrRedacted(fullEmail) {
				findings = append(findings, SecretFinding{
					Rule:    "real_email_address",
					Match:   fullEmail,
					Message: "detected real (non-synthetic) email address: domain must be example.com/org/net or test.com",
				})
			}
		}
	}

	return findings
}
