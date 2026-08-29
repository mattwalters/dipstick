# dipstick
Coding Agent Usage and Metering

## Supported Platforms

`dipstick` supports macOS and Linux. Windows is out of scope for v0.1.

## Provider Support (v0.1)

| Provider | Status | Primary Source | Notes |
|---|---|---|---|
| **Claude Code** | Supported | OAuth Usage API (Tier 1) | Local session transcript token accounting fallback |
| **Codex** | Supported | Local `auth.json` (Tier 2) | Identity and plan claims |
| **OpenCode** | Supported | Local SQLite `opencode.db` (Tier 2) | BYO-provider aggregate token accounting (`derived` confidence); local RPC (Tier 3) and CLI (Tier 5) fallback |
| **Antigravity** | Not Supported | `ReasonNotSupported` | No non-cookie read surface; browser cookie extraction is disallowed by design |
