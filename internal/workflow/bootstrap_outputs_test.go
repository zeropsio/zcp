package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBootstrapComplete_WritesServiceMeta(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvContainer, nil)

	_, err := eng.BootstrapStart("proj-1", "bun + postgres")
	if err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	// Submit plan.
	_, err = eng.BootstrapCompletePlan([]BootstrapTarget{{
		Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", ExplicitStage: "appstage"},
		Dependencies: []Dependency{
			{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA", Resolution: "CREATE"},
		},
	}}, nil, nil)
	if err != nil {
		t.Fatalf("BootstrapCompletePlan: %v", err)
	}

	// Complete remaining steps to trigger autoComplete.
	for _, step := range []string{"provision", "close"} {
		if _, err := eng.BootstrapComplete(context.Background(), step, "Attestation for "+step+" step completed ok", nil); err != nil {
			t.Fatalf("BootstrapComplete(%s): %v", step, err)
		}
	}

	// Verify runtime service meta exists, managed dep meta does NOT.
	metaPath := filepath.Join(dir, "services", "appdev.json")
	if _, err := os.Stat(metaPath); os.IsNotExist(err) {
		t.Error("expected service meta file for appdev")
	}

	dbMetaPath := filepath.Join(dir, "services", "db.json")
	if _, err := os.Stat(dbMetaPath); !os.IsNotExist(err) {
		t.Error("managed dep meta should NOT be written — API is authoritative for managed services")
	}
}

func TestBootstrapComplete_AppendsReflog(t *testing.T) {
	t.Parallel()
	// Create project root with CLAUDE.md.
	projectRoot := t.TempDir()
	stateDir := filepath.Join(projectRoot, ".zcp", "state")

	eng := NewEngine(stateDir, EnvLocal, nil)

	_, err := eng.BootstrapStart("proj-1", "deploy app")
	if err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	_, err = eng.BootstrapCompletePlan([]BootstrapTarget{{
		Runtime: RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2", ExplicitStage: "appstage"},
	}}, nil, nil)
	if err != nil {
		t.Fatalf("BootstrapCompletePlan: %v", err)
	}

	for _, step := range []string{"provision", "close"} {
		_, err = eng.BootstrapComplete(context.Background(), step, "Attestation for "+step+" step completed ok", nil)
		if err != nil {
			t.Fatalf("BootstrapComplete(%s): %v", step, err)
		}
	}

	// Verify CLAUDE.md has reflog entry.
	claudePath := filepath.Join(projectRoot, "CLAUDE.md")
	data, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "ZEROPS:REFLOG") {
		t.Error("CLAUDE.md should contain ZEROPS:REFLOG marker")
	}
	if !strings.Contains(content, "appdev") {
		t.Error("CLAUDE.md should contain hostname appdev")
	}
}

func TestBootstrapComplete_OutputErrorsNonFatal(t *testing.T) {
	t.Parallel()
	// Use a stateDir that doesn't have the expected .zcp/state structure,
	// so reflog path derivation points to a read-only location.
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	_, err := eng.BootstrapStart("proj-1", "test")
	if err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	_, err = eng.BootstrapCompletePlan([]BootstrapTarget{{
		Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", BootstrapMode: "simple"},
	}}, nil, nil)
	if err != nil {
		t.Fatalf("BootstrapCompletePlan: %v", err)
	}

	// Make services dir unwritable to force WriteServiceMeta failure.
	servicesDir := filepath.Join(dir, "services")
	if err := os.MkdirAll(servicesDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Chmod(servicesDir, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(servicesDir, 0o755) })

	// Bootstrap completion should still succeed despite output errors.
	var lastResp *BootstrapResponse
	for _, step := range []string{"provision", "close"} {
		var err error
		lastResp, err = eng.BootstrapComplete(context.Background(), step, "Attestation for "+step+" step completed ok", nil)
		if err != nil {
			t.Fatalf("BootstrapComplete(%s) should not fail due to output errors: %v", step, err)
		}
	}

	// Verify bootstrap actually completed via the response (session is deleted on completion).
	if lastResp == nil || lastResp.Current != nil {
		t.Error("Bootstrap should be completed (no current step in final response)")
	}
	if lastResp.Progress.Completed != 3 {
		t.Errorf("Bootstrap progress: want 3 completed, got %d", lastResp.Progress.Completed)
	}
	if eng.SessionID() != "" {
		t.Errorf("engine SessionID should be empty after bootstrap completion, got %q", eng.SessionID())
	}
}

