# dipstick

Dipstick is a Go library and CLI tool for inspecting local usage, token metering, and rate limit quotas across AI coding agents. It provides a unified, stable data contract (`dipstick.v1`) to help developers, harnesses, and platforms monitor agentic AI resource consumption.

> **Important Caveat**: Dipstick reads local runtime state, credential stores, and uncommitted vendor interfaces that AI agent providers do not formally guarantee. Because upstream vendor releases, local database schemas, or internal RPC endpoints may change without notice, individual provider adapters can break when vendors ship updates. Dipstick is designed with loud degradation and tiered source ladders so callers detect vendor drift immediately rather than receiving silent failures or fabricated metrics.

---

## Go Library Usage

Dipstick is designed library-first. The primary interface is `dipstick.Collect`, which resolves metrics across requested providers using tiered fallback ladders (direct API calls, local state files, RPC servers, session transcripts, and CLI inspection).

### Example

```go
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/mattwalters/dipstick"
)

func main() {
	// 1. Create a context with an overall execution timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 2. Collect usage metrics from specified providers
	report, err := dipstick.Collect(
		ctx,
		dipstick.WithProviders(
			dipstick.ProviderClaude,
			dipstick.ProviderCodex,
			dipstick.ProviderOpenCode,
		),
		dipstick.WithTimeout(8*time.Second),
		dipstick.WithSourcePolicy(dipstick.SourcePolicyDefault),
	)
	if err != nil {
		// Whole-run execution failures: context cancellations, invalid configuration
		log.Fatalf("Fatal collection error: %v", err)
	}

	fmt.Printf("Dipstick Report (Schema: %s, Generated: %s)\n\n",
		report.SchemaVersion, report.GeneratedAt.Format(time.RFC3339))

	// 3. Inspect successful provider reports
	for _, p := range report.Providers {
		fmt.Printf("=== Provider: %s ===\n", p.Provider)
		fmt.Printf("  Source:     %s (Tier %d)\n", p.Source, p.Tier)
		fmt.Printf("  Confidence: %s\n", p.Confidence)
		if p.CLIVersion != "" {
			fmt.Printf("  CLI Vers:   %s\n", p.CLIVersion)
		}

		if p.Identity != nil {
			fmt.Printf("  Identity:   %s (%s, Plan: %s)\n",
				p.Identity.Email, p.Identity.Organization, p.Identity.Plan)
		}

		if len(p.Windows) > 0 {
			fmt.Println("  Rate & Quota Windows:")
			for _, w := range p.Windows {
				fmt.Printf("    - %s: %.1f%% used (%.0f / %.0f, resets %s)\n",
					w.Label, w.UsedPercent, w.Used, w.Limit, w.ResetsAt.Format(time.RFC3339))
			}
		}

		if p.Tokens != nil {
			fmt.Printf("  Token Usage:\n")
			fmt.Printf("    - Total:       %d\n", p.Tokens.TotalTokens)
			fmt.Printf("    - Input:       %d\n", p.Tokens.InputTokens)
			fmt.Printf("    - Output:      %d\n", p.Tokens.OutputTokens)
			fmt.Printf("    - Cache Read:  %d\n", p.Tokens.CacheReadTokens)
			fmt.Printf("    - Cache Write: %d\n", p.Tokens.CacheWriteTokens)
		}
		fmt.Println()
	}

	// 4. Handle non-fatal, partial provider errors and warnings
	if len(report.Errors) > 0 {
		fmt.Println("=== Partial Provider Errors / Warnings ===")
		for _, pe := range report.Errors {
			fmt.Printf("  - Provider:  %s\n", pe.Provider)
			fmt.Printf("    Reason:    %s\n", pe.Reason)
			fmt.Printf("    Retryable: %t\n", pe.Retryable)
			fmt.Printf("    Detail:    %s\n", pe.Detail)
			if pe.Source != "" {
				fmt.Printf("    Source:    %s\n", pe.Source)
			}
			fmt.Println()
		}
	}
}
```

### Collection Options

