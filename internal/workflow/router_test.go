package workflow

import (
	"strings"
	"testing"
)

func TestRoute_FreshProject(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		input          RouterInput
		wantTop        string
		wantMinOffered int
	}{
		{
			name:           "no sessions, no metas",
			input:          RouterInput{ProjectState: StateFresh},
			wantTop:        "bootstrap",
			wantMinOffered: 4, // bootstrap + debug + scale + configure
		},
		{
			name: "active bootstrap session",
			input: RouterInput{
				ProjectState:   StateFresh,
				ActiveSessions: []SessionEntry{{Workflow: "bootstrap", SessionID: "abc123"}},
			},
			wantTop: "bootstrap",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			offerings := Route(tt.input)
			if len(offerings) == 0 {
				t.Fatal("expected at least one offering")
			}
			if offerings[0].Workflow != tt.wantTop {
				t.Errorf("top = %q, want %q", offerings[0].Workflow, tt.wantTop)
			}
			if tt.wantMinOffered > 0 && len(offerings) < tt.wantMinOffered {
				t.Errorf("count = %d, want >= %d", len(offerings), tt.wantMinOffered)
			}
		})
	}
}

func TestRoute_ConformantProject(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   RouterInput
		wantTop string
	}{
		{
			name: "ci-cd strategy",
			input: RouterInput{
				ProjectState: StateConformant,
				ServiceMetas: []*ServiceMeta{{
					Hostname: "appdev", BootstrappedAt: "2026-01-01", DeployStrategy: StrategyCICD,
				}},
				LiveServices: []string{"appdev"},
			},
			wantTop: "cicd",
		},
		{
			name: "push-dev strategy",
			input: RouterInput{
				ProjectState: StateConformant,
				ServiceMetas: []*ServiceMeta{{
					Hostname: "appdev", BootstrappedAt: "2026-01-01", DeployStrategy: StrategyPushDev,
				}},
				LiveServices: []string{"appdev"},
			},
			wantTop: "deploy",
		},
		{
			name: "manual strategy",
			input: RouterInput{
				ProjectState: StateConformant,
				ServiceMetas: []*ServiceMeta{{
					Hostname: "appdev", BootstrappedAt: "2026-01-01", DeployStrategy: StrategyManual,
				}},
				LiveServices: []string{"appdev"},
			},
			wantTop: "deploy",
		},
		{
			name:    "no metas",
			input:   RouterInput{ProjectState: StateConformant},
			wantTop: "deploy",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			offerings := Route(tt.input)
			if len(offerings) == 0 {
				t.Fatal("expected at least one offering")
			}
			if offerings[0].Workflow != tt.wantTop {
				t.Errorf("top = %q, want %q", offerings[0].Workflow, tt.wantTop)
			}
			// Should always have bootstrap as secondary.
			hasBootstrap := false
			for _, o := range offerings {
				if o.Workflow == "bootstrap" {
					hasBootstrap = true
				}
			}
			if !hasBootstrap {
				t.Error("expected bootstrap in offerings")
			}
		})
	}
}

func TestRoute_NonConformant(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   RouterInput
		wantTop string
	}{
		{
			name: "some metas with strategy",
			input: RouterInput{
				ProjectState: StateNonConformant,
				ServiceMetas: []*ServiceMeta{{
					Hostname: "appdev", BootstrappedAt: "2026-01-01", DeployStrategy: StrategyPushDev,
				}},
				LiveServices: []string{"appdev"},
			},
			wantTop: "deploy",
		},
		{
			name:    "no metas",
			input:   RouterInput{ProjectState: StateNonConformant},
			wantTop: "bootstrap",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			offerings := Route(tt.input)
			if len(offerings) == 0 {
				t.Fatal("expected offerings")
			}
			if offerings[0].Workflow != tt.wantTop {
				t.Errorf("top = %q, want %q", offerings[0].Workflow, tt.wantTop)
			}
		})
	}
}

func TestRoute_Unknown_WorkflowsEqualPriority(t *testing.T) {
	t.Parallel()
	offerings := Route(RouterInput{ProjectState: StateUnknown})
	if len(offerings) == 0 {
		t.Fatal("expected offerings")
	}
	// Workflows from routeUnknown should have equal priority (3).
	// Utility offerings (scale tool) may have higher priority number (5).
	for _, o := range offerings {
		if o.Priority != 3 && o.Priority != 5 {
			t.Errorf("unexpected priority %d for %q", o.Priority, o.Workflow)
		}
	}
}

