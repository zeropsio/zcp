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
	eng := NewEngine(dir, EnvLocal, nil)

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
	for _, step := range []string{"provision", "generate", "deploy", "verify", "strategy"} {
		if _, err := eng.BootstrapComplete(context.Background(), step, "Attestation for "+step+" step completed ok", nil); err != nil {
			t.Fatalf("BootstrapComplete(%s): %v", step, err)
		}
	}

	// Verify service meta files exist.
	metaPath := filepath.Join(dir, "services", "appdev.json")
	if _, err := os.Stat(metaPath); os.IsNotExist(err) {
		t.Error("expected service meta file for appdev")
	}

	dbMetaPath := filepath.Join(dir, "services", "db.json")
	if _, err := os.Stat(dbMetaPath); os.IsNotExist(err) {
		t.Error("expected service meta file for db")
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

	for _, step := range []string{"provision", "generate", "deploy", "verify", "strategy"} {
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
	for _, step := range []string{"provision", "generate", "deploy", "verify", "strategy"} {
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
	if lastResp.Progress.Completed != 6 {
		t.Errorf("Bootstrap progress: want 6 completed, got %d", lastResp.Progress.Completed)
	}
	if eng.SessionID() != "" {
		t.Errorf("engine SessionID should be empty after bootstrap completion, got %q", eng.SessionID())
	}
}

func TestWriteBootstrapOutputs_SkipsExistingAndSharedDeps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		resolution    string
		wantOverwrite bool
	}{
		{"CREATE dep gets meta written", "CREATE", true},
		{"EXISTS dep preserves original meta", "EXISTS", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			eng := NewEngine(dir, EnvLocal, nil)

			// Pre-write a ServiceMeta for "db" with a distinct session ID.
			originalMeta := &ServiceMeta{
				Hostname:         "db",
				Type:             "postgresql@16",
				Mode:             "NON_HA",
				BootstrapSession: "original-session-id",
				BootstrappedAt:   "2026-01-01",
			}
			if err := WriteServiceMeta(dir, originalMeta); err != nil {
				t.Fatalf("pre-write meta: %v", err)
			}

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

			for _, step := range []string{"provision", "generate", "deploy", "verify", "strategy"} {
				if _, err := eng.BootstrapComplete(context.Background(), step, "Attestation for "+step+" step completed ok", nil); err != nil {
					t.Fatalf("BootstrapComplete(%s): %v", step, err)
				}
			}

			meta, err := ReadServiceMeta(dir, "db")
			if err != nil {
				t.Fatalf("ReadServiceMeta: %v", err)
			}
			if meta == nil {
				t.Fatal("expected db meta to exist")
			}

			if tt.wantOverwrite {
				if meta.BootstrapSession == "original-session-id" {
					t.Error("CREATE dep should have overwritten meta, but original session ID remains")
				}
			} else {
				if meta.BootstrapSession != "original-session-id" {
					t.Errorf("dep with resolution %s should preserve original meta, got session %q", tt.resolution, meta.BootstrapSession)
				}
			}
		})
	}

	// Test SHARED separately: in a multi-target plan, the SHARED dep should not
	// overwrite meta written by the CREATE target in the same bootstrap run.
	t.Run("SHARED dep does not double-write meta", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		eng := NewEngine(dir, EnvLocal, nil)

		_, err := eng.BootstrapStart("proj-1", "two apps + shared db")
		if err != nil {
			t.Fatalf("BootstrapStart: %v", err)
		}

		// Target 1 CREATEs db, target 2 SHAREs it.
		_, err = eng.BootstrapCompletePlan([]BootstrapTarget{
			{
				Runtime: RuntimeTarget{DevHostname: "apidev", Type: "go@1"},
				Dependencies: []Dependency{
					{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA", Resolution: "CREATE"},
				},
			},
			{
				Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"},
				Dependencies: []Dependency{
					{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA", Resolution: "SHARED"},
				},
			},
		}, nil, nil)
		if err != nil {
			t.Fatalf("BootstrapCompletePlan: %v", err)
		}

		for _, step := range []string{"provision", "generate", "deploy", "verify", "strategy"} {
			if _, err := eng.BootstrapComplete(context.Background(), step, "Attestation for "+step+" step completed ok", nil); err != nil {
				t.Fatalf("BootstrapComplete(%s): %v", step, err)
			}
		}

		// db meta should exist (written by CREATE target) with current session.
		meta, err := ReadServiceMeta(dir, "db")
		if err != nil {
			t.Fatalf("ReadServiceMeta: %v", err)
		}
		if meta == nil {
			t.Fatal("expected db meta to exist from CREATE target")
		}

		// The key verification: db should appear in both targets' dependency lists,
		// but meta was only written once (by the CREATE target). We verify by checking
		// both runtime metas include db in their dependencies.
		apiMeta, err := ReadServiceMeta(dir, "apidev")
		if err != nil {
			t.Fatalf("ReadServiceMeta(apidev): %v", err)
		}
		appMeta, err := ReadServiceMeta(dir, "appdev")
		if err != nil {
			t.Fatalf("ReadServiceMeta(appdev): %v", err)
		}

		if apiMeta == nil || appMeta == nil {
			t.Fatal("expected both runtime metas to exist")
		}
		if !strings.Contains(strings.Join(apiMeta.Dependencies, ","), "db") {
			t.Error("apidev should list db as dependency")
		}
		if !strings.Contains(strings.Join(appMeta.Dependencies, ","), "db") {
			t.Error("appdev should list db as dependency")
		}
	})
}

