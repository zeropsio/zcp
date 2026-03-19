package workflow

import (
	"strings"
	"testing"
)

func TestRoute_FreshProject_NoSessions_ReturnsBootstrap(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		input          RouterInput
		wantTop        string
		wantTopPrio    int
		wantMinOffered int
	}{
		{
			name: "fresh project, no sessions, no metas",
			input: RouterInput{
				ProjectState: StateFresh,
			},
			wantTop:        "bootstrap",
			wantTopPrio:    1,
			wantMinOffered: 4, // bootstrap + debug + scale + configure
		},
		{
			name: "fresh project, active bootstrap",
			input: RouterInput{
				ProjectState:   StateFresh,
				ActiveSessions: []SessionEntry{{Workflow: "bootstrap"}},
			},
			wantTop:     "bootstrap",
			wantTopPrio: 1,
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
				t.Errorf("top offering = %q, want %q", offerings[0].Workflow, tt.wantTop)
			}
			if offerings[0].Priority != tt.wantTopPrio {
				t.Errorf("top priority = %d, want %d", offerings[0].Priority, tt.wantTopPrio)
			}
			if tt.wantMinOffered > 0 && len(offerings) < tt.wantMinOffered {
				t.Errorf("offerings count = %d, want >= %d", len(offerings), tt.wantMinOffered)
			}
		})
	}
}

func TestRoute_ConformantProject_StrategyBased(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		input       RouterInput
		wantTop     string
		wantTopPrio int
		wantHint    string
	}{
		{
			name: "ci-cd strategy",
			input: RouterInput{
				ProjectState: StateConformant,
				ServiceMetas: []*ServiceMeta{{
					Hostname:  "appdev",
					Decisions: map[string]string{DecisionDeployStrategy: StrategyCICD},
				}},
				LiveServices: []string{"appdev"},
			},
			wantTop:     "cicd",
			wantTopPrio: 1,
			wantHint:    "cicd",
		},
		{
			name: "push-dev strategy",
			input: RouterInput{
				ProjectState: StateConformant,
				ServiceMetas: []*ServiceMeta{{
					Hostname:  "appdev",
					Decisions: map[string]string{DecisionDeployStrategy: StrategyPushDev},
				}},
				LiveServices: []string{"appdev"},
			},
			wantTop:     "deploy",
			wantTopPrio: 1,
		},
		{
			name: "manual strategy",
			input: RouterInput{
				ProjectState: StateConformant,
				ServiceMetas: []*ServiceMeta{{
					Hostname:  "appdev",
					Decisions: map[string]string{DecisionDeployStrategy: StrategyManual},
				}},
				LiveServices: []string{"appdev"},
			},
			wantTop:     "manual-deploy",
			wantTopPrio: 1,
		},
		{
			name: "no metas, conformant",
			input: RouterInput{
				ProjectState: StateConformant,
			},
			wantTop:     "deploy",
			wantTopPrio: 1,
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
				t.Errorf("top offering = %q, want %q", offerings[0].Workflow, tt.wantTop)
			}
			if offerings[0].Priority != tt.wantTopPrio {
				t.Errorf("top priority = %d, want %d", offerings[0].Priority, tt.wantTopPrio)
			}
			if tt.wantHint != "" && !strings.Contains(offerings[0].Hint, tt.wantHint) {
				t.Errorf("hint = %q, want to contain %q", offerings[0].Hint, tt.wantHint)
			}
			// Should always have bootstrap as secondary.
			hasBootstrap := false
			for _, o := range offerings {
				if o.Workflow == "bootstrap" {
					hasBootstrap = true
					if o.Priority != 2 {
						t.Errorf("bootstrap priority = %d, want 2", o.Priority)
					}
				}
			}
			if !hasBootstrap {
				t.Error("expected bootstrap as secondary offering")
			}
		})
	}
}

func TestRoute_NonConformant_MixedCoverage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    RouterInput
		wantTop  string
		wantBoot bool
	}{
		{
			name: "some metas with strategy",
			input: RouterInput{
				ProjectState: StateNonConformant,
				ServiceMetas: []*ServiceMeta{{
					Hostname:  "appdev",
					Decisions: map[string]string{DecisionDeployStrategy: StrategyPushDev},
				}},
				LiveServices: []string{"appdev"},
			},
			wantTop:  "deploy",
			wantBoot: true,
		},
		{
			name: "no metas",
			input: RouterInput{
				ProjectState: StateNonConformant,
			},
			wantTop:  "bootstrap",
			wantBoot: true,
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
				t.Errorf("top offering = %q, want %q", offerings[0].Workflow, tt.wantTop)
			}
			if tt.wantBoot {
				found := false
				for _, o := range offerings {
					if o.Workflow == "bootstrap" {
						found = true
					}
				}
				if !found {
					t.Error("expected bootstrap in offerings")
				}
			}
		})
	}
}

