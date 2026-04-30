# ZCP Knowledge Distribution Specification

> **Scope**: How runtime-dependent guidance is authored, filtered, and rendered for the LLM across every workflow and every environment.
> **Companion docs**:
> - `docs/spec-workflows.md` §1.3–§1.6 — pipeline overview + phase enum + plan dispatch.
> - `docs/spec-scenarios.md` — S1–S13 acceptance walkthroughs.

This document is the authoring spec for the atom corpus at `internal/content/atoms/*.md` and the behavioural contract of the synthesizer. Guidance is composed per-turn by filtering axis-tagged atoms against a typed `StateEnvelope`.

---

## 1. The Atom Model

### 1.1 Motivation

Pre-rewrite, the same fact ("dynamic runtimes start with `zsc noop`") appeared in six files. Each file had its own register — imperative workflow markdown, declarative CLAUDE.md template, Go guidance builders. Drift was unavoidable: fixing the fact in one place left five stale copies.

The atom model fixes that at the source. Every piece of runtime-dependent guidance is one file — one **atom** — tagged with the envelope cells it applies to. Per turn, the synthesizer filters the corpus against the live envelope and composes the result. There is one source for each fact; delivery is computed.

### 1.2 Corpus location

```
internal/content/atoms/*.md        # ~74 atoms, embedded via //go:embed
internal/content/content.go        # ReadAllAtoms() loader
internal/workflow/synthesize.go    # Synthesize(env, corpus) pure function
internal/workflow/atom.go          # ParseAtom + AxisVector types
internal/content/atoms_test.go     # corpus-level validation (every atom parses)
```

All atoms compile into the binary. There is no runtime filesystem dependency — the LLM never "reads" an atom file; the synthesizer composes atom bodies into a single guidance string that ships inside the tool response.

### 1.3 What lives outside the atom model

Two pipelines run alongside the atom synthesizer and are intentionally **not** part of it:

1. **Recipe authoring** (`workflow=recipe`). This is the pipeline that helps a human produce a new recipe for the Zerops recipe catalog. It parses recipe block structures, extracts decisions and gotchas, and validates shape. Its guidance is long-form authoring prose, not per-turn runtime advice — the atom model's axis decomposition does not fit. See §7 and `internal/workflow/recipe_*.go`.
2. **Iteration-tier escalation** (`internal/workflow/iteration_delta.go`). Deploy-retry guidance escalates by iteration count (1-2 DIAGNOSE, 3-4 SYSTEMATIC, 5 STOP). Iteration count is not a natural atom axis — atoms describe *what to do*, tiers describe *how hard to look*. Tier text is composed from `BuildIterationDelta` and emitted alongside the synthesized atoms.

Every other runtime-dependent guidance string is an atom.

---

## 2. StateEnvelope — The Live Data Contract

Atoms are filtered against a `StateEnvelope` — the canonical per-turn description of project state. Full Go types live in `internal/workflow/envelope.go`; this section is the reader's summary.

### 2.1 Fields

| Field | Type | Purpose |
|---|---|---|
| `Phase` | `Phase` enum | Drives phase-axis filtering. See §3.1. |
| `Environment` | `container` \| `local` | Drives environment-axis filtering. |
| `SelfService` | `*SelfService` | ZCP host container identity (container env only). |
| `Project` | `ProjectSummary` | `{ID, Name}` — `{project-name}` placeholder source. |
| `Services[]` | `[]ServiceSnapshot` | Per-service: `{Hostname, TypeVersion, RuntimeClass, Status, Bootstrapped, Mode, Strategy, StageHostname}`. Sorted by hostname. |
| `WorkSession` | `*WorkSessionSummary` | Open develop session: intent, scope services, deploy/verify attempts, close state. `nil` outside develop. |
| `Recipe` | `*RecipeSessionSummary` | Recipe session: slug, step. `nil` outside recipe-active. |
| `Bootstrap` | `*BootstrapSummary` | Bootstrap session: route, step, iteration. `nil` outside bootstrap-active. |
| `Generated` | `time.Time` | Diagnostic — not consumed by synthesizer. |

### 2.2 Compaction-safety invariant

For two envelopes whose serialized JSON is byte-equal, `Synthesize(env, corpus)` MUST return byte-identical output. The envelope is the **complete** input to synthesis — no ambient state, no process-local caches, no wall-clock reads in atom bodies.

This invariant is what makes the pipeline compaction-safe: when the LLM compacts context, the status tool re-runs the pipeline from the persisted session data + live API, and the guidance is reconstructed verbatim.

Enforcement: `internal/workflow/envelope.go`'s encoder sorts slices + map keys; `synthesize.go` iterates in sorted order; placeholder substitution is a deterministic string replacement; atom bodies never reference `time.Now()`, random values, or process IDs.

### 2.3 Envelope construction

```go
// internal/workflow/compute_envelope.go
func ComputeEnvelope(
    ctx context.Context,
    client platform.Client,
    stateDir string,
    selfHostname string,
    rtInfo runtime.Info,
) (StateEnvelope, error)
```

Every workflow-aware tool handler (`zerops_workflow` status/start/close, `zerops_deploy`, `zerops_verify`, `zerops_env`) computes the envelope at the top of the handler and passes it to the synthesizer + planner. The function parallelises independent I/O (services API call, local state dir reads) so a tool response pays one round-trip regardless of how many state sources are involved.

If the platform client is unconfigured and no project is bound, the envelope is `{Phase: idle, Environment: ..., Services: []}`. This is not a fallback — it is the literal envelope of "no project yet". All other failures bubble up.

---

## 3. Axes

Six axes decompose the guidance space. Each axis is declared in an atom's frontmatter as a list; the empty-list semantic (wildcard) is axis-specific.

### 3.1 `phases` (required, non-empty)

| Value | Meaning |
|---|---|
| `idle` | No active stateful workflow session. |
| `bootstrap-active` | Bootstrap session in progress. |
| `develop-active` | Work Session open. |
| `develop-closed-auto` | Work Session auto-closed — awaiting explicit close + next. |
| `recipe-active` | Recipe session in progress. |
| `strategy-setup` | Stateless synthesis phase emitted from `action="git-push-setup"` (provisions GIT_TOKEN / .netrc / RemoteURL) and `action="build-integration"` (wires webhook / actions). Replaces retired `cicd-active` and the conflated `action="strategy"` entry point. |
| `export-active` | Stateless export immediate workflow. |

