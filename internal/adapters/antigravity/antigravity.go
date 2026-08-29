package antigravity

// Adapter provides usage collection for the Antigravity coding agent.
//
// DIP-12 Spike Findings & Decision:
// Antigravity is an Electron/VS Code-based application (Antigravity IDE)
// that does not expose a standalone CLI, local state store, or public API
// reporting token usage, rate windows, or quota resets:
//  1. Bundled CLI shims (such as `antigravity-ide`) are VS Code launcher
//     scripts without usage, metrics, or quota reporting subcommands.
//  2. Local state stores (SQLite `state.vscdb`, LevelDB) and session
//     transcripts (`brain/**/transcript.jsonl`) store UI settings, user profile,
//     and raw prompt/tool interactions without numeric token counts or rate limits.
//  3. Keychain entries (`Antigravity Safe Storage`) protect Electron cookies/state;
//     ambient OAuth tokens only grant generic Google cloud scopes and communicate
//     with private internal Google endpoints (`daily-cloudcode-pa.googleapis.com`)
//     rather than a public, authenticated usage API.
//  4. Extension host logs (`Antigravity.log`) record language server RPC traces
//     without per-session token accounting or quota limits.
//
// Per the DIP-12 hard constraint against cookie extraction from Electron profiles,
// Antigravity is not supported in v0.1. This adapter declares an empty source ladder,
// causing the resolver to report ReasonNotSupported ("not supported").
type Adapter struct{}

// New creates a new Antigravity adapter instance.
func New() *Adapter {
	return &Adapter{}
}

// Name returns the provider identifier.
func (a *Adapter) Name() string {
	return "antigravity"
}
