// Tests for: tools/workflow_develop.go, tools/workflow.go — a develop start
// and an idle status in a project whose parts' dev environments are Swarm
// pools (docs/spec-workflows.md §8 DP-29): a scope naming only pools, or a
// project whose only runtimes are pools, is answered with the dev-pool loop —
// lease a container, work in it — and opens no develop session, while
// ADOPT_REQUIRED stays for runtimes that really are unadopted.
package tools

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// devPoolTestService is a service the way the platform lists it.
func devPoolTestService(name, typeVersion, category string) platform.ServiceStack {
	return platform.ServiceStack{ID: "svc-" + name, Name: name, Status: "ACTIVE",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: typeVersion, ServiceStackTypeCategoryName: category}}
}

var (
	devPoolHQ  = devPoolTestService("hqpool", "swarm@1", "USER")
	devPoolWeb = devPoolTestService("webpool", "swarm@1", "USER")
	devPoolDB  = devPoolTestService("db", "postgresql@18", "STANDARD")
	devPoolApp = devPoolTestService("appdev", "nodejs@22", "USER")
)

// adoptAppdev writes appdev's meta, as an adopt leaves it.
func adoptAppdev(t *testing.T, stateDir string) {
	t.Helper()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname: "appdev", Mode: topology.PlanModeDev, BootstrapSession: "s1", BootstrappedAt: "2026-10-24T00:00:00Z",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
}

