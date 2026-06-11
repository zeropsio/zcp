# Authoring Boundary — internal/authoring/

Status: SHIPPED 2026-06-11 (plan: `plans/authoring-boundary-2026-06-11.md`;
analysis: `plans/recipator-oss-extraction-2026-06-11.md`).

`internal/authoring/` is the **maintainer-only authoring domain**: tooling
that produces and publishes content FOR the Zerops platform (recipes for the
corpus) rather than operating a user's project. It ships inside the single
zcp binary but is invisible to end users — its MCP surface registers only
behind the `ZCP_AUTHORING` gate, and two compile-time direction laws make
the boundary mechanical rather than conventional.

The boundary answers one question with certainty: **what relates to what.**
A core refactor cannot silently break authoring (L1-L3 fail the build/test
run); an authoring change cannot silently leak into the end-user product
(gate + L1 + the de-mentioned surfaces). Ownership is a path prefix, not a
file list.

---

## 1. What lives inside

| Package | Contents | Origin |
|---|---|---|
| `authoring/recipe/` | zcprecipator v3 engine: `zerops_recipe` tool (self-registering), Store, plan/briefs/gates/emitters | moved from `internal/recipe` |
| `authoring/publish/` | recipe-repo lifecycle: `zcp sync recipe {create-repo,push-app,publish,export}` implementations + recipe-session close gate + TIMELINE sanitizer | split out of `internal/sync` |
| `authoring/analyze/` | zcprecipator run-analysis harness (`zcp analyze recipe-run*`, `generate-checklist`) + the B-22 recipe-briefs template-vars check | moved from `internal/analyze` |

Gate-registered but still core-coded (transitional): `zerops_guidance` —
v2 recipe-authoring topic guidance (`internal/tools/guidance.go`; its only
content source is the recipe workflow corpus and its only topic-ID producer
is the dispatch-blocked v2 recipe start). Registers inside the gate;
retires with the v2 remnants (`plans/backlog/v2-recipe-remnants.md`).

**Future home:** the OSS-port flow (port→debug→harden→capture; PR #5 was its
first draft and is NOT merged) lands as `authoring/port/` — its own package,
self-registering its own gated tool (NOT fields on `WorkflowInput`, NOT a
`workflow=` value), mirroring `recipe.Register`. Its handlers follow the
recipe model: own input struct, own in-band JSON result envelope, no imports
from `internal/tools` (the credential-contract owner
`errwire.go::appendCredentialContract` stays core-only — port never produces
credential-class platform errors). Its per-PID state stays in the
authoring-owned `.zcp/state/port/` namespace. Landing it extends the L2/L3
allowlists deliberately (visible diff + this spec).

**Deliberately core (NOT authoring):**
- `internal/sync` — content sync (pull/push/cache, GH plumbing, transforms):
  CI runs `zcp sync pull` before every build; docs maintenance is a repo
  concern, not recipe authoring.
- bootstrap `route=recipe` (recipe CONSUMPTION — `internal/workflow/
  recipe_override.go`, `recipe_shape.go`): provisions user projects from the
  corpus; imports nothing from authoring (pinned).
- `internal/knowledge` recipe corpus — shared read-only by both sides.
- v2 recipe remnants (`internal/tools/workflow_recipe.go`,
  `workflow_checks_recipe.go`, `internal/content/workflows/recipe*`) —
  dispatch-blocked legacy; retirement is a separate decision (backlog).

## 2. The laws

| # | Law | depguard rule | Architecture test |
|---|---|---|---|
| L1 | Core never imports authoring. Composition root: `internal/server` only. | `core-not-authoring` | `TestAuthoringBoundary_CoreDoesNotImportAuthoring` |
| L2 | Authoring imports core only through the allowlist: `topology`, `schema`, `knowledge`, `platform`, `workflow`, `sync` (+ stdlib, mcp go-sdk, yaml.v3). | `authoring-allowlist` (strict) | `TestAuthoringBoundary_AuthoringImportsAllowlistedOnly` |
| L3 | The authoring→workflow edge is identifier-pinned: `CanonicalEnvFolders`, `RecipeState`, `LoadSessionByID`, `ListSessions` (production files). | — | `TestAuthoringBoundary_WorkflowIdentifierAllowlist` |
| L4 | No in-process coupling outside §3 contracts. | (consequence of L1+L2) | contract pins per §3 |

Dual enforcement (depguard + AST test) is deliberate — same rationale as
`TestArchitectureLayering`. Scanner self-tests (`*_FiresOnFixture`) prove
the matchers fire. **Extending any allowlist is a deliberate contract
change: update the depguard rule, the test allowlist, and this spec in the
same commit.**

Outside the enforced surface, by design: `cmd/zcp` (CLI wiring for
`sync recipe *` + `analyze *` subcommands), `cmd/zcp-recipe-sim`,
`cmd/zcp-recipe-patch` (dev-only, never released), and `tools/lint/
atom_template_vars` (Makefile B-22 gate whose subject is authoring content).
All sit outside `internal/` and none ship MCP surface.