func TestWriteBootstrapOutputs_NeverWritesDepMetas(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		resolution string
	}{
		{"CREATE dep — no meta written", "CREATE"},
		{"EXISTS dep — no meta written", "EXISTS"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			eng := NewEngine(dir, EnvContainer, nil)

			_, err := eng.BootstrapStart("proj-1", "app + db")
			if err != nil {
				t.Fatalf("BootstrapStart: %v", err)
			}

			_, err = eng.BootstrapCompletePlan([]BootstrapTarget{{
				Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", ExplicitStage: "appstage"},
				Dependencies: []Dependency{
					{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA", Resolution: tt.resolution},
				},
			}}, nil, nil)
			if err != nil {
				t.Fatalf("BootstrapCompletePlan: %v", err)
			}

			for _, step := range []string{"provision", "close"} {
				if _, err := eng.BootstrapComplete(context.Background(), step, "Attestation for "+step+" step completed ok", nil); err != nil {
					t.Fatalf("BootstrapComplete(%s): %v", step, err)
				}
			}

			// Managed dep metas are NEVER written — API is authoritative.
			meta, err := ReadServiceMeta(dir, "db")
			if err != nil {
				t.Fatalf("ReadServiceMeta: %v", err)
			}
			if meta != nil {
				t.Errorf("managed dep meta should NOT be written for resolution %s", tt.resolution)
			}

			// Runtime meta SHOULD exist.
			appMeta, err := ReadServiceMeta(dir, "appdev")
			if err != nil {
				t.Fatalf("ReadServiceMeta(appdev): %v", err)
			}
			if appMeta == nil {
				t.Error("runtime meta should exist for appdev")
			}
		})
	}
}

func TestWriteBootstrapOutputs_PreExistingDepMetaSurvives(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	// Pre-write a legacy dep meta on disk.
	legacyMeta := &ServiceMeta{
		Hostname:         "db",
		BootstrapSession: "old-session",
		BootstrappedAt:   "2026-01-01",
	}
	if err := WriteServiceMeta(dir, legacyMeta); err != nil {
		t.Fatalf("pre-write meta: %v", err)
	}

	_, err := eng.BootstrapStart("proj-1", "app + existing db")
	if err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	_, err = eng.BootstrapCompletePlan([]BootstrapTarget{{
		Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", ExplicitStage: "appstage"},
		Dependencies: []Dependency{
			{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA", Resolution: "EXISTS"},
		},
	}}, nil, nil)
	if err != nil {
		t.Fatalf("BootstrapCompletePlan: %v", err)
	}

	for _, step := range []string{"provision", "close"} {
		if _, err := eng.BootstrapComplete(context.Background(), step, "Attestation for "+step+" step completed ok", nil); err != nil {
			t.Fatalf("BootstrapComplete(%s): %v", step, err)
		}
	}

	// Pre-existing dep meta must survive untouched (not deleted, not overwritten).
	dbMeta, err := ReadServiceMeta(dir, "db")
	if err != nil {
		t.Fatalf("ReadServiceMeta(db): %v", err)
	}
	if dbMeta == nil {
		t.Fatal("pre-existing dep meta should survive bootstrap")
	}
	if dbMeta.BootstrapSession != "old-session" {
		t.Errorf("pre-existing dep meta should be untouched, got session %q", dbMeta.BootstrapSession)
	}
}

func TestWriteBootstrapOutputs_SetsBootstrappedAt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvContainer, nil)

	_, err := eng.BootstrapStart("proj-1", "app + db")
	if err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	_, err = eng.BootstrapCompletePlan([]BootstrapTarget{{
		Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", ExplicitStage: "appstage"},
		Dependencies: []Dependency{
			{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA", Resolution: "CREATE"},
		},
	}}, nil, nil)
	if err != nil {
		t.Fatalf("BootstrapCompletePlan: %v", err)
	}

	for _, step := range []string{"provision", "close"} {
		if _, err := eng.BootstrapComplete(context.Background(), step, "Attestation for "+step+" step completed ok", nil); err != nil {
			t.Fatalf("BootstrapComplete(%s): %v", step, err)
		}
	}

	// After full bootstrap, runtime meta should be complete (BootstrappedAt set).
	appMeta, err := ReadServiceMeta(dir, "appdev")
	if err != nil {
		t.Fatalf("ReadServiceMeta(appdev): %v", err)
	}
	if appMeta == nil {
		t.Fatal("expected appdev meta")
	}
	if !appMeta.IsComplete() {
		t.Error("appdev should be complete after full bootstrap")
	}
	if appMeta.BootstrappedAt == "" {
		t.Error("appdev BootstrappedAt should be set")
	}
}

