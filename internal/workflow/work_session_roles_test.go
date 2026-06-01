package workflow

import (
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

// TestRequiredServices_FiltersNonRequiredRoles pins RC-B: the auto-close
// denominator counts only services whose role is required (the default when
// absent). deferred / out-of-scope services stay in ws.Services (visible,
// trackable) but do NOT gate completion.
func TestRequiredServices_FiltersNonRequiredRoles(t *testing.T) {
	cases := []struct {
		name string
		ws   *WorkSession
		want []string
	}{
		{
			name: "no roles → all required (back-compat)",
			ws:   &WorkSession{Services: []string{"appdev", "appstage"}},
			want: []string{"appdev", "appstage"},
		},
		{
			name: "stage out-of-scope → only dev required",
			ws: &WorkSession{
				Services: []string{"appdev", "appstage"},
				Roles:    map[string]string{"appstage": RoleOutOfScope},
			},
			want: []string{"appdev"},
		},
		{
			name: "deferred also excluded",
			ws: &WorkSession{
				Services: []string{"appdev", "appstage"},
				Roles:    map[string]string{"appstage": RoleDeferred},
			},
			want: []string{"appdev"},
		},
		{
			name: "explicit required is required",
			ws: &WorkSession{
				Services: []string{"appdev"},
				Roles:    map[string]string{"appdev": RoleRequired},
			},
			want: []string{"appdev"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RequiredServices(tc.ws)
			if len(got) != len(tc.want) {
				t.Fatalf("RequiredServices = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("RequiredServices = %v, want %v", got, tc.want)
				}
			}
		})
	}
}

// TestEvaluateAutoClose_OutOfScopeStage pins RC-B end-to-end: a standard pair
// with the stage half marked out-of-scope auto-closes once the dev half alone
// is deployed+verified — "iterate dev only, leave staging" becomes expressible
// without dropping to an explicit close.
func TestEvaluateAutoClose_OutOfScopeStage(t *testing.T) {
	dir := t.TempDir()
	meta := &ServiceMeta{
		Hostname:                 "appdev",
		Mode:                     topology.PlanModeStandard,
		StageHostname:            "appstage",
		CloseDeployMode:          topology.CloseModeAuto,
		CloseDeployModeConfirmed: true,
		BootstrapSession:         "test",
		BootstrappedAt:           "2026-06-01",
		FirstDeployedAt:          "2026-06-01",
	}
	if err := WriteServiceMeta(dir, meta); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	ws := &WorkSession{
		Services: []string{"appdev", "appstage"},
		Roles:    map[string]string{"appstage": RoleOutOfScope},
		Deploys: map[string][]DeployAttempt{
			"appdev": {{AttemptedAt: "t", SucceededAt: "t"}},
		},
		Verifies: map[string][]VerifyAttempt{
			"appdev": {{AttemptedAt: "t", PassedAt: "t", Passed: true}},
		},
	}
	if !EvaluateAutoClose(dir, ws) {
		t.Fatalf("EvaluateAutoClose = false, want true (appstage out-of-scope; appdev green)")
	}

	// Control: without the out-of-scope role, the same state must NOT close
	// (stage pending) — proves the role is what flips the gate.
	ws.Roles = nil
	if EvaluateAutoClose(dir, ws) {
		t.Fatalf("EvaluateAutoClose = true with no roles, want false (appstage pending)")
	}
}
