package tools

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestResolveBuildTarget pins the A-fix: the CI build target is a
// mismatch-FLAGGING decision derived from ONE owner. The dev-builds/stage-receives
// convention stays the silent default, but when the user EXPLICITLY names the
// dev/push-source half of a standard pair the response carries a confirmable
// decision instead of a settled redirect — and an explicit buildTarget override
// is honored as the user's choice. Common flows (stage-named, simple, no
// override) stay zero-friction.
func TestResolveBuildTarget(t *testing.T) {
	t.Parallel()
	pair := func() *workflow.ServiceMeta {
		return &workflow.ServiceMeta{Hostname: "app", StageHostname: "appstage", Mode: topology.ModeStandard}
	}

	tests := []struct {
		name          string
		meta          *workflow.ServiceMeta
		requested     string
		override      string
		wantHost      string
		wantDecision  bool
		wantRequested string // only checked when wantDecision
		wantResolved  string
	}{
		{
			name: "dev-named standard pair flags the stage redirect",
			meta: pair(), requested: "app", override: "",
			wantHost: "appstage", wantDecision: true, wantRequested: "app", wantResolved: "appstage",
		},
		{
			name: "stage-named standard pair — no mismatch (user named the target)",
			meta: pair(), requested: "appstage", override: "",
			wantHost: "appstage", wantDecision: false,
		},
		{
			name: "buildTarget override to dev is honored — build on the named service",
			meta: pair(), requested: "app", override: "app",
			wantHost: "app", wantDecision: false,
		},
		{
			name: "buildTarget override to stage is honored — no decision",
			meta: pair(), requested: "app", override: "appstage",
			wantHost: "appstage", wantDecision: false,
		},
		{
			name:      "simple single-runtime — no stage half, no decision",
			meta:      &workflow.ServiceMeta{Hostname: "api", Mode: topology.PlanModeSimple},
			requested: "api", override: "",
			wantHost: "api", wantDecision: false,
		},
		{
			name: "nil meta — no decision",
			meta: nil, requested: "app", override: "",
			wantHost: "", wantDecision: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			host, _, decision := resolveBuildTarget(tt.meta, tt.requested, tt.override)
			if host != tt.wantHost {
				t.Errorf("host = %q, want %q", host, tt.wantHost)
			}
			if (decision != nil) != tt.wantDecision {
				t.Errorf("decision present = %v, want %v (decision=%v)", decision != nil, tt.wantDecision, decision)
			}
			if tt.wantDecision && decision != nil {
				if decision["requested"] != tt.wantRequested {
					t.Errorf("decision.requested = %v, want %q", decision["requested"], tt.wantRequested)
				}
				if decision["resolved"] != tt.wantResolved {
					t.Errorf("decision.resolved = %v, want %q", decision["resolved"], tt.wantResolved)
				}
				if c, _ := decision["confirm"].(string); !strings.Contains(c, "buildTarget=") {
					t.Errorf("decision.confirm must name the buildTarget override action; got: %v", decision["confirm"])
				}
			}
		})
	}
}
