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
	paramCredsQuoted = regexp.MustCompile(`(?i)(["']?)\b(password|access_token|refresh_token|auth_token|api_key|apikey|client_secret|client_key|secret_key|private_key|token|secret|key)\b(["']?)(\s*[:=]\s*)(["'])([^"'\r\n]+)(["'])`)

	// paramCredsUnquoted matches unquoted key-value pairs in URL query strings, CLI flags, plain text:
	// token=secret, password=secret, key=secret
	paramCredsUnquoted = regexp.MustCompile(`(?i)(["']?)\b(password|access_token|refresh_token|auth_token|api_key|apikey|client_secret|client_key|secret_key|private_key|token|secret|key)\b(["']?)(\s*[:=]\s*)([^\s,"';&]+)`)
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
		if len(submatches) >= 8 {
			val := submatches[6]
			if val == Redacted || strings.Contains(val, Redacted) {
				return match
			}
			return submatches[1] + submatches[2] + submatches[3] + submatches[4] + submatches[5] + Redacted + submatches[7]
		}
		return match
	})

	// 7. Scrub unquoted key-value parameters
	res = paramCredsUnquoted.ReplaceAllStringFunc(res, func(match string) string {
		submatches := paramCredsUnquoted.FindStringSubmatch(match)
		if len(submatches) >= 6 {
			val := submatches[5]
			if val == Redacted || strings.Contains(val, Redacted) {
				return match
			}
			return submatches[1] + submatches[2] + submatches[3] + submatches[4] + Redacted
		}
		return match
	})

	return res
}
