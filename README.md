# dipstick
Coding Agent Usage and Metering

## Supported Platforms

`dipstick` supports macOS and Linux. Windows is out of scope for v0.1.

## Threat Model

`dipstick` reads credentials and configuration off your local machine to meter coding agent usage. Because handling authentication secrets requires high scrutiny, this section provides an auditable accounting of what `dipstick` accesses, transmits, avoids, and how to verify these guarantees against the source code.

### What `dipstick` reads

* **Local vendor configuration**: Standard configuration and state files located in user configuration directories (for example, `~/.codex/auth.json` and `~/.config/opencode/config.json`).
* **OS Keychain entries**: Generic password items stored in platform secret stores (such as macOS Keychain service `Claude Code-credentials` or Linux Secret Service via `internal/localstate/keychain*`).
* **Session transcripts**: Local interaction records and transcript files on disk (for example, OpenCode / Claude conversation logs).

### What `dipstick` sends and to whom

* **Vendor endpoints only**: When running in network mode, outbound HTTP requests are sent exclusively to the official API endpoints of the vendor whose token is being checked (for example, querying Anthropic usage endpoints using the local Claude token).
* **Vendor tokens only**: Each vendor API request carries only that specific vendor's credentials in standard `Authorization` headers.
* **Zero telemetry**: `dipstick` transmits nothing to third parties, intermediaries, or telemetry collectors. There is no analytics collection, pingback mechanism, or external crash reporting, and there never will be.

### What `dipstick` never does

* **Never mutates vendor state**: `dipstick` issues only read-only requests for usage and quota inspection; it never creates, updates, or deletes remote cloud resources or account settings.
* **Never refreshes or rotates tokens**: `dipstick` treats existing credentials on disk or in keychains as read-only and will not trigger token refresh or key rotation flows.
* **Never extracts browser cookies**: `dipstick` does not scan browser profiles, extract session cookies, or decrypt browser cookie storage (such as Electron SQLite stores).
* **Never spends model quota**: `dipstick` calls usage/metering endpoints only; it never invokes LLM inference or prompt completion APIs that consume token quota.

### How to verify

1. **Verify network isolation with local policies**:
   Run `dipstick --policy local` or `dipstick --policy offline` to restrict resolution exclusively to local disk and RPC sources (Tier 2 and above), disabling all outbound network calls.
2. **Audit external dependencies in `go.mod`**:
   Inspect [`go.mod`](go.mod) to verify that only minimal, vetted dependencies (such as `jsonschema`) are imported, with no external analytics or telemetry libraries.
3. **Audit outbound HTTP requests in `internal/adapters`**:
   Grep `http.NewRequest` and `http.Client` calls across [`internal/adapters`](internal/adapters) to confirm that requests are only directed to vendor-official endpoints.
4. **Audit subprocess and credential safety**:
   Inspect [`internal/cliexec`](internal/cliexec) to verify child environment variable allowlisting and execution bounds, and [`internal/scrub`](internal/scrub) to verify automated redaction of tokens and authorization headers from error output.
