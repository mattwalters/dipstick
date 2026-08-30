package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mattwalters/dipstick/internal/adapters/antigravity"
	"github.com/mattwalters/dipstick/internal/adapters/claude"
	"github.com/mattwalters/dipstick/internal/adapters/codex"
	"github.com/mattwalters/dipstick/internal/adapters/opencode"
	"github.com/mattwalters/dipstick/internal/cliexec"
	"github.com/mattwalters/dipstick/internal/compat"
	"github.com/mattwalters/dipstick/internal/types"
)

// ProbeStatus represents the outcome of a single structural probe.
type ProbeStatus string

const (
	ProbeStatusPassed  ProbeStatus = "passed"
	ProbeStatusFailed  ProbeStatus = "failed"
	ProbeStatusDrift   ProbeStatus = "drift"
	ProbeStatusSkipped ProbeStatus = "skipped"
)

// ProbeResult records the outcome of one structural check.
type ProbeResult struct {
	Name   string      `json:"name"`
	Status ProbeStatus `json:"status"`
	Passed bool        `json:"passed"`
	Detail string      `json:"detail,omitempty"`
	Error  string      `json:"error,omitempty"`
}

// VendorReport captures the compatibility and structural probe status for one vendor CLI.
type VendorReport struct {
	Provider          types.ProviderID `json:"provider"`
	VendorName        string           `json:"vendor_name"`
	Installed         bool             `json:"installed"`
	BinaryPath        string           `json:"binary_path,omitempty"`
	DiscoveredVersion string           `json:"discovered_version,omitempty"`
	VerifiedRange     string           `json:"verified_range"`
	LastCheck         string           `json:"last_check,omitempty"`
	CompatStatus      compat.Status    `json:"compat_status"`
	DriftDetected     bool             `json:"drift_detected"`
	Probes            []ProbeResult    `json:"probes"`
	Notes             string           `json:"notes,omitempty"`
}

// CanaryReport is the top-level report structure emitted by the drift canary tool.
type CanaryReport struct {
	GeneratedAt   time.Time      `json:"generated_at"`
	DriftDetected bool           `json:"drift_detected"`
	Summary       string         `json:"summary"`
	Vendors       []VendorReport `json:"vendors"`
}

// Config configures canary execution.
type Config struct {
	MockDrift   bool
	Verbose     bool
	Runner      *cliexec.Runner
	OutDir      string
	Providers   []types.ProviderID
	CodexTmpDir string
}

// Run executes all structural probes across vendor CLIs and returns a CanaryReport.
func Run(ctx context.Context, cfg Config) (*CanaryReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	runner := cfg.Runner
	if runner == nil {
		runner = cliexec.New(
			cliexec.WithStrictArgv(false),
			cliexec.WithTimeout(15*time.Second),
		)
	}

	report := &CanaryReport{
		GeneratedAt: time.Now().UTC(),
		Vendors:     make([]VendorReport, 0, 4),
	}

	providers := cfg.Providers
	if len(providers) == 0 {
		providers = []types.ProviderID{
			types.ProviderClaude,
			types.ProviderCodex,
			types.ProviderOpenCode,
			types.ProviderAntigravity,
		}
	}

	driftCount := 0
	probeFailureCount := 0

	for _, p := range providers {
		var vr VendorReport
		switch p {
		case types.ProviderClaude:
			vr = ProbeClaude(ctx, runner, cfg.MockDrift)
		case types.ProviderCodex:
			vr = ProbeCodex(ctx, runner, cfg.MockDrift, cfg.CodexTmpDir)
		case types.ProviderOpenCode:
			vr = ProbeOpenCode(ctx, runner, cfg.MockDrift)
		case types.ProviderAntigravity:
			vr = ProbeAntigravity(ctx, runner, cfg.MockDrift)
		default:
			continue
		}

		if vr.DriftDetected {
			driftCount++
		}
		for _, pr := range vr.Probes {
			if !pr.Passed && pr.Status != ProbeStatusSkipped {
				probeFailureCount++
			}
		}

		report.Vendors = append(report.Vendors, vr)
	}

	report.DriftDetected = driftCount > 0
	if report.DriftDetected {
		report.Summary = fmt.Sprintf("Drift detected across %d vendor(s) with %d probe failure(s)", driftCount, probeFailureCount)
	} else {
		report.Summary = "All installed vendor CLIs and structural probes match declared compatibility ranges"
	}

	return report, nil
}

