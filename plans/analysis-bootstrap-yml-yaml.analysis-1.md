# Analysis: Bootstrap Flow Instructions + yml→yaml Migration — Iteration 1
**Date**: 2026-04-03
**Scope**: `internal/content/workflows/`, `internal/workflow/`, `internal/ops/`, `internal/eval/`, `internal/knowledge/`, `internal/tools/`, `internal/sync/`, `e2e/`
**Agents**: kb (zerops-knowledge), primary-analyst (Explore), adversarial (Explore)
**Complexity**: Deep (ultrathink, 4 agents)
**Task**: Two-part: (1) Add example tool calls to bootstrap guidance; (2) Migrate `.yml` → `.yaml` default with fallback

---

## Summary

The bootstrap workflow guidance tells the LLM *what* to do but never shows *how* to call tools — the LLM must guess parameter names, which caused `yamlPath` instead of `filePath`. Example calls exist deeper in bootstrap.md (lines 472, 535) but not in the provision rules section where the LLM first encounters the import instruction.

The `.yml` → `.yaml` migration is well-motivated: official Zerops docs already use `zerops.yaml`, zcli defaults to `zerops.yaml`, and ZCP recipes already use `zerops.yaml`. However, ZCP's bootstrap guidance, knowledge themes, eval parser, deploy guidance, and `ParseZeropsYml()` all still hardcode `.yml`. The migration touches ~15 files and requires careful coordination of coupled lookup keys (guidance.go ↔ core.md section headers, eval/prompt.go ↔ recipe section headers).

---

## Findings by Severity

### Critical

| # | Finding | Evidence | Source |
|---|---------|----------|--------|
| F1 | `eval/prompt.go:75,125` uses `strings.Contains(title, "import.yml")` / `"zerops.yml"` substring match. If recipe markdown sections migrate to `## import.yaml` / `## zerops.yaml`, eval stops parsing YAML blocks — recipe metadata extraction breaks silently. | `internal/eval/markdown.go:10` — `strings.Contains(title, sectionSubstr)` | Adversarial [MF1], self-verified |
| F2 | `guidance.go:86,113` and `recipe_guidance.go:101,118` lookup keys `"import.yml Schema"` / `"zerops.yml Schema"` are coupled to `core.md:7,75` section headers `## import.yml Schema` / `## zerops.yml Schema`. Changing one without the other = silent knowledge injection failure. No `guidance_test.go` exists to catch this. | `guidance.go:86`, `recipe_guidance.go:101`, `core.md:7,75` | Primary [B1-B2], adversarial [CH4], self-verified |

### Major

| # | Finding | Evidence | Source |
|---|---------|----------|--------|
| F3 | `ParseZeropsYml()` at `deploy_validate.go:129` hardcodes `"zerops.yml"` with no `.yaml` fallback. zcli already defaults to `zerops.yaml`. Users following official docs will create `zerops.yaml` and ZCP won't find it. | `deploy_validate.go:129` — `filepath.Join(workingDir, "zerops.yml")` | Primary [A1], adversarial [CH3] |
| F4 | `bootstrap.md` provision section (lines 122-202) has NO example tool call for `zerops_import`. Examples exist at lines 472, 535, 817 but in mode-specific subsections (simple/parent-agent), not in the generic provision rules. LLM encounters import instruction at ~line 123 before reaching examples. | `bootstrap.md:122-202` vs `bootstrap.md:472,535` | Primary [T1-R1], adversarial [CH1] |
| F5 | `deploy_guidance.go` has 7 hardcoded `zerops.yml` references in user-facing checklist strings (lines 38, 124, 127, 131, 138, 161, 303). These will be inconsistent with `.yaml` default. | `deploy_guidance.go:38,124,127,131,138,161,303` | Adversarial [MF3], self-verified |
| F6 | E2E tests (`e2e/`) have 10+ files referencing `.yml` — not assessed by primary analyst. Scope underestimate. | `e2e/bootstrap_helpers_test.go`, `e2e/deploy_test.go`, etc. | Adversarial [MF2] |

### Minor

| # | Finding | Evidence | Source |
|---|---------|----------|--------|
| F7 | Type names `ZeropsYmlDoc`, `ParseZeropsYml()` etc. are internal-only (3 callers, all in `tools/`). Rename is cheap but zero user impact. | `deploy_validate.go:96,126` | Primary [C1-C3], adversarial [CH2] — both agree: defer |
| F8 | Knowledge theme files (`model.md`, `services.md`) have mixed `.yml`/`.yaml` references. Non-critical path but inconsistent. | Knowledge grep results | Primary [D3-D4] |

---

## Recommendations (evidence-backed)

| # | Recommendation | Priority | Evidence | Effort |
|---|---------------|----------|----------|--------|
| R1 | Add `zerops_import` example call to bootstrap.md provision rules section (after validation checklist ~line 162), showing both `content=` and `filePath=` parameters | HIGH | F4 — LLM guessed `yamlPath` | S (1 file, ~10 lines) |
| R2 | Add `.yaml`-first fallback to `ParseZeropsYml()` — try `zerops.yaml`, fall back to `zerops.yml` | HIGH | F3 — zcli defaults to `.yaml` | S (1 file, ~10 lines) |
| R3 | Coordinated section header migration: change `core.md` headers AND `guidance.go` + `recipe_guidance.go` lookup keys simultaneously | HIGH | F2 — coupled pair, silent failure | M (3 files, ~10 lines) |
| R4 | Update `eval/prompt.go:75,125` to match both extensions OR migrate to `.yaml` substring | HIGH | F1 — recipe metadata extraction breaks | S (1 file, ~4 lines) |
| R5 | Migrate `bootstrap.md` — 71 `.yml` references → `.yaml` | MEDIUM | Consistency with docs/recipes | M (1 file, ~71 replacements) |
| R6 | Migrate `deploy_guidance.go` — 7 `zerops.yml` strings → `zerops.yaml` | MEDIUM | F5 — user-facing inconsistency | S (1 file, ~7 lines) |
| R7 | Update E2E and unit test fixtures to use `.yaml` (preserved by `.yml` fallback) | LOW | F6 — tests still pass via fallback | M (10+ files) |