**Empty = error.** No atom applies to "any phase" — the phase determines workflow fundamentals. Atoms missing a `phases` declaration fail `LoadAtomCorpus`.

### 3.2 `modes` (service-scoped, optional)

| Value | Meaning |
|---|---|
| `dev` | Dev service in a standard (dev+stage) pair or a dev-only setup. |
| `stage` | Stage service paired with dev. |
| `simple` | Single-service mode (no dev/stage split). |
| `standard` | Dev half of a standard pair when viewed as a runtime (the envelope splits standard into `standard` for the dev snapshot and `stage` for the stage snapshot). |

**Empty = any mode (including pre-bootstrap states with no services).**

Service-scoped axis — see §3.8 for conjunction semantics across service-scoped axes.

### 3.3 `environments` (optional)

| Value | Meaning |
|---|---|
| `container` | ZCP runs inside a Zerops service container (`serviceId` env var present). |
| `local` | ZCP runs on a developer machine. |

**Empty = either.**

### 3.4 `closeDeployModes`, `gitPushStates`, `buildIntegrations` (service-scoped, optional)

Three orthogonal per-pair axes, projected from the corresponding `ServiceMeta` fields.

`closeDeployModes` — what `zerops_workflow action="close"` does:

| Value | Meaning |
|---|---|
| `auto` | Default zcli push at close (default-deploy mechanism, `AttemptInfo.Strategy="zcli"`). |
| `git-push` | Commit + push to configured remote at close. Build trigger is `BuildIntegration`'s concern. |
| `manual` | ZCP yields close orchestration. Tools remain callable; auto-close DOES NOT fire. |
| `unset` | Never chosen yet. Bootstrap leaves it empty; develop's `develop-strategy-review` atom prompts the agent post-first-deploy. |

`gitPushStates` — git-push capability provisioned?

| Value | Meaning |
|---|---|
| `unconfigured` | Default — no `GIT_TOKEN` / `.netrc` / RemoteURL stamped. |
| `configured` | `action="git-push-setup"` succeeded; capability is ready. |
| `broken` | Setup attempted but artifact damaged. |
| `unknown` | Adopted/migrated meta — needs probe. |

`buildIntegrations` — ZCP-managed CI on remote git push (requires `gitPushStates=configured`):

| Value | Meaning |
|---|---|
| `none` | ZCP hasn't wired anything (user may have independent CI/CD that ZCP doesn't track). |
| `webhook` | Zerops dashboard OAuth — Zerops pulls + builds on git push. |
| `actions` | GitHub Actions runs `zcli push` from CI. |

**Empty axis = any value.** All three are service-scoped — see §3.8 for conjunction semantics.

### 3.5 `runtimes` (service-scoped, optional)

| Value | Meaning |
|---|---|
| `dynamic` | Runtime that starts with `zsc noop` and needs explicit server start (Node, Go, Python, Bun, Rust, Java, .NET). |
| `static` | Static-content runtime (nginx-static). |
| `implicit-webserver` | Webserver-native runtime that auto-starts (php-apache, php-nginx). |
| `managed` | Managed service (PostgreSQL, Valkey, …). No deploy, no ServiceMeta. |
| `unknown` | Runtime class not resolved yet. |

**Empty = any runtime.** Service-scoped axis — see §3.8 for conjunction semantics.

### 3.6 `routes` (bootstrap-only, optional)

| Value | Meaning |
|---|---|
| `recipe` | Bootstrap following a matched recipe. |
| `classic` | Bootstrap building services from scratch. |
| `adopt` | Bootstrap registering pre-existing unmanaged services. |

**Empty = any route (within bootstrap-active) OR no-filter.** An atom that declares a `routes` axis implicitly requires `Phase == bootstrap-active` (no route exists in other phases).

### 3.7 `steps` (bootstrap-only, optional)

Bootstrap step names: `discover`, `provision`, `close`. **Empty = any
step.** Any other value produces no matches.

Like `routes`, declaring `steps` implicitly scopes an atom to
`bootstrap-active`.

### 3.8 `deployStates` (service-scoped, optional)

| Value | Meaning |
|---|---|
| `never-deployed` | ServiceMeta is complete (bootstrap finished) but `FirstDeployedAt` is empty. The first-deploy branch atoms gate on this state. |
| `deployed` | ServiceMeta has `FirstDeployedAt` stamped. The edit-loop branch atoms gate on this state. |

**Empty = any state.** Non-bootstrapped services are skipped for this axis entirely — they have no tracked deploy state, and gating first-deploy atoms on them would surface scaffold guidance for pure-adoption services bootstrap never touched.

### 3.9 `serviceStatus` (service-scoped, optional)

Live platform-side service status — the value of `ServiceSnapshot.Status` (e.g. `ACTIVE`, `READY_TO_DEPLOY`, `STARTING`). Use when an atom's content is only relevant for services in a specific runtime state.

**Empty = any status.** Atoms without this axis fire regardless of service status.

Example: `develop-ready-to-deploy` atom describes recovery for services stuck in READY_TO_DEPLOY (created without `startWithoutCode: true`, never deployed). With `serviceStatus: [READY_TO_DEPLOY]` it fires only when at least one service in the envelope is actually in that state — the atom remains 30 lines long but is no-op for the 90% of post-bootstrap envelopes where every service is ACTIVE.

### 3.10 Service-scoped axis conjunction

The seven service-scoped axes (`modes`, `closeDeployModes`, `gitPushStates`, `buildIntegrations`, `runtimes`, `deployStates`, `serviceStatus`) evaluate **together per service**: an atom fires only when a single service in the envelope satisfies EVERY declared service-scoped axis. Axis independence (ANY service satisfies X while a DIFFERENT service satisfies Y) would fire atoms whose `{hostname}` substitution references a service the atom isn't semantically about — e.g. `develop-strategy-review (deployStates=[deployed], closeDeployModes=[unset])` would surface when service A is deployed+`auto` and service B is never-deployed+`unset`, despite no single service being both deployed AND unset.