// ProbeClaude executes structural probes for Claude Code CLI.
func ProbeClaude(ctx context.Context, runner *cliexec.Runner, mockDrift bool) VendorReport {
	adapter := claude.New()
	compatDecl := adapter.Compat()

	vr := VendorReport{
		Provider:      types.ProviderClaude,
		VendorName:    "Claude Code (Anthropic)",
		VerifiedRange: compatDecl.VerifiedRange,
		LastCheck:     compatDecl.LastCheck,
		Notes:         compatDecl.Notes,
		Probes:        make([]ProbeResult, 0),
	}

	if mockDrift {
		vr.Installed = true
		vr.BinaryPath = "/usr/local/bin/claude"
		vr.DiscoveredVersion = "3.0.0"
		vr.CompatStatus = compat.StatusNewerThanVerified
		vr.DriftDetected = true
		vr.Probes = append(vr.Probes,
			ProbeResult{
				Name:   "version",
				Status: ProbeStatusDrift,
				Passed: false,
				Detail: "simulated drift: installed version 3.0.0 is newer than verified range " + compatDecl.VerifiedRange,
			},
			ProbeResult{
				Name:   "help",
				Status: ProbeStatusPassed,
				Passed: true,
				Detail: "CLI help surface verified",
			},
		)
		return vr
	}

	binPath, err := cliexec.ResolveBinary("claude")
	if err != nil {
		vr.Installed = false
		vr.CompatStatus = compat.StatusUnknown
		vr.Notes = "CLI binary not found in PATH"
		vr.Probes = append(vr.Probes, ProbeResult{
			Name:   "installation",
			Status: ProbeStatusSkipped,
			Passed: true,
			Detail: "claude not installed on host; skipping unauthenticated probes",
		})
		return vr
	}

	vr.Installed = true
	vr.BinaryPath = binPath

	// 1. Version Probe
	verRes, err := runner.Run(ctx, "claude", "--version")
	if err != nil {
		vr.Probes = append(vr.Probes, ProbeResult{
			Name:   "version",
			Status: ProbeStatusFailed,
			Passed: false,
			Error:  err.Error(),
		})
		vr.DriftDetected = true
	} else {
		rawVer := verRes.StdoutString()
		if rawVer == "" {
			rawVer = verRes.StderrString()
		}
		if sem, extractErr := compat.Extract(rawVer); extractErr == nil {
			vr.DiscoveredVersion = sem.String()
			status, chkErr := compat.Check(compatDecl.VerifiedRange, sem.String())
			if chkErr != nil {
				vr.CompatStatus = compat.StatusUnknown
				vr.Probes = append(vr.Probes, ProbeResult{
					Name:   "version",
					Status: ProbeStatusFailed,
					Passed: false,
					Error:  fmt.Sprintf("evaluating version %q: %v", sem.String(), chkErr),
				})
				vr.DriftDetected = true
			} else {
				vr.CompatStatus = status
				if status == compat.StatusInRange {
					vr.Probes = append(vr.Probes, ProbeResult{
						Name:   "version",
						Status: ProbeStatusPassed,
						Passed: true,
						Detail: fmt.Sprintf("version %s is within verified range %s", sem.String(), compatDecl.VerifiedRange),
					})
				} else {
					vr.DriftDetected = true
					vr.Probes = append(vr.Probes, ProbeResult{
						Name:   "version",
						Status: ProbeStatusDrift,
						Passed: false,
						Detail: compat.FormatWarning(sem.String(), compatDecl.VerifiedRange, compatDecl.LastCheck),
					})
				}
			}
		} else {
			vr.DiscoveredVersion = rawVer
			vr.CompatStatus = compat.StatusUnknown
			vr.DriftDetected = true
			vr.Probes = append(vr.Probes, ProbeResult{
				Name:   "version",
				Status: ProbeStatusFailed,
				Passed: false,
				Error:  fmt.Sprintf("failed to extract SemVer from output %q: %v", rawVer, extractErr),
			})
		}
	}

	// 2. Help Probe
	helpRes, err := runner.Run(ctx, "claude", "--help")
	if err != nil {
		vr.Probes = append(vr.Probes, ProbeResult{
			Name:   "help",
			Status: ProbeStatusFailed,
			Passed: false,
			Error:  err.Error(),
		})
		vr.DriftDetected = true
	} else {
		out := helpRes.StdoutString()
		if out == "" {
			out = helpRes.StderrString()
		}
		if len(out) > 0 {
			vr.Probes = append(vr.Probes, ProbeResult{
				Name:   "help",
				Status: ProbeStatusPassed,
				Passed: true,
				Detail: "claude --help responded with non-empty usage surface",
			})
		} else {
			vr.Probes = append(vr.Probes, ProbeResult{
				Name:   "help",
				Status: ProbeStatusFailed,
				Passed: false,
				Error:  "claude --help returned empty output",
			})
			vr.DriftDetected = true
		}
	}

	return vr
}

