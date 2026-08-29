package opencode

// Adapter provides usage collection for the OpenCode coding agent.
type Adapter struct{}

// New creates a new OpenCode adapter instance.
func New() *Adapter {
	return &Adapter{}
}

// Name returns the provider identifier.
func (a *Adapter) Name() string {
	return "opencode"
}
