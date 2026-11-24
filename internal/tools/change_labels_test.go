package tools

import (
	"slices"
	"testing"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/workflow"
)

// witness: internal/tools/hq_change_status.go | case "failing": → case "passing":
func TestChangeLabels_CL1_ChecksOfTheHeadAreLabelled(t *testing.T) {
	t.Parallel()
	ready := true
	tests := []struct {
		name   string
		told   workflow.HQChangeTold
		labels []string
	}{
		{name: "failing", told: workflow.HQChangeTold{Ready: &ready, Mergeability: hq.MergeClean, Checks: "failing"}, labels: []string{"ready", "GitHub's checks failed"}},
		{name: "pending", told: workflow.HQChangeTold{Ready: &ready, Mergeability: hq.MergeClean, Checks: "pending"}, labels: []string{"ready", "GitHub's checks running"}},
		{name: "passing", told: workflow.HQChangeTold{Ready: &ready, Mergeability: hq.MergeClean, Checks: "passing"}, labels: []string{"ready"}},
		{name: "unknown", told: workflow.HQChangeTold{Ready: &ready, Mergeability: hq.MergeClean}, labels: []string{"ready"}},
		{name: "failing with conflicts", told: workflow.HQChangeTold{Ready: &ready, Mergeability: hq.MergeConflict, Checks: "failing"}, labels: []string{"ready", "not mergeable: conflicts with main", "GitHub's checks failed"}},
		{name: "failing behind main", told: workflow.HQChangeTold{Ready: &ready, Mergeability: hq.MergeClean, Behind: true, Checks: "failing"}, labels: []string{"ready", "behind main", "GitHub's checks failed"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := changeLabels(&tt.told); !slices.Equal(got, tt.labels) {
				t.Errorf("labels = %q, want %q", got, tt.labels)
			}
		})
	}
}