func TestProvisionMeta_NoMetaAfterPlan(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	_, err := eng.BootstrapStart("proj-1", "app + db")
	if err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	_, err = eng.BootstrapCompletePlan([]BootstrapTarget{{
		Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", ExplicitStage: "appstage"},
		Dependencies: []Dependency{
			{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA", Resolution: "CREATE"},
		},
	}}, nil, nil)
	if err != nil {
		t.Fatalf("BootstrapCompletePlan: %v", err)
	}

	// After plan completion, NO metas should exist yet (meta written at provision, not plan).
	appMeta, err := ReadServiceMeta(dir, "appdev")
	if err != nil {
		t.Fatalf("ReadServiceMeta(appdev): %v", err)
	}
	if appMeta != nil {
		t.Error("no meta should exist after plan step — metas are written at provision")
	}
}

func TestProvisionMeta_WritesPartialMeta(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvContainer, nil)

	_, err := eng.BootstrapStart("proj-1", "app + db")
	if err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	_, err = eng.BootstrapCompletePlan([]BootstrapTarget{{
		Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", ExplicitStage: "appstage"},
		Dependencies: []Dependency{
			{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA", Resolution: "CREATE"},
		},
	}}, nil, nil)
	if err != nil {
		t.Fatalf("BootstrapCompletePlan: %v", err)
	}

	// Complete provision step.
	if _, err := eng.BootstrapComplete(context.Background(), "provision", "Provisioned all services ok", nil); err != nil {
		t.Fatalf("BootstrapComplete(provision): %v", err)
	}

	// After provision, partial metas should exist but be incomplete (no BootstrappedAt).
	appMeta, err := ReadServiceMeta(dir, "appdev")
	if err != nil {
		t.Fatalf("ReadServiceMeta(appdev): %v", err)
	}
	if appMeta == nil {
		t.Fatal("expected appdev meta after provision")
	}
	if appMeta.IsComplete() {
		t.Error("meta should NOT be complete after provision (BootstrappedAt should be empty)")
	}
	if appMeta.Hostname != "appdev" {
		t.Errorf("Hostname: want appdev, got %s", appMeta.Hostname)
	}

	// Managed dep meta should NOT be written at provision.
	dbMeta, err := ReadServiceMeta(dir, "db")
	if err != nil {
		t.Fatalf("ReadServiceMeta(db): %v", err)
	}
	if dbMeta != nil {
		t.Error("managed dep meta should NOT be written at provision — API is authoritative")
	}
}

func TestProvisionMeta_PreExistingDepMetaSurvives(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	// Pre-write a legacy dep meta on disk.
	existingMeta := &ServiceMeta{
		Hostname:         "db",
		BootstrapSession: "old-session",
		BootstrappedAt:   "2026-01-01",
	}
	if err := WriteServiceMeta(dir, existingMeta); err != nil {
		t.Fatalf("pre-write meta: %v", err)
	}

	_, err := eng.BootstrapStart("proj-1", "app + existing db")
	if err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	_, err = eng.BootstrapCompletePlan([]BootstrapTarget{{
		Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", ExplicitStage: "appstage"},
		Dependencies: []Dependency{
			{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA", Resolution: "EXISTS"},
		},
	}}, nil, nil)
	if err != nil {
		t.Fatalf("BootstrapCompletePlan: %v", err)
	}

	// Complete provision — dep meta should NOT be touched.
	if _, err := eng.BootstrapComplete(context.Background(), "provision", "Provisioned all services ok", nil); err != nil {
		t.Fatalf("BootstrapComplete(provision): %v", err)
	}

	// Pre-existing dep meta must survive untouched.
	dbMeta, err := ReadServiceMeta(dir, "db")
	if err != nil {
		t.Fatalf("ReadServiceMeta(db): %v", err)
	}
	if dbMeta == nil {
		t.Fatal("pre-existing dep meta should survive provision")
	}
	if dbMeta.BootstrapSession != "old-session" {
		t.Errorf("pre-existing dep meta should be untouched, got session %q", dbMeta.BootstrapSession)
	}
}

// --- C7: Bootstrap always writes empty DeployStrategy ---

func TestWriteBootstrapOutputs_AlwaysWritesEmptyStrategy(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvContainer, nil)

	_, err := eng.BootstrapStart("proj-1", "strategy must be empty")
	if err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	_, err = eng.BootstrapCompletePlan([]BootstrapTarget{{
		Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", ExplicitStage: "appstage"},
	}}, nil, nil)
	if err != nil {
		t.Fatalf("BootstrapCompletePlan: %v", err)
	}

	for _, step := range []string{"provision", "close"} {
		if _, err := eng.BootstrapComplete(context.Background(), step, "Attestation for "+step+" step completed ok", nil); err != nil {
			t.Fatalf("BootstrapComplete(%s): %v", step, err)
		}
	}

	meta, err := ReadServiceMeta(dir, "appdev")
	if err != nil {
		t.Fatalf("ReadServiceMeta: %v", err)
	}
	if meta == nil {
		t.Fatal("expected appdev meta")
	}
	if meta.DeployStrategy != "" {
		t.Errorf("bootstrap must write empty DeployStrategy, got %q", meta.DeployStrategy)
	}
}

func TestWriteBootstrapOutputs_DefaultEmptyStrategy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		bootstrapMode Mode
		env           Environment
	}{
		{"dev mode gets empty strategy", PlanModeDev, EnvLocal},
		{"simple mode gets empty strategy", PlanModeSimple, EnvLocal},
		{"standard mode gets empty strategy", PlanModeStandard, EnvContainer},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			eng := NewEngine(dir, tt.env, nil)

			_, err := eng.BootstrapStart("proj-1", "no auto-assign test")
			if err != nil {
				t.Fatalf("BootstrapStart: %v", err)
			}

			_, err = eng.BootstrapCompletePlan([]BootstrapTarget{{
				Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", BootstrapMode: tt.bootstrapMode, ExplicitStage: "appstage"},
			}}, nil, nil)
			if err != nil {
				t.Fatalf("BootstrapCompletePlan: %v", err)
			}

			// Bootstrap always writes empty strategy — user sets it later.
			for _, step := range []string{"provision", "close"} {
				if _, err := eng.BootstrapComplete(context.Background(), step, "Attestation for "+step+" step completed ok", nil); err != nil {
					t.Fatalf("BootstrapComplete(%s): %v", step, err)
				}
			}

			meta, err := ReadServiceMeta(dir, "appdev")
			if err != nil {
				t.Fatalf("ReadServiceMeta: %v", err)
			}
			if meta == nil {
				t.Fatal("expected appdev meta")
			}
			if meta.DeployStrategy != "" {
				t.Errorf("DeployStrategy: want empty, got %q", meta.DeployStrategy)
			}
		})
	}
}

