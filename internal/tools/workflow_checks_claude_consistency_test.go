package tools

import (
	"strings"
	"testing"
)

// v8.80 replaces v8.78's regex-keyed-on-README-phrasing approach with
// a pattern-driven detector: a curated closed set of known-forbidden-
// in-prod code patterns. The README parameter is retained for caller
// symmetry but no longer consulted — detection fires whenever CLAUDE.md
// exhibits a hazardous pattern without a cross-reference marker.

// TestClaudeReadmeConsistency_SynchronizeTruePresent_NoMarker_Fails —
// v20 apidev case: CLAUDE.md uses synchronize without a marker.
func TestClaudeReadmeConsistency_SynchronizeTruePresent_NoMarker_Fails(t *testing.T) {
	t.Parallel()
	claude := "## Resetting Dev State\n" +
		"```bash\nnpx ts-node -e \"\n  const ds = await AppDataSource.initialize();\n  await ds.dropDatabase();\n  await ds.synchronize();\n\"\n```\n"
	checks := checkClaudeReadmeConsistency("", claude, "apidev")
	if checks[0].Status != statusFail {
		t.Fatalf("expected fail — synchronize present without marker; got %s — %s", checks[0].Status, checks[0].Detail)
	}
	if !strings.Contains(checks[0].Detail, "TypeORM synchronize") {
		t.Fatalf("detail must name 'TypeORM synchronize': %s", checks[0].Detail)
	}
}

// TestClaudeReadmeConsistency_SynchronizeTruePresent_WithMarker_Passes —
// v21 apidev pattern: CLAUDE.md uses synchronize but acknowledges the
// restriction via a cross-reference marker somewhere in the doc.
func TestClaudeReadmeConsistency_SynchronizeTruePresent_WithMarker_Passes(t *testing.T) {
	t.Parallel()
	claude := "## Resetting Dev State\n" +
		"Use synchronize for fast dev iteration (DEV ONLY — see README gotcha against synchronize in production):\n" +
		"```bash\nawait ds.synchronize();\n```\n"
	checks := checkClaudeReadmeConsistency("", claude, "apidev")
	if checks[0].Status != statusPass {
		t.Fatalf("expected pass — cross-reference marker present; got %s — %s", checks[0].Status, checks[0].Detail)
	}
}

// TestClaudeReadmeConsistency_SynchronizeYaml_Fails — the other
// synchronize shape (`synchronize: true` in config) must also fire.
func TestClaudeReadmeConsistency_SynchronizeYaml_Fails(t *testing.T) {
	t.Parallel()
	claude := "## Datasource\n```typescript\nexport default { synchronize: true, entities: [] };\n```\n"
	checks := checkClaudeReadmeConsistency("", claude, "apidev")
	if checks[0].Status != statusFail {
		t.Fatalf("expected fail — `synchronize: true` config pattern; got %s — %s", checks[0].Status, checks[0].Detail)
	}
}

// TestClaudeReadmeConsistency_DjangoSyncdb_Fails — Django's retired
// syncdb is hazardous in any environment; must fire.
func TestClaudeReadmeConsistency_DjangoSyncdb_Fails(t *testing.T) {
	t.Parallel()
	claude := "## Dev Init\nRun `python manage.py syncdb` to set up tables.\n"
	checks := checkClaudeReadmeConsistency("", claude, "webdev")
	if checks[0].Status != statusFail {
		t.Fatalf("expected fail — syncdb is removed upstream; got %s — %s", checks[0].Status, checks[0].Detail)
	}
	if !strings.Contains(checks[0].Detail, "Django") {
		t.Fatalf("detail must name Django: %s", checks[0].Detail)
	}
}

// TestClaudeReadmeConsistency_DjangoRunserver_Fails — manage.py
// runserver is the dev server and must not appear in a prod-relevant
// procedure without a marker.
func TestClaudeReadmeConsistency_DjangoRunserver_Fails(t *testing.T) {
	t.Parallel()
	claude := "## Production Start\n```bash\npython manage.py runserver 0.0.0.0:8000\n```\n"
	checks := checkClaudeReadmeConsistency("", claude, "webdev")
	if checks[0].Status != statusFail {
		t.Fatalf("expected fail — runserver is dev-only; got %s — %s", checks[0].Status, checks[0].Detail)
	}
}

// TestClaudeReadmeConsistency_RailsDbReset_Fails — destructive reset
// commands without marker.
func TestClaudeReadmeConsistency_RailsDbReset_Fails(t *testing.T) {
	t.Parallel()
	claude := "## Reset\nRun `rails db:reset` between tests.\n"
	checks := checkClaudeReadmeConsistency("", claude, "apidev")
	if checks[0].Status != statusFail {
		t.Fatalf("expected fail — rails db:reset drops data; got %s — %s", checks[0].Status, checks[0].Detail)
	}
}