| Option | Description |
| :--- | :--- |
| `WithProviders(ids ...ProviderID)` | Specific providers to query (default: all registered providers). |
| `WithTimeout(d time.Duration)` | Overall execution timeout across all providers. |
| `WithSourceTimeout(d time.Duration)` | Per-source timeout for individual ladder rung attempts (default: 5s). |
| `WithSourcePolicy(p SourcePolicy)` | Restrict data sources: `SourcePolicyDefault`, `SourcePolicyLocal` (offline/no-network), `SourcePolicyRemote`, etc. |
| `WithStrict(strict bool)` | When true, drift warnings are treated as failures. |
| `WithAdapter(a Adapter)` | Register or override a custom provider adapter implementation. |

---

## CLI Usage

The `dipstick` CLI wraps the Go library and emits machine-readable JSON adhering to the stable `dipstick.v1` schema.

```bash
# Collect usage across all providers
dipstick --json

# Query specific providers
dipstick -p claude,codex --json

# Run in offline/local-only mode (skips remote API network calls)
dipstick --source local --json

# Set execution timeout
dipstick --timeout 10s --json

# Diagnose installed providers, authentication, and source ladders
dipstick doctor
```

### Exit Codes

* `0`: Success (at least one provider returned usage metrics).
* `1`: No data (all requested providers were unavailable, unauthenticated, or unsupported).
* `2`: Invocation error (invalid arguments or flags).

### `jq` Recipes

Dipstick emits structured JSON to stdout and diagnostic logs to stderr, making it ideal for shell automation and monitoring pipelines.

#### 1. Extract quota utilization per provider
```bash
dipstick --json | jq -r '.providers[] | "\(.provider): \(.windows[]?.label // "quota") is \(.windows[]?.used_percent // 0)% used"'
```

#### 2. Alert if any quota window exceeds 80%
```bash
dipstick --json | jq -e '.providers[].windows[]? | select(.used_percent > 80)'
```

#### 3. Calculate total tokens consumed across all providers
```bash
dipstick --json | jq '[.providers[].tokens.total_tokens // 0] | add'
```

#### 4. Filter missing or unauthenticated providers
```bash
dipstick --json | jq -r '.errors[] | select(.reason == "not_authenticated" or .reason == "not_installed") | "\(.provider): \(.reason) (\(.detail))"'
```

#### 5. List account identity per provider
```bash
dipstick --json | jq -r '.providers[] | "\(.provider): \(.identity.email // "unknown") [Plan: \(.identity.plan // "standard")]"'
```

---

## Installation

### Pre-built Binaries

