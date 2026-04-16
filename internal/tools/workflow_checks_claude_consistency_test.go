package tools

import (
	"strings"
	"testing"
)

// TestClaudeConsistency_Synchronize_Conflict — v20 apidev case:
// README gotcha says "`synchronize: true` must be off in production"
// while CLAUDE.md reset-state procedure calls `ds.synchronize()`. Must
// fail. The dev-loop should use the same mechanism the README endorses
// for production (migrations), not a shortcut that bypasses it.
func TestClaudeConsistency_Synchronize_Conflict(t *testing.T) {
	t.Parallel()
	readme := "<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n" +
		"### Gotchas\n" +
		"- **`synchronize: true` must be off in production TypeORM config** — auto-alters tables on every startup, deadlocks under concurrent containers via `initCommands`.\n" +
		"<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->\n"
	claude := "## Resetting Dev State\n" +
		"```bash\nnpx ts-node -e \"\n  const ds = await AppDataSource.initialize();\n  await ds.dropDatabase();\n  await ds.synchronize();\n\"\n```\n"
	checks := checkClaudeReadmeConsistency(readme, claude, "apidev")
	if checks[0].Status != statusFail {
		t.Fatalf("expected fail — synchronize forbidden in README but used in CLAUDE.md; got %s — %s", checks[0].Status, checks[0].Detail)
	}
	if !strings.Contains(checks[0].Detail, "synchronize") {
		t.Fatalf("detail must name 'synchronize': %s", checks[0].Detail)
	}
}

// TestClaudeConsistency_CrossReference_Pass — same conflict but
// CLAUDE.md explicitly cross-references the gotcha (parenthetical
// "dev only — README gotcha warns…"). Reader is informed; passes.
func TestClaudeConsistency_CrossReference_Pass(t *testing.T) {
	t.Parallel()
	readme := "<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n" +
		"### Gotchas\n" +
		"- **`synchronize: true` must be off in production TypeORM config** — auto-alters tables on every startup, deadlocks under concurrent containers.\n" +
		"<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->\n"
	claude := "## Resetting Dev State\n" +
		"Use synchronize for fast dev iteration (DEV ONLY — see README gotcha against synchronize in production):\n" +
		"```bash\nawait ds.synchronize();\n```\n"
	checks := checkClaudeReadmeConsistency(readme, claude, "apidev")
	if checks[0].Status != statusPass {
		t.Fatalf("expected pass — explicit cross-reference present; got %s — %s", checks[0].Status, checks[0].Detail)
	}
}

// TestClaudeConsistency_ProductionEquivalent_Pass — CLAUDE.md uses
// migrations (the prod-endorsed path). No conflict. Passes.
func TestClaudeConsistency_ProductionEquivalent_Pass(t *testing.T) {
	t.Parallel()
	readme := "<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n" +
		"### Gotchas\n" +
		"- **`synchronize: true` must be off in production TypeORM config** — auto-alters tables, causes deadlocks.\n" +
		"<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->\n"
	claude := "## Resetting Dev State\n" +
		"```bash\nnpx ts-node src/migrate.ts revert\nnpx ts-node src/migrate.ts up\nnpx ts-node src/seed.ts\n```\n"
	checks := checkClaudeReadmeConsistency(readme, claude, "apidev")
	if checks[0].Status != statusPass {
		t.Fatalf("expected pass — CLAUDE.md uses migrations not synchronize; got %s — %s", checks[0].Status, checks[0].Detail)
	}
}

// TestClaudeConsistency_NoForbidden_NoOp — README has no
// "must be off" / "never use" patterns → nothing to enforce; the
// check returns no-op (empty slice) rather than an explicit pass.
func TestClaudeConsistency_NoForbidden_NoOp(t *testing.T) {
	t.Parallel()
	readme := "<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n" +
		"### Gotchas\n" +
		"- **Bind 0.0.0.0 not localhost** — L7 balancer cannot route 127.0.0.1.\n" +
		"<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->\n"
	claude := "## Dev Loop\nRun `npm run start:dev`.\n"
	checks := checkClaudeReadmeConsistency(readme, claude, "apidev")
	if len(checks) != 0 {
		t.Fatalf("expected no-op when no forbidden patterns; got %+v", checks)
	}
}

// TestClaudeConsistency_MultipleForbidden — README forbids two
// identifiers; CLAUDE.md uses one of them without cross-reference.
// Must fail and name only the conflicting identifier.
func TestClaudeConsistency_MultipleForbidden(t *testing.T) {
	t.Parallel()
	readme := "<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n" +
		"### Gotchas\n" +
		"- **`synchronize: true` must be off in production** — schema corruption.\n" +
		"- **Never use `eval()` for config parsing** — RCE.\n" +
		"<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->\n"
	claude := "## Reset\n```bash\nawait ds.synchronize();\n```\n## Config\nWe parse JSON safely.\n"
	checks := checkClaudeReadmeConsistency(readme, claude, "apidev")
	if checks[0].Status != statusFail {
		t.Fatalf("expected fail; got %s — %s", checks[0].Status, checks[0].Detail)
	}
	if strings.Contains(checks[0].Detail, "eval") {
		t.Fatalf("detail must NOT name 'eval' (not used in CLAUDE.md): %s", checks[0].Detail)
	}
}

// TestClaudeConsistency_EmptyEither_NoOp — empty README or CLAUDE.md
// → no comparison possible.
func TestClaudeConsistency_EmptyEither_NoOp(t *testing.T) {
	t.Parallel()
	if checks := checkClaudeReadmeConsistency("", "anything", "apidev"); len(checks) != 0 {
		t.Fatalf("empty README → no-op; got %+v", checks)
	}
	if checks := checkClaudeReadmeConsistency("anything", "", "apidev"); len(checks) != 0 {
		t.Fatalf("empty CLAUDE.md → no-op; got %+v", checks)
	}
}
