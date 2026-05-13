package recipe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// self_referential_linter_test.go — Run-46 Item 7 substrate pin.
//
// Run-45 forensic — three apidev KB bullets violated spec
// §"Self-referential decoration prohibition" but 0/3 caught by
// run-45 refinement-2:
//   - apidev KB #5: backticked recipe-stamped headers `X-Cache`,
//     `X-Cache-Elapsed-Ms`, `X-Cache-Key` + "cache demo" recipe-internal
//     feature name
//   - apidev KB #6: `cache.module.ts`, `CacheService`, `CacheController`,
//     `REDIS_CLIENT`, `cache.tokens.ts` — class/file names from the
//     codebase's src/ tree
//   - apidev KB #7: `SearchIndexer`, `ItemsService.onModuleInit`,
//     "Search card", "Items card" — recipe-internal helper names
//
// Item 7 closes the loop with a structural validator at record-fragment
// for codebase KB fragments — when the bullet body backticks a token
// that resolves to a recipe-internal symbol (codebase src/ filename,
// class export, *.module.ts / *.service.ts / *.controller.ts pattern),
// refuse with a redirect message.

// TestSelfReferentialNamingLinter_RecipeSymbolsRefuse — run-45 apidev
// KB #6 body as failing fixture. Bullet backticks `cache.module.ts` +
// `CacheService` + `REDIS_CLIENT` — all recipe-internal symbols. The
// fixture stages `cache.tokens.ts` exporting `REDIS_CLIENT` so the
// G3-followup src-export parser has data to consume; without that
// parser, leaf-module symbols like `REDIS_CLIENT` slip past the
// filename-prefix cross-match. Linter MUST refuse and the refusal
// message MUST name both the filename hit (`cache.module.ts`) AND the
// export-parser hit (`REDIS_CLIENT`).
func TestSelfReferentialNamingLinter_RecipeSymbolsRefuse(t *testing.T) {
	t.Parallel()
	codebaseDir := t.TempDir()
	// Stage a minimal src/ tree that names the recipe-internal symbols.
	if err := os.MkdirAll(filepath.Join(codebaseDir, "src", "cache"), 0o755); err != nil {
		t.Fatalf("mkdir src/cache: %v", err)
	}
	if err := os.WriteFile(filepath.Join(codebaseDir, "src", "cache", "cache.module.ts"),
		[]byte("export class CacheService {}\nexport class CacheController {}\n"),
		0o644); err != nil {
		t.Fatalf("write cache.module.ts: %v", err)
	}
	// G3-followup — `cache.tokens.ts` exports `REDIS_CLIENT`. The token
	// is declared on a leaf module; without export-parsing the linter
	// would miss it.
	if err := os.WriteFile(filepath.Join(codebaseDir, "src", "cache", "cache.tokens.ts"),
		[]byte("export const REDIS_CLIENT = Symbol('redis-client');\n"),
		0o644); err != nil {
		t.Fatalf("write cache.tokens.ts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(codebaseDir, "package.json"),
		[]byte(`{"name":"api","dependencies":{}}`),
		0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}

	body := `<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
- **NestJS module wiring** — register the Redis token in ` + "`cache.module.ts`" + ` so
  ` + "`CacheService`" + ` can inject it via ` + "`REDIS_CLIENT`" + `.
<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`
	vs, err := lintSelfReferentialKB(body, codebaseDir)
	if err != nil {
		t.Fatalf("lintSelfReferentialKB: %v", err)
	}
	if len(vs) == 0 {
		t.Fatal("expected self-referential-naming refusal; got no violations")
	}
	// G6-followup — the message must name BOTH the filename hit AND the
	// exported-symbol hit so a regression in either detection path
	// surfaces in the test instead of being silently masked by the
	// other.
	joined := strings.Join(vs, "\n")
	for _, want := range []string{
		"cache.module.ts",
		"REDIS_CLIENT",
		"principle",
		"Self-referential decoration",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("violation message missing %q; got %s", want, joined)
		}
	}
}