func TestWriteBootstrapOutputs_LocalMode_DefaultEmptyStrategy(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	_, err := eng.BootstrapStart("proj-1", "local mode strategy test")
	if err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	_, err = eng.BootstrapCompletePlan([]BootstrapTarget{{
		Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", BootstrapMode: PlanModeDev},
	}}, nil, nil)
	if err != nil {
		t.Fatalf("BootstrapCompletePlan: %v", err)
	}

	for _, step := range []string{"provision", "close"} {
		if _, err := eng.BootstrapComplete(context.Background(), step, "Attestation for "+step+" step completed ok", nil); err != nil {
			t.Fatalf("BootstrapComplete(%s): %v", step, err)
		}
	}

	meta, err := ReadServiceMeta(dir, "appdev")
	if err != nil {
		t.Fatalf("ReadServiceMeta: %v", err)
	}
	if meta == nil {
		t.Fatal("expected appdev meta")
	}
	if meta.DeployStrategy != "" {
		t.Errorf("bootstrap must write empty DeployStrategy, got %q", meta.DeployStrategy)
	}
}

func TestProvisionMeta_SetsMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		bootstrapMode Mode
		wantMode      Mode
		env           Environment
	}{
		{"standard mode (default)", "", PlanModeStandard, EnvContainer},
		{"dev mode", PlanModeDev, PlanModeDev, EnvLocal},
		{"simple mode", PlanModeSimple, PlanModeSimple, EnvLocal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			eng := NewEngine(dir, tt.env, nil)

			_, err := eng.BootstrapStart("proj-1", "mode field test")
			if err != nil {
				t.Fatalf("BootstrapStart: %v", err)
			}

			_, err = eng.BootstrapCompletePlan([]BootstrapTarget{{
				Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", BootstrapMode: tt.bootstrapMode, ExplicitStage: "appstage"},
			}}, nil, nil)
			if err != nil {
				t.Fatalf("BootstrapCompletePlan: %v", err)
			}

			// Complete provision step to trigger meta write.
			if _, err := eng.BootstrapComplete(context.Background(), "provision", "Provisioned all services ok", nil); err != nil {
				t.Fatalf("BootstrapComplete(provision): %v", err)
			}

			// After provision, check that partial meta has Mode set.
			meta, err := ReadServiceMeta(dir, "appdev")
			if err != nil {
				t.Fatalf("ReadServiceMeta: %v", err)
			}
			if meta == nil {
				t.Fatal("expected appdev meta after provision")
			}
			if meta.Mode != tt.wantMode {
				t.Errorf("Mode: want %q, got %q", tt.wantMode, meta.Mode)
			}
		})
	}
}

// --- BuildTransitionMessage tests (Phase 3) ---

func TestBuildTransitionMessage_NilState_ReturnsSimpleMessage(t *testing.T) {
	t.Parallel()
	msg := BuildTransitionMessage(nil)
	if msg != "Bootstrap complete." {
		t.Errorf("want simple message for nil state, got: %q", msg)
	}
}

func TestBuildTransitionMessage_NilPlan_ReturnsSimpleMessage(t *testing.T) {
	t.Parallel()
	state := &WorkflowState{}
	msg := BuildTransitionMessage(state)
	if msg != "Bootstrap complete." {
		t.Errorf("want simple message for nil plan, got: %q", msg)
	}
}

func TestBuildTransitionMessage_WithPlan_IncludesServices(t *testing.T) {
	t.Parallel()
	state := &WorkflowState{
		Bootstrap: &BootstrapState{
			Plan: &ServicePlan{
				Targets: []BootstrapTarget{
					{
						Runtime: RuntimeTarget{
							DevHostname:   "appdev",
							Type:          "nodejs@22",
							BootstrapMode: PlanModeStandard,
						},
						Dependencies: []Dependency{
							{Hostname: "db", Type: "postgresql@16"},
						},
					},
				},
			},
		},
	}
	msg := BuildTransitionMessage(state)
	if !strings.Contains(msg, "appdev") {
		t.Error("message should contain service hostname appdev")
	}
	if !strings.Contains(msg, "nodejs@22") {
		t.Error("message should contain runtime type nodejs@22")
	}
	if !strings.Contains(msg, "db") {
		t.Error("message should contain dependency db")
	}
}

func TestBuildTransitionMessage_WithPlan_NoStrategySection(t *testing.T) {
	t.Parallel()
	state := &WorkflowState{
		Bootstrap: &BootstrapState{
			Plan: &ServicePlan{
				Targets: []BootstrapTarget{
					{
						Runtime: RuntimeTarget{
							DevHostname:   "appdev",
							Type:          "nodejs@22",
							BootstrapMode: PlanModeStandard,
						},
					},
				},
			},
		},
	}
	msg := BuildTransitionMessage(state)
	// Bootstrap must NOT include strategy selection.
	if strings.Contains(msg, "Deploy Strategy") {
		t.Error("bootstrap transition must NOT contain Deploy Strategy section")
	}
	if strings.Contains(msg, `action="strategy"`) {
		t.Error("bootstrap transition must NOT contain strategy action command")
	}
}