func TestHandleDevelopBriefing_DevPoolScope_AnswersTheLeaseLoop(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		services []platform.ServiceStack
		adopted  bool
		scope    []string
		want     string // the plan's lease step
	}{
		{
			name:     "a scope naming only pools beside runtimes still to adopt",
			services: []platform.ServiceStack{devPoolApp, devPoolDB, devPoolHQ, devPoolWeb},
			scope:    []string{"webpool", "hqpool"},
			want:     `zerops_lease action="acquire" pools=["hqpool","webpool"]`,
		},
		{
			name:     "a project whose only runtimes are pools, no scope",
			services: []platform.ServiceStack{devPoolDB, devPoolHQ, devPoolWeb},
			want:     `zerops_lease action="acquire" pools=["hqpool","webpool"]`,
		},
		{
			name:     "a project whose only runtimes are pools, scope naming one",
			services: []platform.ServiceStack{devPoolDB, devPoolHQ, devPoolWeb},
			scope:    []string{"webpool"},
			want:     `zerops_lease action="acquire" pool="webpool"`,
		},
		{
			name:     "a scope naming only a pool beside an adopted runtime",
			services: []platform.ServiceStack{devPoolApp, devPoolWeb},
			adopted:  true,
			scope:    []string{"webpool"},
			want:     `zerops_lease action="acquire" pool="webpool"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stateDir := t.TempDir()
			if tt.adopted {
				adoptAppdev(t, stateDir)
			}
			engine := workflow.NewEngine(stateDir, workflow.EnvContainer, nil)
			mock := platform.NewMock().WithServices(tt.services).WithProject(&platform.Project{ID: "proj-1", Name: "demo"})
			t.Cleanup(func() { _ = workflow.DeleteWorkSession(stateDir, os.Getpid()) })

			result, _, err := handleDevelopBriefing(context.Background(), engine, mock, "proj-1",
				WorkflowInput{Intent: "say whose Chief each project is", Scope: tt.scope}, runtime.Info{InContainer: true}, nil)
			if err != nil {
				t.Fatalf("handleDevelopBriefing: %v", err)
			}
			text := extractText(result)
			if result.IsError {
				t.Fatalf("develop start refused: %s", text)
			}
			for _, want := range []string{tt.want, "### A part's dev pool", "No develop session"} {
				if !strings.Contains(text, want) {
					t.Errorf("briefing lacks %q:\n%s", want, text)
				}
			}
			if strings.Contains(text, platform.ErrAdoptRequired) {
				t.Errorf("briefing names %s:\n%s", platform.ErrAdoptRequired, text)
			}
			if ws, _ := workflow.CurrentWorkSession(stateDir); ws != nil {
				t.Errorf("a dev-pool briefing opened a develop session: %+v", ws)
			}
		})
	}
}

func TestHandleDevelopBriefing_DevPools_UnadoptedRuntime_StillAdoptRequired(t *testing.T) {
	t.Parallel()

	for _, scope := range [][]string{nil, {"webpool", "appdev"}} {
		t.Run(strings.Join(scope, "+"), func(t *testing.T) {
			t.Parallel()
			stateDir := t.TempDir()
			engine := workflow.NewEngine(stateDir, workflow.EnvContainer, nil)
			mock := platform.NewMock().WithServices([]platform.ServiceStack{devPoolApp, devPoolWeb})

			result, _, err := handleDevelopBriefing(context.Background(), engine, mock, "proj-1",
				WorkflowInput{Intent: "add a health endpoint", Scope: scope}, runtime.Info{InContainer: true}, nil)
			if err != nil {
				t.Fatalf("handleDevelopBriefing: %v", err)
			}
			if text := extractText(result); !result.IsError || !strings.Contains(text, platform.ErrAdoptRequired) {
				t.Errorf("scope %v beside an unadopted runtime, want %s:\n%s", scope, platform.ErrAdoptRequired, text)
			}
		})
	}
}

func TestHandleDevelopBriefing_DevPools_ScopeBesideOtherHosts_RefusedNamingTheLease(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		services []platform.ServiceStack
		adopted  bool
		scope    []string
	}{
		{"a pool beside an adopted runtime", []platform.ServiceStack{devPoolApp, devPoolWeb}, true, []string{"webpool", "appdev"}},
		{"a host no service of a project of pools", []platform.ServiceStack{devPoolDB, devPoolWeb}, false, []string{"nosuch"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stateDir := t.TempDir()
			if tt.adopted {
				adoptAppdev(t, stateDir)
			}
			engine := workflow.NewEngine(stateDir, workflow.EnvContainer, nil)
			mock := platform.NewMock().WithServices(tt.services)
			t.Cleanup(func() { _ = workflow.DeleteWorkSession(stateDir, os.Getpid()) })

			result, _, err := handleDevelopBriefing(context.Background(), engine, mock, "proj-1",
				WorkflowInput{Intent: "fix the header", Scope: tt.scope}, runtime.Info{InContainer: true}, nil)
			if err != nil {
				t.Fatalf("handleDevelopBriefing: %v", err)
			}
			text := extractText(result)
			if !result.IsError || !strings.Contains(text, platform.ErrInvalidParameter) {
				t.Fatalf("scope %v, want %s:\n%s", tt.scope, platform.ErrInvalidParameter, text)
			}
			for _, want := range []string{"webpool", "zerops_lease"} {
				if !strings.Contains(text, want) {
					t.Errorf("refusal does not name %q:\n%s", want, text)
				}
			}
			if ws, _ := workflow.CurrentWorkSession(stateDir); ws != nil {
				t.Errorf("a refused scope opened a session: %+v", ws)
			}
		})
	}
}

// TestHandleLifecycleStatus_DevPools_NameTheLease: idle status names
// zerops_lease as the way to work on the project's pools — a project of pools
// only is led to a lease, never told it is empty; beside a runtime still to
// adopt the adopt stays the next step and the lease is named beside it.
func TestHandleLifecycleStatus_DevPools_NameTheLease(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		services    []platform.ServiceStack
		wantPrimary string
	}{
		{"a project whose only runtimes are pools", []platform.ServiceStack{devPoolDB, devPoolHQ, devPoolWeb},
			`Primary: Lease one container per part the branch edits and runs — zerops_lease action="acquire" pools=["hqpool","webpool"]`},
		{"pools beside a runtime still to adopt", []platform.ServiceStack{devPoolApp, devPoolHQ, devPoolWeb},
			`zerops_workflow action="start" route="adopt" workflow="bootstrap"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			eng := workflow.NewEngine(t.TempDir(), workflow.EnvContainer, nil)
			mock := platform.NewMock().WithProject(&platform.Project{ID: "proj-1", Name: "demo"}).WithServices(tt.services)

			result, _, err := handleLifecycleStatus(context.Background(), eng, mock, "proj-1", runtime.Info{InContainer: true}, nil)
			if err != nil {
				t.Fatalf("handleLifecycleStatus: %v", err)
			}
			text := getTextContent(t, result)
			for _, want := range []string{"zerops_lease action=status", "zerops_lease action=acquire", tt.wantPrimary} {
				if !strings.Contains(text, want) {
					t.Errorf("status lacks %q:\n%s", want, text)
				}
			}
			if strings.Contains(text, "This is an empty project") {
				t.Errorf("status of a project holding pools calls it empty:\n%s", text)
			}
		})
	}
}
