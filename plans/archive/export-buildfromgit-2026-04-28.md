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
| X9 | Surface duplication | 🟢 | Standalone `zerops_export` MCP tool at `internal/tools/export.go:11-41` coexists with the workflow-phase atom path. The tool returns raw platform YAML + Discover metadata; the workflow guides a re-importable bundle. RETAINED as orthogonal raw-export surface per §4 decision 2026-04-28 (Codex Agent A). Not a regression — orthogonal-surface clarification only. |

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

8 of 9 root problems resolved in scope; X9 (surface duplication) is a
clarification only (RETAIN orthogonal surface) — not a code change.

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

### 3.3 Import service scaling mode and topology metadata (revised in Phase 5)

`services[].mode` in `zerops-project-import.yaml` is the Zerops platform scaling enum, NOT ZCP topology. The published schema accepts only `HA` / `NON_HA` (`internal/schema/testdata/import_yml_schema.json:199-205`); single-runtime export bundles emit `mode: NON_HA` for every source topology because there is no `verticalAutoscaling` + `minContainers` declaration accompanying the runtime entry to justify HA.

| Source half | Import `services[].mode` | Preserved bundle metadata |
|---|---|---|
| dev half of standard pair | `NON_HA` | `variant=dev`, chosen hostname, matched `zeropsSetup` |
| stage half of standard pair | `NON_HA` | `variant=stage`, chosen hostname, matched `zeropsSetup` |
| dev / simple / local-only | `NON_HA` | chosen hostname, matched `zeropsSetup` |
| local-stage dev / stage | `NON_HA` | `variant=dev` or `variant=stage`, chosen hostname, matched `zeropsSetup` |

