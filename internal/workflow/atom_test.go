package workflow

import (
	"strings"
	"testing"
)

func TestParseAtom_FrontmatterParsing(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		content     string
		wantID      string
		wantPhases  []Phase
		wantRunts   []RuntimeClass
		wantModes   []Mode
		wantEnv     []Environment
		wantPrio    int
		wantBodyHas string
		wantErr     bool
	}{
		{
			name: "minimal_valid",
			content: `---
id: test-atom
phases: [develop-active]
---
body content`,
			wantID:      "test-atom",
			wantPhases:  []Phase{PhaseDevelopActive},
			wantPrio:    5,
			wantBodyHas: "body content",
		},
		{
			name: "full_axes",
			content: `---
id: full
priority: 2
phases: [develop-active, develop-closed-auto]
modes: [dev]
environments: [container, local]
runtimes: [dynamic]
title: "Dynamic runtime start"
---
SSH into the service and start the process.`,
			wantID:      "full",
			wantPhases:  []Phase{PhaseDevelopActive, PhaseDevelopClosed},
			wantModes:   []Mode{ModeDev},
			wantEnv:     []Environment{EnvContainer, EnvLocal},
			wantRunts:   []RuntimeClass{RuntimeDynamic},
			wantPrio:    2,
			wantBodyHas: "SSH into the service",
		},
		{
			name: "routes_axis",
			content: `---
id: adopt-only
priority: 3
phases: [bootstrap-active]
routes: [adopt]
---
Adopt flow guidance.`,
			wantID:      "adopt-only",
			wantPhases:  []Phase{PhaseBootstrapActive},
			wantPrio:    3,
			wantBodyHas: "Adopt flow guidance.",
		},
		{
			name: "empty_axes_except_phases",
			content: `---
id: any-mode
phases: [idle]
---
Applies broadly.`,
			wantID:      "any-mode",
			wantPhases:  []Phase{PhaseIdle},
			wantPrio:    5,
			wantBodyHas: "Applies broadly.",
		},
		{
			name: "invalid_priority_defaults",
			content: `---
id: bad-priority
priority: 42
phases: [idle]
---
x`,
			wantID:     "bad-priority",
			wantPhases: []Phase{PhaseIdle},
			wantPrio:   5,
		},
		{
			name: "missing_id",
			content: `---
phases: [idle]
---
body`,
			wantErr: true,
		},
		{
			name: "missing_phases",
			content: `---
id: no-phase
---
body`,
			wantErr: true,
		},
		{
			name: "missing_opening_delimiter",
			content: `id: x
phases: [idle]
---
body`,
			wantErr: true,
		},
		{
			name: "missing_closing_delimiter",
			content: `---
id: x
phases: [idle]
body`,
			wantErr: true,
		},
		{
			name: "malformed_frontmatter_line",
			content: `---
id: x
phases: [idle]
not a valid line
---
body`,
			wantErr: true,
		},
		{
			name: "priority_below_one_defaults",
			content: `---
id: low
priority: 0
phases: [idle]
---
body`,
			wantID:     "low",
			wantPhases: []Phase{PhaseIdle},
			wantPrio:   5,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			atom, err := ParseAtom(tt.content)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got atom=%+v", atom)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if atom.ID != tt.wantID {
				t.Errorf("ID: want %q got %q", tt.wantID, atom.ID)
			}
			if !equalPhases(atom.Axes.Phases, tt.wantPhases) {
				t.Errorf("Phases: want %v got %v", tt.wantPhases, atom.Axes.Phases)
			}
			if tt.wantModes != nil && !equalModes(atom.Axes.Modes, tt.wantModes) {
				t.Errorf("Modes: want %v got %v", tt.wantModes, atom.Axes.Modes)
			}
			if tt.wantEnv != nil && !equalEnvs(atom.Axes.Environments, tt.wantEnv) {
				t.Errorf("Environments: want %v got %v", tt.wantEnv, atom.Axes.Environments)
			}
			if tt.wantRunts != nil && !equalRuntimes(atom.Axes.Runtimes, tt.wantRunts) {
				t.Errorf("Runtimes: want %v got %v", tt.wantRunts, atom.Axes.Runtimes)
			}
			if atom.Priority != tt.wantPrio {
				t.Errorf("Priority: want %d got %d", tt.wantPrio, atom.Priority)
			}
			if tt.wantBodyHas != "" && !strings.Contains(atom.Body, tt.wantBodyHas) {
				t.Errorf("Body missing %q: %q", tt.wantBodyHas, atom.Body)
			}
		})
	}
}

func equalPhases(a, b []Phase) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalModes(a, b []Mode) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalEnvs(a, b []Environment) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalRuntimes(a, b []RuntimeClass) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestParseAtom_RoutesAxis isolates the routes frontmatter parsing since the
// other cases don't exercise it. Confirms both single-value and multi-value
// route lists round-trip into AxisVector.Routes.
func TestParseAtom_RoutesAxis(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		frontmatter string
		want        []BootstrapRoute
	}{
		{"single", "routes: [recipe]", []BootstrapRoute{BootstrapRouteRecipe}},
		{"multiple", "routes: [classic, adopt]", []BootstrapRoute{BootstrapRouteClassic, BootstrapRouteAdopt}},
		{"absent", "", nil},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			content := "---\nid: t\nphases: [bootstrap-active]\n"
			if tt.frontmatter != "" {
				content += tt.frontmatter + "\n"
			}
			content += "---\nbody"
			atom, err := ParseAtom(content)
			if err != nil {
				t.Fatalf("ParseAtom: %v", err)
			}
			if len(atom.Axes.Routes) != len(tt.want) {
				t.Fatalf("Routes len = %d, want %d (%v)", len(atom.Axes.Routes), len(tt.want), atom.Axes.Routes)
			}
			for i, w := range tt.want {
				if atom.Axes.Routes[i] != w {
					t.Errorf("Routes[%d] = %q, want %q", i, atom.Axes.Routes[i], w)
				}
			}
		})
	}
}
