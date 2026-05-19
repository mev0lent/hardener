package executor

type RunMode string

const (
	ModeAudit RunMode = "audit"
	ModeFix   RunMode = "fix"
)
