// Tests for: workflow.go (workflowDescription) / hq_wiring.go
// (hqRepositoryActionsRefused) — zerops_workflow's description per runtime.
package tools

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/runtime"
)

// WD-1 — zerops_workflow's description offers git-push-setup and
// build-integration for CI/CD exactly where the handler accepts them, and
// where a Mate refuses them it says CI/CD is HQ's and that neither runs there.
// witness: internal/tools/hq_wiring.go | return rt.MateEnabled → return false
func TestWorkflowDescription_WD1_CICDOfferedExactlyWhereAccepted(t *testing.T) {
	t.Parallel() // both functions read only their argument
	standaloneHolds := []string{`action="git-push-setup", then action="build-integration"`}
	standaloneLacks := []string{`In a Mate, CI/CD is HQ's`}
	tests := []struct {
		name    string
		rt      runtime.Info
		refused bool
		holds   []string
		lacks   []string
	}{
		{"local", runtime.Info{}, false, standaloneHolds, standaloneLacks},
		{"standalone zcp", runtime.Info{InContainer: true}, false, standaloneHolds, standaloneLacks},
		{"a Mate's container", runtime.Info{InContainer: true, MateEnabled: true}, true,
			[]string{`In a Mate, CI/CD is HQ's`, `zerops_mirror`},
			[]string{`git-push-setup|build-integration`, `action="git-push-setup" service=`, `then action="build-integration"`}},
		{"a Mate outside a container", runtime.Info{MateEnabled: true}, true,
			[]string{`In a Mate, CI/CD is HQ's`},
			[]string{`then action="build-integration"`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := hqRepositoryActionsRefused(tt.rt); got != tt.refused {
				t.Errorf("hqRepositoryActionsRefused(%+v) = %v, want %v", tt.rt, got, tt.refused)
			}
			desc := workflowDescription(tt.rt)
			for _, s := range tt.holds {
				if !strings.Contains(desc, s) {
					t.Errorf("description lacks %q", s)
				}
			}
			for _, s := range tt.lacks {
				if strings.Contains(desc, s) {
					t.Errorf("description holds %q", s)
				}
			}
		})
	}
}

// witness: internal/tools/deploy_local.go | rt.MateEnabled && state.Status == "none" → !rt.MateEnabled && state.Status == "none"
func TestSessionAnnotation_WDNoSession_StageRuleOnlyInMate(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		mate bool
	}{
		{name: "standalone"},
		{name: "Mate", mate: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := sessionAnnotationsForRuntime(t.TempDir(), runtime.Info{MateEnabled: tc.mate})
			if got.Status != "none" || strings.Contains(got.Note, "Building the stage because") != tc.mate {
				t.Fatalf("untracked deploy annotation = %+v, want Mate stage rule %v", got, tc.mate)
			}
		})
	}
}

// witness: internal/tools/deploy_git_push.go | deploy = mateStageRule → deploy = noActiveSessionNote
func TestPushRefusal_WDPairPush_StageRuleOnlyInMate(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		mate bool
	}{
		{name: "standalone"},
		{name: "Mate", mate: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := hqGitPushRefusal("appdev", tc.mate).Suggestion
			if strings.Contains(got, "Building the stage because") != tc.mate || (!tc.mate && !strings.Contains(got, `strategy "ssh"`)) {
				t.Fatalf("push suggestion = %q, want Mate stage rule %v", got, tc.mate)
			}
		})
	}
}

// witness: internal/tools/deploy_git_push.go | mateStageRule, alias) → "", alias)
func TestPushRefusal_WDLeasePush_StageRuleOnlyInMate(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		mate bool
	}{
		{name: "standalone"},
		{name: "Mate", mate: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := leaseGitPushRefusal(ops.LeaseTargetInfo{Alias: "pool-1"}, runtime.Info{MateEnabled: tc.mate}).Suggestion
			if strings.Contains(got, "Building the stage because") != tc.mate || (!tc.mate && !strings.Contains(got, "stage check")) {
				t.Fatalf("lease suggestion = %q, want Mate stage rule %v", got, tc.mate)
			}
		})
	}
}
