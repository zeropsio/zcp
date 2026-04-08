// Tests for: server/instructions.go — BuildInstructions, buildProjectSummary, classification.
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

// --- Classification tests ---

func TestClassifyServices_EmptyProject(t *testing.T) {
	t.Parallel()
	cls := classifyServices(nil, nil, "")
	if cls.total != 0 {
		t.Errorf("total = %d, want 0", cls.total)
	}
	if len(cls.bootstrapped) != 0 || len(cls.unmanaged) != 0 || len(cls.managed) != 0 {
		t.Error("all buckets should be empty for nil services")
	}
}

func TestClassifyServices_MixedProject(t *testing.T) {
	t.Parallel()
	services := []platform.ServiceStack{
		{Name: "appdev", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		{Name: "appstage", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		{Name: "apidev", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "go@1"}},
		{Name: "db", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16"}},
	}
	metas := []*workflow.ServiceMeta{
		{Hostname: "appdev", BootstrappedAt: "2026-01-01", StageHostname: "appstage"},
	}

	cls := classifyServices(services, metas, "")

	if len(cls.bootstrapped) != 1 || cls.bootstrapped[0].Hostname != "appdev" {
		t.Errorf("bootstrapped = %v, want [appdev]", cls.bootstrapped)
	}
	if len(cls.unmanagedNames) != 1 || cls.unmanagedNames[0] != "apidev" {
		t.Errorf("unmanagedNames = %v, want [apidev]", cls.unmanagedNames)
	}
	if len(cls.managed) != 1 || cls.managed[0].Name != "db" {
		t.Errorf("managed = %v, want [db]", cls.managed)
	}
	// appstage is stage of bootstrapped — not in unmanaged.
	for _, name := range cls.unmanagedNames {
		if name == "appstage" {
			t.Error("appstage should not be in unmanagedNames (it's a stage of bootstrapped)")
		}
	}
}

func TestClassifyServices_SelfExcluded(t *testing.T) {
	t.Parallel()
	services := []platform.ServiceStack{
		{Name: "zcpx", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "zcp@1"}},
		{Name: "appdev", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
	}
	cls := classifyServices(services, nil, "zcpx")
	if cls.total != 1 {
		t.Errorf("total = %d, want 1 (zcpx excluded)", cls.total)
	}
}

func TestClassifyServices_LabelFor(t *testing.T) {
	t.Parallel()
	services := []platform.ServiceStack{
		{Name: "appdev", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		{Name: "appstage", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		{Name: "apidev", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "go@1"}},
		{Name: "db", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16"}},
	}
	metas := []*workflow.ServiceMeta{
		{Hostname: "appdev", BootstrappedAt: "2026-01-01", StageHostname: "appstage"},
	}
	cls := classifyServices(services, metas, "")

	if l := cls.labelFor("appdev"); l != "" {
		t.Errorf("bootstrapped label = %q, want empty", l)
	}
	if l := cls.labelFor("appstage"); l != "" {
		t.Errorf("stage label = %q, want empty", l)
	}
	if l := cls.labelFor("apidev"); !strings.Contains(l, "auto-adopted") {
		t.Errorf("unmanaged label = %q, want 'auto-adopted'", l)
	}
	if l := cls.labelFor("db"); l != "" {
		t.Errorf("managed label = %q, want empty", l)
	}
}

// --- BuildInstructions integration tests ---

func TestBuildInstructions_UnmanagedRuntimes_HasAdoptionHint(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{Name: "nodedev", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		{Name: "nodestage", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
	})

	stateDir := t.TempDir()
	result := BuildInstructions(context.Background(), mock, "proj-1", runtime.Info{}, stateDir)

	// Must contain auto-adopt messaging for unmanaged services.
	if !strings.Contains(result, "auto-adopted") {
		t.Error("expected 'auto-adopted' label for unbootstrapped runtimes")
	}
	// Must list services.
	if !strings.Contains(result, "nodedev") {
		t.Error("expected service name in project summary")
	}
}

func TestBuildInstructions_EmptyProject_HasBootstrapOnly(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{})
	result := BuildInstructions(context.Background(), mock, "proj-1", runtime.Info{}, t.TempDir())
	if !strings.Contains(result, "bootstrap") {
		t.Error("expected bootstrap workflow hint for empty project")
	}
}

func TestBuildInstructions_BootstrappedWithUnmanaged_BothVisible(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{Name: "appdev", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		{Name: "apidev", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "go@1"}},
		{Name: "db", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16"}},
	})
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname: "appdev", BootstrappedAt: "2026-01-01", DeployStrategy: workflow.StrategyPushDev,
	}); err != nil {
		t.Fatal(err)
	}

	result := BuildInstructions(context.Background(), mock, "proj-1", runtime.Info{}, stateDir)

	// Orientation for bootstrapped service.
	if !strings.Contains(result, "appdev") {
		t.Error("expected bootstrapped service detail")
	}
	// Auto-adopt hint for unmanaged runtime.
	if !strings.Contains(result, "auto-adopted") {
		t.Error("expected auto-adopt label for apidev")
	}
	// Router offerings (no short-circuit).
	if !strings.Contains(result, "Available workflows") {
		t.Error("expected router offerings even when orientation exists")
	}
	// Develop offering from strategy.
	if !strings.Contains(result, "develop") {
		t.Error("expected develop offering from bootstrapped meta strategy")
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
			name:     "empty project has router",
			services: []platform.ServiceStack{},
			wantContains: []string{
				"bootstrap",
				"Available workflows",
			},
		},
		{
			name: "bootstrapped with push-git meta",
			services: []platform.ServiceStack{
				{ID: "s1", Name: "appdev", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "bun@1.2"}},
				{ID: "s2", Name: "appstage", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "bun@1.2"}},
			},
			metas: []*workflow.ServiceMeta{
				{Hostname: "appdev", BootstrappedAt: "2026-01-01", DeployStrategy: workflow.StrategyPushGit},
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
				{Hostname: "deletedservice", BootstrappedAt: "2026-01-01", DeployStrategy: workflow.StrategyPushGit},
			},
			wantContains: []string{
				"bootstrap", // Stale meta filtered; unmanaged runtimes trigger adoption
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := platform.NewMock().WithServices(tt.services)
			stateDir := t.TempDir()
			for _, meta := range tt.metas {
				if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
					t.Fatalf("write meta: %v", err)
				}
			}
			summary := buildProjectSummary(context.Background(), mock, "proj-1", stateDir, "")
			for _, want := range tt.wantContains {
				if !strings.Contains(summary, want) {
					t.Errorf("summary missing %q.\nGot:\n%s", want, summary)
				}
			}
		})
	}
}