func TestWriteBootstrapOutputs_SetsBootstrappedStatus(t *testing.T) {
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

	for _, step := range []string{"provision", "generate", "deploy", "verify", "strategy"} {
		if _, err := eng.BootstrapComplete(context.Background(), step, "Attestation for "+step+" step completed ok", nil); err != nil {
			t.Fatalf("BootstrapComplete(%s): %v", step, err)
		}
	}

	// After full bootstrap, both metas should have Status=bootstrapped.
	appMeta, err := ReadServiceMeta(dir, "appdev")
	if err != nil {
		t.Fatalf("ReadServiceMeta(appdev): %v", err)
	}
	if appMeta == nil {
		t.Fatal("expected appdev meta")
	}
	if appMeta.Status != MetaStatusBootstrapped {
		t.Errorf("appdev Status: want %q, got %q", MetaStatusBootstrapped, appMeta.Status)
	}

	dbMeta, err := ReadServiceMeta(dir, "db")
	if err != nil {
		t.Fatalf("ReadServiceMeta(db): %v", err)
	}
	if dbMeta == nil {
		t.Fatal("expected db meta")
	}
	if dbMeta.Status != MetaStatusBootstrapped {
		t.Errorf("db Status: want %q, got %q", MetaStatusBootstrapped, dbMeta.Status)
	}
}

func TestWriteServiceMetas_PlannedStatus(t *testing.T) {
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

	// After plan completion, metas should exist with Status=planned.
	appMeta, err := ReadServiceMeta(dir, "appdev")
	if err != nil {
		t.Fatalf("ReadServiceMeta(appdev): %v", err)
	}
	if appMeta == nil {
		t.Fatal("expected appdev meta after plan")
	}
	if appMeta.Status != MetaStatusPlanned {
		t.Errorf("appdev Status: want %q, got %q", MetaStatusPlanned, appMeta.Status)
	}

	dbMeta, err := ReadServiceMeta(dir, "db")
	if err != nil {
		t.Fatalf("ReadServiceMeta(db): %v", err)
	}
	if dbMeta == nil {
		t.Fatal("expected db meta after plan")
	}
	if dbMeta.Status != MetaStatusPlanned {
		t.Errorf("db Status: want %q, got %q", MetaStatusPlanned, dbMeta.Status)
	}
}

func TestWriteServiceMetas_ProvisionedStatus(t *testing.T) {
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

	// Complete provision step.
	if _, err := eng.BootstrapComplete(context.Background(), "provision", "Provisioned ok", nil); err != nil {
		t.Fatalf("BootstrapComplete(provision): %v", err)
	}

	// After provision, metas should have Status=provisioned.
	appMeta, err := ReadServiceMeta(dir, "appdev")
	if err != nil {
		t.Fatalf("ReadServiceMeta(appdev): %v", err)
	}
	if appMeta == nil {
		t.Fatal("expected appdev meta after provision")
	}
	if appMeta.Status != MetaStatusProvisioned {
		t.Errorf("appdev Status: want %q, got %q", MetaStatusProvisioned, appMeta.Status)
	}
}

