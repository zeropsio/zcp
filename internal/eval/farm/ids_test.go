package farm

import (
	"strings"
	"testing"
)

// TestIDs_Grammar pins the FM-47 id grammar (docs/spec-eval-farm.md §7.6):
// a batch id is `^[a-z0-9][a-z0-9-]{0,62}$`, a run id is `<batch>-<scenarioId>`
// matching `^[a-z0-9][a-z0-9-]{0,127}$`. Independent oracle: every literal
// below is copied from the brief/spec text or hand-built to a known length,
// never derived from ValidBatchID/ValidRunID's own regexp.
func TestIDs_Grammar(t *testing.T) {
	t.Parallel()

	sixtyFour := strings.Repeat("a", 64) // exceeds the batch cap (63), fits the run cap (128)

	tests := []struct {
		name      string
		id        string
		wantBatch bool
		wantRun   bool
	}{
		{"short alnum", "final1", true, true},
		{"batch with hyphen and digits", "batch-1757590000", true, true},
		{"long hyphenated scenario-shaped id", "final1-recover-failed-buildfromgit-missing-dep", true, true},
		{"empty", "", false, false},
		{"leading hyphen", "-x", false, false},
		{"slash", "x/y", false, false},
		{"dot-dot", "..", false, false},
		{"uppercase", "X", false, false},
		{"64 chars exceeds batch cap but fits run cap", sixtyFour, false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ValidBatchID(tc.id); got != tc.wantBatch {
				t.Errorf("ValidBatchID(%q) = %v, want %v", tc.id, got, tc.wantBatch)
			}
			if got := ValidRunID(tc.id); got != tc.wantRun {
				t.Errorf("ValidRunID(%q) = %v, want %v", tc.id, got, tc.wantRun)
			}
		})
	}
}
