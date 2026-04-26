// Tests for: errwire.go — repo-level grep contracts pinning the
// elimination of legacy error wire shapes.
//
// Adversarial review (Codex pass-2) caught that the original plan only
// converted deploy_ssh's preflight site, leaving deploy_local + deploy_batch
// with their own non-canonical shapes. These contract tests scan the
// internal/tools/ package for any reintroduction of those patterns,
// catching future drift at build-time rather than at runtime.

package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoLegacyPreflightShapes pins the elimination of two preflight
// wire shapes (G14 plan §3.4):
//   - jsonResult(pfResult) — old deploy_ssh + deploy_local return shape
//   - "preFlightFailedFor" literal — old deploy_batch ad-hoc map
//
// Both are now routed through ConvertError + WithChecks("preflight", ...).
// Reintroducing either is a regression of the unification work and
// must be caught here, not via integration test churn.
func TestNoLegacyPreflightShapes(t *testing.T) {
	t.Parallel()
	forbidden := map[string]string{
		"jsonResult(pfResult)": "preflight failures must use ConvertError + WithChecks, not raw jsonResult — G14 unification",
		"preFlightFailedFor":   "deploy_batch's ad-hoc shape was eliminated; use ConvertError(ErrPreflightFailed, WithChecks(...)) — G14 §3.4",
	}

	dir := "."
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read tools dir: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		// Skip test files — they may legitimately reference the
		// eliminated shapes (e.g. errwire_contract_test.go itself
		// names them in this map).
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		body, readErr := os.ReadFile(filepath.Join(dir, name))
		if readErr != nil {
			t.Fatalf("read %s: %v", name, readErr)
		}
		text := string(body)
		for needle, reason := range forbidden {
			if strings.Contains(text, needle) {
				t.Errorf("%s contains forbidden pattern %q: %s", name, needle, reason)
			}
		}
	}
}