Envelope-wide axes (`phases`, `environments`, `routes`, `steps`, `idleScenarios`) match the envelope directly — conjunction only applies to the service-scoped group.

---

## 4. Atom Format

### 4.1 File layout

One-fact-one-home — see the full atom at
`internal/content/atoms/develop-dynamic-runtime-start-container.md`. The
spec references the atom by path; it does not copy the body inline. The
atom prescribes the canonical primitive `zerops_dev_server action=start`
for the container env; the matching local-env atom prescribes the
harness background task primitive (e.g. `Bash run_in_background=true` in
Claude Code). See `plans/dev-server-canonical-primitive.md` for the
canonicalization rationale.

### 4.2 Frontmatter fields

| Key | Required? | Notes |
|---|---|---|
| `id` | yes | Stable slug; matches filename. Used as secondary sort key. |
| `title` | yes | Human-readable summary. Not rendered by default. |
| `priority` | yes | Integer. Lower renders earlier. Convention: 1 = essential/early, 9 = late/optional. |
| `phases` | yes | Non-empty list (§3.1). |
| `modes` | no | Service-scoped (§3.2). |
| `environments` | no | (§3.3). |
| `strategies` | no | Service-scoped (§3.4). |
| `runtimes` | no | Service-scoped (§3.5). |
| `routes` | no | Bootstrap-only (§3.6). |
| `steps` | no | Bootstrap-only (§3.7). |
| `deployStates` | no | Service-scoped (§3.8). Combines with other service-scoped axes under §3.9 conjunction. |
| `references-fields` | no | List of Go struct fields in `pkg.Type.Field` form (e.g. `ops.DeployResult.Status`) cited by the atom body. Validated: parser enforces the shape regex, `TestAtomReferenceFieldIntegrity` (in `internal/workflow/`) resolves each entry against `internal/{ops,tools,platform,workflow}/*.go` via AST scan. Part of the authoring contract (§11). |
| `references-atoms` | no | List of atom IDs the body cross-references. Validated by `TestAtomReferencesAtomsIntegrity` (target atom must exist). Prevents rename drift; part of the authoring contract (§11). |
| `pinned-by-scenario` | no | List of scenario-test anchors (e.g. `S7_DevelopClosedAuto`). Informational — helps future edits locate downstream test expectations. Not validated at runtime. |

Frontmatter uses a minimal parser in `internal/workflow/atom.go::parseFrontmatter`. List values use the inline YAML form `[a, b, c]`. Comments (`#`) and blank lines are ignored. Malformed lines fail `LoadAtomCorpus`; malformed `references-fields` entries fail `ParseAtom` with a specific message.

### 4.3 Body

Markdown. Rendered as-is (after placeholder substitution). Length per atom targets ≤80 lines; most atoms are 5–30 lines. Soft cap is 4500 total lines across the corpus.

### 4.4 Placeholders

Two categories:

**Envelope-filled** (substituted by the synthesizer from `StateEnvelope`):

| Placeholder | Source |
|---|---|
| `{hostname}` | First runtime service in `env.Services` — dynamic > implicit-webserver > static. Empty if no runtime. |
| `{stage-hostname}` | Paired stage hostname of the `{hostname}` service, if any. |
| `{project-name}` | `env.Project.Name`. |

**Agent-filled** (survive substitution untouched; the LLM substitutes them from its own context):

`{start-command}`, `{task-description}`, `{your-description}`, `{next-task}`, `{port}`, `{name}`, `{token}`, `{url}`, `{runtimeVersion}`, `{runtimeBase}`, `{setup}`, `{serviceId}`, `{targetHostname}`, `{devHostname}`, `{repoUrl}`, `{owner}`, `{repoName}`, `{repo}`, `{branchName}`, `{branch}`, `{zeropsToken}`, `{runtime}`.

Shell-style `${name}` env-var references are ignored (they belong to the generated `zerops.yaml`, not the atom).

**Unknown placeholders are build-time errors.** After substitution, `findUnknownPlaceholder` scans each atom body for leftover `{word}` tokens that aren't envelope-filled and aren't whitelisted; any match fails with `"atom <id>: unknown placeholder "{foo}" in atom body"`. No literal braces ever leak to the LLM.

---

## 5. Synthesizer

```go
// internal/workflow/synthesize.go
func Synthesize(envelope StateEnvelope, corpus []KnowledgeAtom) ([]string, error)
```

### 5.1 Algorithm

1. **Filter**: for each atom, evaluate `atomMatches(atom, envelope)`:
   - `phases`: `env.Phase` must be in the atom's phase set.
   - `environments` (if non-empty): `env.Environment` must be in the set.
   - `routes` / `steps` (if non-empty): `env.Bootstrap` must exist and the route/step must match.
   - `modes` / `strategies` / `runtimes` / `deployStates` (service-scoped group, if any is non-empty): at least one service in `env.Services` must satisfy EVERY non-empty service-scoped axis simultaneously (conjunction per service — see §3.9).
   - An empty axis = wildcard.
2. **Sort**: priority ascending (1 first), then id lexicographically (stable tiebreaker).
3. **Substitute**: apply a shared `strings.NewReplacer` (built once per Synthesize from envelope hostnames + project name) to each atom body, then scan for unknown placeholders.
4. **Return**: the ordered list of rendered bodies.

### 5.2 Rendering into the tool response

Callers are responsible for joining. The status renderer (`RenderStatus`) emits each body as a separate paragraph in the "Guidance" section, separated by blank lines. Stateless synthesis (`strategy-setup`, `export-active`) uses `SynthesizeImmediateWorkflow(phase, env)` which joins bodies with `\n\n---\n\n` and returns a single string. `strategy-setup` is invoked from `handleGitPushSetup` and `handleBuildIntegration` (the two split actions that replaced the retired `action="strategy"`); `export-active` is invoked from the `workflow=export` immediate entry.

### 5.3 Determinism guarantees

- Sort is `sort.SliceStable` over (priority, id) — same input ordering every time.
- Placeholder substitution goes through a single `strings.NewReplacer` built from a fixed literal key list — not map iteration.
- The service-scoped match helpers iterate `env.Services` in the envelope's serialized order (hostname-sorted).
- No goroutines are spawned inside `Synthesize`.

