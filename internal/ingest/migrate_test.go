package ingest

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/fstest"
)

// fakeMigrationConn is a driver.Conn test double for the migration runner —
// records every Exec statement (in order), returns configurable rows from
// Select (simulating already-applied migration ids), and can be told to
// fail a specific Exec call by matching a substring.
type fakeMigrationConn struct {
	fakeCHConn // embed: reuse the unused-stub methods (ServerVersion, Query, ...)

	selectIDs []uint32
	selectErr error

	execs  []string
	failOn string // substring match; when non-empty, the matching Exec call fails
}

func (c *fakeMigrationConn) Select(_ context.Context, dest any, _ string, _ ...any) error {
	if c.selectErr != nil {
		return c.selectErr
	}
	ids, ok := dest.(*[]uint32)
	if !ok {
		return errors.New("fakeMigrationConn.Select: dest is not *[]uint32")
	}
	*ids = c.selectIDs
	return nil
}

func (c *fakeMigrationConn) Exec(_ context.Context, query string, _ ...any) error {
	c.execs = append(c.execs, query)
	if c.failOn != "" && strings.Contains(query, c.failOn) {
		return errors.New("simulated exec failure")
	}
	return nil
}

func mustLoadMigrationsFrom(t *testing.T, fsys fstest.MapFS) []migration {
	t.Helper()
	ms, err := loadMigrationsFrom(fsys)
	if err != nil {
		t.Fatalf("loadMigrationsFrom: %v", err)
	}
	return ms
}

func TestLoadMigrations_RealEmbeddedDirParsesBaseline(t *testing.T) {
	t.Parallel()

	ms, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	if len(ms) == 0 {
		t.Fatal("want at least the baseline migration, got none")
	}
	if ms[0].id != 1 {
		t.Errorf("first migration id = %d, want 1 (baseline)", ms[0].id)
	}
	if !strings.Contains(ms[0].name, "baseline") {
		t.Errorf("first migration name = %q, want it to mention baseline", ms[0].name)
	}
}

// TestLoadMigrations_RealEmbeddedDirIncludesErrorSubcode pins the S4
// migration (docs/spec-telemetry.md §4.2/§8, telemetry-production-readiness
// plan S4): the embedded migration set carries 0002_add_error_subcode.sql
// with an idempotent ADD COLUMN statement against __DB__.events.
func TestLoadMigrations_RealEmbeddedDirIncludesErrorSubcode(t *testing.T) {
	t.Parallel()

	ms, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	var found *migration
	for i := range ms {
		if ms[i].id == 2 {
			found = &ms[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected migration id 2 (add error_subcode), got: %+v", ms)
	}
	if !strings.Contains(found.name, "error_subcode") {
		t.Errorf("migration 2 name = %q, want it to mention error_subcode", found.name)
	}
	if len(found.stmts) != 1 {
		t.Fatalf("migration 2 has %d statements, want 1: %v", len(found.stmts), found.stmts)
	}
	stmt := found.stmts[0]
	for _, want := range []string{"ALTER TABLE __DB__.events", "ADD COLUMN IF NOT EXISTS error_subcode"} {
		if !strings.Contains(stmt, want) {
			t.Errorf("migration 2 statement %q missing %q", stmt, want)
		}
	}
}

func TestLoadMigrationsFrom_OrdersByID(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"migrations/0002_second.sql": &fstest.MapFile{Data: []byte("ALTER TABLE __DB__.events ADD COLUMN IF NOT EXISTS foo String;")},
		"migrations/0001_first.sql":  &fstest.MapFile{Data: []byte("-- no-op")},
	}
	ms := mustLoadMigrationsFrom(t, fsys)
	if len(ms) != 2 {
		t.Fatalf("got %d migrations, want 2", len(ms))
	}
	if ms[0].id != 1 || ms[1].id != 2 {
		t.Errorf("migrations not ordered by id: %+v", ms)
	}
}

func TestLoadMigrationsFrom_RejectsBadFileName(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"migrations/baseline.sql": &fstest.MapFile{Data: []byte("SELECT 1;")},
	}
	if _, err := loadMigrationsFrom(fsys); err == nil {
		t.Error("want an error for a file that violates the NNNN_name.sql naming convention")
	}
}

func TestLoadMigrationsFrom_RejectsDuplicateID(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"migrations/0001_a.sql": &fstest.MapFile{Data: []byte("SELECT 1;")},
		"migrations/0001_b.sql": &fstest.MapFile{Data: []byte("SELECT 2;")},
	}
	if _, err := loadMigrationsFrom(fsys); err == nil {
		t.Error("want an error for two migration files sharing the same id")
	}
}

func TestFetchAppliedMigrationIDs_ReturnsSetFromSelect(t *testing.T) {
	t.Parallel()

	conn := &fakeMigrationConn{selectIDs: []uint32{1, 2}}
	applied, err := fetchAppliedMigrationIDs(context.Background(), conn, "telemetry")
	if err != nil {
		t.Fatalf("fetchAppliedMigrationIDs: %v", err)
	}
	if !applied[1] || !applied[2] || applied[3] {
		t.Errorf("applied = %v, want {1:true, 2:true}", applied)
	}
}

func TestFetchAppliedMigrationIDs_SelectError_Propagates(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("connection refused")
	conn := &fakeMigrationConn{selectErr: wantErr}
	if _, err := fetchAppliedMigrationIDs(context.Background(), conn, "telemetry"); !errors.Is(err, wantErr) {
		t.Errorf("error = %v, want wrapping %v", err, wantErr)
	}
}

