// Tests for: integration — test package lifecycle.
//
// TestMain clears any .zcp/state leftover from prior runs so that workflow
// engines constructed via server.New (which resolves state dir via os.Getwd)
// start with a clean registry. Without this, dead-PID sessions leaked by
// previous runs are auto-recovered by NewEngine and collide with tests that
// start a fresh workflow via zerops_workflow.

package integration_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/zeropsio/zcp/internal/ops/origintest"
)

// TestMain also trusts, once and before any test, the certificate of the
// HTTPS origins the pool-keyed part's tests serve (origintest): zcp's origin
// scratch speaks https alone (docs/spec-workflows.md §10 P-LP-20).
func TestMain(m *testing.M) {
	os.RemoveAll(".zcp")
	dir, err := os.MkdirTemp("", "zcp-integration-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := origintest.TrustCA(dir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(".zcp")
	os.RemoveAll(dir)
	os.Exit(code)
}
