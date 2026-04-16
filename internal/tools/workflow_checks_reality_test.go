package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReality_FilePathClaims_Pass covers the happy path: every file path
// named in a gotcha or IG item exists somewhere in the codebase.
func TestReality_FilePathClaims_Pass(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "src", "main.ts"), "export const x = 1;\n")
	mustWrite(t, filepath.Join(dir, "src", "migrate.ts"), "export async function migrate() {}\n")
	mustWrite(t, filepath.Join(dir, "vite.config.ts"), "export default {};\n")

	readme := `## Knowledge Base
<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
### Gotchas
- ` + "**`src/main.ts`" + ` binds 0.0.0.0** — keep the bind explicit.

  ` + "```typescript\nimport { something } from './src/main.ts';\n```" + `

- **Vite host gate** — see ` + "`vite.config.ts`" + `:

  ` + "```typescript\nexport default {};\n```" + `

<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`
	checks := checkContentReality(dir, "apidev", readme, "")
	if len(checks) != 1 {
		t.Fatalf("expected 1 check, got %d: %+v", len(checks), checks)
	}
	if checks[0].Status != statusPass {
		t.Fatalf("expected pass, got %s — %s", checks[0].Status, checks[0].Detail)
	}
}

// TestReality_FilePathClaims_Fail_MissingFile is the v20 appdev gotcha #1
// case: gotcha cites `_nginx.json` as a fix, but the codebase doesn't ship
// it. Must fail because the gotcha reads as documenting shipped behavior.
func TestReality_FilePathClaims_Fail_MissingFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "vite.config.ts"), "export default {};\n")

	readme := `## Knowledge Base
<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
### Gotchas
- **200 OK with text/html on /api/* in production** — Nginx returns index.html for unknown paths. The fix is to ensure the ` + "`_nginx.json`" + ` fallback excludes /api so they return 404 instead.

  ` + "```json\n{\n  \"locations\": [{\"path\": \"/api\", \"proxy_pass\": \"http://api:3000\"}]\n}\n```" + `

<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`
	checks := checkContentReality(dir, "appdev", readme, "")
	if len(checks) != 1 {
		t.Fatalf("expected 1 check, got %d", len(checks))
	}
	if checks[0].Status != statusFail {
		t.Fatalf("expected fail, got %s — %s", checks[0].Status, checks[0].Detail)
	}
	if !strings.Contains(checks[0].Detail, "_nginx.json") {
		t.Fatalf("detail must name the missing file: %s", checks[0].Detail)
	}
}

// TestReality_FilePathClaims_Pass_Advisory frames the same missing file as
// "Pattern to add if you proxy /api at Nginx" — must pass because the
// gotcha is now framed as an alternative the reader can adopt.
func TestReality_FilePathClaims_Pass_Advisory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "vite.config.ts"), "export default {};\n")

	readme := `## Knowledge Base
<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
### Gotchas
- **200 OK with text/html on /api/* in production** — Nginx returns index.html for unknown paths. Pattern to add if you proxy ` + "`/api`" + ` from the static container instead of calling the API directly: ship a ` + "`_nginx.json`" + ` that excludes /api from the SPA fallback.

  ` + "```json\n{\n  \"locations\": [{\"path\": \"/api\", \"proxy_pass\": \"http://api:3000\"}]\n}\n```" + `

<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`
	checks := checkContentReality(dir, "appdev", readme, "")
	if checks[0].Status != statusPass {
		t.Fatalf("advisory framing must pass; got %s — %s", checks[0].Status, checks[0].Detail)
	}
}

// TestReality_FilePathClaims_IgnoresZeropsYaml — `zerops.yaml` itself is a
// universal reference and shouldn't be flagged when it exists, which it
// always does in a recipe codebase.
func TestReality_FilePathClaims_IgnoresZeropsYaml(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "zerops.yaml"), "zerops: []\n")

	readme := `## IG
<!-- #ZEROPS_EXTRACT_START:integration-guide# -->
### 1. Adding ` + "`zerops.yaml`" + `
Place ` + "`zerops.yaml`" + ` at the repo root.
` + "```yaml\nzerops: []\n```" + `
<!-- #ZEROPS_EXTRACT_END:integration-guide# -->
`
	checks := checkContentReality(dir, "apidev", readme, "")
	if checks[0].Status != statusPass {
		t.Fatalf("zerops.yaml reference must pass when shipped; got %s — %s", checks[0].Status, checks[0].Detail)
	}
}