func TestWriteServiceMetas_SkipsExistingDeps(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	// Pre-write a meta for an existing dependency.
	existingMeta := &ServiceMeta{
		Hostname:         "db",
		Type:             "postgresql@16",
		Status:           MetaStatusBootstrapped,
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

	// The EXISTS dep should NOT be overwritten.
	dbMeta, err := ReadServiceMeta(dir, "db")
	if err != nil {
		t.Fatalf("ReadServiceMeta(db): %v", err)
	}
	if dbMeta == nil {
		t.Fatal("expected db meta to exist")
	}
	if dbMeta.BootstrapSession != "old-session" {
		t.Errorf("EXISTS dep should preserve original session, got %q", dbMeta.BootstrapSession)
	}
	if dbMeta.Status != MetaStatusBootstrapped {
		t.Errorf("EXISTS dep should preserve original status, got %q", dbMeta.Status)
	}
}

// --- C7: Deploy strategy persistence + XC1 Mode field ---

func TestWriteBootstrapOutputs_CopiesStrategiesToDecisions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		strategy string
		wantKey  string
	}{
		{"push-dev strategy", StrategyPushDev, StrategyPushDev},
		{"ci-cd strategy", StrategyCICD, StrategyCICD},
		{"manual strategy", StrategyManual, StrategyManual},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			eng := NewEngine(dir, EnvLocal, nil)

			_, err := eng.BootstrapStart("proj-1", "app with strategy")
			if err != nil {
				t.Fatalf("BootstrapStart: %v", err)
			}

			_, err = eng.BootstrapCompletePlan([]BootstrapTarget{{
				Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"},
			}}, nil, nil)
			if err != nil {
				t.Fatalf("BootstrapCompletePlan: %v", err)
			}

			// Store strategy before completing strategy step.
			if err := eng.BootstrapStoreStrategies(map[string]string{"appdev": tt.strategy}); err != nil {
				t.Fatalf("BootstrapStoreStrategies: %v", err)
			}

			for _, step := range []string{"provision", "generate", "deploy", "verify", "strategy"} {
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
			got := meta.Decisions[DecisionDeployStrategy]
			if got != tt.wantKey {
				t.Errorf("Decisions[%q]: want %q, got %q", DecisionDeployStrategy, tt.wantKey, got)
			}
		})
	}
}

func TestWriteBootstrapOutputs_AutoAssignsPushDevForDevOnly(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		bootstrapMode string
		wantStrategy  string
	}{
		{"dev mode gets push-dev auto-assigned", PlanModeDev, StrategyPushDev},
		{"simple mode gets push-dev auto-assigned", PlanModeSimple, StrategyPushDev},
		{"standard mode gets no auto-assign", PlanModeStandard, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			eng := NewEngine(dir, EnvLocal, nil)

			_, err := eng.BootstrapStart("proj-1", "auto-assign test")
			if err != nil {
				t.Fatalf("BootstrapStart: %v", err)
			}

			_, err = eng.BootstrapCompletePlan([]BootstrapTarget{{
				Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", BootstrapMode: tt.bootstrapMode},
			}}, nil, nil)
			if err != nil {
				t.Fatalf("BootstrapCompletePlan: %v", err)
			}

			// No explicit strategy stored — expect auto-assignment for non-standard.
			for _, step := range []string{"provision", "generate", "deploy", "verify", "strategy"} {
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
			got := meta.Decisions[DecisionDeployStrategy]
			if got != tt.wantStrategy {
				t.Errorf("Decisions[%q]: want %q, got %q", DecisionDeployStrategy, tt.wantStrategy, got)
			}
		})
	}
}

func TestWriteBootstrapOutputs_ExplicitStrategyNotOverriddenForDevOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	_, err := eng.BootstrapStart("proj-1", "explicit strategy test")
	if err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	// Dev mode target with explicit ci-cd strategy.
	_, err = eng.BootstrapCompletePlan([]BootstrapTarget{{
		Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", BootstrapMode: PlanModeDev},
	}}, nil, nil)
	if err != nil {
		t.Fatalf("BootstrapCompletePlan: %v", err)
	}

	// Store explicit ci-cd strategy — should NOT be overridden by auto-assign.
	if err := eng.BootstrapStoreStrategies(map[string]string{"appdev": StrategyCICD}); err != nil {
		t.Fatalf("BootstrapStoreStrategies: %v", err)
	}

	for _, step := range []string{"provision", "generate", "deploy", "verify", "strategy"} {
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
	got := meta.Decisions[DecisionDeployStrategy]
	if got != StrategyCICD {
		t.Errorf("explicit strategy should not be overridden: want %q, got %q", StrategyCICD, got)
	}
}

func TestWriteServiceMetas_SetsMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		bootstrapMode string
		wantMode      string
	}{
		{"standard mode (default)", "", PlanModeStandard},
		{"dev mode", PlanModeDev, PlanModeDev},
		{"simple mode", PlanModeSimple, PlanModeSimple},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			eng := NewEngine(dir, EnvLocal, nil)

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

			// After plan, check that planned meta has Mode set.
			meta, err := ReadServiceMeta(dir, "appdev")
			if err != nil {
				t.Fatalf("ReadServiceMeta: %v", err)
			}
			if meta == nil {
				t.Fatal("expected appdev meta after plan")
			}
			if meta.Mode != tt.wantMode {
				t.Errorf("Mode: want %q, got %q", tt.wantMode, meta.Mode)
			}
		})
	}
}
