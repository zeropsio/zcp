# Plan: Export-for-buildFromGit — turn a live project into an importable single-repo bundle (2026-04-28)

> **Reader contract.** Self-contained for a fresh Claude session.
> Read end-to-end before starting Phase 0.
>
> **Sister plans (precursors)**:
> - `plans/archive/deploy-strategy-decomposition-2026-04-28.md` — established
>   the three-orthogonal-axis model (CloseDeployMode, GitPushState,
>   BuildIntegration). This plan reuses `GitPushState=configured` as the
>   publish-step prerequisite via `setup-git-push-{container,local}` chain.
> - `plans/archive/atom-corpus-hygiene-followup-2-2026-04-27.md` — atom
>   authoring conventions (Axis K/L/M/N enforcement).
>
> **This plan**: implements the user-driven decomposition surfaced by the
> 2026-04-28 design dialogue. Replaces today's 220-line procedural
> `export.md` atom with a phased workflow (probe → variant choice →
> generate → validate → publish) backed by a Go generator that produces
> a self-referential single-repo bundle (`import.yaml` +
> `zerops.yaml` + code), ready for `buildFromGit:` re-import into a
> fresh project. Always one half of any pair (user-chosen). LLM-driven
> four-category secret classification. Schema-validated output.

---

## 1. Problem

Today's `zerops_workflow action="start" workflow="export"` is a stateless
immediate phase that returns a single 220-line procedural atom
(`internal/content/atoms/export.md:1-229`). The atom tells the agent to
SSH into a container, run platform export, hand-edit YAML to inject
`buildFromGit:` and `zeropsSetup:`, scrub defaults, parameterize secrets
via hardcoded name patterns, write `import.yaml`, commit, and push via
`zerops_deploy strategy="git-push"`. No tool generates the artifact, no
schema validation, no prerequisite chain, no decomposition for standard
pairs (dev + stage). Eight concrete gaps surfaced in the 2026-04-28
investigation:

| # | Layer | Severity | Problem |
|---|---|---|---|
| X1 | Atom prose | 🔴 | 229 lines of procedural prose with hardcoded heuristics (corePackage defaults, secret-name list at L140-148, test-fixture handling at L153-155). Fragile, no validation. |
| X2 | Tool surface | 🔴 | No tool generates `import.yaml`. Agent hand-edits via SSH heredoc (export.md:173-201). Result is unverified before push. |
| X3 | Decomposition | 🔴 | Standard mode (dev + stage pair) unhandled. Atom assumes single container; agent picks whichever happens to be in scope. No "dev or stage?" prompt. |
| X4 | Prereq chain | 🟡 | Publish step (export.md:184-201) implicitly requires `GitPushState=configured`. Missing-prereq case lands as a generic deploy error, not a chained guidance pointer to `setup-git-push-{container,local}`. |
| X5 | Secret detection | 🟡 | Hardcoded list (`APP_KEY`, `SECRET_KEY_BASE`, `JWT_SECRET`, etc., export.md:140-148). Custom secrets miss. Test-fixture values force a mid-export user prompt (export.md:153-155). |
| X6 | zerops.yaml validation | 🟡 | Atom assumes `/var/www/zerops.yaml` exists with matching `setup:` names. Never validated. Re-import failure mode (`zerops-docs/.../import.mdx:791`) is preventable client-side; today it's not. |
| X7 | Subdomain drift | 🟡 | E2E pinned (`e2e/subdomain_lifecycle_test.go:5-9, 76-92`) that `enableSubdomainAccess` in import.yaml does NOT flip API state at import. Atom emits the field from Discover, but next export sees subdomain off and drops it. Bidirectional drift. |
| X8 | Filename | 🟢 | Atom writes `import.yaml` at repo root (export.md:175). Recipe convention is `zerops-project-import.yaml` (recipe-create-recipe docs). User discovery / dashboard upload UX favors the recipe convention. |

The architectural drift propagates as:
- **Tool path**: `internal/tools/workflow_immediate.go:35-38` maps
  `workflow="export"` → `PhaseExportActive` → atom-only response.
  `internal/ops/export.go:40-79` (`ExportProject`) returns raw platform YAML
  + Discover metadata; no transformation, no `buildFromGit:` injection.
- **Atom corpus**: one atom (`export.md`), no decomposition, no chained
  flow, no validation surface.
- **Test surface**: scenarios pin one envelope shape (`scenarios_test.go:882`),
  corpus_coverage pins the atom (`corpus_coverage_test.go:768`); no
  variant coverage, no schema validation pin.

## 2. Goal

A two-tool/four-atom decomposition that:

1. **Asks the user** for variant on standard / local-stage pairs
   (dev or stage; default skip on dev / simple / local-only).
2. **Generates** `import.yaml` + verifies/scaffolds `zerops.yaml` via Go
   code (replaces the bulk of the 229-line prose).
3. **Classifies envs** via LLM-driven four-category protocol (no
   hardcoded name lists).
4. **Schema-validates** against `import-project-yml-json-schema.json`
   + `zerops-yml-json-schema.json` before publishing.
5. **Chains to** `setup-git-push-{container,local}` when
   `GitPushState != configured`.
6. **Refuses cleanly** when live `/var/www/zerops.yaml` is absent
   (chains to a `scaffold-zerops-yaml` atom rather than best-effort
   silent generation).
7. **Surfaces drift limitations** (subdomain, private-repo auth) in
   atom prose rather than pretending to fix them.

8 of 8 root problems resolved in scope; 0 deferred.

## 3. Mental model

### 3.1 The single-repo self-referential shape

Output of a successful export: ONE git repository containing
- `<source code>` — copy of the chosen half's working tree
- `zerops.yaml` at root — one `setup:` block matching the chosen half
- `zerops-project-import.yaml` at root — `project: { ... }` + ONE
  service with `buildFromGit: <THIS_REPO_URL>` and `zeropsSetup:
  <setup-name>`

