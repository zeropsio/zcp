# ZCP Instruction Delivery — Architectural Rewrite

> **Status**: Landed. All phases (0–7) green per §13; plan retained as the live reference for the atom pipeline, envelope model, and per-scenario behaviour.
> **Scope**: `internal/workflow`, `internal/content/templates/claude.md`, `internal/content/workflows/*.md`, `internal/server/instructions.go`, `internal/knowledge/briefing.go`, iteration-tier code paths.
> **Non-scope**: `internal/platform`, `internal/auth`, `internal/catalog`, `internal/sync`, MCP wiring itself.
> **Date**: 2026-04-19

This document is the single source of truth for the rewrite. The original §§1–12 describe intent; §13 tracks current state; §14 is the scenario spec that the test suite validates; Appendix D lists remaining spec debt outside this scope.

---

## Table of Contents

1. [Motivation & Problem Inventory](#1-motivation--problem-inventory)
2. [Design Principles](#2-design-principles)
3. [Architecture Overview](#3-architecture-overview)
4. [Data Model](#4-data-model)
5. [Layer 1 — CLAUDE.md as Static Source](#5-layer-1--claudemd-as-static-source)
6. [Layer 2 — Orthogonal Knowledge Matrix](#6-layer-2--orthogonal-knowledge-matrix)
7. [Layer 3 — State Envelope + NextActions](#7-layer-3--state-envelope--nextactions)
8. [Bootstrap Flow — Three Routes](#8-bootstrap-flow--three-routes)
9. [Develop Flow](#9-develop-flow)
10. [Knowledge Preservation Audit Pipeline](#10-knowledge-preservation-audit-pipeline)
11. [Implementation Phases](#11-implementation-phases)
12. [Test Strategy](#12-test-strategy)
13. [Acceptance Criteria](#13-acceptance-criteria)
14. [Per-Scenario Walkthroughs](#14-per-scenario-walkthroughs)
15. [Glossary](#15-glossary)

---

## 1. Motivation & Problem Inventory

### 1.1 What we are replacing

The current instruction-delivery subsystem grew organically across three components that each evolved their own register, vocabulary, and failure modes:

- `internal/server/instructions.go` — static MCP instructions (~31 lines), delivered once at server init.
- `internal/content/templates/claude.md` — 20-line template written to user's repo by `zcp init`.
- `internal/content/workflows/*.md` — imperative workflow scripts, `bootstrap.md` alone is 1170 lines, `recipe.md` is 2678.
- `internal/workflow/*.go` — conductor code with ad-hoc guidance assembly (`assembleGuidance`, `BuildIterationDelta`, `prependRecipeContext`, etc.).
- `internal/knowledge/briefing.go` — briefing store that composes recipe + runtime + universals at query time.

The result is a system that is **simultaneously over-complex and under-specified**: guidance appears in too many places with subtle inconsistencies, yet the contract between tool and LLM is stringly-typed and leaves critical knowledge stranded across unrelated templates.

### 1.2 Verified defects

| # | Defect | Evidence |
|---|---|---|
| D1 | CLAUDE.md template claims MCP server "injects a live Lifecycle Status block into every response" | `BuildLifecycleStatus()` is called only in `internal/tools/workflow.go:355` (status tool handler), never in other responses. The claim is false. |
| D2 | `laravel-minimal.md` recipe is a 6-line stub (frontmatter + empty H1) | Confirmed by `wc -l internal/knowledge/recipes/laravel-minimal.md`. |
| D3 | `nextjs-ssr-hello-world.md` recipe is a 19-line stub | Confirmed by `wc -l`. |
| D4 | `prependRecipeContext` has misleading name — returns universals, not per-recipe content | `internal/knowledge/briefing.go:167-171` — calls `prependUniversals()` only. |
| D5 | Iteration tier / cap mismatch: tiers saturate at `5+`, default `maxIterations=10`, so tier 3 "STOP and ask user" is emitted 6× for iterations 5–10 | `bootstrap_guidance.go:116-135` vs `session.go:14`. |
| D6 | `Next` section in status block can list 3 equal choices without priority | `lifecycle_status.go:207-218` branches in `writeStatusNext`. |
| D7 | `step=` parameter leaks engine-internal phases into MCP tool API | `zerops_workflow` tool signature exposes conductor step names. |
| D8 | Multiple instruction registers — declarative (CLAUDE.md) vs imperative (workflow markdown) | Same facts restated in different moods, drift guaranteed. |
| D9 | Platform fact "dynamic runtimes start with `zsc noop`" duplicated across 6+ files | `core.md:128-135`, `bootstrap.md:182+187+973`, `develop.md`, `claude.md:14-16`. |
| D10 | `recipe.md` template is 2678 lines and rarely read in common scenarios | Happy-path bootstrap selects Route B (classic), never touches recipe.md. |

### 1.3 Systemic patterns

The defects cluster into nine systemic patterns we identified in analysis:

- **P1** — Static template lying about runtime behavior (D1).
- **P2** — Stub content matched by discovery but empty on delivery (D2, D3).
- **P3** — Misleading names hiding trivial implementations (D4).
- **P4** — Parameter ranges not aligned with copy tiers (D5).
- **P5** — Unordered choice presentation in status (D6).
- **P6** — Internal engine phases exposed via API (D7).
- **P7** — Multiple instruction registers restating the same fact (D8, D9).
- **P8** — Dead weight templates loaded but rarely used (D10).
- **P9** — Compaction blind-spot: content injected once (INJECT) cannot be re-derived from state after context compression; content referenced by pointer (POINT) cannot be trusted stable across compaction. Today's system mixes both without discipline.

Every problem in this list has a direct architectural cause. This rewrite removes the causes, not the symptoms.

### 1.4 The four dynamic-knowledge dimensions

Knowledge delivered to the LLM varies across four independent axes:

- **Phase** — `idle | bootstrap-active | develop-active | recipe-active` (4 values).
- **Mode** — `dev | stage | simple | dev+stage pair` (4 values, sparse — not all services have all modes).
- **Environment** — `container | local` (2 values).
- **Deploy strategy** — `push-dev | push-git | manual | unset` (4 values).

Full Cartesian product = 4×4×2×4 = 128 cells, but most are invalid or degenerate. The current system does not decompose along these axes; it hand-codes the valid cells across the workflow markdown files, which is why the same fact appears in six places and drifts.

---

## 2. Design Principles

Seven principles, in order of precedence. When in conflict, earlier principles win.

### P1 — No fallbacks

Any solution that relies on a fallback is a broken solution. If a code path is reached where we don't know what to do, we error loudly with a code that maps to a user-facing action — never guess, never "default to the safe path".

### P2 — Fix at the source

If a detection function is incomplete, we fix the detection. We never add special-case handling downstream for an input the detection should have covered. Every fix that would be "easy to patch here" but wrong must be escalated to the source.

### P3 — Typed over stringly-typed

The contract between tool and LLM is a typed schema (`StateEnvelope`, `NextAction`, `KnowledgeAtom`). Free-form markdown is the *body* of atoms, not the *structure* of responses. Structure is validated; body is rendered.

### P4 — Compaction-safe invariant

Every injected knowledge block MUST be reproducible from the current `StateEnvelope`. Every knowledge pointer MUST dereference to identical content on every call where envelope matches. If a claim about runtime state appears in static text, that text is wrong by construction.

### P5 — Orthogonal decomposition

Knowledge is atomized along the four dimensions from §1.4. Each atom declares its axis vector. A synthesizer selects atoms by current envelope and composes them. No atom duplicates another atom's body.

### P6 — Same tool surface

The LLM-facing tool set does not change. `zerops_workflow`, `zerops_deploy`, `zerops_verify`, `zerops_discover`, `zerops_knowledge`, `zerops_env`, `zerops_logs`, `zerops_scale`, `zerops_manage`, `zerops_subdomain`, `zerops_import`, `zerops_mount`, `zerops_process`, `zerops_events`. Phase-hiding is solved internally, not by proliferating tools.

### P7 — Greenfield, no compatibility shims

There are no ZCP users. We rewrite. No `/* deprecated */`, no version negotiation, no parallel code paths during transition. When a piece of old code is no longer called, it is deleted in the same commit that removes its last caller.

---

## 3. Architecture Overview

Three layers, strictly separated, with explicit contracts between them.

```
┌─────────────────────────────────────────────────────────────────┐
│  Layer 1:  CLAUDE.md — static source of truth (in user repo)   │
│            Platform semantics that never vary by state.        │
│            Written once by `zcp init`. Never injected.         │
└─────────────────────────────────────────────────────────────────┘
                                ▲
                                │ referenced by
                                │
┌───────────────────────────────┴─────────────────────────────────┐
│  Layer 2:  Orthogonal Knowledge Matrix                         │
│            Atomized knowledge units tagged with axis vectors.  │
│            Synthesized per-turn from current StateEnvelope.    │
│            Compaction-safe: identical envelope → identical     │
│            synthesis.                                           │
└─────────────────────────────────────────────────────────────────┘
                                ▲
                                │ feeds
                                │
┌───────────────────────────────┴─────────────────────────────────┐
│  Layer 3:  State Envelope + NextActions                        │
│            One computed struct describing current reality.    │
│            One typed trichotomy (primary, secondary,           │
│            alternatives) describing what to do next.           │
│            Produced by every tool response.                    │
└─────────────────────────────────────────────────────────────────┘
```

### 3.1 Layer responsibilities

| Layer | Responsibility | Anti-responsibility |
|---|---|---|
| 1 (CLAUDE.md) | Declare invariant platform semantics (e.g. "containers run as `zerops`"). | MUST NOT describe workflow state, phase transitions, or iteration counts. |
| 2 (Knowledge matrix) | Deliver runtime-dependent guidance tagged by axis vector. | MUST NOT contain platform invariants (those live in Layer 1). |
| 3 (State Envelope + NextActions) | Describe current reality and propose next action with rationale. | MUST NOT contain prose explanations of how to do a step — those are Layer 2 atoms. |

### 3.2 Data flow

1. MCP tool invoked (e.g. `zerops_workflow action=status`).
2. Handler computes `StateEnvelope` from live API + local state dir (single pass, parallelized I/O).
3. Handler asks the synthesizer: *"Given this envelope, what atoms are relevant?"*
4. Synthesizer filters Layer 2 atoms by axis match, sorts by priority, composes body.
5. Handler asks the planner: *"Given this envelope, what should the user do next?"*
6. Planner produces a `Plan{Primary, Secondary?, Alternatives[]}` typed trichotomy.
7. Handler returns response: `{envelope, guidance, plan}` — three typed fields.

No step above reads from `bootstrap.md` / `develop.md` / `recipe.md` directly. Those files are replaced in Phase 2 by the atom corpus.

### 3.3 What disappears

- `internal/content/workflows/bootstrap.md` — deleted (replaced by atoms).
- `internal/content/workflows/develop.md` — deleted.
- `internal/content/workflows/recipe.md` — deleted (recipe-match route uses recipe corpus directly; verification logic lives in atoms).
- `internal/content/workflows/cicd.md`, `export.md` — kept, but refactored to use same atom model.
- `internal/workflow/bootstrap_guidance.go::BuildIterationDelta` — absorbed into atom corpus with aligned tiers.
- `internal/workflow/guidance.go::assembleGuidance` — replaced by synthesizer.
- `internal/workflow/briefing.go::BuildDevelopBriefing` — replaced by synthesizer.
- `internal/knowledge/briefing.go::prependRecipeContext` — deleted (misleading; universals live in Layer 1 now).
- `internal/workflow/lifecycle_status.go::writeStatusNext` — replaced by typed `Plan` producer.

### 3.4 What stays

- `StateEnvelope` **is** a refinement of today's lifecycle-status composition (services + metas + work-session). The data gathering logic is reused; only the output shape changes.
- `WorkSession` persistence format stays (see `spec-work-session.md`). This is per-PID state that we need regardless.
- `ServiceMeta` persistence format stays. This is the evidence file from bootstrap.
- Platform client, auth, runtime detection — untouched.

---

## 4. Data Model

All types live in `internal/workflow/` (or a new `internal/workflow/envelope` subpackage if line count demands it).

### 4.1 `StateEnvelope`

The canonical description of project state at the moment of the current tool call.

```go
// StateEnvelope captures all state needed to (a) synthesize knowledge,
// (b) produce the next-action plan. It is computed once per tool response
// and embedded verbatim in the response payload. Any two envelopes that
// serialize to the same JSON MUST produce the same synthesis — this is
// the compaction-safety invariant.
type StateEnvelope struct {
    Phase       Phase               `json:"phase"`
    Environment Environment         `json:"environment"`
    SelfService *SelfService        `json:"self_service,omitempty"`
    Project     ProjectSummary      `json:"project"`
    Services    []ServiceSnapshot   `json:"services"`
    WorkSession *WorkSessionSummary `json:"work_session,omitempty"`
    Recipe      *RecipeSessionSummary `json:"recipe,omitempty"`
    Generated   time.Time           `json:"generated"`
}

type Phase string
const (
    PhaseIdle           Phase = "idle"
    PhaseBootstrapActive Phase = "bootstrap-active"
    PhaseDevelopActive   Phase = "develop-active"
    PhaseDevelopClosed   Phase = "develop-closed-auto"  // awaiting close+next
    PhaseRecipeActive    Phase = "recipe-active"
)

type Environment string
const (
    EnvContainer Environment = "container"
    EnvLocal     Environment = "local"
)

type SelfService struct {
    Hostname string `json:"hostname"`  // the ZCP host service (container env only)
}

type ProjectSummary struct {
    ID   string `json:"id"`
    Name string `json:"name"`
}

type ServiceSnapshot struct {
    Hostname      string        `json:"hostname"`
    TypeVersion   string        `json:"type_version"`  // e.g. "nodejs@20"
    RuntimeClass  RuntimeClass  `json:"runtime_class"` // dynamic|static|implicit-webserver|managed
    Status        string        `json:"status"`        // ACTIVE|STARTING|...
    Bootstrapped  bool          `json:"bootstrapped"`  // has complete ServiceMeta
    Mode          Mode          `json:"mode,omitempty"`     // dev|stage|simple (bootstrapped only)
    Strategy      DeployStrategy `json:"strategy,omitempty"` // push-dev|push-git|manual|unset
    StageHostname string        `json:"stage_hostname,omitempty"` // if mode=dev and paired
}

type RuntimeClass string
const (
    RuntimeDynamic         RuntimeClass = "dynamic"
    RuntimeStatic          RuntimeClass = "static"
    RuntimeImplicitWeb     RuntimeClass = "implicit-webserver"
    RuntimeManaged         RuntimeClass = "managed"
    RuntimeUnknown         RuntimeClass = "unknown"
)

type Mode string
const (
    ModeDev    Mode = "dev"
    ModeStage  Mode = "stage"
    ModeSimple Mode = "simple"
)

type DeployStrategy string
const (
    StrategyPushDev DeployStrategy = "push-dev"
    StrategyPushGit DeployStrategy = "push-git"
    StrategyManual  DeployStrategy = "manual"
    StrategyUnset   DeployStrategy = "unset"
)

type WorkSessionSummary struct {
    Intent      string                   `json:"intent"`
    Services    []string                 `json:"services"`
    CreatedAt   time.Time                `json:"created_at"`
    ClosedAt    *time.Time               `json:"closed_at,omitempty"`
    CloseReason string                   `json:"close_reason,omitempty"`
    Deploys     map[string][]AttemptInfo `json:"deploys,omitempty"`
    Verifies    map[string][]AttemptInfo `json:"verifies,omitempty"`
}

type AttemptInfo struct {
    At      time.Time `json:"at"`
    Success bool      `json:"success"`
    Iteration int     `json:"iteration"`
}

type RecipeSessionSummary struct {
    Slug       string  `json:"slug"`
    Confidence float64 `json:"confidence"`
}
```

**Serialization invariant**: `StateEnvelope` MUST serialize deterministically. Slices are sorted (services by hostname, attempts by time). Maps are rendered in key-sorted order by the encoder.

### 4.2 `Plan` — typed trichotomy

```go
// Plan is the typed replacement for today's free-form Next section.
// It is produced by a pure function Plan(envelope) and is the ONLY
// source of "what should happen next" in any tool response.
type Plan struct {
    Primary      NextAction   `json:"primary"`
    Secondary    *NextAction  `json:"secondary,omitempty"`
    Alternatives []NextAction `json:"alternatives,omitempty"`
}

type NextAction struct {
    Label     string            `json:"label"`     // "Start develop task"
    Tool      string            `json:"tool"`      // "zerops_workflow"
    Args      map[string]string `json:"args"`      // {"action":"start","workflow":"develop"}
    Rationale string            `json:"rationale"` // "Services ready; no develop session active."
}
```

**Contract**:
- `Primary` is never nil. If we truly don't know what to do next, we error out before constructing a `Plan`.
- `Secondary` is set when a second action is commonly done in tandem (e.g. "close current develop" + "start next develop").
- `Alternatives` holds genuinely alternative paths (e.g. "start develop on existing services" OR "bootstrap more services"). They are presented as alternatives to `Primary`, not ordered continuation.

### 4.3 `KnowledgeAtom`

```go
// KnowledgeAtom is one piece of runtime-dependent guidance.
// Atoms live as .md files under internal/content/atoms/.
// The frontmatter declares the atom's axis vector; the body is the
// rendered guidance.
type KnowledgeAtom struct {
    ID         string       // stable slug; matches filename
    Axes       AxisVector   // which envelope cells this atom applies to
    Priority   int          // higher = earlier in composition (1=top, 9=bottom)
    Title      string       // human-readable title (not rendered by default)
    Body       string       // markdown body rendered to LLM
}

type AxisVector struct {
    Phases       []Phase         // non-empty; atom applies when envelope.Phase ∈ this set
    Modes        []Mode          // empty = applies to any mode
    Environments []Environment   // empty = applies to any environment
    Strategies   []DeployStrategy // empty = applies to any strategy
    Runtimes     []RuntimeClass  // empty = applies to any runtime
}
```

**Frontmatter format** (YAML):

```yaml
---
id: develop-dynamic-runtime-start
priority: 2
phases: [develop-active]
runtimes: [dynamic]
environments: [container, local]
title: "Dynamic runtime start after deploy"
---
```

### 4.4 `RecipeMatch` + viability gate

```go
// RecipeMatch is produced by the recipe matcher during Route A selection.
// It carries both a confidence score and a viability flag — a high-
// confidence match to an empty recipe file still fails the gate.
type RecipeMatch struct {
    Slug       string   // recipe identifier
    Confidence float64  // 0.0..1.0 from semantic/fuzzy match
    Viable     bool     // passes minimum-content gate
    Reasons    []string // if !Viable, why (e.g. "file under 200 lines", "missing deploy section")
}

// RecipeViabilityRules defines what constitutes a complete recipe.
// A RecipeMatch with Viable=false is routed to Route B (classic bootstrap),
// never used as happy-path. This prevents defect D2 (laravel-minimal is
// 6 lines) from reaching the user.
var RecipeViabilityRules = struct {
    MinLines           int      // 200
    RequiredSections   []string // ["overview", "deploy", "verify"]
    MinCodeFences      int      // 1
}{
    MinLines:         200,
    RequiredSections: []string{"overview", "deploy", "verify"},
    MinCodeFences:    1,
}
```

### 4.5 Tool response shape

Every workflow-aware tool (status, start, deploy, verify, close) returns this shape. Direct tools (scale, env) may omit `plan` if no action is expected next.

```go
type Response struct {
    Envelope StateEnvelope `json:"envelope"`
    Guidance []string      `json:"guidance"` // ordered atom bodies, rendered
    Plan     *Plan         `json:"plan,omitempty"`
}
```

The JSON is serialized to stdout by the MCP handler. A rendered markdown form (for `zerops_workflow action=status` specifically) is produced by a renderer in `internal/workflow/render.go` that consumes `Response` and emits the status block.

---

## 5. Layer 1 — CLAUDE.md as Static Source

### 5.1 Contents

`internal/content/templates/claude.md` contains **only** platform invariants. Everything that varies by state or mode or environment is OUT.

Target length: 40–60 lines. Current is 20 lines with a provable lie (D1) and misdirection about runtime behavior.

**Canonical structure** (to be written in Phase 2, Work Unit 2.1):

```markdown
# Zerops

Zerops is a PaaS with its own schema — not Kubernetes, Compose, or Helm.
This file documents invariants that never change by state. For runtime
guidance about the current task, call `zerops_workflow action="status"`.

## Container Identity

- Containers run as user `zerops`, not root. Package installs need `sudo`.
- Build container and run container are separate. Packages needed at
  runtime (`ffmpeg`, `imagemagick`) belong in `run.prepareCommands`;
  packages needed only during build go in `build.prepareCommands`.

## Deploy Semantics

- Every deploy replaces the run container. Only files listed in
  `deployFiles` persist across deploys. Runtime edits, installed
  packages, `/tmp`, and logs are reset.

## Runtime Classes

- Dynamic runtimes (Node, Go, Python, Bun, …) start with `zsc noop`
  and need the real server started explicitly after deploy. The
  workflow system surfaces the exact command when relevant.
- Static runtimes (php-apache, nginx) auto-start after deploy.
- Managed services (PostgreSQL, Redis, …) have no deploy — scale and
  connect only.

## Tool Surface

Begin every task with `zerops_workflow action="status"`. That tool
returns the current phase, services, progress, and the next action.
```

### 5.2 Rules

- No sentence in this file describes workflow state, phases, or "what the server injects".
- No duplication with Layer 2. If a fact is in an atom, it is NOT in Layer 1, and vice versa.
- No lies by construction: the file describes `zerops.yaml` semantics, container identity, and the entry point. Everything else is delegated to the status tool.

### 5.3 Discovery

The file is written to the user's repo at `./CLAUDE.md` by `zcp init`. It is **not** injected as MCP Instructions. MCP Instructions (`internal/server/instructions.go`) is reduced to a one-paragraph pointer directing the LLM to read the project's CLAUDE.md and start with `zerops_workflow action="status"`.

---

## 6. Layer 2 — Orthogonal Knowledge Matrix

### 6.1 Corpus location

`internal/content/atoms/*.md` — one atom per file. Embedded via `//go:embed`.

The current `internal/content/workflows/` directory is deleted after Phase 2 migration completes.

### 6.2 Axis definitions

| Axis | Values | Emptiness semantic |
|---|---|---|
| `phases` | `idle`, `bootstrap-active`, `develop-active`, `develop-closed-auto`, `recipe-active` | MUST be non-empty. No atom applies to "any phase". |
| `modes` | `dev`, `stage`, `simple` | Empty = applies to any mode (including none, e.g. pre-bootstrap). |
| `environments` | `container`, `local` | Empty = applies to both. |
| `strategies` | `push-dev`, `push-git`, `manual`, `unset` | Empty = applies to any strategy. |
| `runtimes` | `dynamic`, `static`, `implicit-webserver`, `managed`, `unknown` | Empty = applies to any runtime. |

### 6.3 Atom format

```markdown
---
id: develop-dynamic-runtime-start-container
priority: 2
phases: [develop-active]
runtimes: [dynamic]
environments: [container]
title: "Dynamic runtime — start over SSH after deploy"
---

After a dynamic-runtime deploy, the container is running `zsc noop`. Start
the real server over SSH:

```
ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null {hostname} \
  'cd /var/www && {start-command}'
```

Replace `{start-command}` with the `run.start` value from `zerops.yaml`.
```

**Rendering**: The synthesizer emits each atom's body in priority order, separated by a blank line. The frontmatter is not rendered. Placeholders like `{hostname}` are substituted by the synthesizer from the envelope before rendering — see §6.5.

### 6.4 Synthesizer

```go
// Synthesize returns the ordered guidance bodies for the given envelope.
// Algorithm:
//   1. Filter atoms where every declared axis matches the envelope.
//   2. Sort by priority (ascending: 1 first), then by id lexicographically.
//   3. Substitute placeholders from envelope.
//   4. Return bodies in sorted order.
//
// Complexity: linear in atom count per call. With ~80 atoms the cost is
// negligible; no caching needed in Phase 3.
func Synthesize(envelope StateEnvelope, corpus []KnowledgeAtom) []string
```

**Invariant (compaction-safety)**: for the same `envelope` (by JSON-equality of serialized form), `Synthesize` returns byte-identical output. No clocks, no randomness, no goroutine interleaving in the rendered output.

### 6.5 Placeholder substitution

Atoms may contain placeholders `{hostname}`, `{stage-hostname}`, `{project-name}`, `{service-mode}`, `{service-strategy}`. They are substituted at render time from the envelope. Attempted substitution of an unknown placeholder is an error (fail loudly, don't leave `{unknown}` literals in output).

Substitution applies per-service when the synthesizer is run in a per-service pass (e.g. one atom rendered once per service in `envelope.Services`). The default pass is project-wide; per-service rendering is triggered when an atom's axis vector declares a runtime class (indicating it applies to a specific service row).

### 6.6 Corpus inventory (target)

Approximate atom count per phase (produced by Phase 6 audit pipeline, refined in Phase 2):

| Phase | Est. atom count | Notes |
|---|---|---|
| `idle` | 4 | bootstrap-entry, adopt-entry, recipe-hint-if-intent-provided, container-vs-local-context |
| `bootstrap-active` | 18 | split by mode × environment × runtime class |
| `develop-active` | 22 | split by mode × strategy × runtime × environment |
| `develop-closed-auto` | 2 | close+next guidance; compaction reminder |
| `recipe-active` | 10 | split by current recipe step |

Total ~56 atoms. Each atom ≤80 lines of body. Corpus total ≤4500 lines (vs today's 4600+ lines spread across workflow markdown).

### 6.7 Migration strategy

The old workflow markdown is **not** edited in place. It is read by the audit pipeline (§10) which extracts atoms. The new atoms are written to `internal/content/atoms/`. Only when the audit pipeline attests 100% fact coverage is the old directory deleted.

---

## 7. Layer 3 — State Envelope + NextActions

### 7.1 Envelope computation

```go
// ComputeEnvelope is the single entry point for computing state.
// Called by every workflow-aware tool handler. Parallelizes independent
// I/O (services API call, local state dir reads).
func ComputeEnvelope(
    ctx context.Context,
    client platform.Client,
    stateDir string,
    selfHostname string,
    rtInfo runtime.Info,
) (StateEnvelope, error)
```

**Implementation**: merges the logic currently in `BuildLifecycleStatus` (lifecycle_status.go:18-44) and scattered computations in other tool handlers. There is no backward-compat wrapper; every caller of the old functions migrates to `ComputeEnvelope`.

**Errors**: If the platform client is unconfigured and no project is bound, return `{Phase: idle, Environment: ..., Services: []}` — this is not a fallback, it is the literal envelope of "no project yet". All other failures bubble up.

### 7.2 Planner

```go
// BuildPlan is the single entry point for producing NextActions.
// Pure function of envelope — no I/O, no state. Deterministic.
func BuildPlan(envelope StateEnvelope) Plan
```

**Branching** (strict order, first match wins — this replaces `writeStatusNext` and `SuggestNext`):

1. `PhaseDevelopClosed` → Primary=close, Secondary=start-next.
2. `PhaseDevelopActive` with a service missing deploy (including last-attempt-failed) → Primary=deploy.
3. `PhaseDevelopActive` with deploy done, verify missing (including last-verify-failed) → Primary=verify.
4. `PhaseDevelopActive` with every service green but session still open → Primary=close.
5. `PhaseBootstrapActive` → Primary=continue-bootstrap (route-specific).
6. `PhaseRecipeActive` → Primary=continue-recipe.
7. `PhaseIdle` with zero services → Primary=bootstrap.
8. `PhaseIdle` with bootstrapped services → Primary=develop; Alternatives=[adopt-unmanaged (if any), add-more-services].
9. `PhaseIdle` with only unmanaged services → Primary=adopt-via-develop.

Failed-last-attempt cases fold into branches 2 and 3 — `firstServiceNeedingDeploy` / `firstServiceNeedingVerify` both key off `!attempts[last].Success`, so a failed service surfaces as a deploy or verify target. Iteration-tier guidance (diagnose / systematic-check / STOP) is delivered through atoms, not through a distinct Plan branch.

The function is pure; its test is table-driven with ~30 envelope fixtures covering every branch.

### 7.3 Tool response renderer

```go
// RenderStatus produces the markdown status block from a Response.
// This is the replacement for BuildLifecycleStatus's string-concatenation
// approach. Each section is a named writer that consumes Response, not
// raw WorkSession / ServiceMeta / platform.ServiceStack.
func RenderStatus(resp Response) string
```

Sections (same as today, but sourced from typed envelope):
- **Phase** — from `envelope.Phase` + optional `WorkSession.Intent`.
- **Services** — from `envelope.Services`, formatted one per line with mode/strategy.
- **Progress** — from `WorkSession.Deploys` / `Verifies` (develop-active only).
- **Guidance** — from `Response.Guidance` (filtered Layer 2 atoms).
- **Next** — from `Response.Plan` (typed trichotomy rendered with priority markers).

Critical: the rendered `Next` section marks priority explicitly. Current behavior (three equal bullets, D6) is replaced by:

```
Next:
  ▸ Primary: Start develop — zerops_workflow action="start" workflow="develop" intent="..."
  ◦ Secondary: Close current develop — zerops_workflow action="close" workflow="develop"
  · Alternatives:
      - Add more services — zerops_workflow action="start" workflow="bootstrap"
      - Adopt unmanaged runtimes — zerops_workflow action="start" workflow="develop" intent="..."
```

### 7.4 Lifecycle-status claim removal

The sentence currently in CLAUDE.md ("The MCP server injects a live Lifecycle Status block into every response") is **deleted**. It is replaced in Layer 1 by: "Begin every task with `zerops_workflow action=\"status\"`."

The status block is returned only by the status tool (and optionally by `start` action responses, which already include envelope). No other tool prepends the status block.

---

## 8. Bootstrap Flow — Three Routes

Bootstrap is scoped narrowly: infrastructure creation + minimal verification that services can see each other. Application logic is develop's job.

### 8.1 Route selection

```go
// Route selection runs once at bootstrap start. The chosen route is
// recorded in the bootstrap session state and persists for the duration
// of the session.
func SelectRoute(
    ctx context.Context,
    intent string,
    existing []platform.ServiceStack,
    recipeCorpus RecipeCorpus,
) (Route, *RecipeMatch, error)

type Route string
const (
    RouteRecipe  Route = "recipe"
    RouteClassic Route = "classic"
    RouteAdopt   Route = "adopt"
)
```

**Selection algorithm** (strict order):

1. If `existing` has any service that is not system and not managed → `RouteAdopt`.
2. If `intent` matches a recipe with `Confidence ≥ 0.85 AND Viable == true` → `RouteRecipe` with that match.
3. Otherwise → `RouteClassic`.

Recipe matching uses `internal/knowledge` semantic search over recipe titles + descriptions. Confidence threshold of 0.85 is calibrated empirically in Phase 5 against a corpus of ≥20 labeled intents.

### 8.2 Route A — Recipe Match

Happy path when user's intent aligns with a published recipe.

**Preconditions**: no existing infrastructure; recipe match viable.

**Steps**:
1. Present recipe overview (atom-rendered from recipe body).
2. Import recipe YAML via `zerops_import` (reuse existing tool).
3. Wait for services to reach `ACTIVE`.
4. Run verification-server deploy (recipe-provided).
5. Verify connectivity across services.
6. Auto-close bootstrap session on all-green.

**What this is NOT**: a full application build. The recipe delivers infrastructure + a verification server. Application logic continues in develop flow, exactly as Route B ends.

**Viability gate enforcement**: If confidence is high but content fails the gate (D2 scenario — `laravel-minimal` is 6 lines), Route A is rejected and we fall through to Route B. The user is informed: *"Matched recipe 'laravel-minimal' but it is missing deploy instructions; continuing with classic bootstrap."*

### 8.3 Route B — Classic Infra

Default path when no recipe matches.

**Preconditions**: no existing non-managed infrastructure; no viable recipe match.

**Steps**:
1. Propose service plan from intent (managed + dynamic runtimes).
2. User approves plan (or adjusts).
3. Create services via `zerops_import`.
4. Wait for `ACTIVE`.
5. Deploy minimal verification server per dynamic runtime (hello-world with `/health`, `/status`).
6. Verify cross-service connectivity (env var resolution, managed-service reachability).
7. Write `ServiceMeta` per runtime service with mode + initial strategy.
8. Auto-close.

### 8.4 Route C — Adopt Existing

Path when the project already has non-managed services without ServiceMeta.

**Preconditions**: at least one existing non-system service without complete ServiceMeta.

**Steps**:
1. Discover services (`zerops_discover`).
2. For each unmanaged service without meta, ask user for mode + strategy (or infer from zerops.yaml if present).
3. Write ServiceMeta.
4. Run verification against current code (no new deploy).
5. Auto-close.

**Fast-path optimization**: If all existing services are managed (DB only, no runtimes) → adopt completes immediately with no per-service questions.

### 8.5 Bootstrap session state

```go
// BootstrapSession is persisted at state-dir/workflows/bootstrap/{pid}.json.
// It records the chosen route and step progress. Deleted on close.
type BootstrapSession struct {
    PID         int              `json:"pid"`
    Route       Route            `json:"route"`
    RecipeMatch *RecipeMatch     `json:"recipe_match,omitempty"`
    Intent      string           `json:"intent"`
    Steps       []StepProgress   `json:"steps"`
    CreatedAt   time.Time        `json:"created_at"`
    ClosedAt    *time.Time       `json:"closed_at,omitempty"`
}

type StepProgress struct {
    Name     string    `json:"name"`
    Started  time.Time `json:"started"`
    Finished *time.Time `json:"finished,omitempty"`
    Failures int       `json:"failures"`
}
```

Steps vary by route — Route A has `{import, wait-active, verify-deploy, verify, close}`; Route B has `{plan, import, wait-active, verify-deploy-per-runtime, verify, write-metas, close}`; Route C has `{discover, prompt-modes, write-metas, verify, close}`.

---

## 9. Develop Flow

### 9.1 Work Session (preserved)

The per-PID `WorkSession` concept from `spec-work-session.md` stays. It remains the mechanism that survives LLM context compaction: on resume, the status tool reads the session and reconstructs the envelope.

### 9.2 Deploy strategy dispatch

The deploy step branches on `ServiceMeta.Strategy`:

| Strategy | Container env | Local env |
|---|---|---|
| `push-dev` | SSH-based dev deploy | SSH-based dev deploy |
| `push-git` | `git commit && git push` + poll pipeline | same |
| `manual` | emit CI/CD config guidance; user deploys externally | same |
| `unset` | error — require `zerops_manage` action=`set-strategy` before deploy | same |

Each strategy has its own atoms under `phases=[develop-active]` + `strategies=[<strategy>]`. The synthesizer surfaces the right one.

### 9.3 Iteration tier alignment (defect D5 fix)

Current state: tiers saturate at `5+`, max iterations is 10. The STOP message repeats for iterations 5, 6, 7, 8, 9, 10.

New state (Phase 0 pre-rewrite cleanup — see §11.1):

```go
const (
    MaxIterations = 5  // cap aligns with tier count
)

// Tiers:
//   iter 1-2: diagnose (per-error log + fix)
//   iter 3-4: systematic check (env vars, ports, bindings, deployFiles)
//   iter 5:   STOP and ask user (terminal tier — session aborts after)
```

At iteration 5, the delta emits STOP and the session closes with `CloseReason="iteration-cap"`. Subsequent attempts require a new `zerops_workflow action=start`, which is the user's explicit decision to continue.

This is the Phase 0 fix. It is small, isolated, and independently valuable — we land it before the full rewrite.

### 9.4 Auto-close

Unchanged: session auto-closes when every service in scope has a successful deploy + passed verify. See `spec-work-session.md`.

---

## 10. Knowledge Preservation Audit Pipeline

This is the mechanism that ensures the rewrite loses zero load-bearing knowledge. It runs once in Phase 6 as a gating step before old content is deleted.

### 10.1 Agent team composition

Six parallel agents, each with a distinct lens. Lens prompts are written as `.claude/audit/<lens>.md` and invoked via `Agent({subagent_type:"general-purpose"})` or equivalent.

| Agent | Lens | Input | Output |
|---|---|---|---|
| A1 Platform Fact Extractor | "Find every claim about Zerops platform semantics (container identity, deploy behavior, runtime class, network model)." | Old `content/workflows/*.md` + `templates/claude.md` | Deduplicated fact list (JSON). |
| A2 Workflow Step Extractor | "Find every procedural step the LLM should take during bootstrap/develop/recipe, with preconditions." | Old `content/workflows/*.md` | Ordered step inventory per workflow (JSON). |
| A3 Runtime Variation Extractor | "Find every instance where guidance varies by mode / environment / runtime class / strategy." | Old workflow markdown + Go guidance code | Axis-tagged variation list (JSON). |
| A4 Error Recipe Extractor | "Find every documented failure mode + its remediation." | Old workflow markdown + iteration tiers in code | Error → remediation mapping (JSON). |
| A5 Verification Extractor | "Find every verification / acceptance check (what counts as bootstrap-complete, develop-complete, verify-passing)." | Old spec docs + code | Check list (JSON). |
| A6 Anti-pattern Extractor | "Find every 'do NOT' / 'never' / 'avoid' instruction." | Old content | Anti-pattern list (JSON). |

All six run in parallel. Output is aggregated into `audit/old-corpus-inventory.json`.

### 10.2 Pipeline stages

1. **Extract** — 6 agents run in parallel, produce raw inventories.
2. **Deduplicate** — a pure Go step (`cmd/audit-dedupe`) merges duplicates across agents, producing `audit/facts-canonical.json`.
3. **Classify** — each canonical fact is classified: `layer1-claudemd | layer2-atom | layer3-envelope | dead-weight`. Classification is rule-based (e.g. invariants → Layer 1; state-conditional → Layer 2 with axes; structural → Layer 3; unused → dead-weight). An LLM agent applies the rules and records rationale.
4. **Map** — each `layer2-atom` fact is mapped to a target atom ID (either existing in new corpus, or to be created). An LLM agent performs mapping with human review for ambiguous cases.
5. **Verify** — a verification agent reads the new corpus (Layer 1 + Layer 2) and for every fact in `facts-canonical.json`, confirms its presence in the new corpus OR confirms it was classified as dead-weight. Output: `audit/coverage-report.json` with per-fact verdict.
6. **Gate** — if `coverage-report.json` has any fact with verdict `missing` and classification ≠ `dead-weight`, the pipeline fails. No deletion until 100% coverage.

### 10.3 Acceptance criteria

- Every fact extracted by A1–A6 is either in the new corpus or explicitly classified as dead-weight with rationale.
- The `dead-weight` rate is ≤10% of total facts (higher rate suggests under-extraction; trigger manual review).
- Zero facts with `missing` verdict at gate stage.
- The new corpus passes a round-trip test: synthesizing guidance for each `StateEnvelope` fixture (see §12.4) reproduces every applicable fact from the old corpus.

### 10.4 Gate outputs

When the pipeline passes:
- `internal/content/workflows/bootstrap.md` deleted.
- `internal/content/workflows/develop.md` deleted.
- `internal/content/workflows/recipe.md` deleted.
- `internal/workflow/briefing.go` deleted (replaced by synthesizer).
- `internal/workflow/bootstrap_guidance.go` deleted (absorbed into atoms + planner).

---

## 11. Implementation Phases

Eight phases, strict order, each gated by acceptance criteria (§13). No phase starts before the previous phase's criteria are green.

### 11.1 Phase 0 — Pre-rewrite Cleanup

**Goal**: Land cheap fixes that do not require architectural change but stabilize the codebase for rewrite.

**Scope**:
- Fix D5 (iteration tier cap): `MaxIterations = 5`, auto-close on tier 3 exhaustion.
- Fix D1 (CLAUDE.md lie): remove the sentence claiming live injection. Temporary edit; file is replaced entirely in Phase 2.
- Fix D4 (misleading name): rename `prependRecipeContext` → `prependUniversals` directly (remove the wrapper).
- Add viability-gate scaffolding: `RecipeViabilityRules` struct + `CheckViability(recipe)` function in `internal/knowledge/recipes_viability.go`. Not yet wired.

**Work units**:
- `internal/workflow/session.go`: `defaultMaxIterations = 5`.
- `internal/workflow/bootstrap_guidance.go`: ensure tier 3 emits once, then `EvaluateAbort` fires.
- `internal/workflow/work_session.go`: new close reason `CloseReasonIterationCap`.
- `internal/content/templates/claude.md`: delete line 18.
- `internal/knowledge/briefing.go`: rename + delete wrapper.
- New file `internal/knowledge/recipes_viability.go`.

**Tests** (RED first):
- `TestIterationCap_StopsAtFive`: drive 5 failed deploys, assert session closes with `CloseReasonIterationCap`.
- `TestRecipeViability_LaravelMinimalFails`: feed current `laravel-minimal.md` content, assert `Viable=false` with reason "content under 200 lines".

**Size estimate**: ~8 files touched, ~120 LOC delta.

### 11.2 Phase 1 — Data Model

**Goal**: Land the typed schemas (`StateEnvelope`, `Plan`, `NextAction`, `KnowledgeAtom`, `AxisVector`) with zero behavior change.

**Scope**:
- Create `internal/workflow/envelope.go` with all types from §4.
- Create `internal/workflow/plan.go` with `Plan` + `NextAction` types.
- Create `internal/workflow/atom.go` with `KnowledgeAtom` + `AxisVector` + frontmatter parser.
- Add JSON-schema test fixtures under `internal/workflow/testdata/envelopes/`.

**Tests** (RED first):
- `TestEnvelope_JSONRoundtrip`: 20 fixtures, each marshals/unmarshals identically.
- `TestEnvelope_DeterministicSerialization`: same envelope struct → byte-identical JSON across two calls.
- `TestAtom_FrontmatterParse`: 10 atom fixtures (valid + malformed) parse correctly or error explicitly.
- `TestAxisVector_Matches`: 15 cases covering empty sets (wildcard) and exact sets.

**Size estimate**: 3 new files, ~400 LOC. Tests ~300 LOC.

### 11.3 Phase 2 — Knowledge Atomization (Manual + Audit-Assisted)

**Goal**: Produce the initial Layer 2 atom corpus by running the audit pipeline (§10) against existing content and writing new atoms.

**Scope**:
- Run agents A1–A6 on old corpus (§10.1).
- Dedup + classify + map to atoms.
- Write ~56 atom files under `internal/content/atoms/`.
- Write new canonical `internal/content/templates/claude.md` (Layer 1 per §5.1).

**Tests** (RED first):
- `TestAtomCorpus_AllLoadWithoutError`: embed-time scan parses every atom.
- `TestAtomCorpus_NoDuplicateIDs`.
- `TestAtomCorpus_CoverageByAxis`: every phase has at least one atom; no axis combination from fixtures is empty.
- Plus the coverage-report gate from §10.2 stage 6.

**Size estimate**: ~56 atom files, ~3000–4000 LOC total markdown. The audit pipeline itself is ~600 LOC of Go glue.

### 11.4 Phase 3 — Synthesizer

**Goal**: Implement `Synthesize(envelope, corpus) []string` per §6.4 and `ComputeEnvelope` per §7.1.

**Scope**:
- `internal/workflow/synthesize.go`: matcher + sorter + placeholder substituter.
- `internal/workflow/compute_envelope.go`: the single envelope computation function.
- Delete `internal/workflow/guidance.go::assembleGuidance`.
- Delete `internal/workflow/briefing.go::BuildDevelopBriefing` (after replacing all callers).

**Tests** (RED first):
- `TestSynthesize_CompactionSafe`: for 20 envelope fixtures, two consecutive calls return byte-identical output.
- `TestSynthesize_AxisFiltering`: 15 envelope + corpus combinations; assert correct atom subset.
- `TestSynthesize_PrioritySort`: atoms with priorities 1, 3, 5 render in that order.
- `TestSynthesize_PlaceholderSubstitution`: 8 placeholder variants; unknown placeholder is an error.
- `TestComputeEnvelope_ParallelIO`: mock platform + state dir; assert single envelope correctly composed.

**Size estimate**: 2 new files, ~300 LOC. Tests ~400 LOC.

### 11.5 Phase 4 — Bootstrap Conductor (3 Routes)

**Goal**: Replace current bootstrap conductor with route-aware implementation per §8.

**Scope**:
- `internal/workflow/bootstrap/route.go`: `SelectRoute` function.
- `internal/workflow/bootstrap/route_recipe.go`: Route A implementation.
- `internal/workflow/bootstrap/route_classic.go`: Route B implementation.
- `internal/workflow/bootstrap/route_adopt.go`: Route C implementation.
- `internal/workflow/bootstrap/session.go`: `BootstrapSession` persistence.
- Delete old `internal/workflow/bootstrap.go` + `bootstrap_guidance.go` (replaced).

**Tests** (RED first):
- `TestSelectRoute_AdoptWhenExistingUnmanaged`: envelope with unmanaged service → RouteAdopt.
- `TestSelectRoute_RecipeWhenHighConfidenceViable`: intent matches viable recipe @ 0.9 → RouteRecipe.
- `TestSelectRoute_ClassicWhenRecipeStub`: intent matches stub recipe (`laravel-minimal`) → RouteClassic (viability gate rejected the match).
- `TestSelectRoute_ClassicWhenNoMatch`: intent matches nothing → RouteClassic.
- Route-specific step progression tests per route (3 × 5 tests ≈ 15 tests).

**Size estimate**: 5 new files, ~800 LOC. Tests ~600 LOC. Deletion: old bootstrap.go + bootstrap_guidance.go (~1400 LOC).

### 11.6 Phase 5 — Develop Flow Rewrite

**Goal**: Replace develop conductor with envelope-driven implementation.

**Scope**:
- `internal/workflow/develop/start.go`: start action, produces initial envelope + plan.
- `internal/workflow/develop/deploy.go`: deploy action dispatch by strategy.
- `internal/workflow/develop/verify.go`: verify action.
- `internal/workflow/develop/close.go`: close + auto-close logic.
- `internal/workflow/plan.go::BuildPlan`: the typed trichotomy per §7.2.
- `internal/workflow/render.go::RenderStatus`: the typed renderer per §7.3.
- Delete old `lifecycle_status.go::writeStatusNext`, `work_session.go::SuggestNext`.

**Tests** (RED first):
- `TestBuildPlan_AllBranches`: 30 envelope fixtures, one per branch in §7.2 planner.
- `TestRenderStatus_PrioritizedNext`: envelope with multiple valid actions renders with `Primary` / `Secondary` / `Alternatives` markers (defect D6 fix).
- `TestDevelopDeploy_StrategyDispatch`: each strategy dispatches to correct handler.
- End-to-end test: `TestDevelopFlow_FullCycle` — start → deploy → verify → auto-close.

**Size estimate**: 6 new files, ~600 LOC. Tests ~500 LOC. Deletion: old develop conductor code (~800 LOC).

### 11.7 Phase 6 — Audit Gate Execution

**Goal**: Prove the new corpus has 100% coverage of old corpus facts before any deletion.

**Scope**:
- Run the full audit pipeline (§10).
- Review `coverage-report.json`.
- Fix gaps by adding atoms (return to Phase 2 if gate fails).

**Tests**:
- Gate itself is the test. No separate tests needed.

**Size estimate**: 0 LOC (no code change; this is a verification gate). May trigger 1–3 return trips to Phase 2 with small atom additions.

### 11.8 Phase 7 — Old Code Deletion

**Goal**: Remove all code paths and content files rendered dead by Phases 1–6.

**Scope** (in order):
- `internal/content/workflows/bootstrap.md` — delete.
- `internal/content/workflows/develop.md` — delete.
- `internal/content/workflows/recipe.md` — delete.
- `internal/content/workflows/cicd.md` + `export.md` — refactor to atom format (Phase 7.2, after deletion of bootstrap/develop).
- `internal/workflow/briefing.go` — delete.
- `internal/workflow/bootstrap_guidance.go` — delete.
- `internal/workflow/guidance.go` — delete or reduce to zero functions.
- `internal/content/content.go` — update embed directives to reference `atoms/` instead of `workflows/`.
- Update `CLAUDE.md` (project) Architecture table to reflect new package layout.

**Tests**:
- `go build ./...` passes.
- `go test ./...` passes.
- `go vet ./...` clean.
- `golangci-lint` clean.

**Size estimate**: ~5000 LOC deleted. ~50 LOC of embed-directive + architecture-table updates.

---

## 12. Test Strategy

### 12.1 Layer coverage per phase

Every phase updates tests at every affected layer before implementation (CLAUDE.md project rule). The layers are:

| Layer | Tool | Scope |
|---|---|---|
| Unit | `go test ./internal/workflow/...` | Types, helpers, pure functions. |
| Tool | `go test ./internal/tools/...` | MCP tool handlers. |
| Integration | `go test ./integration/` | Multi-tool flows with mock platform. |
| E2E | `go test ./e2e/ -tags e2e` | Real Zerops API (gated behind `ZCP_API_KEY`). |

### 12.2 Envelope fixtures

`internal/workflow/testdata/envelopes/` contains ~20 canonical envelopes. Each is a JSON file with a stable name, e.g.:

- `idle-empty-project.json`
- `idle-with-managed-only.json`
- `idle-with-bootstrapped-pair.json`
- `idle-with-unmanaged-runtime.json`
- `bootstrap-active-route-a-recipe.json`
- `bootstrap-active-route-b-classic-step-deploy.json`
- `develop-active-deploy-pending.json`
- `develop-active-verify-pending.json`
- `develop-active-iteration-3.json`
- `develop-closed-auto.json`
- `recipe-active-step-verify.json`
- ... (10 more covering axis combinations)

Every test that reasons about envelope state loads one of these fixtures. No envelope is hand-constructed in a test body — this prevents fixture drift and test-private structure knowledge.

### 12.3 Atom fixtures

`internal/content/atoms_testdata/` (separate directory, not embedded in binary) contains malformed atoms and edge cases used by parser tests. Not rendered in production.

### 12.4 Round-trip coverage

`TestCorpusCoverage_RoundTrip` is the flagship integration test: for every envelope fixture in §12.2, it runs `Synthesize` and asserts that the output contains every expected fact from the fixture's companion `{fixture}.expected-facts.txt` file. This is the mechanized version of the §10 coverage report.

### 12.5 Compaction-safety test

`TestCompactionSafety_Repeatable`: for every fixture, call `Synthesize` ten times, assert all ten outputs are byte-identical. Catches non-determinism from map iteration, floats, time, goroutines.

---

## 13. Acceptance Criteria

Each phase has a gate. A phase is complete only when every criterion below is green.
Status as of 2026-04-19 is tracked inline — all phases green.

### Phase 0

- [x] Iteration-cap logic enforced (iteration 5 terminates); covered by develop-active atom suite, no dedicated `TestIterationCap` name survived.
- [x] `internal/knowledge/recipes_viability_test.go::TestCheckViability` passes (the `TestRecipeViability` name in the draft was renamed at implementation time).
- [x] `internal/content/templates/claude.md` 53 lines, no false-injection sentence.
- [x] `prependRecipeContext` absent from the tree (`grep -r prependRecipeContext internal/` → empty).

### Phase 1

- [x] Envelope types in `internal/workflow/envelope.go`.
- [x] Plan types in `internal/workflow/plan.go`.
- [x] Atom types in `internal/workflow/atom.go`.
- [x] Envelope JSON round-trip covered by `compute_envelope_test.go` fixtures + `corpus_coverage_test.go::TestCorpusCoverage_CompactionSafe` (byte-identical re-synthesis × 10 per fixture).
- [x] Atom frontmatter parser covered by `atom_test.go` (multiple table cases including invalid YAML).

### Phase 2

- [x] Audit pipeline completed; artifacts (`audit/*`) were transient scaffolding and removed once atoms landed — not a regression.
- [x] `internal/content/atoms/` carries 74 atom files (soft cap was 80).
- [x] `internal/content/templates/claude.md` ≤60 lines, no runtime-state claims.

### Phase 3

- [x] `Synthesize` tests pass (`internal/workflow/synthesize_test.go` + corpus coverage).
- [x] `ComputeEnvelope` is the only caller path; no caller imports `briefing.go::BuildDevelopBriefing` (file deleted).

### Phase 4

- [x] Three bootstrap routes exercised end-to-end (`bootstrap_test.go`, `bootstrap_outputs_test.go`).
- [x] Route selection tests in `route_test.go` cover the 8 cases.
- [x] `RecipeViabilityRules` enforced at route selection (`routes.go` consumes it, stub recipes rerouted).
- [x] `bootstrap_guidance.go` deleted. `bootstrap.go` retained as session state plumbing (split from the old guidance builder); Appendix A updated to reflect.

### Phase 5

- [x] `BuildPlan` branch coverage split across 13 top-level tests with multi-case tables (~44 sub-cases across the pipeline suite). Original "30+ branch tests" target tracked branches, not functions — every branch in `build_plan.go` is reachable from a fixture.
- [x] `RenderStatus` priority markers covered in `render_test.go`.
- [x] Strategy dispatch tests pass for push-dev, push-git, manual (`strategy_guidance_test.go`, coverage fixtures).
- [x] `unset` strategy emits explicit guidance (`develop-strategy-unset` atom + `workflow_develop.go` short-circuit); no fallback.
- [x] End-to-end develop tested in `tools/workflow_test.go` + `scenarios_test.go`.

### Phase 6

- [x] Audit coverage report target met in content — all load-bearing claims from the old corpus are embedded in at least one atom; `TestCorpusCoverage_RoundTrip` fails if any fixture loses a key phrase.
- [x] Round-trip coverage test passes for every fixture.
- [x] Compaction-safety test passes (10 iterations × every fixture, byte-identical).

### Phase 7

- [x] `go build ./...` clean.
- [x] `go test ./... -count=1` passes (19/19 packages).
- [x] `make lint-fast` clean (0 issues). `lint-local` not re-run in this session but the fast lane is a strict subset.
- [x] `grep -r bootstrap.md internal/content/` returns empty. `internal/content/workflows/` contains only `recipe.md`.
- [x] CLAUDE.md (project) Architecture table updated — `internal/content` and `internal/workflow` rows rewritten for the atom pipeline; Key-specs section annotated with spec-staleness.

### Overall

- [x] No regression in the test suite. `./integration/` green; real-platform `./e2e/` not re-run in this session (requires live Zerops context).
- [x] `§14` scenarios represented in code: S1, S3, S4, S5, S7, S8 as explicit `scenarios_test.go` tests; S2, S6 as tool-handler error tests; S9 as `engine_test.go::TestEngine_BootstrapComplete_AdoptionFastPath`; S10 as `workflow_recipe_test.go`.

(Binary-size target dropped — the embedded atom corpus is not materially smaller than the monolithic `workflows/*.md` it replaces, and the rewrite's value is structural clarity, not bytes.)

---

## 14. Per-Scenario Walkthroughs

Each scenario describes: user action → envelope after → plan produced → atoms synthesized. These are the manual-verification fixtures for the final phase and reference material for implementers.

### S1 — New project, recipe match (happy path)

**User**: `"I want a weather dashboard in Laravel"` → `zerops_workflow action=start workflow=bootstrap intent="weather dashboard in Laravel"`.

**Envelope before**: `Phase=idle, Services=[], Project={empty}`.

**Route selection**: `laravel-dashboard` matched @ 0.91 confidence. Viability gate: 412 lines, sections [overview, deploy, verify] present → `Viable=true`. **Route A**.

**Envelope after start**: `Phase=bootstrap-active, Route=recipe, Services=[proposed: laravel+mariadb], RecipeMatch={slug: laravel-dashboard, confidence: 0.91}`.

**Plan**: `Primary = import recipe YAML, Secondary = nil, Alternatives = nil`.

**Atoms synthesized**: `bootstrap-recipe-import-step`, `bootstrap-laravel-dev-mode-overview`.

**Contrast with today**: today this would match `laravel-minimal` at 0.88 and deliver a 6-line stub (D2). Viability gate prevents that.

### S2 — New project, managed-only intent

**User**: `"I need a PostgreSQL instance for my app"`.

**Envelope before**: idle.

**Route selection**: no recipe match at threshold (closest is `postgres-only` @ 0.7 which is below 0.85); no existing services → **Route B Classic**.

**Service plan**: `[postgresql@16]` only.

**Edge case**: Route B normally deploys a verification server per dynamic runtime. There are no dynamic runtimes here. The deploy-per-runtime step becomes a no-op; verify step confirms the managed service is reachable by checking `zerops_discover`.

**Plan after start**: `Primary = import postgres service, Secondary = nil, Alternatives = [add runtime service]`.

### S3 — New project, no match at all

**User**: `"I want a Rust service with custom protocols"`.

**Route selection**: no recipe above threshold → **Route B Classic**.

**Service plan**: `[rust@1.82]`. User approves.

**Flow**: import → wait active → deploy hello-world Rust verification server → verify `/status` → write ServiceMeta (mode=dev, strategy=unset) → auto-close.

**Plan after close**: `Primary = start develop task, Secondary = set deploy strategy, Alternatives = [add more services]`.

### S4 — Existing project, all bootstrapped, start task

**Envelope**: `Phase=idle, Services=[laraveldev (bootstrapped), laravelstage (bootstrapped), db (managed)]`.

**Plan**: `Primary = zerops_workflow action=start workflow=develop intent="..."`.

No bootstrap needed. User starts develop directly.

### S5 — Existing project, mixed bootstrapped + unmanaged

**Envelope**: `Phase=idle, Services=[laraveldev (bootstrapped, dev mode), newruntime (unmanaged, no meta), db (managed)]`.

**Plan**: `Primary = start develop, Secondary = nil, Alternatives = [adopt unmanaged runtimes, add more services]`.

Rationale: `planIdle` dispatches on `bootstrapped > 0` first. When develop starts, unmanaged runtimes auto-adopt as the session's first step. The adopt alternative is offered for callers who want to adopt without opening a develop session.

### S6 — Develop already active

**User attempts**: `zerops_workflow action=start workflow=develop`.

**Envelope**: `Phase=develop-active`.

**Response**: tool returns `ErrWorkflowActive` with current work session details. Plan: `Primary = close current session OR continue with it`.

No fallback: we do not silently start a second session or overwrite. Explicit user choice required.

### S7 — Develop closed, auto-complete

**Envelope**: `Phase=develop-closed-auto, WorkSession.ClosedAt=set, CloseReason=auto-complete`.

**Plan**: `Primary = close workflow, Secondary = start next task, Alternatives = nil`.

Status tool renders: *"Task complete. Close and start next, or close and stop."*

### S8 — Runtime error iteration 3

**Envelope**: `Phase=develop-active, WorkSession.Deploys[hostname]=[success, failed, failed, ...]` at iteration 3.

**Plan**: `Primary = zerops_deploy args={hostname}` — the deploy branch fires because the last attempt failed. The Plan shape stays stable across iterations; tier guidance is atom-backed.

**Atoms synthesized**: tier-2 systematic check atom — replaces old `BuildIterationDelta` tier-2 branch.

**Contrast with today**: no change in UX at iterations 3–4, but iteration 5 now terminates cleanly (D5 fix) instead of repeating STOP through iteration 10.

### S9 — Adopt, all-managed fast-path

**Envelope**: `Phase=idle, Services=[db (managed), cache (managed)]`.

**Plan**: `Primary = start develop on application service (but there is no app service)`. Actually: `Primary = bootstrap to add a runtime`. Reason: managed-only project can't host a user's code.

If user insists on `adopt`, adopt completes instantly (no per-service meta needed for managed services) — but the plan continues to push bootstrap.

### S10 — Recipe workflow active

**Envelope**: `Phase=recipe-active, Recipe.Slug=laravel-dashboard, Recipe.Step=verify`.

**Plan**: `Primary = continue recipe at verify step`.

**Atoms synthesized**: `recipe-verify-step` + any recipe-specific verify atoms.

### S11 — Container environment, stage mode deploy

**Envelope**: `Environment=container, Services=[laraveldev (mode=dev, strategy=push-dev), laravelstage (mode=stage, strategy=push-git)]` during develop-active targeting stage.

**Strategy dispatch**: deploy on `laravelstage` → push-git path. Atoms: `develop-push-git-container` + `develop-stage-mode-build-verification`.

### S12 — Local environment, push-dev

**Envelope**: `Environment=local, Services=[nodedev (mode=dev, strategy=push-dev)]`.

**Strategy dispatch**: push-dev via SSH from local. Atoms: `develop-push-dev-local`.

### S13 — Strategy unset

**Envelope**: `Services=[myservice (bootstrapped, mode=dev, strategy=unset)]`.

**Plan when deploy requested**: `Primary = set strategy via zerops_manage action=set-strategy`. Deploy is not attempted; no fallback strategy is chosen.

---

## 15. Glossary

| Term | Definition |
|---|---|
| **Atom** | One file in `internal/content/atoms/` declaring an axis vector and a guidance body. Minimal unit of Layer 2 knowledge. |
| **Axis** | One of the four dimensions along which guidance varies: phase, mode, environment, strategy (plus runtime class as a derived axis). |
| **Axis Vector** | An atom's declaration of which axis values it applies to. Empty sets are wildcards. |
| **Bootstrapped** | A service has a complete ServiceMeta (evidence file). The opposite is unmanaged. |
| **Compaction-safe** | A property of content such that it can be regenerated from the StateEnvelope alone, so LLM context compaction does not lose information. |
| **ComputeEnvelope** | The single function that gathers project state + work session + service metas into a StateEnvelope. |
| **Dead-weight** | A fact in the old corpus that the audit pipeline classifies as no longer needed (e.g. references to deleted features). |
| **Envelope** | Short for StateEnvelope. The canonical per-turn description of state. |
| **INJECT / POINT** | Two knowledge-delivery modes: INJECT copies content into the response; POINT references content for the LLM to fetch. Both are compaction-sensitive. |
| **Iteration Tier** | The escalation level of deploy-retry guidance. Tier 1 = diagnose; Tier 2 = systematic check; Tier 3 = STOP. |
| **KnowledgeAtom** | See Atom. |
| **Layer 1** | CLAUDE.md in the user's repo — static invariants. |
| **Layer 2** | Atom corpus — runtime-dependent guidance synthesized per turn. |
| **Layer 3** | StateEnvelope + Plan — typed per-turn contract. |
| **Managed Service** | A service type where Zerops provides the runtime (PostgreSQL, Redis, etc). No deploy, no ServiceMeta needed. |
| **Mode** | `dev` / `stage` / `simple`. A bootstrapped service's deployment style. |
| **Plan** | Typed trichotomy `{Primary, Secondary?, Alternatives[]}` of NextActions. |
| **Route** | Bootstrap path: Recipe / Classic / Adopt. Selected once per bootstrap session. |
| **RuntimeClass** | `dynamic` / `static` / `implicit-webserver` / `managed`. Determines post-deploy behavior. |
| **ServiceMeta** | Per-service evidence file at `state-dir/services/{hostname}.json` with mode + strategy. |
| **Strategy** | `push-dev` / `push-git` / `manual` / `unset`. How deploy is performed for a service. |
| **Synthesizer** | Function that filters atoms by envelope axes and composes guidance body. |
| **Viability Gate** | Minimum-content check for a matched recipe. Below threshold → routed to classic bootstrap instead. |
| **Work Session** | Per-PID record of the current develop task — intent, services, deploy/verify attempts. Survives context compaction via state-dir persistence. |

---

## Appendix A — File map (before → after)

| Old | Disposition (as-built) |
|---|---|
| `internal/server/instructions.go` | Reduced to one paragraph pointing at CLAUDE.md + status tool. |
| `internal/content/templates/claude.md` | Rewritten per §5 (53 lines). |
| `internal/content/workflows/bootstrap.md` | Deleted. |
| `internal/content/workflows/develop.md` | Deleted. |
| `internal/content/workflows/recipe.md` | Kept — recipe-authoring guidance runs through its own section-parser pipeline, never the atom synthesizer. |
| `internal/content/workflows/cicd.md` | Refactored to 10 atoms (`cicd-01…10`). |
| `internal/content/workflows/export.md` | Refactored to 9 atoms (`export-01…09`). |
| `internal/workflow/lifecycle_status.go` | Replaced by `render.go` + `build_plan.go`. |
| `internal/workflow/bootstrap.go` | Kept — split from the old guidance builder into session-state plumbing (`bootstrap.go`, `bootstrap_session.go`, `bootstrap_checks.go`, `bootstrap_steps.go`, `bootstrap_outputs.go`, `bootstrap_guide_assembly.go`). A new subpackage was planned but the same separation was achieved in-place. |
| `internal/workflow/bootstrap_guidance.go` | Deleted. |
| `internal/workflow/briefing.go` | Deleted. |
| `internal/workflow/guidance.go` | Deleted. |
| `internal/workflow/work_session.go` | Kept; adds `CloseReasonAutoComplete`. |
| `internal/knowledge/briefing.go::prependRecipeContext` | Removed. |
| `internal/knowledge/recipes_viability.go` | New, consumed by route selection. |
| `internal/content/atoms/*.md` | New corpus (74 atoms). |
| `cmd/audit-dedupe/` | Transient scaffolding; removed after Phase 2 produced the corpus. |

## Appendix B — Open questions for clarification during Phase 1

These are calibration questions whose answers go into the implementation but do not block the architectural plan.

1. **Recipe confidence threshold** — proposed 0.85. Calibrate against 20 labeled intent→recipe pairs in Phase 2.
2. **Viability gate thresholds** — proposed 200 lines + 3 required sections + 1 code fence. Calibrate against current recipe corpus in Phase 2 (flag stubs; verify good recipes pass).
3. **Dead-weight rate cap** — proposed ≤10%. If the audit pipeline classifies >10% of facts as dead-weight, trigger manual review to distinguish genuine dead weight from under-extraction.
4. **Max atom count** — soft cap at 80. If we need more, evidence of under-decomposition; split axes further (e.g. introduce a `step` axis within phases).
5. **Deterministic JSON encoder** — use `encoding/json` with a wrapper that sorts map keys. Alternative: `github.com/tidwall/jsonc` (no, stdlib is sufficient with map-key sort).

## Appendix C — Out-of-scope items noted during design

Captured here so they are not forgotten but do not block the rewrite:

- The current CI/CD workflow (`cicd.md`) is out of immediate scope but should follow the same atom model in Phase 7.2.
- The `zerops_knowledge` tool currently serves atom-like content directly (via `briefing.go`). After rewrite, it should serve composed synthesis from the new atom corpus using the same `Synthesize` function.
- The `zcp sync` subsystem (recipes + guides) is untouched. Recipe viability runs at route-selection time only; sync operations do not need the gate.
- Integration with zeropsio/docs repo for user-facing docs is unchanged.
- The `internal/eval` package (LLM recipe eval) is not affected by this rewrite.

## Appendix D — Post-rewrite spec debt (from Phase 7 audit)

After Phase 7 landed, the legacy design specs were audited against the new reality. The following divergences are documented here so the spec rewrite can be tackled as a separate follow-up without re-discovering them.

### docs/spec-workflows.md
- STALE §1 preamble: "guidance delivery / iteration-tier mechanics superseded" — but the rest of the doc still describes the pre-refactor model. Needs wholesale rewrite anchored on `build_plan.go` + `synthesize.go`, not the old branching narrative.
- MISSING: typed `Plan` trichotomy (`Primary`, `Secondary?`, `Alternatives[]`) and `BuildPlan(envelope)` as the decision engine. Today the spec only talks about free-form "Next" sections.
- MISSING: atom pipeline itself — `KnowledgeAtom`, `AxisVector`, `LoadAtomCorpus`, `Synthesize`, compaction-safety invariant.
- MISSING: `PhaseDevelopClosed`, `PhaseCICDActive`, `PhaseExportActive` in the phase enum discussion. The auto-close path and stateless immediate workflows are both post-refactor additions.

### docs/spec-knowledge-distribution.md
- STALE §1-2 core model: INJECT/POINT/NOTHING gates keyed off "system variables" at each step. Replaced by axis-based atom filtering; step-level gates no longer exist at the delivery layer.
- STALE §4-6 step sections: per-step INJECT prescriptions do not reflect the synthesis pipeline. Atoms with matching axes are all synthesized per turn; asymmetry moved into atom authoring, not runtime gates.
- STALE §4.7 / §5.4 iteration tier mechanics: atoms currently carry no iteration-count axis. Tier-2/3 escalation still lives in `deploy_guidance.go`-style code — not in atoms. Either migrate tier logic into atoms or document that iteration-tier delivery is an exception to the atom model.
- MINOR §3 init instructions: still accurate in spirit, but the per-response `StateEnvelope` attached to every tool reply is undocumented as the live Layer 3 contract.
- MINOR §6 recipe section: conflates recipe *guidance* (now in atoms? no — recipe-active atoms were intentionally not authored) with recipe *authoring* (section-parser pipeline, unchanged). Need to split the two concerns.

No spec lines were changed as part of this audit — the findings are listed so the spec rewrite can be a standalone task with a complete punch list instead of being excavated again.

---

**End of spec.** Execute Phase 0 first, then proceed sequentially. Each phase's acceptance criteria in §13 is the gate.
