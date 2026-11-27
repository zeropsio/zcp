package workflow

import (
	"strings"
	"testing"
)

// witness: internal/workflow/router.go | if len(incomplete) > 0 && !owners[s.SessionID] { → if false {
func TestResumeHint_RHOwned_OnlyOfferedOwners(t *testing.T) {
	t.Parallel()
	sessions := []SessionEntry{
		{Workflow: WorkflowBootstrap, SessionID: "session-A", Intent: "add alpha", CreatedAt: "2026-01-01T00:00:00Z"},
		{Workflow: WorkflowBootstrap, SessionID: "session-B", Intent: "add beta", CreatedAt: "2026-01-02T00:00:00Z"},
	}
	for _, tc := range []struct {
		name       string
		metas      []*ServiceMeta
		want, omit []string
	}{
		{"only B owns the offered meta", []*ServiceMeta{{BootstrapSession: "session-B"}}, []string{`sessionId="session-B" (intent="add beta", created="2026-01-02T00:00:00Z")`}, []string{"session-A", "add alpha"}},
		{"A and B own offered metas", []*ServiceMeta{{BootstrapSession: "session-A"}, {BootstrapSession: "session-B"}}, []string{`sessionId="session-A" (intent="add alpha", created="2026-01-01T00:00:00Z")`, `sessionId="session-B" (intent="add beta", created="2026-01-02T00:00:00Z")`}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := resumeHint(true, sessions, tc.metas)
			for _, part := range tc.want {
				if !strings.Contains(got, part) {
					t.Errorf("hint %q does not name %q", got, part)
				}
			}
			for _, part := range tc.omit {
				if strings.Contains(got, part) {
					t.Errorf("hint %q names unrelated %q", got, part)
				}
			}
		})
	}
}

// witness: internal/workflow/router.go | lead = start + "\nInterrupted bootstrap sessions:" → lead = "Interrupted bootstrap sessions: " + start
func TestResumeHint_RHEmpty_StartBeforeInterruptedSessions(t *testing.T) {
	t.Parallel()
	sessions := []SessionEntry{
		{Workflow: WorkflowBootstrap, SessionID: "session-A", Intent: "add alpha", CreatedAt: "2026-01-01T00:00:00Z"},
		{Workflow: WorkflowBootstrap, SessionID: "session-B", Intent: "add beta", CreatedAt: "2026-01-02T00:00:00Z"},
	}
	got := resumeHint(true, sessions, nil)
	if !strings.HasPrefix(got, `zerops_workflow action="start" workflow="bootstrap"`) {
		t.Errorf("start is not first: %q", got)
	}
	for _, part := range []string{`sessionId="session-A" (intent="add alpha", created="2026-01-01T00:00:00Z")`, `sessionId="session-B" (intent="add beta", created="2026-01-02T00:00:00Z")`, "conversation that began"} {
		if !strings.Contains(got, part) {
			t.Errorf("hint %q omits %q", got, part)
		}
	}
}

// witness: internal/workflow/router.go | if s.PID == currentPID { → if s.PID != currentPID {
func TestResumableSessions_RHLive_ExcludesOtherProcesses(t *testing.T) {
	t.Parallel()
	alive := []SessionEntry{
		{Workflow: WorkflowBootstrap, SessionID: "other", PID: 200},
		{Workflow: WorkflowBootstrap, SessionID: "own", PID: 100},
	}
	dead := []SessionEntry{{Workflow: WorkflowBootstrap, SessionID: "interrupted", PID: 300}}
	got := resumeHint(true, ResumableSessions(alive, dead, 100), nil)
	if strings.Contains(got, "other") || !strings.Contains(got, "own") || !strings.Contains(got, "interrupted") {
		t.Fatalf("resumable hint = %q", got)
	}
}

// witness: internal/workflow/router.go | if s.Workflow == WorkflowBootstrap { → if s.Workflow != WorkflowBootstrap {
func TestResumeHint_RHStandalone_FirstBootstrapOrStart(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		sessions []SessionEntry
		metas    []*ServiceMeta
		want     string
	}{
		{"empty", nil, nil, `zerops_workflow action="start" workflow="bootstrap"`},
		{"first bootstrap", []SessionEntry{{Workflow: "work"}, {Workflow: WorkflowBootstrap, SessionID: "A"}, {Workflow: WorkflowBootstrap, SessionID: "B"}}, []*ServiceMeta{{BootstrapSession: "B"}}, `zerops_workflow action="resume" sessionId="A"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := resumeHint(false, tc.sessions, tc.metas); got != tc.want {
				t.Fatalf("hint = %q, want %q", got, tc.want)
			}
		})
	}
}

// witness: internal/workflow/router.go | Hint:     resumeHint(input.Mate, input.ActiveSessions, incompleteMetas), → Hint:     resumeHint(input.Mate, input.ActiveSessions, nil),
func TestRoute_RHRoute_IncompleteOfferingUsesOwners(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		metas []*ServiceMeta
		want  []string
		omit  []string
	}{
		{"one owner", []*ServiceMeta{{Hostname: "dev-b", BootstrapSession: "session-B"}}, []string{"session-B", "add beta"}, []string{"session-A", "action=\"start\""}},
		{"both owners", []*ServiceMeta{{Hostname: "dev-a", BootstrapSession: "session-A"}, {Hostname: "dev-b", BootstrapSession: "session-B"}}, []string{"session-A", "add alpha", "session-B", "add beta"}, []string{"action=\"start\""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			offerings := Route(RouterInput{Mate: true, ServiceMetas: tc.metas, ActiveSessions: []SessionEntry{
				{Workflow: WorkflowBootstrap, SessionID: "session-A", Intent: "add alpha", CreatedAt: "2026-01-01T00:00:00Z"},
				{Workflow: WorkflowBootstrap, SessionID: "session-B", Intent: "add beta", CreatedAt: "2026-01-02T00:00:00Z"},
			}})
			var hints []string
			for _, offering := range offerings {
				if offering.Workflow == WorkflowBootstrap {
					hints = append(hints, offering.Hint)
				}
			}
			if len(hints) != 1 {
				t.Fatalf("bootstrap hints = %q, want one incomplete offering", hints)
			}
			for _, part := range tc.want {
				if !strings.Contains(hints[0], part) {
					t.Errorf("hint %q omits %q", hints[0], part)
				}
			}
			for _, part := range tc.omit {
				if strings.Contains(hints[0], part) {
					t.Errorf("hint %q contains %q", hints[0], part)
				}
			}
		})
	}
}