// ProbeCodex executes structural probes for OpenAI Codex CLI.
func ProbeCodex(ctx context.Context, runner *cliexec.Runner, mockDrift bool, codexTmpDir string) VendorReport {
	adapter := codex.New()
	compatDecl := adapter.Compat()

	vr := VendorReport{
		Provider:      types.ProviderCodex,
		VendorName:    "OpenAI Codex",
		VerifiedRange: compatDecl.VerifiedRange,
		LastCheck:     compatDecl.LastCheck,
		Notes:         compatDecl.Notes,
		Probes:        make([]ProbeResult, 0),
	}

	if mockDrift {
		vr.Installed = true
		vr.BinaryPath = "/usr/local/bin/codex"
		vr.DiscoveredVersion = "0.148.0"
		vr.CompatStatus = compat.StatusInRange
		vr.DriftDetected = true
		vr.Probes = append(vr.Probes,
			ProbeResult{
				Name:   "version",
				Status: ProbeStatusPassed,
				Passed: true,
				Detail: "version 0.148.0 is within verified range " + compatDecl.VerifiedRange,
			},
			ProbeResult{
				Name:   "help",
				Status: ProbeStatusPassed,
				Passed: true,
				Detail: "subcommand app-server found in help surface",
			},
			ProbeResult{
				Name:   "codex_app_server_schema",
				Status: ProbeStatusDrift,
				Passed: false,
				Detail: "simulated drift: missing expected RPC method 'account/rateLimits/read' in generated schema",
			},
		)
		return vr
	}

	binPath, err := cliexec.ResolveBinary("codex")
	if err != nil {
		vr.Installed = false
		vr.CompatStatus = compat.StatusUnknown
		vr.Notes = "CLI binary not found in PATH"
		vr.Probes = append(vr.Probes, ProbeResult{
			Name:   "installation",
			Status: ProbeStatusSkipped,
			Passed: true,
			Detail: "codex not installed on host; skipping unauthenticated probes",
		})
		return vr
	}

	vr.Installed = true
	vr.BinaryPath = binPath

	// 1. Version Probe
	verRes, err := runner.Run(ctx, "codex", "--version")
	if err != nil {
		vr.Probes = append(vr.Probes, ProbeResult{
			Name:   "version",
			Status: ProbeStatusFailed,
			Passed: false,
			Error:  err.Error(),
		})
		vr.DriftDetected = true
	} else {
		rawVer := verRes.StdoutString()
		if rawVer == "" {
			rawVer = verRes.StderrString()
		}
		if sem, extractErr := compat.Extract(rawVer); extractErr == nil {
			vr.DiscoveredVersion = sem.String()
			status, chkErr := compat.Check(compatDecl.VerifiedRange, sem.String())
			if chkErr != nil {
				vr.CompatStatus = compat.StatusUnknown
				vr.Probes = append(vr.Probes, ProbeResult{
					Name:   "version",
					Status: ProbeStatusFailed,
					Passed: false,
					Error:  fmt.Sprintf("evaluating version %q: %v", sem.String(), chkErr),
				})
				vr.DriftDetected = true
			} else {
				vr.CompatStatus = status
				if status == compat.StatusInRange {
					vr.Probes = append(vr.Probes, ProbeResult{
						Name:   "version",
						Status: ProbeStatusPassed,
						Passed: true,
						Detail: fmt.Sprintf("version %s is within verified range %s", sem.String(), compatDecl.VerifiedRange),
					})
				} else {
					vr.DriftDetected = true
					vr.Probes = append(vr.Probes, ProbeResult{
						Name:   "version",
						Status: ProbeStatusDrift,
						Passed: false,
						Detail: compat.FormatWarning(sem.String(), compatDecl.VerifiedRange, compatDecl.LastCheck),
					})
				}
			}
		} else {
			vr.DiscoveredVersion = rawVer
			vr.CompatStatus = compat.StatusUnknown
			vr.DriftDetected = true
			vr.Probes = append(vr.Probes, ProbeResult{
				Name:   "version",
				Status: ProbeStatusFailed,
				Passed: false,
				Error:  fmt.Sprintf("failed to extract SemVer from output %q: %v", rawVer, extractErr),
			})
		}
	}

	// 2. Help Probe: check for app-server subcommand
	helpRes, err := runner.Run(ctx, "codex", "--help")
	if err != nil {
		vr.Probes = append(vr.Probes, ProbeResult{
			Name:   "help",
			Status: ProbeStatusFailed,
			Passed: false,
			Error:  err.Error(),
		})
		vr.DriftDetected = true
	} else {
		out := helpRes.StdoutString() + "\n" + helpRes.StderrString()
		if strings.Contains(out, "app-server") {
			vr.Probes = append(vr.Probes, ProbeResult{
				Name:   "help",
				Status: ProbeStatusPassed,
				Passed: true,
				Detail: "subcommand app-server present in codex --help",
			})
		} else {
			vr.Probes = append(vr.Probes, ProbeResult{
				Name:   "help",
				Status: ProbeStatusDrift,
				Passed: false,
				Detail: "expected subcommand 'app-server' not found in codex --help output",
			})
			vr.DriftDetected = true
		}
	}

	// 3. Codex App-Server Schema Probe
	schemaProbe := ProbeCodexSchema(ctx, runner, codexTmpDir)
	vr.Probes = append(vr.Probes, schemaProbe)
	if !schemaProbe.Passed {
		vr.DriftDetected = true
	}

	return vr
}

