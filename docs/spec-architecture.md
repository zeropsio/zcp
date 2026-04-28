# spec-architecture — ZCP 4-layer architecture

ZCP is a single Go binary (MCP server + CLI). Internally it is organized
into four layers plus cross-cutting packages. This spec is the reference
for code reviewers and contributors deciding where new code belongs.

The dependency rule is pinned by:
- `internal/architecture_test.go` — fails `go test ./...` on any
  forbidden import.
- `.golangci.yml::depguard` — surfaces violations during `make lint-local`.

CLAUDE.md `## Architecture` carries the high-level diagram. This file
fleshes it out.

---

## The four layers

### Layer 1 — Raw platform API

**Package**: `internal/platform/`

The Zerops REST API client and the data shapes the API returns
(`ServiceStack`, `EnvVar`, `Process`, `Project`, `User`, etc.). No
ZCP-specific concepts live here; the package would compile against any
caller that speaks Zerops.

**Allowed imports**: stdlib, third-party (`go-mod-vendor`-tracked).

**Forbidden imports**: any other `internal/` package.

**Examples**:
- `platform.Client` — HTTP client + auth.
- `platform.ServiceStack` — REST shape, NOT enriched with ZCP semantics.
- `platform.ErrAPIError` / `ErrNetworkError` — error categorization at
  the wire level.

### Layer 2 — ZCP topology vocabulary

**Package**: `internal/topology/`

The shared **ZCP-specific** vocabulary that names concepts the platform
itself does not name. Zerops API knows about service-stack-type-name
strings; ZCP organizes them into `Mode` (dev/standard/stage/simple),
`RuntimeClass` (dynamic/static/implicit-web/managed/unknown), etc. This
classification is a ZCP organizing principle for the LLM and the user,
not a Zerops API concept.

**Allowed imports**: stdlib only.

**Forbidden imports**: any other `internal/` package.

**Symbols**:
- Types: `Mode`, `RuntimeClass`, `CloseDeployMode`, `GitPushState`,
  `BuildIntegration`, `FailureClass`.
- Constants: `ModeDev/Standard/Stage/Simple/LocalStage/LocalOnly`,
  `RuntimeDynamic/Static/ImplicitWeb/Managed/Unknown`,
  `CloseModeUnset/Auto/GitPush/Manual`,
  `GitPushUnconfigured/Configured/Broken/Unknown`,
  `BuildIntegrationNone/Webhook/Actions`.
- Predicates: `IsManagedService`, `ServiceSupportsMode`,
  `IsRuntimeType`, `IsUtilityType` (+ supporting maps).
- Aliases: `PlanModeStandard/Dev/Stage/Simple/LocalStage/LocalOnly`,
  `DeployRoleDev/Stage/Simple` (semantic synonyms for `Mode`
  constants used by the bootstrap planner; kept as aliases until call
  sites are migrated, after which aliases are deletable).

**Promotion rule**: any new type that ops AND workflow both need belongs
here — never in workflow/. Any new predicate over Zerops type strings
belongs here.

### Layer 3 — Orchestration + operations (peer layers)

**Packages**: `internal/workflow/`, `internal/ops/`

Two **peer** packages. They must NOT import each other; shared types go
through `topology/` (layer 2). Both depend on `platform/` (layer 1) and
`topology/` (layer 2).

#### `internal/workflow/` — orchestration

The MCP `zerops_workflow` engine: state machine, sessions (recipe,
bootstrap, develop work session), atom selection, briefing rendering,
route logic.

Holds **engine state**, not platform calls. When the engine needs a
platform operation, it calls `ops/`.

**Allowed imports**: stdlib, third-party, `platform/`, `topology/`,
cross-cutting packages where applicable (`knowledge/`, `content/`,
`runtime/`).

**Forbidden imports**: `ops/`, `tools/`, `recipe/`.

**Symbols that stay here** (engine concerns):
- `Phase` + lifecycle constants (`PhaseIdle`, `PhaseBootstrapActive`, …).
- `IdleScenario`, `BootstrapRoute`, `DeployState` — engine concepts.
- `StateEnvelope`, `ServiceSnapshot`, `ServiceMeta` — engine output
  structures (their `Mode` field is `topology.Mode`).
- `Engine`, sessions, atoms, briefing, route logic.

#### `internal/ops/` — discrete platform operations

A flat catalog of operations that touch the platform: `Deploy`,
`Discover`, `Subdomain`, `Verify`, `LookupService`, `ListProjectServices`,
log fetchers, etc. Each operation is a plain function or small struct
that takes `platform.Client` + inputs and returns typed results or
errors.

**Allowed imports**: stdlib, third-party, `platform/`, `topology/`.

**Forbidden imports**: `workflow/`, `tools/`, `recipe/`.

When `ops/` needs a topology predicate (e.g. `IsManagedService`), it
imports `topology/` — never `workflow/`. Reverse-importing `workflow/`
is the layering violation that motivated extracting `topology/`.

### Layer 4 — Entry points

**Packages**: `cmd/zcp/`, `internal/server/`, `internal/tools/`

