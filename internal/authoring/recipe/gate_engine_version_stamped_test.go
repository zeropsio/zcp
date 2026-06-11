package recipe

import "testing"

func TestGateEngineVersionStamped_Table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		plan          *Plan
		engineVersion string
		wantCode      string
	}{
		{
			name:          "empty_plan_engine_version_blocks",
			plan:          &Plan{},
			engineVersion: "v9.80.0",
			wantCode:      "engine-version-not-stamped",
		},
		{
			name:          "matching_version_passes",
			plan:          &Plan{EngineVersion: "v9.80.0"},
			engineVersion: "v9.80.0",
		},
		{
			name:          "mismatched_version_blocks",
			plan:          &Plan{EngineVersion: "v9.78.0"},
			engineVersion: "v9.80.0",
			wantCode:      "engine-version-mismatch",
		},
		{
			name:          "dev_plan_with_dev_binary_passes",
			plan:          &Plan{EngineVersion: "dev"},
			engineVersion: "dev",
		},
		{
			name:          "prod_plan_with_dev_binary_passes",
			plan:          &Plan{EngineVersion: "v9.78.0"},
			engineVersion: "dev",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			vs := gateEngineVersionStamped(GateContext{Plan: tc.plan, EngineVersion: tc.engineVersion})
			if tc.wantCode == "" {
				if len(vs) > 0 {
					t.Fatalf("expected no violations, got %+v", vs)
				}
				return
			}
			if len(vs) != 1 || vs[0].Code != tc.wantCode {
				t.Fatalf("violations = %+v, want one %s", vs, tc.wantCode)
			}
		})
	}
}

func TestDefaultGates_EngineVersionStampedRegistered(t *testing.T) {
	t.Parallel()
	if !gateRegistered(DefaultGates(), "engine-version-stamped") {
		t.Fatalf("DefaultGates missing engine-version-stamped")
	}
}

func TestNewSession_StampsEngineVersion(t *testing.T) {
	t.Parallel()
	sess := NewSession("synth-showcase", "v9.80.0", nil, t.TempDir(), nil)
	if sess.EngineVersion != "v9.80.0" {
		t.Fatalf("Session.EngineVersion=%q, want v9.80.0", sess.EngineVersion)
	}
	if sess.Plan == nil || sess.Plan.EngineVersion != "v9.80.0" {
		t.Fatalf("Plan.EngineVersion=%q, want v9.80.0", sess.Plan.EngineVersion)
	}
}
