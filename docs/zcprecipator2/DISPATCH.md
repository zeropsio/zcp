# DISPATCH.md — dispatcher-facing composition guide

**Scope**: how the server composes sub-agent dispatch briefs from the atom tree at `internal/content/workflows/recipe/`. This document is **never transmitted** to sub-agents (per [principles.md §P2](03-architecture/principles.md)). It lives here so future maintainers understand the composition surfaces without reading the Go source. If you are a sub-agent reading this file: stop. This content is not for you.

**Audience**: humans maintaining the stitcher + atom manifest; reviewers evaluating composed briefs against step-4 goldens; future agent instances picking up the zcprecipator2 rollout.

---

## 1. The composition surface

The server owns five dispatch composition functions, all in [`internal/workflow/atom_stitcher.go`](../../internal/workflow/atom_stitcher.go). Each consumes a `*RecipePlan` (plus role-specific pointer inputs) and returns a fully-composed `string` that the main agent transmits verbatim to the named sub-agent:

| Function | Sub-agent role | Substep | Showcase-only? |
|---|---|---|---|
| `BuildScaffoldDispatchBrief(plan, role)` | scaffold (one per codebase; role ∈ {api, app, worker}) | `generate.scaffold` | both tiers |
| `BuildFeatureDispatchBrief(plan)` | feature | `deploy.subagent` | showcase only |
| `BuildWriterDispatchBrief(plan, factsLogPath)` | writer | `deploy.readmes` | showcase only |
| `BuildCodeReviewDispatchBrief(plan, manifestPath)` | code-review | `close.code-review` | both tiers (showcase gated; minimal discretionary) |
| `BuildEditorialReviewDispatchBrief(plan, factsLogPath, manifestPath)` | editorial-review | `close.editorial-review` | showcase only (minimal deferred) |

Each function stitches a fixed sequence of atoms from `briefs/<role>/` and optionally interpolates plan-derived content (SymbolContract JSON) or runtime paths (facts log, manifest). Output is byte-stable for a given plan — the same plan always produces the same brief. This matters: P3 requires parallel scaffold dispatches to see the byte-identical SymbolContract fragment.

---

## 2. Stitching recipe

Every dispatch brief follows the same shape:

```
<mandatory-core atom>      ← role framing + forbidden-tool guardrails
<role-specific atoms>      ← the bulk of the brief; task + taxonomy + reporting
<principles pointer-includes>  ← platform-principles/ atoms inlined wholesale
<interpolated pointer inputs>  ← facts log path, manifest path, SymbolContract JSON
```

The concrete atom sequence for each role is declared as a `[]string` inside its build function. Adding or reordering atoms in that list is how you refine a brief — but beware: the step-4 golden files in [`04-verification/brief-*-composed.md`](04-verification/) were authored against the current sequence, and a reordering requires a matching golden refresh.

### Pointer-include vs inline

Principles atoms are **pointer-included** — inlined by reference, not paraphrased. Example: `BuildEditorialReviewDispatchBrief` inlines `principles/where-commands-run.md` + `principles/file-op-sequencing.md` + `principles/tool-use-policy.md` after the role-specific body. If you want to add a new principle to a brief, add the atom ID to that function's `editorialReviewPrinciples()` (or the equivalent list helper) — don't paraphrase the content inside the brief function.

### Interpolated inputs

Three pointer-shape inputs can be interpolated at stitch time:

1. **`{factsLogPath}`** — absolute path the sub-agent reads when it needs the prior-fact accumulator (`/tmp/zcp-facts-<sessionID>.jsonl`). Writer + editorial-review briefs use this; scaffold + feature briefs do not (per P7 porter-premise for editorial; per lane-filtered `BuildPriorDiscoveriesBlockForLane` for writer).
2. **`{manifestPath}`** — absolute path to `ZCP_CONTENT_MANIFEST.json`. Code-review + editorial-review briefs use this; scaffold + feature + writer do not (writer WRITES the manifest; others read it).
3. **`{SymbolContract | toJSON}`** — the plan's SymbolContract serialized to canonical JSON. Scaffold briefs interpolate it directly after the symbol-contract-consumption atom; feature brief interpolates it similarly. Writer + code-review + editorial-review do not — they read downstream of scaffold and don't need the contract.

---

## 3. Multi-codebase branching

Scaffold + feature briefs differ across (tier × codebase-count × worker-topology). The branching lives inside the atom content, not inside the stitcher:

- **`briefs/scaffold/mandatory-core.md`** names only "the codebase for which you are dispatched" — the role parameter on the build function is how the stitcher tells the sub-agent which codebase it owns. A single invocation of `BuildScaffoldDispatchBrief(plan, "api")` produces the brief for apidev; a second invocation with `role="app"` produces the brief for appdev; parallel dispatches see byte-identical SymbolContract fragments but different role contexts.
- **Shared-codebase workers** (worker target with `SharesCodebaseWith != ""`) do NOT get their own scaffold dispatch — the host target's dispatch covers them via the SymbolContract's NATS subjects/queues + platform-principles atoms. Separate-codebase workers (`SharesCodebaseWith == ""`) get their own `role="worker"` dispatch.
- **Minimal tier dual-runtime** (frontend + api, no worker) dispatches two scaffold briefs: `role="app"` for the frontend, `role="api"` for the api. Tier branching inside `briefs/scaffold/*` atoms handles the reduced scope (no NATS subjects, no shared-codebase worker, no managed-service bootstrap beyond the single DB).

---

## 4. Why certain tokens are forbidden in transmitted briefs