Download pre-built binary archives for your platform (`darwin` / `linux` on `amd64` / `arm64`) from the [GitHub Releases](https://github.com/mattwalters/dipstick/releases) page. Extract the archive and place the `dipstick` binary in your `PATH`.

### Using `go install`

Install the latest release directly using `go install`:

```bash
go install github.com/mattwalters/dipstick/cmd/dipstick@latest
```

### From Source

Clone the repository and build or install using `make`:

```bash
git clone https://github.com/mattwalters/dipstick.git
cd dipstick
make install
```

### Supported Platforms

* **macOS** (Darwin arm64, x86_64)
* **Linux** (amd64, arm64)

*Note: Windows and Homebrew packaging are out of scope for v0.1.*

---

## Provider Support Matrix

The table below is auto-generated from in-tree compatibility declarations.

<!-- BEGIN SUPPORT MATRIX -->
| Vendor | Provider ID | Verified Versions | Supported Sources / Tiers | Status & Notes |
| :--- | :--- | :--- | :--- | :--- |
| **Claude Code** (Anthropic) | `claude` | `v0.2.x` – `v0.3.x` | Tier 1 (`oauth_api`), Tier 2 (`local_state`), Tier 4 (`transcripts`) | Supported |
| **OpenAI Codex** | `codex` | `v0.1.x` – `v0.2.x` | Tier 1 (`oauth_api`), Tier 3 (`local_rpc`), Tier 4 (`transcripts`) | Supported |
| **OpenCode** (`anomalyco/opencode`) | `opencode` | `v1.18.x`+ | Tier 2 (`local_state`), Tier 3 (`local_rpc`), Tier 5 (`cli_stdout`) | Supported via local SQLite (`opencode.db`) |
| **Google Antigravity** | `antigravity` | None (`N/A`) | None (`ReasonNotSupported`) | Exposes no token usage API; cookie extraction prohibited |
<!-- END SUPPORT MATRIX -->

To update or verify the support matrix after modifying provider metadata:
```bash
make matrix
# or: go run ./cmd/genmatrix
```

---

## Threat Model & Security

Dipstick reads credentials and configuration off your local machine to meter coding agent usage. Because handling authentication secrets requires high scrutiny, this section provides an auditable accounting of what Dipstick accesses, transmits, avoids, and how to verify these guarantees against the source code.

### What Dipstick Reads

* **Local vendor configuration**: Standard configuration and state files located in user configuration directories (for example, `~/.codex/auth.json`, `~/.config/opencode/config.json`, and `~/.local/share/opencode/opencode.db`).
* **OS Keychain entries**: Generic password items stored in platform secret stores (such as macOS Keychain service `Claude Code-credentials` or Linux Secret Service via `internal/localstate/keychain*`).
* **Session transcripts**: Local interaction records and transcript files on disk (for example, OpenCode / Claude conversation logs).

### What Dipstick Sends and to Whom

* **Vendor endpoints only**: When running in network mode, outbound HTTP requests are sent exclusively to the official API endpoints of the vendor whose token is being checked (for example, querying Anthropic usage endpoints using the local Claude token).
* **Vendor tokens only**: Each vendor API request carries only that specific vendor's credentials in standard `Authorization` headers.
* **Zero telemetry**: Dipstick transmits nothing to third parties, intermediaries, or telemetry collectors. There is no analytics collection, pingback mechanism, or external crash reporting, and there never will be.

### What Dipstick Never Does

* **Never mutates vendor state**: Dipstick issues only read-only requests for usage and quota inspection; it never creates, updates, or deletes remote cloud resources or account settings.
* **Never refreshes or rotates tokens**: Dipstick treats existing credentials on disk or in keychains as read-only and will not trigger token refresh or key rotation flows.
* **Never extracts browser cookies**: Dipstick does not scan browser profiles, extract session cookies, or decrypt browser cookie storage (such as Electron SQLite stores).
* **Never spends model quota**: Dipstick calls usage/metering endpoints only; it never invokes LLM inference or prompt completion APIs that consume token quota.

### How to Verify

1. **Verify network isolation with local policies**:
   Run `dipstick --policy local` or `dipstick --source local` to restrict resolution exclusively to local disk and RPC sources (Tier 2 and above), disabling all outbound network calls.
2. **Audit external dependencies in `go.mod`**:
   Inspect [`go.mod`](go.mod) to verify that only minimal, vetted dependencies (such as `jsonschema` and `lipgloss`) are imported, with no external analytics or telemetry libraries.
3. **Audit outbound HTTP requests in `internal/adapters`**:
   Grep `http.NewRequest` and `http.Client` calls across [`internal/adapters`](internal/adapters) to confirm that requests are only directed to vendor-official endpoints.
4. **Audit subprocess and credential safety**:
   Inspect [`internal/cliexec`](internal/cliexec) to verify child environment variable allowlisting and execution bounds, and [`internal/scrub`](internal/scrub) to verify automated redaction of tokens and authorization headers from error output.

### Secret Scrubbing
All error strings and diagnostic outputs pass through Dipstick's automated scrubber (`internal/scrub`) prior to serialization, ensuring bearer tokens, API keys, and session cookies are never exposed in reports or logs. Callers can enforce 100% offline operation by passing `WithSourcePolicy(SourcePolicyLocal)` or CLI `--source local`.

---

## Contributing

Please see [CONTRIBUTING.md](CONTRIBUTING.md) for development workflows, testing guidelines (`go test -race ./...`), and instructions on capturing and redacting vendor test fixtures (`make capture`).

---

## License

Dipstick is open-source software licensed under the [MIT License](LICENSE).
