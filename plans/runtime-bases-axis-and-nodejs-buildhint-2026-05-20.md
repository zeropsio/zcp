# `runtimeBases:` axis + Node greenfield buildhint atom

**Status**: REFINED — codex review applied (2026-05-20). Ready to execute.
**Created**: 2026-05-20
**Promoted from**: `plans/backlog/npm-ci-greenfield-default-deflection.md` (surfaced 2026-05-17).
**Replaces**: rejected `plans/scaffold-composer-integrated-2026-05-20.md` (over-engineered composer redesign).

---

## 1. Context

### Problem

Across multiple flow-eval runs, agents default to `npm ci` on greenfield Node deploys. `npm ci` requires committed `package-lock.json`; fresh scaffold doesn't have one → build fails ~25s with `EUSAGE`. Failure classifier (`build:npm-ci-missing-lockfile`) recovers in one retry, but the wasted cycle recurs every Node greenfield.

Retros naming the problem:
- `eval/behavioral/runs/20260516-225809/pm-app-czech-byty-run1-replay/self-review.md` (item 1)
- `eval/behavioral/runs/20260517-044505/pm-app-czech-byty-run1-replay/self-review.md`
- `eval/behavioral/runs/20260517-045943/cadence-multiservice-build-run2-replay/self-review.md` (item 1)
- `eval/behavioral/runs/20260518-185225/api-node-postgres-classic-dev/self-review.md`
- `eval/behavioral/runs/20260520-064223/develop-loop-after-bootstrap/self-review.md`
- (~11 retros total across May 2026; 11/11 Node greenfield deploys exhibit the pattern)

### Rejected alternatives (with reasons)

| Approach | Why rejected |
|---|---|
| Single table atom (12 runtimes) | LLM noise — Python/Go projects see Node hint, Node project sees Python hint, etc. Karel: "neposílat irrelevant info" |
| Token substitution + Go map `runtimeBuildDefaults` | Solves only buildCommands field, adds drift surface vs recipes |
| Full scaffold composer (Plan v2 — over-engineered) | 600+ lines Go for problem failure classifier already recovers from. ROI doesn't justify surface. |
| Recipe Gotchas in `nodejs-hello-world.md` | 11/11 failures were classic route, never recipe path. ROI=0 for greenfield |
| Edit `develop-first-deploy-scaffold-yaml.md` with Node-only line | LLM noise — Python/Go projects see Node-specific text |

### Chosen approach

Add **`runtimeBases:` axis** to atom corpus — per-runtime-base filtering (`nodejs`, `python`, `go`, `php`, etc.) parallel to existing `runtimes:` (class-level: `dynamic`, `implicit-webserver`, `static`).

Then add **one** atom `develop-nodejs-greenfield-buildhint.md` with `runtimeBases: [nodejs]` — fires only when a Node service is in scope. Python/Go/PHP projects don't see it. Pay-as-discover for other runtimes (add atom per language when eval surfaces evidence).

This solves "noise" concern (irrelevant guidance for unrelated runtimes), respects pay-as-discover principle (CLAUDE.md: *"Don't design for hypothetical future requirements"*), and ships a structural mechanism reusable for any future runtime-base-specific friction.

---

## 2. Goal

1. Add `runtimeBases:` axis filter to atom corpus (infrastructure)
2. Add `develop-nodejs-greenfield-buildhint.md` atom — fires on `runtimeBases: [nodejs]` + `envelopeDeployStates: [never-deployed]`
3. Verify: eval `api-node-postgres-classic-dev` first-try success (no `npm ci` failure cycle); Python/Go/PHP scenarios do NOT see Node hint

### Success criteria

- `runtimeBases:` axis matches per `topology.CanonicalBareForm(svc.TypeVersion)` (e.g. `alpine/nodejs@22` → bare `nodejs@22` → base `nodejs`)
- Atom lint + tests green; new axis follows existing patterns (`runtimes:`, `modes:`, `routes:`)
- Node atom fires in Node scenarios only — verified by golden tests
- Eval `api-node-postgres-classic-dev` first-try success per structured gate:
  - exactly 1 `zerops_deploy` call per target
  - first deploy: `status="success"`
  - no `failureClassification.signals` containing `build:npm-ci-missing-lockfile`

### Non-goals

