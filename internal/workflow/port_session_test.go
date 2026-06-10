package workflow

import (
	"testing"
)

func TestPortSession_SaveLoad_RoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	plan := PortPlan{
		Target:      "strapi",
		Acquisition: AcquireSourceBuild,
		Band:        BandEasy,
		Runtimes:    []string{"nodejs@22"},
		Dependencies: []PortDependency{
			{Declared: "postgresql", Mapping: DepMappingManaged, ManagedType: "postgresql@16"},
			{Declared: "object-storage", Mapping: DepMappingManaged, ManagedType: "object-storage"},
		},
	}
	ws := NewWorkSession("proj-1", "container", "port strapi", []string{"strapi"})
	ps := NewPortSession(ws, plan)

	if err := SavePortSession(dir, ps); err != nil {
		t.Fatalf("SavePortSession: %v", err)
	}

	got, err := LoadPortSession(dir, ps.PID)
	if err != nil {
		t.Fatalf("LoadPortSession: %v", err)
	}
	if got == nil {
		t.Fatal("LoadPortSession returned nil for a saved session")
	}
	if got.PID != ps.PID {
		t.Errorf("PID: got %d want %d", got.PID, ps.PID)
	}
	if got.Plan.Target != "strapi" || got.Plan.Acquisition != AcquireSourceBuild || got.Plan.Band != BandEasy {
		t.Errorf("Plan round-trip mismatch: %+v", got.Plan)
	}
	if len(got.Plan.Dependencies) != 2 {
		t.Fatalf("dependencies: got %d want 2", len(got.Plan.Dependencies))
	}
	if got.Plan.Dependencies[0].ManagedType != "postgresql@16" {
		t.Errorf("dep[0].ManagedType: got %q", got.Plan.Dependencies[0].ManagedType)
	}
	if got.WorkSession == nil || got.WorkSession.Intent != "port strapi" {
		t.Errorf("wrapped WorkSession not round-tripped: %+v", got.WorkSession)
	}
}

func TestPortSession_Load_NoFile_ReturnsNil(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	got, err := LoadPortSession(dir, 999999)
	if err != nil {
		t.Fatalf("LoadPortSession on missing file: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for missing session, got %+v", got)
	}
}