// TestSelfReferentialNamingLinter_ExportedClassRefuses — Run-46 Item 7
// G3-followup. A class exported from a non-eponymous file (cache class
// declared in `cache.service.ts` but the published symbol is
// `CacheService`) must be flagged when backticked. The filename
// `cache.service.ts` matches `cache.service` kebab; the export parser
// also picks up the exported identifier directly. Either path catches
// the class — the test pins both reach.
func TestSelfReferentialNamingLinter_ExportedClassRefuses(t *testing.T) {
	t.Parallel()
	codebaseDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(codebaseDir, "src", "cache"), 0o755); err != nil {
		t.Fatalf("mkdir src/cache: %v", err)
	}
	if err := os.WriteFile(filepath.Join(codebaseDir, "src", "cache", "cache.service.ts"),
		[]byte("export class CacheService {\n  resolve() {}\n}\n"),
		0o644); err != nil {
		t.Fatalf("write cache.service.ts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(codebaseDir, "package.json"),
		[]byte(`{"name":"api","dependencies":{}}`),
		0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
	body := `<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
- **Cache injection** — wire ` + "`CacheService`" + ` into the DI graph at
  bootstrap so request handlers can inject it.
<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`
	vs, err := lintSelfReferentialKB(body, codebaseDir)
	if err != nil {
		t.Fatalf("lintSelfReferentialKB: %v", err)
	}
	if len(vs) == 0 {
		t.Fatal("expected refusal on exported-class backtick; got no violations")
	}
	joined := strings.Join(vs, "\n")
	if !strings.Contains(joined, "CacheService") {
		t.Errorf("violation message must name `CacheService`; got %s", joined)
	}
}

// TestSelfReferentialNamingLinter_AllowsNpmDependency — Run-46 Item 7
// G7-followup. Backticking an npm dependency name (e.g. `redis`,
// `@nestjs/common`) is platform-spec content, NOT recipe-internal
// scaffold decoration. The linter MUST NOT fire on dependency names.
func TestSelfReferentialNamingLinter_AllowsNpmDependency(t *testing.T) {
	t.Parallel()
	codebaseDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(codebaseDir, "src"), 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}
	if err := os.WriteFile(filepath.Join(codebaseDir, "package.json"),
		[]byte(`{"name":"api","dependencies":{"redis":"^4.0.0","@nestjs/common":"^10.0.0"}}`),
		0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
	body := `<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
- **Redis driver selection** — the ` + "`redis`" + ` npm package speaks the
  binary RESP protocol; ` + "`@nestjs/common`" + ` wires the Redis client
  module into the DI graph at bootstrap.
<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`
	vs, err := lintSelfReferentialKB(body, codebaseDir)
	if err != nil {
		t.Fatalf("lintSelfReferentialKB: %v", err)
	}
	if len(vs) != 0 {
		t.Errorf("expected no violations on npm-dependency backticks; got %v", vs)
	}
}

// TestSelfReferentialNamingLinter_AllowsGenericTokens — Run-46 Item 7
// G7-followup. The original test used prose `Vary header` text and
// would have passed even if the allow-list path completely broke.
// This version backticks platform-spec content the allow-list MUST
// pass — `X-Cache` / `X-Cache-Elapsed-Ms` / `X-Cache-Key` HTTP-header
// NAMES belong to the platform's response shape, not the recipe's
// scaffold; backticking them is a legitimate teaching pattern.
func TestSelfReferentialNamingLinter_AllowsGenericTokens(t *testing.T) {
	t.Parallel()
	codebaseDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(codebaseDir, "src"), 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}
	if err := os.WriteFile(filepath.Join(codebaseDir, "package.json"),
		[]byte(`{"name":"api","dependencies":{}}`),
		0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}

	body := `<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
- **Cross-origin headers** — the ` + "`X-Cache`" + `, ` + "`X-Cache-Elapsed-Ms`" + `,
  and ` + "`X-Cache-Key`" + ` response headers are visible to ` + "`curl`" + ` but
  undefined when the SPA reads them via ` + "`fetch().headers.get(...)`" + `.
  Browsers hide non-CORS-safelisted headers from cross-origin JS unless
  the api enumerates them under ` + "`exposedHeaders`" + `.
<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`
	vs, err := lintSelfReferentialKB(body, codebaseDir)
	if err != nil {
		t.Fatalf("lintSelfReferentialKB: %v", err)
	}
	if len(vs) != 0 {
		t.Errorf("expected no violations on backticked HTTP-header allow-list tokens; got %v", vs)
	}
}