func TestBuildTransitionMessage_WithPlan_IncludesTransitionHint(t *testing.T) {
	t.Parallel()
	state := &WorkflowState{
		Bootstrap: &BootstrapState{
			Plan: &ServicePlan{
				Targets: []BootstrapTarget{
					{
						Runtime: RuntimeTarget{
							DevHostname:   "appdev",
							Type:          "nodejs@22",
							BootstrapMode: PlanModeStandard,
						},
					},
				},
			},
		},
	}
	msg := BuildTransitionMessage(state)
	if !strings.Contains(msg, "develop") {
		t.Error("message should hint at develop flow")
	}
	// Option A: bootstrap hands off to develop BEFORE any code/deploy —
	// the transition message explains infra is ready and develop owns
	// scaffolding + first deploy.
	if !strings.Contains(msg, "Develop owns code") {
		t.Error("message should explain develop owns code scaffolding and first deploy")
	}
}

func TestBuildTransitionMessage_WithPlan_IncludesRouterOffering(t *testing.T) {
	t.Parallel()
	state := &WorkflowState{
		Bootstrap: &BootstrapState{
			Plan: &ServicePlan{
				Targets: []BootstrapTarget{
					{
						Runtime: RuntimeTarget{
							DevHostname:   "appdev",
							Type:          "nodejs@22",
							BootstrapMode: PlanModeStandard,
						},
					},
				},
			},
		},
	}
	msg := BuildTransitionMessage(state)
	// Should list available workflows with priorities.
	if !strings.Contains(msg, "develop") && !strings.Contains(msg, "workflow") {
		t.Error("message should include available workflows from router")
	}
}

func TestBuildTransitionMessage_WithMultipleServices_ListsAll(t *testing.T) {
	t.Parallel()
	state := &WorkflowState{
		Bootstrap: &BootstrapState{
			Plan: &ServicePlan{
				Targets: []BootstrapTarget{
					{
						Runtime: RuntimeTarget{
							DevHostname:   "appdev",
							Type:          "nodejs@22",
							BootstrapMode: PlanModeStandard,
						},
					},
					{
						Runtime: RuntimeTarget{
							DevHostname:   "apidev",
							Type:          "go@1.22",
							BootstrapMode: PlanModeStandard,
						},
					},
				},
			},
		},
	}
	msg := BuildTransitionMessage(state)
	if !strings.Contains(msg, "appdev") {
		t.Error("message should list appdev")
	}
	if !strings.Contains(msg, "apidev") {
		t.Error("message should list apidev")
	}
}

func TestBuildTransitionMessage_NoStrategyOptions(t *testing.T) {
	t.Parallel()
	state := &WorkflowState{
		Bootstrap: &BootstrapState{
			Plan: &ServicePlan{
				Targets: []BootstrapTarget{
					{
						Runtime: RuntimeTarget{
							DevHostname:   "appdev",
							Type:          "nodejs@22",
							BootstrapMode: PlanModeStandard,
						},
					},
				},
			},
		},
	}
	msg := BuildTransitionMessage(state)
	// Bootstrap transition must NOT list strategy options.
	if strings.Contains(msg, "push-dev") {
		t.Error("bootstrap transition must NOT list push-dev strategy")
	}
	if strings.Contains(msg, "push-git") {
		t.Error("bootstrap transition must NOT list push-git strategy")
	}
	if strings.Contains(msg, "manual") {
		t.Error("bootstrap transition must NOT list manual strategy")
	}
}

func TestBuildTransitionMessage_EmptyTargets_ManagedOnly(t *testing.T) {
	t.Parallel()
	state := &WorkflowState{
		Bootstrap: &BootstrapState{
			Plan: &ServicePlan{
				Targets: []BootstrapTarget{},
			},
		},
	}
	msg := BuildTransitionMessage(state)

	// Should contain completion message.
	if !strings.Contains(msg, "Bootstrap complete.") {
		t.Error("message should contain bootstrap complete")
	}
	// Should mention managed-only context.
	if !strings.Contains(msg, "Managed services provisioned") {
		t.Error("message should mention managed services provisioned")
	}
	// Should NOT contain runtime-oriented sections.
	if strings.Contains(msg, "Deploy Strategy") {
		t.Error("managed-only message should NOT contain Deploy Strategy section")
	}
	if strings.Contains(msg, "push-git Gate") {
		t.Error("managed-only message should NOT contain push-git Gate section")
	}
	// Should offer utility operations.
	if !strings.Contains(msg, "scale") {
		t.Error("managed-only message should offer scale tool")
	}
	if !strings.Contains(msg, "zerops_env") {
		t.Error("managed-only message should offer env var management")
	}
}

func TestBuildTransitionMessage_IncludesRouterOfferings(t *testing.T) {
	t.Parallel()
	state := &WorkflowState{
		Bootstrap: &BootstrapState{
			Plan: &ServicePlan{
				Targets: []BootstrapTarget{
					{
						Runtime: RuntimeTarget{
							DevHostname:   "appdev",
							Type:          "nodejs@22",
							BootstrapMode: PlanModeStandard,
						},
					},
				},
			},
		},
	}
	msg := BuildTransitionMessage(state)
	// Should include deploy and push-git as router offerings.
	if !strings.Contains(msg, "Develop") && !strings.Contains(msg, "develop") {
		t.Error("message should include develop offering")
	}
	if !strings.Contains(msg, "What's Next") {
		t.Error("message should include What's Next section")
	}
}