// --- buildWorkflowHint tests ---

func TestBuildWorkflowHint_DeadPID_ShownAsResumable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	entry := workflow.SessionEntry{
		SessionID: "dead-session-abc", PID: 9999999,
		Workflow: "bootstrap", ProjectID: "proj-1", Intent: "deploy my app",
	}
	if err := workflow.RegisterSession(dir, entry); err != nil {
		t.Fatalf("RegisterSession: %v", err)
	}
	// Create session file so LoadSessionByID works.
	if err := workflow.SaveSessionState(dir, "dead-session-abc", &workflow.WorkflowState{
		SessionID: "dead-session-abc", PID: 9999999, Workflow: "bootstrap",
	}); err != nil {
		t.Fatalf("SaveSessionState: %v", err)
	}
	hint := buildWorkflowHint(dir)
	if !strings.Contains(hint, "Resumable") {
		t.Errorf("dead PID sessions should be shown as resumable, got: %s", hint)
	}
	if !strings.Contains(hint, "dead-session-abc") {
		t.Errorf("hint should contain session ID, got: %s", hint)
	}
	// Session should NOT be pruned — still in registry.
	sessions, _ := workflow.ListSessions(dir)
	if len(sessions) != 1 {
		t.Errorf("expected 1 session preserved, got %d", len(sessions))
	}
}

func TestBuildWorkflowHint_AlivePID_ShowsActive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	entry := workflow.SessionEntry{
		SessionID: "alive-session-xyz", PID: os.Getpid(),
		Workflow: "develop", ProjectID: "proj-1", Intent: "deploy code",
	}
	if err := workflow.RegisterSession(dir, entry); err != nil {
		t.Fatalf("RegisterSession: %v", err)
	}
	hint := buildWorkflowHint(dir)
	if !strings.Contains(hint, "Active workflow") {
		t.Errorf("hint should contain 'Active workflow', got: %s", hint)
	}
}

func TestBuildWorkflowHint_EmptyStateDir_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	if hint := buildWorkflowHint(""); hint != "" {
		t.Errorf("expected empty hint, got: %s", hint)
	}
}

func TestBuildWorkflowHint_NoSessions_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	if hint := buildWorkflowHint(t.TempDir()); hint != "" {
		t.Errorf("expected empty hint, got: %s", hint)
	}
}

