# Authoring boundary — env-gated internal tooling with enforced borders (2026-06-11)

**Status: SHIPPED 2026-06-11** — P1a/P1b/P2/P3/P5 implemented (commits
`refactor(authoring): move internal/recipe…` → `docs(authoring): spec…`).
Living truth: `docs/spec-authoring-boundary.md`. Deviations from this plan:
- P0 resolved by Karel's directive (names left to implementer):
  `internal/authoring/` + `ZCP_AUTHORING` + recipe-model self-registration.
- P4 is N/A as written — Karel guaranteed PR #5 will NOT merge; the spec §1
  documents `authoring/port/` as the port flow's future home + handler
  rules, so the one-way door is closed without touching the PR.
- `CheckAtomTemplateVarsBound` extraction NOT done — its subject
  (recipe-briefs template vars) is authoring content, so the check stays in
  `authoring/analyze` and `tools/lint/atom_template_vars` (outside the
  enforced surface) imports it; the plan's §P1b assumption that it guards
  the core atom corpus was wrong.
- `TestNoCrossCallHandlerState` scopes to `authoring/{publish,analyze}`
  (not the whole subtree): `authoring/recipe`'s Store/embed/sync.Once vars
  are deliberate, documented exceptions.
- P6 backlogged: `plans/backlog/v2-recipe-remnants.md`.

Builds on the analysis in `plans/recipator-oss-extraction-2026-06-11.md` (option C chosen:
env-gated, same binary). This plan adds what option C alone doesn't give: a **mechanically
enforced boundary** so that (a) core refactors provably cannot break the recipator/OSS-port
code, (b) authoring changes provably cannot leak into the end-user product surface, and
(c) the "is this Aleš's? can I touch it?" question is answered by a path prefix, not by a
hand-maintained file list.

All facts below verified against main + `feat/oss-port-flow` (two agent sweeps, 2026-06-11).

---

## 1. Goal + non-goals

**Goal:** one subtree = the authoring domain; one env var = its activation; two compile-time
direction rules + an enumerated runtime-contract list = the entire coupling surface.
After this ships, "what relates to what" has exactly one answer: the boundary tests.

**Non-goals:**
- No separate repo, no second binary, no build tags (rejected in the analysis doc).
- No behavior change for end users beyond tool-list shrink + de-mentioned strings.
- No redesign of Aleš's engine internals — `internal/recipe` moves as-is (pure `git mv`).
- v2 recipe-workflow remnants retirement is NOT in scope (P6 backlog, separate decision).

## 2. Target layout

```
internal/authoring/              ← THE boundary. Everything under it = authoring domain.
  recipe/      ← git mv internal/recipe (package name stays `recipe`; Aleš's engine, untouched content)
  port/        ← PR #5: 13 port_*.go state machines (from package workflow)
               + 4 workflow_port*.go handlers (from package tools), self-registering MCP tool
  publish/     ← internal/sync/{export,publish_recipe,session_load,timeline_sanitizer}.go
               (recipe-repo lifecycle: create-repo / push-app / publish / export)
  analyze/     ← internal/analyze (zcprecipator run-analysis harness, per its own pkg charter)
```

Stays CORE: `internal/sync` (10 files: config, cache, http, github, result, transform,
pull_recipes, pull_guides, push_recipes, push_guides — content sync used by CI/build/docs
maintenance), bootstrap `route=recipe` consumption (`internal/workflow/recipe_override.go`
+ `recipe_shape.go` — imports no recipe code today, pinned), the knowledge corpus
(`internal/knowledge` — shared read-only), v2 remnants (`internal/tools/workflow_recipe.go`,
`workflow_checks_recipe.go`, `internal/content/workflows/recipe*` — P6).

Dev binaries `cmd/zcp-recipe-sim`, `cmd/zcp-recipe-patch` stay in `cmd/` (entrypoints;
import-path update only).

## 3. The boundary law (two directions + enumerated exceptions)

**L1 — core never imports authoring.**
Deny `github.com/zeropsio/zcp/internal/authoring` everywhere under `internal/` EXCEPT
`internal/server` (composition root: constructs the store, registers gated tools, wires
the probe). `cmd/zcp` (CLI wiring for `sync recipe *` + `analyze *`) and `tools/lint/*`
(repo lint tooling, not product) sit outside `internal/` and outside the rule's scope —
documented, not accidental.