## 3. Cross-boundary runtime contracts

| # | Contract | Mechanism | Pin |
|---|---|---|---|
| C1 | `tools.RecipeSessionProbe` (3 methods, nil-tolerant) — core guards accept an active recipe session as workflow context + adoption exemption | in-process interface owned by CORE (`tools/guard.go`), satisfied by `*recipe.Store`, wired only in `server.go` inside the gate; untyped nil when gate off | `TestServer_AllToolsRegistered` (leak guard), guard tests with `fakeRecipeProbe`, semantics mirrored in the fake's comment |
| C2 | Schema provider — recipe gates validate against the live schema cache | `recipeStore.SetSchemaProvider` closure, constructor injection in `server.go` | recipe package tests |
| C3 | State namespaces — authoring owns `~/recipes` (`ZCP_RECIPE_MOUNT_ROOT`) + future `.zcp/state/port/`; core owns `.zcp/state/{work,services,…}`; neither reads the other's | filesystem convention | pin lands with `authoring/port` (first second-namespace consumer) |
| C4 | Knowledge corpus — both sides read `internal/knowledge`'s embedded store | shared read-only dependency | existing corpus tests |
| C5 | workflow identifier set (= L3) | compile-time | L3 test |
| C6 | Core-sync exported surface consumed by `authoring/publish`: `Config` (+ `Push.*` fields), `GH` + exported methods (incl. `ListDirectory`), `PushResult`/`Status` | normal allowed import | L2 + publish tests; `today()`/`shortRand()` deliberately duplicated (trivial utilities — exporting would widen C6) |

NOT contracts (look like coupling, aren't): engine-version stamp (same
binary ⇒ same `server.Version` stream); the agent itself carrying
observations between core tools and authoring tools (MCP-level composition,
no process coupling); `instructions.go`'s recipe-session hint (keys on a
live v2 REGISTRY session, not the gated v3 store — v2 recipe sessions
cannot be started since the dispatch block, so the hint is dead-for-end-users
in practice and retires with the v2 remnants).

## 4. The gate

`ZCP_AUTHORING=1` (exactly `"1"`; default off) — read once in
`server.go::authoringEnabled`. Inside the gate block: store construction
(`ZCP_RECIPE_MOUNT_ROOT` handling), schema-provider wiring,
`recipe.Register`, probe assignment. One env var covers the whole domain —
recipe authoring, future port flow, future authoring tools share the
audience (Aleš + maintainers).

- Gate OFF (every end user): tool list has NO authoring tool, agents pay
  zero context cost, probe is nil, guards behave as "no recipe session".
  No end-user-reachable string names a gated tool (swept 2026-06-11;
  session-gated redirect strings and code comments legitimately remain).
- Gate ON (maintainer container/shell env): identical end-user surface PLUS
  the authoring tools. Same binary, same auto-update, same interactive
  claude/codex flow — `zcp serve` inherits the env through the agent
  process; MCP config templates need no change.
- The gate is activation, not security: the code ships either way; the
  tools are non-destructive without operator-local credentials (gh auth,
  `.sync.yaml`, Strapi token).
- CLI subcommands (`zcp sync recipe *`, `zcp analyze *`) are deliberately
  ungated — operator-driven, no agent context cost, harmless without
  credentials.
- Pinned by: `TestServer_AllToolsRegistered`,
  `TestServer_AuthoringToolsRegistered`, `TestAnnotations_AuthoringTools`,
  `TestAnnotations_AuthoringToolsAbsentByDefault`.

## 5. Ownership

`internal/authoring/**` = the authoring domain — **Aleš primary owner**;
the CLAUDE.md flag+discuss protocol applies to the whole subtree. Core =
everything else, freely refactorable: if L1-L3 stay green, a core change
cannot have broken authoring at compile level, and the only behavior
contracts to think about are the six in §3. Conversely Aleš refactors
inside the subtree freely; breaking C1's interface satisfaction fails the
`server.go` compile immediately.

Statelessness: authoring tool-handler packages without a deliberate store
(publish, analyze, future port) are scanned by `TestNoCrossCallHandlerState`
(add new packages to its roots when they land); `authoring/recipe` is exempt
by design — its Store IS deliberate cross-call session state with the
plan.json-rehydration recovery model, pinned by its own tests.

## 6. Operational notes

- **Aleš's flow:** set `ZCP_AUTHORING=1` on the container (Zerops GUI env)
  or `export` in shell; everything else is unchanged — same binary, same
  auto-update, "create Laravel minimal recipe" in claude/codex works as
  before.
- **flow-eval:** container mode propagates only `^ZCP_E2E_`-prefixed vars
  (`eval/behavioral/flow-eval.sh`); when the first recipe-AUTHORING
  scenario lands, extend the grep to `^ZCP_(E2E_|AUTHORING)`. Local mode
  inherits the operator shell. No current scenario calls the authoring
  tools (all recipe-* scenarios are bootstrap-route consumers).
- **User projects:** the agents_shared.md de-mention rides the existing
  idempotent AGENTS.md/CLAUDE.md auto-refresh at server start.