// TestClaudeReadmeConsistency_DropTableCascade_Fails — mass-drop SQL.
func TestClaudeReadmeConsistency_DropTableCascade_Fails(t *testing.T) {
	t.Parallel()
	claude := "## Teardown\n```sql\nDROP TABLE all_users CASCADE;\n```\n"
	checks := checkClaudeReadmeConsistency("", claude, "apidev")
	if checks[0].Status != statusFail {
		t.Fatalf("expected fail — mass-destructive SQL; got %s — %s", checks[0].Status, checks[0].Detail)
	}
}

// TestClaudeReadmeConsistency_RmRfDbFile_Fails — literal rm -rf of a
// database file. Never belongs in a deploy path without marker.
func TestClaudeReadmeConsistency_RmRfDbFile_Fails(t *testing.T) {
	t.Parallel()
	claude := "## Reset\n```bash\nrm -rf dev.db\n```\n"
	checks := checkClaudeReadmeConsistency("", claude, "apidev")
	if checks[0].Status != statusFail {
		t.Fatalf("expected fail — rm -rf .db drops data; got %s — %s", checks[0].Status, checks[0].Detail)
	}
}

// TestClaudeReadmeConsistency_NoForbiddenPatterns_Passes — clean
// CLAUDE.md with no hazardous patterns returns an explicit pass event.
func TestClaudeReadmeConsistency_NoForbiddenPatterns_Passes(t *testing.T) {
	t.Parallel()
	claude := "## Dev Loop\nRun `npm run start:dev`.\n## Build\n`npm run build`.\n"
	checks := checkClaudeReadmeConsistency("", claude, "apidev")
	if len(checks) != 1 || checks[0].Status != statusPass {
		t.Fatalf("expected single pass; got %+v", checks)
	}
}

// TestClaudeReadmeConsistency_EmptyClaudeContent_Skips — empty body
// returns nil (no event).
func TestClaudeReadmeConsistency_EmptyClaudeContent_Skips(t *testing.T) {
	t.Parallel()
	if checks := checkClaudeReadmeConsistency("README body", "", "apidev"); len(checks) != 0 {
		t.Fatalf("expected nil on empty CLAUDE.md; got %+v", checks)
	}
}

// TestClaudeReadmeConsistency_MultipleViolations_ReportsAll — two
// distinct forbidden patterns concatenated in the detail message.
func TestClaudeReadmeConsistency_MultipleViolations_ReportsAll(t *testing.T) {
	t.Parallel()
	claude := "## Reset\n```bash\nawait ds.synchronize();\nrails db:drop\n```\n"
	checks := checkClaudeReadmeConsistency("", claude, "apidev")
	if checks[0].Status != statusFail {
		t.Fatalf("expected fail; got %+v", checks)
	}
	if !strings.Contains(checks[0].Detail, "TypeORM synchronize") {
		t.Fatalf("detail must name TypeORM synchronize: %s", checks[0].Detail)
	}
	if !strings.Contains(checks[0].Detail, "Rails") {
		t.Fatalf("detail must name Rails db:drop/reset: %s", checks[0].Detail)
	}
	if !strings.Contains(checks[0].Detail, "; ") {
		t.Fatalf("detail must join multiple conflicts with '; ': %s", checks[0].Detail)
	}
}

// TestClaudeReadmeConsistency_ShadowV20_ApiDev — representative v20
// apidev CLAUDE.md content (uses synchronize for dev reset without a
// cross-reference marker). Must fail.
func TestClaudeReadmeConsistency_ShadowV20_ApiDev(t *testing.T) {
	t.Parallel()
	claude := "## Resetting database\n" +
		"```bash\nnpx ts-node -e \"\n  import { AppDataSource } from './src/data-source';\n  const ds = await AppDataSource.initialize();\n  await ds.dropDatabase();\n  await ds.synchronize();\n\"\n```\n\n" +
		"## Starting the dev server\n```bash\nnpm run start:dev\n```\n"
	checks := checkClaudeReadmeConsistency("", claude, "apidev")
	if checks[0].Status != statusFail {
		t.Fatalf("shadow v20 content must fail (no marker): %s — %s", checks[0].Status, checks[0].Detail)
	}
}

// TestClaudeReadmeConsistency_ShadowV21_ApiDev — representative v21
// apidev CLAUDE.md content with the expected cross-reference marker
// (`DEV ONLY — see README gotcha against synchronize in production`).
// Must pass.
func TestClaudeReadmeConsistency_ShadowV21_ApiDev(t *testing.T) {
	t.Parallel()
	claude := "## Resetting database (DEV ONLY — see README gotcha against synchronize in production)\n" +
		"```bash\nnpx ts-node -e \"\n  const ds = await AppDataSource.initialize();\n  await ds.dropDatabase();\n  await ds.synchronize();\n\"\n```\n"
	checks := checkClaudeReadmeConsistency("", claude, "apidev")
	if checks[0].Status != statusPass {
		t.Fatalf("shadow v21 content with marker must pass: %s — %s", checks[0].Status, checks[0].Detail)
	}
}