**L2 — authoring imports core only through an allowlist.**
Allowed: `internal/{topology, schema, knowledge, platform, runtime, workflow, sync}`
+ stdlib + mcp go-sdk + yaml.v3 + jsonschema-go + intra-authoring. Anything else fails.

**L3 — the workflow edge is identifier-pinned.**
`internal/workflow` is the most-refactored core package; authoring's use of it is pinned
to an explicit identifier allowlist (AST test): `WorkSession`, `NewWorkSession`,
`CurrentProcessStartTime`, `CloseReasonIterationCap`, `DetectEnvironment`, `Phase`,
`LoadSessionByID`, `ListSessions`, `RecipeState`, `CanonicalEnvFolders` (final list fixed
in P2 from actual post-move usage). Adding an identifier = deliberate contract change,
visible in diff of the allowlist.

**L4 — no in-process coupling outside the composition root.**
The ONLY runtime seams between core and authoring are the contracts in §4. New seams
require updating the contract spec + its pin — the boundary test failing is the prompt.

Enforcement is **dual** (per existing repo convention — deliberate redundancy so a
regression is caught even if one layer is disabled):
- `.golangci.yaml` depguard: new rule `core-not-authoring` (files `**/internal/**/*.go`
  with `!**/internal/authoring/**` + `!**/internal/server/**`, deny authoring) + new rule
  `authoring-allowlist` (files `**/internal/authoring/**/*.go`, `list-mode: strict`,
  allow list per L2). Existing 3 deny entries naming `internal/recipe` (ops-not-workflow,
  ops-checks-legacy, workflow-not-ops) retarget to `internal/authoring`.
- `internal/topology/architecture_test.go`: same `parser.ImportsOnly` walker —
  one new `layerRule` {rootDir: "" (= all internal/), excludeSubdir: ["authoring",
  "server"], deny: [".../internal/authoring"]} + one new inverted-allowlist test for the
  authoring direction + the L3 identifier pin (reuse the `scanForMethodCalls` engine from
  `architecture_call_discipline_test.go`; include a `_FiresOnFixture` scanner self-test,
  house style). Existing deny strings at architecture_test.go:55,67,77 retarget.
- `TestNoCrossCallHandlerState` roots extend from `{"../tools"}` to include
  `"../authoring"` (handlers must stay stateless wherever they live; may surface
  pre-existing recipe package-level vars — triage then: initialized vars are allowed).
- `TestNoStdoutOutsideJSONPath` already scans all of `internal/` — authoring covered
  automatically, no change.

## 4. Cross-boundary runtime contracts (the complete list)

| # | Contract | Direction | Mechanism | Pin |
|---|---|---|---|---|
| C1 | `RecipeSessionProbe` (3 methods, nil-tolerant) | core tools ← authoring store | in-process interface, satisfied by `*recipe.Store`, wired ONLY in server.go; nil when gate off ⇒ end-user-correct behavior (verified guard.go:44-52,103) | existing guard tests + new gate-off server test |
| C2 | `SetSchemaProvider` closure | authoring ← core schema cache | constructor injection in server.go, inside gate block | existing recipe tests |
| C3 | State namespaces | disk | authoring owns `.zcp/state/port/` + `~/recipes` (outputRoot, `ZCP_RECIPE_MOUNT_ROOT`); core owns `.zcp/state/work/` + the rest; neither reads the other's | new AST/grep pin in P2 (core must not reference `state/port`) |
| C4 | Knowledge corpus | shared read-only | both consume `internal/knowledge` embedded store | existing |
| C5 | workflow identifier allowlist (L3) | authoring → core | compile-time | new AST pin |
| C6 | core-sync exported surface | authoring/publish → core sync | `Config` (+ Push fields), `GH` + exported methods (incl. `ListDirectory`), `PushResult`/`Status` — all already exported | depguard allows sync; godoc note on GH |

Everything else that LOOKS like coupling is not: engine-version stamp (same binary, same
`server.Version`), agent-bridged port loop (the agent carries observations between core
tools and authoring tools — no process coupling), `instructions.go` recipe-session hint
(fires only when a session exists ⇒ only when gate on).

## 5. The env gate

`ZCP_AUTHORING=1` (exactly `"1"`; anything else = off; default off). One flag for the whole
domain — recipe authoring, OSS port, and future authoring tools share the audience.

`server.go::registerTools` (precedents: browser gate at :244, env read at :164):

