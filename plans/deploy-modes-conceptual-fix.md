# Deploy Modes — Conceptual Fix for F3 + F4

> **Scope**: Elevate the self-deploy vs cross-deploy asymmetry to a first-class concept in ZCP. Replace one role-gated advice + one source-tree-speculating existence check with mode-aware validation. Restructure the dotnet recipe to cover the majority single-app case by default and comment the migration-case variant. Document five DM-* invariants in `docs/spec-workflows.md` §8. Update atom corpus so the mental model is visible to every LLM from first scaffold.
>
> **Root cause documentation**: `plans/f3-f4-deep-dive-2026-04-24.md` — full flow trace, live repro, platform-docs cross-refs. This plan assumes that understanding.
>
> **Not a patch**: A minimal fix would just delete `deploy_validate.go:86-100`. That removes the false positive but leaves the conceptual vacuum: nowhere in spec/atoms/code does the system express *why* self-deploy and cross-deploy validate differently. The next similar bug (F3' some future year) would reproduce the same reasoning error because there's no anchor to cite. The plan closes that vacuum.
>
> **Companion specs (authoritative)**:
> - `docs/spec-workflows.md` §8 — add DM-1…DM-5 subsection
> - `docs/spec-workflows.md` §4.6 — cross-reference Mode-Specific Deploy Behavior to DM invariants
> - `plans/api-validation-plumbing.md` — already shipped W1–W8; DM-4 formalizes W6's "delete duplicate validation" philosophy
> - `plans/friction-root-causes.md` P2.4 — partially shipped; this plan supersedes for dotnet recipe

---

## 0. Principle

**Self-deploy and cross-deploy share a tool but have opposite contracts.** Self-deploy refreshes a mutable workspace — `deployFiles` semantics are "what to preserve in place", narrower-than-`.` means destruction. Cross-deploy produces an immutable artifact — `deployFiles` semantics are "what to cherry-pick from the post-build tree". Today ZCP conflates them in validation and the dotnet recipe conflates them in content structure. This plan separates both.

Five invariants (DM-1…DM-5) codify the separation. Code and content follow.

---

## 1. Five invariants (DM-1…DM-5)

Authoritative text for `docs/spec-workflows.md` §8 new subsection *Deploy Modes*:

### Deploy Modes

| ID | Invariant |
|---|---|
| **DM-1** | Every `zerops_deploy` invocation resolves to exactly one of two classes, determined at tool entry: **self-deploy** when `sourceService == targetService` (after auto-infer when sourceService is omitted), **cross-deploy** otherwise OR when `strategy=git-push`. The class propagates through `DeploySSH`/`DeployLocal`/`handleGitPush` and into `ValidateZeropsYml`. No code path inspects mode heuristically later — the class is a carried parameter. |
| **DM-2** | Self-deploy's `deployFiles` for the resolved setup block MUST be `.` or `./`. A narrower pattern destroys the target's working tree on artifact extraction: the artifact contains only the cherry-picked subset, the runtime container's `/var/www/` is overwritten with that subset, and the source tree is permanently lost on the target (and on subsequent self-deploys, since the target no longer has source to re-upload). ZCP client-side pre-flight rejects DM-2 violations with `ErrInvalidZeropsYml`. |
| **DM-3** | Cross-deploy's `deployFiles` is defined over the **build container's post-buildCommands filesystem**. The source tree is INPUT (uploaded by `zcli push`), the build output is OUTPUT (produced by `buildCommands`), and `deployFiles` selects from OUTPUT. A cross-deploy `deployFiles` path that doesn't exist in the source tree (e.g. `./out`, `./dist`, `./target`) is normal, not an error. |
| **DM-4** | Validation is layered with disjoint authority. ZCP client-side pre-flight validates only source-tree-knowable facts: YAML syntax, schema shape, role↔setup coherence, DM-2. Zerops API pre-flight (`ValidateZeropsYaml`) validates field values against the live service-type catalog. Zerops builder validates the post-build filesystem at build time (deployFiles paths existing in build container). Runtime validates initCommands, readiness checks, start. **No layer duplicates another's authority.** DM-4 formalizes W6 of `api-validation-plumbing.md`. |
| **DM-5** | At runtime start, CWD is `/var/www`. Content-root expectations of foreground processes (ASP.NET's `ContentRootPath = Directory.GetCurrentDirectory()` → `wwwroot/` lookup at `/var/www/wwwroot`; Python's `__file__`-relative resolution; Java's classpath) are **runtime concerns**. Recipes MUST document content-root implications when their default `deployFiles` choice interacts with a well-known runtime gotcha. Agents pick `deployFiles` preserve-vs-extract (`./out` vs `./out/~`) to match the runtime's content-root expectation. |

Cross-references from §4.6 (*Mode-Specific Deploy Behavior*) and §1.1 (*Service Lifecycle*) to the DM-* block. Existing E8, GLC-1…GLC-6 pattern followed.

---

## 2. Non-negotiable constraints

Violating any fails the plan.

1. DM-1…DM-5 are binding on all deploy-related code. AST or contract tests pin them.
2. TDD at every affected layer — RED test before GREEN code.
3. No backward-compat shims. Old `deployFiles paths not found` behavior removed outright.
4. Atom-code contract (P2.5 from `friction-root-causes.md`) extended — every atom claim about deploy modes has a code-anchor entry.
5. Recipe edit runs through `zcp sync pull/push` — no direct API mutation.
6. `ValidateZeropsYml` signature change is a single atomic refactor, not phased.
7. File-size cap (350 LOC/go, per CLAUDE.md) respected.

---

## 3. Scope — changes vs preserved

### Changes (this plan)

| File | Change class | What |
|---|---|---|
| `docs/spec-workflows.md` | spec | Add §8 subsection "Deploy Modes" with DM-1…DM-5 |
| `docs/spec-workflows.md` | spec | §4.6 cross-reference to DM-* |
| `internal/ops/deploy_validate.go` | code | Add `DeployClass` parameter; replace existence check + "dev should use [.]" advice with DM-2 enforcement |
| `internal/ops/deploy_validate_test.go` | test | Restructure deployFiles cases around DM-1/DM-2/DM-3 |
| `internal/ops/deploy_ssh.go` | code | Compute DeployClass from source==target; pass to ValidateZeropsYml |
| `internal/ops/deploy_local.go` | code | Same |
| `internal/tools/deploy_git_push.go` | code | Pass DeployClass=CrossDeploy |
| `internal/content/atoms/develop-deploy-modes.md` | content | NEW atom — explicit DM-1/DM-2/DM-3 explanation |
| `internal/content/atoms/develop-first-deploy-scaffold-yaml.md` | content | Add tilde-extraction guidance for content-root-sensitive runtimes |
| `internal/content/atoms/develop-dev-mode-deploy-files.md` | content | Replace role-based with DM-2-based wording |
| `internal/content/atoms/develop-push-dev-deploy-container.md` | content | Brief reference to DM-2 (tool description already carries it) |
| `internal/knowledge/recipes/dotnet-hello-world.md` | content (via sync) | Default to single-app-with-tilde; commented multi-app variant |
| `internal/knowledge/recipes/dotnet-hello-world.import.yml` | content (via sync) | Align envs/hostnames if restructure touches them |
| `internal/eval/scenarios/deploy-warnings-fresh-only.md` | eval | Verify no regression after pre-flight-check delete |
| `internal/eval/scenarios/develop-dotnet-stage-wwwroot.md` | eval (NEW) | Pin end-to-end wwwroot works on stage cross-deploy with tilde |
| `internal/workflow/atom_contract_test.go` | test | Add contract entries for DM-* claims in atoms |

### Not changed (out of scope)

- `ServiceMeta` shape (DeployRole stays; DeployClass is orthogonal — computed from source/target, not stored)
- Git lifecycle (GLC-1…GLC-6 unchanged)
- API-validation plumbing W1–W8 (shipped; this plan builds on it)
- Bootstrap flow, adoption, strategy handling
- Simple/dev/standard mode matrix (DM-* is orthogonal to those)

---

## 4. Workstreams

Each workstream lists: **goal**, **files**, **tests (TDD RED)**, **GREEN changes**, **file budget**, **dependencies**.

### W1 — DM-1…DM-5 spec subsection

**Goal**: Authoritative invariant block. All later workstreams cite it.

**Files**: `docs/spec-workflows.md` §8 (new subsection after *Evidence*); §4.6 cross-reference.

**Tests**: no code tests; spec text review only.

**GREEN**: Insert DM-1…DM-5 table (verbatim from §1 of this plan). Add single-line cross-ref from §4.6 `Mode-Specific Deploy Behavior`: *"Self-deploy vs cross-deploy semantics — see §8 Deploy Modes (DM-1…DM-5)."*

**File budget**: +60 LOC `docs/spec-workflows.md`.

**Dependencies**: none; blocks W2, W4.

---

### W2 — Mode-aware validator

**Goal**: `ValidateZeropsYml` accepts a `DeployClass` and dispatches:
- `SelfDeploy`: enforce DM-2 (`deployFiles` MUST be `.`/`./`), no filesystem-existence check
- `CrossDeploy`: no filesystem-existence check (DM-3 → builder authority; DM-4 → no duplicate)

**Files**:
- `internal/ops/deploy_validate.go` — signature + logic
- `internal/ops/deploy_validate_test.go` — restructured test cases
- `internal/ops/deploy_ssh.go` — pass DeployClass at line 125
- `internal/ops/deploy_local.go` — pass DeployClass at line 110
- `internal/tools/deploy_git_push.go` — pass DeployClass=CrossDeploy
- `internal/ops/deploy_common.go` — DeployClass type definition

**Type definition** (`deploy_common.go`):
```go
// DeployClass classifies a deploy as self-deploy (source==target) or
// cross-deploy (source≠target, including strategy=git-push). See DM-1 in
// docs/spec-workflows.md §8. Computed at tool entry; passed through all
// deploy layers.
type DeployClass string
const (
    DeployClassSelf  DeployClass = "self"
    DeployClassCross DeployClass = "cross"
)

func ClassifyDeploy(source, target string) DeployClass {
    if source == "" || source == target {
        return DeployClassSelf
    }
    return DeployClassCross
}
```

**`ValidateZeropsYml` signature change**:
```go
// BEFORE:
// func ValidateZeropsYml(workingDir, targetHostname, serviceType string, roles ...string) []string
//
// AFTER:
// func ValidateZeropsYml(workingDir, setupName, serviceType string, class DeployClass, roles ...string) (warnings []string, err error)
//   - warnings: non-blocking advisories (existing channel)
//   - err: DM-2 violation is a *PlatformError{Code: ErrInvalidZeropsYml} — deploy aborts
```

Callers change to handle the error return. DM-2 violation is a HARD STOP before API validation.

**Semantic changes inside validator**:
1. Delete lines 86-100 (source-tree existence check) — DM-3/DM-4.
2. Replace lines 80-84 ("dev should use [.]" advice) with a DeployClass-aware DM-2 enforcer:
   ```go
   if class == DeployClassSelf {
       if !slices.Contains(deployFiles, ".") && !slices.Contains(deployFiles, "./") {
           return warnings, platform.NewPlatformError(
               platform.ErrInvalidZeropsYml,
               fmt.Sprintf("self-deploy setup %q: deployFiles must be [.] or [./] — narrower patterns destroy the target's working tree on artifact extraction (DM-2)", entry.Setup),
               "Set `deployFiles: [.]` for self-deploy. For cherry-picking build outputs, use cross-deploy (set sourceService != targetService, or strategy=git-push).",
           )
       }
   }
   ```
3. Preserve: missing-deployFiles warning, misplaced-deployFiles warning, zsc-noop-on-stage warning, dev-healthCheck/readinessCheck warnings, sudo check.

**Tests (TDD RED → GREEN)**:

Delete:
- `"cherry-picked deployFiles with missing paths"` (deploy_validate_test.go, existing)
- `"dev without dot deployFiles"` (existing advice-warning case)

Add:
```go
func TestValidateZeropsYml_DM2_SelfDeployRequiresDotSlash(t *testing.T) {
    cases := []struct {
        name         string
        yaml         string
        class        DeployClass
        wantErrCode  string
        wantErrContains string
    }{
        {"self-deploy with [.] passes", minimalYaml(".", "appdev"), DeployClassSelf, "", ""},
        {"self-deploy with [./] passes", minimalYaml("./", "appdev"), DeployClassSelf, "", ""},
        {"self-deploy with [./out] errors", minimalYaml("./out", "appdev"), DeployClassSelf, "INVALID_ZEROPS_YML", "DM-2"},
        {"self-deploy with [dist] errors", minimalYaml("dist", "appdev"), DeployClassSelf, "INVALID_ZEROPS_YML", "DM-2"},
    }
    // ...
}

func TestValidateZeropsYml_DM3_CrossDeployNoSourceExistenceCheck(t *testing.T) {
    cases := []struct {
        name         string
        yaml         string
        class        DeployClass
        wantWarnings int
    }{
        {"cross-deploy [./out] with no out in source — no warning", ymlDotnetStage("./out"), DeployClassCross, 0},
        {"cross-deploy [./dist] — no warning", ymlDotnetStage("./dist"), DeployClassCross, 0},
        {"cross-deploy [.] — no warning", ymlDotnetStage("."), DeployClassCross, 0},
    }
    // ...
}

func TestValidateZeropsYml_DM4_YamlShapeOnly(t *testing.T) {
    // Proves pre-flight only checks yaml-shape things, never filesystem existence.
    // Regression gate against re-introducing the existence check.
}
```

Preserve (adjusted to new signature):
- yaml shape tests (`TestValidateZeropsYml_Parsing`)
- healthcheck/readinessCheck tests (role-gated, unchanged)
- implicit-webserver tests (unchanged)
- misplaced-deployFiles tests

**File budget**: net −40 LOC production (-40 check, +30 DM-2 error, delete 3 cases, add 3 cases); +80 LOC tests.

**Dependencies**: W1 shipped (for spec cite in comments); blocks W6.

---

### W3 — Dotnet recipe restructure (sync flow)

**Goal**: Default case is simple ASP.NET single-app with wwwroot works out of the box. Migration variant explicitly commented and opt-in.

**Flow**: `zcp sync pull recipes dotnet-hello-world` → edit `internal/knowledge/recipes/dotnet-hello-world.md` → `zcp sync push recipes dotnet-hello-world` → (GitHub PR review on `zerops-recipe-apps/dotnet-hello-world-app`) → merge → `zcp sync cache-clear dotnet-hello-world` → `zcp sync pull` to verify.

**Default PROD block (single-app-with-tilde)**:
```yaml
- setup: prod
  build:
    base: dotnet@9
    buildCommands:
      - dotnet publish App.csproj -c Release -o out
    deployFiles:
      - ./out/~    # Extract CONTENTS into /var/www/ — App.dll lands at /var/www/App.dll,
                   # wwwroot/ at /var/www/wwwroot/. Aligns with ASP.NET's default
                   # ContentRootPath = Directory.GetCurrentDirectory() = /var/www.
                   # See DM-5 in docs/spec-workflows.md §8 for the content-root rule.
    cache: true
  deploy:
    readinessCheck:
      httpGet:
        port: 8080
        path: /
  run:
    base: dotnet@9
    ports:
      - port: 8080
        httpSupport: true
    envVariables:
      ASPNETCORE_ENVIRONMENT: Production
      ASPNETCORE_URLS: http://0.0.0.0:8080
    start: dotnet App.dll
```

**Commented migration variant (multi-app-with-cd)**:

Appended under default as `## Variant — with database migration`:
```yaml
# When you need an out-of-band migration step before app start, publish
# app and migrator to separate directories under out/ and reference them
# explicitly. Note the `cd` in start to align CWD with ContentRootPath.
- setup: prod
  build:
    base: dotnet@9
    buildCommands:
      - dotnet publish App/App.csproj -c Release -o out/app
      - dotnet publish Migrate/Migrate.csproj -c Release -o out/migrate
    deployFiles:
      - ./out    # PRESERVE directory — /var/www/out/{app,migrate}/
  run:
    base: dotnet@9
    initCommands:
      - zsc execOnce ${appVersionId} --retryUntilSuccessful -- dotnet /var/www/out/migrate/Migrate.dll
    # CWD change aligns ASP.NET's ContentRootPath with app dir so wwwroot/
    # at /var/www/out/app/wwwroot/ is discovered correctly.
    start: cd /var/www/out/app && dotnet App.dll
    ports:
      - port: 8080
        httpSupport: true
    envVariables:
      ASPNETCORE_ENVIRONMENT: Production
      ASPNETCORE_URLS: http://0.0.0.0:8080
      DB_HOST: ${db_hostname}
      DB_PORT: ${db_port}
      DB_USER: ${db_user}
      DB_PASS: ${db_password}
```

**DEV block unchanged** (self-deploy target; `deployFiles: ./` conforms to DM-2).

**Recipe header prose (new section)**:
```markdown
## Deploy path patterns

This recipe demonstrates the two canonical dotnet patterns:

1. **Single-app with tilde** (default) — publish to `./out`, extract contents
   via `./out/~`. Runtime sees the app at `/var/www/`. Works with ASP.NET
   static file serving (`wwwroot/`) out of the box.

2. **Multi-app with preserve + cd** (variant below) — publish multiple apps
   to `./out/{app,migrate}/`, preserve the `out/` directory. Start command
   uses `cd` to align CWD with app directory.

Mode A (self-deploy to the dev service) uses the DEV block's
`deployFiles: ./` — ships the whole source tree. Mode B (cross-deploy to
the stage service) uses the PROD block's `deployFiles`. See `docs/spec-workflows.md`
§8 Deploy Modes (DM-1…DM-5) for the underlying invariants.
```

**File budget**: ~50 LOC net change in `dotnet-hello-world.md` (keeps variants visible).

**Dependencies**: W1 shipped (spec anchor for cross-references); independent of W2.

---

### W4 — Atom corpus updates

**Goal**: Every LLM reading atoms during develop flow sees the mode distinction explicitly.

#### W4.1 — NEW atom `develop-deploy-modes.md`

```markdown
---
id: develop-deploy-modes
priority: 2
phases: [develop-active]
title: "Deploy modes — self-deploy vs cross-deploy"
---

### Two deploy classes, one tool

`zerops_deploy` has two classes, determined by source vs target:

- **Self-deploy** — `sourceService == targetService` (or sourceService
  omitted). Refreshes a mutable workspace. Runtime receives the working
  tree as-is; `deployFiles` MUST be `[.]` or `[./]`. A narrower pattern
  destroys the target's source. Typical for dev services (`start: zsc
  noop --silent`; agent SSHes and runs the app interactively).
- **Cross-deploy** — `sourceService != targetService`, or
  `strategy=git-push`. Produces an immutable artifact. Runtime receives
  the build container's post-`buildCommands` output selected by
  `deployFiles` (typically cherry-picked: `./out`, `./dist`, `./build`).
  Typical for dev→stage promotion.

### Picking deployFiles

- **Self-deploy block** — `deployFiles: [.]`. Non-negotiable (DM-2).
- **Cross-deploy block** — `deployFiles` selects from the build output.
  - Preserve directory: `[./out]` ships as `/var/www/out/...`. Choose
    when start command references paths like `./out/app/App.dll` or
    multiple artifacts live in subdirs.
  - Extract contents: `[./out/~]` ships as `/var/www/...` (tilde strips
    the `out/` prefix). Choose when the runtime expects assets at root
    (ASP.NET's `wwwroot/` at ContentRootPath = `/var/www/`).

### Why the source tree sometimes doesn't have `./out`

`deployFiles` is defined over the build container's filesystem AFTER
`buildCommands` runs. A cross-deploy `deployFiles: [./out]` is normal
even when `./out` doesn't exist in your editor — the build container
creates it. ZCP client-side pre-flight does NOT check path existence
for cross-deploy (DM-3/DM-4); the Zerops builder validates at build
time.

**Reference**: `docs/spec-workflows.md` §8 Deploy Modes (DM-1…DM-5).
```

#### W4.2 — UPDATE `develop-first-deploy-scaffold-yaml.md`

Insert after line 44 (the current mode-aware tips):

```markdown
**Content-root tip (ASP.NET, static-serving frameworks):**

When a foreground runtime process expects assets at `ContentRootPath = CWD`
(e.g. ASP.NET's `wwwroot/`), cross-deploy's `deployFiles` must ship those
assets to `/var/www/` level. Choose the tilde-extract pattern (`./out/~`)
over preserve (`./out`) in that case. See `develop-deploy-modes` atom for
the decision rule and DM-5 in `docs/spec-workflows.md` §8.
```

#### W4.3 — REPLACE `develop-dev-mode-deploy-files.md`

Current body refers to "dev mode" and "skips the build step". Replace with DM-2-based wording:

```markdown
---
id: develop-deploy-files-self-deploy
priority: 3
phases: [develop-active]
title: "Self-deploy requires deployFiles: [.] — DM-2"
---

### Self-deploy invariant (DM-2)

Any service self-deploying (source == target — the typical pattern for
dev services and simple mode) MUST have `deployFiles: [.]` or `[./]` in
the matching setup block. A narrower pattern destroys the target's
working tree on the next deploy: the artifact contains only the
selected subset, the runtime container's `/var/www/` is overwritten
with that subset, and the source is gone.

Client-side pre-flight rejects DM-2 violations with
`INVALID_ZEROPS_YML` before any build triggers.

Cross-deploy (source ≠ target, or strategy=git-push) has different
semantics — see `develop-deploy-modes` atom. DM-2 does NOT apply to
cross-deploy.

**Reference**: `docs/spec-workflows.md` §8 Deploy Modes.
```

File renamed from `develop-dev-mode-deploy-files.md` to
`develop-deploy-files-self-deploy.md` to reflect DM-grounded naming.
Renaming requires updating `corpus_coverage_test.go` references if any.

#### W4.4 — TOUCH `develop-push-dev-deploy-container.md`

Append one line after existing content:

> `deployFiles` discipline: self-deploy needs `[.]` (DM-2). Cross-deploy cherry-picks build output. See `develop-deploy-modes`.

**File budget**: +1 new atom ~60 LOC; +10 LOC in scaffold atom; rename + rewrite of one existing atom (−15 LOC → +30 LOC net); +1 line in push-dev atom.

**Dependencies**: W1 shipped; independent of W2/W3 (parallel).

---

### W5 — Atom-code contract entries (extension of P2.5)

**Goal**: Each DM-grounded atom claim pins to an AST-locatable code symbol so atom edits that drop claims or code refactors that drop behavior fail the same test.

**File**: `internal/workflow/atom_contract_test.go`.

**New entries**:
```go
{
    atomID:         "develop-deploy-modes",
    phraseRequired: []string{"DM-2", "cross-deploy", "self-deploy", "./out/~"},
    testName:       "TestValidateZeropsYml_DM2_SelfDeployRequiresDotSlash",
    codeAnchor:     "internal/ops/deploy_validate.go#ValidateZeropsYml",
},
{
    atomID:         "develop-deploy-files-self-deploy",
    phraseRequired: []string{"DM-2", "[.]", "destroys the target"},
    testName:       "TestValidateZeropsYml_DM2_SelfDeployRequiresDotSlash",
    codeAnchor:     "internal/ops/deploy_validate.go#ValidateZeropsYml",
},
{
    atomID:         "develop-first-deploy-scaffold-yaml",
    phraseRequired: []string{"tilde-extract", "ContentRootPath", "DM-5"},
    testName:       "TestScenario_DotnetStageWwwroot_TildeExtracts",
    codeAnchor:     "internal/knowledge/recipes/dotnet-hello-world.md",
},
```

**File budget**: +15 LOC in the contract table.

**Dependencies**: W2 (tests must exist); W4 (atoms must be authored); W1 (invariant IDs must be in spec).

---

### W6 — Eval scenarios

#### W6.1 — Regression check `deploy-warnings-fresh-only.md`

After W2 removes the client-side existence check, the eval scenario should still pass. The scenario's `forbiddenPatterns` contains `'deployFiles.*not.*found.*dist'` — that's about **build-container warnings not leaking across deploys** (I-LOG-2), which is orthogonal to DM-4's client-side delete. Re-run and verify PASS.

If the scenario's FIRST deploy depended on the client-side warning appearing, swap it for a test of the builder-side warning instead (same text, different source field: `BuildLogs` not `Warnings`).

#### W6.2 — NEW scenario `develop-dotnet-stage-wwwroot.md`

Pins end-to-end that tilde extraction fixes the ASP.NET wwwroot case.

```markdown
---
id: develop-dotnet-stage-wwwroot
description: Greenfield .NET app with a wwwroot/ static file; stage cross-deploy must serve the static file at /. Validates DM-5 (tilde extraction aligns with ASP.NET ContentRootPath).
seed: empty
fixture: fixtures/dotnet-minimal-with-wwwroot/   # App.csproj, Program.cs, wwwroot/index.html
expect:
  mustCallTools:
    - zerops_workflow
    - zerops_import
    - zerops_deploy
    - zerops_verify
  workflowCallsMin: 8
  mustEnterWorkflow:
    - bootstrap
    - develop
  requiredPatterns:
    - '"workflow":"bootstrap"'
    - '"workflow":"develop"'
  forbiddenPatterns:
    # DM-4 fix regression gate: no more source-tree-based deployFiles warning
    - 'deployFiles paths not found.*/out'
  requireAssessment: true
  finalUrlStatus: 200
  finalUrlPathContains: "hello from wwwroot"
followUp:
  - "Jaký deployFiles pattern jsi zvolil pro stage? `./out` nebo `./out/~`? Proč?"
  - "Co se stane s wwwroot/index.html v obou patternech? Kde končí na /var/www?"
  - "Proč ASP.NET 9 ve výchozím stavu hledá wwwroot v /var/www ne v dll dir?"
---

# Úkol

Vytvoř minimální ASP.NET aplikaci s wwwroot statickým souborem (index.html
s textem "hello from wwwroot"). Nasaď do standard-mode dev+stage páru.
Stage musí vracet 200 na `GET /` s obsahem tohoto souboru.
```

**File budget**: 1 new fixture tree + 1 scenario md (~50 LOC scenario).

**Dependencies**: W2, W3 shipped.

---

### W7 — Documentation cross-references

**Goal**: Closed graph of references; any reader entering from any node finds DM invariants.

**Edits**:

- `docs/spec-workflows.md` §4.6 — one sentence linking to §8 DM-*
- `docs/spec-workflows.md` §1.1 — one sentence in Service Lifecycle noting deploy-class orthogonality
- `CLAUDE.md` — add DM-* to the Conventions section (one bullet pointing to spec)
- `internal/knowledge/bases/static.md` — header sentence citing DM-5 for tilde rationale

**File budget**: ~15 LOC total.

**Dependencies**: W1 shipped.

---

## 5. TDD layers

Per CLAUDE.md *"tests FIRST at ALL affected layers"*.

| Change | Unit | Tool | Integration | E2E | Eval |
|---|---|---|---|---|---|
| W2 validator mode-aware | ✓ deploy_validate_test | ✓ deploy_ssh_test, deploy_local_test | ✓ tools/deploy_*_test | ✓ — existing SSH e2e still passes | W6.1 regress |
| W3 recipe edit | — (content) | — | — | ✓ live deploy dotnet stage on eval-zcp | W6.2 scenario |
| W4 atoms | ✓ atoms_test.go (frontmatter) + corpus_coverage_test.go | — | — | — | atom-surface tests |
| W5 atom-code contract | ✓ atom_contract_test.go | — | — | — | — |
| W6 scenarios | — | — | — | — | ✓ W6.1/W6.2 |
| W7 doc cross-refs | — (prose) | — | — | — | — |

Every behavioral change has RED test before GREEN code:
- W2: `TestValidateZeropsYml_DM2_*` + `TestValidateZeropsYml_DM3_*` written first and failing on current code, pass after validator refactor.
- W3: `TestScenario_DotnetStageWwwroot` failing on current recipe (wwwroot 404), passing after recipe edit via sync push.
- W5: contract test failing when atom missing phrase, passing after atom authoring.

---

## 6. Execution order

Numbered; each step green before next step starts.

1. **W1 spec text** — authoritative reference for all later citations. Standalone PR, review for exact invariant wording.
2. **W2 RED tests** — write failing tests first. Commit them standalone so the failure is visible in history.
3. **W2 GREEN** — DeployClass plumbing + validator refactor + signature change through callers. Tests go green.
4. **W4.1 NEW atom + W4.2 scaffold edit** — content work; can run parallel to W2 once W1 is in.
5. **W4.3 rename + rewrite** — depends on corpus_coverage_test updates.
6. **W5 contract test entries** — after W2 tests + W4 atoms.
7. **W3 recipe sync push** — independent PR flow on Strapi/recipe repo; can run parallel from step 4 onward.
8. **W6.1 eval regress** — after W2 ships.
9. **W6.2 new scenario + fixture** — after W3 merges (recipe needs to be live in Strapi).
10. **W7 doc cross-refs** — last, once all anchors exist.

### Sizing estimate

| Phase | Wall-clock |
|---|---|
| W1 spec draft + review | 1h |
| W2 TDD (RED + GREEN + test refactor) | 3–4h |
| W3 recipe sync + PR | 1h + upstream review time |
| W4 atom corpus | 2h |
| W5 contract entries | 30min |
| W6 eval scenarios | 1–2h |
| W7 doc cross-refs | 30min |
| **Total hands-on** | **~9–11h** |

Single-committer sequencing. Parallelizable if multiple hands — W2 (code) + W4 (content) + W3 (recipe PR) after W1 spec lands.

---

## 7. Scenario coverage matrix

Every DM invariant has at least one eval scenario pin.

| Invariant | Scenario | Proves |
|---|---|---|
| DM-1 (class determined at entry) | existing `bootstrap-git-init`, existing `develop-add-endpoint` | DeployClass propagates through both self- and cross-deploy paths |
| DM-2 (self-deploy requires [.]) | NEW `develop-self-deploy-enforces-dot-slash` (unit-level, in deploy_validate_test.go) | attempting self-deploy with `[./out]` returns INVALID_ZEROPS_YML |
| DM-3 (cross-deploy over post-build) | W6.2 `develop-dotnet-stage-wwwroot` | stage cross-deploy with `./out/~` against source lacking `./out` succeeds without pre-flight warning |
| DM-4 (layered authority) | W6.1 `deploy-warnings-fresh-only` (regression) | no client-side `deployFiles paths not found` warning reaches LLM |
| DM-5 (runtime content-root) | W6.2 `develop-dotnet-stage-wwwroot` | wwwroot served correctly via tilde-extract pattern |

---

## 8. Rollback strategy

Each workstream is a discrete commit; revert per workstream if needed.

| Revert | Effect |
|---|---|
| W1 | Spec rolls back; code still uses DM-* in comments (comments become dangling until follow-up commit clears them) |
| W2 | Validator returns to role-based + existence-check model; F3 false positive returns |
| W3 | Recipe returns to multi-app default; F4 reproduces for simple case |
| W4 | Atoms revert; agents lose explicit mode guidance |
| W5 | Contract test entries removed; atom-code drift detection loses DM-* coverage |
| W6 | Eval coverage shrinks |
| W7 | Cross-references lost |

**Atomicity risk**: W2 signature change cascades to 3 callers. Revert must be simultaneous OR use feature-branch. Recommendation: W2 + W5 + callers in single commit; W1 + W3 + W4 + W6 + W7 independent.

---

## 9. Evidence index

### Code anchors (verified 2026-04-24)
- `internal/ops/deploy_validate.go:22` — ValidateZeropsYml signature (W2 target)
- `internal/ops/deploy_validate.go:77-78,80-84` — isDev/isStage role gates (W2 replace)
- `internal/ops/deploy_validate.go:86-100` — existence check (W2 delete)
- `internal/ops/deploy_ssh.go:69` — `includeGit := sourceService == targetService` (DM-1 computation already exists)
- `internal/ops/deploy_ssh.go:125` — ValidateZeropsYml call site (W2 update)
- `internal/ops/deploy_local.go:117` — same (W2 update)
- `internal/content/atoms/develop-dev-mode-deploy-files.md` — to be renamed + rewritten (W4.3)
- `internal/content/atoms/develop-first-deploy-scaffold-yaml.md:44` — append point (W4.2)
- `internal/knowledge/recipes/dotnet-hello-world.md:26-73` — current multi-app default (W3 rewrite)
- `docs/spec-workflows.md:1034` — §8 Invariants start (W1 insert point)
- `docs/spec-workflows.md:824` — §4.6 Mode-Specific Deploy Behavior (W1 cross-ref)

### Live evidence
- `TestE2E_InitServiceGit` PASS 2026-04-24 (git lifecycle — orthogonal but depended-on)
- `TestF3Repro_DotnetBuildOutput` confirmed warning fires role-independent
- `zcp sync pull recipes dotnet-hello-world` 2026-04-24 — current recipe still multi-app-default

### Platform docs
- `../zerops-docs/apps/docs/content/alpine/how-to/build-pipeline.mdx:256-272` — deployFiles semantics authority
- `../zerops-docs/apps/docs/content/nodejs/how-to/build-pipeline.mdx:338-362` — tilde wildcard semantics
- `../zerops-docs/apps/docs/src/components/content/deploy-process.mdx:12-35` — deploy pipeline phases

### Prior art
- `d961012` (2026-03-03) — implicit-webserver suppression pattern (W2 borrows the wrap-not-gate-detection pattern, but here we go further and remove the check entirely)
- `api-validation-plumbing.md` W6 — delete duplicate validation philosophy (DM-4 formalizes)
- `friction-root-causes.md` P2.4 — dotnet recipe edits (this plan supersedes with conceptual framing)
- `plans/archive/git-service-lifecycle.md` — GLC-1…GLC-6 pattern for invariant-grounded refactor

---

## 10. Open questions

1. **DeployClass naming**: "self-deploy"/"cross-deploy" vs "Mode A"/"Mode B" vs "Refresh"/"Produce". Plan uses "self-deploy"/"cross-deploy" consistent with existing code comments. Confirm before W1 spec text lands.
2. **Renaming `develop-dev-mode-deploy-files.md` → `develop-deploy-files-self-deploy.md`**: git-mv preserves blame history. But atom IDs are load-bearing in `corpus_coverage_test.go` and `atom_contract_test.go`. Search and update all references; single commit.
3. **Recipe sync push review cadence**: Strapi PR review timing unknown. W6.2 scenario blocks on recipe being live in API. Acceptable lag?
4. **DM-2 error vs warning**: plan elevates self-deploy-with-cherry-pick to blocking error. Confirm: is there any LEGITIMATE use case for self-deploy with cherry-pick? (Scan corpus and real-world recipes; none found.)
5. **API-side DM-2 enforcement**: should the Zerops platform API itself reject self-deploy with cherry-pick deployFiles? Out of scope here (platform change), but worth asking. If API adds this check, DM-2 becomes even cheaper client-side.
6. **Multi-app variant placement**: commented block inline in recipe vs. separate recipe `dotnet-multi-app`. Plan goes inline; revisit if it grows.
7. **DM-5 for other runtimes**: plan scopes DM-5 documentation to ASP.NET (the trigger). Java SpringBoot similar (classpath resources)? Python (sys.path)? Audit as follow-up; not blocking.

---

## 11. Out-of-band dependencies

- `zcp sync push recipes dotnet-hello-world` creates PR on external repo (zerops-recipe-apps). PR reviewer ≠ this plan's implementer in general.
- Strapi cache-clear is a separate API call; after PR merge, run `zcp sync cache-clear dotnet-hello-world`.
- If recipe edit requires coordinating `dotnet-hello-world.import.yml` changes, check authoring conventions in `internal/recipe/` (zcprecipator3 engine) for field naming.

---

## 12. What this plan supersedes / extends

**Supersedes**:
- `friction-root-causes.md` P2.4 (.NET recipe edits) — this plan restructures differently (single-app default + variant) with explicit DM grounding.

**Extends**:
- `api-validation-plumbing.md` W6 (delete duplicate validation) — DM-4 formalizes the principle across a new layer.
- `friction-root-causes.md` P2.5 (atom-code contract framework) — W5 adds DM-grounded entries.
- `plans/archive/git-service-lifecycle.md` (GLC invariants pattern) — DM-1…DM-5 follows the same pattern (spec invariants + code tests + atom content).

**Independent of**:
- P3 adaptive envelope (orthogonal; P3 shrinks guidance, this plan restructures guidance).
