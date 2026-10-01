package rollback

import (
	"testing"

	"hardener/internal/config"
)

// recordFix runs change between Begin and Commit, as RunFix does with a fix.
func recordFix(t *testing.T, ctx *config.ExecContext, check config.Check, change func()) {
	t.Helper()
	txn, err := Begin(ctx, check)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	change()
	if err := txn.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
}