Every atom under `internal/content/workflows/recipe/` is subject to the build-time lints in [`calibration-bars.md §9 B-1..B-8`](runs/v35/calibration-bars.md). Those lints encode the following invariants — if you edit an atom, make sure your edit still passes:

### B-1: no version anchors

**Forbidden**: `v[0-9]+(\.[0-9]+)*` + `v8\.[0-9]+` tokens.

**Why**: the atom content is sub-agent-facing prose. Internal session version numbers (v8.96, v34, v8.104 Fix E) are dispatcher-side history — they name regressions we caught and closed. A sub-agent reading "v34 regressed DB_PASS" has no context for the reference and no use for it. The lint is a ratchet: any future atom edit that leaks a version anchor fails CI.

### B-2: no dispatcher vocabulary

**Forbidden** (inside `briefs/`): `compress`, `verbatim`, `include as-is`, `main agent`, `dispatcher`.

**Why**: dispatcher vocabulary names the server's composition surface. A sub-agent reading "the main agent compresses this before transmission" has received meta-information about its caller — a leakage that breaks the porter-premise for editorial-review and introduces hallucination vectors for scaffold + feature. Briefs are addressed to the sub-agent alone; the sub-agent should read them as direct instructions, not as a transcript of how the main agent built them.

### B-3: no internal check names

**Forbidden** (inside `briefs/`): `writer_manifest_`, `_env_self_shadow`, `_content_reality`, `_causal_anchor`.

**Why**: check names are server-side gate identifiers. Naming them in a brief teaches the sub-agent to game the specific check token rather than satisfy the underlying invariant. Atoms describe the invariant ("every gotcha must cite a real platform behavior or failure mode") — the check name that enforces it is a server-side concern the sub-agent doesn't need.

### B-4: no Go source paths

**Forbidden** (inside `briefs/`): `internal/.*\.go`.

**Why**: Go source paths are server implementation. A brief that names `internal/tools/workflow_checks_content_manifest.go` teaches the sub-agent the check surface by file path, which both leaks implementation and locks atom content to a specific refactor-era path.

### B-5: 300-line cap per atom

**Enforced** over the full tree (`find internal/content/workflows/recipe/ -name '*.md' -exec wc -l`).

**Why**: per [P6 §Enforcement](03-architecture/principles.md), atoms are composable leaf content. A 400-line atom is a monolith in disguise — it probably belongs split into 2-3 cohesive leaves. The cap is a forcing function.

### B-7: orphan-prohibition lint

**Enforced**: any atom containing `do not`, `avoid`, `never`, or `MUST NOT` must also contain a positive-form statement in the same atom.

**Why**: per [P8 positive allow-list](03-architecture/principles.md), prohibition without a positive alternative leaves the sub-agent without an action. "Never bind to 127.0.0.1" without "bind to 0.0.0.0" gives the sub-agent half a contract. The lint's heuristic (positive-form phrase in the ±10 surrounding lines) catches orphan prohibitions without requiring perfect structural matching.

### H-4: step-entry atoms use positive P4 form

**Forbidden** (inside `phases/*/entry.md`): `your tasks for this phase are`.

**Why**: per [P4 server state = plan](03-architecture/principles.md), entry atoms name what the current state requires, not what the agent should do next. "Your tasks are..." frames the agent as executing a dispatcher's plan; the positive form frames the current state ("state X requires condition Y") so the agent reads the invariant directly.

---

## 5. Where to edit vs where to look

| Change | Where |
|---|---|
| Add a new scaffold sub-agent instruction | `briefs/scaffold/<new-atom>.md` + atom_manifest entry + `scaffoldBriefAtomIDs()` list |
| Add a new principle transmitted across roles | `principles/<new-atom>.md` + atom_manifest entry + relevant `<role>Principles()` helper list |
| Change the SymbolContract JSON shape | `internal/workflow/symbol_contract.go` (type + derivation) + all consumer atoms (they currently name the fields structurally) |
| Change a brief's atom sequence | the `[]string` inside `Build<Role>DispatchBrief` + golden refresh at `04-verification/brief-<role>-composed.md` |
| Add a new sub-agent role | new `briefs/<role>/` directory + 6-12 atoms + new `Build<Role>DispatchBrief` function + new substep registration + atom_manifest + (optionally) new dispatch-runnable checks per §16a |

---

## 6. Golden-diff testing

Each role has a step-4 verification golden at [`04-verification/brief-<role>-<tier>-composed.md`](04-verification/). These were authored during the research phase as ideal-composed-output references; they are synthetic (not byte-identical to stitcher output in every case) and serve as cold-read review artifacts for C-7.5 and beyond.

C-14's `zcp dry-run recipe` harness (once landed) exercises every `Build<Role>DispatchBrief` across the (tier × codebase-count) matrix and emits diff reports against these goldens. A composition refactor that breaks a golden is a signal — either the refactor is wrong OR the golden needs an update to reflect the new intent. The harness reports the diff; the human decides which way to reconcile.

---

## 7. Operational boundary

Dispatchers (server-side Go code) see this document. Sub-agents (Claude Code subagents dispatched via the Agent tool) see the composed briefs. The transmitted-brief surface is described in [`atomic-layout.md §1–§6`](03-architecture/atomic-layout.md); this document is the dispatcher-side complement.

If a future change adds a new dispatch surface (e.g. a hypothetical `briefs/<new-role>/`), update both documents together: atomic-layout for the atom shape, DISPATCH.md for the composition semantics. The lint tree in C-13 enforces atom-side invariants; golden-diff testing in C-14 enforces composition semantics.