`buildFromGit` is **self-referential**: the generated import.yaml's
`buildFromGit:` URL points at the same repo it lives in. This is the
genuine inverse of buildFromGit-import for a single concrete project.

NOT a recipe. Recipes are a separate product (multi-repo, registry-published,
app-repo + recipe-repo separation, `cmd/zcp/sync.go:420-448`). The
similar-looking `zerops-project-import.yaml` filename is a deliberate
mirror of recipe convention for discoverability — but the recipe v3
engine is out of scope for this plan.

### 3.2 Always one half (per user decision 2026-04-28)

For a standard or local-stage pair, the export packages ONE half:

- **dev half** picked → import.yaml service has `mode: NON_HA`,
  `hostname: <dev-host>`, `zeropsSetup: <dev-setup-name>`. zerops.yaml
  has the dev setup block (build/run/deploy as dev). Re-imports as a
  dev-shaped single service.
- **stage half** picked → import.yaml service has `mode: NON_HA`,
  `hostname: <stage-host>`, `zeropsSetup: <stage-setup-name>`.
  zerops.yaml has the stage setup block. Re-imports as a single service
  that builds end-to-end from buildFromGit (no dev to cross-deploy from
  in the new project).

Code in repo = chosen half's `/var/www` working tree.

For dev / simple / local-only: only one half exists, no question.

For local-stage: ask same dev/stage question, but local container is the source.

### 3.3 Post-import mode (decision Q7 = β)

| Source half | Re-imported as |
|---|---|
| dev half of standard pair | `mode: dev` (preserves intent: "this was our dev environment") |
| stage half of standard pair | `mode: simple` (no dev to cross-deploy from; collapses cleanly) |
| dev mode (no stage) | `mode: dev` |
| simple mode | `mode: simple` |
| local-stage dev | `mode: dev` |
| local-stage stage | `mode: simple` |
| local-only | `mode: local-only` |

### 3.4 Four-category LLM-driven secret classification