```go
if os.Getenv("ZCP_AUTHORING") == "1" {
    mountRoot := ...                         // ZCP_RECIPE_MOUNT_ROOT handling moves inside
    recipeStore := recipe.NewStore(mountRoot, Version)
    recipeStore.SetSchemaProvider(...)
    recipe.Register(s.server, recipeStore)
    port.Register(s.server, schemaCache, projectID, stateDir, s.rtInfo)  // after P4
    probe = recipeStore                      // else probe stays nil
}
```

The 7 probe-threaded registrations (`RegisterRecordFact`, `RegisterWorkspaceManifest`,
`RegisterDeploySSH/Batch/Local`, `RegisterImport`, `RegisterMount`) keep their signatures;
they receive nil when off — already production-safe and semantically correct (no recipe
sessions can exist).

Operational path for Aleš: set `ZCP_AUTHORING=1` on his container (Zerops GUI env) or
`export` in shell — same binary, same auto-update, same `claude`/`codex` interactive flow.
MCP config templates need NO change (spawned `zcp serve` inherits the agent's env;
verified mcp-config.json carries no env block).

## 6. Phases

### P0 — decisions (Karel + Aleš, no code)
1. Subtree + env name: `internal/authoring` + `ZCP_AUTHORING` (alt: `recipator` branding).
2. Port tool name: proposal `zerops_oss_port` (avoid bare `zerops_port` ↔ network-port
   confusion). Aleš confirms.
3. Aleš agrees PR #5 retargets to the new layout (P4) instead of merging as-is.
4. Gate the CLI too? Proposal: NO — `zcp sync recipe *` / `zcp analyze *` stay ungated
   (operator CLI, not agent context cost; harmless without gh auth + `.sync.yaml`).

### P1 — mechanical moves (zero behavior change)
**P1a — recipe move:**
- `git mv internal/recipe internal/authoring/recipe` (package name stays `recipe`).
- Import-path updates: server.go, `internal/sync/export.go` (until P1b moves it),
  `internal/analyze/surface_validation.go` (until P1b), `cmd/zcp/analyze/recipe_run_v3.go`,
  `cmd/zcp-recipe-sim` (7 files), `cmd/zcp-recipe-patch`, `internal/sync` tests.
- Rewrite `internal/tools/record_fact_test.go` + `workspace_manifest_test.go` with local
  3-method fake probes (drop their recipe imports — tests must not cross the boundary
  either; the interface is trivial to fake).
- Retarget the 6 path strings: `.golangci.yaml`:110,124,137 +
  `architecture_test.go`:55,67,77 (`internal/recipe` → `internal/authoring`).
