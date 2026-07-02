package ingest

import (
	"regexp"
	"strings"
	"testing"
)

func TestSplitStatements_StripsCommentsAndEmpties(t *testing.T) {
	t.Parallel()
	in := "-- leading comment\nCREATE TABLE a (x Int8);\n\n-- tail comment with semicolon;\n-- another;\nSELECT 1;\n-- only comments after final\n"
	got := splitStatements(in)
	if len(got) != 2 {
		t.Fatalf("want 2 statements, got %d: %q", len(got), got)
	}
	if !strings.HasPrefix(got[0], "CREATE TABLE a") {
		t.Errorf("first statement = %q", got[0])
	}
	if got[1] != "SELECT 1" {
		t.Errorf("second statement = %q", got[1])
	}
}

func TestSplitStatements_KeepsTrailingSameLineComments(t *testing.T) {
	t.Parallel()
	in := "CREATE TABLE a (\n  x Int8, -- inline note\n  y Int8\n);"
	got := splitStatements(in)
	if len(got) != 1 {
		t.Fatalf("want 1 statement, got %d: %q", len(got), got)
	}
	if !strings.Contains(got[0], "-- inline note") {
		t.Errorf("trailing same-line comment must survive: %q", got[0])
	}
}

func TestSplitStatements_SemicolonInsideCommentLineDoesNotSplit(t *testing.T) {
	t.Parallel()
	// Regression: live-hit 2026-07-02 — a full-line comment containing a
	// semicolon must not leak its tail as a bogus statement.
	in := "-- header does X; see somewhere else for Y\nCREATE TABLE a (x Int8);\n-- another; trailing\n"
	got := splitStatements(in)
	if len(got) != 1 {
		t.Fatalf("want 1 statement, got %d: %q", len(got), got)
	}
	if !strings.HasPrefix(got[0], "CREATE TABLE a") {
		t.Errorf("statement = %q", got[0])
	}
}

func TestRenderBootstrapStatements_EveryStatementStartsWithSQLKeyword(t *testing.T) {
	t.Parallel()
	for _, cluster := range []string{"", "zerops"} {
		stmts, err := renderBootstrapStatements("telemetry", "zerops", cluster)
		if err != nil {
			t.Fatalf("cluster=%q render: %v", cluster, err)
		}
		for _, s := range stmts {
			if !strings.HasPrefix(s, "CREATE ") && !strings.HasPrefix(s, "GRANT ") {
				t.Errorf("cluster=%q: statement does not start with an SQL keyword (comment-splitting leak?): %.60q", cluster, s)
			}
		}
	}
}

func TestRenderBootstrapStatements_SingleNode(t *testing.T) {
	t.Parallel()
	stmts, err := renderBootstrapStatements("telemetry", "zerops", "")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(stmts) < 3 {
		t.Fatalf("want preamble + body, got %d statements", len(stmts))
	}
	if stmts[0] != "CREATE DATABASE IF NOT EXISTS telemetry" {
		t.Errorf("db statement = %q", stmts[0])
	}
	if stmts[1] != "GRANT SELECT, INSERT ON telemetry.* TO zerops" {
		t.Errorf("grant statement = %q", stmts[1])
	}
	all := strings.Join(stmts, "\n")
	for _, forbidden := range []string{"__DB__", "__REPLICATED__", "ON CLUSTER", "Replicated"} {
		if strings.Contains(all, forbidden) {
			t.Errorf("single-node render must not contain %q", forbidden)
		}
	}
	if !strings.Contains(all, "ENGINE = ReplacingMergeTree(ingest_version)") {
		t.Errorf("single-node events engine missing plain ReplacingMergeTree")
	}
}