// TestSelfReferentialNamingLinter_NoCodebaseDirIsNoOp — when the
// codebase dir doesn't exist (in-memory test fixtures, missing mount),
// the linter is a silent no-op rather than a hard failure. Production
// callers pass a real path; tests without a dev mount fall through.
func TestSelfReferentialNamingLinter_NoCodebaseDirIsNoOp(t *testing.T) {
	t.Parallel()
	body := `<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
- **Self-referential symbol** — see ` + "`CacheService`" + ` in the codebase.
<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`
	vs, err := lintSelfReferentialKB(body, "/nonexistent/path")
	if err != nil {
		t.Errorf("lintSelfReferentialKB should be silent on missing codebase dir; got error %v", err)
	}
	if len(vs) != 0 {
		t.Errorf("missing-codebase-dir path should not produce violations; got %v", vs)
	}
}

// TestSelfReferentialNamingLinter_NestModulePatternFires — even without
// an on-disk match, the `*.module.ts` / `*.service.ts` / `*.controller.ts`
// pattern (the NestJS recipe-internal symbol shape) MUST fire. The
// pattern is a hard recipe-internal signal that doesn't require src/
// path resolution.
func TestSelfReferentialNamingLinter_NestModulePatternFires(t *testing.T) {
	t.Parallel()
	codebaseDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(codebaseDir, "src"), 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}
	if err := os.WriteFile(filepath.Join(codebaseDir, "package.json"),
		[]byte(`{"name":"api","dependencies":{}}`),
		0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}

	body := `<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
- **Module bootstrap order** — wire ` + "`items.module.ts`" + ` after the cache
  module to avoid circular DI.
<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`
	vs, err := lintSelfReferentialKB(body, codebaseDir)
	if err != nil {
		t.Fatalf("lintSelfReferentialKB: %v", err)
	}
	if len(vs) == 0 {
		t.Errorf("expected refusal on *.module.ts pattern even without on-disk match; got no violations")
	}
}

// TestRecordFragmentSelfReferential_RefusesKBBody — the linter is wired
// into handleRecordFragment for fragmentIds matching
// `codebase/<host>/knowledge-base`. Refusal sets r.Error and leaves the
// fragment unrecorded.
func TestRecordFragmentSelfReferential_RefusesKBBody(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	sess := NewSession("synth-showcase", "dev", log, dir, nil)
	sess.Plan = syntheticShowcasePlan()
	sess.OutputRoot = dir
	// Stage the codebase dir so the linter can read src/.
	apiDir := filepath.Join(dir, "apidev")
	if err := os.MkdirAll(filepath.Join(apiDir, "src", "cache"), 0o755); err != nil {
		t.Fatalf("mkdir apidev/src: %v", err)
	}
	if err := os.WriteFile(filepath.Join(apiDir, "src", "cache", "cache.module.ts"),
		[]byte("export class CacheService {}\n"), 0o644); err != nil {
		t.Fatalf("write cache.module.ts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(apiDir, "package.json"),
		[]byte(`{"name":"api"}`), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}

	// Use a symptom-shape stem that passes the pre-Item-7 slot-shape
	// check so the test isolates Item 7's refusal path.
	body := `<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
- **NestJS DI throws "Nest can't resolve dependencies"** — register the Redis
  token in ` + "`cache.module.ts`" + ` so ` + "`CacheService`" + ` can inject
  ` + "`REDIS_CLIENT`" + ` cleanly. See the ` + "`init-commands`" + ` guide.
<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`
	in := RecipeInput{
		Action:         "record-fragment",
		FragmentID:     "codebase/api/knowledge-base",
		Fragment:       body,
		Mode:           modeReplace,
		Classification: string(ClassIntersection),
	}
	r := handleRecordFragment(sess, in, RecipeResult{Action: "record-fragment"})
	if r.OK {
		t.Fatal("expected ok=false on self-referential KB body; got OK=true")
	}
	if !strings.Contains(r.Error, "self-referential") && !strings.Contains(r.Error, "principle") {
		t.Errorf("Error must redirect to the principle level; got %q", r.Error)
	}
}
