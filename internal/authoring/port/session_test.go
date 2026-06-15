package port

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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
	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	ps := NewPortSession("proj-1", "container", "port strapi", plan, now)

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
	if got.Intent != "port strapi" || got.ProjectID != "proj-1" || got.Environment != "container" {
		t.Errorf("session metadata not round-tripped: %+v", got)
	}
	if got.CreatedAt != "2026-06-12T10:00:00Z" {
		t.Errorf("CreatedAt: got %q", got.CreatedAt)
	}
	if got.StartTime != processInstanceToken {
		t.Errorf("StartTime identity: got %q want the current process token", got.StartTime)
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

// TestPortSession_Load_RecycledPID_ReturnsNil pins the (pid,startTime)
// staleness guard: a port file keyed by OUR PID but recorded by a different
// process instance (different StartTime token) is a dead predecessor's
// session and must read as absent. An empty StartTime trusts the bare PID.
func TestPortSession_Load_RecycledPID_ReturnsNil(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	ps := NewPortSession("proj-1", "local", "port ghost", PortPlan{Target: "ghost"}, now)
	ps.StartTime = "some-dead-predecessor-token"
	if err := SavePortSession(dir, ps); err != nil {
		t.Fatalf("SavePortSession: %v", err)
	}

	got, err := LoadPortSession(dir, os.Getpid())
	if err != nil {
		t.Fatalf("LoadPortSession: %v", err)
	}
	if got != nil {
		t.Fatalf("recycled-PID session must read as absent, got %+v", got)
	}

	// Empty StartTime (legacy file) trusts the bare PID.
	ps.StartTime = ""
	if err := SavePortSession(dir, ps); err != nil {
		t.Fatalf("SavePortSession (legacy): %v", err)
	}
	got, err = LoadPortSession(dir, os.Getpid())
	if err != nil {
		t.Fatalf("LoadPortSession (legacy): %v", err)
	}
	if got == nil {
		t.Fatal("legacy empty-StartTime session must trust the bare PID")
	}
}

func TestPortSession_Delete_Idempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	ps := NewPortSession("proj-1", "local", "port ghost", PortPlan{Target: "ghost"}, now)
	if err := SavePortSession(dir, ps); err != nil {
		t.Fatalf("SavePortSession: %v", err)
	}
	if err := DeletePortSession(dir, ps.PID); err != nil {
		t.Fatalf("DeletePortSession: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "port")); err != nil {
		t.Fatalf("port state dir should remain after delete: %v", err)
	}
	// Second delete is a no-op.
	if err := DeletePortSession(dir, ps.PID); err != nil {
		t.Fatalf("DeletePortSession (second): %v", err)
	}
}

// TestPortSession_StateNamespace pins the C3 boundary contract: the port
// sidecar persists under the authoring-owned `port/` namespace inside the
// state dir — never the core `work/` / `services/` namespaces.
func TestPortSession_StateNamespace(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	ps := NewPortSession("proj-1", "local", "port ghost", PortPlan{Target: "ghost"}, now)
	if err := SavePortSession(dir, ps); err != nil {
		t.Fatalf("SavePortSession: %v", err)
	}
	want := filepath.Join(dir, "port", "")
	entries, err := os.ReadDir(filepath.Join(dir, "port"))
	if err != nil {
		t.Fatalf("port namespace dir missing (%s): %v", want, err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly the per-PID session file, got %d entries", len(entries))
	}
	if others, _ := os.ReadDir(dir); len(others) != 1 {
		t.Fatalf("session must write ONLY the port/ namespace, state dir has %d entries", len(others))
	}
}

func TestPortSession_CloseOnIterationCap_Idempotent(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	ps := NewPortSession("proj-1", "local", "port ghost", PortPlan{Target: "ghost"}, now)
	ps.CloseOnIterationCap(now)
	if ps.CloseReason != closeReasonIterationCap || ps.ClosedAt == "" {
		t.Fatalf("cap close not stamped: %+v", ps)
	}
	stamped := ps.ClosedAt
	ps.CloseOnIterationCap(now.Add(time.Hour))
	if ps.ClosedAt != stamped {
		t.Fatalf("cap close must be idempotent: %q -> %q", stamped, ps.ClosedAt)
	}
}