func TestRoute_Unknown_EqualPriority(t *testing.T) {
	t.Parallel()
	offerings := Route(RouterInput{ProjectState: StateUnknown})
	if len(offerings) == 0 {
		t.Fatal("expected offerings")
	}
	// All should have equal priority.
	prio := offerings[0].Priority
	for _, o := range offerings {
		if o.Priority != prio {
			t.Errorf("expected equal priorities, got %d and %d", prio, o.Priority)
		}
	}
}

func TestRoute_AlwaysIncludesUtilityWorkflows(t *testing.T) {
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
			{
				Hostname:  "appdev",
				Decisions: map[string]string{DecisionDeployStrategy: StrategyCICD},
			},
			{
				Hostname:  "staleservice",
				Decisions: map[string]string{DecisionDeployStrategy: StrategyPushDev},
			},
		},
		LiveServices: []string{"appdev"}, // staleservice is NOT live
	}
	offerings := Route(input)
	// Should route based on appdev's ci-cd strategy, not staleservice's push-dev.
	if offerings[0].Workflow != "cicd" {
		t.Errorf("top offering = %q, want cicd (stale meta should be filtered)", offerings[0].Workflow)
	}
}

func TestRoute_ActiveBootstrapSession_ResumeHint(t *testing.T) {
	t.Parallel()
	input := RouterInput{
		ProjectState:   StateFresh,
		ActiveSessions: []SessionEntry{{Workflow: "bootstrap", SessionID: "abc123"}},
	}
	offerings := Route(input)
	if len(offerings) == 0 {
		t.Fatal("expected offerings")
	}
	if offerings[0].Workflow != "bootstrap" {
		t.Errorf("top offering = %q, want bootstrap", offerings[0].Workflow)
	}
	if !strings.Contains(offerings[0].Hint, "resume") {
		t.Errorf("hint = %q, want to contain 'resume'", offerings[0].Hint)
	}
}

func TestFormatOfferings_Compact(t *testing.T) {
	t.Parallel()
	offerings := []FlowOffering{
		{Workflow: "bootstrap", Priority: 1, Reason: "Fresh project", Hint: "zerops_workflow action=\"start\" workflow=\"bootstrap\""},
		{Workflow: "debug", Priority: 5, Reason: "Always available", Hint: "zerops_workflow action=\"start\" workflow=\"debug\""},
		{Workflow: "scale", Priority: 5, Reason: "Always available", Hint: "zerops_workflow action=\"start\" workflow=\"scale\""},
		{Workflow: "configure", Priority: 5, Reason: "Always available", Hint: "zerops_workflow action=\"start\" workflow=\"configure\""},
	}
	result := FormatOfferings(offerings)
	if result == "" {
		t.Fatal("expected non-empty formatted output")
	}
	lines := strings.Split(strings.TrimSpace(result), "\n")
	if len(lines) > 8 {
		t.Errorf("formatted output has %d lines, want <= 8", len(lines))
	}
	if !strings.Contains(result, "bootstrap") {
		t.Error("formatted output missing bootstrap")
	}
}

func TestRoute_IntentBoost_PromotesMatchingWorkflow(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   RouterInput
		wantTop string
	}{
		{
			name: "deploy intent promotes deploy over bootstrap",
			input: RouterInput{
				ProjectState: StateNonConformant,
				ServiceMetas: []*ServiceMeta{{
					Hostname:  "appdev",
					Decisions: map[string]string{DecisionDeployStrategy: StrategyPushDev},
				}},
				LiveServices: []string{"appdev"},
				Intent:       "I want to deploy my code",
			},
			wantTop: "deploy",
		},
		{
			name: "debug intent boosts debug priority",
			input: RouterInput{
				ProjectState: StateUnknown,
				Intent:       "my app is broken and not working",
			},
			wantTop: "debug",
		},
		{
			name: "scale intent boosts scale priority",
			input: RouterInput{
				ProjectState: StateUnknown,
				Intent:       "app is slow, need more cpu",
			},
			wantTop: "scale",
		},
		{
			name: "cicd intent promotes cicd",
			input: RouterInput{
				ProjectState: StateConformant,
				ServiceMetas: []*ServiceMeta{{
					Hostname:  "appdev",
					Decisions: map[string]string{DecisionDeployStrategy: StrategyCICD},
				}},
				LiveServices: []string{"appdev"},
				Intent:       "set up github actions pipeline",
			},
			wantTop: "cicd",
		},
		{
			name: "no intent keeps default order",
			input: RouterInput{
				ProjectState: StateFresh,
				Intent:       "",
			},
			wantTop: "bootstrap",
		},
		{
			name: "case insensitive matching",
			input: RouterInput{
				ProjectState: StateUnknown,
				Intent:       "DEPLOY my app NOW",
			},
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
				t.Errorf("top offering = %q, want %q", offerings[0].Workflow, tt.wantTop)
			}
		})
	}
}

func TestFormatOfferings_Empty(t *testing.T) {
	t.Parallel()
	result := FormatOfferings(nil)
	if result != "" {
		t.Errorf("expected empty string for nil offerings, got %q", result)
	}
}