func TestBuildTransitionMessage_IncludesDeployModelPrimer(t *testing.T) {
	t.Parallel()
	state := &WorkflowState{
		Bootstrap: &BootstrapState{
			Plan: &ServicePlan{
				Targets: []BootstrapTarget{
					{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", ExplicitStage: "appstage"}},
				},
			},
		},
	}
	msg := BuildTransitionMessage(state)
	if !strings.Contains(msg, "new container") {
		t.Error("transition message should mention that deploy creates a new container")
	}
	if !strings.Contains(msg, "deployFiles") {
		t.Error("transition message should mention deployFiles as what persists")
	}
	if !strings.Contains(msg, "sudo") {
		t.Error("transition message should mention sudo for prepareCommands")
	}
}

func TestBuildTransitionMessage_Adoption_NoHelloWorld(t *testing.T) {
	t.Parallel()
	state := &WorkflowState{
		Bootstrap: &BootstrapState{
			Plan: &ServicePlan{
				Targets: []BootstrapTarget{
					{
						Runtime:      RuntimeTarget{DevHostname: "appdev", Type: "php-nginx@8.4", IsExisting: true},
						Dependencies: []Dependency{{Hostname: "db", Type: "postgresql@18", Resolution: "EXISTS"}},
					},
				},
			},
		},
	}
	msg := BuildTransitionMessage(state)
	if strings.Contains(msg, "hello-world") {
		t.Error("adoption transition should NOT mention hello-world verification server")
	}
	if strings.Contains(msg, "No application code") {
		t.Error("adoption transition should NOT say 'No application code'")
	}
	if !strings.Contains(msg, "adopted") && !strings.Contains(msg, "Adopted") {
		t.Error("adoption transition should mention adoption")
	}
	if !strings.Contains(msg, "appdev") {
		t.Error("adoption transition should list service hostnames")
	}
	if !strings.Contains(msg, "develop") {
		t.Error("adoption transition should mention develop workflow")
	}
}

// Phase B.5: bootstrap writes the meta keyed by the dev hostname regardless
// of environment — the legacy "local mode inverts hostname to stage" hack
// was deleted. Local envs that want a stage-only topology go through
// adopt-local (writes Mode=local-stage), not bootstrap. This test pins the
// uniform shape.
func TestWriteBootstrapOutputs_LocalMode_KeyedByDevHostname(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	_, err := eng.BootstrapStart("proj-1", "test")
	if err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}
	_, err = eng.BootstrapCompletePlan([]BootstrapTarget{{
		Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", ExplicitStage: "appstage"},
	}}, nil, nil)
	if err != nil {
		t.Fatalf("BootstrapCompletePlan: %v", err)
	}
	for _, step := range []string{"provision", "close"} {
		if _, err := eng.BootstrapComplete(context.Background(), step, "Attestation for "+step+" step completed ok", nil); err != nil {
			t.Fatalf("BootstrapComplete(%s): %v", step, err)
		}
	}

	meta, err := ReadServiceMeta(dir, "appdev")
	if err != nil {
		t.Fatalf("ReadServiceMeta(appdev): %v", err)
	}
	if meta == nil {
		t.Fatal("expected appdev meta keyed by dev hostname in local mode")
	}
	if meta.Hostname != "appdev" {
		t.Errorf("hostname = %s, want appdev", meta.Hostname)
	}
	if meta.StageHostname != "appstage" {
		t.Errorf("stageHostname = %s, want appstage", meta.StageHostname)
	}
	if meta.Mode != PlanModeStandard {
		t.Errorf("mode = %s, want standard", meta.Mode)
	}
	if meta.DeployStrategy != "" {
		t.Errorf("bootstrap must write empty DeployStrategy, got %q", meta.DeployStrategy)
	}

	// No separate stage-keyed meta — the stage lives inside the dev meta's
	// StageHostname field.
	if stageMeta, _ := ReadServiceMeta(dir, "appstage"); stageMeta != nil {
		t.Error("appstage meta should NOT exist on its own — stage is a field of the dev meta")
	}
}

// --- Phase 3: Adoption simplification — isExisting targets get empty BootstrapSession ---

func TestWriteBootstrapOutputs_AdoptedService_EmptyBootstrapSession(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name             string
		isExisting       bool
		wantEmptySession bool
	}{
		{"adopted (isExisting=true) gets empty BootstrapSession", true, true},
		{"new service (isExisting=false) gets session ID", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			eng := NewEngine(dir, EnvContainer, nil)

			_, err := eng.BootstrapStart("proj-1", "adoption test")
			if err != nil {
				t.Fatalf("BootstrapStart: %v", err)
			}

			sessionID := eng.SessionID()
			if sessionID == "" {
				t.Fatal("expected non-empty session ID")
			}

			_, err = eng.BootstrapCompletePlan([]BootstrapTarget{{
				Runtime: RuntimeTarget{
					DevHostname:   "appdev",
					Type:          "nodejs@22",
					IsExisting:    tt.isExisting,
					ExplicitStage: "appstage",
				},
			}}, nil, nil)
			if err != nil {
				t.Fatalf("BootstrapCompletePlan: %v", err)
			}

			if tt.isExisting {
				// Adoption fast path: provision auto-completes remaining steps.
				if _, err := eng.BootstrapComplete(context.Background(), "provision", "All services exist and are running", nil); err != nil {
					t.Fatalf("BootstrapComplete(provision): %v", err)
				}
			} else {
				for _, step := range []string{"provision", "close"} {
					if _, err := eng.BootstrapComplete(context.Background(), step, "Attestation for "+step+" step completed ok", nil); err != nil {
						t.Fatalf("BootstrapComplete(%s): %v", step, err)
					}
				}
			}

			meta, err := ReadServiceMeta(dir, "appdev")
			if err != nil {
				t.Fatalf("ReadServiceMeta: %v", err)
			}
			if meta == nil {
				t.Fatal("expected appdev meta")
			}

			if tt.wantEmptySession {
				if meta.BootstrapSession != "" {
					t.Errorf("adopted service BootstrapSession: want empty, got %q", meta.BootstrapSession)
				}
			} else {
				if meta.BootstrapSession != sessionID {
					t.Errorf("new service BootstrapSession: want %q, got %q", sessionID, meta.BootstrapSession)
				}
			}

			// BootstrappedAt should always be set regardless of adoption.
			if meta.BootstrappedAt == "" {
				t.Error("BootstrappedAt should be set for both adopted and new services")
			}
		})
	}
}

