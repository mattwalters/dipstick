package dipstick

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mattwalters/dipstick/internal/cliexec"
	"github.com/mattwalters/dipstick/internal/localstate"
	"github.com/mattwalters/dipstick/internal/scrub"
)

// CompatVerdict represents the compatibility evaluation for an installed provider version.
type CompatVerdict string

const (
	CompatVerified          CompatVerdict = "verified"
	CompatNewerThanVerified CompatVerdict = "newer_than_verified"
	CompatOlderThanFloor    CompatVerdict = "older_than_floor"
	CompatUnknown           CompatVerdict = "unknown"
	CompatNotInstalled      CompatVerdict = "not_installed"
)

// ConfigDirInfo captures the configuration directory path and override status.
type ConfigDirInfo struct {
	Path       string `json:"path"`
	Overridden bool   `json:"overridden"`
	EnvVar     string `json:"env_var,omitempty"`
}

// AuthInfo captures the presence and storage location of provider credentials.
// It strictly never contains secret values, hashes, prefixes, or lengths.
type AuthInfo struct {
	Found    bool   `json:"found"`
	Location string `json:"location,omitempty"`
	Type     string `json:"type,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

// DoctorSourceReport records the evaluation of a single tier in a provider's source ladder.
type DoctorSourceReport struct {
	Tier     SourceTier    `json:"tier"`
	SourceID SourceID      `json:"source"`
	Status   AttemptStatus `json:"status"`
	Summary  string        `json:"summary,omitempty"`
	Reason   Reason        `json:"reason,omitempty"`
	Detail   string        `json:"detail,omitempty"`
	Duration time.Duration `json:"duration,omitempty"`
	NextStep string        `json:"next_step,omitempty"`
}

// DoctorProviderReport holds the complete diagnostic result for a single provider.
type DoctorProviderReport struct {
	Provider      ProviderID           `json:"provider"`
	Installed     bool                 `json:"installed"`
	BinaryPath    string               `json:"binary_path,omitempty"`
	Version       string               `json:"version,omitempty"`
	CompatVerdict CompatVerdict        `json:"compat_verdict"`
	CompatRange   string               `json:"compat_range,omitempty"`
	ConfigDir     ConfigDirInfo        `json:"config_dir"`
	Auth          AuthInfo             `json:"auth"`
	Sources       []DoctorSourceReport `json:"sources,omitempty"`
	NextSteps     []string             `json:"next_steps,omitempty"`
}

// DoctorReport is the top-level container for a complete doctor diagnostic run.
type DoctorReport struct {
	SchemaVersion string                 `json:"schema_version,omitempty"`
	GeneratedAt   time.Time              `json:"generated_at"`
	Providers     []DoctorProviderReport `json:"providers"`
}

type providerSpec struct {
	binaryName    string
	minFloor      string
	maxVerified   string
	verifiedRange string
	envOverrides  []string
}

var knownProviderSpecs = map[ProviderID]providerSpec{
	ProviderClaude: {
		binaryName:    "claude",
		minFloor:      "2.1.0",
		maxVerified:   "2.2.0",
		verifiedRange: ">=2.1.0 <2.2.0",
		envOverrides:  []string{"CLAUDE_CONFIG_DIR"},
	},
	ProviderCodex: {
		binaryName:    "codex",
		minFloor:      "0.148.0",
		maxVerified:   "0.150.0",
		verifiedRange: ">=0.148.0 <0.150.0",
		envOverrides:  []string{"CODEX_HOME", "CODEX_CONFIG_DIR"},
	},
	ProviderOpenCode: {
		binaryName:    "opencode",
		minFloor:      "1.18.0",
		maxVerified:   "",
		verifiedRange: ">=1.18.0",
		envOverrides:  []string{"OPENCODE_CONFIG_DIR"},
	},
	ProviderAntigravity: {
		binaryName:    "antigravity",
		minFloor:      "",
		maxVerified:   "",
		verifiedRange: "",
		envOverrides:  []string{"ANTIGRAVITY_CONFIG_DIR"},
	},
}

type semver struct {
	major int
	minor int
	patch int
}

var semverRegex = regexp.MustCompile(`(\d+)\.(\d+)(?:\.(\d+))?`)

func parseSemver(v string) (*semver, bool) {
	m := semverRegex.FindStringSubmatch(v)
	if m == nil {
		return nil, false
	}
	maj, err1 := strconv.Atoi(m[1])
	min, err2 := strconv.Atoi(m[2])
	patch := 0
	if len(m) > 3 && m[3] != "" {
		patch, _ = strconv.Atoi(m[3])
	}
	if err1 != nil || err2 != nil {
		return nil, false
	}
	return &semver{major: maj, minor: min, patch: patch}, true
}

func compareSemver(a, b semver) int {
	if a.major != b.major {
		return a.major - b.major
	}
	if a.minor != b.minor {
		return a.minor - b.minor
	}
	return a.patch - b.patch
}

// Doctor evaluates provider installations, version compatibility, credential storage,
// and full tiered source ladders to diagnose data collection health.
func Doctor(ctx context.Context, opts ...Option) (*DoctorReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	cfg := &config{
		sourcePolicy:  SourcePolicyDefault,
		sourceTimeout: 5 * time.Second,
		adapters:      make(map[ProviderID]Adapter),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}

	if cfg.timeout < 0 {
		return nil, fmt.Errorf("invalid timeout: %v", cfg.timeout)
	}
	if cfg.sourceTimeout < 0 {
		return nil, fmt.Errorf("invalid source timeout: %v", cfg.sourceTimeout)
	}

	if cfg.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.timeout)
		defer cancel()
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	targetProviders := cfg.providers
	if len(targetProviders) == 0 {
		targetProviders = Providers()
		if len(cfg.adapters) > 0 {
			targetProviders = nil
			for id := range cfg.adapters {
				targetProviders = append(targetProviders, id)
			}
			sort.Slice(targetProviders, func(i, j int) bool {
				return targetProviders[i] < targetProviders[j]
			})
		}
	}

	seen := make(map[ProviderID]bool)
	var ordered []ProviderID
	for _, p := range targetProviders {
		if _, ok := cfg.adapters[p]; !ok {
			if _, ok := defaultAdapterRegistry[p]; !ok {
				return nil, fmt.Errorf("unknown provider: %q", p)
			}
		}
		if !seen[p] {
			seen[p] = true
			ordered = append(ordered, p)
		}
	}

	report := &DoctorReport{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   time.Now().UTC(),
		Providers:     make([]DoctorProviderReport, 0, len(ordered)),
	}

	for _, pID := range ordered {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		var adp Adapter
		if custom, ok := cfg.adapters[pID]; ok {
			adp = custom
		} else {
			adp = defaultAdapterRegistry[pID]()
		}

		pReport := diagnoseProvider(ctx, pID, adp, cfg)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		report.Providers = append(report.Providers, pReport)
	}

	return report, nil
}

func diagnoseProvider(ctx context.Context, pID ProviderID, adp Adapter, cfg *config) DoctorProviderReport {
	if ctx.Err() != nil {
		return DoctorProviderReport{Provider: pID}
	}

	spec, hasSpec := knownProviderSpecs[pID]

	cfgDirInfo := probeConfigDir(pID)
	authInfo := probeAuthInfo(ctx, pID)

	var (
		installed     bool
		binaryPath    string
		version       string
		verdict       CompatVerdict
		verdictRange  string
		customSources []Source
	)

	if adp != nil {
		customSources = adp.Sources()
		if det, err := adp.Detect(ctx); err == nil {
			if det.Installed {
				installed = true
				binaryPath = det.BinaryPath
				version = det.Version
			}
		}
	}

	if !installed && hasSpec && spec.binaryName != "" {
		if resolved, err := cliexec.ResolveBinary(spec.binaryName); err == nil && resolved != "" {
			installed = true
			binaryPath = resolved
			if probedVer, pErr := cliexec.ProbeVersion(ctx, spec.binaryName); pErr == nil && probedVer != "" {
				if parsed := semverRegex.FindString(probedVer); parsed != "" {
					version = parsed
				} else {
					version = probedVer
				}
			}
		}
	}

	if !installed {
		verdict = CompatNotInstalled
	} else if version != "" && hasSpec && spec.verifiedRange != "" {
		verdictRange = spec.verifiedRange
		if parsedVer, ok := parseSemver(version); ok {
			if spec.minFloor != "" {
				if minFloor, ok := parseSemver(spec.minFloor); ok {
					if compareSemver(*parsedVer, *minFloor) < 0 {
						verdict = CompatOlderThanFloor
						verdictRange = "<" + spec.minFloor
					}
				}
			}
			if verdict == "" && spec.maxVerified != "" {
				if maxVer, ok := parseSemver(spec.maxVerified); ok {
					if compareSemver(*parsedVer, *maxVer) >= 0 {
						verdict = CompatNewerThanVerified
					}
				}
			}
			if verdict == "" {
				verdict = CompatVerified
			}
		} else {
			verdict = CompatUnknown
		}
	} else if installed {
		verdict = CompatVerified
	}

	sources := sortSourcesByTier(customSources)
	var sourceReports []DoctorSourceReport
	var nextSteps []string
	nextStepSeen := make(map[string]bool)

	addNextStep := func(step string) {
		clean := strings.TrimSpace(step)
		if clean != "" && !nextStepSeen[clean] {
			nextStepSeen[clean] = true
			nextSteps = append(nextSteps, clean)
		}
	}

	higherTierSucceeded := false

	for _, src := range sources {
		if src == nil {
			continue
		}
		if ctx.Err() != nil {
			return DoctorProviderReport{Provider: pID}
		}

		if higherTierSucceeded {
			sourceReports = append(sourceReports, DoctorSourceReport{
				Tier:     src.Tier(),
				SourceID: src.ID(),
				Status:   AttemptStatusSkipped,
				Summary:  "skipped — higher tier succeeded",
				Detail:   "higher tier succeeded",
			})
			continue
		}

		if !cfg.sourcePolicy.Allows(src) {
			sourceReports = append(sourceReports, DoctorSourceReport{
				Tier:     src.Tier(),
				SourceID: src.ID(),
				Status:   AttemptStatusSkipped,
				Summary:  "skipped by source policy",
				Detail:   "excluded by source policy",
			})
			continue
		}

		availStart := time.Now()
		availCtx, availCancel := context.WithTimeout(ctx, cfg.sourceTimeout)

		availCh := make(chan bool, 1)
		go func(s Source, aCtx context.Context) {
			availCh <- s.Available(aCtx)
		}(src, availCtx)

		var isAvailable bool
		var availTimedOut bool

		select {
		case avail := <-availCh:
			availCancel()
			isAvailable = avail
		case <-availCtx.Done():
			availCancel()
			if ctx.Err() != nil {
				return DoctorProviderReport{Provider: pID}
			}
			availTimedOut = true
		}

		duration := time.Since(availStart)

		if availTimedOut {
			step := remediationFor(pID, ReasonTimeout)
			addNextStep(step)
			sourceReports = append(sourceReports, DoctorSourceReport{
				Tier:     src.Tier(),
				SourceID: src.ID(),
				Status:   AttemptStatusTimeout,
				Reason:   ReasonTimeout,
				Duration: duration,
				Detail:   "availability check timed out",
				Summary:  "timeout — availability check timed out",
				NextStep: step,
			})
			continue
		}

		if !isAvailable {
			reason := ReasonNotAuthenticated
			if !installed {
				reason = ReasonNotInstalled
			}
			step := remediationFor(pID, reason)
			addNextStep(step)
			sourceReports = append(sourceReports, DoctorSourceReport{
				Tier:     src.Tier(),
				SourceID: src.ID(),
				Status:   AttemptStatusUnavailable,
				Reason:   reason,
				Duration: duration,
				Detail:   "prerequisites not met",
				Summary:  "unavailable — prerequisites not met",
				NextStep: step,
			})
			continue
		}

		fetchStart := time.Now()
		fetchCtx, fetchCancel := context.WithTimeout(ctx, cfg.sourceTimeout)

		type fetchResult struct {
			report *ProviderReport
			err    error
		}
		resCh := make(chan fetchResult, 1)
		go func(s Source, fCtx context.Context) {
			rep, err := s.Fetch(fCtx)
			resCh <- fetchResult{report: rep, err: err}
		}(src, fetchCtx)

		var rep *ProviderReport
		var fetchErr error
		var fetchTimedOut bool

		select {
		case res := <-resCh:
			fetchCancel()
			rep = res.report
			fetchErr = res.err
		case <-fetchCtx.Done():
			fetchCancel()
			if ctx.Err() != nil {
				return DoctorProviderReport{Provider: pID}
			}
			fetchTimedOut = true
		}

		fetchDuration := time.Since(fetchStart)

		if fetchTimedOut {
			step := remediationFor(pID, ReasonTimeout)
			addNextStep(step)
			sourceReports = append(sourceReports, DoctorSourceReport{
				Tier:     src.Tier(),
				SourceID: src.ID(),
				Status:   AttemptStatusTimeout,
				Reason:   ReasonTimeout,
				Duration: fetchDuration,
				Detail:   ErrSourceTimeout.Error(),
				Summary:  "timeout — source fetch timed out",
				NextStep: step,
			})
			continue
		}

		if fetchErr != nil {
			errReason := ReasonForError(fetchErr)
			if errReason == "" {
				errReason = ReasonUpstreamError
			}
			cleanDetail := scrub.Scrub(fetchErr.Error())
			if sent := errReason.Sentinel(); sent != nil {
				cleanDetail = strings.TrimPrefix(cleanDetail, sent.Error()+": ")
			}
			cleanDetail = strings.TrimPrefix(cleanDetail, string(errReason)+": ")
			cleanDetail = strings.TrimPrefix(cleanDetail, string(errReason)+" — ")

			summary := fmt.Sprintf("%s — %s", errReason, cleanDetail)
			step := remediationFor(pID, errReason)
			addNextStep(step)

			sourceReports = append(sourceReports, DoctorSourceReport{
				Tier:     src.Tier(),
				SourceID: src.ID(),
				Status:   AttemptStatusError,
				Reason:   errReason,
				Duration: fetchDuration,
				Detail:   cleanDetail,
				Summary:  summary,
				NextStep: step,
			})
			continue
		}

		higherTierSucceeded = true
		summary := summarizeSuccessReport(rep)
		sourceReports = append(sourceReports, DoctorSourceReport{
			Tier:     src.Tier(),
			SourceID: src.ID(),
			Status:   AttemptStatusSuccess,
			Duration: fetchDuration,
			Summary:  summary,
		})
	}

	return DoctorProviderReport{
		Provider:      pID,
		Installed:     installed,
		BinaryPath:    binaryPath,
		Version:       version,
		CompatVerdict: verdict,
		CompatRange:   verdictRange,
		ConfigDir:     cfgDirInfo,
		Auth:          authInfo,
		Sources:       sourceReports,
		NextSteps:     nextSteps,
	}
}

func probeConfigDir(pID ProviderID) ConfigDirInfo {
	spec, hasSpec := knownProviderSpecs[pID]
	resolver := localstate.New()
	resolvedPath, _ := resolver.ProviderConfigDir(string(pID))

	if hasSpec {
		for _, env := range spec.envOverrides {
			if val, ok := os.LookupEnv(env); ok && val != "" {
				return ConfigDirInfo{
					Path:       filepath.Clean(val),
					Overridden: true,
					EnvVar:     env,
				}
			}
		}
	}

	return ConfigDirInfo{
		Path:       resolvedPath,
		Overridden: false,
	}
}

func probeAuthInfo(ctx context.Context, pID ProviderID) AuthInfo {
	resolver := localstate.New()

	switch pID {
	case ProviderClaude:
		claudePaths, pErr := resolver.ClaudePaths()
		if pErr == nil && claudePaths != nil {
			if _, statErr := os.Stat(claudePaths.CredentialsFile); statErr == nil {
				return AuthInfo{
					Found:    true,
					Location: fmt.Sprintf("file %s", claudePaths.CredentialsFile),
					Type:     "file",
					Detail:   fmt.Sprintf("token found at %s", claudePaths.CredentialsFile),
				}
			}
		}

		creds, err := resolver.ReadClaudeCredentials(ctx)
		if err == nil && creds != nil {
			return AuthInfo{
				Found:    true,
				Location: fmt.Sprintf("Keychain service `%s`", localstate.ClaudeCredentialService),
				Type:     "keychain",
				Detail:   "token found in Keychain",
			}
		}

		return AuthInfo{
			Found:  false,
			Type:   "none",
			Detail: "no credentials found",
		}

	case ProviderCodex:
		codexPaths, pErr := resolver.CodexPaths()
		if pErr == nil && codexPaths != nil {
			if _, statErr := os.Stat(codexPaths.AuthFile); statErr == nil {
				return AuthInfo{
					Found:    true,
					Location: fmt.Sprintf("file %s", codexPaths.AuthFile),
					Type:     "file",
					Detail:   fmt.Sprintf("token found at %s", codexPaths.AuthFile),
				}
			}
		}
		if key, ok := os.LookupEnv("OPENAI_API_KEY"); ok && key != "" {
			return AuthInfo{
				Found:    true,
				Location: "environment variable `OPENAI_API_KEY`",
				Type:     "env",
				Detail:   "token found in environment",
			}
		}
		return AuthInfo{
			Found:  false,
			Type:   "none",
			Detail: "no credentials found",
		}

	case ProviderOpenCode:
		openCodePaths, pErr := resolver.OpenCodePaths()
		if pErr == nil && openCodePaths != nil {
			if _, statErr := os.Stat(openCodePaths.AuthFile); statErr == nil {
				return AuthInfo{
					Found:    true,
					Location: fmt.Sprintf("file %s", openCodePaths.AuthFile),
					Type:     "file",
					Detail:   fmt.Sprintf("token found at %s", openCodePaths.AuthFile),
				}
			}
		}
		return AuthInfo{
			Found:  false,
			Type:   "none",
			Detail: "no credentials found",
		}

	case ProviderAntigravity:
		return AuthInfo{
			Found:  false,
			Type:   "none",
			Detail: "no credentials found",
		}

	default:
		return AuthInfo{
			Found:  false,
			Type:   "none",
			Detail: "no credentials found",
		}
	}
}

func remediationFor(provider ProviderID, reason Reason) string {
	switch reason {
	case ReasonNotAuthenticated, ReasonCredentialExpired:
		switch provider {
		case ProviderClaude:
			return "run `claude auth`"
		case ProviderCodex:
			return "run `codex auth login`"
		case ProviderOpenCode:
			return "run `opencode auth`"
		default:
			return fmt.Sprintf("run `%s auth`", provider)
		}
	case ReasonNotInstalled:
		switch provider {
		case ProviderClaude:
			return "install claude: npm install -g @anthropic-ai/claude-code"
		case ProviderCodex:
			return "install codex CLI"
		case ProviderOpenCode:
			return "install opencode CLI"
		case ProviderAntigravity:
			return "install antigravity from https://gemini.google.com"
		default:
			return fmt.Sprintf("install %s CLI", provider)
		}
	case ReasonUnsupportedVersion:
		switch provider {
		case ProviderClaude:
			return "update claude: npm install -g @anthropic-ai/claude-code@latest"
		case ProviderCodex:
			return "update codex CLI"
		case ProviderOpenCode:
			return "update opencode CLI"
		default:
			return fmt.Sprintf("update %s to a supported version", provider)
		}
	case ReasonNotSupported:
		if provider == ProviderAntigravity {
			return "antigravity is not supported in dipstick v0.1"
		}
		return ""
	case ReasonTimeout:
		return "check network connectivity or increase timeout with --timeout / --source-timeout"
	case ReasonUpstreamError:
		return "check provider service status and network connectivity"
	default:
		return ""
	}
}

func summarizeSuccessReport(rep *ProviderReport) string {
	if rep == nil {
		return "ok"
	}
	if len(rep.Windows) > 0 {
		var parts []string
		for _, w := range rep.Windows {
			if w.UsedPercent != nil {
				parts = append(parts, fmt.Sprintf("%s %g%%", w.Label, *w.UsedPercent))
			} else if w.Used != nil && w.Limit != nil {
				parts = append(parts, fmt.Sprintf("%s %g/%g", w.Label, *w.Used, *w.Limit))
			} else if w.Label != "" {
				parts = append(parts, w.Label)
			}
		}
		if len(parts) > 0 {
			return fmt.Sprintf("ok (%s)", strings.Join(parts, ", "))
		}
	}
	if rep.Identity != nil {
		return "ok (identity only)"
	}
	if rep.Tokens != nil {
		return "ok (tokens only)"
	}
	return "ok"
}

func formatSourceName(pID ProviderID, sID SourceID) string {
	switch sID {
	case SourceOAuthAPI:
		if pID == ProviderCodex {
			return "usage-api"
		}
		return "oauth-api"
	case SourceLocalState:
		if pID == ProviderCodex {
			return "auth-json"
		}
		return "local-state"
	case SourceAppServer:
		return "app-server"
	case SourceTranscript:
		return "transcripts"
	case SourceCLIStdout:
		return "cli-scrape"
	default:
		return strings.ReplaceAll(string(sID), "_", "-")
	}
}

// RenderText writes the columnar human-readable doctor diagnostic report to w.
func (r *DoctorReport) RenderText(w io.Writer) error {
	for i, p := range r.Providers {
		if i > 0 {
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}

		if !p.Installed {
			if _, err := fmt.Fprintf(w, "%-12s —  ✗ not_installed\n", p.Provider); err != nil {
				return err
			}
			for _, ns := range p.NextSteps {
				if ns != "" {
					if _, err := fmt.Fprintf(w, "  → Next step: %s\n", ns); err != nil {
						return err
					}
				}
			}
			continue
		}

		var sym string
		var verdictStr string
		switch p.CompatVerdict {
		case CompatVerified:
			sym = "✓"
			verdictStr = "verified range"
		case CompatNewerThanVerified:
			sym = "⚠"
			if p.CompatRange != "" {
				verdictStr = fmt.Sprintf("newer than verified (%s)", p.CompatRange)
			} else {
				verdictStr = "newer than verified"
			}
		case CompatOlderThanFloor:
			sym = "✗"
			if p.CompatRange != "" {
				verdictStr = fmt.Sprintf("older than floor (%s)", p.CompatRange)
			} else {
				verdictStr = "older than floor"
			}
		case CompatUnknown:
			sym = "?"
			verdictStr = "unknown version"
		default:
			sym = "✗"
			verdictStr = string(p.CompatVerdict)
		}

		verText := p.Version
		if verText == "" {
			verText = "—"
		}

		if _, err := fmt.Fprintf(w, "%-8s %-8s %s %s\n", p.Provider, verText, sym, verdictStr); err != nil {
			return err
		}

		for _, s := range p.Sources {
			var srcSym string
			switch s.Status {
			case AttemptStatusSuccess:
				srcSym = "✓"
			case AttemptStatusSkipped:
				srcSym = "·"
			default:
				srcSym = "✗"
			}

			srcName := formatSourceName(p.Provider, s.SourceID)
			summaryText := s.Summary
			if summaryText == "" {
				if s.Detail != "" {
					summaryText = s.Detail
				} else if s.Reason != "" {
					summaryText = string(s.Reason)
				} else {
					summaryText = string(s.Status)
				}
			}

			if _, err := fmt.Fprintf(w, "  %s tier %d  %-16s %s\n", srcSym, s.Tier, srcName, summaryText); err != nil {
				return err
			}
		}

		for _, ns := range p.NextSteps {
			if ns != "" {
				if _, err := fmt.Fprintf(w, "  → Next step: %s\n", ns); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
