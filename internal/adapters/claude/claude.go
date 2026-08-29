package claude

// Adapter provides usage collection for the Claude coding agent.
type Adapter struct{}

// New creates a new Claude adapter instance.
func New() *Adapter {
	return &Adapter{}
}

// Name returns the provider identifier.
func (a *Adapter) Name() string {
	return "claude"
}
