package workflow

import (
	"strings"
	"testing"
)

func TestBuildPrepareGuide_Personalized(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		state        *DeployState
		env          Environment
		wantContains []string
		wantAbsent   []string
	}{
		{
			name: "standard",
			state: &DeployState{
				Mode: PlanModeStandard,
				Targets: []DeployTarget{
					{Hostname: "appdev", Role: DeployRoleDev},
					{Hostname: "appstage", Role: DeployRoleStage},
				},
			},
			env: EnvContainer,
			wantContains: []string{
				"appdev",
				"appstage",
				"standard",
				"deployFiles",
				"${",
				"zerops_knowledge",
			},
		},
		{
			name: "dev",
			state: &DeployState{
				Mode: PlanModeDev,
				Targets: []DeployTarget{
					{Hostname: "apidev", Role: DeployRoleDev},
				},
			},
			env: EnvContainer,
			wantContains: []string{
				"apidev",
				"dev",
				"zerops_knowledge",
			},
		},
		{
			name: "simple_local",
			state: &DeployState{
				Mode: PlanModeSimple,
				Targets: []DeployTarget{
					{Hostname: "web", Role: DeployRoleSimple},
				},
			},
			env: EnvLocal,
			wantContains: []string{
				"web",
				"simple",
				"healthCheck",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			guide := buildPrepareGuide(tt.state, tt.env, "")

			if guide == "" {
				t.Fatal("expected non-empty guidance")
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(guide, want) {
					t.Errorf("guide should contain %q\ngot:\n%s", want, guide)
				}
			}
			for _, absent := range tt.wantAbsent {
				if strings.Contains(guide, absent) {
					t.Errorf("guide should NOT contain %q", absent)
				}
			}
			// Max 55 lines.
			lines := strings.Count(guide, "\n") + 1
			if lines > 55 {
				t.Errorf("guide has %d lines, max 55", lines)
			}
		})
	}
}

func TestBuildDeployGuide_Personalized(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		state        *DeployState
		iteration    int
		env          Environment
		wantContains []string
	}{
		{
			name: "standard_push_dev_iter0",
			state: &DeployState{
				Mode: PlanModeStandard,
				Targets: []DeployTarget{
					{Hostname: "appdev", Role: DeployRoleDev},
					{Hostname: "appstage", Role: DeployRoleStage},
				},
			},
			iteration: 0,
			env:       EnvContainer,
			wantContains: []string{
				"appdev",           // personalized hostname
				"appstage",         // stage hostname
				"zerops_deploy",    // deploy command
				"zerops_subdomain", // subdomain command
				"zerops_verify",    // verify command
				"DEPLOYED",         // platform fact
				"deployFiles",      // platform fact
			},
		},
		{
			name: "dev_cicd_iter0",
			state: &DeployState{
				Mode: PlanModeDev,
				Targets: []DeployTarget{
					{Hostname: "apidev", Role: DeployRoleDev},
				},
			},
			iteration: 0,
			env:       EnvContainer,
			wantContains: []string{
				"apidev",
				"zerops_deploy",
			},
		},
		{
			name: "simple_manual_iter0",
			state: &DeployState{
				Mode: PlanModeSimple,
				Targets: []DeployTarget{
					{Hostname: "web", Role: DeployRoleSimple},
				},
			},
			iteration: 0,
			env:       EnvLocal,
			wantContains: []string{
				"web",
				"auto-starts",
			},
		},
		{
			name: "iteration_1_escalation",
			state: &DeployState{
				Mode: PlanModeStandard,
				Targets: []DeployTarget{
					{Hostname: "appdev", Role: DeployRoleDev},
				},
			},
			iteration: 1,
			env:       EnvContainer,
			wantContains: []string{
				"Iteration 1",
				"zerops_logs",
			},
		},
		{
			name: "iteration_3_user_escalation",
			state: &DeployState{
				Mode: PlanModeDev,
				Targets: []DeployTarget{
					{Hostname: "appdev", Role: DeployRoleDev},
				},
			},
			iteration: 3,
			env:       EnvContainer,
			wantContains: []string{
				"Iteration 3",
				"user",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			guide := buildDeployGuide(tt.state, tt.iteration, EnvContainer, "")

			if guide == "" {
				t.Fatal("expected non-empty guidance")
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(guide, want) {
					t.Errorf("guide should contain %q\ngot:\n%s", want, guide)
				}
			}
			// Max 55 lines.
			lines := strings.Count(guide, "\n") + 1
			if lines > 55 {
				t.Errorf("guide has %d lines, max 55", lines)
			}
		})
	}
}

func TestBuildVerifyGuide_NonEmpty(t *testing.T) {
	t.Parallel()
	guide := buildVerifyGuide()
	if guide == "" {
		t.Fatal("expected non-empty verify guidance")
	}
	if !strings.Contains(guide, "zerops_verify") {
		t.Error("verify guide should mention zerops_verify")
	}
}

func TestBuildKnowledgeMap_Personalized(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		targets      []DeployTarget
		wantContains []string
		wantAbsent   []string
	}{
		{
			name: "with_runtime_type",
			targets: []DeployTarget{
				{Hostname: "appdev", RuntimeType: "nodejs@22", Role: DeployRoleDev},
				{Hostname: "appstage", RuntimeType: "nodejs@22", Role: DeployRoleStage},
			},
			wantContains: []string{
				"appdev (nodejs@22)",
				`query="nodejs"`,
				"zerops_discover",
			},
			wantAbsent: []string{
				"appstage",       // stage targets skipped
				"<your runtime>", // generic fallback not used
			},
		},
		{
			name:    "no_runtime_type_fallback",
			targets: []DeployTarget{{Hostname: "app", Role: DeployRoleSimple}},
			wantContains: []string{
				"<your runtime>", // generic fallback
				"zerops_discover",
			},
		},
		{
			name:    "empty_targets",
			targets: nil,
			wantContains: []string{
				"<your runtime>",
				"zerops_discover",
			},
		},
		{
			name: "multiple_runtimes_deduped",
			targets: []DeployTarget{
				{Hostname: "webdev", RuntimeType: "nodejs@22", Role: DeployRoleDev},
				{Hostname: "apidev", RuntimeType: "go@1", Role: DeployRoleDev},
			},
			wantContains: []string{
				"webdev (nodejs@22)",
				"apidev (go@1)",
				`query="nodejs"`,
				`query="go"`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := buildKnowledgeMap(tt.targets)
			if result == "" {
				t.Fatal("expected non-empty knowledge map")
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(result, want) {
					t.Errorf("should contain %q, got:\n%s", want, result)
				}
			}
			for _, absent := range tt.wantAbsent {
				if strings.Contains(result, absent) {
					t.Errorf("should NOT contain %q, got:\n%s", absent, result)
				}
			}
		})
	}
}
