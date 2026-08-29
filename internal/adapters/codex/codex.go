package codex

// Adapter provides usage collection for the Codex coding agent.
type Adapter struct{}

// New creates a new Codex adapter instance.
func New() *Adapter {
	return &Adapter{}
}

// Name returns the provider identifier.
func (a *Adapter) Name() string {
	return "codex"
}