- Composer / Go-driven yaml generation (rejected)
- Other runtime atoms (python, go, php) — add later only when eval surfaces evidence per runtime
- Failure classifier changes (stays as defense-in-depth)
- Recipe path changes (recipes own their yaml)

---

## 3. Design

### 3.1 New axis `runtimeBases:`

Add field to `AxisVector` in `internal/workflow/atom.go`:

```go
type AxisVector struct {
    // ... existing fields ...
    RuntimeBases []string `json:"runtimeBases,omitempty"`
}
```

Add `runtimeBases` to `validAtomFrontmatterKeys` (same parser strict-set used today).

### 3.2 Match logic in `synthesize.go`

**Service-scoped axis** — `runtimeBases` must be added to TWO places (per codex review):

1. **`hasServiceScopedAxes`** (`internal/workflow/synthesize.go:350`) — declares that atom requires per-service matching (so `deployStates`, `mode`, etc. all satisfied by the SAME service, not OR across envelope)
2. **`serviceSatisfiesAxes`** (`internal/workflow/synthesize.go:370`) — per-service match function

Implementation:

```go
// matchesRuntimeBase returns true if the service's runtime base
// (canonical bare form before "@version") is in the allowed list.
func matchesRuntimeBase(typeVersion string, bases []string) bool {
    bare := topology.CanonicalBareForm(typeVersion)   // "alpine/nodejs@22" → "nodejs@22"
    base, _, _ := strings.Cut(bare, "@")              // "nodejs@22" → "nodejs"
    for _, want := range bases {
        if base == want {
            return true
        }
    }
    return false
}

// In hasServiceScopedAxes:
if len(atom.RuntimeBases) > 0 { return true }

// In serviceSatisfiesAxes:
if len(atom.RuntimeBases) > 0 {
    if !matchesRuntimeBase(svc.TypeVersion, atom.RuntimeBases) {
        return false
    }
}
```

**Critical**: Putting `runtimeBases` in only ONE of the two leaks the atom globally (without service conjunction) OR makes axis silently inert. Both edit points required for same-service conjunction with `deployStates`.

For atoms with `multiService: aggregate` semantic, match if ANY in-scope service satisfies ALL service-scoped axes (conjunction over fields, disjunction over services).

**PHP family clarification**: `runtimeBases: [php]` will NOT match `php-nginx@8.4` (base is `php-nginx`, not `php`). Future PHP atoms must enumerate explicitly: `runtimeBases: [php-nginx, php-apache]`. Family matching is a deliberately separate abstraction not introduced by this plan.

### 3.3 Node atom

`internal/content/atoms/develop-nodejs-greenfield-buildhint.md`:

```markdown
---
id: develop-nodejs-greenfield-buildhint
priority: 2
phases: [develop-active]
runtimeBases: [nodejs]
deployStates: [never-deployed]
multiService: aggregate
title: "Node.js greenfield — use npm install, not npm ci"
references-fields: [workflow.ServiceSnapshot.TypeVersion]
---

### Node.js greenfield — use `npm install`, not `npm ci`

Fresh Node scaffold with no committed `package-lock.json`: use `npm install`
in first-deploy `build.buildCommands`. `npm ci` fails with `EUSAGE` because
it requires the lockfile.

If a lockfile already exists (e.g. brownfield deploy through develop
scaffold), use `npm ci` for reproducibility.
```

**Key frontmatter decisions** (per codex review):

- `deployStates` (per-service) NOT `envelopeDeployStates` (per-envelope) — without this, atom fires for already-deployed Node service when a separate never-deployed Python service exists in same project
- `multiService: aggregate` explicit — documents "render once even with multiple Node services in scope"
- Atom body: 3 lines trigger-first per CLAUDE.local.md (TRIGGER + ACTION + FAILURE MODE)
- Recipe-route awareness: current envelope doesn't preserve `Bootstrap.Route` for develop-active scenarios (verified at `scenarios_fixtures_test.go:219`). Atom WILL fire for Node recipe scenarios. Body's "if lockfile exists, use `npm ci`" handles that case — recipes typically commit lockfiles
- No YAML snippet, no pnpm/yarn/bun table — covered by failure classifier and runtime-specific atoms (added per evidence in Phase 3)

### 3.4 What the atom does NOT do

