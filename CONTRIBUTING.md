# Contributing to Dipstick

Thank you for contributing to Dipstick! This guide explains how to develop, test, and contribute new provider adapters and bug fixes to the codebase.

---

## Core Tenets & Architectural Principles

Before contributing code or adding new provider adapters, keep our core design principles in mind:

1. **Library-First**: Dipstick is primarily a Go library (`github.com/mattwalters/dipstick`). The CLI (`cmd/dipstick`) is a thin client wrapping `dipstick.Collect`. All business logic, ladder resolution, and error modeling belong in the core library.
2. **Read-Only & Safe**: Dipstick strictly observes usage and quota metrics. It **never writes** to vendor config files, **never triggers token refreshes**, **never decrypts browser/Electron cookie jars**, and **never consumes user LLM generation quota**.
3. **Tiered Source Ladders**: Each provider adapter defines a descending hierarchy of source strategies (Tier 1: API -> Tier 2: Local State -> Tier 3: Local RPC -> Tier 4: Transcripts -> Tier 5: CLI stdout). High-fidelity sources win; unsupported rungs degrade gracefully without fabricating metrics.
4. **Zero Telemetry & Strict Redaction**: Dipstick has no analytics. All logs and serialized outputs are scrubbed of secrets before emission.

---

## Development Workflow

### Prerequisites

* Go 1.23+ (matching `go.mod`)
* `golangci-lint` (v1.64+)
* `git` and `make`

### Building and Testing

```bash
# Build all packages and binaries
make build
# or: go build ./...

# Run the complete test suite with Go's race detector enabled
make test
# or: go test -race ./...

# Run linters and formatting checks
make lint
# or: golangci-lint run

# Format code and imports
make fmt
# or: gofmt -w -s . && goimports -w -local github.com/mattwalters/dipstick .

# Check or synchronize the README support matrix
make matrix
# or: go run ./cmd/genmatrix
```

---

## The Fixture Capture & Redaction Workflow

The non-obvious part of contributing to Dipstick is maintaining **reproducible test fixtures**. Because CI cannot maintain live paid subscriptions for every AI coding agent, provider adapters rely on **golden test fixtures** (`testdata/`) representing authentic local state, SQLite databases, config files, and API responses.

When adding support for a new vendor version or debugging schema drift:

### 1. Capturing Fixtures

Run the fixture capture target or capture raw diagnostic state from your local environment:
```bash
make capture
# or run dipstick diagnostic capture against local vendor installations
```

### 2. Redacting Captured Data (CRITICAL)

> **WARNING**: Never commit real API keys, bearer tokens, passwords, cookies, or personally identifiable information (PII) to version control.

Before committing any captured fixture file to `testdata/`:
1. **Scrub Credentials**: Replace tokens and API keys with dummy tokens matching the vendor format (e.g. `sk-ant-api03-REDACTED-SAMPLE-KEY-0000000000000000`).
2. **Anonymize PII**: Replace real user emails with `user@example.com` or `dev@domain.org`, and remove personal organization IDs or private project paths.
3. **Verify Scrubbing**: Ensure all text passes through `internal/scrub` without leaking unmasked tokens.
4. **Test Fixtures**: Ensure golden tests in `internal/adapters/<provider>/` pass cleanly against the newly committed fixtures.

---

## Adding or Updating a Provider Adapter

1. **Implement Adapter Interface**: In `internal/adapters/<provider>/`, implement `dipstick.Adapter` declaring the provider's `ID()`, `Detect()`, and `Sources()`.
2. **Declare Source Ladder**: Implement `dipstick.Source` for each supported tier (e.g. `local_state`, `oauth_api`, `local_rpc`).
3. **Register Provider**: Add the provider constant to `ProviderID` in `types.go`, register it in `dipstick.go`, and update the JSON schema in `schema/dipstick.v1.json`.
4. **Update Compatibility Declarations**: Add the verified version range and status notes to `cmd/genmatrix/main.go`, then run `make matrix` to update `README.md`.
5. **Add Tests**: Include unit tests, golden fixture parsing tests, and error-handling tests (`ReasonNotInstalled`, `ReasonNotAuthenticated`, `ReasonParseFailed`, etc.).

---

## Pull Request Guidelines

* Follow conventional commit messages: `feat:`, `fix:`, `docs:`, `chore:`, `refactor:`.
* Keep pull requests focused on a single change or provider adapter.
* Ensure all tests pass (`go test -race ./...`), the linter is clean (`golangci-lint run`), and `README.md` is synchronized with `make matrix`.