CLI entrypoints, MCP server, and the per-tool handlers. This is where
incoming MCP JSON arrives as untyped strings and gets validated +
converted to `topology.Mode`, `topology.CloseDeployMode`, etc., before
being passed down. Inside layers 1–3, types are concrete; only the
boundary deals with strings.

**Allowed imports**: every layer below.

---

## Cross-cutting packages

These do not slot strictly into layers 1–4. They are peer-of-equal-level
within their domain and may be imported by layer 4 or by other
cross-cutting peers, but they observe the foundational rules
(`topology/` and `platform/` are below them).

| Package | Role |
|---|---|
| `internal/auth/` | Pre-engine startup auth flow. Talks directly to `platform/`. Documented exception: this runs before any Engine exists, so it cannot route through `ops/`. |
| `internal/runtime/` | Container/local detection (`runtime.Info`). Pure utility; no platform calls. |
| `internal/knowledge/` | Atom corpus loader + briefing renderer. Peer to workflow; called from workflow during briefing. |
| `internal/content/` | Atom storage backend (file system). Peer to knowledge. |
| `internal/recipe/` | v3 recipe engine. Peer to workflow, separate scope. Out of scope for this spec's enforcement. |
| `internal/eval/` | Test/dev tooling that drives ZCP from the outside. Peer to tools. May import `ops/` and `topology/`. |
| `internal/preprocess/`, `internal/schema/`, `internal/catalog/`, `internal/sync/`, `internal/init/`, `internal/update/` | Utility / cross-cutting. Each obeys "import only what you actually need from below." |
| `internal/service/` | Container exec wrappers (nginx/vscode). Name-collision-distinct from `topology/` — that is why the new package is `topology/`, not `service/`. |

---

## OK / not OK examples

### OK — ops uses topology predicate

```go
// internal/ops/discover.go
import (
    "github.com/zeropsio/zcp/internal/platform"
    "github.com/zeropsio/zcp/internal/topology"
)

func classifyService(s platform.ServiceStack) topology.RuntimeClass {
    if topology.IsManagedService(s.ServiceStackTypeVersionName) {
        return topology.RuntimeManaged
    }
    return topology.RuntimeDynamic
}
```

### NOT OK — ops imports workflow

```go
// internal/ops/discover.go
import "github.com/zeropsio/zcp/internal/workflow"  // FORBIDDEN

func reportRecipe(plan *workflow.RecipePlan) error { ... }
```

`architecture_test.go` fails. `make lint-local` fails on depguard.
Anything genuinely shared between ops and workflow belongs in
`topology/` (ZCP vocabulary) or `platform/` (wire shapes).

### OK — tool boundary parses string → topology type once

```go
// internal/tools/workflow_close_mode.go
func handle(input Input) error {
    mode, err := topology.ParseMode(input.Mode)  // boundary parse
    if err != nil { return err }
    // mode is topology.Mode from here on; no more string casts.
    return engine.UpdateCloseMode(ctx, mode, ...)
}
```

### NOT OK — tool casts at the comparison site

```go
if string(topology.BuildIntegrationWebhook) == input.Integration {  // boundary leak
    ...
}
```

Cast-for-comparison is the symptom of unparsed input. Parse once at the
entry, then compare typed values.

### NOT OK — workflow imports ops

```go
// internal/workflow/develop_step.go
import "github.com/zeropsio/zcp/internal/ops"   // FORBIDDEN
```

Workflow describes WHAT, ops execute it. The caller (a tool handler in
layer 4) runs the engine step, observes that the engine returned an
action ("deploy this service", "verify subdomain"), and then invokes
the matching `ops/` operation. Workflow itself never reaches sideways
into ops.

If a true shared piece between workflow and ops surfaces, extract it
into `topology/` (when it's a ZCP-vocabulary type), into `platform/`
(when it's a wire shape), or into a new cross-cutting peer package.
Never an upward or sideways import.

### OK — auth goes direct to platform

```go
// internal/auth/auth.go
import "github.com/zeropsio/zcp/internal/platform"

func Login(...) error {
    return platform.NewClient(...).GetUserInfo(ctx)
}
```

`auth/` runs before the engine exists; it is the bootstrap loop. This is
the documented exception, not a license to add more.

---

## How to add a new shared type

1. **Is it a Zerops API shape?** → goes into `internal/platform/`.
2. **Is it a ZCP organizing concept (mode, classification, predicate)?**
   → goes into `internal/topology/`.
3. **Is it engine state (phase, session, briefing-side struct)?** →
   `internal/workflow/`.
4. **Is it a discrete platform operation result?** → `internal/ops/`.

If you find yourself reaching for `import "github.com/zeropsio/zcp/internal/workflow"`
from `ops/` because of a type, the type is in the wrong package. Move
it to `topology/`.

---

## Why this layering matters

ZCP is increasingly authored and audited by LLMs. A four-layer model with
a single shared-vocabulary package gives an LLM a stable mental map:

- New shared type? → `topology/` first.
- Need to call the platform from workflow? → no, that is the caller's
  job; the engine surfaces an action.
- Adding a predicate over Zerops type strings? → `topology/predicates.go`,
  not yet another helper file.

The pinning (depguard + architecture_test) means these decisions get
flagged automatically when an LLM (or human) drifts. Without the pin,
the layering decays back to the god-package state we extracted out of.