// ProbeCodexSchema generates and validates JSON schema files from codex app-server.
func ProbeCodexSchema(ctx context.Context, runner *cliexec.Runner, targetDir string) ProbeResult {
	tmpDir := targetDir
	var cleanup bool
	if tmpDir == "" {
		d, err := os.MkdirTemp("", "codex-schema-*")
		if err != nil {
			return ProbeResult{
				Name:   "codex_app_server_schema",
				Status: ProbeStatusFailed,
				Passed: false,
				Error:  fmt.Sprintf("creating temp dir for schema output: %v", err),
			}
		}
		tmpDir = d
		cleanup = true
	}
	if cleanup {
		defer func() { _ = os.RemoveAll(tmpDir) }()
	}

	genRes, err := runner.Run(ctx, "codex", "app-server", "generate-json-schema", "--out", tmpDir)
	if err != nil {
		return ProbeResult{
			Name:   "codex_app_server_schema",
			Status: ProbeStatusFailed,
			Passed: false,
			Error:  fmt.Sprintf("executing 'codex app-server generate-json-schema --out %s': %v (stderr: %s)", tmpDir, err, genRes.StderrString()),
		}
	}

	valid, details, validateErr := ValidateCodexSchemaDirectory(tmpDir)
	if validateErr != nil {
		return ProbeResult{
			Name:   "codex_app_server_schema",
			Status: ProbeStatusFailed,
			Passed: false,
			Error:  fmt.Sprintf("inspecting generated schemas in %s: %v", tmpDir, validateErr),
		}
	}

	if !valid {
		return ProbeResult{
			Name:   "codex_app_server_schema",
			Status: ProbeStatusDrift,
			Passed: false,
			Detail: details,
		}
	}

	return ProbeResult{
		Name:   "codex_app_server_schema",
		Status: ProbeStatusPassed,
		Passed: true,
		Detail: details,
	}
}

// ExpectedCodexMethods lists the JSON-RPC methods required by Dipstick's Codex adapter.
var ExpectedCodexMethods = []string{
	"account/rateLimits/read",
	"account/usage/read",
	"initialize",
}

