// Tests for: workflow.go (workflowDescription) / hq_wiring.go
// (hqRepositoryActionsRefused) — zerops_workflow's description per runtime.
package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
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

// witness: internal/tools/deploy_local.go | if rt.MateEnabled { → if !rt.MateEnabled {
func TestSessionAnnotation_WDHandoff_OnlyStandaloneStageChange(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		mate   bool
		stage  bool
		wired  bool
		change string
		closed bool
		want   bool
	}{
		{name: "standalone stage and change", stage: true, wired: true, change: "appdev", closed: true, want: true},
		{name: "Mate stage and change", mate: true, stage: true, wired: true, change: "appdev", closed: true},
		{name: "stage not deployed", wired: true, change: "appdev", closed: true},
		{name: "no open change", stage: true, wired: true, closed: true},
		{name: "another repository's change", stage: true, wired: true, change: "other", closed: true},
		{name: "stage has no HQ repository", stage: true, change: "appdev", closed: true},
		{name: "session still open", stage: true, wired: true, change: "appdev"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			meta := &workflow.ServiceMeta{Hostname: "appdev", Stages: []topology.StageRef{{Hostname: "appstage"}}}
			if tc.wired {
				meta.HQ = &workflow.HQRepoRef{AppID: "app", Repo: "appdev"}
			}
			if err := workflow.WriteServiceMeta(dir, meta); err != nil {
				t.Fatal(err)
			}
			if tc.change != "" {
				ledger := filepath.Join(dir, "hq", "changes", "app", tc.change)
				if err := os.MkdirAll(ledger, 0o700); err != nil {
					t.Fatal(err)
				}
				body := `{"appId":"app","repo":"` + tc.change + `","number":7,"branch":"feature","recordedAt":"2026-01-01T00:00:00Z"}`
				if err := os.WriteFile(filepath.Join(ledger, "7.json"), []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			ws := &workflow.WorkSession{PID: os.Getpid(), Services: []string{"appdev", "appstage"},
				Deploys: map[string][]workflow.DeployAttempt{"appdev": {{SucceededAt: "2026-01-01T00:00:00Z"}}}}
			if tc.stage {
				ws.Deploys["appstage"] = []workflow.DeployAttempt{{SucceededAt: "2026-01-01T00:00:00Z"}}
			}
			if tc.closed {
				ws.ClosedAt, ws.CloseReason = "2026-01-01T00:00:00Z", workflow.CloseReasonAutoComplete
			}
			if err := workflow.SaveWorkSession(dir, ws); err != nil {
				t.Fatal(err)
			}
			rt := runtime.Info{MateEnabled: tc.mate, HQ: &runtime.HQLink{StatePath: filepath.Join(dir, "state.json")}}
			if tc.closed {
				if got := sessionHandsOverAChange(dir, ws, rt); got != tc.want {
					t.Errorf("stage change handoff = %v, want %v", got, tc.want)
				}
			}
			annotation := sessionAnnotationsForRuntime(dir, rt)
			if got := strings.Contains(annotation.Note, "Review"); got != tc.want {
				t.Errorf("closing annotation points to Review = %v, want %v", got, tc.want)
			}
		})
	}
}

// witness: internal/tools/deploy_local.go | rt.MateEnabled && state.Status == sessionStatusNone → !rt.MateEnabled && state.Status == sessionStatusNone
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
