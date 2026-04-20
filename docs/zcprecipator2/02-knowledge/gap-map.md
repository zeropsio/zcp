# gap-map.md

**Purpose**: enumerate facts the agent **needs** at a given phase but doesn't have, or has in a form it can't act on. Each entry ties to a concrete v20-v34 defect class from [`../../recipe-version-log.md`](../../recipe-version-log.md).

A gap differs from a misroute: misroute = fact delivered at the wrong time/audience but exists somewhere in the system. Gap = fact does not reliably exist in the system at the moment it's needed.

---

## 1. v34 cross-scaffold env-var coordination (DB_PASS vs DB_PASSWORD)

**Concrete defect class**: v34 — apidev scaffold read `process.env.DB_PASS`; workerdev scaffold read `process.env.DB_PASSWORD`. Both NestJS codebases share the same Postgres service (env vars prefixed `DB_*`). The platform provides `DB_PASS`, not `DB_PASSWORD`. Workerdev crashed at runtime with SASL error at 10:42:06 (v34 main trace).

**Evidence from v34 main trace**:
- Event #59-#61: main diagnoses by `ssh workerdev 'env | grep DB_'` (shows `DB_PASS`), then `node -e` test to confirm var name.
- Event #63: `ssh apidev 'grep -rn DB_PASS'` — confirms apidev uses `DB_PASS`.
- Event #64: `Edit /var/www/workerdev/src/app.module.ts` — changes `process.env.DB_PASSWORD` → `process.env.DB_PASS`.
- Total debug window: 10:42:06 → 10:42:43 (~37 seconds on this one gap).

**Where the gap lives**:
- Scaffold sub-agents are dispatched in parallel (10:23:14 / 10:24:38 / 10:25:27 — within ~2 minutes).
- Each receives its own brief with its own feature list; briefs don't cross-reference each other.
- Plan fields (`plan.Research.Targets`) contain hostnames but NOT a shared symbol table declaring "the DB env var name across all codebases is `DB_PASS`".
- The env-var name is **known at dispatch time** — it's the platform convention, discoverable via `zerops_discover`. But the discovery is consumed by main, then re-derived independently by each scaffold sub-agent from its *own* code reading.

**Why the current architecture misses it**:
- `env-var-discovery` topic (recipe.md:353-375) tells main about the env vars as they exist on the platform. Main interpolates env info into each scaffold dispatch but NOT as a structured symbol-naming contract. Scaffold apidev independently decides "I'll name my TypeORM password field from DB_PASS" (correct); scaffold workerdev independently decides "I'll name mine from DB_PASSWORD" (wrong — imagined the more-conventional `DB_PASSWORD` from general Node.js experience).
- No check at generate-complete or deploy-dev compares env-var references across codebases for name-consistency.

**Architectural invariant that would close this**: principle #3 — scaffold sub-agents share a **symbol-naming contract**. Plan field: `SymbolContract { envVars: { db: { host: DB_HOST, port: DB_PORT, user: DB_USER, pass: DB_PASS, name: DB_NAME }, nats: { host, port, user, pass }, ... }, natsSubjects: { jobs: "jobs.process" }, natsQueues: { workers: "workers" }, ... }`. Every scaffold dispatch receives the same object. Code-side derivation uses it as source-of-truth.