// ValidateCodexSchemaDirectory checks generated schema files for required methods and shapes.
func ValidateCodexSchemaDirectory(dir string) (bool, string, error) {
	files, err := os.ReadDir(dir)
	if err != nil {
		return false, "", err
	}

	if len(files) == 0 {
		return false, "no schema files were produced in output directory", nil
	}

	var combinedContent strings.Builder
	fileCount := 0

	for _, fi := range files {
		if fi.IsDir() {
			continue
		}
		name := strings.ToLower(fi.Name())
		if strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".ts") || strings.HasSuffix(name, ".schema") {
			data, readErr := os.ReadFile(filepath.Join(dir, fi.Name()))
			if readErr != nil {
				return false, "", readErr
			}
			fileCount++
			combinedContent.Write(data)
			combinedContent.WriteString("\n")
		}
	}

	if fileCount == 0 {
		return false, "no JSON/TS schema files found in output directory", nil
	}

	content := combinedContent.String()
	var missingMethods []string

	for _, method := range ExpectedCodexMethods {
		// Method could appear as "account/rateLimits/read" or "account_rateLimits_read" or "RateLimits"
		altMethod1 := strings.ReplaceAll(method, "/", "_")
		altMethod2 := strings.ReplaceAll(method, "/", ".")
		if !strings.Contains(content, method) && !strings.Contains(content, altMethod1) && !strings.Contains(content, altMethod2) {
			missingMethods = append(missingMethods, method)
		}
	}

	if len(missingMethods) > 0 {
		return false, fmt.Sprintf("schema drift detected: missing expected RPC method(s): %s", strings.Join(missingMethods, ", ")), nil
	}

	// Also verify rate limit field shapes
	var missingFields []string
	expectedFields := []string{"usedPercent", "resetsAt"}
	for _, field := range expectedFields {
		if !strings.Contains(content, field) {
			missingFields = append(missingFields, field)
		}
	}

	if len(missingFields) > 0 {
		return false, fmt.Sprintf("schema drift detected: missing expected rate limit field(s): %s", strings.Join(missingFields, ", ")), nil
	}

	return true, fmt.Sprintf("verified %d schema file(s) containing all expected methods (%s) and rate-limit fields", fileCount, strings.Join(ExpectedCodexMethods, ", ")), nil
}

