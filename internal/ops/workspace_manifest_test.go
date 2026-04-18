package ops

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestWorkspaceManifest_ReadMissingReturnsSkeleton(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "zcp-workspace-session-1.json")

	m, err := ReadWorkspaceManifest(path, "session-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m == nil {
		t.Fatal("expected skeleton, got nil")
	}
	if m.SessionID != "session-1" {
		t.Errorf("SessionID = %q, want session-1", m.SessionID)
	}
	if m.Codebases == nil {
		t.Error("Codebases map should be initialized, got nil")
	}
	if m.LastUpdated == "" {
		t.Error("LastUpdated should be populated on skeleton")
	}
}

func TestWorkspaceManifest_WriteThenRead(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")

	original := &WorkspaceManifest{
		SessionID: "s1",
		PlanSlug:  "nestjs-showcase",
		Codebases: map[string]*CodebaseInfo{
			"apidev": {
				Framework: "NestJS 11",
				Runtime:   "nodejs@22",
				SourceFiles: []SourceFile{
					{Path: "src/main.ts", Purpose: "bootstrap + listen", Exports: []string{"bootstrap"}},
				},
				ZeropsYAML: &ZeropsYAMLInfo{
					Path:                 "zerops.yaml",
					Setups:               []string{"dev", "prod"},
					ManagedServicesWired: []string{"db", "cache"},
					HasInitCommands:      true,
					ExposesHTTP:          true,
					HTTPPort:             3000,
				},
				PreFlightChecks: &PreFlightInfo{
					Passed: []string{"self-shadow", "0.0.0.0-bind"},
				},
			},
		},
		FeaturesImplemented: []FeatureRecord{
			{ID: "items-crud", Touches: []string{"apidev/src/items"}, At: "2026-04-18T11:35:00Z"},
		},
	}

	if err := WriteWorkspaceManifest(path, original); err != nil {
		t.Fatalf("write: %v", err)
	}

	loaded, err := ReadWorkspaceManifest(path, "s1")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if loaded.SessionID != "s1" || loaded.PlanSlug != "nestjs-showcase" {
		t.Errorf("top-level mismatch: %+v", loaded)
	}
	api, ok := loaded.Codebases["apidev"]
	if !ok {
		t.Fatal("apidev codebase missing from loaded manifest")
	}
	if api.Framework != "NestJS 11" {
		t.Errorf("apidev framework = %q, want NestJS 11", api.Framework)
	}
	if len(api.SourceFiles) != 1 || api.SourceFiles[0].Path != "src/main.ts" {
		t.Errorf("apidev source files wrong: %+v", api.SourceFiles)
	}
	if api.ZeropsYAML == nil || api.ZeropsYAML.HTTPPort != 3000 {
		t.Errorf("apidev zerops.yaml wrong: %+v", api.ZeropsYAML)
	}
	if len(loaded.FeaturesImplemented) != 1 {
		t.Errorf("features len = %d, want 1", len(loaded.FeaturesImplemented))
	}
}

func TestApplyWorkspaceManifestUpdate_MergesCodebases(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")

	// First update — apidev info from scaffold-subagent.
	_, err := ApplyWorkspaceManifestUpdate(path, "s1", WorkspaceManifestUpdate{
		PlanSlug: "nestjs-showcase",
		Codebases: map[string]*CodebaseInfo{
			"apidev": {Framework: "NestJS 11", Runtime: "nodejs@22"},
		},
	})
	if err != nil {
		t.Fatalf("first update: %v", err)
	}

	// Second update — appdev info from another subagent; apidev must survive.
	m, err := ApplyWorkspaceManifestUpdate(path, "s1", WorkspaceManifestUpdate{
		Codebases: map[string]*CodebaseInfo{
			"appdev": {Framework: "SvelteKit", Runtime: "nodejs@22"},
		},
	})
	if err != nil {
		t.Fatalf("second update: %v", err)
	}
	if _, ok := m.Codebases["apidev"]; !ok {
		t.Error("apidev should survive second update that only touched appdev")
	}
	if _, ok := m.Codebases["appdev"]; !ok {
		t.Error("appdev should be present after second update")
	}
	if m.PlanSlug != "nestjs-showcase" {
		t.Errorf("PlanSlug overridden: %q", m.PlanSlug)
	}
}

func TestApplyWorkspaceManifestUpdate_AppendsFeatures(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")

	_, err := ApplyWorkspaceManifestUpdate(path, "s1", WorkspaceManifestUpdate{
		FeaturesImplemented: []FeatureRecord{{ID: "items-crud"}},
	})
	if err != nil {
		t.Fatalf("first update: %v", err)
	}
	m, err := ApplyWorkspaceManifestUpdate(path, "s1", WorkspaceManifestUpdate{
		FeaturesImplemented: []FeatureRecord{{ID: "cache-demo"}, {ID: "jobs-dispatch"}},
	})
	if err != nil {
		t.Fatalf("second update: %v", err)
	}
	if len(m.FeaturesImplemented) != 3 {
		t.Errorf("features len = %d, want 3 (appended across updates)", len(m.FeaturesImplemented))
	}
	ids := []string{m.FeaturesImplemented[0].ID, m.FeaturesImplemented[1].ID, m.FeaturesImplemented[2].ID}
	want := []string{"items-crud", "cache-demo", "jobs-dispatch"}
	for i := range want {
		if ids[i] != want[i] {
			t.Errorf("feature[%d] = %q, want %q", i, ids[i], want[i])
		}
	}
}

func TestApplyWorkspaceManifestUpdate_ReplacesContracts(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")

	first := json.RawMessage(`{"group":"jobs-workers"}`)
	_, err := ApplyWorkspaceManifestUpdate(path, "s1", WorkspaceManifestUpdate{
		Contracts: &ContractInfo{
			NATSSubjects: map[string]json.RawMessage{"jobs.dispatch": first},
		},
	})
	if err != nil {
		t.Fatalf("first update: %v", err)
	}

	// Contracts updates replace whole (feature subagent emits one coherent set).
	second := json.RawMessage(`{"group":"updated"}`)
	m, err := ApplyWorkspaceManifestUpdate(path, "s1", WorkspaceManifestUpdate{
		Contracts: &ContractInfo{
			NATSSubjects: map[string]json.RawMessage{"jobs.dispatch": second},
		},
	})
	if err != nil {
		t.Fatalf("second update: %v", err)
	}
	got := string(m.Contracts.NATSSubjects["jobs.dispatch"])
	if got != `{"group":"updated"}` {
		t.Errorf("contracts subject = %q, want updated value", got)
	}
}

func TestWorkspaceManifestPath_UsesSession(t *testing.T) {
	t.Parallel()
	p := WorkspaceManifestPath("abc123")
	if !filepath.IsAbs(p) {
		t.Error("manifest path should be absolute")
	}
	if filepath.Base(p) != "zcp-workspace-abc123.json" {
		t.Errorf("unexpected filename: %s", filepath.Base(p))
	}
}
