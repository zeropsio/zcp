package ops

import (
	"fmt"
	"os"
	"testing"

	"github.com/zeropsio/zcp/internal/ops/origintest"
)

// TestMain trusts, once for the whole package and before any test, the
// certificate of the HTTPS origins the origin-scratch tests serve
// (origintest): zcp's origin scratch speaks https alone
// (docs/spec-workflows.md §10 P-LP-19). Never changed after, so it is no
// state the tests share.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "zcp-ops-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := origintest.TrustCA(dir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		_ = os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