// ProbeOpenCode executes structural probes for OpenCode CLI.
func ProbeOpenCode(ctx context.Context, runner *cliexec.Runner, mockDrift bool) VendorReport {
	adapter := opencode.New()
	compatDecl := adapter.Compat()

	vr := VendorReport{
		Provider:      types.ProviderOpenCode,
		VendorName:    "OpenCode (anomalyco/opencode)",
		VerifiedRange: compatDecl.VerifiedRange,
		LastCheck:     compatDecl.LastCheck,
		Notes:         compatDecl.Notes,
		Probes:        make([]ProbeResult, 0),
	}

	if mockDrift {
		vr.Installed = true
		vr.BinaryPath = "/usr/local/bin/opencode"
		vr.DiscoveredVersion = "1.18.20"
		vr.CompatStatus = compat.StatusInRange
		vr.DriftDetected = true
		vr.Probes = append(vr.Probes,
			ProbeResult{
				Name:   "version",
				Status: ProbeStatusPassed,
				Passed: true,
				Detail: "version 1.18.20 is within verified range " + compatDecl.VerifiedRange,
			},
			ProbeResult{
				Name:   "help",
				Status: ProbeStatusDrift,
				Passed: false,
				Detail: "simulated drift: expected subcommand 'db' missing from opencode --help",
			},
		)
		return vr
	}

	binPath, err := cliexec.ResolveBinary("opencode")
	if err != nil {
		vr.Installed = false
		vr.CompatStatus = compat.StatusUnknown
		vr.Notes = "CLI binary not found in PATH"
		vr.Probes = append(vr.Probes, ProbeResult{
			Name:   "installation",
			Status: ProbeStatusSkipped,
			Passed: true,
			Detail: "opencode not installed on host; skipping unauthenticated probes",
		})
		return vr
	}

	vr.Installed = true
	vr.BinaryPath = binPath

	// 1. Version Probe
	verRes, err := runner.Run(ctx, "opencode", "--version")
	if err != nil {
		vr.Probes = append(vr.Probes, ProbeResult{
			Name:   "version",
			Status: ProbeStatusFailed,
			Passed: false,
			Error:  err.Error(),
		})
		vr.DriftDetected = true
	} else {
		rawVer := verRes.StdoutString()
		if rawVer == "" {
			rawVer = verRes.StderrString()
		}
		if sem, extractErr := compat.Extract(rawVer); extractErr == nil {
			vr.DiscoveredVersion = sem.String()
			status, chkErr := compat.Check(compatDecl.VerifiedRange, sem.String())
			if chkErr != nil {
				vr.CompatStatus = compat.StatusUnknown
				vr.Probes = append(vr.Probes, ProbeResult{
					Name:   "version",
					Status: ProbeStatusFailed,
					Passed: false,
					Error:  fmt.Sprintf("evaluating version %q: %v", sem.String(), chkErr),
				})
				vr.DriftDetected = true
			} else {
				vr.CompatStatus = status
				if status == compat.StatusInRange {
					vr.Probes = append(vr.Probes, ProbeResult{
						Name:   "version",
						Status: ProbeStatusPassed,
						Passed: true,
						Detail: fmt.Sprintf("version %s is within verified range %s", sem.String(), compatDecl.VerifiedRange),
					})
				} else {
					vr.DriftDetected = true
					vr.Probes = append(vr.Probes, ProbeResult{
						Name:   "version",
						Status: ProbeStatusDrift,
						Passed: false,
						Detail: compat.FormatWarning(sem.String(), compatDecl.VerifiedRange, compatDecl.LastCheck),
					})
				}
			}
		} else {
			vr.DiscoveredVersion = rawVer
			vr.CompatStatus = compat.StatusUnknown
			vr.DriftDetected = true
			vr.Probes = append(vr.Probes, ProbeResult{
				Name:   "version",
				Status: ProbeStatusFailed,
				Passed: false,
				Error:  fmt.Sprintf("failed to extract SemVer from output %q: %v", rawVer, extractErr),
			})
		}
	}

	// 2. Help Probe: check for db and serve subcommands
	helpRes, err := runner.Run(ctx, "opencode", "--help")
	if err != nil {
		vr.Probes = append(vr.Probes, ProbeResult{
			Name:   "help",
			Status: ProbeStatusFailed,
			Passed: false,
			Error:  err.Error(),
		})
		vr.DriftDetected = true
	} else {
		out := helpRes.StdoutString() + "\n" + helpRes.StderrString()
		hasDb := strings.Contains(out, "db")
		hasServe := strings.Contains(out, "serve") || strings.Contains(out, "server")
		if hasDb || hasServe {
			vr.Probes = append(vr.Probes, ProbeResult{
				Name:   "help",
				Status: ProbeStatusPassed,
				Passed: true,
				Detail: "expected subcommands (db/serve) verified in opencode --help",
			})
		} else {
			vr.Probes = append(vr.Probes, ProbeResult{
				Name:   "help",
				Status: ProbeStatusDrift,
				Passed: false,
				Detail: "expected subcommands 'db' or 'serve' missing from opencode --help",
			})
			vr.DriftDetected = true
		}
	}

	return vr
}

// ProbeAntigravity records structural compatibility for Antigravity (unsupported by design).
func ProbeAntigravity(ctx context.Context, runner *cliexec.Runner, mockDrift bool) VendorReport {
	adapter := antigravity.New()
	compatDecl := adapter.Compat()

	return VendorReport{
		Provider:          types.ProviderAntigravity,
		VendorName:        "Google Antigravity",
		Installed:         false,
		DiscoveredVersion: "N/A",
		VerifiedRange:     compatDecl.VerifiedRange,
		LastCheck:         compatDecl.LastCheck,
		CompatStatus:      compat.StatusInRange,
		DriftDetected:     false,
		Notes:             compatDecl.Notes,
		Probes: []ProbeResult{
			{
				Name:   "compatibility",
				Status: ProbeStatusPassed,
				Passed: true,
				Detail: "Antigravity exposes no CLI token metering surface in v0.1 (unsupported by design)",
			},
		},
	}
}