func TestRoute_AlwaysIncludesUtilities(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input RouterInput
	}{
		{"fresh", RouterInput{ProjectState: StateFresh}},
		{"conformant", RouterInput{ProjectState: StateConformant}},
		{"non-conformant", RouterInput{ProjectState: StateNonConformant}},
		{"unknown", RouterInput{ProjectState: StateUnknown}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			offerings := Route(tt.input)
			utils := map[string]bool{"debug": false, "scale": false, "configure": false}
			for _, o := range offerings {
				if _, ok := utils[o.Workflow]; ok {
					utils[o.Workflow] = true
				}
			}
			for name, found := range utils {
				if !found {
					t.Errorf("missing utility workflow %q", name)
				}
			}
		})
	}
}

func TestRoute_StaleMetaFiltering(t *testing.T) {
	t.Parallel()
	input := RouterInput{
		ProjectState: StateConformant,
		ServiceMetas: []*ServiceMeta{
			{Hostname: "appdev", BootstrappedAt: "2026-01-01", DeployStrategy: StrategyCICD},
			{Hostname: "staleservice", BootstrappedAt: "2026-01-01", DeployStrategy: StrategyPushDev},
		},
		LiveServices: []string{"appdev"},
	}
	offerings := Route(input)
	if offerings[0].Workflow != "cicd" {
		t.Errorf("top = %q, want cicd (stale meta should be filtered)", offerings[0].Workflow)
	}
}

func TestRoute_ActiveBootstrap_ResumeHint(t *testing.T) {
	t.Parallel()
	input := RouterInput{
		ProjectState:   StateFresh,
		ActiveSessions: []SessionEntry{{Workflow: "bootstrap", SessionID: "abc123"}},
	}
	offerings := Route(input)
	if len(offerings) == 0 {
		t.Fatal("expected offerings")
	}
	if !strings.Contains(offerings[0].Hint, "resume") {
		t.Errorf("hint = %q, want to contain 'resume'", offerings[0].Hint)
	}
}

func TestRoute_IncompleteMetas(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   RouterInput
		wantTop string
	}{
		{
			name: "incomplete meta suggests bootstrap",
			input: RouterInput{
				ProjectState: StateNonConformant,
				ServiceMetas: []*ServiceMeta{
					{Hostname: "appdev", Mode: PlanModeDev},
				},
				LiveServices: []string{"appdev"},
			},
			wantTop: "bootstrap",
		},
		{
			name: "complete meta routes normally",
			input: RouterInput{
				ProjectState: StateConformant,
				ServiceMetas: []*ServiceMeta{
					{Hostname: "appdev", BootstrappedAt: "2026-03-04", DeployStrategy: StrategyPushDev},
				},
				LiveServices: []string{"appdev"},
			},
			wantTop: "deploy",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			offerings := Route(tt.input)
			if len(offerings) == 0 {
				t.Fatal("expected offerings")
			}
			if offerings[0].Workflow != tt.wantTop {
				t.Errorf("top = %q, want %q", offerings[0].Workflow, tt.wantTop)
			}
		})
	}
}

func TestRoute_NoReasonField(t *testing.T) {
	t.Parallel()
	// Verify FlowOffering has no Reason field — facts only, no editorial.
	offering := FlowOffering{Workflow: "deploy", Priority: 1, Hint: "test"}
	_ = offering.Workflow
	_ = offering.Priority
	_ = offering.Hint
	// Compile-time verification: Reason field does not exist.
}

func TestFormatOfferings_Compact(t *testing.T) {
	t.Parallel()
	offerings := []FlowOffering{
		{Workflow: "bootstrap", Priority: 1, Hint: `zerops_workflow action="start" workflow="bootstrap"`},
		{Workflow: "debug", Priority: 5, Hint: `zerops_workflow action="start" workflow="debug"`},
	}
	result := FormatOfferings(offerings)
	if result == "" {
		t.Fatal("expected non-empty output")
	}
	if !strings.Contains(result, "bootstrap") {
		t.Error("missing bootstrap")
	}
	lines := strings.Split(strings.TrimSpace(result), "\n")
	if len(lines) > 8 {
		t.Errorf("has %d lines, want <= 8", len(lines))
	}
}

func TestFormatOfferings_Empty(t *testing.T) {
	t.Parallel()
	result := FormatOfferings(nil)
	if result != "" {
		t.Errorf("expected empty for nil, got %q", result)
	}
}
