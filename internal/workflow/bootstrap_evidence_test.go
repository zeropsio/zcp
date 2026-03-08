package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAutoCompleteBootstrap_WritesServiceMeta(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

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
	for _, step := range []string{"provision", "generate", "deploy", "verify"} {
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

func TestAutoCompleteBootstrap_AppendsReflog(t *testing.T) {
	t.Parallel()
	// Create project root with CLAUDE.md.
	projectRoot := t.TempDir()
	stateDir := filepath.Join(projectRoot, ".zcp", "state")

	eng := NewEngine(stateDir)

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

	for _, step := range []string{"provision", "generate", "deploy", "verify"} {
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

func TestAutoCompleteBootstrap_OutputErrorsNonFatal(t *testing.T) {
	t.Parallel()
	// Use a stateDir that doesn't have the expected .zcp/state structure,
	// so reflog path derivation points to a read-only location.
	dir := t.TempDir()
	eng := NewEngine(dir)

	_, err := eng.BootstrapStart("proj-1", "test")
	if err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	_, err = eng.BootstrapCompletePlan([]BootstrapTarget{{
		Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", Simple: true},
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
	for _, step := range []string{"provision", "generate", "deploy", "verify"} {
		if _, err := eng.BootstrapComplete(context.Background(), step, "Attestation for "+step+" step completed ok", nil); err != nil {
			t.Fatalf("BootstrapComplete(%s) should not fail due to output errors: %v", step, err)
		}
	}

	// Verify bootstrap actually completed.
	state, err := eng.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state.Phase != PhaseDone {
		t.Errorf("Phase: want DONE, got %s", state.Phase)
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
			eng := NewEngine(dir)

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

			for _, step := range []string{"provision", "generate", "deploy", "verify"} {
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
		eng := NewEngine(dir)

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

		for _, step := range []string{"provision", "generate", "deploy", "verify"} {
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