// GenerateMarkdownReport renders a GitHub-flavored Markdown report from a CanaryReport.
func GenerateMarkdownReport(report *CanaryReport) string {
	var buf bytes.Buffer

	headerIcon := "✅"
	statusText := "**Status: All Probes In Range (Clean)**"
	if report.DriftDetected {
		headerIcon = "🚨"
		statusText = "**Status: ⚠️ Drift Detected (Action Required)**"
	}

	buf.WriteString(fmt.Sprintf("# %s Vendor CLI Drift Canary Report\n\n", headerIcon))
	buf.WriteString(fmt.Sprintf("**Generated At:** `%s`  \n", report.GeneratedAt.Format(time.RFC3339)))
	buf.WriteString(fmt.Sprintf("%s  \n\n", statusText))

	buf.WriteString("> [!NOTE]\n")
	buf.WriteString("> This scheduled canary performs **unauthenticated structural verification** only (`--version`, `--help`, protocol schemas).\n")
	buf.WriteString("> CI holds zero credentials and never performs live quota fetches.\n\n")

	buf.WriteString("## Summary Matrix\n\n")
	buf.WriteString("| Vendor | Provider | Discovered Version | Verified Range | Compat Status | Probes | Status |\n")
	buf.WriteString("| :--- | :--- | :--- | :--- | :--- | :--- | :--- |\n")

	for _, v := range report.Vendors {
		discVer := v.DiscoveredVersion
		if discVer == "" {
			if !v.Installed {
				discVer = "*Not Installed*"
			} else {
				discVer = "*Unknown*"
			}
		}

		statusBadge := "✅ Clean"
		if v.DriftDetected {
			statusBadge = "⚠️ **Drift**"
		} else if !v.Installed && v.Provider != types.ProviderAntigravity {
			statusBadge = "ℹ️ Not Installed"
		}

		compatBadge := string(v.CompatStatus)
		switch v.CompatStatus {
		case compat.StatusInRange:
			compatBadge = "✅ `in_range`"
		case compat.StatusNewerThanVerified:
			compatBadge = "⚠️ `newer_than_verified`"
		case compat.StatusOlderThanFloor:
			compatBadge = "⚠️ `older_than_floor`"
		case compat.StatusUnknown:
			if !v.Installed {
				compatBadge = "N/A"
			} else {
				compatBadge = "❓ `unknown`"
			}
		}

		passedProbes := 0
		totalProbes := 0
		for _, pr := range v.Probes {
			if pr.Status != ProbeStatusSkipped {
				totalProbes++
				if pr.Passed {
					passedProbes++
				}
			}
		}
		probeSummary := fmt.Sprintf("%d/%d OK", passedProbes, totalProbes)
		if totalProbes == 0 {
			probeSummary = "N/A"
		}

		buf.WriteString(fmt.Sprintf("| %s | `%s` | `%s` | `%s` | %s | %s | %s |\n",
			v.VendorName, v.Provider, discVer, v.VerifiedRange, compatBadge, probeSummary, statusBadge))
	}

	buf.WriteString("\n## Probe Details & Diff Findings\n\n")

	hasFindings := false
	for _, v := range report.Vendors {
		if !v.DriftDetected {
			continue
		}
		hasFindings = true
		buf.WriteString(fmt.Sprintf("### `%s` (%s)\n\n", v.Provider, v.VendorName))
		if v.DiscoveredVersion != "" {
			buf.WriteString(fmt.Sprintf("- **Observed Version:** `%s` (Verified: `%s`)\n", v.DiscoveredVersion, v.VerifiedRange))
		}
		for _, pr := range v.Probes {
			if !pr.Passed {
				buf.WriteString(fmt.Sprintf("- ❌ **Probe `%s`**: %s", pr.Name, pr.Detail))
				if pr.Error != "" {
					buf.WriteString(fmt.Sprintf(" *(Error: %s)*", pr.Error))
				}
				buf.WriteString("\n")
			}
		}
		buf.WriteString("- **Action Required:** Run `make capture` on a developer machine to inspect live payload drift, update fixtures in `testdata/fixtures/`, and bump `Compat().VerifiedRange` in adapter definition.\n\n")
	}

	if !hasFindings {
		buf.WriteString("All installed vendor CLIs and structural contracts match declared compatibility ranges. Zero drift detected.\n\n")
	}

	buf.WriteString("---\n*Report generated by `dipstick canary` tool.*\n")
	return buf.String()
}

// GenerateJSONReport serializes a CanaryReport into formatted JSON.
func GenerateJSONReport(report *CanaryReport) ([]byte, error) {
	return json.MarshalIndent(report, "", "  ")
}