- Does NOT replace `develop-first-deploy-scaffold-yaml.md` (general scaffold guidance stays)
- Does NOT teach env vars (`develop-first-deploy-env-vars.md` owns that)
- Does NOT teach ports/start/healthCheck (`develop-first-deploy-write-app.md` + `develop-first-deploy-verify.md` own that)
- Does NOT fire for Python/Go/PHP/Rust/etc. (axis filter prevents)
- Does NOT fire for already-deployed services (`envelopeDeployStates: [never-deployed]` filter)

---

## 4. Phasing

### Phase 1 — Axis infrastructure

**Files modified**:
- `internal/workflow/atom.go` (+ `RuntimeBases` field, + validAtomFrontmatterKeys entry)
- `internal/workflow/synthesize.go` (+ `matchesRuntimeBase` + filter logic)
- `internal/topology/type_equivalence.go` (verify `CanonicalBareForm` handles all expected forms; add helper if needed)

**TDD red sequence**:

1. Unit test `internal/workflow/synthesize_test.go::TestRuntimeBasesAxis`:
   - Atom `runtimeBases: [nodejs]` + service `nodejs@22` → match
   - Same atom + service `python@3.12` → no match
   - Same atom + service `alpine/nodejs@22` → match (composite canonicalized)
   - Empty `runtimeBases:` → atom unaffected (axis not enforced)
   - Multiple bases `[nodejs, python]` + nodejs service → match
   - Multiple bases + go service → no match

2. Atom parser test: atom with `runtimeBases: [nodejs]` parses cleanly; unknown frontmatter still rejected by existing strict set

3. `TestAtomAuthoringLint` — verify new axis doesn't trigger false-positive axis violations (axis K/L/M/N unaffected)

**Verification gate Phase 1**:
- Unit tests green
- Existing scenarios golden test green (no atom uses new axis yet — no behavior change visible to LLM)
- Atom corpus lint green
- `references-fields` lint green (RuntimeBases is in `workflow/` package, lint root already covers it)

### Phase 2 — Node atom

**Files added**:
- `internal/content/atoms/develop-nodejs-greenfield-buildhint.md`

**Files modified (golden refresh)**:
- `internal/workflow/testdata/atom-goldens/develop/first-deploy-dev-dynamic-container.md` (atom appears in render for nodejs scenarios)
- `internal/workflow/testdata/atom-goldens/_coverage-map.md` (atom listed)
- Possibly `develop/failure-tier-3.md` (depends on scenario runtime)
- Possibly `internal/workflow/scenarios_test.go::TestScenario_PinCoverage_AllAtomsReachable` (if inventory list)

**TDD red sequence**:

1. Add atom file
2. Run `ZCP_UPDATE_ATOM_GOLDENS=1 go test ./internal/workflow -run TestScenarios_GoldenComparison -v`
3. Review golden diffs:
   - Atom body appears in `first-deploy-dev-dynamic-container.md` (which is nodejs-typed)
   - Verify it does NOT appear in non-Node scenario goldens (search for atom ID across all goldens)
4. Verify atom passes lint: `TestAtomAuthoringLint`, `TestAtomReferencesAtomsIntegrity`, `TestAtomReferenceFieldIntegrity`
5. Update `TestScenario_PinCoverage_AllAtomsReachable` if it inventories atom IDs (per scenarios golden test agent finding)

**Verification gate Phase 2**:
- Atom lint green
- Goldens refreshed + diffs reviewed manually
- Coverage map regenerated
- Eval re-run `api-node-postgres-classic-dev`, `develop-loop-after-bootstrap`:
  - Structured gate: no `build:npm-ci-missing-lockfile` classification fires, single deploy call, first verify pass
  - Strict pass/fail single-run initially (per codex eval semantics — no ≥95% gate until repeated-run harness exists)

### Phase 3 — Pay-as-discover backlog setup

Add `plans/backlog/per-runtime-greenfield-buildhints.md` documenting:

- Pattern: one atom per runtime, axis-filtered (`runtimeBases: [X]` + `deployStates: [never-deployed]`), 1-3 line body trigger-first
- **Refined trigger** (per codex): "same greenfield **install-command failure class** repeats ≥2 times for the same runtime"
- **NOT promoted** by: any general runtime-specific friction (start command shape, healthCheck confusion, dev-mode lifecycle — those are separate atom problems with different fixes)
- Current eval data (informational):
  - Python: gunicorn path friction exists (different class — start command, not install command)
  - Go: dev-mode iteration friction exists (different class — start command shape)
  - Bun: dev-mode start/healthCheck confusion (different class — lifecycle)
  - None of these are install-command failures; the Node `npm ci` pattern is unique so far
