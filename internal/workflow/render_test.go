package workflow

import (
	"strings"
	"testing"
	"time"
)

// sampleIdlePlan returns a Plan shaped like the idle-with-bootstrapped scenario:
// primary + secondary-like alternatives. Keeps the render tests focused on
// formatting rather than the planner branch logic.
func sampleIdlePlan() *Plan {
	return &Plan{
		Primary: NextAction{
			Label:     "Start a develop task",
			Tool:      "zerops_workflow",
			Args:      map[string]string{"action": "start", "workflow": "develop", "intent": "..."},
			Rationale: "Bootstrapped services are ready.",
		},
		Alternatives: []NextAction{
			{
				Label: "Adopt unmanaged runtimes",
				Tool:  "zerops_workflow",
				Args:  map[string]string{"action": "start", "workflow": "develop", "intent": "adopt"},
			},
			{
				Label: "Add more services",
				Tool:  "zerops_workflow",
				Args:  map[string]string{"action": "start", "workflow": "bootstrap"},
			},
		},
	}
}

func TestRenderStatus_PrioritizedNext(t *testing.T) {
	t.Parallel()

	resp := Response{
		Envelope: StateEnvelope{
			Phase:     PhaseIdle,
			Generated: time.Date(2026, 4, 19, 0, 0, 0, 0, time.UTC),
		},
		Plan: &Plan{
			Primary: NextAction{
				Label: "Close current develop session",
				Tool:  "zerops_workflow",
				Args:  map[string]string{"action": "close", "workflow": "develop"},
			},
			Secondary: &NextAction{
				Label: "Start next develop task",
				Tool:  "zerops_workflow",
				Args:  map[string]string{"action": "start", "workflow": "develop"},
			},
			Alternatives: []NextAction{
				{Label: "Inspect logs", Tool: "zerops_logs"},
			},
		},
	}
	out := RenderStatus(resp)

	// D6 fix: priority markers must appear and in the correct order.
	primaryIdx := strings.Index(out, "▸ Primary:")
	secondaryIdx := strings.Index(out, "◦ Secondary:")
	altIdx := strings.Index(out, "· Alternatives:")
	if primaryIdx < 0 || secondaryIdx < 0 || altIdx < 0 {
		t.Fatalf("missing priority markers:\n%s", out)
	}
	if primaryIdx >= secondaryIdx || secondaryIdx >= altIdx {
		t.Errorf("markers out of order — primary=%d secondary=%d alt=%d", primaryIdx, secondaryIdx, altIdx)
	}
}

func TestRenderStatus_SkipsEmptyGuidance(t *testing.T) {
	t.Parallel()

	resp := Response{
		Envelope: StateEnvelope{Phase: PhaseIdle},
		// Guidance intentionally empty.
	}
	out := RenderStatus(resp)
	if strings.Contains(out, "Guidance:") {
		t.Errorf("empty guidance should not emit header:\n%s", out)
	}
}

func TestRenderStatus_ProgressOnlyWhenActive(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		session   *WorkSessionSummary
		wantShown bool
	}{
		{"nil-session", nil, false},
		{
			name: "closed-session",
			session: &WorkSessionSummary{
				Intent:   "done",
				Services: []string{"appdev"},
				Deploys:  map[string][]AttemptInfo{"appdev": {{Success: true}}},
				ClosedAt: timePtr(time.Date(2026, 4, 19, 1, 0, 0, 0, time.UTC)),
			},
			wantShown: false,
		},
		{
			name: "open-with-attempts",
			session: &WorkSessionSummary{
				Intent:   "fix",
				Services: []string{"appdev"},
				Deploys:  map[string][]AttemptInfo{"appdev": {{Success: true}}},
			},
			wantShown: true,
		},
		{
			name: "open-no-attempts",
			session: &WorkSessionSummary{
				Intent:   "fresh",
				Services: []string{"appdev"},
			},
			wantShown: false,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out := RenderStatus(Response{
				Envelope: StateEnvelope{
					Phase:       PhaseDevelopActive,
					WorkSession: tt.session,
				},
			})
			gotShown := strings.Contains(out, "Progress:")
			if gotShown != tt.wantShown {
				t.Errorf("Progress shown = %v, want %v\n%s", gotShown, tt.wantShown, out)
			}
		})
	}
}

func TestRenderStatus_IdleRenders(t *testing.T) {
	t.Parallel()

	resp := Response{
		Envelope: StateEnvelope{
			Phase: PhaseIdle,
			Services: []ServiceSnapshot{
				{Hostname: "appdev", TypeVersion: "nodejs@22", RuntimeClass: RuntimeDynamic, Bootstrapped: true, Mode: ModeDev, Strategy: "push-git"},
				{Hostname: "db", TypeVersion: "postgresql@16", RuntimeClass: RuntimeManaged},
			},
		},
		Plan: sampleIdlePlan(),
	}
	out := RenderStatus(resp)

	for _, want := range []string{
		"Phase: idle",
		"Services: appdev, db",
		"appdev (nodejs@22)",
		"db (postgresql@16) — managed",
		"mode=dev",
		"strategy=push-git",
		"▸ Primary:",
		"· Alternatives:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Progress:") {
		t.Errorf("idle should not render progress:\n%s", out)
	}
}

func TestRenderStatus_DeterministicArgs(t *testing.T) {
	t.Parallel()

	// Call RenderStatus repeatedly with a map-valued Plan.Primary. Go's map
	// iteration is non-deterministic; formatAction must sort keys to produce
	// byte-identical output run to run.
	resp := Response{
		Envelope: StateEnvelope{Phase: PhaseIdle},
		Plan: &Plan{
			Primary: NextAction{
				Label: "x",
				Tool:  "zerops_workflow",
				Args: map[string]string{
					"zebra": "z",
					"alpha": "a",
					"mike":  "m",
				},
			},
		},
	}
	first := RenderStatus(resp)
	for i := range 20 {
		if got := RenderStatus(resp); got != first {
			t.Fatalf("render %d differs:\nfirst: %s\ngot:   %s", i, first, got)
		}
	}
	// The args line must appear in alpha, mike, zebra order.
	wantOrder := `alpha="a" mike="m" zebra="z"`
	if !strings.Contains(first, wantOrder) {
		t.Errorf("args not sorted:\n%s", first)
	}
}

func TestRenderStatus_NoPlanOmitsNextSection(t *testing.T) {
	t.Parallel()

	out := RenderStatus(Response{
		Envelope: StateEnvelope{Phase: PhaseIdle},
		// Plan nil
	})
	if strings.Contains(out, "Next:") {
		t.Errorf("nil plan should not render Next section:\n%s", out)
	}
}

func TestRenderStatus_DevelopActivePhaseLineShowsIntent(t *testing.T) {
	t.Parallel()

	out := RenderStatus(Response{
		Envelope: StateEnvelope{
			Phase: PhaseDevelopActive,
			WorkSession: &WorkSessionSummary{
				Intent:   "repair login flow",
				Services: []string{"appdev"},
			},
		},
	})
	if !strings.Contains(out, `intent: "repair login flow"`) {
		t.Errorf("phase line missing intent:\n%s", out)
	}
}

func timePtr(t time.Time) *time.Time { return &t }