---

## Evidence Map

| Finding | Confidence | Basis |
|---------|-----------|-------|
| F1 | VERIFIED | Read `eval/markdown.go:6-15` — `strings.Contains` substring match confirmed |
| F2 | VERIFIED | Read `guidance.go:86,113`, `recipe_guidance.go:101,118`, `core.md:7,75` — exact string coupling. Confirmed no `guidance_test.go` exists |
| F3 | VERIFIED | Read `deploy_validate.go:126-139` — single hardcoded path, no fallback |
| F4 | VERIFIED | Read `bootstrap.md:122-202` (no example) and `bootstrap.md:465-535` (examples exist in mode subsections) |
| F5 | VERIFIED | Grep `deploy_guidance.go` — 7 matches confirmed |
| F6 | LOGICAL | Adversarial grep found 10 E2E files; not individually read |
| F7 | VERIFIED | Read `deploy_validate.go:96-138` — 3 callers confirmed |
| F8 | LOGICAL | Knowledge agent reports; individual files not verified |

---

## Adversarial Challenges

| Challenge | Against | Result | Evidence |
|-----------|---------|--------|----------|
| CH1 | T1-R1 line 169 imprecise | ACCEPTED — examples exist at 472/535/817 but in mode-specific sections. Insert point should be ~line 162 (after validation checklist), not 169 | `bootstrap.md:465-535` |
| CH2 | C1-C3 type rename | CONFIRMED analyst correct — rename is cheap (3 callers) but zero user value | `deploy_validate.go` call graph |
| CH3 | A1 complexity understated | ACCEPTED — function signature takes `workingDir`, not file path. Fallback must be INSIDE the function | `deploy_validate.go:128` |
| CH4 | B1-B2 incomplete — `recipe_guidance.go` also has lookup keys | ACCEPTED — added to scope | `recipe_guidance.go:101,118` |
| MF1 | eval/prompt.go missed | ACCEPTED as F1 (CRITICAL) | `eval/markdown.go:10` |
| MF2 | E2E tests missed | ACCEPTED as F6 (MAJOR) | Grep results |
| MF3 | deploy_guidance.go missed from change map | ACCEPTED as F5 (MAJOR) | `deploy_guidance.go` grep |

---

## Recommended Implementation Sequence

### Phase 1: Non-breaking additions (no behavioral change)
**Files**: 2 | **Lines**: ~20

1. Add example tool call to `bootstrap.md` provision section (R1)
2. Add `.yaml`-first fallback to `ParseZeropsYml()` in `deploy_validate.go` (R2)
   - TDD: write test for `.yaml` lookup first, then `.yml` fallback, then both-missing error
3. All existing tests pass (fallback preserves `.yml` behavior)

### Phase 2: Coordinated knowledge migration (coupled changes — atomic commit)
**Files**: 4 | **Lines**: ~20

1. Change `core.md:7` header: `## import.yml Schema` → `## import.yaml Schema`
2. Change `core.md:75` header: `## zerops.yml Schema` → `## zerops.yaml Schema`
3. Update `guidance.go:86,113` lookup keys to match
4. Update `recipe_guidance.go:101,118` lookup keys to match
5. TDD: write `guidance_test.go` seed test verifying knowledge injection works with new headers

### Phase 3: Eval parser migration (coupled with recipe content)
**Files**: 1-2 | **Lines**: ~10

1. Update `eval/prompt.go:75` — `"import.yml"` → `"import.yaml"` (or handle both)
2. Update `eval/prompt.go:125` — `"zerops.yml"` → `"zerops.yaml"` (or handle both)
3. Verify recipe markdown files use consistent headers (they already use `zerops.yaml` per KB)

### Phase 4: Content migration (user-facing text)
**Files**: 3 | **Lines**: ~80

1. `bootstrap.md` — 71 `.yml` → `.yaml` replacements
2. `deploy_guidance.go` — 7 string replacements
3. Knowledge theme files (`core.md` content, `model.md`, `services.md`)

### Phase 5: Test fixture updates
**Files**: 10+ | **Lines**: ~50

1. Update test helpers (`writeZeropsYml` → write `zerops.yaml`)
2. Update E2E test fixtures
3. All tests must pass with `.yaml` primary, `.yml` fallback

---

## Total Scope

- **Files to modify**: ~15
- **Lines affected**: ~200-250
- **Tests to update/create**: 10+ files
- **Breaking changes**: NONE (`.yml` fallback preserves backward compatibility)
- **Critical coordination points**: 3 (guidance.go↔core.md, recipe_guidance.go↔core.md, eval/prompt.go↔recipe sections)
- **Missing test coverage**: `guidance_test.go` does not exist — should be created in Phase 2
