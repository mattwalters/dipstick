# Security Policy

`dipstick` reads local credentials, configuration files, and session state to meter coding agent usage. Because `dipstick` handles sensitive credentials and may be embedded as a library in downstream developer tools, security and supply-chain hygiene are top priorities.

## Supported Versions

Only the latest minor release version of `dipstick` is actively supported with security fixes and vulnerability updates.

| Version | Supported          |
| ------- | ------------------ |
| 0.1.x   | :white_check_mark: |
| < 0.1   | :x:                |

## Reporting a Vulnerability

We appreciate coordinated vulnerability disclosure to protect users and downstream integrators.

### Preferred Reporting Method

Please report potential security vulnerabilities privately using **GitHub Private Vulnerability Reporting**:

* Navigate to the repository's [Security Advisories](https://github.com/mattwalters/dipstick/security/advisories/new) page to create a private report.

### Alternative Contact

If you are unable to use GitHub Private Vulnerability Reporting, you may submit vulnerability reports via email to:

* `matthewrobertwalters@gmail.com`

Please include in your report:
1. A description of the vulnerability, including severity assessment and attack prerequisites.
2. Step-by-step reproduction instructions or a minimal proof-of-concept.
3. Impact on confidentiality, integrity, or credential exposure.
4. Any proposed patches or mitigations.

### Response Timeframe and Remediation Lifecycle

* **Initial Response**: We aim to acknowledge receipt of all vulnerability reports within **48 hours**.
* **Triage & Assessment**: Within 5 business days, we will validate the vulnerability and determine severity/scope.
* **Remediation & Patching**: We will develop and test a fix in a private fork or advisory workspace.
* **Release & Advisory**: We will publish a patched release and public security advisory with proper credit to the reporter.

## Supply Chain and Dependency Budget

`dipstick` adheres to strict dependency minimization principles:

* **Minimal Transitive Tree**: Direct third-party dependencies must be strictly justified and audited before introduction. Consumers embedding `dipstick` inherit its dependency tree, so we avoid large frameworks, telemetry SDKs, or unvetted libraries.
* **Automated Scans**: CI runs automated dependency checks (`govulncheck`) and CodeQL static analysis on all pull requests, pushes, and schedules. Dependabot keeps dependencies up to date with grouped minor/patch updates.
* **Secret Scanning**: Secret scanning and push protection are enabled on the repository to prevent accidental credential leakage in test fixtures or commits.
* **Hermetic Execution**: Subprocess execution (`internal/cliexec`) scrubs child environment variables, enforces timeouts, caps stream buffers, and disallows execution of relative binaries.
* **Token Scrubbing**: Error messages, logs, and output streams are filtered through `internal/scrub` to redact OAuth tokens, API keys, and authorization headers before surfacing to callers.