func TestRunMigrations_SkipsAlreadyApplied(t *testing.T) {
	t.Parallel()

	conn := &fakeMigrationConn{selectIDs: []uint32{1}}
	migrations := []migration{
		{id: 1, name: "0001_baseline.sql", stmts: nil},
		{id: 2, name: "0002_add_col.sql", stmts: []string{"ALTER TABLE telemetry.events ADD COLUMN IF NOT EXISTS foo String"}},
	}
	if err := runMigrations(context.Background(), conn, "telemetry", migrations, testLogger()); err != nil {
		t.Fatalf("runMigrations: %v", err)
	}

	// Migration 1 is already applied: no DDL, no re-record. Migration 2 is
	// pending: its one DDL statement executes, then it's recorded applied.
	var ddlSeen, recordSeen bool
	for _, e := range conn.execs {
		if strings.Contains(e, "ADD COLUMN") {
			ddlSeen = true
		}
		if strings.Contains(e, "INSERT INTO telemetry.schema_migrations") && strings.Contains(e, "2") {
			recordSeen = true
		}
		if strings.Contains(e, "INSERT INTO telemetry.schema_migrations") && strings.HasSuffix(strings.TrimSpace(e), "(1)") {
			t.Errorf("already-applied migration 1 must not be re-recorded, got exec %q", e)
		}
	}
	if !ddlSeen {
		t.Error("pending migration's DDL statement was never executed")
	}
	if !recordSeen {
		t.Error("pending migration was never recorded as applied")
	}
}

func TestRunMigrations_ReRunIsNoop(t *testing.T) {
	t.Parallel()

	// First run: nothing applied yet.
	conn := &fakeMigrationConn{}
	migrations := []migration{
		{id: 1, name: "0001_baseline.sql", stmts: nil},
	}
	if err := runMigrations(context.Background(), conn, "telemetry", migrations, testLogger()); err != nil {
		t.Fatalf("first runMigrations: %v", err)
	}
	firstExecCount := len(conn.execs)
	if firstExecCount == 0 {
		t.Fatal("first run recorded nothing")
	}

	// Simulate the recorded row now existing (as a real restart would see
	// via SELECT), then re-run: must be a no-op (spec §8 "restart-race note
	// dies").
	conn2 := &fakeMigrationConn{selectIDs: []uint32{1}}
	if err := runMigrations(context.Background(), conn2, "telemetry", migrations, testLogger()); err != nil {
		t.Fatalf("second runMigrations: %v", err)
	}
	// Only the SELECT itself touches the connection; no Exec at all.
	if len(conn2.execs) != 0 {
		t.Errorf("re-run executed %d statements, want 0 (already applied): %v", len(conn2.execs), conn2.execs)
	}
}

func TestRunMigrations_ExecFailure_DoesNotRecordAsApplied(t *testing.T) {
	t.Parallel()

	conn := &fakeMigrationConn{failOn: "ADD COLUMN"}
	migrations := []migration{
		{id: 2, name: "0002_add_col.sql", stmts: []string{"ALTER TABLE telemetry.events ADD COLUMN IF NOT EXISTS foo String"}},
	}
	err := runMigrations(context.Background(), conn, "telemetry", migrations, testLogger())
	if err == nil {
		t.Fatal("want an error when a migration's DDL statement fails")
	}
	for _, e := range conn.execs {
		if strings.Contains(e, "INSERT INTO telemetry.schema_migrations") {
			t.Errorf("a failed migration must not be recorded as applied, got exec %q", e)
		}
	}
}

// TestRenderMigrationDB_SubstitutesDBToken pins the __DB__ token
// substitution every migration author relies on (the naming convention
// already encoded by TestLoadMigrationsFrom_OrdersByID's own fixture,
// "ALTER TABLE __DB__.events ...") — openAdminConn carries no default
// database (spec §7/§8), so a migration statement referencing a base-schema
// table must be fully qualified, exactly like renderBootstrapStatements does
// for schema.sql.
func TestRenderMigrationDB_SubstitutesDBToken(t *testing.T) {
	t.Parallel()

	in := []migration{
		{id: 2, name: "0002_add_error_subcode.sql", stmts: []string{
			"ALTER TABLE __DB__.events ADD COLUMN IF NOT EXISTS error_subcode LowCardinality(String)",
		}},
	}
	out := renderMigrationDB(in, "telemetry")
	if len(out) != 1 || len(out[0].stmts) != 1 {
		t.Fatalf("unexpected shape: %+v", out)
	}
	got := out[0].stmts[0]
	want := "ALTER TABLE telemetry.events ADD COLUMN IF NOT EXISTS error_subcode LowCardinality(String)"
	if got != want {
		t.Errorf("stmt = %q, want %q", got, want)
	}
	// id/name pass through untouched.
	if out[0].id != 2 || out[0].name != "0002_add_error_subcode.sql" {
		t.Errorf("id/name mutated: %+v", out[0])
	}
	// Input slice/statement is not mutated in place (defensive copy).
	if in[0].stmts[0] != "ALTER TABLE __DB__.events ADD COLUMN IF NOT EXISTS error_subcode LowCardinality(String)" {
		t.Errorf("input migration mutated in place: %+v", in[0])
	}
}

func TestRunMigrations_RecordFailure_PropagatesError(t *testing.T) {
	t.Parallel()

	conn := &fakeMigrationConn{failOn: "INSERT INTO telemetry.schema_migrations"}
	migrations := []migration{
		{id: 1, name: "0001_baseline.sql", stmts: nil},
	}
	if err := runMigrations(context.Background(), conn, "telemetry", migrations, testLogger()); err == nil {
		t.Error("want an error when recording a migration applied fails")
	}
}