- Gate: full suite + `make lint-local` green; `cmd/zcp-recipe-sim` builds; recipe package
  tests green (Aleš's loop intact).

**P1b — sync split + analyze move:**
- Move `export.go`, `publish_recipe.go`, `session_load.go`, `timeline_sanitizer.go`
  (+ their tests) → `internal/authoring/publish` (these 4 are recipe-lifecycle-only;
  verified zero core references to anything in the group).
- Duplicate `today()`/`shortRand()` (6 lines) into publish — they stay in core sync too
  (push_guides/push_recipes use them); trivial-utility duplication beats API pollution.
- Delete orphans while touching: `sync.CheckGH` + `GH.SetSecret` (zero callers repo-wide).
- Move `internal/analyze` → `internal/authoring/analyze`. Resolve the one core consumer:
  `tools/lint/atom_template_vars` (Makefile B-22 gate) — relocate
  `CheckAtomTemplateVarsBound` + `DefaultAllowedAtomFields` into the lint tool with a
  local result type (the atom-corpus gate is core-content tooling that landed in analyze
  for convenience; resolve analyze-internal callers during implementation, expected none).
- Kill the hand-mirrored `CanonicalEnvFolders` copy in analyze — import the exported
  `workflow.CanonicalEnvFolders()` instead (allowed edge; one-owner win; delete the
  mirror comment in `recipe_templates.go`).
- Update `cmd/zcp/sync.go` (recipe subcommands call `authoring/publish`) +
  `cmd/zcp/analyze/*` imports.
- Gate: full suite + lint green; `zcp sync recipe export --dry-run` + `zcp analyze
  recipe-run-v3` smoke on a frozen run dir behave identically.

### P2 — enforcement (the law becomes executable)
- New depguard rules `core-not-authoring` + `authoring-allowlist` (§3).
- New architecture tests: core-deny rule, authoring inverted-allowlist, L3 workflow
  identifier pin (+ fixture self-tests), C3 state-namespace pin.
- Extend `TestNoCrossCallHandlerState` roots with `"../authoring"`.
- Gate: lint + tests green; PROVE each rule fires (temporarily add a violating import /
  identifier locally, observe failure, revert — note in commit message).

### P3 — the gate + end-user surface cleanup
- server.go gate block per §5 (store construction + both registrations inside).
- `server_test.go`: drop `zerops_recipe` from `expectedTools` (20 names, exact-count);
  add non-parallel `TestServer_AuthoringToolsRegistered` with
  `t.Setenv("ZCP_AUTHORING","1")` asserting 21 (22 after P4). (Test is already
  non-parallel — `t.Chdir`; `t.Setenv` precedent at server_test.go:549.)
- `annotations_test.go`: move `zerops_recipe` to a gated-exempt set following the
  `browserExempt` pattern + dedicated non-parallel annotations test with env set.
- De-mention sweep — unconditional agent-visible strings only:
  - `agents_shared.md`:8,9,37 — drop the zerops_recipe routing rows (auto-refresh
    migrates every user project at next server start; idempotent, existing mechanism).
  - Tool descriptions: `knowledge.go`:32-33,87; `import.go`:82; `record_fact.go`:67,71 —
    rewrite without the tool name (schema-visible to all users).
  - `atoms/idle-bootstrap-entry.md`:11 — remove the zerops_recipe sentence (end users
    never see the tool now; atom lint must stay green).
  - v2 `workflow=recipe` hard-block redirect (`workflow.go`:599-607) — genericize
    ("recipe authoring is maintainer tooling; enable ZCP_AUTHORING").
  - KEEP session-gated runtime strings (`guard.go`:58, `record_fact.go`:131,141,146) —
    they render only during an active recipe session, i.e. only for Aleš, and the
    guidance is correct there.
- Gate: full suite green both ways (`go test ./...` runs without env; the new env tests
  cover on); manual: `zcp serve` tool list = 20 without env / +authoring with env; one
  container flow-eval smoke (any bootstrap scenario) to verify end-user surface
  regressed nowhere.

### P4 — PR #5 retarget (Aleš's branch, coordinated)
- 13 `port_*.go` → `internal/authoring/port` (own package): duplicate `atomicWriteJSON`
  (28 stdlib-only lines — deliberate: keeps the L3 workflow contract minimal; generic
  utility, not a domain concept), keep using exported `WorkSession`/`NewWorkSession`/
  `CurrentProcessStartTime`/`CloseReasonIterationCap`.
- 4 handler files → same package; self-register `zerops_oss_port` (P0 name) via
  `port.Register` (mirrors `recipe.Register`), gated by the same env in server.go.
- Input struct: port-local (RecipeInput model). Delete the ~71 `Port*` lines +
  `PortRubricInput`/`PortHardenInput`/`PortGlueRepoInput` from `WorkflowInput`; delete
  the `routePortAction` hook + `workflowPort` const from `handleWorkflowAction`; delete
  `WorkflowPort` from port_recon.go. Duplicate `FlexBool` into authoring (self-contained
  142-line file) if the input keeps stringly-bool tolerance.
- Result/error shaping: adopt the recipe model (in-band JSON envelope, `okResult`/
  `errResult` style, handler never returns Go errors to the SDK). Explicitly do NOT
  duplicate `convertError`/`ErrorWire` — the credential-contract single-owner
  (`errwire.go::appendCredentialContract`) stays core-only; port handlers never produce
  credential-class platform errors (verified: capture uses only `NewPlatformError` +
  ErrNotImplemented/ErrSessionNotFound/ErrInvalidParameter). Recovery pointers become
  fields of the port envelope.
- Revert the 3 shared-file touches on the branch: `envelope.go` `PhasePortActive` const
  (becomes a port-local string — core never produces it; this also documents away the
  dead-enum/status-recovery review note: recovery is the port tool's own status action),
  `build_plan.go` + `render.go` case-list lines.
- Capture's `tools→recipe` + `tools→sync` imports become intra-authoring
  (`authoring/port` → `authoring/recipe` + `authoring/publish`) — the seam erosion the
  analysis flagged dissolves structurally.
- `internal/authoring/recipe` touches on the branch (GlueRepoURL, ModeMeasured,
  ManagedServiceModeForTier, clickhouse HA entries) — unchanged, now intra-boundary,
  Aleš self-coordinates.
- State stays `.zcp/state/port/{pid}.json`; `stateDir/port-recipes/` output unchanged.
- Gate: PR5's own test suite green in the new layout; boundary tests green; PR5's
  pending live-e2e items (harden behavior, publish OQ-1) unchanged — they were open
  before and stay tracked in PR5's spec.

### P5 — docs + ownership rewrite
- `docs/spec-authoring-boundary.md`: §3 law + §4 contracts C1-C6 + §5 gate semantics +
  the composition-root exception list (server.go, cmd/zcp, tools/lint).
- CLAUDE.md: replace the "Recipe generation = Aleš's scope" path list with the path rule
  (`internal/authoring/** = authoring domain, Aleš primary, flag+discuss protocol;
  v2 remnants enumerated as temporary exceptions pending P6`) + ONE new invariant bullet
  (boundary + gate + pins). Update the recipe-AUTHORING vs bootstrap-route=recipe
  disambiguation block to the new paths.
- Mark `plans/recipator-oss-extraction-2026-06-11.md` superseded-by this plan for the
  implementation part.
- flow-eval note: when the first authoring flow-eval scenario lands, extend the
  container env propagation grep (`flow-eval.sh`:120 `^ZCP_E2E_` → `^ZCP_(E2E_|AUTHORING)`);
  local mode inherits the operator shell. No scenario calls zerops_recipe today
  (verified: all 5 recipe-* scenarios are bootstrap-route consumers).

### P6 — backlog (separate decisions, NOT this plan)
- v2 recipe remnants retirement: `workflow_recipe.go`, `workflow_checks_recipe.go`,
  `internal/content/workflows/recipe*`, and `authoring/publish/session_load.go`'s v2
  `RecipeState` close-gate (v3 uses the refinement-closed marker — is the v2 gate dead?).
  → `plans/backlog/v2-recipe-remnants.md` after P5.
- Atom-template-vars gate conceptual home (if P1b extraction surfaces internal callers).
- CLI help visibility of `sync recipe` / `analyze` subcommands (cosmetic).

## 7. Backward compat (user-facing surfaces)

- **Tool list**: `zerops_recipe` leaves the default registration. Permission allowlists
  are wildcard `mcp__zerops__*` (verified, pinned by init_test) — no breakage. No
  integration/e2e test references the tool (verified).
- **AGENTS.md/CLAUDE.md in user projects**: de-mention rides the existing idempotent
  auto-refresh at server start — one-way, automatic, tested mechanism.
- **CLI**: `zcp sync ...` + `zcp analyze ...` byte-identical behavior (import paths only).
- **State on disk**: `~/recipes`, plan.json, EngineVersion semantics, `.zcp/state/*` —
  all unchanged (same binary, same version stream).
- **PR #5**: not yet merged ⇒ the Port* schema fields never ship to users; P4 happens
  pre-merge by design.
- **Aleš's dev loop**: recipe-sim/patch keep working (path updates only); engine-version
  "dev" gate-skip unchanged; `ZCP_RECIPE_MOUNT_ROOT` honored (moves inside gate block).

## 8. Risks

| Risk | Mitigation |
|---|---|
| PR #5 merges before P4 → Port* fields in published schema + tools→recipe import shipped | P0 item 3 is the sync point with Aleš; this plan's sequencing exists precisely to avoid it |
| Mechanical-move diff noise hides a real change | P1 commits are move-only (no logic edits except import paths + the two test-fake rewrites); reviewers diff with `--find-renames` |
| depguard strict allowlist fights new legit deps | The allowlist is in one place; adding an entry is a reviewed, deliberate act — that's the feature, not a bug |
| Probe-nil path regression for end users | Already production-safe today (nil-tolerant, verified); P3 adds the gate-off server test pinning the 20-tool surface |
| recipe handlers surface package-level state when TestNoCrossCallHandlerState scope extends | Initialized vars are allowed by the lint; zero-value findings get triaged in P2 (fix or document) |
| Aleš's in-flight work collides with the recipe move | P1a is a single `git mv` commit — coordinate timing with him in P0; rebase cost for his branches is import-path-only |

## 9. Effort

- P1a ~0.5 day, P1b ~1 day (mechanical, wide), P2 ~1 day, P3 ~1 day, P4 ~1-1.5 day
  (inside PR #5, with Aleš), P5 ~0.5 day. **Total ~5 days.**
- New logic is small (~600-800 LOC: gate + tests + port envelope); the bulk is moves.
