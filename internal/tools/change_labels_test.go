package tools

import (
	"slices"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/workflow"
)

// witness: internal/tools/hq_change_status.go | switch t.Checks { → switch t.ReviewBuildState {
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
			if got := changeLabels(&tt.told, time.Time{}); !slices.Equal(got, tt.labels) {
				t.Errorf("labels = %q, want %q", got, tt.labels)
			}
		})
	}
}

// witness: internal/tools/hq_change_status.go | case "passing": → case "pass":
func TestChangeLabels_CL2_ReviewBuildAndChecksHandoff(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		told   *workflow.HQChangeTold
		labels []string
	}{
		{name: "no standing"},
		{name: "no build", told: &workflow.HQChangeTold{}},
		{name: "unknown state", told: &workflow.HQChangeTold{ReviewBuildState: "other"}},
		{name: "awaiting", told: &workflow.HQChangeTold{ReviewBuildState: "awaiting"}, labels: []string{"Checking it builds"}},
		{name: "passing", told: &workflow.HQChangeTold{ReviewBuildState: "passing"}, labels: []string{"Build passed"}},
		{name: "failing with GitHub", told: &workflow.HQChangeTold{ReviewBuildState: "failing", Checks: "failing"}, labels: []string{"GitHub's checks failed", "Not added yet: did not build"}},
		{name: "unavailable with reason", told: &workflow.HQChangeTold{ReviewBuildState: "unavailable", ReviewBuildReason: "stage moved"}, labels: []string{"Could not check the build (stage moved)"}},
		{name: "unavailable without reason", told: &workflow.HQChangeTold{ReviewBuildState: "unavailable"}, labels: []string{"Could not check the build"}},
		{name: "given up", told: &workflow.HQChangeTold{ReviewBuildState: "failing", ChecksGivenUp: true}, labels: []string{"Not added yet: did not build", "over to you"}},
		{name: "GitHub given up without a build", told: &workflow.HQChangeTold{Checks: "failing", ChecksGivenUp: true}, labels: []string{"GitHub's checks failed", "over to you"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := changeLabels(tt.told, time.Time{}); !slices.Equal(got, tt.labels) {
				t.Errorf("labels = %q, want %q", got, tt.labels)
			}
		})
	}
}

// witness: internal/tools/hq_change_status.go | now.After(until) → now.Before(until)
func TestChangeLabels_CL3_DeadlineChangesAwaitingToUnavailable(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name, until, want string
	}{
		{"before", "2026-11-26T12:00:01Z", "Checking it builds"},
		{"at", "2026-11-26T12:00:00Z", "Checking it builds"},
		{"after", "2026-11-26T11:59:59Z", "Could not check the build (no result arrived)"},
		{"unreadable deadline", "broken", "Checking it builds"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			told := &workflow.HQChangeTold{ReviewBuildState: "awaiting", ReviewBuildUntil: tt.until}
			if got := changeLabels(told, at); !slices.Equal(got, []string{tt.want}) {
				t.Errorf("labels = %q, want %q", got, tt.want)
			}
		})
	}
}

// witness: internal/tools/hq_change.go | told.ReviewBuildState, told.ReviewBuildUntil, told.ReviewBuildReason = build.State, build.Until, build.Reason → told.ReviewBuildState, told.ReviewBuildUntil, told.ReviewBuildReason = "", build.Until, build.Reason
func TestChangeLabels_CL4_HQStateReachesEveryLabel(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	app, head, ready := "app-1", "head", true
	for _, tt := range []struct {
		name    string
		build   *hq.ReviewBuild
		givenUp bool
		labels  []string
	}{
		{"no build", nil, false, []string{"ready"}},
		{"awaiting", &hq.ReviewBuild{State: "awaiting", Until: "2026-11-26T12:01:00Z"}, false, []string{"ready", "Checking it builds"}},
		{"deadline passed", &hq.ReviewBuild{State: "awaiting", Until: "2026-11-26T11:59:00Z"}, false, []string{"ready", "Could not check the build (no result arrived)"}},
		{"unavailable", &hq.ReviewBuild{State: "unavailable", Reason: "stage moved"}, false, []string{"ready", "Could not check the build (stage moved)"}},
		{"failed and given up", &hq.ReviewBuild{State: "failing"}, true, []string{"ready", "Not added yet: did not build", "over to you"}},
		{"passed", &hq.ReviewBuild{State: "passing"}, false, []string{"ready", "Build passed"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := hq.MateChange{Repo: "app", Number: 7, State: hq.ChangeOpen, Head: &head, Ready: &ready, Mergeability: hq.MergeClean, ReviewBuild: tt.build, ChecksGivenUp: tt.givenUp}
			standing := standingOf(s)
			change := workflow.HQChange{AppID: app, Repo: "app", Number: 7, Told: &standing}
			state := &hq.MateState{AppID: &app, Changes: []hq.MateChange{s}}
			current, _ := changeLineNow(change, state, at)
			lastTold, _ := changeLineNow(change, nil, at)
			described := describedLabels(hq.Change{State: hq.ChangeOpen, Ready: &ready, Mergeability: hq.MergeClean, ReviewBuild: tt.build, ChecksGivenUp: tt.givenUp}, at)
			for name, got := range map[string][]string{"current": current.Labels, "last told": lastTold.Labels, "description": described} {
				if !slices.Equal(got, tt.labels) {
					t.Errorf("%s labels = %q, want %q", name, got, tt.labels)
				}
			}
		})
	}
}