- Each future atom is 1-3 lines body + golden refresh = ~30 min work, deferred until install-command evidence per runtime

---

## 5. Test strategy

### 5.1 Unit (`internal/workflow/synthesize_test.go` + `internal/workflow/atom_test.go`)

Table-driven `TestRuntimeBasesAxis`:

| TypeVersion | RuntimeBases (atom) | Expected match |
|---|---|---|
| `nodejs@22` | `[nodejs]` | true |
| `python@3.12` | `[nodejs]` | false |
| `alpine/nodejs@22` | `[nodejs]` | true (canonical bare) |
| `ubuntu/python@3.12` | `[nodejs]` | false |
| `nodejs@22` | `[nodejs, python]` | true |
| `go@1` | `[nodejs, python]` | false |
| `nodejs@22` | empty/nil | true (axis not enforced) |
| `postgresql@18` (managed) | `[nodejs]` | false |
| `php-nginx@8.4` | `[php-nginx]` | true |
| `php-nginx@8.4` | `[php]` | **false** (family matching NOT supported) |
| `php-apache@8.4` | `[php-nginx, php-apache]` | true |
| `nodejs@22` | `[nonexistent_runtime]` | false (parses cleanly, never matches) |

**Same-service conjunction tests** (CRITICAL per codex):

- Atom `runtimeBases: [nodejs] + deployStates: [never-deployed]` + envelope with (deployed `appdev` nodejs + never-deployed `pythondev` python) → atom must **NOT** fire (no single service satisfies both axes)
- Same atom + envelope with (never-deployed `appdev` nodejs + deployed `db` postgres) → atom **fires** for `appdev`

**WorkSession scope test**:

- Out-of-scope Node service must not make scoped Python task see Node guidance

**Parser test** (`atom_test.go`):

- Atom with `runtimeBases: [nodejs]` parses cleanly
- Unknown frontmatter key still rejected by strict set (verify backward compat)

### 5.2 Atom corpus lint

- `TestAtomAuthoringLint` — accept `runtimeBases:` in frontmatter
- `TestAtomReferencesAtomsIntegrity` — atom has no `references-atoms`; trivially passes
- `TestAtomReferenceFieldIntegrity` — atom references `workflow.ServiceSnapshot.TypeVersion`; field exists; passes

### 5.3 Scenarios golden tests

Refresh affected goldens via `ZCP_UPDATE_ATOM_GOLDENS=1`. Manual review:

**Definitely refresh** (per codex confirmed via `scenarios_fixtures_test.go:299`):
- `develop/first-deploy-dev-dynamic-container.md` — Node never-deployed, atom WILL appear
- `develop/failure-tier-3.md` — Node never-deployed, atom WILL appear
- `_coverage-map.md` — atom listed
- `TestScenario_PinCoverage_AllAtomsReachable` at `scenarios_test.go:969` — explicit atom-ID inventory, MUST be updated

**Definitely NOT changed**:
- Non-Node scenarios (`recipe-laravel-showcase-fullstack`, future python/go fixtures) — atom does NOT appear

### 5.4 Eval acceptance

Two scenarios re-run with structured gate:
- `api-node-postgres-classic-dev`
- `develop-loop-after-bootstrap`

Pass criteria:
- Exactly 1 `zerops_deploy targetService="appdev"` call
- First deploy returns `status="success"`
- No `failureClassification.signals[]` contains `build:npm-ci-missing-lockfile`
- `zerops_verify` returns `status="healthy"` on first call
- Workflow can close (auto or explicit) without second corrective deploy

Strict single-run pass/fail. If fails, investigate root cause.

### 5.5 Negative test (atom does NOT leak)

Scenarios golden test confirms a non-Node scenario does NOT see Node atom. Possible target: add or use `classic-python-postgres-dev-only` if exists; otherwise rely on `recipe-laravel-showcase-fullstack` PHP path.

---

## 6. Risks + mitigations

