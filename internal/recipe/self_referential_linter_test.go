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
// `CacheService` — both recipe-internal symbols. Linter MUST refuse.
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
	// The message must name the offending token + redirect to the
	// principle level + cite the spec.
	joined := strings.Join(vs, "\n")
	for _, want := range []string{
		"cache.module.ts",
		"principle",
		"Self-referential decoration",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("violation message missing %q; got %s", want, joined)
		}
	}
}

// TestSelfReferentialNamingLinter_AllowsGenericTokens — `X-Cache`
// header NAME is platform-spec content (not recipe-internal symbol).
// Linter MUST NOT fire on generic HTTP header names.
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
- **Cross-origin cache headers** — the Vary header keys on Origin so
  upstream caches don't return a CORS-mismatched response.
<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`
	vs, err := lintSelfReferentialKB(body, codebaseDir)
	if err != nil {
		t.Fatalf("lintSelfReferentialKB: %v", err)
	}
	if len(vs) != 0 {
		t.Errorf("expected no violations on generic content; got %v", vs)
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