For every env var (project envVariables AND `${var}` references in
zerops.yaml's `run.envVariables`), the agent classifies via grep over
source code:

| Category | Detection | Emit shape |
|---|---|---|
| **Infrastructure-derived** | resolves to managed-service-emitted shape (`${db_connectionString}`, `${redis_hostname}`) | DROP from import.yaml's `project.envVariables`; keeps `${...}` reference in zerops.yaml — re-imported managed services emit fresh values |
| **Auto-generatable secret** | source uses var as encryption/signing key (`Cipher.encrypt(_, env.X)`, `jwt.sign(_, env.X)`) | `<@generateRandomString(32, true, false)>` in `project.envVariables` |
| **External secret** | source calls third-party SDK (Stripe, OpenAI, GitHub, Mailgun) | Comment + placeholder: `# external secret — set in dashboard after import` + `<@pickRandom(["REPLACE_ME"])>` |
| **Plain config** | source uses literal string-shaped (LOG_LEVEL, NODE_ENV, FEATURE_FLAGS) | Verbatim literal value |

The atom describes the protocol with one example per category; the LLM
agent does the grep + classify per env. ZCP's Go code stays out of
classification heuristics.

### 3.5 Phase shape

```
workflow="export"
   │
   ├── Phase A: PROBE (read-only, no git)
   │     1. Resolve scope: which runtime service to export
   │     2. Resolve variant: 
   │           - dev/simple/local-only → no question, single half
   │           - standard/local-stage → ask user "dev or stage?"
   │     3. Probe state: SSH into chosen container; verify /var/www/zerops.yaml
   │        exists; collect project envVariables; collect run.envVariables refs;
   │        read live `git remote get-url origin`
   │     4. Refuse early if zerops.yaml absent → chain to `scaffold-zerops-yaml`
   │
   ├── Phase B: GENERATE (Go code, no git push)
   │     5. Compose `zerops-project-import.yaml` from variant + live state
   │     6. Verify zerops.yaml has matching setup name (chain to scaffold if not)
   │     7. Classify envs (agent runs grep + four-category buckets)
   │     8. Schema-validate both YAMLs against published schemas
   │     9. Surface diff for user review
   │
   └── Phase C: PUBLISH (requires GitPushState=configured)
        10. If GitPushState != configured → chain to setup-git-push-{env}
        11. Refresh ServiceMeta.RemoteURL from live `git remote -v`
        12. SSH chosen container; write yamls; git add/commit/push via
            zerops_deploy strategy="git-push"
        13. Verify push landed; record-deploy
```

### 3.6 Prereq chain (clean, layered)

| Prereq | Required for | Chain target |
|---|---|---|
| Live container SSH-reachable | Phase A | (built-in; container env normal) |
| `/var/www/zerops.yaml` exists with matching setup name | Phase B | `scaffold-zerops-yaml.md` (NEW atom in this plan) |
| `GitPushState=configured` (per chosen pair) | Phase C only | `setup-git-push-{container,local}` (existing atoms) |
| Live `git remote get-url origin` resolves to a URL | Phase C | `setup-git-push-*` (same as above) |

Phase A + Phase B can run with **no git setup at all** — pure
read+generate+validate. Only Phase C (publish) needs git.

## 4. User-confirmed decisions (2026-04-28 dialogue)

| ID | Decision | Status |
|---|---|---|
| Q1 | **Single-runtime per export call.** Multi-runtime users run export per runtime. | ✅ DEFAULTED (challenge in Phase 0 if user wants otherwise) |
| Q2 | **Filename**: `zerops-project-import.yaml` at repo root. Mirrors recipe convention; namespaced; dashboard discovery friendly. | ✅ DEFAULTED |
| Q3 | **Subdomain drift**: document as known limitation in atom prose. Emit `enableSubdomainAccess: true` from Discover where applicable; importer must manually flip subdomain on after import. | ✅ DEFAULTED |
| Q4 | **Secret classification**: four-category LLM-driven (no hardcoded name lists). Atom describes protocol; agent greps source per env. | ✅ CONFIRMED 2026-04-28 |
| Q5 | **Missing live zerops.yaml**: refuse with chain to `scaffold-zerops-yaml.md` atom. NO best-effort silent scaffolding. | ✅ DEFAULTED |
| Q6 | **Code-comparison method**: N/A — variant 3 (both halves) dropped. Always one half, user-chosen. | ✅ N/A |
| Q7 | **Post-import mode**: dev half → `mode: dev`; stage half → `mode: simple`. Preserves user intent (β) per §3.3. | ✅ CONFIRMED 2026-04-28 |
| Pair handling | Always export ONE half of any pair. User chooses dev or stage. | ✅ CONFIRMED 2026-04-28 |
| `RemoteURL` source of truth | Live `git remote get-url origin` (cache in `ServiceMeta.RemoteURL` per `internal/workflow/service_meta.go:47-48`). Refresh cache on every export pass. | ✅ DEFAULTED (matches existing `RemoteURL` semantics) |
| Atom prereq chaining | Handler-side composition, mirrors deploy-strategy-decomposition Phase 5 pattern (`internal/tools/workflow_close_mode.go`-style chaining). | ✅ DESIGN PINNED |

## 5. Baseline snapshot (2026-04-28)

### 5.1 Symbol blast radius (estimated)

- Current `export.md`: 229 lines of prose
- Current `internal/ops/export.go`: 132 lines (raw platform export wrapper)
- Current `internal/tools/workflow_immediate.go`: 39 lines (phase mapping)
- Estimated new code: ~600-900 LOC (generator + classification protocol +
  schema validation + handler + tests)
- Atoms: net +3 to +4 (replace 1, add 4-5, none deleted from outside this plan's scope)

### 5.2 Current entry points (file:line)

- Tool route: `internal/tools/workflow.go:142` (router) → `synthesizeImmediateGuidance` at `:150`
- Phase mapping: `internal/tools/workflow_immediate.go:35-38`
- Synthesizer: `internal/workflow/synthesize.go:524-526` (`SynthesizeImmediatePhase`)
- Plan dispatch: `internal/workflow/build_plan.go:43-48` (PhaseExportActive yields empty plan)
- Atom: `internal/content/atoms/export.md`
- Raw export: `internal/ops/export.go:40-79`
- Router hint: `internal/workflow/router.go:209` (`workflow="export"` offered when bootstrapped+deployed services exist)

### 5.3 Test fixtures inventory

- `internal/workflow/scenarios_test.go:600` — S12 export-active scenario (single-snapshot fixture)
- `internal/workflow/scenarios_test.go:882` — pin-coverage closure includes `export` atom
- `internal/workflow/corpus_coverage_test.go:768` — export envelope coverage
- No e2e tests for export today

### 5.4 Schema URLs (CLAUDE.md `Source of Truth` §)

- import: `https://api.app-prg1.zerops.io/api/rest/public/settings/import-project-yml-json-schema.json`
- zerops.yaml: `https://api.app-prg1.zerops.io/api/rest/public/settings/zerops-yml-json-schema.json`

`internal/schema/` already has parsing infrastructure (per
`internal/schema/schema.go:37-49`).

## 6. Phased execution

> **Pacing rule**: 11 phases (0-10). Each phase ENTRY/WORK-SCOPE/EXIT
> criteria. Codex protocol per §7. Trackers per `phase-N-tracker.md`
> schema (created during execution).
>
> **Critical path discipline**: each phase ends with verify gate green
> (`make lint-local && go test ./... -short -race`). No phase begins
> until prior EXIT criteria satisfied. No "I'll fix it later" — broken
> state at phase boundary = stop, fix, retry.

### Phase 0 — Calibration

**ENTRY**: working tree clean; HEAD on main; this plan committed.

**WORK-SCOPE**:
1. Read this plan end-to-end. Walk §3 mental model + §4 decisions.
2. Verify env: `make setup` → `make lint-fast` green → `go test ./... -short` green.
3. Snapshot baseline:
   - `git rev-parse HEAD > plans/export-buildfromgit/baseline-head.txt`
   - `wc -l internal/content/atoms/export.md internal/ops/export.go internal/tools/workflow_immediate.go > plans/export-buildfromgit/baseline-loc.txt`
   - `grep -rn "PhaseExportActive\|buildFromGit\|zeropsSetup" internal/ docs/ > plans/export-buildfromgit/baseline-callsites.txt`
4. Init tracker dir: `plans/export-buildfromgit/` with `phase-0-tracker.md`.
5. Read live import schema URLs into local cache for Phase 5 schema validation work:
   ```
   curl -s https://api.app-prg1.zerops.io/api/rest/public/settings/import-project-yml-json-schema.json > plans/export-buildfromgit/import-schema.json
   curl -s https://api.app-prg1.zerops.io/api/rest/public/settings/zerops-yml-json-schema.json > plans/export-buildfromgit/zerops-schema.json
   ```
6. **Codex PRE-WORK round** (mandatory): hand Codex this plan + ask:
   - "Validate against current corpus state — any decision in §4 already invalidated by post-2026-04-28 commits?"
   - "Challenge Q1 (single-runtime). Is multi-runtime cheap enough that we should just do it now?"
   - "Challenge Q5 (refuse on missing zerops.yaml). Is best-effort scaffold actually safer in practice?"
   - "Independently verify the four-category classification protocol (§3.4) on a sample real-world Laravel + Node app — does the agent reliably bucket each env correctly?"
   - "Find any rendering pipeline assumption that would break when we replace export.md with multiple atoms (priority ordering, axis matching, primaryHostnames placeholder substitution)."

**EXIT**:
- Baseline files committed to `plans/export-buildfromgit/`
- Codex PRE-WORK APPROVE (or NEEDS-REVISION amendments folded in)
- `phase-0-tracker.md` committed with Codex round transcript
- Verify gate green

**Risk**: LOW (calibration only).

### Phase 1 — Types + tool input/output shape

**ENTRY**: Phase 0 EXIT satisfied.

**WORK-SCOPE**:
1. Add to `internal/topology/types.go`:
   ```go
   // ExportVariant selects which half of a pair to export.
   // Only meaningful for ModeStandard / ModeLocalStage; other modes
   // have a single half and the variant is forced.
   type ExportVariant string
   const (
       ExportVariantUnset ExportVariant = ""
       ExportVariantDev   ExportVariant = "dev"
       ExportVariantStage ExportVariant = "stage"
   )

   // SecretClassification buckets project + service env vars per §3.4.
   type SecretClassification string
   const (
       SecretClassUnset          SecretClassification = ""
       SecretClassInfrastructure SecretClassification = "infrastructure"
       SecretClassAutoSecret     SecretClassification = "auto-secret"
       SecretClassExternalSecret SecretClassification = "external-secret"
       SecretClassPlainConfig    SecretClassification = "plain-config"
   )
   ```
2. Extend `WorkflowInput` in `internal/tools/workflow.go` with:
   - `TargetService string` (the runtime hostname to export)
   - `Variant string` (`dev` | `stage` | empty)
   - `EnvClassifications map[string]string` (per-env user-resolved bucket; empty on Phase A call)
3. No new atom axes (variants are runtime decisions, not atom filters).
4. Pin parser tests for the new types in `internal/topology/types_test.go`.
5. Verify gate green; commit: `topology(P1): add ExportVariant + SecretClassification enums`.

**EXIT**:
- Types compile + lint clean
- Tests pin enum values
- `phase-1-tracker.md` committed

**Risk**: LOW.

### Phase 2 — Generator code (`internal/ops/export_bundle.go`)

**ENTRY**: Phase 1 EXIT satisfied.

**WORK-SCOPE**:
1. New file: `internal/ops/export_bundle.go`. Functions:
   - `BuildBundle(ctx, client, project, scope, variant, classifications) (*ExportBundle, error)` — top-level composition.
   - `composeImportYAML(...)` — produces `project: { name, envVariables: {...} }` + ONE service entry with `buildFromGit:` + `zeropsSetup:` + `enableSubdomainAccess:` from Discover.
   - `composeServiceEnvVariables(...)` — applies four-category classification map; emits literals, preprocessor directives, or drops infrastructure-derived.
   - `verifyOrFetchZeropsYAML(...)` — SSH-reads `/var/www/zerops.yaml` from chosen container; verifies the named setup exists; returns content + the per-setup block.
   - `scrubCorePackageDefaults(...)` — drops fields that match runtime corePackage presets (existing logic from atom prose, ported to Go).
   - `addPreprocessorHeader(...)` — prepends `#zeropsPreprocessor=on` if any `<@...>` directive surfaced.
2. Bundle struct:
   ```go
   type ExportBundle struct {
       ImportYAML       string                          // contents of zerops-project-import.yaml
       ZeropsYAML       string                          // contents of zerops.yaml (may be unchanged from live)
       ZeropsYAMLSource string                          // "live" | "scaffolded"
       RepoURL          string                          // resolved buildFromGit URL
       Variant          topology.ExportVariant
       TargetHostname   string                          // dev or stage hostname picked
       SetupName        string                          // matched setup: in zerops.yaml
       Classifications  map[string]topology.SecretClassification
       Warnings         []string
   }
   ```
3. Stage-only mode mapping per §3.3 (β): when `variant=stage`, `mode: simple` in import.yaml.
4. Tests in `internal/ops/export_bundle_test.go` — unit-test each composer + integration-test BuildBundle against fixture data (mock client/SSH).
5. Verify gate green; commit: `ops(P2): export bundle generator with variant + classification`.

**EXIT**:
- Generator compiles + tests pass
- Coverage on each composer
- `phase-2-tracker.md` committed
- Codex POST-WORK APPROVE on generator behavior (verify edge cases:
  empty envVariables, secret-mid-string, multi-line zerops.yaml setups)

**Risk**: MEDIUM. New Go code; YAML composition edge cases; tests must
cover real-world shapes (Laravel, Node, static, PHP).

### Phase 3 — Tool handler (`internal/tools/workflow_export.go`)

**ENTRY**: Phase 2 EXIT satisfied.

**WORK-SCOPE**:
1. New file: `internal/tools/workflow_export.go`. Handler shape:
   ```go
   func handleExport(ctx, engine, client, projectID, input, runtime) (
       *mcp.CallToolResult, *workflow.SessionMutation, error,
   ) {
       // Phase A: probe + variant choice (return atom guidance asking
       // for variant if not provided)
       // Phase B: generate via ops.BuildBundle (return generated YAMLs
       // + classification ask if envClassifications empty)
       // Phase C: publish via SSH + zerops_deploy strategy="git-push"
       //         (chain to setup-git-push-{env} if GitPushState != configured)
   }
   ```
2. Replace the immediate-phase mapping in
   `internal/tools/workflow_immediate.go:35-38` for `"export"` —
   route to `handleExport` instead of `SynthesizeImmediatePhase`.
3. Handler returns structured response with progressive narrowing:
   - First call (no variant set, no classifications): atom asks for
     variant choice (or skips for non-pair modes)
   - Second call (variant set, no classifications): generated YAMLs
     + atom asks for classification per env
   - Third call (variant + classifications set): PUBLISH; chains to
     setup-git-push if `GitPushState != configured`
4. Reuse existing `chainSetupGitPushGuidance(...)` pattern from
   `internal/tools/workflow_close_mode.go` (or extract to a shared
   helper if it lives in close-mode-only today).
5. Tests in `internal/tools/workflow_export_test.go`:
   - First-call flow (variant prompt)
   - Second-call flow (classification prompt + generated YAML diff)
   - Third-call flow (publish path)
   - GitPushState=unconfigured chain
   - Missing zerops.yaml chain to scaffold atom
6. Update `internal/tools/workflow.go:142` router hint if needed.
7. Verify gate green; commit: `tool(P3): handleExport with phased probe/generate/publish flow`.

**EXIT**:
- Handler compiles + tests pass
- All three phases pin tested
- Chain-to-prereq pin tested
- `phase-3-tracker.md` committed
- Codex POST-WORK APPROVE on handler

**Risk**: MEDIUM. Touches the workflow router; introduces multi-call
state (variant + classifications carried across calls).

### Phase 4 — Atom corpus restructure

**ENTRY**: Phase 3 EXIT satisfied.

**WORK-SCOPE**:

#### Atoms to ADD:

1. **NEW** `export-intro.md` — entry atom; describes the goal in 1-2
   paragraphs; for standard/local-stage modes prompts variant choice
   (dev or stage). Front matter: `phases: [export-active]`,
   `closeDeployModes: [unset, auto, git-push, manual]` (any),
   `gitPushStates: [unconfigured, configured, broken, unknown]` (any).

2. **NEW** `export-classify-envs.md` — describes the four-category
   protocol with one example per category. Includes the grep recipe
   per language family. Front matter: `phases: [export-active]`.

3. **NEW** `export-validate.md` — schema-validation surface; describes
   what failed and what the fix is. Front matter: `phases: [export-active]`,
   `references-fields: [ops.ExportBundle.ImportYAML, ops.ExportBundle.ZeropsYAML, ops.ExportBundle.Warnings]`.

4. **NEW** `export-publish.md` — commit + push protocol. Notes
   GitPushState prereq, chains to setup-git-push if missing. Front
   matter: `phases: [export-active]`, `gitPushStates: [configured]`.

5. **NEW** `export-publish-needs-setup.md` — fires when
   `gitPushStates != configured`. Tells the agent to run
   `setup-git-push-{container,local}` first. Front matter:
   `phases: [export-active]`, `gitPushStates: [unconfigured, broken, unknown]`.

6. **NEW** `scaffold-zerops-yaml.md` — fires when live
   `/var/www/zerops.yaml` is absent. Walks the agent through emitting
   a minimal valid zerops.yaml from runtime-detected fields (type,
   version, ports). Front matter: `phases: [export-active]`. (May also
   be reachable from other phases later if scaffolding becomes a
   shared concern.)

#### Atoms to DELETE:

7. **DELETE** `export.md` (229 lines) — superseded by the five new
   `export-*.md` atoms.

#### Front-matter axis usage:

- All new atoms use existing axes (no new axis introduced).
- Variant is NOT an atom axis (it's a runtime decision).
- Classifications are NOT an atom axis (the atom describes the protocol; the agent fills the map).

#### Tests:

- `corpus_coverage_test.go`: replace export fixtures with new atoms.
- `scenarios_test.go`: add S12 variants (S12a probe, S12b generate, S12c publish).
- `atom_test.go`: pin front-matter + references-fields integrity.
- Pin coverage closure update in `scenarios_test.go:882`.

#### Codex involvement:

- **PER-EDIT MANDATORY** for `export-classify-envs.md` (the four-category
  protocol — load-bearing for agent guidance).
- **PER-EDIT MANDATORY** for `export-publish-needs-setup.md` (chain
  contract semantics).
- **POST-WORK round**: verify no Axis K/L/M/N regressions per recent
  atom hygiene work.

**EXIT**:
- 6 new atoms in corpus + 1 deleted
- All scenario tests pass
- Atom lint passes
- Codex PER-EDIT + POST-WORK APPROVE
- `phase-4-tracker.md` committed

**Risk**: HIGH. Largest user-facing surface change. Atom prose drives
agent behavior end-to-end.

### Phase 5 — Schema validation pass

**ENTRY**: Phase 4 EXIT satisfied.

**WORK-SCOPE**:
1. Read import-schema.json + zerops-schema.json fetched in Phase 0;
   embed via `embed.FS` if not already (per
   `internal/schema/schema.go:37-49`).
2. Add `ValidateImportYAML(content string) []ValidationError` and
   `ValidateZeropsYAML(content string, requiredSetup string) []ValidationError`
   to `internal/schema/`.
3. Wire into `ops.BuildBundle` Phase B (validation step).
4. ValidationError shape includes line/column when JSONSchema permits.
5. Surface via `ExportBundle.Warnings` (non-fatal hints) and
   `ExportBundle.Errors` (blocking).
6. Tests in `internal/schema/validate_test.go`:
   - Valid bundle passes
   - Missing `setup:` in zerops.yaml fails
   - Missing `buildFromGit:` for runtime fails
   - Mismatched `zeropsSetup` ↔ zerops.yaml setup names fails
   - Preprocessor header missing when directives present fails
7. Verify gate green; commit: `schema(P5): import + zerops yaml validation in export bundle`.

**EXIT**:
- Validation compiles + tests pass
- Wired into BuildBundle
- `phase-5-tracker.md` committed
- Codex POST-WORK APPROVE on validation correctness

**Risk**: LOW-MEDIUM. JSONSchema is well-defined; main risk is
mismatch between cached schema and live platform behavior.

### Phase 6 — Prereq chain wiring + RemoteURL refresh

**ENTRY**: Phase 5 EXIT satisfied.

**WORK-SCOPE**:
1. Verify chain logic in `handleExport` correctly composes the
   setup-git-push response when `GitPushState != configured`. Pattern
   matches deploy-strategy-decomposition Phase 5/6 chain composition
   (see `internal/tools/workflow_close_mode.go`-style synthesizer
   composition).
2. Add `refreshRemoteURL(ctx, hostname) (string, error)` helper that
   SSH-reads live `git remote get-url origin` and updates
   `ServiceMeta.RemoteURL` cache. Per `service_meta.go:47-48`,
   live remote is the source of truth; cache is just a hint.
3. Call refresh BEFORE composing buildFromGit URL in BuildBundle.
4. Surface mismatches: if cached RemoteURL differs from live, emit a
   warning in `ExportBundle.Warnings` and use the live value.
5. Tests in `internal/tools/workflow_export_test.go`:
   - Cache hit (RemoteURL matches live)
   - Cache miss (live overrides cache; warning surfaces)
   - Live empty / no remote configured (chains to setup-git-push)
6. Verify gate green; commit: `tool(P6): GitPushState chain + RemoteURL freshness in export`.

**EXIT**:
- Chain pin tested
- RemoteURL refresh pin tested
- `phase-6-tracker.md` committed
- Codex POST-WORK APPROVE on chain logic

**Risk**: MEDIUM. Interacts with existing setup-git-push flow; chain
composition mistakes are user-facing UX failures.

### Phase 7 — Tests (scenarios + corpus_coverage + e2e mock)

**ENTRY**: Phase 6 EXIT satisfied.

**WORK-SCOPE**:
1. Update `scenarios_test.go`:
   - S12 split into S12a (probe variant), S12b (generate + classify),
     S12c (publish + verify), S12d (publish-needs-setup chain)
   - Pin coverage closure includes all new atom IDs
2. Update `corpus_coverage_test.go`:
   - Export envelope coverage for variant=dev / variant=stage / variant=unset
3. Update `bootstrap_outputs_test.go`: confirm export doesn't perturb meta state.
4. Add `internal/tools/workflow_export_test.go` integration paths
   covering all three handler call shapes.
5. Mock e2e: add `integration/export_test.go` using mock client to
   verify the full flow against synthetic project state.
6. Verify gate green; commit: `tests(P7): scenarios + integration coverage for export-buildFromGit`.

**EXIT**:
- All test layers green
- `phase-7-tracker.md` committed

**Risk**: LOW.

### Phase 8 — E2E live verification on eval-zcp

**ENTRY**: Phase 7 EXIT satisfied.

**WORK-SCOPE**:
1. Use `eval-zcp` project (project ID `i6HLVWoiQeeLv8tV0ZZ0EQ`, org
   `Muad`) per `CLAUDE.local.md`.
2. Provision a small test scenario via `ssh zcp "zcli ..."`:
   - One Laravel-style project (php-apache + db + redis) bootstrapped
     in standard mode
   - Live for `>=` 5 minutes; deploy + verify successful
3. Run the new export workflow:
   - First call: variant prompt → pick dev
   - Second call: classification prompt → classify each env
   - Third call: publish → push to remote
4. Verify the exported repo:
   - `git clone <remote>` to a fresh clone
   - Inspect `zerops-project-import.yaml` — `buildFromGit` matches
     remote URL; service shape correct; envVariables parameterized
     per classification
   - Inspect `zerops.yaml` — setup name matches; build/run reasonable
5. Re-import the exported bundle into a fresh test project:
   - Create a NEW project on eval-zcp via `zerops_import`
   - Pass the exported `zerops-project-import.yaml`
   - Wait for deploy
   - Verify the new project comes up healthy (subdomain reachable,
     basic HTTP response)
6. Document any drift, surprises, or platform behavior that diverges
   from atom guidance in `phase-8-tracker.md`.
7. Repeat with stage variant to cover both branches.
8. Cleanup test projects.
9. Commit any atom prose corrections that surface from real-world run.

**EXIT**:
- Two end-to-end runs (dev + stage variants) succeed
- Re-import lands healthy services
- `phase-8-tracker.md` committed with run logs
- Codex POST-WORK reviews the run logs for missed signals

**Risk**: MEDIUM. Live platform interaction; uncovered platform
quirks (private-repo auth, branch defaults, schema drift) may surface.

### Phase 9 — Documentation

**ENTRY**: Phase 8 EXIT satisfied.

**WORK-SCOPE**:
1. Update `docs/spec-workflows.md`:
   - Add or rewrite §X "Export workflow" section.
   - Note: export = inverse of buildFromGit; single-repo
     self-referential; one half per call for pairs;
     LLM-driven secret classification.
   - Add invariants: E1 export always emits exactly one runtime service;
     E2 generated YAMLs schema-valid before publish; E3 GitPushState=configured prereq for Phase C only.
2. Update `CLAUDE.md`:
   - Add invariant: "Export-for-buildFromGit is a single-repo
     self-referential snapshot; always one pair-half; pinned by
     `TestExportBundle_*`. Spec: `docs/spec-workflows.md §X`."
3. Update `docs/spec-knowledge-distribution.md` if atom corpus changes
   warrant notation (5 new atoms + 1 deleted).
4. Verify gate green; commit: `docs(P9): export workflow spec + invariants`.

**EXIT**:
- spec-workflows.md aligned with implementation
- CLAUDE.md invariant added
- `phase-9-tracker.md` committed
- Codex POST-WORK approves docs

**Risk**: LOW.

### Phase 10 — SHIP

**ENTRY**: Phase 9 EXIT satisfied.

**WORK-SCOPE**:
1. Re-run full test suite: `go test ./... -race -count=1`.
2. Re-run lint: `make lint-local`.
3. Composition re-score: count atom byte deltas, scenario coverage
   delta vs Phase 0 baseline.
4. **Codex FINAL-VERDICT round**: hand Codex final state + delta from
   Phase 0 baseline. Ask for SHIP / SHIP-WITH-NOTES / NOSHIP verdict.
   Codex must check:
   - All 8 root problems (X1-X8) resolved per Phase 4-9 work
   - No regression in pre-export flows (deploy-strategy-decomposition
     work still passes)
   - Atom corpus axis hygiene clean
5. Update tracker with verdict.
6. **DO NOT** call `make release` — user controls release timing per
   `CLAUDE.local.md`.
7. Archive plan: `git mv plans/export-buildfromgit-2026-04-28.md plans/archive/`.
8. Move tracker dir: `git mv plans/export-buildfromgit plans/archive/export-buildfromgit`.
9. Write `plans/archive/export-buildfromgit/SHIP-WITH-NOTES.md` (or `SHIP.md`) summarizing landed work, deferred items, Codex round summary.
10. Commit: `PLAN COMPLETE: export-buildFromGit — SHIP`.

**EXIT**:
- Codex SHIP verdict recorded
- Plan + tracker archived
- All gates green
- `phase-10-tracker.md` committed (in-archive)

**Target SHIP outcome**: clean SHIP. SHIP-WITH-NOTES acceptable if any
of:
- Private-repo auth ground truth not yet confirmed (X8 mitigation)
- Subdomain drift not yet normalized (X7 documented as known limitation per Q3)
- Multi-runtime out of scope (Q1 default); separate follow-up plan if user opts in later

## 7. Codex collaboration protocol

| Phase | Codex round | Mandatory? | Scope |
|---|---|---|---|
| 0 | PRE-WORK | **Yes** | Validate plan; challenge Q1/Q5; verify rendering pipeline assumptions; sanity-check classification protocol on real apps |
| 1 | (none) | No | LOW risk |
| 2 | POST-WORK | **Yes** | Generator behavior; YAML composition edge cases |
| 3 | POST-WORK | **Yes** | Handler multi-call state; chain composition |
| 4 | PER-EDIT + POST-WORK | **Yes** | HIGH risk atom corpus; load-bearing classification protocol prose |
| 5 | POST-WORK | **Yes** | Schema validation correctness; live platform schema match |
| 6 | POST-WORK | **Yes** | Chain logic; RemoteURL freshness |
| 7 | (none) | No | LOW risk |
| 8 | POST-WORK | **Yes** | E2E run-log review for missed signals |
| 9 | POST-WORK | Yes | Docs alignment |
| 10 | FINAL-VERDICT | **Yes** | SHIP gate |

Estimated Codex rounds: ~10-11 across plan execution. Heavy on Phases
4 (PER-EDIT) and 8 (E2E review).

**Parallel Codex usage opportunities**:
- Phase 0 PRE-WORK can fan out: one agent challenges decisions, one
  agent independently audits the rendering pipeline, one agent samples
  classification on a real Laravel/Node app.
- Phase 4 PER-EDIT can fan out per atom: parallel reviews of
  export-intro / export-classify-envs / export-validate /
  export-publish / export-publish-needs-setup / scaffold-zerops-yaml.
- Phase 8 POST-WORK can fan out: one agent reviews dev variant log,
  one stage variant log, one re-import behavior.

## 8. Acceptance criteria (G1-G9 ship gates)

- **G1**: All 11 phases (0-10) closed per §6 EXIT criteria.
- **G2**: Full test suite green (`go test ./... -race -count=1` + `make lint-local`).
- **G3**: All 8 root problems (X1-X8) addressed (per §1 table). Subdomain drift (X7) acceptable as documented limitation if Codex agrees.
- **G4**: Codex FINAL-VERDICT = SHIP or SHIP-WITH-NOTES.
- **G5**: E2E run on eval-zcp succeeds for both dev and stage variants; re-imported project boots healthy.
- **G6**: All atom front-matter axes lint-clean (Axis K/L/M/N from prior cycles).
- **G7**: spec-workflows.md + CLAUDE.md aligned with new model.
- **G8**: Zero references to old `export.md` atom anywhere in `internal/` or `docs/`.
- **G9**: Generated `zerops-project-import.yaml` parses cleanly against the published `import-project-yml-json-schema.json` (Phase 5 pin).

## 9. Out of scope (deferred)

1. **Multi-runtime export in one bundle.** Q1 default = single-runtime per call. If real demand surfaces (multi-app monorepo with shared deploys), a follow-up plan can lift the constraint via per-service repo arrays in import.yaml. Not blocking for this plan.

2. **Subdomain drift active normalization.** Q3 default = document as known limitation. Active normalization (re-flipping subdomain on after import) requires platform API work + ongoing background reconciler — out of scope.

3. **Private-repo buildFromGit auth.** zerops-docs has no documented authentication mechanism for buildFromGit on private repos (`zerops-docs/.../references/import.mdx:398-402` shows public URLs only; `zerops-docs/.../github-integration.mdx:14-39` covers continuous deployment, not one-time import). Phase 8 may surface ground truth from the platform team; if private-repo auth has a known shape, encode it in atom prose. Otherwise document as a public-repo-only feature.

4. **Recipe v3 engine integration.** Recipes (`cmd/zcp/sync.go:420-448`) are a separate product — multi-repo, registry-published. The shared primitives (buildFromGit, zerops.yaml at root) make some code reuse possible but the user-facing intent differs. Out of scope; the export workflow does NOT route to recipe-publish.

5. **Dry-run import via platform API.** `internal/platform/client.go:45-46` ImportServices has no dry-run parameter; `zcli project-import` doesn't expose one (`zerops-docs/.../zcli/commands.mdx:143-154`). The `dryRun: true` claim in `zerops-docs/knowledge/zerops-complete-knowledge.md:77-80` is not corroborated. Phase 5 schema-only validation is the strongest available client-side gate; deeper validation is a platform-team request.

6. **Scaffold-zerops-yaml automation.** `scaffold-zerops-yaml.md` atom in Phase 4 walks the agent through manual emission. Auto-scaffolding from runtime info is a separate effort that overlaps with bootstrap workflow primitives.

## 10. Anti-patterns + risks

- **Don't merge phases**: each phase ends in clean state for verifiability. Combining Phase 2 (generator) and Phase 3 (handler) loses the safety of incremental verification.
- **Don't skip Codex PER-EDIT rounds on Phase 4**: HIGH-risk atom changes need second eyes. The classification protocol in `export-classify-envs.md` is load-bearing for agent behavior; a wrong example can mis-train every downstream export run.
- **Don't best-effort scaffold zerops.yaml on missing**: Q5 says refuse + chain. Silent best-effort scaffolding creates broken imports that fail at re-import time with platform errors that are hard to map back to a missing live zerops.yaml.
- **Don't hardcode secret-name lists**: Q4 = LLM-driven classification. Hardcoded lists were the main failure mode of the old atom (export.md:140-148). Resist the urge to add "just one more name" — bias toward improving the protocol prose instead.
- **Don't conflate variant choice with mode-expansion**: when stage half is picked and re-imports as `mode: simple` (β), the imported project is NOT a degraded standard pair — it's a clean standalone. Atom prose must communicate this clearly so users don't expect dev cross-deploy in the new project.
- **Don't write `import.yaml` to `/var/www/import.yaml` (legacy convention)**: Q2 = `zerops-project-import.yaml` at repo root. Mirrors recipe convention; less ambiguous in dashboard upload UX.
- **Don't assume `git remote get-url origin` returns success**: live container may have no remote configured (push-dev only project). Phase A must handle this and chain to setup-git-push when it returns empty.
- **Don't assume `ServiceMeta.RemoteURL` is fresh**: it's a CACHE per `service_meta.go:47-48`. Refresh from live `git remote -v` before composing buildFromGit URL (Phase 6 pin).
- **Don't break the prereq layering**: Phase A + Phase B run with no git setup. Only Phase C needs `GitPushState=configured`. Don't add a global "must have git-push configured to run export at all" gate — it would block users who want to validate their export shape before committing to a publish.
- **Don't forget two paths for SSH**: container env uses `ssh <hostname>`; local env uses local filesystem. The atom corpus must distinguish (`environments: [container]` vs `[local]` axis).
- **Don't commit phase EXIT without verify gate green**: rebuild discipline is the load-bearing safety net.
- **Don't leak preprocessor directives into zerops.yaml**: directives belong in the import.yaml only (per `zerops-docs/.../import-yaml/pre-processor.mdx:8-17`). `<@generateRandomString>` in zerops.yaml's run.envVariables would not be processed.
- **Don't treat `scaffold-zerops-yaml` as a fallback**: it's a CHAIN target. The agent runs it, completes the prerequisite, then re-runs export. Phase 4 atom prose must make this loop explicit (not "you can also try scaffolding" — but "scaffold first, then re-run export").

## 11. First moves for fresh instance

**Step 0 — prereq verification**:
1. `git status` → clean tree
2. `git rev-parse HEAD` → main branch
3. `make lint-fast` → green
4. `go test ./... -short` → green

**Step 1 — read context**:
1. This plan end-to-end (top to bottom; do not skim).
2. `plans/archive/deploy-strategy-decomposition-2026-04-28.md` — sister plan structure + Codex protocol patterns + chain-composition reference.
3. `plans/archive/strategy-decomp/SHIP-WITH-NOTES.md` — completion notes from the precursor plan; reuse patterns where applicable.
4. `internal/topology/types.go` — current topology vocabulary.
5. `internal/content/atoms/export.md` — the atom this plan replaces.
6. `internal/ops/export.go` — current raw export wrapper.
7. `internal/tools/workflow_immediate.go` + `internal/tools/workflow.go` — current tool wiring.
8. `internal/workflow/synthesize.go::SynthesizeImmediatePhase` — what stateless workflows look like today.
9. `internal/tools/workflow_close_mode.go` (or wherever the chain-composition pattern lives) — how to compose chained guidance from a handler.
10. `zerops-docs/apps/docs/content/references/import.mdx` — import schema authority.
11. `zerops-docs/apps/docs/content/zerops-yaml/specification.mdx` — zerops.yaml authority.
12. `zerops-docs/apps/docs/content/references/import-yaml/pre-processor.mdx` — preprocessor directive shapes.
13. CLAUDE.md + CLAUDE.local.md — project conventions; live env config.

**Step 2 — initialize tracker dir**: `mkdir -p plans/export-buildfromgit/` with `phase-0-tracker.md`.

**Step 3 — Phase 0 PRE-WORK Codex round** (parallel fan-out per §7) validating the plan against current corpus state. Phase 1 starts only after APPROVE.

**Step 4 — Begin Phase 1** (topology types).

## 12. Open questions / TBD

All 7 design questions answered as defaults in §4. Each is challengeable in Phase 0 PRE-WORK Codex round; if any flips, this plan's structure adapts (most flips affect Phase 4 atom prose, not the phase shape).

If Phase 8 E2E surfaces platform behavior diverging from this plan's assumptions, document in `phase-8-tracker.md` and amend this plan's §3.5 (phase shape) or §4 (decisions) accordingly. Plan amendment requires a follow-up commit; the plan is not frozen post-Phase-0.