func TestProvisionMeta_AdoptedService_EmptyBootstrapSession(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvContainer, nil)

	_, err := eng.BootstrapStart("proj-1", "adoption provision test")
	if err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	_, err = eng.BootstrapCompletePlan([]BootstrapTarget{{
		Runtime: RuntimeTarget{
			DevHostname:   "appdev",
			Type:          "nodejs@22",
			IsExisting:    true,
			ExplicitStage: "appstage",
		},
	}}, nil, nil)
	if err != nil {
		t.Fatalf("BootstrapCompletePlan: %v", err)
	}

	if _, err := eng.BootstrapComplete(context.Background(), "provision", "Provisioned ok", nil); err != nil {
		t.Fatalf("BootstrapComplete(provision): %v", err)
	}

	meta, err := ReadServiceMeta(dir, "appdev")
	if err != nil {
		t.Fatalf("ReadServiceMeta: %v", err)
	}
	if meta == nil {
		t.Fatal("expected appdev meta after provision")
	}

	// Adopted service provision meta should also have empty BootstrapSession.
	if meta.BootstrapSession != "" {
		t.Errorf("adopted service provision BootstrapSession: want empty, got %q", meta.BootstrapSession)
	}
}

// Mode-expansion path (§9.1): the existing runtime's user-authored fields —
// BootstrappedAt, DeployStrategy, StrategyConfirmed, FirstDeployedAt —
// must survive the upgrade. Only Mode / StageHostname change. Without the
// merge, a dev→standard upgrade would silently revert the user's push-git
// choice and lose deploy history.
//
// Flow note: when the single runtime target is IsExisting=true with no
// live-new dependencies, bootstrap's fast-path auto-skips `close` (set by
// the adoption branch in BootstrapComplete after provision). The final
// meta write fires from provision's tail call to writeBootstrapOutputs —
// so completing provision is sufficient to drive the flow to the final
// meta state.
func TestWriteBootstrapOutputs_ExpansionPreservesExistingFields(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Seed the existing dev-mode meta — simulates a service the user has been
	// running for a while with a confirmed push-git strategy.
	existing := &ServiceMeta{
		Hostname:          "appdev",
		Mode:              PlanModeDev,
		BootstrapSession:  "original-sess",
		BootstrappedAt:    "2026-01-15",
		DeployStrategy:    StrategyPushGit,
		StrategyConfirmed: true,
		FirstDeployedAt:   "2026-01-16T10:00:00Z",
	}
	if err := WriteServiceMeta(dir, existing); err != nil {
		t.Fatalf("seed WriteServiceMeta: %v", err)
	}

	// Now run a bootstrap that upgrades appdev to standard.
	eng := NewEngine(dir, EnvContainer, nil)
	if _, err := eng.BootstrapStart("proj-1", "expand appdev to standard"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}
	if _, err := eng.BootstrapCompletePlan([]BootstrapTarget{{
		Runtime: RuntimeTarget{
			DevHostname:   "appdev",
			Type:          "nodejs@22",
			IsExisting:    true,
			BootstrapMode: PlanModeStandard,
			ExplicitStage: "appstage",
		},
	}}, nil, nil); err != nil {
		t.Fatalf("BootstrapCompletePlan: %v", err)
	}
	// Fast-path auto-skips close once provision completes on an all-existing
	// plan; writeBootstrapOutputs fires from the provision tail.
	if _, err := eng.BootstrapComplete(context.Background(), "provision", "Provisioned new stage", nil); err != nil {
		t.Fatalf("BootstrapComplete(provision): %v", err)
	}

	got, err := ReadServiceMeta(dir, "appdev")
	if err != nil {
		t.Fatalf("ReadServiceMeta: %v", err)
	}
	if got == nil {
		t.Fatal("expected meta after expansion")
	}

	// Upgrade contribution:
	if got.Mode != PlanModeStandard {
		t.Errorf("Mode: got %q, want %q (upgrade)", got.Mode, PlanModeStandard)
	}
	if got.StageHostname != "appstage" {
		t.Errorf("StageHostname: got %q, want appstage (upgrade)", got.StageHostname)
	}

	// Preserved fields:
	if got.BootstrappedAt != existing.BootstrappedAt {
		t.Errorf("BootstrappedAt: got %q, want %q (must be preserved)", got.BootstrappedAt, existing.BootstrappedAt)
	}
	if got.DeployStrategy != existing.DeployStrategy {
		t.Errorf("DeployStrategy: got %q, want %q (must be preserved)", got.DeployStrategy, existing.DeployStrategy)
	}
	if !got.StrategyConfirmed {
		t.Error("StrategyConfirmed lost — must be preserved through expansion")
	}
	if got.FirstDeployedAt != existing.FirstDeployedAt {
		t.Errorf("FirstDeployedAt: got %q, want %q (must be preserved)", got.FirstDeployedAt, existing.FirstDeployedAt)
	}
}

