# Recipator + OSS-port extraction — analysis & recommendation (2026-06-11)

**Status: ANALYSIS — option C chosen; implementation plan lives in
`plans/authoring-boundary-2026-06-11.md` (boundary layout, enforcement, phases).**

Question (Karel): the framework-recipe generator (`zerops_recipe` v3, `internal/recipe/`)
and the new OSS-port flow (PR #5, `feat/oss-port-flow`) are internal maintainer tools that
don't belong in the public ZCP product. Extract them — but how? Options on the table:
(A) separate repo + second MCP server cooperating with zcp, (B) second binary in the same
repo, (C) same binary + env-gated registration, (D) build tags excluding internal code from
production builds.

Evidence below comes from a 5-agent verified sweep (PR #5 diff, recipe coupling map,
shared-code closure, distribution pipeline, adversarial completeness pass).

---

## 1. What end users pay TODAY (the actual harm)

- `zerops_recipe` registers **unconditionally** (`internal/server/server.go:233`) and
  injects a measured **12,603 bytes** (12,266 B input schema + 337 B description) into
  every end-user agent's context, every session. The `handlers.go:358` comment claiming
  a strangler-fig gate is stale — there is no gate.
- PR #5 as-written adds **~71 lines of `Port*` fields** to the shared `WorkflowInput`
  struct (`internal/tools/workflow.go`), so the port surface ships in the **public
  jsonschema of `zerops_workflow` for every user** the moment it merges — regardless of
  any later dispatch gating (the SDK infers schema from the struct type).
- Binary size is a non-argument: `internal/recipe` ≈ 560 KB symbols + 516 KB embedded
  content in a 30 MB binary (~3%). The win is surface/ownership, not bytes.

## 2. Load-bearing facts per candidate

### Recipe engine (Aleš, v3)
- 24,741 prod LOC, single package; imports only `knowledge`, `schema`, `topology`.
- Coupling into zcp is **seam-shaped already**: `internal/tools` never imports recipe in
  production — it consumes the 3-method nil-tolerant `RecipeSessionProbe` interface
  (`tools/guard.go:32`), wired only in `server.go`. Other compile-time consumers:
  `internal/sync/export.go` (reads on-disk plan.json + `.refinement-closed` marker),
  `internal/analyze` + `zcp analyze recipe-run-v3` CLI, two never-released dev binaries
  (`cmd/zcp-recipe-sim`, `cmd/zcp-recipe-patch`).
- Cross-process operation is **already a designed pattern**: sessions rehydrate from
  `<outputRoot>/plan.json` precisely because "a sub-agent dispatch runs in a separate MCP
  server instance" (`handlers.go:162-214`). The ONLY signal with no on-disk
  representation is `HasAnySession` (in-memory map) — the one new contract a
  two-process design would need.
- Engine-version gate is per-hosting-binary (`gate_engine_version_stamped.go`); "dev"
  builds skip it silently.

### OSS-port flow (PR #5)
- NOT a new tool: `workflow="port"` dispatched inside `zerops_workflow` via a 5-line hook;
  5 actions (start/iterate/harden/capture/status).
- Stage A (port→debug→harden) makes **zero in-process ops/platform calls** — the agent
  runs deploys via ordinary `zerops_*` tools between turns and reports observed
  `FailureClassification` back. The 9 `port_*.go` files in `internal/workflow` import only
  topology/schema/stdlib + one unexported helper (`atomicWriteJSON`). State = per-PID
  sidecar `.zcp/state/port/{pid}.json`, separate from work sessions and the recipe store.
- Stage B (capture/publish) is the only heavy edge: `workflow_port_capture.go` is the
  **first-ever production `tools→recipe` AND `tools→sync` import** (PR's own verification
  plan: "no production precedent today"). It drives `sync.{CreateRecipeRepo,PushAppSource,
  PublishRecipe}` — making `internal/sync` MCP-reachable for the first time (gh CLI auth +
  `.sync.yaml` operator assumptions enter a tool path).
- Shared-file touches are hook-shaped (1-line case extensions; `PhasePortActive` const
  that `ComputeEnvelope` never produces — generic `action=status` cannot discover a port
  session, recovery only via the port-specific status fork). `internal/recipe` touches:
  2 default-off struct fields (`Plan.GlueRepoURL`, `Service.ModeMeasured`), an emitter
  signature change, a single-owner mode function — byte-identical for framework path,
  pinned by tests.

### Shared-code bill for a separate repo (option A)
- Go forbids cross-module `internal/` imports. Recipe alone would drag ~12.1k LOC
  (platform/schema/topology/knowledge/content closure). Port drags the **full workflow
  engine (23k LOC) + sync (3.5k, which itself re-imports recipe AND workflow)** →
  **~38.7k LOC** would have to be promoted to `pkg/` of a PUBLIC repo (de-facto public
  API; no `pkg/` exists today, single module, no go.work, `replace` forbidden) or
  duplicated.
- Embedded knowledge corpus: recipes/guides .md are gitignored + synced, so a go-module
  dependency embeds an EMPTY corpus. Workable alternative exists (release CI already runs
  unauthenticated `zcp sync pull` before build; `.sync.yaml` is committed) — but it must
  be deliberately carried over, and the failure is silent.

### Distribution
- Release builds ONLY `./cmd/zcp`; auto-update is hardwired to repo `zeropsio/zcp` +
  asset pattern `zcp-<os>-<arch>` (`internal/update/check.go:20,55`). A second binary has
  no update channel and no in-repo container install path (zcp lands on containers
  platform-side).
- Conditional-registration precedent exists: browser gate
  (`server.go:244: InContainer && AgentBrowserAvailable`) + `annotations_test.go`
  `browserExempt` carve-out. Gating blast radius for `zerops_recipe` is **unit-layer
  only** (server_test exact tool count, annotations table; zero integration/e2e refs).
- Residue strings if gated/extracted: `agents_shared.md` (rendered into every user
  project's AGENTS.md/CLAUDE.md, auto-refreshed at server start — one-way idempotent
  migration), `zerops_recipe` mentions in always-registered tool descriptions
  (knowledge.go, import.go, record_fact.go, guard.go:58), v2 `workflow=recipe`
  hard-block redirect text, `idle-bootstrap-entry` atom. Permission allowlists are
  wildcard `mcp__zerops__*` — no per-tool breakage.

## 3. Options verdict

| Option | Verdict | Deciding fact |
|---|---|---|
| **A** separate repo + 2nd MCP | **Rejected** | ~38.7k LOC `internal/`→`pkg/` public-API bill; in-process seams (probe, SetSchemaProvider) break with SILENT regressions (false ADOPT_REQUIRED during authoring = run-24 class); own release+update+install channel; corpus pipeline duplication. All for an internal tool with ~2 users. The cooperation pattern itself is proven (agent bridges servers; Stage A needs no in-process calls) — it's the code-sharing wall that kills it. |
| **B** 2nd binary, same repo | **Deferred — the evolution path, not the first step** | `internal/` sharing is free (precedent: `cmd/zcp-recipe-sim` imports recipe, never shipped). But: severs the same in-process seams as A (needs the file-level `HasAnySession` contract), no auto-update channel, second `.mcp.json` entry (5/6 init adapters hardcode single `zerops` entry), PID-keyed port state assumes one server process. Buys repo-internal code removal from the public binary — do it later if C proves insufficient. |
| **C** env-gated, same binary | **RECOMMENDED** | One conditional around 2 registration sites; existing browser-gate + browserExempt template; unit-layer-only test residue; removes 12.6 KB+port schema from every end-user context; probe/schema-provider/PID-state/engine-version all keep working unchanged. Cost: code stays in the public binary of a public repo (gate is discoverable — acceptable: tools are non-destructive without gh/org write access). |
| **D** build tags | **Rejected** | Zero feature-tag precedent (verified: all existing tags are GOOS or test-harness); Go can't tag struct fields — `WorkflowInput.Port*` and `Plan.GlueRepoURL`/`ModeMeasured` survive tagging unless structs are duplicated; doubles the test/lint/CI matrix. Fails to remove exactly the surface most worth removing. |

## 4. The sequencing insight — PR #5 is a one-way door

Once PR #5 merges and releases as-is, the `Port*` fields are part of the **published**
`zerops_workflow` schema (backward-compat seam per Engineering Priority), and the first
production `tools→recipe`/`tools→sync` imports erode the probe seam that makes recipe
extraction cheap. **Reshape before merge, not after.**

## 5. Recommended plan

### P1 — reshape PR #5 pre-merge (coordinate with Aleš; it's his PR)
- Port flow becomes its **own MCP tool** (working name `zerops_port`) with its own input
  struct + registration site. Delete the ~71 `Port*` lines from `WorkflowInput`, delete
  the dispatch hook from `handleWorkflowAction`. The port files already have their own
  dispatch + session sidecar and don't use the engine — this is a registration move, not
  a redesign.
- Review notes to carry along: `PhasePortActive` is a dead enum member from the engine's
  perspective (status recovery invisible to generic `action=status`) — either wire it or
  scope it to the port tool; Stage B publish authority (OQ-1) stays open.

### P2 — env-gate the internal surface (e.g. `ZCP_INTERNAL_TOOLS=1`)
- Gate `recipe.Register` (server.go:233) + the new `RegisterPort` behind the env var.
  Aleš's flow: set the env on his container (Zerops GUI env / shell export), same zcp
  binary, same auto-update, same interactive claude/codex usage — zero workflow change.
- Test carve-outs via the `browserExempt` pattern; fix server_test exact-count list.
- De-mention `zerops_recipe` from end-user surfaces: `agents_shared.md` (auto-refresh
  handles existing projects), always-registered tool descriptions, v2 redirect text,
  `idle-bootstrap-entry` atom. Keep `zcp sync *` + `zcp analyze recipe-run-v3` CLI
  subcommands as-is (operator-driven, harmless without gh auth + `.sync.yaml`).
- Decide: does `zerops_workspace_manifest` gate too? (Effectively recipe-only in
  practice; its no-session error says so.)

### P3 (optional, later) — second binary if true removal is wanted
- Prereq: file-level open-session contract (marker file beside the already-file-shaped
  plan.json) so zcp's probe answers `HasAnySession`/`CoversHost` cross-process.
- Ship as extra release asset from the same repo; manual/`zcp`-driven install for the
  two maintainers. Only worth it if "code in public binary" becomes a real problem.

## 6. Open questions

1. Env var name + granularity (`ZCP_INTERNAL_TOOLS` vs separate `ZCP_RECIPATOR` /
   `ZCP_OSS_PORT`)? One flag covering both is simpler; they're the same audience.
2. Stage B `tools→recipe`+`tools→sync` import: acceptable under the gate (recommended),
   or should capture live in its own package to preserve the probe seam for P3?
3. `zerops_workspace_manifest` — gate with the recipator or keep public for v2 sessions?
4. Aleš sign-off: P1 reshapes his PR; P2 gates his tool. CLAUDE.md protocol =
   flag + discuss first. This document is the flag.

## 7. Effort

- P1: ~1 day inside PR #5 (move registration + input struct, retests).
- P2: ~1 day (~300-500 LOC incl. test carve-outs + string de-mentions).
- P3: not estimated — only sketch; decide after P1+P2 live for a while.