**Phase 0 plan author's intent** was to encode topology-level "this was our dev environment" semantics in the bundle. Phase 5 schema validation surfaced that the `mode` field is not the right carrier — topology Mode is established by ZCP's bootstrap when the destination project is created, not by import.yaml content. The bundle preserves topology context via `bundle.variant` + `bundle.targetHostname` + `bundle.setupName` for downstream tooling (e.g., a future "import + bootstrap" companion that sets the destination project's topology Mode after re-import).

### 3.4 Four-category LLM-driven secret classification

For every env var (project envVariables AND `${var}` references in
zerops.yaml's `run.envVariables`), the agent classifies by combining
source grep, zerops.yaml reference provenance, and framework/config-file
reads. Grep is evidence, not the whole classifier.

| Category | Detection | Emit shape |
|---|---|---|
| **Infrastructure-derived** | value or component comes from a recognized managed-service reference in zerops.yaml or platform export metadata (`${db_*}`, `${redis_*}`, plus documented service-specific prefixes such as Mongo / Postgres / MySQL variants). Includes compound URLs assembled in app code from `${...}` components (`DATABASE_URL=postgres://${DB_USER}:${DB_PASSWORD}@${DB_HOST}/${DB_NAME}`). | DROP from import.yaml's `project.envVariables`; keep `${...}` reference in zerops.yaml — re-imported managed services emit fresh values |
| **Auto-generatable secret** | source or framework convention uses var as local encryption/signing key (`Cipher.encrypt(_, env.X)`, `jwt.sign(_, env.X)`, Laravel `APP_KEY`, Django `SECRET_KEY`, Rails `SECRET_KEY_BASE`, Express session/JWT secrets — even when the encryption call is inside the framework). Warn before regenerating when state, cookies, sessions, or test fixtures may rely on the old value. | `<@generateRandomString(32, true, false)>` in `project.envVariables` + stability warning |
| **External secret** | source calls third-party SDK (Stripe, OpenAI, GitHub, Mailgun) using the var, including aliased imports (`from stripe import Stripe as PaymentProvider`) and webhook verification secrets (`stripe.webhooks.constructEvent(_, _, env.X)`). Empty / sentinel external values (`STRIPE_SECRET=`, `disabled`, `test_xxx`, `sk_test_*`) are review-required — do NOT blindly substitute `REPLACE_ME` for an empty staging key. | Comment + placeholder: `# external secret — set in dashboard after import` + `<@pickRandom(["REPLACE_ME"])>`; OR empty + comment when the live value was empty/sentinel |
| **Plain config** | source uses var as literal string-shaped runtime config (LOG_LEVEL, NODE_ENV, FEATURE_FLAGS). Flag privacy-sensitive literals (real emails, customer names, internal domain/webhook URLs, sender identities like `MAILGUN_FROM="Acme Support <support@acme.com>"`) for user review before verbatim emission. | Verbatim literal value (unless privacy-flagged) |

The atom describes the protocol with worked examples per category; the
LLM agent does grep + provenance + framework reasoning per env. ZCP's
Go code stays out of classification heuristics.

**Phase B emits a per-env review table** — env var, evidence (grep
match / `${...}` provenance / framework convention), bucket, emitted
value, risk note, user override status. Phase C does NOT proceed until
the user has accepted or corrected the table. This is the recovery
path for misclassification surfaced by Codex Agent C 2026-04-28
(failure modes M1 aliased imports, M2 indirect resolution, M3
multi-purpose framework keys, M4 empty-sentinel external secrets, M5
privacy-sensitive plain config, M6 test fixtures, M7 non-default
managed prefixes).

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
   │     7. Classify envs (agent runs grep + provenance + framework reasoning)
   │     8. Schema-validate both YAMLs against published schemas
   │     9. Emit per-env review table; user accepts or corrects classifications
   │        before Phase C (mandatory gate per Codex Agent C 2026-04-28)
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
| Q7 | **Post-import service `mode`**: always `NON_HA` for single-runtime bundles (Zerops platform schema enforces `HA`/`NON_HA` only). Topology metadata (variant, hostname, setupName) preserved on the bundle for downstream tooling. Phase 0's β decision (`mode: dev`/`mode: simple`) was based on a topology/scaling-mode conflation surfaced by Phase 5 schema validation; revised in §3.3. | ✅ REVISED 2026-04-29 (Phase 5 amendment) |
| Pair handling | Always export ONE half of any pair. User chooses dev or stage. | ✅ CONFIRMED 2026-04-28 |
| `RemoteURL` source of truth | Live `git remote get-url origin` (cache in `ServiceMeta.RemoteURL` per `internal/workflow/service_meta.go:48`). Refresh cache on every export pass. | ✅ DEFAULTED (matches existing `RemoteURL` semantics) |
| Atom prereq chaining | Handler-side composition. The pattern referenced as `chainSetupGitPushGuidance(...)` is INLINE at `internal/tools/workflow_close_mode.go:120-136` (no helper); export Phase 3 either reuses inline or extracts a shared helper as optional Phase 2.5. Chain pointers land in the response payload's `nextSteps` list — NOT via a `gitPushStates` atom axis (`SynthesizeImmediatePhase` passes no service context, so service-scoped axes silently never fire). | ✅ DESIGN PINNED 2026-04-28 (Codex Agent A+B) |
| `zerops_export` standalone MCP tool | RETAIN as orthogonal raw export. `internal/tools/export.go:11-41` + `internal/server/server.go:197` registers the tool; it wraps `ops.ExportProject` for raw platform YAML. The new workflow handler is independent. Do NOT remove or redirect; the two surfaces serve different intents (raw debugging vs guided export). | ✅ DEFAULTED 2026-04-28 (Codex Agent A) |
| JSON Schema validator for Phase 5 | Vendor `github.com/santhosh-tekuri/jsonschema/v5` for full schema validation. Current `internal/schema/validate.go::ValidateZeropsYmlRaw` only does unknown-field detection against extracted enums. Phase 5 adds `ValidateImportYAML` + `ValidateZeropsYAML` over the live JSON Schema. Embedded `internal/schema/testdata/import_yml_schema.json` is 202B behind live (`plans/export-buildfromgit/import-schema.json`); refresh testdata as part of Phase 5. | ✅ DESIGN PINNED 2026-04-28 (Codex Agent A) |

## 5. Baseline snapshot (2026-04-28)

### 5.1 Symbol blast radius (estimated)

- Current `export.md`: 229 lines of prose
- Current `internal/ops/export.go`: 132 lines (raw platform export wrapper)
- Current `internal/tools/workflow_immediate.go`: 39 lines (phase mapping)
- Estimated new code: ~600-900 LOC (generator + classification protocol +
  schema validation + handler + tests)
- Atoms: net +3 to +4 (replace 1, add 4-5, none deleted from outside this plan's scope)

### 5.2 Current entry points (file:line) — verified 2026-04-28 at HEAD `b743cda0`

- Tool route: `internal/tools/workflow.go:144` (`IsImmediateWorkflow` gate) → `synthesizeImmediateGuidance` call at `:150`
- Standalone MCP tool: `internal/tools/export.go:19-35` (`zerops_export` registration); `internal/server/server.go:197` (server registration call site). Stays after this plan per §4 decision row.
- Phase mapping: `internal/tools/workflow_immediate.go:36` (`workflow="export"` → `PhaseExportActive`)
- Synthesizer: `internal/workflow/synthesize.go:524-525` (`SynthesizeImmediatePhase`); composes ALL matching atoms via priority-then-ID sort (`synthesize.go:51-81`, `atom.go:438-449`)
- Plan dispatch: `internal/workflow/build_plan.go:43-44` (PhaseExportActive yields empty plan)
- Atom: `internal/content/atoms/export.md` (229 lines, `priority: 2`, `phases: [export-active]`, `environments: [container]`)
- Raw export: `internal/ops/export.go:40-79` (`ExportProject` — raw platform YAML + Discover wrapper, no transformation)
- Router hint: `internal/workflow/router.go:208-209` (`workflow="export"` offered when bootstrapped+deployed services exist)
- S12 scenario: `internal/workflow/scenarios_test.go:589-618` (`TestScenario_S12_ExportActiveEmptyPlan` — currently asserts EXACTLY ONE atom: `export`)
- Corpus coverage fixture: `internal/workflow/corpus_coverage_test.go:766-779` (`Name: "export_active"`, `MustContain: ["buildFromGit", "zerops_export", "import.yaml"]`); Phase 4 must update `MustContain` to `"zerops-project-import.yaml"`
- Atom-corpus axis parsers: `internal/workflow/atom.go:131-133` (`closeDeployModes` / `gitPushStates` / `buildIntegrations` already in `validAtomFrontmatterKeys`); `:201`/`:207`/`:213` per-axis. **No parser-extension Phase 1.0 needed** (in contrast to sister plan).
- Handler-state guard: `TestNoCrossCallHandlerState` at `internal/topology/architecture_handler_state_test.go:117-149` (forbids zero-value package-level vars in `internal/tools/`)
- Atom reference-field integrity: `internal/workflow/atom_reference_field_integrity_test.go:17-57` — Phase 4 atoms cannot declare `references-fields: [ops.ExportBundle.*]` until Phase 2 lands the struct
- Template-var substitution: `internal/workflow/synthesize.go:419-480` — whitelist tokens are `{repoUrl}` (not `{repoURL}`) and `{targetHostname}`

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

**GATE**: Phase 2 MUST land `ops.ExportBundle` (the struct shape below)
before Phase 4 atom prose declares `references-fields:
[ops.ExportBundle.*]`. `TestAtomReferenceFieldIntegrity` at
`internal/workflow/atom_reference_field_integrity_test.go:17-57` fails
at lint time when an atom references a non-existent field path. Plan
phase order respects this — pinned explicitly per Codex Agent B
2026-04-28.

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
3. Service `mode` is always `NON_HA` per revised §3.3 (single-runtime bundles cannot justify HA without explicit scaling fields; the platform schema enforces `HA`/`NON_HA` only). Topology context preserved on `ExportBundle.Variant` + `TargetHostname` + `SetupName`.
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
3. Handler returns structured response with progressive narrowing.
   `WorkflowInput.Variant` and `WorkflowInput.EnvClassifications` are
   **per-request inputs supplied by the agent on every call** — NOT
   server-side state. `TestNoCrossCallHandlerState`
   (`internal/topology/architecture_handler_state_test.go:117-149`,
   CLAUDE.md "Stateless STDIO tools" invariant) forbids package-level
   mutable handler state. The agent threads the values across the
   three calls. Pinned explicitly per Codex Agent B 2026-04-28:
   - First call (no variant set, no classifications): atom asks for
     variant choice (or skips for non-pair modes)
   - Second call (variant set, no classifications): generated YAMLs
     + atom asks for classification per env (per-env review table
     per §3.4 + §3.5 Phase B)
   - Third call (variant + classifications set, user-accepted):
     PUBLISH; chains to setup-git-push if `GitPushState != configured`
4. Chain composition pattern: `chainSetupGitPushGuidance(...)` does
   NOT exist as a function — the close-mode chaining is INLINE
   `nextSteps` construction at
   `internal/tools/workflow_close_mode.go:120-136` (`setupPointers =
   append(setupPointers, fmt.Sprintf(...))`). Phase 3 either reuses
   the inline pattern OR extracts a shared helper as an optional
   Phase 2.5 prep (recommended if the close-mode + git-push-setup +
   build-integration handlers also benefit). Chain pointers land in
   the response payload's `nextSteps` list — NOT via atom front
   matter (`SynthesizeImmediatePhase` doesn't pass services, so
   service-scoped axes silently never fire).
5. Tests in `internal/tools/workflow_export_test.go`:
   - First-call flow (variant prompt)
   - Second-call flow (classification prompt + generated YAML diff
     + per-env review table)
   - Third-call flow (publish path)
   - GitPushState=unconfigured chain (handler returns `nextSteps`
     pointer to setup-git-push, no atom-axis match required)
   - Missing zerops.yaml chain to scaffold atom (same pattern)
6. Update `internal/tools/workflow.go` router hint at `:144`/`:150`
   if needed.
7. Verify gate green; commit: `tool(P3): handleExport with phased probe/generate/publish flow`.

**EXIT**:
- Handler compiles + tests pass
- All three phases pin tested
- Chain-to-prereq pin tested
- `phase-3-tracker.md` committed
- Codex POST-WORK APPROVE on handler

**Risk**: MEDIUM. Touches the workflow router; multi-call flow
threads state via per-request inputs (no server-side state introduced).

### Phase 4 — Atom corpus restructure

**ENTRY**: Phase 3 EXIT satisfied.

**WORK-SCOPE**:

#### Atoms to ADD:

> **Priority discipline**: Render order is `priority` ASC then ID ASC
> (`internal/workflow/synthesize.go:51-81`, `atom.go:438-449`). Omitted
> `priority:` defaults to `5` — six new atoms at the same default
> would collide. Explicit priorities below pin a deterministic order
> per Codex Agent B 2026-04-28.

1. **NEW** `export-intro.md` (`priority: 1`) — entry atom; describes
   the goal in 1-2 paragraphs; for standard/local-stage modes prompts
   variant choice (dev or stage). Front matter: `phases: [export-active]`.
   No `closeDeployModes` / `gitPushStates` axes — these are
   service-scoped and `SynthesizeImmediatePhase` doesn't pass services,
   so they would silently never fire.

2. **NEW** `export-classify-envs.md` (`priority: 2`) — describes the
   four-category protocol per §3.4 (provenance + framework + grep), one
   worked example per category, M1–M7 mitigation patterns from Codex
   Agent C, and the per-env review-table format (env / evidence / bucket
   / emit / risk note / override). Front matter: `phases: [export-active]`.

3. **NEW** `export-validate.md` (`priority: 3`) — schema-validation
   surface; describes what failed and what the fix is. Front matter:
   `phases: [export-active]`, `references-fields: [ops.ExportBundle.ImportYAML, ops.ExportBundle.ZeropsYAML, ops.ExportBundle.Warnings]`.
   **Lands AFTER Phase 2** (struct gate per Phase 2).

4. **NEW** `export-publish.md` (`priority: 4`) — commit + push protocol
   when `GitPushState=configured`. Front matter: `phases: [export-active]`.
   Uses template vars `{repoUrl}` (lowercase `Url`) and `{targetHostname}` —
   `{repoURL}` (capital URL) is unknown to the synth pipeline
   (`internal/workflow/synthesize.go:419-480`). Distinguish container
   vs local SSH push paths with prose markers
   `<!-- axis-n-keep: signal-#N -->` per Axis N (`internal/content/atoms_lint_axes.go:256-278`)
   instead of bare `container env` / `local env` tokens.

5. **NEW** `export-publish-needs-setup.md` (`priority: 5`) — fires when
   chain composition lands a pointer to setup-git-push in the handler's
   `nextSteps` list. Front matter: `phases: [export-active]` ONLY. Do
   NOT add `gitPushStates: [unconfigured, broken, unknown]` —
   `SynthesizeImmediatePhase` passes no service context, so this axis
   silently never fires. The handler routes to this atom via response
   payload, not via atom-axis match (per Codex Agent A+B 2026-04-28).

6. **NEW** `scaffold-zerops-yaml.md` (`priority: 6`) — fires when live
   `/var/www/zerops.yaml` is absent. Walks the agent through emitting
   a minimal valid zerops.yaml from runtime-detected fields (type,
   version, ports). Front matter: `phases: [export-active]`. Same
   axis-marker discipline as `export-publish.md` for SSH-path prose.
   (May also be reachable from other phases later if scaffolding becomes
   a shared concern.)

#### Atoms to DELETE:

7. **DELETE** `export.md` (229 lines) — superseded by the six new
   `export-*.md` / `scaffold-zerops-yaml.md` atoms.

#### Front-matter axis usage:

- All new atoms use existing axes (no new axis introduced).
- Variant is NOT an atom axis (it's a runtime decision).
- Classifications are NOT an atom axis (the atom describes the protocol; the agent fills the map).
- `gitPushStates` / `closeDeployModes` / `buildIntegrations` are NOT used on export atoms — `SynthesizeImmediatePhase` passes no service context. Chain-target routing is via handler response payload.

#### Tests:

- `corpus_coverage_test.go:766-779`: update the `export_active` fixture's
  `MustContain` from `["buildFromGit", "zerops_export", "import.yaml"]`
  to include `"zerops-project-import.yaml"` (the new filename per §4
  decision Q2). Optionally retain `zerops_export` if the standalone tool
  is mentioned in atom prose; otherwise drop. Per Codex Agent A+B.
- `scenarios_test.go:589-618`: split `TestScenario_S12_ExportActiveEmptyPlan`
  into `S12a` (probe variant), `S12b` (generate + classify),
  `S12c` (publish), `S12d` (publish-needs-setup chain).
- `atom_test.go`: pin front-matter + `references-fields` integrity (gate
  enforced once Phase 2 lands `ops.ExportBundle`).
- Pin coverage closure update in `scenarios_test.go:881`.

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

**REALITY CHECK** (Codex Agent A 2026-04-28): no JSON Schema library is
currently vendored in `go.mod`. `internal/schema/validate.go` only does
unknown-field detection against extracted enums (`ValidateZeropsYmlRaw`
at `:54-57`), NOT full JSON Schema validation. Phase 5 effort is
materially larger than the original draft implied.

**WORK-SCOPE**:
1. **Library choice**. Vendor `github.com/santhosh-tekuri/jsonschema/v5`
   (mature, draft-2020-12 compliant, no cgo, BSD-2). Alternative:
   hand-roll per-rule validation against a curated allowlist of
   required fields + enums. Default: vendor the lib for full
   coverage; the per-rule path is fallback if vendoring is rejected.
2. **Embed the live schemas via `embed.FS`**. Refresh
   `internal/schema/testdata/import_yml_schema.json` from
   `plans/export-buildfromgit/import-schema.json` (live is 202B newer
   than embedded; `zerops_yml_schema.json` is identical). Existing
   test data path is `internal/schema/testdata/` per
   `internal/schema/schema.go:15-16` (`ImportYmlURL`, `ZeropsYmlURL`).
3. Add `ValidateImportYAML(content string) []ValidationError` and
   `ValidateZeropsYAML(content string, requiredSetup string) []ValidationError`
   to `internal/schema/validate.go`. ValidationError shape includes
   path / message; line/column when the parser surfaces it.
4. Wire into `ops.BuildBundle` Phase B (validation step).
5. Surface via `ExportBundle.Warnings` (non-fatal hints) and
   `ExportBundle.Errors` (blocking).
6. Tests in `internal/schema/validate_test.go`:
   - Valid bundle passes
   - Missing `setup:` in zerops.yaml fails
   - Missing `buildFromGit:` for runtime fails
   - Mismatched `zeropsSetup` ↔ zerops.yaml setup names fails
   - Preprocessor header missing when directives present fails
   - Refresh-drift test: embedded schema must match live (CI hint)
7. Verify gate green; commit: `schema(P5): import + zerops yaml validation in export bundle`.

**EXIT**:
- JSON Schema lib vendored OR per-rule fallback landed
- Validation compiles + tests pass
- Wired into BuildBundle
- Embedded schemas refreshed
- `phase-5-tracker.md` committed
- Codex POST-WORK APPROVE on validation correctness

**Risk**: MEDIUM. JSONSchema lib vendor is a `go.mod` change; per-rule
fallback risks coverage gaps; both need careful test design. Schema
drift (live vs embedded) is a recurring maintenance cost — refresh
cadence not yet pinned.

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
- **Don't conflate variant choice with platform scaling mode**: revised §3.3 — `services[].mode` in import.yaml is the Zerops platform's scaling enum (`HA`/`NON_HA`), not ZCP topology. Single-runtime bundles always emit `NON_HA`. Topology context (variant, hostname, setupName) lives on the bundle metadata, not in the rendered YAML's `mode:` field. Atom prose must NOT claim "dev variant re-imports as mode=dev" (Phase 0 plan author's β confusion, fixed in Phase 5).
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

## 13. Phase 0 PRE-WORK amendments (Codex round, 2026-04-28)

Three parallel Codex agents (`plans/export-buildfromgit/codex-round-p0-prework-{decisions,rendering,classification}.md`). Convergent verdict: NEEDS-REVISION → in-place amendments folded → effective APPROVE per sister-plan §10.5 work-economics rule.

| # | source | section touched | amendment |
|---|---|---|---|
| 1 | A | §1 + §4 | Add row X9: standalone `zerops_export` MCP tool RETAINED as orthogonal raw-export surface. |
| 2 | A | §4 | Add JSON Schema validator decision row: vendor `github.com/santhosh-tekuri/jsonschema/v5`. |
| 3 | A+B | §4 + §6 P3 | Reframe atom prereq chaining: pattern is INLINE at `workflow_close_mode.go:120-136`, not a `chainSetupGitPushGuidance(...)` helper. Optional Phase 2.5 extracts a shared helper. |
| 4 | A+B | §6 P3 + P4 | Drop `gitPushStates` axis from `export-publish-needs-setup.md`. `SynthesizeImmediatePhase` doesn't pass services, so service-scoped axes silently never fire. Chain routing via handler response payload. |
| 5 | B | §6 P4 | Declare explicit `priority:` on all six new atoms (1–6). Default 5 collision is non-deterministic. |
| 6 | B | §6 P4 | Use `{repoUrl}` (lowercase Url), not `{repoURL}`. Synth pipeline whitelist at `synthesize.go:419-480`. |
| 7 | B | §6 P3 | Clarify `WorkflowInput.Variant` + `EnvClassifications` are per-request inputs (stateless). Cite `TestNoCrossCallHandlerState` at `architecture_handler_state_test.go:117-149`. |
| 8 | B | §6 P2 | Pin Phase 2 → Phase 4 GATE: `ops.ExportBundle` lands BEFORE atom `references-fields:` declarations. `TestAtomReferenceFieldIntegrity` at `atom_reference_field_integrity_test.go:17-57` enforces. |
| 9 | A+B | §6 P4 + P7 | `corpus_coverage_test.go:766-779` `MustContain` updates to include `"zerops-project-import.yaml"`. S12 split (a/b/c/d) per Phase 7. |
| 10 | A | §6 P5 | Reshape Phase 5 to acknowledge no JSON Schema lib vendored; vendor + refresh embedded `import_yml_schema.json` (live is 202B newer). |
| 11 | C | §3.4 | Five surgical edits to category descriptions: provenance + framework reasoning + aliased-import handling + empty/sentinel review-required + privacy-flag for plain config. |
| 12 | C | §3.4 + §3.5 | Add Phase B per-env review table (env / evidence / bucket / emit / risk / override). Phase C blocked until user accepts or corrects. |
| 13 | A | §5.2 | Citation hygiene: `workflow.go:142` → `:144`/`:150`; `corpus_coverage_test.go:768` → `:766-779` with explicit `:778` `MustContain` note; `scenarios_test.go:600` → `:589-618`; full entry-points table refreshed. |

All 13 amendments folded into the plan in-place. No structural redesign required. Phase 1 may enter on user explicit go (per session pause instruction).

## 14. Phase 2 POST-WORK amendments (Codex round, 2026-04-28)

Two parallel Codex agents (`plans/export-buildfromgit/codex-round-p2-postwork-{generator,architecture}.md`). Convergent verdict: NEEDS-REVISION → in-place amendments folded → effective APPROVE per §10.5 work-economics rule.

### 14.1 Phase 2 implementation deviations from plan §6 Phase 2 step 1 (acknowledged)

| plan name | implementation | ruling |
|---|---|---|
| `composeServiceEnvVariables` | renamed `composeProjectEnvVariables` | CORRECT — §3.4 four-category protocol applies to project-level envs, not service-level (which are platform-injected on managed services and zerops.yaml-resolved on runtime). |
| `verifyOrFetchZeropsYAML` | pure `verifyZeropsYAMLSetup` (no SSH) | CORRECT — Phase A handler does the SSH read; Phase B generator stays pure. Matches §3.5 phase split. |
| `scrubCorePackageDefaults` | OMITTED | CORRECT — minimal bundle shape (hostname/type/mode/buildFromGit/zeropsSetup/subdomain + managed services with hostname/type/mode/priority) doesn't emit fields needing scrubbing. The plan's helper list was aspirational. |
| `BundleInputs.ManagedServices []ManagedServiceEntry` | NEW field on inputs | CORRECT — §3.1 said "ONE service entry"; §3.4 implies managed services are re-imported (so `${db_*}` resolves). The handler decides which managed services to bundle; composer accepts whatever's passed. |

### 14.2 Phase 3 clarifications (folded into §6 Phase 3 work scope when Phase 3 starts)

1. **Handler prepares `BundleInputs` upstream**: Discover-derived service metadata, SSH-read `/var/www/zerops.yaml`, project env snapshot, managed-service discovery, and live `git remote get-url origin` resolution. `BuildBundle` is pure composition — no further I/O.

2. **Phase B preview redaction**: when `EnvClassifications` is incomplete, the handler MUST redact unclassified env values in the agent-facing preview response. `BuildBundle` itself emits unclassified envs verbatim with warnings; the handler decides whether to surface that body to the user vs. show a redacted version + the per-env review table.

3. **Per-env review table is handler-built**, not bundle-emitted. The handler combines:
   - Bundle output (Classifications map, ImportYAML, Warnings).
   - Handler-tracked metadata (Evidence from agent grep input, Risk from bundle warnings + handler analysis, Override status from session state).
   This decision overrides Codex Agent A's blocker 2 (`Reviews []EnvReview` on bundle) — Agent B's architectural framing won: composer stays minimal; review-row DTO lives at handler level.

4. **RemoteURL freshness lives at handler / Phase 6 helper**, not `BuildBundle`. `BundleInputs.RepoURL` is consumed as-supplied; emptiness is a fail-fast composition error chained to setup-git-push by the handler.

### 14.3 Code amendments landed in Phase 2 (commit per Phase 2 EXIT)

| # | source | section touched | amendment |
|---|---|---|---|
| 1 | A blocker 1 | new file `internal/ops/export_bundle_classify.go` | M2 indirect-reference detector — `extractZeropsYAMLRunEnvRefs` + `parseDollarBraceRefs` + `detectIndirectInfraReferences`. Surfaces a warning when an Infrastructure-classified env is referenced by zerops.yaml's run.envVariables. Defensive only — agent retains classification authority. |
| 2 | A polish | `composeProjectEnvVariables` external-secret branch | Sentinel pattern detection via `isLikelySentinel` (Stripe `sk_test_*` / `pk_test_*` / `rk_test_*`; common `disabled` / `none` / `null` / `false` / `off` / `n/a` / `noop`). External-secret with sentinel-pattern value emits REPLACE_ME but adds a "verify classification" warning. |
| 3 | B 8a + 8b | this §14 | Plan retrospective + Phase 3 clarifications. |

### 14.4 Tests added in Phase 2 amendment pass

- `TestBuildBundle_M2IndirectInfraReference` — pins M2 warning emission for compound `DATABASE_URL` shapes referencing project-level `DB_HOST` / `DB_PASSWORD` / `DB_USER` / `DB_PORT` / `DB_NAME`.
- `TestBuildBundle_M2NoFalsePositiveOnManagedServiceRef` — pins absence of M2 warning when zerops.yaml references managed-service envs (`${db_hostname}`) without corresponding project envs.
- `TestBuildBundle_SentinelExternalSecretFlags` — pins sentinel warning emission per pattern (8 cases).
- `TestParseDollarBraceRefs` — 9 cases including duplicates / unclosed / empty / compound URLs.
- `TestExtractZeropsYAMLRunEnvRefs` — 5 cases including malformed bodies / multi-setup merge.
- `TestIsLikelySentinel` — 17 cases pinning the conservative allowlist.

Effective verdict: APPROVE. Phase 3 may enter on user explicit go.