func TestRenderBootstrapStatements_HACluster(t *testing.T) {
	t.Parallel()
	stmts, err := renderBootstrapStatements("telemetry", "zerops", "zerops")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if want := "CREATE DATABASE IF NOT EXISTS telemetry ON CLUSTER 'zerops' ENGINE = Replicated('/clickhouse/databases/{uuid}', '{shard}', '{replica}')"; stmts[0] != want {
		t.Errorf("db statement = %q, want %q", stmts[0], want)
	}
	if want := "GRANT ON CLUSTER 'zerops' SELECT, INSERT ON telemetry.* TO zerops"; stmts[1] != want {
		t.Errorf("grant statement = %q, want %q", stmts[1], want)
	}
	all := strings.Join(stmts, "\n")
	if strings.Contains(all, "__DB__") || strings.Contains(all, "__REPLICATED__") {
		t.Errorf("unrendered tokens remain")
	}
	if !strings.Contains(all, "ENGINE = ReplicatedReplacingMergeTree(ingest_version)") {
		t.Errorf("HA events engine missing ReplicatedReplacingMergeTree")
	}
	if !strings.Contains(all, "ENGINE = ReplicatedAggregatingMergeTree") {
		t.Errorf("HA aggregate engine missing ReplicatedAggregatingMergeTree")
	}
	// Body DDL must NOT carry ON CLUSTER — the Replicated database engine
	// propagates DDL itself; only the preamble targets the cluster.
	for _, s := range stmts[2:] {
		if strings.Contains(s, "ON CLUSTER") {
			t.Errorf("body statement carries ON CLUSTER: %q", s)
		}
	}
}

func TestRenderBootstrapStatements_RejectsBadIdentifiers(t *testing.T) {
	t.Parallel()
	cases := [][3]string{
		{"tele-metry", "zerops", ""},
		{"telemetry", "zer ops", ""},
		{"telemetry", "zerops", "zer;ops"},
		{"", "zerops", ""},
	}
	for _, c := range cases {
		if _, err := renderBootstrapStatements(c[0], c[1], c[2]); err == nil {
			t.Errorf("db=%q user=%q cluster=%q: want identifier error, got nil", c[0], c[1], c[2])
		}
	}
}

func TestRenderBootstrapStatements_IncludesSchemaMigrationsTable(t *testing.T) {
	t.Parallel()
	// The migration mechanism (spec §8, S3) needs telemetry.schema_migrations
	// to exist before applyMigrations ever queries it — it must ship as part
	// of the base bootstrap, not a migration itself (chicken-and-egg).
	stmts, err := renderBootstrapStatements("telemetry", "zerops", "")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var found bool
	for _, s := range stmts {
		if strings.Contains(s, "CREATE TABLE IF NOT EXISTS telemetry.schema_migrations") {
			found = true
			break
		}
	}
	if !found {
		t.Error("base bootstrap DDL is missing telemetry.schema_migrations")
	}
}

// addColumnPattern extracts the column name from a migration's
// "ADD COLUMN IF NOT EXISTS <col> ..." statement (spec §8 authoring
// convention) — used by TestRenderBootstrapStatements_EventsColumnsMatchInsertColumns
// to widen the base-schema lockstep check to migration-added columns too.
var addColumnPattern = regexp.MustCompile(`ADD COLUMN IF NOT EXISTS (\w+)`)

func TestRenderBootstrapStatements_EventsColumnsMatchInsertColumns(t *testing.T) {
	t.Parallel()
	stmts, err := renderBootstrapStatements("telemetry", "zerops", "")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var events string
	for _, s := range stmts {
		if strings.Contains(s, "CREATE TABLE IF NOT EXISTS telemetry.events") {
			events = s
			break
		}
	}
	if events == "" {
		t.Fatal("events CREATE TABLE not found")
	}

	// A migration can ADD COLUMN onto the base events shape (spec §8) — the
	// insertColumns lockstep now spans the base DDL PLUS every migration's
	// ADD COLUMN statements, not schema.sql alone.
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	migratedCols := map[string]bool{}
	for _, m := range migrations {
		for _, stmt := range m.stmts {
			for _, match := range addColumnPattern.FindAllStringSubmatch(stmt, -1) {
				migratedCols[match[1]] = true
			}
		}
	}

	// Every inserted column must exist in the embedded base schema DDL or a
	// migration's ADD COLUMN — pins the clickhouse.go insertColumns ↔
	// schema.sql+migrations lockstep note.
	for col := range strings.SplitSeq(insertColumns, ", ") {
		if strings.Contains(events, col) || migratedCols[col] {
			continue
		}
		t.Errorf("insertColumns column %q missing from embedded events DDL and every migration's ADD COLUMN", col)
	}
}