// TestReality_DeclaredSymbol_Fail is the v20 watchdog case: gotcha
// imperatively says "Implement an internal watchdog" with a setInterval
// code block declaring `lastActivity` and `setInterval` watchdog logic,
// but no such symbol appears in src/. Must fail — the gotcha is decorative,
// not load-bearing.
func TestReality_DeclaredSymbol_Fail(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "src", "main.ts"), "export async function bootstrap() {}\n")
	mustWrite(t, filepath.Join(dir, "src", "jobs", "jobs.service.ts"), "export class JobsService {}\n")

	readme := `## KB
<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
### Gotchas
- **Workers have no health check — a hung process runs forever** — Implement an internal watchdog that exits the process if no message is processed within a threshold:

  ` + "```typescript\nlet lastActivity = Date.now();\nfunction onMessage() { lastActivity = Date.now(); }\nsetInterval(() => {\n  if (Date.now() - lastActivity > 5 * 60 * 1000) process.exit(1);\n}, 60_000);\n```" + `

<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`
	checks := checkContentReality(dir, "workerdev", readme, "")
	if checks[0].Status != statusFail {
		t.Fatalf("expected fail — gotcha is declarative but watchdog symbol not in code; got %s — %s", checks[0].Status, checks[0].Detail)
	}
}

// TestReality_DeclaredSymbol_Pass_Implemented same shape but the
// `lastActivity` symbol does appear in src — gotcha documents shipped behavior.
func TestReality_DeclaredSymbol_Pass_Implemented(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "src", "watchdog.ts"), "let lastActivity = Date.now();\nexport function tick() { lastActivity = Date.now(); }\n")

	readme := `## KB
<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
### Gotchas
- **Workers have no health check** — Implement an internal watchdog (ships in ` + "`src/watchdog.ts`" + `):

  ` + "```typescript\nlet lastActivity = Date.now();\n```" + `

<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`
	checks := checkContentReality(dir, "workerdev", readme, "")
	if checks[0].Status != statusPass {
		t.Fatalf("expected pass — symbol exists in src; got %s — %s", checks[0].Status, checks[0].Detail)
	}
}

// TestReality_DeclaredSymbol_Pass_Advisory frames the watchdog as
// "Pattern to add if your worker has long-running handlers" — must pass.
func TestReality_DeclaredSymbol_Pass_Advisory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "src", "main.ts"), "export async function bootstrap() {}\n")

	readme := `## KB
<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
### Gotchas
- **Workers have no health check** — Pattern to add if your worker has long-running handlers: an internal watchdog timer that exits if idle.

  ` + "```typescript\nlet lastActivity = Date.now();\nsetInterval(() => process.exit(1), 60_000);\n```" + `

<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`
	checks := checkContentReality(dir, "workerdev", readme, "")
	if checks[0].Status != statusPass {
		t.Fatalf("advisory framing must pass; got %s — %s", checks[0].Status, checks[0].Detail)
	}
}

// TestReality_ScansClaude scans CLAUDE.md too — its commands cite
// file paths that must exist on the codebase.
func TestReality_ScansClaude_Fail(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "src", "seed.ts"), "export async function seed() {}\n")
	// no migrate.ts shipped

	claude := `## Migrations
Run ` + "`npx ts-node src/migrate.ts`" + ` then ` + "`npx ts-node src/seed.ts`" + `.
` + "```bash\nnpx ts-node src/migrate.ts\n```" + `
`
	checks := checkContentReality(dir, "apidev", "", claude)
	if checks[0].Status != statusFail {
		t.Fatalf("expected fail — CLAUDE.md cites src/migrate.ts but file missing; got %s — %s", checks[0].Status, checks[0].Detail)
	}
}

// TestReality_NoCodebase no-ops gracefully — defensive against test
// configurations passing an empty codebaseDir.
func TestReality_NoCodebase_NoOp(t *testing.T) {
	t.Parallel()
	checks := checkContentReality("", "apidev", "irrelevant content", "")
	if len(checks) != 0 {
		t.Fatalf("empty codebaseDir should no-op; got %d checks", len(checks))
	}
}

// TestReality_EmptyContent no-ops gracefully — when neither README nor
// CLAUDE.md has content, there's nothing to check.
func TestReality_EmptyContent_NoOp(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	checks := checkContentReality(dir, "apidev", "", "")
	if len(checks) != 0 {
		t.Fatalf("empty content should no-op; got %d checks", len(checks))
	}
}

// TestReality_IgnoresStandardZeropsRefs — references to `_nginx.json`,
// `package.json`, `tsconfig.json`, `node_modules`, `dist/`, `.env`, etc.
// are NOT auto-skipped (the whole point of the check is to catch
// `_nginx.json` claims when no such file ships). But `zerops.yaml`
// IS skipped because it's the workflow's own configuration file —
// always present, never the shape of a "claim about the codebase".
func TestReality_IgnoresZeropsYamlOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// neither package.json nor _nginx.json shipped
	mustWrite(t, filepath.Join(dir, "zerops.yaml"), "zerops: []\n")

	readme := `## KB
<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
### Gotchas
- **Need to ship _nginx.json** — see ` + "`_nginx.json`" + `:
  ` + "```json\n{}\n```" + `
<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`
	checks := checkContentReality(dir, "appdev", readme, "")
	if checks[0].Status != statusFail {
		t.Fatalf("_nginx.json claim must fail when not shipped; got %s — %s", checks[0].Status, checks[0].Detail)
	}
}

// mustWrite is a small helper for fixture setup.
func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
