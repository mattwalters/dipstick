package antigravity

// Adapter provides usage collection for the Antigravity coding agent.
type Adapter struct{}

// New creates a new Antigravity adapter instance.
func New() *Adapter {
	return &Adapter{}
}

// Name returns the provider identifier.
func (a *Adapter) Name() string {
	return "antigravity"
}
