package conformance

import (
	"os"
	"runtime"
	"testing"
)

// requireSourceTree skips the calling test when the conformance package's own
// source directory is not on disk — i.e. the suite is running as a shipped
// compiled test binary (`make dc-live-remote` on the container) with no repo
// checkout. The source-dependent lints (the AST test-name scan, the
// version-matrix YAML shape check) are REPO-CONTEXT checks: in CI and dev
// trees the sources are always present, so this skip can never hide them
// there; in a compiled-binary run they are not that lane's claims — the
// engine×proof matrix is (docs/spec-dataconsole-testing.md §1 placement
// rule). Keyed on the SOURCE TREE, not the individual input file, so a
// genuinely missing input inside a present repo still fails loudly.
func requireSourceTree(t *testing.T) {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Skip("source context unavailable (no caller info) — repo-context lint")
	}
	if _, err := os.Stat(self); err != nil {
		t.Skipf("conformance source tree not present (%v) — repo-context lint, skipped in compiled-binary runs", err)
	}
}