**Complementary runnable check** (principle #1): `ssh {hostname} 'node -e "console.log(process.env)"'` + grep the source for `process.env.*` references + diff the sets — runs locally before `complete step=deploy substep=start-processes`. Closes the gap at the moment it matters.

**Related v22 class**: NATS URL-embedded creds recurrence — same gap shape (scaffold agents independently derived NATS connection code, one embedded creds in URL). Closed in content (gotcha documentation, Principle 5) but not in contract (symbol table).

---

## 2. v34 manifest ↔ content inconsistency (DB_PASS as gotcha despite manifest routing to claude-md)

**Concrete defect class**: v34 — workerdev's README shipped "SASL: client password must be a string" as a knowledge-base gotcha. The writer's own `ZCP_CONTENT_MANIFEST.json` classified this fact as `classification: self-inflicted, routed_to: claude-md, override_reason: (empty)`.

Writer routed the fact to CLAUDE.md but ALSO published it as a gotcha — a direct internal contradiction.

**Where the gap lives**:
- `writer_manifest_honesty` check (workflow_checks_content_manifest.go:156-185) covers dimensions:
  - `(discarded, published_as_gotcha)` — fact was classified as discard but appeared in README knowledge-base. ✅ CHECKED via Jaccard 0.3 threshold.
- The check does NOT cover:
  - `(routed_to=claude-md, published_as_gotcha)` — this is v34's exact violation.
  - `(routed_to=integration-guide, published_as_gotcha)`
  - `(routed_to=zerops-yaml-comment, published_as_gotcha)`
  - `(classification=library-meta override without override_reason, published_as_gotcha)`
  - `(any routing, published_as_intro)` — intro is scope-limited by `intro_length` but not manifest-cross-checked.

**Architectural invariant that would close this**: principle #5 — fact routing is a two-way graph. Every fact has at most one manifest entry; every published item has a fact source. The check dimensions expand to all routing × publishing pairs.

**Runnable form**: author can run `jq '.facts[] | select(.classification != "framework-invariant" and .routed_to != "discarded") | .fact_title' ZCP_CONTENT_MANIFEST.json` + grep those titles against each codebase README's gotcha stems. If any match, the manifest is lying about where the fact went.

**Evidence**: v34 workerdev gotcha #3 shipped despite manifest; check passed because it only covered the `discarded` dimension; the gotcha was caught **by humans reading the recipe-version-log.md entry**, not by any gate.

---

## 3. Feature sub-agent reads no scaffold-phase symbol table

**Gap**: feature sub-agent implements CRUD across three codebases (apidev, appdev, workerdev). It needs to know:
- DTO interface names — must match across codebases.
- Route paths — `/api/items` in apidev must match `fetch('/api/items')` in appdev.
- NATS subject + queue — `jobs.process` / `workers` must match between publisher (apidev) and subscriber (workerdev).
- Env var names (see gap #1).

**Current delivery**:
- `dev-deploy-subagent-brief` mentions "contract discipline (DTO interfaces first)" as a principle.
- Prior Discoveries block injection includes scaffold-recorded facts (but only facts the scaffold agent chose to record).
- Plan fields include feature list but not cross-codebase contract object.

**Concrete example from v34**: feature sub-agent recorded 3 `cross_codebase_contract` facts during its run (NATS subject+queue, DTO duplication pattern, and one more). These facts are IT declaring the contract AT implementation time, not reading a pre-declared contract.

**Consequence**: each feature is implemented by discovery rather than by contract. Discovery works at v34 scale (0 runtime contract breaks at feature stage — only the env-var gap at gap #1). At higher complexity (more features, more codebases), discovery rate will drop.

**Architectural invariant**: principle #3 (symbol-naming contract) covers this too — same plan field carries feature-level contracts.

---

## 4. `zerops_workspace_manifest` availability — registered but not observable as consumed

**Gap**: the `zerops_workspace_manifest` tool is architected (v8.94) as fresh-context input for the writer sub-agent. v34 main trace shows 0 calls; v34 sub-agent traces show 0 calls.

**Why this is a gap**:
- The `content-authoring-brief` block describes the writer's input model in terms of the facts log + citation map + canonical output tree. The `zerops_workspace_manifest` tool is referenced (recipe.md has `zerops_workspace_manifest` mentioned) but the writer brief doesn't explicitly instruct invocation.
- Writer effectively reads the mount filesystem directly via Read/Grep/Glob to understand workspace state, not via the manifest tool.
- Intended architecture (v8.94): writer receives a structured manifest describing each codebase's decision space, with fewer tokens than open-reading the mount. Actual: writer reads the mount.

**Defect class**: no specific version defect — this is architectural under-realization rather than a regression. But it means the v8.94 mechanism is in effect unused by v34.

**Recommendation**: step 3 should either (a) wire the manifest into the writer dispatch as a compose-time interpolation, or (b) delete the tool if Read-the-mount is the validated pattern. Don't leave a half-used tool.

---

## 5. Minimal-tier writer dispatch path — gap of *which flow is canonical*

**Gap**: for minimal tier, the readmes substep can be either (a) dispatched to a writer sub-agent using the OLD `readme-with-fragments` block, or (b) executed inline by main. nestjs-minimal-v3's TIMELINE.md shows main-inline.

**Consequence for step 2**:
- knowledge-matrix-minimal.md rows for sub-agent 2.1 (readme-with-fragments) carry "low confidence, schematic" tags.
- The matrix cannot populate an authoritative sizing / tool mix for this dispatch.

**Defect class**: not a named v20-v34 defect — but the observation "the minimal tier has been under-audited" (README §2) IS the gap.

**Escalation rule** per RESUME decision #1: commission targeted minimal run only if step 3 surfaces a brief-composition question that only live evidence resolves.

---

## 6. Recipe-version-log knowledge not available to sub-agents

**Gap**: `recipe-version-log.md` contains the canonical record of which defect classes were closed by which version, how verdicts (✅/❌) were assigned, and what the mechanism is.

**Current state**:
- Main agent has no injection of this file.
- Sub-agents don't receive it.
- Version anchors leak into briefs (see misroute-map #7) as shorthand references but without the log itself the references are opaque.

**Why this is a gap (paradoxically)**:
- The system has the knowledge (the log exists, authoritative).
- The system doesn't route it to any consumer.
- Version anchors leaked in briefs (v33 phantom tree) are consumed as unexplained patterns.

**Recommendation**: keep the gap — version anchors SHOULD NOT be in briefs. The log is for humans + for Claude-Code's context-loading, not for sub-agent runtime knowledge. Architectural principle #6.

---

## 7. Tier-aware check surface — implicit, not declarative

**Gap**: checks are gated by conditionals like `if plan.Tier == RecipeTierShowcase` or `if sharesCodebaseWith == ""`. The gating is scattered across `workflow_checks_*.go` files. There's no single table saying "for tier=minimal, these N checks run; for tier=showcase, these M checks run."

**Why this matters**:
- Minimal tier has been under-audited (per README §2).
- If a check is mistakenly gated to showcase-only and the author intends it as tier-invariant, minimal recipes silently lack the enforcement.
- Inversely, a check gated too broadly may mis-fire on minimal.

**Evidence**: knowledge-matrix-minimal.md §5 enumerates tier-gated checks by reading scattered source. A declarative check table would make the mapping explicit.

**Recommendation**: step 3's check-rewrite work should produce a single declarative check table mapping (check-name, tier-set, multi-codebase-predicate, worker-separate-codebase-predicate). This is adjacent to the atomic-layout work but specifically for the check suite.

---

## 8. No signal when Prior Discoveries is empty

**Gap**: when a substep's topic declares `IncludePriorDiscoveries=true` but no upstream fact exists (early run, sparse facts), `BuildPriorDiscoveriesBlock` may emit an empty section (or skip entirely). The brief then contains a "Prior discoveries" heading with nothing below it, OR the heading doesn't appear, depending on implementation.

**Why this is a gap**:
- The absence of a fact isn't the same as the absence of a rule — if a scaffold forgot to record a known principle, downstream sub-agents have no idea the rule was broken silently.
- v8.96 Theme B's `scope=downstream` was added to let scaffolds propagate "things the feature agent should know" — but this is opt-in at the scaffold agent's discretion. A scaffold agent that skips recording leaves the downstream blind.

**Recommendation**: make known-required-topics part of the plan. When the plan specifies "shared Postgres + NATS + S3", the dispatcher emits a fixed contract object (see gap #1, #3) so downstream sub-agents have the contract independent of whether an upstream agent chose to record a fact about it.

---

## 9. Dev-server spawn contract — implicit in `zerops_dev_server` behavior, not in briefs

**Gap**: `zerops_dev_server action=start hostname=X` spawns a dev server and returns a port + health status. Sub-agents use this tool. But the behavior's contract — the port is on the container, health check goes through the platform, startup must be idempotent, pkill-self classification — is not in the sub-agent brief except by reference to the tool's schema.

**Evidence**:
- v17.1 (dev-server spawn shape), v8.80 (pkill self-kill classifier), v8.104 Quality Fix #2 (port-stop polling) — these are all server-side behaviors.
- Brief doesn't restate them; sub-agent discovers by tool use.

**Why this is mostly fine**:
- v34 shows 0 dev-server-related errors. The tool-schema-via-invocation model works.
- Sub-agents don't need the internals — they need the interface.

**Why it's still a gap**:
- When a dev-server call returns an error (v34: 0 errors; other versions: various), the sub-agent has to re-derive what the error class means. No error-class guide.
- `zerops_dev_server` error classes (pkill-self, port-in-use, container-rebuild) each have a specific recovery shape that the brief could state up-front.

**Recommendation**: small dedicated atom `principles/dev-server-contract.md` referenced from scaffold and feature briefs. Declares the expected error classes and the per-class recovery.

---

## 10. No canonical output-tree declaration for minimal tier

**Gap**: the `content-authoring-brief` (showcase) enumerates canonical output: `README.md` per codebase + `CLAUDE.md` per codebase + `ZCP_CONTENT_MANIFEST.json` at project root.

Minimal tier's equivalent is `readme-with-fragments` (OLD v8 block) which declares: README fragments + CLAUDE.md. The block text predates the manifest artifact.

**Consequence**:
- If a minimal run ever dispatches its writer, `ZCP_CONTENT_MANIFEST.json` may or may not be produced (block doesn't declare it).
- The `writer_content_manifest_exists` check will fail for minimal unless the dispatched writer happens to produce it.
- Currently: v3 TIMELINE shows main-inline writing with no manifest produced.

**Runnable check status**: `writer_content_manifest_exists` runs for minimal tier (not tier-gated per check enumeration) — so minimal readmes substep would fail this check if the writer doesn't produce the manifest. This may be why minimal defaults to main-inline: the dispatch path's check surface is incompatible with the block text.

**Defect class**: structural gap, not named. Potentially v34-adjacent.

**Recommendation**: step 3 — declare canonical output tree per tier explicitly; rewrite minimal writer atom to match the showcase fresh-context shape OR gate the manifest checks to `tier=showcase`. Don't leave a check that reliably fails on minimal unless dispatch is skipped.

---

## 11. Summary — gaps by architectural axis

| Axis | Concrete defect class | Gap | Closing mechanism |
|---|---|---|---|
| Cross-codebase symbol consistency | v22, v34 env-var | No shared symbol-naming contract | Principle #3 + runnable cross-codebase env-var check |
| Manifest ↔ content | v34 DB_PASS gotcha | `writer_manifest_honesty` only covers (discarded, published) | Principle #5 — all routing dimensions |
| Cross-codebase feature contracts | implicit in v22 | Feature sub-agent receives contract by discovery | Principle #3 (broader) |
| Workspace manifest tool | v8.94 partial-realization | Tool registered but not wired to writer brief | Step 3 decision: wire or delete |
| Minimal-tier writer path | under-auditing | Dispatch vs main-inline is observationally unclear | Escalation: live minimal run if step 3 needs it |
| Version-log distribution | v33 phantom tree | Version anchors leak, log itself doesn't flow | Anti-goal: anchors out of briefs, log stays in human tooling |
| Tier-aware check surface | structural | Gating scattered across files | Declarative check table in step 3 |
| Empty Prior Discoveries signal | opt-in-discretion | Missing fact is indistinguishable from rule satisfied | Plan-field contract + facts log together |
| Dev-server error classes | v17.1-era | Brief doesn't state interface | Small dedicated atom |
| Minimal canonical output tree | v34-adjacent structural | Manifest check vs minimal writer path inconsistent | Tier-aware output-tree declaration |

Each entry has a defect class pointer and a proposed closing mechanism tied to architectural principles #1-#7 from README.md §5.