These choices together satisfy the compaction-safety invariant (§2.2). A unit test in `synthesize_test.go` pins the invariant against a fixed corpus.

### 5.4 Corpus coverage

`internal/workflow/corpus_coverage_test.go` asserts that for every `(Phase, Environment)` combination used in production, `Synthesize` returns a non-empty body. This catches axis mis-tagging (e.g. an atom meant for `develop-active` accidentally scoped to `idle`) before it reaches a release.

Inventory as of 2026-04-19 (74 atoms total):

| Prefix | Count | Notes |
|---|---|---|
| `idle-*` | 3 | Entry atoms for idle phase. |
| `bootstrap-*` | 27 | Split by mode × environment × runtime × route × step. |
| `develop-*` | 25 | Split by mode × close-mode × runtime × environment × deploy state. |
| `setup-git-push-{container,local}`, `setup-build-integration-{webhook,actions}` | 4 | Strategy-setup phase atoms — emitted from `action="git-push-setup"` (GIT_TOKEN / .netrc / RemoteURL) and `action="build-integration"` (webhook / actions). Replace the retired 6-atom cicd-* set. |
| `export-*` | 6 | Topic-scoped atoms for `workflow=export` (intro / classify-envs / validate / publish / publish-needs-setup / scaffold-yaml). |

---

## 6. Plan — Typed Trichotomy

Guidance tells the agent **how** to proceed with the current phase; the Plan tells it **what** to do next. They are produced independently from the same envelope.

```go
// internal/workflow/plan.go
type Plan struct {
    Primary      NextAction   // never zero
    Secondary    *NextAction  // tandem action, optional
    Alternatives []NextAction // genuinely alternative paths
}

type NextAction struct {
    Label     string
    Tool      string
    Args      map[string]string
    Rationale string
}
```

`Args` keys match the target tool's input schema verbatim so the rendered suggestion can be copied into a tool call without translation (e.g. `zerops_deploy` → `targetService`, `zerops_verify` → `serviceHostname`). Every new action constructor must use the same key name the MCP tool declares.

`BuildPlan(env)` is a pure function. Dispatch rules and table are in `docs/spec-workflows.md` §1.4. The Plan is rendered in the "Next" section of the status block with priority markers:

```
Next:
  ▸ Primary: Start develop — zerops_workflow action="start" workflow="develop" intent="..."
  ◦ Secondary: Close current develop — zerops_workflow action="close" workflow="develop"
  · Alternatives:
      - Add more services — zerops_workflow action="start" workflow="bootstrap"
      - Adopt unmanaged runtimes — zerops_workflow action="start" workflow="develop" intent="..."
```

No free-form "Next" strings appear anywhere else in a tool response. Every piece of "what next" prose is a Plan.

---

## 7. Recipe Authoring — Separate Pipeline

Recipe authoring (`workflow=recipe`) is the one workflow whose *content guidance* does NOT flow through the atom synthesizer. This is deliberate.

### 7.1 Why separate

Atoms encode **axis-tagged runtime guidance** — "when the envelope looks like X, show this snippet." Recipe authoring guidance is a structured long-form walkthrough: research a framework, propose a plan, generate block-structured recipes, audit the output. The content is decision-tree shaped, not axis shaped; attempting to atomise it would require a per-step axis that nothing else in the corpus uses and would produce atoms longer than the whole of any other phase.

### 7.2 Pipeline entry points

| File | Responsibility |
|---|---|
| `internal/workflow/recipe.go` | Recipe session lifecycle (steps, iteration, close). |
| `internal/workflow/recipe_guidance.go` | Assembles guidance per recipe step. |
| `internal/workflow/recipe_block_parser.go` | Parses the block-structured recipe output. |
| `internal/workflow/recipe_decisions.go` | Extracts decision rationale from the recipe body. |
| `internal/workflow/recipe_features.go` | Validates declared features against detected code. |
| `internal/workflow/recipe_gotcha_*.go` | Extracts and shapes "gotcha" sections. |
| `internal/content/workflows/recipe.md` | The source markdown consulted by the pipeline. |

### 7.3 Contract with the rest of the system

Recipe authoring shares the envelope: the status tool emits `Phase=recipe-active` and a `Recipe` summary. But the guidance body is produced by `recipe_guidance.go`, not `Synthesize`. Atoms with `phases: [recipe-active]` exist only as entry-point framing (if any); the substantive content lives in `recipe.md`.

Per-turn recipe guidance is NOT part of the corpus-coverage test (§5.4) — its coverage is validated by `recipe_guidance_audit_test.go` and `recipe_content_placement_test.go` instead.

---

## 8. ServiceMeta Lifecycle

ServiceMeta files (`.zcp/state/services/{hostname}.json`) are the persistent bridge between bootstrap/adoption and develop. They record per-service decisions consumed by envelope construction and atom filtering. The semantics below are unchanged from the pre-rewrite spec — ServiceMeta existed before the pipeline rewrite and survives it intact.

### 8.1 Partial Meta After Provision

After the bootstrap provision step completes, `writeProvisionMetas()` writes a partial meta for each runtime target:

- Fields set: `Hostname`, `Mode`, `StageHostname`, `Environment`, `BootstrapSession`.
- Fields NOT set: `BootstrappedAt` (empty), `CloseDeployMode` / `GitPushState` / `BuildIntegration` (all empty).
- `IsComplete()` returns `false` — signals bootstrap in-progress.

**Purpose**: Hostname lock. Other sessions check for incomplete metas to prevent concurrent bootstrap of the same service. If the owning session's PID is alive, bootstrap is blocked. If PID is dead (orphaned meta), the lock auto-releases.

### 8.2 Full Meta After Bootstrap or Adoption

When bootstrap completes, `writeBootstrapOutputs()` overwrites with full meta:

- `BootstrappedAt` = today's date → `IsComplete()` returns `true`.
- `CloseDeployMode` / `GitPushState` / `BuildIntegration` stay empty — set separately by `action="close-mode"` / `action="git-push-setup"` / `action="build-integration"`.
- `BootstrapSession` = the 16-hex session ID that created this.

