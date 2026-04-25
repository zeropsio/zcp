package workflow

import (
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/topology"
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
				{Hostname: "appdev", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Bootstrapped: true, Mode: topology.ModeDev, Strategy: "push-git"},
				{Hostname: "db", TypeVersion: "postgresql@16", RuntimeClass: topology.RuntimeManaged},
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
		"bootstrapped=true",
		"mode=dev",
		"strategy=push-git",
		"deployed=false",
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

// TestRenderStatus_PerServiceMultiEmitsSection pins the multi-service render
// contract: when len(PerService) > 1 the `Per service:` block lists every
// hostname alphabetically.
func TestRenderStatus_PerServiceMultiEmitsSection(t *testing.T) {
	t.Parallel()

	plan := &Plan{
		Primary: NextAction{
			Label: "Deploy apidev",
			Tool:  "zerops_deploy",
			Args:  map[string]string{"targetService": "apidev"},
		},
		PerService: map[string]NextAction{
			"apidev": {
				Label: "Deploy apidev",
				Tool:  "zerops_deploy",
				Args:  map[string]string{"targetService": "apidev"},
			},
			"webdev": {
				Label: "Verify webdev",
				Tool:  "zerops_verify",
				Args:  map[string]string{"serviceHostname": "webdev"},
			},
		},
	}
	out := RenderStatus(Response{
		Envelope: StateEnvelope{Phase: PhaseDevelopActive},
		Plan:     plan,
	})
	if !strings.Contains(out, "Per service:") {
		t.Errorf("missing Per service: section:\n%s", out)
	}
	// Hostnames must appear in sorted order (apidev before webdev).
	api := strings.Index(out, "- apidev:")
	web := strings.Index(out, "- webdev:")
	if api < 0 || web < 0 {
		t.Fatalf("per-service rows missing:\n%s", out)
	}
	if api >= web {
		t.Errorf("per-service rows out of order — apidev=%d webdev=%d", api, web)
	}
	// Each row carries the tool + schema-matching arg key.
	if !strings.Contains(out, `zerops_deploy targetService="apidev"`) {
		t.Errorf("apidev row missing deploy args:\n%s", out)
	}
	if !strings.Contains(out, `zerops_verify serviceHostname="webdev"`) {
		t.Errorf("webdev row missing verify args:\n%s", out)
	}
}

// TestRenderStatus_PerServiceSingleOmitsSection pins the "single service =
// redundant" rule: one entry in PerService already lives in Primary, so the
// section is suppressed.
func TestRenderStatus_PerServiceSingleOmitsSection(t *testing.T) {
	t.Parallel()

	plan := &Plan{
		Primary: NextAction{
			Label: "Deploy appdev",
			Tool:  "zerops_deploy",
			Args:  map[string]string{"hostname": "appdev"},
		},
		PerService: map[string]NextAction{
			"appdev": {
				Label: "Deploy appdev",
				Tool:  "zerops_deploy",
				Args:  map[string]string{"hostname": "appdev"},
			},
		},
	}
	out := RenderStatus(Response{
		Envelope: StateEnvelope{Phase: PhaseDevelopActive},
		Plan:     plan,
	})
	if strings.Contains(out, "Per service:") {
		t.Errorf("single-service Plan should not emit Per service: section:\n%s", out)
	}
}

func timePtr(t time.Time) *time.Time { return &t }

// P8: an open work session with services pending renders a one-line
// "Auto-close blocked" call-to-action between Progress and Guidance.
// The line names the first pending host and suggests the concrete tool
// call that clears it (deploy if the service has no successful deploy,
// verify otherwise).
func TestRenderStatus_BlockersLineWhenPending(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		session      *WorkSessionSummary
		wantContains []string
		wantAbsent   []string
	}{
		{
			name:         "nil-session",
			session:      nil,
			wantContains: nil,
			wantAbsent:   []string{"Auto-close blocked"},
		},
		{
			name: "all-green",
			session: &WorkSessionSummary{
				Services: []string{"appdev"},
				Deploys:  map[string][]AttemptInfo{"appdev": {{Success: true}}},
				Verifies: map[string][]AttemptInfo{"appdev": {{Success: true}}},
			},
			wantAbsent: []string{"Auto-close blocked"},
		},
		{
			name: "deploy-pending",
			session: &WorkSessionSummary{
				Services: []string{"appdev"},
			},
			wantContains: []string{"Auto-close blocked: 0/1", "pending appdev", "zerops_deploy"},
		},
		{
			name: "verify-pending",
			session: &WorkSessionSummary{
				Services: []string{"appdev", "appstage"},
				Deploys: map[string][]AttemptInfo{
					"appdev":   {{Success: true}},
					"appstage": {{Success: true}},
				},
				Verifies: map[string][]AttemptInfo{
					"appdev": {{Success: true}},
				},
			},
			wantContains: []string{"Auto-close blocked: 1/2", "pending appstage", "zerops_verify", `"appstage"`},
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
			for _, needle := range tt.wantContains {
				if !strings.Contains(out, needle) {
					t.Errorf("output missing %q:\n%s", needle, out)
				}
			}
			for _, needle := range tt.wantAbsent {
				if strings.Contains(out, needle) {
					t.Errorf("output should not contain %q:\n%s", needle, out)
				}
			}
		})
	}
}
