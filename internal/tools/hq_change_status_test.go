package tools

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/hq"
)

func TestStatus_ReadyChangeNamesHQMergeability(t *testing.T) {
	// non-parallel: the subtests update one fake HQ's change before each status read.
	lab := describedChanges(t)
	for _, tt := range []struct {
		name, mergeability, want string
	}{
		{"a described change says checking until HQ judges whether it merges", "unknown", "ready · checking mergeability"},
		{"a described change from an older HQ does not imply it can merge", "", "ready · checking mergeability"},
		{"a described change with conflicts says it is not mergeable", hq.MergeConflict, "ready · not mergeable: conflicts with main"},
		{"a described change with no difference says it is not mergeable", "empty", "ready · not mergeable: no changes against main"},
		{"a described change already in main says it is not mergeable", "already_merged", "ready · not mergeable: already merged into main"},
		{"a described change with unrelated history says it is not mergeable", "unrelated", "ready · not mergeable: unrelated to main"},
		{"a described change HQ judged clean keeps its ready label", hq.MergeClean, "ready"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			lab.hq.mu.Lock()
			for i := range lab.hq.changes {
				if lab.hq.changes[i].Number == 2 {
					lab.hq.changes[i].Mergeability = tt.mergeability
				}
			}
			lab.hq.mu.Unlock()

			got := lab.status()
			want := "#2 add-signup (" + tt.want + ")"
			if !strings.Contains(got, want) {
				t.Errorf("ASSERTION: status must say %q; got:\n%s", want, got)
			}
		})
	}
}