| Risk | Likelihood | Mitigation |
|---|---|---|
| LLM ignores atom (still defaults to `npm ci` from muscle memory) | LOW | Atom appears in develop response Guidance section before LLM writes yaml. If observed in eval, investigate atom positioning / wording / priority |
| Axis match logic edge case (case sensitivity in canonical form, unexpected service type) | LOW | Table-driven test covers known forms; `CanonicalBareForm` already production-pinned for other callers |
| Atom corpus over-fires (matches when shouldn't) | LOW | `envelopeDeployStates: [never-deployed]` filter prevents firing after first successful deploy; runtime base filter prevents firing on non-Node |
| Per-runtime atom sprawl in future | LOW | Pay-as-discover principle in §4 Phase 3 backlog — atom per evidence, not preemptive |
| Brownfield false-positive (already has lockfile, atom still tells `npm install`) | MEDIUM | Atom body explicit: "If your project already has a committed lockfile (brownfield), use the strict variant from the start." Override path stated |

---

## 7. Rollback

- Phase 1: revert axis field + filter logic. No atoms use it yet, neutral.
- Phase 2: revert atom + golden refresh. Other atoms unaffected.

Forward-only safe per phase.

---

## 8. Out of scope

- Composer redesign (rejected — `plans/scaffold-composer-integrated-2026-05-20.md` deleted)
- Per-runtime atoms for python/go/php/rust/bun/deno/dotnet/java/elixir/gleam/ruby (deferred to backlog Phase 3)
- Failure classifier changes (stays as defense-in-depth)
- Recipe path changes (recipes own their yaml; atom doesn't affect recipe scenarios)
- Bootstrap import yaml composer (separate sister problem)
- `nodejs-hello-world.md:85` schema-invalid `run.verticalAutoscaling` fix (separate bug surfaced during validation)
- `scaffold-zerops-yaml.md:21-28` table emits guessed commands as known (separate atom edit if eval data shows export-time friction)

---

## 9. Implementation effort estimate

| Phase | Effort | Lines |
|---|---|---|
| Phase 1 (axis infrastructure) | ~4 hours | ~80 Go + ~60 test |
| Phase 2 (Node atom + goldens) | ~1 hour | ~30 atom + golden refresh |
| Phase 3 (backlog setup) | ~15 min | ~40 backlog doc |
| **Total** | **~5-6 hours** | **~170 lines new code/tests/content** |

Compare to rejected Plan v2 (composer redesign): ~600 lines Go + 4 phases. Difference: ~10× smaller, same problem coverage for the runtime that empirically fails, pay-as-discover for the rest.

---

## 10. Codex review — applied (2026-05-20)

Codex review at `/tmp/codex-out-1779286725-77320-978.md` returned **right-sized but not ready as-is**. Critical fix + 8 refinements applied:

| # | Codex finding | Applied in plan |
|---|---|---|
| 1 | `envelopeDeployStates: [never-deployed]` allows atom to fire for deployed Node when Python is never-deployed in same envelope | §3.3: changed to `deployStates: [never-deployed]` (service-scoped) |
| 2 | `runtimeBases` must be added to BOTH `hasServiceScopedAxes` AND `serviceSatisfiesAxes` | §3.2: both edit points named explicitly |
| 3 | `multiService: aggregate` explicit documentation | §3.3: added to atom frontmatter |
| 4 | Atom body too long, lacks trigger-first framing per CLAUDE.local.md | §3.3: rewritten to 3 lines trigger-first |
| 5 | Atom WILL fire for Node recipe scenarios (envelopes don't carry `Bootstrap.Route` for develop) | §3.3: body lockfile-sensitive — "if lockfile exists, use `npm ci`" handles recipe case |
| 6 | PHP family clarification: `[php]` ≠ `php-nginx`/`php-apache` | §3.2: explicit upfront |
| 7 | `failure-tier-3` golden refresh confirmed (Node never-deployed fixture) | §5.3: moved from "possibly" to "definitely" |
| 8 | Phase 3 trigger too broad ("any runtime friction") | §4 Phase 3: refined to "same install-command failure class ≥2 times per runtime" |
| 9 | Parser test in `atom_test.go` + 5 additional test cases | §5.1: added |

Single most important fix: **`deployStates: [never-deployed]` instead of `envelopeDeployStates: [never-deployed]`**. Without this, atom fires for already-deployed Node service when separate never-deployed service exists in same project.

---

**END OF PLAN — ready to execute.**
