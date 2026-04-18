// Tests for: integration — test package lifecycle.
//
// TestMain clears any .zcp/state leftover from prior runs so that workflow
// engines constructed via server.New (which resolves state dir via os.Getwd)
// start with a clean registry. Without this, dead-PID sessions leaked by
// previous runs are auto-recovered by NewEngine and collide with tests that
// start a fresh workflow via zerops_workflow.

package integration_test

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	os.RemoveAll(".zcp")
	code := m.Run()
	os.RemoveAll(".zcp")
	os.Exit(code)
}