Adoption follows the same pattern but writes `BootstrapSession = ""` as its marker. `IsAdopted()` returns `true` when `BootstrapSession` is empty AND the meta is complete.

**Environment-specific hostname**:
- Container: `ServiceMeta.Hostname` = devHostname, `StageHostname` = stageHostname.
- Local + standard mode: `ServiceMeta.Hostname` = stageHostname, `StageHostname` = "" (inverted). Reason: in local mode, stage is the primary deployment target since developers iterate locally and deploy to stage.

The meta file is **pair-keyed**: `m.Hostname` and `m.StageHostname` together name every live hostname the pair represents, and they resolve to the same file on disk. See `docs/spec-workflows.md` E8 and `internal/workflow/service_meta.go::Hostnames()` for the canonical enumeration; use `ManagedRuntimeIndex` for slice→map construction and `FindServiceMeta` for disk lookup. Keying a hostname index by `m.Hostname` alone violates E8.

Subdomain activation is a deploy-handler concern (see `docs/spec-workflows.md` §4.8), not a ServiceMeta field. The meta records lifecycle state owned by ZCP (bootstrapped, close-mode, git-push capability, build integration, first-deployed-at); L7 subdomain activation is owned by the Zerops platform and reflected at read time via `GetService.SubdomainAccess`, so it is not mirrored into the meta file.

### 8.3 Close-Mode + Capability Updates

Three orthogonal actions write to ServiceMeta atomically:
- `zerops_workflow action="close-mode" closeMode={hostname:value}` writes `CloseDeployMode` (validates `auto` / `git-push` / `manual`) and stamps `CloseDeployModeConfirmed=true`.
- `zerops_workflow action="git-push-setup" service="..." remoteUrl="..."` writes `GitPushState=configured` + `RemoteURL`.
- `zerops_workflow action="build-integration" service="..." integration="..."` writes `BuildIntegration` (validates `webhook` / `actions`; refuses unless `GitPushState=configured`).

Develop flow always reads these fields fresh from meta (never cached in Work Session). A user can flip any of them mid-session and the next deploy/close step picks up the new value automatically.

### 8.4 Envelope Integration

`ComputeEnvelope` reads every ServiceMeta at construction time and emits per-service snapshots:

- `ServiceSnapshot.Bootstrapped` = `meta.IsComplete()`.
- `ServiceSnapshot.Mode` = `meta.Mode`.
- `ServiceSnapshot.CloseDeployMode` = `meta.CloseDeployMode` (or `CloseModeUnset` if empty).
- `ServiceSnapshot.GitPushState` = `meta.GitPushState`.
- `ServiceSnapshot.BuildIntegration` = `meta.BuildIntegration`.
- `ServiceSnapshot.RemoteURL` = `meta.RemoteURL`.
- `ServiceSnapshot.StageHostname` = `meta.StageHostname`.

The atom synthesizer filters against these fields via the `modes`, `closeDeployModes`, `gitPushStates`, `buildIntegrations`, and (runtime-class-derived) `runtimes` axes.

---

## 9. Workflow Routing

`zerops_workflow action="route"` evaluates project state and returns prioritised offerings. Unchanged from pre-rewrite — `Route()` is a pure function of the envelope.

Priority ordering:

1. (P1) Incomplete bootstrap → resume hint or start hint.
2. (P1) Unmanaged runtimes → bootstrap adoption offering.
3. (P1-P2) Bootstrapped services with strategy set → deploy or cicd offering based on strategy.
4. (P3) Add new services → bootstrap start hint.
5. (P4-P5) Utilities → recipe, scale.

Manual strategy produces no deploy/cicd offering — user manages directly.

Route returns **facts, not recommendations**. It enumerates what workflows are available; `BuildPlan` is what decides which one the agent should pick next.

---

## 10. Invariants

Every invariant here is a property of the implementation and can be verified by reading the referenced code.

