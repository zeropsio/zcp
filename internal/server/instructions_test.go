// Tests for: server/instructions.go — BuildInstructions, buildProjectSummary.
package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

func TestBuildInstructions_ConformantState_HasBootstrapHint(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{
				ID:     "svc-1",
				Name:   "nodedev",
				Status: "ACTIVE",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{
					ServiceStackTypeVersionName: "nodejs@22",
				},
			},
			{
				ID:     "svc-2",
				Name:   "nodestage",
				Status: "ACTIVE",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{
					ServiceStackTypeVersionName: "nodejs@22",
				},
			},
		})

	stateDir := t.TempDir()
	result := BuildInstructions(context.Background(), mock, "proj-1", runtime.Info{}, stateDir)

	// Must contain deploy routing.
	if !strings.Contains(result, "deploy") {
		t.Error("expected deploy workflow in CONFORMANT state")
	}

	// Must contain bootstrap as secondary option.
	if !strings.Contains(result, "bootstrap") {
		t.Error("expected bootstrap workflow hint in CONFORMANT project summary")
	}

	// Must list services.
	summary := buildProjectSummary(context.Background(), mock, "proj-1", stateDir)
	if !strings.Contains(summary, "nodedev") {
		t.Error("expected service name in project summary")
	}
}

func TestBuildInstructions_EmptyProject_HasBootstrapOnly(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{})

	result := BuildInstructions(context.Background(), mock, "proj-1", runtime.Info{}, t.TempDir())

	if !strings.Contains(result, "bootstrap") {
		t.Error("expected bootstrap workflow hint for empty project")
	}
}

func TestBuildProjectSummary_RouterIntegration(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		services     []platform.ServiceStack
		metas        []*workflow.ServiceMeta
		wantContains []string
	}{
		{
			name:     "fresh project uses router",
			services: []platform.ServiceStack{},
			wantContains: []string{
				"bootstrap",
				"Available workflows",
			},
		},
		{
			name: "conformant with ci-cd meta",
			services: []platform.ServiceStack{
				{ID: "s1", Name: "appdev", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "bun@1.2"}},
				{ID: "s2", Name: "appstage", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "bun@1.2"}},
			},
			metas: []*workflow.ServiceMeta{
				{Hostname: "appdev", BootstrappedAt: "2026-01-01", DeployStrategy: workflow.StrategyCICD},
			},
			wantContains: []string{
				"cicd",
				"appdev",
			},
		},
		{
			name: "stale metas filtered",
			services: []platform.ServiceStack{
				{ID: "s1", Name: "appdev", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "bun@1.2"}},
				{ID: "s2", Name: "appstage", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "bun@1.2"}},
			},
			metas: []*workflow.ServiceMeta{
				{Hostname: "deletedservice", BootstrappedAt: "2026-01-01", DeployStrategy: workflow.StrategyCICD},
			},
			wantContains: []string{
				"deploy", // Falls back to deploy since ci-cd meta is stale
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := platform.NewMock().WithServices(tt.services)
			stateDir := t.TempDir()

			// Write service metas if provided.
			for _, meta := range tt.metas {
				if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
					t.Fatalf("write meta: %v", err)
				}
			}

			summary := buildProjectSummary(context.Background(), mock, "proj-1", stateDir)
			for _, want := range tt.wantContains {
				if !strings.Contains(summary, want) {
					t.Errorf("summary missing %q.\nGot:\n%s", want, summary)
				}
			}
		})
	}
}

// --- buildWorkflowHint tests ---

func TestBuildWorkflowHint_DeadPID_ShowsResumable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Register a dead PID session.
	entry := workflow.SessionEntry{
		SessionID: "dead-session-abc",
		PID:       9999999,
		Workflow:  "bootstrap",
		ProjectID: "proj-1",
		Intent:    "deploy my app",
	}
	if err := workflow.RegisterSession(dir, entry); err != nil {
		t.Fatalf("RegisterSession: %v", err)
	}

	hint := buildWorkflowHint(dir)
	if hint == "" {
		t.Fatal("expected non-empty hint for dead PID session")
	}
	if !strings.Contains(hint, "Resumable") {
		t.Errorf("hint should contain 'Resumable', got: %s", hint)
	}
	if !strings.Contains(hint, "dead-session-abc") {
		t.Errorf("hint should contain session ID, got: %s", hint)
	}
	if !strings.Contains(hint, `action="resume"`) {
		t.Errorf("hint should contain resume action, got: %s", hint)
	}
}

func TestBuildWorkflowHint_AlivePID_ShowsActive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Register an alive PID session.
	entry := workflow.SessionEntry{
		SessionID: "alive-session-xyz",
		PID:       os.Getpid(),
		Workflow:  "deploy",
		ProjectID: "proj-1",
		Intent:    "deploy code",
	}
	if err := workflow.RegisterSession(dir, entry); err != nil {
		t.Fatalf("RegisterSession: %v", err)
	}

	hint := buildWorkflowHint(dir)
	if hint == "" {
		t.Fatal("expected non-empty hint for alive PID session")
	}
	if !strings.Contains(hint, "Active workflow") {
		t.Errorf("hint should contain 'Active workflow', got: %s", hint)
	}
	if strings.Contains(hint, "Resumable") {
		t.Errorf("alive session should not be marked as Resumable, got: %s", hint)
	}
}

func TestBuildWorkflowHint_EmptyStateDir_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	if hint := buildWorkflowHint(""); hint != "" {
		t.Errorf("expected empty hint for empty stateDir, got: %s", hint)
	}
}

func TestBuildWorkflowHint_NoSessions_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if hint := buildWorkflowHint(dir); hint != "" {
		t.Errorf("expected empty hint for no sessions, got: %s", hint)
	}
}

func TestBuildProjectSummary_NilClient(t *testing.T) {
	t.Parallel()
	summary := buildProjectSummary(context.Background(), nil, "proj-1", t.TempDir())
	if summary != "" {
		t.Errorf("expected empty summary with nil client, got %q", summary)
	}
}

func TestBuildProjectSummary_EmptyStateDir(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{})
	// Empty stateDir should still work (no metas).
	summary := buildProjectSummary(context.Background(), mock, "proj-1", "")
	if !strings.Contains(summary, "bootstrap") {
		t.Error("expected bootstrap for empty project even without stateDir")
	}
}

func TestBuildProjectSummary_BadMetasDir_Graceful(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "s1", Name: "appdev", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "bun@1.2"}},
		{ID: "s2", Name: "appstage", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "bun@1.2"}},
	})
	stateDir := t.TempDir()
	// Create a file where directory is expected to cause ListServiceMetas to fail gracefully.
	badPath := filepath.Join(stateDir, "services")
	if err := os.WriteFile(badPath, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	summary := buildProjectSummary(context.Background(), mock, "proj-1", stateDir)
	// Should still produce summary (just without meta-based routing).
	if summary == "" {
		t.Error("expected non-empty summary even with bad metas dir")
	}
}