// --- instruction content tests ---

func TestBaseInstructions_ContainsWorkflowDirective(t *testing.T) {
	t.Parallel()
	for _, want := range []string{"Every code task", "workflow", "bootstrap", "develop"} {
		if !strings.Contains(baseInstructions, want) {
			t.Errorf("baseInstructions should contain %q", want)
		}
	}
}

func TestContainerEnvironment_Compact(t *testing.T) {
	t.Parallel()
	for _, want := range []string{"SSHFS", "ssh", "/var/www/"} {
		if !strings.Contains(containerEnvironment, want) {
			t.Errorf("containerEnvironment should contain %q", want)
		}
	}
}

func TestInstructions_FitIn2KB(t *testing.T) {
	t.Parallel()
	containerStatic := baseInstructions + routingInstructions + containerEnvironment
	localStatic := baseInstructions + routingInstructions + localEnvironment
	const limit = 2048
	if len(containerStatic) > limit {
		t.Errorf("container static instructions = %d bytes, must be under %d to leave room for dynamic content", len(containerStatic), limit)
	}
	if len(localStatic) > limit {
		t.Errorf("local static instructions = %d bytes, must be under %d to leave room for dynamic content", len(localStatic), limit)
	}
}

// --- Post-bootstrap orientation tests ---

func TestOrientation_DevMode_ManualStrategy(t *testing.T) {
	t.Parallel()
	cls := buildTestClassification([]*workflow.ServiceMeta{{
		Hostname: "appdev", Mode: "dev", DeployStrategy: workflow.StrategyManual, BootstrappedAt: "2026-03-04",
	}},
		[]platform.ServiceStack{{
			ID: "s1", Name: "appdev", Status: "ACTIVE",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"},
		}},
	)
	result := buildPostBootstrapOrientation(cls)
	for _, want := range []string{"appdev", "nodejs@22", "/var/www/appdev/", "ssh appdev", "zerops_deploy", "manual", "zerops_knowledge"} {
		if !strings.Contains(result, want) {
			t.Errorf("orientation missing %q.\nGot:\n%s", want, result)
		}
	}
	// Develop workflow now covers development/deployment/fixing for all strategies.
	if !strings.Contains(result, "Develop/deploy/fix") {
		t.Errorf("operations should include develop/deploy/fix.\nGot:\n%s", result)
	}
}

func TestOrientation_StandardMode_DevAndStage(t *testing.T) {
	t.Parallel()
	cls := buildTestClassification([]*workflow.ServiceMeta{{
		Hostname: "appdev", Mode: "standard", StageHostname: "appstage",
		DeployStrategy: workflow.StrategyPushDev, BootstrappedAt: "2026-03-04",
	}},
		[]platform.ServiceStack{
			{ID: "s1", Name: "appdev", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "go@1"}},
			{ID: "s2", Name: "appstage", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "go@1"}},
		},
	)
	result := buildPostBootstrapOrientation(cls)
	for _, want := range []string{"appdev", "appstage", "go@1", "ssh appdev", "auto-starts"} {
		if !strings.Contains(result, want) {
			t.Errorf("orientation missing %q.\nGot:\n%s", want, result)
		}
	}
	if !strings.Contains(result, `workflow="develop"`) {
		t.Errorf("push-dev strategy should mention develop workflow.\nGot:\n%s", result)
	}
}

func TestOrientation_SimpleMode(t *testing.T) {
	t.Parallel()
	cls := buildTestClassification([]*workflow.ServiceMeta{{
		Hostname: "web", Mode: "simple", DeployStrategy: workflow.StrategyPushDev, BootstrappedAt: "2026-03-04",
	}},
		[]platform.ServiceStack{{
			ID: "s1", Name: "web", Status: "ACTIVE",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "php-nginx@8.4"},
		}},
	)
	result := buildPostBootstrapOrientation(cls)
	if !strings.Contains(result, "auto-starts") {
		t.Errorf("simple mode should mention auto-start.\nGot:\n%s", result)
	}
	if strings.Contains(result, "zsc noop") {
		t.Errorf("simple mode should not mention zsc noop.\nGot:\n%s", result)
	}
}

func TestOrientation_NoServices(t *testing.T) {
	t.Parallel()
	cls := serviceClassification{}
	result := buildPostBootstrapOrientation(cls)
	if result != "" {
		t.Errorf("empty classification should return empty, got:\n%s", result)
	}
}