| ID | Invariant | Code reference |
|----|-----------|---------------|
| KD-01 | The lifecycle status response (`zerops_workflow action="status"`, plus `start workflow="develop"`'s briefing) goes through `ComputeEnvelope` → `BuildPlan` → `Synthesize` — that is the canonical pipeline path. Mutation responses MAY be terse and MAY NOT pass through the synthesizer; the LLM recovers lifecycle context via `status`. Aligned with `spec-workflows.md` P4. | Grep for `ComputeEnvelope(` in `internal/tools/` |
| KD-02 | `Synthesize(env, corpus)` is pure: same envelope JSON → byte-identical output. | `internal/workflow/synthesize_test.go` |
| KD-03 | `BuildPlan(env)` is pure: no I/O, no time, no randomness. | `internal/workflow/build_plan_test.go` |
| KD-04 | Every atom declares a non-empty `phases` axis. | `ParseAtom` rejects empty `phases` in `internal/workflow/atom.go` |
| KD-05 | Unknown `{placeholder}` tokens in atom bodies fail the corpus load. | `findUnknownPlaceholder` in `synthesize.go` |
| KD-06 | `Plan.Primary` is never zero in a well-formed response. | Gated by `BuildPlan` tests; callers error on empty Plan. |
| KD-07 | `strategy-setup` (from `handleGitPushSetup` + `handleBuildIntegration`) and `export-active` are stateless — no session file is written. | `internal/tools/workflow_git_push_setup.go`, `internal/tools/workflow_build_integration.go`, `workflow_export.go` |
| KD-08 | Recipe authoring runs through `recipe_*.go` section parsers, NOT the atom synthesizer. | `internal/workflow/recipe_guidance.go` does not call `Synthesize` |
| KD-09 | Iteration-tier text (`BuildIterationDelta`) is emitted as an addendum to synthesized atoms, not as an atom. | `internal/workflow/iteration_delta.go` is called independently by deploy handlers |
| KD-10 | Envelope slice ordering is deterministic: services sort by hostname, attempts by time, map keys by string order. | `envelope.go` encoder + `compute_envelope.go` sort passes |
| KD-11 | ServiceMeta is the ONLY persistent per-service state read by envelope construction. Work Session is per-PID and does not cache strategy. | `compute_envelope.go` reads `.zcp/state/services/`; Work Session structure has no strategy field |
| KD-12 | For every `(Phase, Environment)` used in production, `Synthesize` returns at least one atom body. | `corpus_coverage_test.go` |
| KD-13 | Partial ServiceMeta (no `BootstrappedAt`) signals incomplete bootstrap. | `service_meta.go:IsComplete()` |
| KD-14 | Adopted ServiceMeta has `BootstrapSession == ""` AND `IsComplete()`. | `service_meta.go:IsAdopted()` |
| KD-15 | Mixed strategies across a single Work Session scope are permitted — each service's strategy drives its own atom filtering. | `build_plan.go` + atom `strategies` axis |
| KD-16 | Service-scoped axes (`modes`, `strategies`, `runtimes`, `deployStates`) evaluate under conjunction per service: an atom matches only when a single service in the envelope satisfies every declared service-scoped axis. | `synthesize.go:anyServiceMatchesAll`; `TestSynthesize_ServiceScopedAxesRequireSameService` |
| KD-17 | `MarkServiceDeployed` resolves hostname via `findMetaForHostname`, so verifying the stage half of a standard pair stamps the dev-keyed meta. Exits the first-deploy branch regardless of which half the agent verified first. | `service_meta.go:findMetaForHostname`; `TestMarkServiceDeployed_StampsViaStageHostname` |

---

## 11. Authoring Contract

The corpus is the runtime projection of what the agent can observe; its
prose must match that projection. Enforcement is unified — no
per-topic contract tests exist, and none should be added.

### 11.1 What atoms describe

1. **Observable response fields** — identifiers backed by Go struct
   fields in `internal/{ops,tools,platform,workflow}/*.go`. Cited
   fields MUST appear in the atom's `references-fields` frontmatter.
2. **Observable envelope fields** — `StateEnvelope`, `ServiceSnapshot`,
   `WorkSessionSummary`, `Plan`, `BootstrapRouteOption`,
   `BootstrapDiscoveryResponse`. Rendered tokens in
   `RenderStatus` output (e.g. `bootstrapped=true`,
   `deployed=false`, `mode=dev`) are first-class.
3. **Orchestration sequences** — ordered tool-call flows.
4. **Platform concepts** — mode taxonomy, runtime classes, pair
   structure, workflow phases.
5. **Preventive rules** — anti-patterns the agent should not attempt.
6. **Cross-references to other atoms** — via `references-atoms`
   frontmatter (rename tracking).

### 11.2 What atoms MUST NOT describe

Enforced by `TestAtomAuthoringLint` (`internal/content/atoms_lint.go`):

1. **Handler internals** — verbs like "the X handler … automatically /
   auto-* / writes / stamps / activates", "tool … auto-…", "ZCP writes
   / stamps / activates / enables / disables".
2. **Invisible state** — `FirstDeployedAt`, `BootstrapSession`,
   `CloseDeployModeConfirmed` (on-disk ServiceMeta fields the agent
   never sees).
3. **Spec invariant IDs** — `DM-*`, `DS-0[1-4]`, `GLC-[1-6]`,
   `KD-NN`, `TA-NN`, `E[1-8]`, `O[1-4]`, `F#[1-9]`, `INV-*`. These
   are developer taxonomy; the agent has no use for them at runtime.
4. **Plan document paths** — `plans/<slug>.md` in atom prose.
   Superseded plans drift; spec should be self-contained.

### 11.3 Enforcement

Three tests enforce the contract; they live outside the atom tree so
rule changes do not churn atom files:

| Test | Location | Responsibility |
|---|---|---|
| `TestAtomReferenceFieldIntegrity` | `internal/workflow/atom_reference_field_integrity_test.go` | Every `references-fields` entry resolves to a named struct field via AST scan. |
| `TestAtomReferencesAtomsIntegrity` | `internal/workflow/atom_references_atoms_integrity_test.go` | Every `references-atoms` entry resolves to an existing atom. |
| `TestAtomAuthoringLint` | `internal/content/atoms_lint_test.go` | Body prose matches no forbidden pattern (§11.2) and no axis K/L/M/N drift (§11.5/§11.6). |

### 11.4 Allowlist policy

`atomLintAllowlist` in `atoms_lint.go` accepts `"<file>::<exact-line>"`
keys for documented exceptions across every rule family. The default
set is empty; every entry is an audit target. When adding one, commit
the rationale in the map value. Prefer rewriting the atom —
allowlisting is the escape hatch, not the default.

Per-axis allowlists (`axisLAllowlist`, `axisKAllowlist`,
`axisMAllowlist`, `axisNAllowlist`) live in
`atoms_lint_seed_allowlist.go` and follow the same key/rationale
shape; an entry there suppresses ONE rule for ONE atom line.

### 11.5 Content-quality axes (K, L, M)

Three axes apply to atom prose beyond the §11.2 lint patterns. Each
axis is documented at the level of the rule + a worked example; full
corpus-scan ledgers live in `plans/audit-composition/axis-{k,l,m}-*.md`.

**Lint enforcement** (engine plan E4, shipped 2026-04-27): all four
axes (K, L, M, N) are enforced by `internal/content/atoms_lint_axes.go`
and pinned by `TestAtomAuthoringLint`. Axis L is HARD-FORBID — no
escape valve except a per-line allowlist entry. Axes K, M, N use the
inline marker convention documented in §11.7 below.

#### Axis K — ABSTRACTION-LEAK

**Definition**: an atom mentions flows, mechanisms, or
implementation details from OUTSIDE the envelope it fires on. The
agent reading the atom never had a reason to know about those
things; the mention either gives them anti-information ("there's
no X here") or implementation detail they shouldn't care about
("Y runs under the hood").

**Judgment test**: "Without this sentence, would the agent —
operating on this envelope only AND carrying plausible cross-flow
training reflexes — actually do the wrong thing?"

**HIGH-risk signals** (mandatory KEEP unless an explicit Codex
per-edit rejects):

1. Negation tied to a tool/action: "Don't run X", "Never use Y",
   "No Z available here". The negation IS the guardrail.
2. Cross-env contrast as mental-model framing: "Local mode builds
   from your committed tree — no SSHFS, no dev container" —
   couples a positive operational claim to the negation; prevents
   a likely cross-flow reflex.
3. Tool-selection guidance: "Use `zerops_deploy` here, not `zcli
   push`"; "Do NOT use `zerops_dev_server` — that tool is
   container-only".
4. Recovery guidance: "If X fails, do Y" — the alternative path
   is the guardrail.
5. Sentences with `do not` / `never` / `no X` tied to an
   operational choice.

**LOW-risk DROP candidates** (only when no HIGH-risk signal
applies):

- Pure implementation trivia, no operational consequence (e.g.
  "`zcli push` under the hood" — agent calls `zerops_deploy`,
  dispatch invisible).
- Standalone negation with no operational coupling.
- Comparative diagram of flow differences in UNRELATED env.
- Historical context: "this used to be different in v1".

**Default rule: when uncertain, KEEP**. Document the keep
rationale in the per-atom fact inventory.

#### Axis L — TITLE-OVER-QUALIFIED

**Definition**: atom title (or H1/H2/H3 inside body) contains env
qualifiers (`(container)`, `(local)`, `— container`, etc.) that
the axis filter already implies. The agent only RECEIVES this
atom on envelopes matching the axis; the qualifier conveys
nothing the framing-context doesn't already convey.

**Token-level rule** (split title qualifier on commas/em-dashes/
parens):

- **Env-only token** (`container`, `local`, `container env`,
  `local env`) → DROP.
- **Mode/runtime/close-mode distinguisher** (`dev mode`, `simple
  mode`, `standard mode`, `dynamic`, `static`, `auto`,
  `git-push`, `manual`) → KEEP — distinguishes from sibling
  atoms in the rendered output.
- **Mechanism payload** (`GIT_TOKEN + .netrc`, `user's git`,
  runtime constraint, credential channel) → KEEP — load-bearing
  operational distinction.

**Worked examples**:

- `"close-mode=auto Deploy — container"` → drop `— container`
  (only env token).
- `"close-mode=auto iteration cycle (dev mode, container)"` → drop
  `, container`; keep `dev mode` (mode distinguisher).
- `"git-push setup — container env (GIT_TOKEN + .netrc)"`
  → drop `— container env`; KEEP `(GIT_TOKEN + .netrc)`
  (mechanism payload distinguishing from local-env credential
  flow).

**Pin-migration discipline**: before dropping a phrase from H1/H2/
H3 that's pinned by `coverageFixtures().MustContain` in
`internal/workflow/corpus_coverage_test.go`, migrate the pin to a
new unique phrase from the post-edit body in the same commit.
Pin-migration discipline mirrors `TestCorpusCoverage_RoundTrip`.

#### Axis M — TERMINOLOGY-DRIFT

**Definition**: same concept written differently in different
atoms costs the agent's parsing budget. The agent has to
canonicalise mentally to map e.g. "Zerops container" + "service
container" + "dev container" + "the runtime" to the same
referent.

**Five drift clusters** (with risk class):

| # | Concept | Risk | Canonical decision |
|---|---|---|---|
| 1 | Container holding user code | HIGH | Per-occurrence sub-table below |
| 2 | Code-change → durable-state action | HIGH | `deploy` (first-action) vs `redeploy` (subsequent) — semantically distinct; per-occurrence judgment |
| 3 | The platform itself | HIGH | `Zerops` (the platform); `ZCP` (control-plane); avoid bare "the platform" |
| 4 | Agent's tool family | MEDIUM | `zerops_<name>` (specific); `MCP tool` (general); avoid "the tool" |
| 5 | The agent itself | LOW | `you` (atom is direct address); avoid "the agent" / "the LLM" — those are author-perspective |

**Cluster #1 container sub-table**:

| Use this term | When the atom is talking about |
|---|---|
| `dev container` | Mutable SSHFS-mounted context — the developer-mutable container for dev-mode-dynamic flows (close-mode=auto path). |
| `runtime container` | A running service instance generally. The default for cross-cluster references when no other distinction applies. |
| `build container` | The build-stage filesystem (zbuilder context) before the runtime swap. Only when the atom is explicitly talking about build vs runtime. |
| `Zerops container` | Broad first-introduction framing only — when the atom is orienting an unfamiliar reader. Avoid in detailed operational guidance. |
| `new container` | The replacement container created on each deploy (deploy-replacement semantics specifically). |

**Verification rates** per cluster (from corpus-hygiene followup
amendment 3 / Codex C13):

- HIGH-risk clusters #1, #2, #3: per-occurrence Codex review of
  EVERY touched occurrence. Not 10% sampling. Global sed is
  forbidden.
- MEDIUM-risk cluster #4: ≥50% sampling.
- LOW-risk cluster #5: 10% sampling.

**Special caveat** (cluster #5): in `develop-verify-matrix`, the
word "agent" refers to a SPAWNED SUB-AGENT (Sonnet model via the
`Agent()` template). KEEP "agent" there — it's intentional
sub-agent terminology, not author-perspective drift.

### 11.6 Content-quality axis N — UNIVERSAL-ATOM PER-ENV LEAK

(Authored 2026-04-27 cycle 3; corpus-scan ledger lives in
`plans/audit-composition-v3/axis-n-candidates.md`.)

**Definition**: an atom WITHOUT `environments:` axis restriction
(or with both env values implicitly) carries env-specific edit-
location, runtime-shell, or storage-layer detail. The per-env
context is already established by `develop-platform-rules-local`
and `develop-platform-rules-container` (which always co-fire on
develop-active envelopes). Universal atoms should write at the
universal-truth layer; let per-env atoms fill the where-to-edit /
how-to-run gaps.

**Distinction from Axis K**:
- Axis K = atom mentions OUTSIDE-its-envelope flow detail (cross-
  flow leakage; e.g. a container-axis atom mentions local mode).
- Axis N = atom WITHOUT env axis carries env-specific detail
  that belongs in per-env atoms (within-flow over-specification).

**Judgment test (per phrase)**: would an agent on EITHER env
benefit from this phrase, or does it hard-code one env's mental
model?

**HIGH-risk env tokens** (flag for classification):
- `locally`, `your machine`, `your editor`, `your IDE`
- `SSHFS`, `/var/www/{hostname}`, `container env`, `local env`
- `on your CWD`, `on the mount`, `via SSH`, `over SSH`,
  `dev container` (when the atom isn't intrinsically about
  close-mode=auto container flows)

**Per-match classification**:

- **DROP-LEAK**: atom is universal; per-env detail belongs in
  `develop-platform-rules-{local,container}`; drop and rely on
  per-env atoms.
- **KEEP-LOAD-BEARING**: the per-env detail IS the guardrail;
  can't be dropped without losing operational guidance (treat as
  Axis K signal #3/#4/#5).
- **SPLIT-CANDIDATE**: atom genuinely needs per-env split; tighten
  the `environments:` axis restriction.
- **UNIFICATION-CANDIDATE**: atom is currently env-split but
  marginalia is env-irrelevant; merge candidate (see inverse
  rule below).

**Worked examples** (post-cycle-2 baseline):

- `develop-static-workflow.md` L13 "Edit files locally, or on the
  SSHFS mount in container mode." → DROP-LEAK; rewrite "Edit
  files." Per-env edit location is in
  `develop-platform-rules-{local,container}` which always co-fire
  on develop-active envelopes.
- `develop-strategy-review.md` L15 parenthetical "(zcli push from
  your workspace: dev container → stage, or local CWD → stage)"
  → DROP-LEAK; drop the parenthetical. Per-env shape is in
  `develop-close-mode-auto-deploy-{container,local}`.

**Inverse rule (UNIFICATION candidate)**:

> If two env-split atoms differ ONLY in env-marginal phrasing
> (one says "edit files locally", the other says "edit files on
> SSHFS mount") and the per-env detail is already in platform-
> rules atoms, the two are candidates for UNIFICATION into a
> single env-agnostic atom. Resolution: merge atoms + cross-link
> to per-env platform-rules atoms; drop the env axis on the
> merged atom.

**DO-NOT-UNIFY exception**: if the env split itself encodes a
tool-selection (signal #3), recovery (#4), or do-not (#5)
guardrail — e.g., `develop-platform-rules-local` (use
`Bash run_in_background=true` harness; `zerops_dev_server` is
container-only) vs `develop-platform-rules-container` (use
`zerops_dev_server`; do NOT hand-roll `ssh <host> "cmd &"`
backgrounding) — the env split IS the load-bearing signal. Such
atoms are NEVER unification candidates regardless of marginal
phrasing similarity. Apply this exception BEFORE flagging a
pair as UNIFICATION-CANDIDATE.

**Risk + mitigation**: LOW. The mitigation is that
`develop-platform-rules-{local,container}` always co-fire on
develop-active envelopes (verified by Phase 4 fire-audit-style
check). Universal atoms can rely on the cross-link rendering
the per-env detail next to them in the agent's context window.
If a future axis change breaks that co-fire guarantee, every
DROP-LEAK applied here regresses to missing-information; the
cycle-3 Phase 4 POST-WORK Codex round verified the cross-link
holds at the time of the corpus pass.

### 11.7 Marker convention (axes K, M, N)

The axis-K, axis-M, and axis-N lints (`atoms_lint_axes.go`) are
heuristic — the patterns flag CANDIDATES, not certain violations.
Authors who want to KEEP a flagged phrase add an inline HTML comment
on the SAME line, the IMMEDIATELY PREVIOUS non-blank line, or the
IMMEDIATELY FOLLOWING non-blank line:

```
<!-- axis-k-keep: signal-#3 -->
**Do NOT use `zerops_dev_server`** — that tool is container-only.
```

Markers accept a free-form trailing annotation (commonly the spec
signal number for K, the cluster number for M, or a one-line keep
rationale). `<!-- axis-{k,m,n}-drop -->` is also accepted as an
explicit "this should be removed, leaving here pending edit" marker.

**Markers are stripped from rendered atom bodies** — `ParseAtom` calls
`content.StripAxisMarkers` before assigning the body to the
`KnowledgeAtom`, so agents never see the metadata. Marker-only lines
are dropped entirely; inline markers consume their leading whitespace
so prose flow is preserved.

**Axis L does NOT honor markers.** It is HARD-FORBID — env-only title
qualifiers (`container`, `local`, `container env`, `local env` as
standalone tokens after splitting on em-dash, parens, commas, or `+`)
are rejected unconditionally. The escape valve is the per-line
`axisLAllowlist` entry in `atoms_lint_seed_allowlist.go`, with a
documented rationale.

**Per-axis allowlists** (`axisLAllowlist`, `axisKAllowlist`,
`axisMAllowlist`, `axisNAllowlist`) live in
`atoms_lint_seed_allowlist.go`; each entry is keyed
`<atomFile>::<exact-trimmed-line>` and MUST carry a one-line rationale
in the map value. Allowlists were seeded during the 2026-04-27 audit
to grandfather HIGH-signal guardrails, sub-agent terminology
(cluster-#5), and structurally unavoidable env-tokens; new edits
should prefer markers.

---

## Appendix: Code Reference Map

| Section | Primary code location |
|---------|----------------------|
| §1 Corpus | `internal/content/atoms/*.md`, `internal/content/content.go` |
| §2 Envelope | `internal/workflow/envelope.go`, `compute_envelope.go` |
| §3 Axes | `internal/workflow/atom.go::AxisVector` |
| §4 Atom format | `internal/workflow/atom.go::ParseAtom` |
| §5 Synthesizer | `internal/workflow/synthesize.go` |
| §6 Plan | `internal/workflow/plan.go`, `build_plan.go` |
| §7 Recipe authoring | `internal/workflow/recipe_*.go`, `internal/content/workflows/recipe.md` |
| §8 ServiceMeta | `internal/workflow/service_meta.go`, `bootstrap_outputs.go` |
| §9 Routing | `internal/workflow/router.go` |
| §10 Invariants | Tests in `internal/workflow/*_test.go` pin these |
| §11.5/§11.6 Axis lint | `internal/content/atoms_lint_axes.go`, `atoms_lint_seed_allowlist.go` |
| §11.7 Marker stripping | `internal/content/atoms_lint_axes.go::StripAxisMarkers`, called from `internal/workflow/atom.go::ParseAtom` |
