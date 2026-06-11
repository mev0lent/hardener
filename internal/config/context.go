package config

type ExecContext struct {
	RunID         string
	BaseDir       string
	SecurityLevel string
	OSName        string
	ArchName      string
	DistroName    string
	Profile       string
	Labels        []string
	Timestamp     string
	Extra         map[string]interface{}
}

// Create a new context
func NewExecContext(runID, baseDir string) *ExecContext {
	return &ExecContext{
		RunID:   runID,
		BaseDir: baseDir,
		Extra:   make(map[string]interface{}),
	}
}

// Add or update a value
func (c *ExecContext) Set(key string, value interface{}) {
	c.Extra[key] = value
}

// Get a value (type-assert when reading)
func (c *ExecContext) Get(key string) (interface{}, bool) {
	v, ok := c.Extra[key]
	return v, ok
}