// TestWriteProvisionMetas_ExpansionPreservesExistingFields covers the
// intermediate provision write. If bootstrap crashes between provision and
// close, the partial meta must not silently revert the user's strategy
// choice. Same merge rules as writeBootstrapOutputs, but the emitted meta
// is partial (IsComplete remains true because we preserve BootstrappedAt
// from the source — which also means the close step's final write finds an
// IsComplete meta and merges again idempotently).
func TestWriteProvisionMetas_ExpansionPreservesExistingFields(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	existing := &ServiceMeta{
		Hostname:          "appdev",
		Mode:              PlanModeDev,
		BootstrapSession:  "earlier",
		BootstrappedAt:    "2026-02-01",
		DeployStrategy:    StrategyPushDev,
		StrategyConfirmed: true,
	}
	if err := WriteServiceMeta(dir, existing); err != nil {
		t.Fatalf("seed: %v", err)
	}

	eng := NewEngine(dir, EnvContainer, nil)
	if _, err := eng.BootstrapStart("proj-1", "expansion crash test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}
	if _, err := eng.BootstrapCompletePlan([]BootstrapTarget{{
		Runtime: RuntimeTarget{
			DevHostname:   "appdev",
			Type:          "nodejs@22",
			IsExisting:    true,
			BootstrapMode: PlanModeStandard,
			ExplicitStage: "appstage",
		},
	}}, nil, nil); err != nil {
		t.Fatalf("BootstrapCompletePlan: %v", err)
	}
	// Stop after provision — simulates crash before close.
	if _, err := eng.BootstrapComplete(context.Background(), "provision", "Provisioned new stage", nil); err != nil {
		t.Fatalf("BootstrapComplete(provision): %v", err)
	}

	got, err := ReadServiceMeta(dir, "appdev")
	if err != nil {
		t.Fatalf("ReadServiceMeta: %v", err)
	}
	if got == nil {
		t.Fatal("expected partial meta after provision")
	}
	// Upgrade contribution already visible at provision time.
	if got.Mode != PlanModeStandard {
		t.Errorf("Mode: got %q, want %q at provision", got.Mode, PlanModeStandard)
	}
	if got.StageHostname != "appstage" {
		t.Errorf("StageHostname: got %q, want appstage at provision", got.StageHostname)
	}
	// User-authored fields preserved across provision write too.
	if got.DeployStrategy != StrategyPushDev {
		t.Errorf("DeployStrategy: got %q, want push-dev (preserved through provision)", got.DeployStrategy)
	}
	if !got.StrategyConfirmed {
		t.Error("StrategyConfirmed lost at provision write")
	}
	if got.BootstrappedAt != existing.BootstrappedAt {
		t.Errorf("BootstrappedAt: got %q, want %q at provision", got.BootstrappedAt, existing.BootstrappedAt)
	}
}

// TestMergeExistingMeta pins the helper contract: upgrade fields (Mode,
// StageHostname) stay as set on the new meta; preserved fields come from
// the existing meta. Unit test so regressions in the helper surface here
// instead of only in the integration paths above.
func TestMergeExistingMeta(t *testing.T) {
	t.Parallel()

	meta := &ServiceMeta{
		Hostname:      "appdev",
		Mode:          PlanModeStandard, // upgrade
		StageHostname: "appstage",       // upgrade
	}
	existing := &ServiceMeta{
		Hostname:          "appdev",
		Mode:              PlanModeDev,
		BootstrappedAt:    "2026-01-15",
		DeployStrategy:    StrategyPushGit,
		StrategyConfirmed: true,
		FirstDeployedAt:   "2026-01-16T10:00:00Z",
	}

	mergeExistingMeta(meta, existing)

	if meta.Mode != PlanModeStandard {
		t.Errorf("Mode must stay upgrade value, got %q", meta.Mode)
	}
	if meta.StageHostname != "appstage" {
		t.Errorf("StageHostname must stay upgrade value, got %q", meta.StageHostname)
	}
	if meta.BootstrappedAt != "2026-01-15" {
		t.Errorf("BootstrappedAt not preserved: got %q", meta.BootstrappedAt)
	}
	if meta.DeployStrategy != StrategyPushGit {
		t.Errorf("DeployStrategy not preserved: got %q", meta.DeployStrategy)
	}
	if !meta.StrategyConfirmed {
		t.Error("StrategyConfirmed not preserved")
	}
	if meta.FirstDeployedAt != "2026-01-16T10:00:00Z" {
		t.Errorf("FirstDeployedAt not preserved: got %q", meta.FirstDeployedAt)
	}
}
