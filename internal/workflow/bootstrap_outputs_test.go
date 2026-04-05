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
		Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"},
		Dependencies: []Dependency{
			{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA", Resolution: "CREATE"},
		},
	}}, nil, nil)
	if err != nil {
		t.Fatalf("BootstrapCompletePlan: %v", err)
	}

	// Complete remaining steps to trigger autoComplete.
	for _, step := range []string{"provision", "generate", "deploy", "close"} {
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
		Runtime: RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2"},
	}}, nil, nil)
	if err != nil {
		t.Fatalf("BootstrapCompletePlan: %v", err)
	}

	for _, step := range []string{"provision", "generate", "deploy", "close"} {
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
	for _, step := range []string{"provision", "generate", "deploy", "close"} {
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
	if lastResp.Progress.Completed != 5 {
		t.Errorf("Bootstrap progress: want 5 completed, got %d", lastResp.Progress.Completed)
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
				Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"},
				Dependencies: []Dependency{
					{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA", Resolution: tt.resolution},
				},
			}}, nil, nil)
			if err != nil {
				t.Fatalf("BootstrapCompletePlan: %v", err)
			}

			for _, step := range []string{"provision", "generate", "deploy", "close"} {
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
		Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"},
		Dependencies: []Dependency{
			{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA", Resolution: "EXISTS"},
		},
	}}, nil, nil)
	if err != nil {
		t.Fatalf("BootstrapCompletePlan: %v", err)
	}

	for _, step := range []string{"provision", "generate", "deploy", "close"} {
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
		Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"},
		Dependencies: []Dependency{
			{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA", Resolution: "CREATE"},
		},
	}}, nil, nil)
	if err != nil {
		t.Fatalf("BootstrapCompletePlan: %v", err)
	}

	for _, step := range []string{"provision", "generate", "deploy", "close"} {
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
		Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"},
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
		Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"},
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
		Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"},
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
		Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"},
	}}, nil, nil)
	if err != nil {
		t.Fatalf("BootstrapCompletePlan: %v", err)
	}

	for _, step := range []string{"provision", "generate", "deploy", "close"} {
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
		bootstrapMode string
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
				Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", BootstrapMode: tt.bootstrapMode},
			}}, nil, nil)
			if err != nil {
				t.Fatalf("BootstrapCompletePlan: %v", err)
			}

			// Bootstrap always writes empty strategy — user sets it later.
			for _, step := range []string{"provision", "generate", "deploy", "close"} {
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

	for _, step := range []string{"provision", "generate", "deploy", "close"} {
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
		bootstrapMode string
		wantMode      string
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
				Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", BootstrapMode: tt.bootstrapMode},
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
	if !strings.Contains(msg, "verification server") {
		t.Error("message should mention verification server")
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
					{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"}},
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

func TestWriteBootstrapOutputs_EnvironmentField(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		env     Environment
		wantEnv string
	}{
		{
			name:    "container mode sets environment=container",
			env:     EnvContainer,
			wantEnv: "container",
		},
		{
			name:    "local mode sets environment=local",
			env:     EnvLocal,
			wantEnv: "local",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			eng := NewEngine(dir, tt.env, nil)

			_, err := eng.BootstrapStart("proj-1", "test")
			if err != nil {
				t.Fatalf("BootstrapStart: %v", err)
			}
			_, err = eng.BootstrapCompletePlan([]BootstrapTarget{{
				Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"},
			}}, nil, nil)
			if err != nil {
				t.Fatalf("BootstrapCompletePlan: %v", err)
			}
			for _, step := range []string{"provision", "generate", "deploy", "close"} {
				if _, err := eng.BootstrapComplete(context.Background(), step, "Attestation for "+step+" step completed ok", nil); err != nil {
					t.Fatalf("BootstrapComplete(%s): %v", step, err)
				}
			}

			// In local mode, meta hostname = appstage (stage), not appdev.
			metaHostname := "appdev"
			if tt.env == EnvLocal {
				metaHostname = "appstage"
			}
			meta, err := ReadServiceMeta(dir, metaHostname)
			if err != nil {
				t.Fatalf("ReadServiceMeta(%s): %v", metaHostname, err)
			}
			if meta == nil {
				t.Fatalf("expected %s meta to exist", metaHostname)
			}
			if meta.Environment != tt.wantEnv {
				t.Errorf("Environment = %q, want %q", meta.Environment, tt.wantEnv)
			}
		})
	}
}

func TestWriteBootstrapOutputs_LocalMode_HostnameIsStage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	_, err := eng.BootstrapStart("proj-1", "test")
	if err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}
	_, err = eng.BootstrapCompletePlan([]BootstrapTarget{{
		Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"},
	}}, nil, nil)
	if err != nil {
		t.Fatalf("BootstrapCompletePlan: %v", err)
	}
	for _, step := range []string{"provision", "generate", "deploy", "close"} {
		if _, err := eng.BootstrapComplete(context.Background(), step, "Attestation for "+step+" step completed ok", nil); err != nil {
			t.Fatalf("BootstrapComplete(%s): %v", step, err)
		}
	}

	// In local mode, meta should be written for appstage (not appdev).
	meta, err := ReadServiceMeta(dir, "appstage")
	if err != nil {
		t.Fatalf("ReadServiceMeta(appstage): %v", err)
	}
	if meta == nil {
		t.Fatal("expected appstage meta in local mode")
	}
	if meta.Hostname != "appstage" {
		t.Errorf("hostname = %s, want appstage", meta.Hostname)
	}
	if meta.StageHostname != "" {
		t.Errorf("stageHostname = %s, want empty (local mode has no dev/stage pair)", meta.StageHostname)
	}
	if meta.Mode != PlanModeStandard {
		t.Errorf("mode = %s, want standard", meta.Mode)
	}

	// appdev should NOT have a meta file in local mode.
	devMeta, _ := ReadServiceMeta(dir, "appdev")
	if devMeta != nil {
		t.Error("appdev meta should NOT exist in local mode (dev service not created)")
	}
}

func TestWriteBootstrapOutputs_LocalMode_DefaultEmptyStrategy2(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	_, err := eng.BootstrapStart("proj-1", "test")
	if err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}
	_, err = eng.BootstrapCompletePlan([]BootstrapTarget{{
		Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"},
	}}, nil, nil)
	if err != nil {
		t.Fatalf("BootstrapCompletePlan: %v", err)
	}

	for _, step := range []string{"provision", "generate", "deploy", "close"} {
		if _, err := eng.BootstrapComplete(context.Background(), step, "Attestation for "+step+" step completed ok", nil); err != nil {
			t.Fatalf("BootstrapComplete(%s): %v", step, err)
		}
	}

	// Local mode: meta written as appstage. Strategy is empty (bootstrap default).
	meta, err := ReadServiceMeta(dir, "appstage")
	if err != nil {
		t.Fatalf("ReadServiceMeta(appstage): %v", err)
	}
	if meta == nil {
		t.Fatal("expected appstage meta")
	}
	if meta.DeployStrategy != "" {
		t.Errorf("bootstrap must write empty DeployStrategy, got %q", meta.DeployStrategy)
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
					DevHostname: "appdev",
					Type:        "nodejs@22",
					IsExisting:  tt.isExisting,
				},
			}}, nil, nil)
			if err != nil {
				t.Fatalf("BootstrapCompletePlan: %v", err)
			}

			for _, step := range []string{"provision", "generate", "deploy", "close"} {
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
			DevHostname: "appdev",
			Type:        "nodejs@22",
			IsExisting:  true,
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