func TestOrientation_ManagedOnly(t *testing.T) {
	t.Parallel()
	cls := serviceClassification{
		managed: []platform.ServiceStack{
			{Name: "db", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16"}},
		},
		allServices: []platform.ServiceStack{
			{Name: "db", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16"}},
		},
	}
	result := buildPostBootstrapOrientation(cls)
	if !strings.Contains(result, "Managed infrastructure") {
		t.Errorf("managed-only should show managed section, got:\n%s", result)
	}
	if strings.Contains(result, "Bootstrapped") {
		t.Error("managed-only should not show bootstrapped section")
	}
}

func TestOrientation_UnmanagedOnly(t *testing.T) {
	t.Parallel()
	cls := serviceClassification{
		unmanaged: []platform.ServiceStack{
			{Name: "apidev", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "go@1"}},
		},
		allServices: []platform.ServiceStack{
			{Name: "apidev", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "go@1"}},
		},
	}
	result := buildPostBootstrapOrientation(cls)
	if !strings.Contains(result, "Runtime services needing adoption") {
		t.Errorf("unmanaged-only should show adoption section, got:\n%s", result)
	}
	if !strings.Contains(result, "Auto-adopted") {
		t.Errorf("adoption section should mention auto-adopt, got:\n%s", result)
	}
}

func TestOrientation_PushDevStrategy(t *testing.T) {
	t.Parallel()
	cls := buildTestClassification([]*workflow.ServiceMeta{{
		Hostname: "appdev", Mode: "dev", DeployStrategy: workflow.StrategyPushDev, BootstrappedAt: "2026-03-04",
	}},
		[]platform.ServiceStack{{
			ID: "s1", Name: "appdev", Status: "ACTIVE",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "bun@1.2"},
		}},
	)
	result := buildPostBootstrapOrientation(cls)
	if !strings.Contains(result, `workflow="develop"`) {
		t.Errorf("push-dev should suggest develop workflow.\nGot:\n%s", result)
	}
}

func TestBuildProjectSummary_NilClient(t *testing.T) {
	t.Parallel()
	if s := buildProjectSummary(context.Background(), nil, "proj-1", t.TempDir(), ""); s != "" {
		t.Errorf("expected empty summary with nil client, got %q", s)
	}
}

func TestBuildProjectSummary_EmptyStateDir(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{})
	summary := buildProjectSummary(context.Background(), mock, "proj-1", "", "")
	if !strings.Contains(summary, "bootstrap") {
		t.Error("expected bootstrap for empty project even without stateDir")
	}
}

func TestBuildProjectSummary_BadMetasDir_Graceful(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "s1", Name: "appdev", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "bun@1.2"}},
	})
	stateDir := t.TempDir()
	badPath := filepath.Join(stateDir, "services")
	if err := os.WriteFile(badPath, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	summary := buildProjectSummary(context.Background(), mock, "proj-1", stateDir, "")
	if summary == "" {
		t.Error("expected non-empty summary even with bad metas dir")
	}
}

// --- Base instruction content tests ---

func TestBaseInstructions_HasWorkflowCycle(t *testing.T) {
	t.Parallel()
	if !strings.Contains(baseInstructions, "After deploy") {
		t.Error("baseInstructions should mention starting new workflow after deploy")
	}
}

func TestBaseInstructions_HasDirectTools(t *testing.T) {
	t.Parallel()
	if !strings.Contains(baseInstructions, "zerops_scale") {
		t.Error("baseInstructions should list direct tools")
	}
}

func TestBaseInstructions_TrackedModeSyntax(t *testing.T) {
	t.Parallel()
	if !strings.Contains(baseInstructions, `action="start"`) {
		t.Error("baseInstructions should use tracked mode syntax")
	}
}

// --- test helpers ---

// buildTestClassification creates a serviceClassification for bootstrapped services.
func buildTestClassification(
	bootstrapped []*workflow.ServiceMeta,
	allServices []platform.ServiceStack,
) serviceClassification {
	metaMap := make(map[string]*workflow.ServiceMeta)
	stageOf := make(map[string]bool)
	for _, m := range bootstrapped {
		metaMap[m.Hostname] = m
		if m.StageHostname != "" {
			stageOf[m.StageHostname] = true
		}
	}
	return serviceClassification{
		bootstrapped:        bootstrapped,
		allServices:         allServices,
		metaMap:             metaMap,
		stageOfBootstrapped: stageOf,
	}
}
