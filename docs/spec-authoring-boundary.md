# Authoring Boundary — internal/authoring/

Status: SHIPPED 2026-06-11 (plan: `plans/archive/authoring-boundary-2026-06-11.md`;
analysis: `plans/archive/recipator-oss-extraction-2026-06-11.md`).

`internal/authoring/` is the **maintainer-only authoring domain**: tooling
that produces and publishes content FOR the Zerops platform (recipes for the
corpus) rather than operating a user's project. It ships inside the single
zcp binary but is invisible to end users — its MCP surface registers only
behind the `ZCP_AUTHORING` gate, and two compile-time direction laws make
the boundary mechanical rather than conventional.

The boundary answers one question with certainty: **what relates to what.**
A core refactor cannot silently break authoring (L1-L2 fail the build/test
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
| `authoring/port/` | OSS port flow: `zerops_port` tool (self-registering) — port→debug→harden→capture engine, standalone per-PID session, Stage B capture/publish | integrated from PR #5 (2026-06-12), reshaped to this boundary |

`authoring/port/` follows the prescribed shape exactly: its own package,
self-registering its own gated tool (NOT fields on `WorkflowInput`, NOT a
`workflow=` value), mirroring `recipe.Register`. Its handlers follow the
recipe model: own input struct, own in-band JSON result envelope, no imports
from `internal/tools` (the credential-contract owner
`errwire.go::appendCredentialContract` stays core-only — port never produces
credential-class platform errors). Its per-PID state lives in the
authoring-owned `.zcp/state/port/` namespace (C3). Spec:
`docs/spec-oss-port-flow.md`.

**Deliberately core (NOT authoring):**
- `internal/sync` — content sync (pull/push/cache, GH plumbing, transforms):
  CI runs `zcp sync pull` before every build; docs maintenance is a repo
  concern, not recipe authoring.
- bootstrap `route=recipe` (recipe CONSUMPTION — `internal/workflow/
  recipe_override.go`, `recipe_shape.go`): provisions user projects from the
  corpus; imports nothing from authoring (pinned).
- `internal/knowledge` recipe corpus — shared read-only by both sides.

(The v2 recipe remnants that used to sit dispatch-blocked in core —
`workflow_recipe.go`, `zerops_guidance`, `internal/content/workflows/recipe*`,
the `internal/workflow` recipe cluster — were deleted 2026-06-12.)

## 2. The laws

| # | Law | depguard rule | Architecture test |
|---|---|---|---|
| L1 | Core never imports authoring. Composition root: `internal/server` only. | `core-not-authoring` | `TestAuthoringBoundary_CoreDoesNotImportAuthoring` |
| L2 | Authoring imports core only through the allowlist: `topology`, `schema`, `knowledge`, `platform`, `sync` (+ stdlib, mcp go-sdk, jsonschema-go, yaml.v3). Notably NOT `workflow` — the last edges (v2 session close-gate, `CanonicalEnvFolders`) retired with the v2 remnants 2026-06-12. jsonschema-go is the mcp go-sdk's own schema vocabulary (`mcp.Tool.InputSchema` IS `*jsonschema.Schema`), admitted 2026-06-12 for the port tool's FlexBool schema patch. | `authoring-allowlist` (strict) | `TestAuthoringBoundary_AuthoringImportsAllowlistedOnly` |
| L3 | No in-process coupling outside §3 contracts. | (consequence of L1+L2) | contract pins per §3 |

Dual enforcement (depguard + AST test) is deliberate — same rationale as
`TestArchitectureLayering`. Scanner self-tests (`*_FiresOnFixture`) prove
the matchers fire. **Extending any allowlist is a deliberate contract
change: update the depguard rule, the test allowlist, and this spec in the
same commit.**

Outside the enforced surface, by design: `cmd/zcp` (CLI wiring for
`sync recipe *` + `analyze *` subcommands), `cmd/zcp-recipe-sim` and
`cmd/zcp-recipe-patch` (dev-only, never released).
All sit outside `internal/` and none ship MCP surface. The repo-root test
harnesses (`integration/`, `e2e/`) ARE inside L1's enforcement (depguard
globs + the L1 test's extra roots): they exercise authoring only through
the composed server (gate env), never by import — a direct import would
couple the harness to the domain and break its severability.

## 3. Cross-boundary runtime contracts

| # | Contract | Mechanism | Pin |
|---|---|---|---|
| C1 | `tools.RecipeSessionProbe` (3 methods, nil-tolerant) — core guards accept an active recipe session as workflow context + adoption exemption | in-process interface owned by CORE (`tools/guard.go`), satisfied by `*recipe.Store`, wired only in `server.go` inside the gate; untyped nil when gate off | `TestServer_AllToolsRegistered` (leak guard), guard tests with `fakeRecipeProbe`, semantics mirrored in the fake's comment |
| C2 | Schema provider — recipe gates validate against the live schema cache | `recipeStore.SetSchemaProvider` closure, constructor injection in `server.go` | recipe package tests |
| C3 | State namespaces — authoring owns `~/recipes` (`ZCP_RECIPE_MOUNT_ROOT`) + `.zcp/state/port/` (PortSession sidecars) + `.zcp/state/port-recipes/` (capture output); core owns `.zcp/state/{work,services,…}`; neither reads the other's | filesystem convention | `TestAuthoringBoundary_StateNamespaces` (AST scan of every `filepath.Join(stateDir, …)` site on both sides; extending `authoringStateNamespaces` is a deliberate contract change) |
| C4 | Knowledge corpus — both sides read `internal/knowledge`'s embedded store | shared read-only dependency | existing corpus tests |
| C5 | Core-sync exported surface consumed by `authoring/publish`: `Config` (+ `Push.*` fields), `GH` + exported methods (incl. `ListDirectory`), `PushResult`/`Status` | normal allowed import | L2 + publish tests; `today()`/`shortRand()` deliberately duplicated (trivial utilities — exporting would widen C5) |

NOT contracts (look like coupling, aren't): engine-version stamp (same
binary ⇒ same `server.Version` stream); the agent itself carrying
observations between core tools and authoring tools (MCP-level composition,
no process coupling).

## 4. The gate

`ZCP_AUTHORING=1` (exactly `"1"`; default off) — read ONCE by
`runtime.Detect()` into `runtime.Info.Authoring`, the single owner. That
one flag drives BOTH gated surfaces, so they cannot drift:

1. **Tool registration** (`server.go`, gated on `s.rtInfo.Authoring`):
   store construction (`ZCP_RECIPE_MOUNT_ROOT` handling), schema-provider
   wiring, `recipe.Register`, probe assignment, `port.Register`.
2. **Emitted agent context** (`content.BuildAgentsMD`, gated on
   `rt.Authoring`): appends the env-agnostic `agents_authoring.md` block
   (authoring guidance + trigger routing for `zerops_recipe` and
   `zerops_port` — framework showcase vs foreign-OSS port) to
   AGENTS.md/CLAUDE.md only when on.

One env var covers the whole domain — recipe authoring, the OSS port flow,
future authoring tools share the audience (maintainers).

- Gate OFF (every end user): tool list has NO authoring tool, agents pay
  zero context cost, probe is nil, guards behave as "no recipe session";
  AGENTS.md carries NO recipe-authoring guidance (only the universal
  bootstrap `route="recipe"` consumption line). No end-user-reachable
  string names a gated tool (session-gated redirect strings and code
  comments legitimately remain).
- Gate ON (maintainer container/shell env): identical end-user surface PLUS
  the authoring tools AND the AGENTS.md authoring block. Same binary, same
  auto-update, same interactive claude/codex flow — `zcp serve` and
  `zcp init` both resolve `rt` via `runtime.Detect`, so a maintainer shell
  with the env set produces both the tools and the matching agent context;
  MCP config templates need no change. The serve-startup refresh
  re-renders AGENTS.md to match the current gate state, so toggling the
  env converges the on-disk context on next start.
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

`internal/authoring/**` = the authoring domain — a **maintained product
surface in its own right**: it must stay functional release over release.
Core = everything else, freely refactorable: if L1-L2 stay green, a core
change cannot have broken authoring at compile level, and the only
behavior contracts to think about are the five in §3. Conversely the
authoring subtree refactors freely from the inside; breaking C1's
interface satisfaction fails the `server.go` compile immediately. A change
that touches a cross-boundary contract (allowlist entry, probe shape, gate
semantics) updates the depguard rule + test allowlist + this spec in the
same commit and re-verifies the authoring flow (recipe unit tests +
recipe-sim + a recipe flow-eval).

Statelessness: authoring tool-handler packages without a deliberate store
(publish, analyze, port) are scanned by `TestNoCrossCallHandlerState`
(add new packages to its roots when they land); `authoring/recipe` is exempt
by design — its Store IS deliberate cross-call session state with the
plan.json-rehydration recovery model, pinned by its own tests. The port
flow's per-PID disk sidecar (`.zcp/state/port/`) is persisted state, not
in-process handler state — every call re-loads it (compaction-safe).

## 6. Operational notes

- **Maintainer flow:** set `ZCP_AUTHORING=1` on the container (Zerops GUI env,
  or `export` in `~/.bashrc`/`~/.zshrc` for the terminal flow); open a NEW
  terminal so the agent it spawns inherits the var; "create Laravel minimal
  recipe" in claude/codex works as before. Same binary, same auto-update.
  Note: a Zerops env var reaches a fresh `zcp init`/`zcp serve` via
  `runtime.Detect`; an already-running code-server froze its env at boot, so
  the shell-rc export (sourced per terminal) is the reliable path for an
  existing session, the Zerops env var the persistent one.
- **flow-eval:** container mode propagates only `^ZCP_E2E_`-prefixed vars
  (`eval/behavioral/flow-eval.sh`); when the first recipe-AUTHORING
  scenario lands, extend the grep to `^ZCP_(E2E_|AUTHORING)`. Local mode
  inherits the operator shell. No current scenario calls the authoring
  tools (all recipe-* scenarios are bootstrap-route consumers).
- **User projects:** AGENTS.md/CLAUDE.md re-render on every `zcp serve`
  start (idempotent managed-section refresh) and on `zcp init`, gated on
  `rt.Authoring` — so an end-user install never shows recipe-authoring
  guidance, and a maintainer install that sets the env gets it on next
  start. Both `runtime.Detect`-resolved, so the agent context always
  matches that install's tool surface.
