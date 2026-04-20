# principles.md — architectural invariants

**Purpose**: the final, pressure-tested invariant list that governs zcprecipator2's guidance / brief / check layer. Starts from [`README.md §5`](../README.md)'s seven stake-in-the-ground principles; each is pressure-tested against step 1+2 evidence. Cut-for-insufficient-trace: none (all 7 earn their keep). Added: **P8 — Positive allow-list, not enumerated prohibition** (surfaced by v33's three invention classes and README.md §2's "replaced with positive allow-lists" constraint). Final count: **8 principles**.

Legend for defect-class citations:
- `v25` = recipe-version-log.md per-version entry for run N
- `v8.NN` = mechanism-layer fix version (lives between per-run entries)
- `§N.M` = subsection reference within that entry

Convention: every principle lists (a) concrete testable statement, (b) defect classes it closes with version anchors, (c) enforcement mechanism in the new architecture, (d) what in the current system it replaces. A principle with zero defect-class trace gets cut. §9 performs the cross-audit: every defect class closed by v8.78 → v8.104 and still architecturally relevant must be covered by ≥1 principle.

---

## P1 — Every content check has an author-runnable pre-attest form

### Concrete statement

For every check C in the rewritten check suite: the author (main agent OR a dispatched sub-agent) can execute a shell command locally against its own in-mount draft that reproduces the same pass/fail verdict the server-side gate returns. "Runnable" means the command is a grep / awk / jq / ratio-computation / python one-liner bounded by a seconds-long timeout; "locally" means it runs against SSHFS-mount paths without a `zerops_workflow` round-trip. If a check has no runnable form expressible in that shape, it either gains one (see [`check-rewrite.md`](check-rewrite.md)) or gets deleted. The gate becomes confirmation, not discovery.

### Defect classes closed

- **v23 5-round content-fix spiral (~23 min wall)** — `recipe-version-log.md §Architectural insights / v23`: external-gate + fix-subagent dispatch is anti-convergent. Writer shipped → gate → fail → re-dispatch → writer rebuilds brief each round.
- **v31 3-round deploy + 3-round finalize** — `§v31 / Top convergence cost drivers`: author knows what to write; check knows what to read; asymmetry is the structural limiter.
- **v32 3-round deploy + 2-round finalize** — `§v32 item 5`: Theme A target was 1 round each; hand-maintained coupling table was incomplete.
- **v33 3-round deploy + 2-round finalize** — `§v33 item 5`: `PerturbsChecks` added on dedup + cross-README, still didn't converge.
- **v34 4-round deploy + 3-round finalize** — `§v34 summary`: **Fix E shipped structurally and regressed rounds**. Two generations of richer-failure-metadata (v8.96 Theme A + v8.104 Fix E) without round-count improvement empirically validates that the writer needs pre-attestation verification, not post-attestation diagnostics.
- **v21 `claude_readme_consistency` silently-dead regex** — `§v21 Six zerops_dev_server ... content-check effectiveness`: check passed 0 / failed 0 across v21 because its regex matched zero phrasings outside v20's exact one. A runnable author-side command (grep against the brief's explicit vocabulary set) would have surfaced the dead state at author time.

### Enforcement mechanism

- Each atom in `principles/`, `phases/*/completion.md`, and each brief declares its **Runnable Pre-Attest** section: a fenced shell block the author pastes into its SSH session. Exit 0 = the gate will pass; non-zero = the gate will fail with the stated check name.
- Gate remains server-side (defense-in-depth) but no longer exposes failure detail rich enough to make the agent feel productive iterating on failure metadata. Failure payload shrinks to: check name + the author-runnable command + "rerun this locally, fix, then re-attest."
- The check-rewrite table in [`check-rewrite.md`](check-rewrite.md) assigns every current check to one of {keep, rewrite-to-runnable, delete}. A check staying in the "keep" lane must already emit a Runnable form in its brief.

### Replaces

- v8.81 `content_fix_dispatch_required` external-fix-subagent dispatch gate.
- v8.96 Theme A `ReadSurface` / `Required` / `Actual` / `HowToFix` / `CoupledWith` structured-failure fields (still emitted, but no longer load-bearing for convergence — now debugging signal only).
- v8.104 Fix E `PerturbsChecks` (refuted by v34 data; atoms can keep the field for human-readable tracing but the architecture doesn't rely on it for convergence).

---

## P2 — Transmitted briefs are leaf artifacts

### Concrete statement

For every Agent-tool dispatch: the prompt passed to the sub-agent contains **only** sub-agent-facing content. No dispatcher instructions ("compress this", "include verbatim", "adapt per codebase"), no version anchors (v5, v7, v8.85, v33), no internal check vocabulary (`writer_manifest_completeness`, `hostname_gotcha_distinct_from_guide`), no Go-source file paths (`internal/workflow/recipe_templates.go`). Dispatcher instructions live in a separate human-facing document (`docs/zcprecipator2/DISPATCH.md`) that is **never transmitted**. The leaf artifact is reviewable cold-read by a fresh reader who has no access to the dispatcher document.

### Defect classes closed

- **v17 scaffold sub-agents ran `cd /var/www/{host} && <exec>` zcp-side** — `§v17`: scaffold-subagent-brief's "Target mount: /var/www/appdev/" was written for the dispatcher's mental model (name the mount); sub-agent read the same text and inferred "run commands there." Mixed audience.
- **v32 Read-before-Edit rule lost across 3 scaffold subagents** — `§v32 item 3 / recipe.md:845`: main-agent dispatch compression dropped the load-bearing rule because the source block's MANDATORY sentinels weren't bounded regions byte-identically transmitted.
- **v33 phantom `/var/www/recipe-nestjs-showcase/` tree** — `§v33 item 1`: main synthesized "Output root for env READMEs + root README: /var/www/recipe-nestjs-showcase/" in the writer dispatch prompt AND paraphrased env folder names. Writer followed faithfully. Dispatch-composition axis was unspecified in source, so main invented.
- **v33 Unicode box-drawing separators in zerops.yaml** — `§v33 item 4`: recipe.md:2914 covered comment voice (word choice) but not visual style (decorative horizontal rules). Brief unspecified → agent decorated.
- **v33 9-min diagnostic-probe panic burst** — `§v33 item 3`: feature-subagent brief didn't spec probe cadence. When probing felt productive, no rule limited it; concurrency queue saturated.

### Enforcement mechanism

- Physical file-tree separation. Transmitted content lives under `internal/content/workflows/recipe/briefs/`. Dispatcher guidance lives under `docs/zcprecipator2/DISPATCH.md` (outside the content tree; never loaded by the server).
- Go stitching layer (`recipe_guidance.go`) reads only from `briefs/` + `principles/` + `phases/*/*.md` at dispatch time. The DISPATCH.md file is invisible to the emission path.
- Static test (seed-test pattern, see CLAUDE.md): every file under `briefs/` is grepped for forbidden tokens at build time — version anchors (`v[0-9]+(\.[0-9]+)*`), check names (`[a-z_]+_exists`, `writer_manifest_*`, `*_self_shadow`), Go-source paths (`internal/`), dispatcher vocabulary (`compress`, `verbatim`, `include as-is`, `main agent`, `dispatcher`). Build fails if any atom under `briefs/` matches.

### Replaces

- Current recipe.md blocks: `scaffold-subagent-brief` (L790–1125), `dev-deploy-subagent-brief` (L1675–1828), `content-authoring-brief` (L2390–2736), `readme-with-fragments` (L2205–2388), `code-review-subagent` (L3050–3158) — all mix dispatcher-facing and sub-agent-facing instructions inside one named region.

---

## P3 — Parallel sub-agents share a symbol-naming contract

### Concrete statement

For every multi-codebase recipe (showcase, and any minimal recipe with >1 codebase): the main agent computes a `SymbolContract` object from `plan.Research` **exactly once**, before the first scaffold dispatch. Every scaffold sub-agent receives the identical contract as a structured plan-field interpolation (JSON inside its prompt), not a prose "also consider these names" sentence. The contract covers: (a) env-var names per managed-service kind (DB_*, CACHE_*, QUEUE_*, STORAGE_*, SEARCH_*), (b) NATS subject + queue names, (c) HTTP route paths (`/api/items`, `/api/status/{kind}`), (d) DTO interface names, (e) hostname conventions (apidev/apistage, workerdev/workerstage, appdev/appstage). Additionally, the contract carries **fix-recurrence rules** — a list of "scaffold-phase MUST-DO" items that close past v22/v30/v34 recurrence classes: NATS separate user/pass, S3 `forcePathStyle: true`, worker SIGTERM handler + `enableShutdownHooks()`, self-shadow prohibition, `.gitignore` baseline, `.env.example` preservation, `--skip-git` on framework scaffolders.

### Defect classes closed

- **v22 NATS URL-embedded creds recurrence** — `§v22 CRIT #1`: v21 TIMELINE documented the gotcha, v21 postmortem added the README gotcha, **v22 scaffold subagents re-emitted URL-embedded-creds anyway** because each derived connection code independently from framework conventions. Gotcha in the deliverable doesn't help the scaffolder.
- **v22 S3 301 HTTPS endpoint recurrence** — `§v22 CRIT #2`: same mechanism (scaffolder independently inferred endpoint from `storage_apiHost`).
- **v30 workerdev SIGTERM handler missing** — `§v30 Close-step review`: writer-brief said MANDATORY in README; feature-subagent's scaffolded `main.ts` didn't implement it. No pre-flight rule forced the implementation.
- **v31 apidev `enableShutdownHooks()` missing** — `§v31 v8.95 calibration bar row 4`: same class as v30 but on apidev-gateway-drain.
- **v34 cross-scaffold env-var DB_PASS vs DB_PASSWORD** — `§v34 Two new defect classes item 2`: apidev scaffold decided `process.env.DB_PASS`; workerdev decided `process.env.DB_PASSWORD`. Platform provides `DB_PASS`. Runtime crash + close-review WRONG. **Single-feature-subagent pattern prevents the class at feature-phase but not at scaffold-phase.**
- **v29 Nest circular-import** — `§v29 Feature-subagent recorded-and-discarded defect class`: `cache.module.ts` ↔ `cache.controller.ts` via `REDIS_CLIENT`; recurrent across runs; fact was discarded by writer taxonomy because "framework-quirk"; no route back to scaffold-preamble.

### Enforcement mechanism

- New plan field `SymbolContract` declared in `recipe_plan.go` schema. Populated during `research` step from `plan.Research.Targets` + managed-services list + tier.
- `buildSubStepGuide` for `generate.scaffold` interpolates the contract JSON into every scaffold dispatch template identically (byte-identical JSON fragment, not per-codebase rewording).
- New pre-attest runnable check: `ssh {hostname} 'node -e "Object.keys(process.env).filter(k=>/^(DB_|CACHE_|QUEUE_|STORAGE_|SEARCH_|NATS_)/.test(k)).sort()"'` diffed across all codebases; any divergence fails the check with "env-var names in code do not match the shared SymbolContract."
- Scaffold brief MANDATORY list includes "before returning: grep your code for the contract's recurrence-rule tokens and confirm each is satisfied" (positive allow-list per P8).

### Replaces

- Independent per-scaffold env-var derivation in current `scaffold-subagent-brief` (L790–1125).
- Facts-log based cross-scaffold propagation via `scope=both` / `scope=downstream` (v8.96 Theme B): the facts log is a reactive after-the-fact signal; the contract is a proactive before-the-fact declaration. Both coexist but contract supersedes discovery where both would apply.

---

## P4 — Server workflow state IS the plan

### Concrete statement

The authoritative workflow state is `zerops_workflow action=status` at any moment. Agents (main + every sub-agent) treat it as the plan; they do not maintain a parallel plan object. Specifically: (a) step-entry guidance is framed as "substep X completes when predicate P holds on the mount" — not as "your tasks for this phase are A, B, C"; (b) `TodoWrite` usage is restricted to check-off-only mirroring of server substep state, never full-rewrite at step-entries (RESUME decision #3); (c) sub-agents never call `zerops_workflow` at spawn (server-enforced `SUBAGENT_MISUSE`, v8.90); (d) substep ordering is authoritative and substep-scoped guidance delivers at substep-complete, not step-entry (v8.90 de-eager).

### Defect classes closed

- **v25 substep-bypass** — `§v25 Defect 1`: main did 40 min of deploy work silently, then backfilled 12 substep attestations in 2 min at step end. ~73 KB of phase-scoped guidance delivered into dead phases. `subagent-brief` landed 33 min after the feature subagent finished; `readme-fragments` landed after the writer shipped.
- **v25 subagents calling `zerops_workflow` at spawn** — `§v25 Defect 2`: 2 subagents called `zerops_workflow action=start` at first tool use; server returned misleading `PREREQUISITE_MISSING` + `"Run bootstrap first"` suggestion. Closed by v8.90 `SUBAGENT_MISUSE`.
- **v34 12 TodoWrite full-rewrites** — `redundancy-map.md §6`: 10 of 12 TodoWrite events were full-rewrites at step-entry boundaries, duplicating the server's authoritative substep list. ~8 KB of redundant traffic + cognitive tax.
- **v32 close step never completed + premature export** — `§v32 item 1`: `zcp sync recipe export` ran even though `step=close` was incomplete; no export gate existed. Closed by v8.97 Fix 1 (`ExportRecipe` refuses when close incomplete).
- **v33 auto-export at close (v8.98 framing)** — `§v33 item 2`: `NextSteps[0]` autonomous framing had main run 3 `zcp sync recipe export` invocations right after close. Reverted by v8.103 export-on-request.

### Enforcement mechanism

- Step-entry atom template (`phases/*/entry.md`) forbids "your tasks for this phase are …" framing. Positive form: "this phase completes when every substep's predicate holds; `zerops_workflow action=status` shows the list."
- Substep-entry atoms state predicates first, guidance second. Example: `phases/deploy/init-commands.md` opens with "attest complete when all targets return DEPLOYED from `zerops_deploy setup=dev`; the following guidance helps you get there."
- TodoWrite rule atom (`principles/todowrite-mirror-only.md`): "TodoWrite MAY be used inside a substep to track ad-hoc sub-tasks (e.g. 3-round README fix loop). TodoWrite MUST NOT be rewritten at step-entry or substep-entry boundaries — the server's substep list IS the plan."
- Server-side invariant test: `zerops_workflow action=start` from any sub-agent context returns `SUBAGENT_MISUSE` (v8.90 survives rewrite).
- NextSteps disposition: close completion guidance declares no autonomous follow-up. Export and publish are user-request-only per v8.103.

### Replaces

- Current `deploy-framing` + step-entry composite guides that read as fresh planning context (drive the 12-rewrite pattern).
- `dev-deploy-subagent-brief` + `readme-fragments` eager-at-step-entry delivery (v8.90 de-eager is preserved).
- v8.98 NextSteps[0] autonomous-export framing.

---

## P5 — Fact routing is a two-way graph

### Concrete statement

For every recorded fact in the facts log and every published item across the six content surfaces (per-codebase README, per-codebase CLAUDE.md, env import.yaml comments, env README, zerops.yaml comments, root README): a directed edge exists. Every fact has at most one **routed_to** destination declared in `ZCP_CONTENT_MANIFEST.json`; every published item has exactly one fact source (either direct from facts log or reasoned-from-state with explicit override reason). The `writer_manifest_honesty` check enumerates **every** (routed_to × published-surface) pair, not only `(discarded, published_gotcha)`. Dimensions covered: `(routed_to=claude_md, published_gotcha)`, `(routed_to=integration_guide, published_gotcha)`, `(routed_to=zerops_yaml_comment, published_gotcha)`, `(routed_to=env_comment, published_gotcha)`, `(routed_to=discarded, any_published)`, `(routed_to=any, published_intro)`.

### Defect classes closed

- **v34 workerdev DB_PASS gotcha-despite-manifest-routing-to-claude-md** — `§v34 Two new defect classes item 1`: writer's own `ZCP_CONTENT_MANIFEST.json` classified the fact self-inflicted, routed to `claude-md`, with empty override_reason; workerdev README shipped it as a knowledge-base gotcha. `writer_manifest_honesty` covers only `(discarded, published_gotcha)` so the check passed; defect was caught by human reading the version log. The single-direction honesty check misses 5 of 6 routing dimensions.
- **v28 33% genuine gotchas + 5 wrong-surface items** — `§v28 Honest content audit`: apidev execOnce-silent-success (self-inflicted, discard class); apidev Valkey-no-password (1-line fact bloated); apidev setGlobalPrefix (framework docs); appdev api.ts-helper (recipe's own scaffold helper); appdev plugin-svelte peer-dep (npm meta).
- **v29 2/14 writer DISCARD override** — `§v29 Writer-kept-despite-DISCARD flag`: v8.94 brief pre-classified healthCheck-bare-GET + Multer-FormData as DISCARD; writer kept both as gotchas. Brief taxonomy was a suggestion; the check gate didn't cover the override path.

### Enforcement mechanism

- Expanded `workflow_checks_content_manifest.go` `checkManifestHonesty` iterates all (routed_to × surface) pairs. Each pair is a separate row in `StepCheck[]` so failure naming distinguishes them.
- Code-review sub-agent brief adds "Read `ZCP_CONTENT_MANIFEST.json`; for every fact entry, verify the claimed routed_to surface contains (or does not contain, per the routing) the fact's title-tokens."
- Facts log emits `FactRecord.Scope` (v8.96 Theme B) + new `FactRecord.RouteTo ∈ {content_gotcha, content_intro, content_ig, content_env_comment, claude_md, zerops_yaml_comment, scaffold_preamble, feature_preamble, discarded}`. Writer manifest contract includes this field as required.
- Pre-attest runnable (P1): `jq '.facts[] | select(.routed_to != "discarded" and .routed_to != "content_gotcha") | .fact_title' ZCP_CONTENT_MANIFEST.json | while read t; do for host in */README.md; do if grep -qi "$t" "$host/README.md" | awk '/knowledge-base/{f=1}/#ZEROPS_EXTRACT_END:knowledge-base/{f=0}f'; then echo "LEAK: $t in $host"; fi; done; done`.

### Replaces

- Current `writer_manifest_honesty` at [workflow_checks_content_manifest.go:156-185](../../../internal/tools/workflow_checks_content_manifest.go) covering only `(discarded, published_gotcha)`.
- Current code-review brief's feature-coverage scan (which doesn't open the manifest).

---

## P6 — Guidance is atomic; version anchors only in archive

### Concrete statement

Every piece of operational guidance lives in exactly one per-topic file under `internal/content/workflows/recipe/` (phases/ + briefs/ + principles/). The file is ≤ 300 lines. Stitching happens at dispatch time via the Go guidance layer (`recipe_guidance.go`). No atom contains version anchors (`v\d+(\.\d+)*`, `v8.\d+`). Version history is authoritative only in `docs/recipe-version-log.md`. Grep over the atomic tree for version-anchor patterns returns empty.

### Defect classes closed

- **recipe.md bloat** — `README.md §0`: current monolith is 3,438 lines with 60+ `<block>` regions. Blast radius of any block edit is unbounded; review is cognitively infeasible.
- **v33 phantom output tree w/ paraphrased env names** — `§v33 item 1`: main synthesized paths + names from the version-anchor-laden context. Every version anchor is an imitation vector for an agent pattern-matching on "recipe v32 had this behavior."
- **v33 Unicode box-drawing invention** — `§v33 item 4`: guidance about comment voice lived far from guidance about visual style; agent invented a decoration axis because no atom owned it. Atomization surfaces unowned axes (they have no file).
- **v32 dispatch compression** — `§v32 item 3`: 336-line scaffold-subagent-brief was too large to transmit verbatim; main compressed; MANDATORY sentinels were byte-identical only because v8.97 Fix 3 wrapped them in bounded regions. Atomization reduces the pressure to compress.
- **recipe.md cross-audience mixing** — `redundancy-map.md §8`: same block read by main (as dispatcher) and sub-agent (as task-brief); produces v32's dispatch-compression class structurally.

### Enforcement mechanism

- Directory tree under `internal/content/workflows/recipe/` (see [`atomic-layout.md`](atomic-layout.md)). Every atom is a named .md file with one concern.
- Go layer's `emitAtom(id)` helper loads + validates each atom at dispatch; build-time test caps every atom at 300 lines.
- Build-time grep guard: `grep -rE 'v[0-9]+(\.[0-9]+)*|v8\.[0-9]+' internal/content/workflows/recipe/` must return empty. Build fails if matches.
- Author-runnable pre-attest (P1): `wc -l internal/content/workflows/recipe/**/*.md | sort -nr | head` — any atom >300 lines is a principle-violation.

### Replaces

- Monolithic `internal/content/workflows/recipe.md` (3,438 lines).
- Version anchors inline in block bodies (grep `v25|v8\.` against current recipe.md shows ~80 matches).
- `recipe_topic_registry.go` topic-to-block mapping (block IDs become atom paths; registry becomes a path manifest).

---

## P7 — Every brief passes cold-read + defect-coverage test

### Concrete statement

Before any atomic file under `briefs/` or `phases/*/entry.md` is merged: (a) a fresh reader with no prior context reads the composed output (post-stitching), records ambiguities / contradictions / impossible-to-act-on instructions; (b) the defect-class registry from `docs/zcprecipator2/05-regression/defect-class-registry.md` (step 5 output) is walked — every closed class v20–v34 has a prevention mechanism cited in the composed brief OR in a runnable check (P1) OR in a Go-layer runtime injection. If coverage for any class is absent, the brief is not merged. Steps 4 + 5 from the research protocol are the physical gate that enforces this.

### Defect classes closed

- **v32 per-codebase READMEs + CLAUDE.md missing from exported deliverable** — `§v32 item 2`: writer produced content but close never completed → `OverlayRealREADMEs` never fired. Cold-read of the close-step atoms would have surfaced "what happens if close never completes? is the export gated?"
- **v33 writer hallucinated output paths** — `§v33 item 1`: cold-read simulation of the writer dispatch composition, starting from clean context, would have asked "where is the canonical output root declared?" and surfaced the gap.
- **v28 writer kept DISCARD-marked facts** — `§v28 Writer-kept-despite-DISCARD flag`: cold-read of the v8.94 brief would have surfaced "classification says DISCARD, but what structurally prevents the writer from publishing? the brief reads as 'consider this guidance.'"
- **v31 apidev `enableShutdownHooks()` missing (feature-subagent brief)** — `§v31 v8.95 calibration row 4`: brief covered worker SIGTERM handler; did not generalize to any OnModuleDestroy-bearing provider in apidev. Cold-read against defect-class registry would have flagged the uncovered class.

### Enforcement mechanism

- Step-4 verification artifact per (sub-agent role × tier): `brief-<role>-<tier>-composed.md` + `brief-<role>-<tier>-simulation.md` + `brief-<role>-<tier>-diff.md` + `brief-<role>-<tier>-coverage.md`.
- Coverage table rows = every row in `defect-class-registry.md`; columns = the prevention mechanism (principle N / atom X / check Y / Go injection Z). Empty cell = not merged.
- Simulation runs cold: reader has no prior context, no access to the dispatcher document, no access to the version log. Reader flags ambiguities + proposes clarifying edits to the atom.

### Replaces

- Current ad-hoc brief review (no structured verification gate).
- Implicit "looks right to me" validation at merge time.

---

## P8 — Positive allow-lists, not enumerated prohibitions

### Concrete statement

Every axis that a sub-agent must decide along must be **positively declared** in the brief: canonical output paths, visual style (ASCII vs Unicode), probe cadence, export triggering, hostname conventions, file extensions. The agent is not told "don't write box-drawing separators" — it is told "ASCII `#` comments only; one hash per line." The agent is not told "don't invent an output root" — it is told "write env READMEs to `environments/{EnvFolder(i)}/README.md`; write per-codebase files to `{hostname}/`; any other path is out-of-scope." Enumeration of forbidden behaviors is forbidden because the enumeration names the invented forbidden path and turns it into a menu of attack options.

### Defect classes closed

- **v33 phantom output tree** — `§v33 item 1`: brief didn't positively declare canonical output paths; main hallucinated "Output root for env READMEs + root README: /var/www/recipe-nestjs-showcase/" and paraphrased env folder names. v8.103 added canonical-output-tree guard; v8.104 Fix A added MANDATORY sentinel — but v8.104 Fix A is still a **prohibition** of the named phantom pattern. The positive form is "here is the one canonical tree; anything else is out-of-scope."
- **v33 Unicode box-drawing** — `§v33 item 4`: brief didn't positively declare visual style. v8.104 Fix C added "Visual style" subsection forbidding Unicode box-drawing + ASCII dividers + emoji — **still an enumeration of forbidden patterns** (agent can invent new decoration: ANSI color codes, Markdown tables, mock ASCII art). Positive form: "comments are single `#` prefixed, one line, no dividers."
- **v33 auto-export at close** — `§v33 item 2`: close-completion `NextSteps` were autonomous by default. Positive form replaces: export + publish are triggered only by explicit user request messaging; the close step's `NextSteps` field is empty.
- **v33 paraphrased env folder names** — `§v33 item 1`: main paraphrased "0 — Development with agent" etc. Brief didn't declare canonical names. Positive form: `environments/{EnvFolder(i)}/` names are computed by `recipe_templates.go EnvFolder(i)`; brief interpolates them verbatim.
- **v25 subagent rationalisation loop on `SUBAGENT_MISUSE`** — `§v25 Defect 2`: server said "Run bootstrap first" (enumerated wrong remediation); agent correctly ignored but the enumeration is dangerous. v8.90 replaced with positive form "this session already has a recipe workflow; subagent should not start another." Positive allow-list version: "subagent permitted tools: {X, Y, Z}; workflow management is the main agent's responsibility."

### Enforcement mechanism

- Brief-authoring convention: every MANDATORY section leads with "You do X" not "Do not do Y." Only after the positive form is stated may a short explicit counter-example follow (for disambiguation, not as the primary rule).
- Build-time lint on `briefs/` + `principles/`: atoms containing "do not", "avoid", "never", "MUST NOT" must also declare the positive form within the same atom; orphan prohibitions fail the lint.
- Step-4 simulation audit includes: for each negative rule found, ask "what is the positive form?" and document it; rewrite the atom.

### Replaces

- Negative enumeration patterns in current recipe.md: "don't do zcp-side git" (positive: "all git runs SSH-side"), "don't write Unicode separators" (positive: "ASCII comments only"), "don't write to recipe-{slug}/" (positive: "canonical tree is <path>"), "don't start a second workflow" (positive: "workflow management belongs to the main agent").
- v8.104 Fix A + Fix C prohibition sentinels (kept in one transitional release; refactored to positive form in the same atom pass).

---

## 9. Cross-audit — every v8.78 → v8.104 defect class covered

Every defect class closed by a mechanism-layer fix v8.78 through v8.104 (and still architecturally relevant, i.e. not purely a substrate bug) must be covered by ≥1 principle. If not, either add a principle or acknowledge the gap.

| Defect class (origin → fix) | Principle coverage | Notes |
|---|---|---|
| v21 scaffold hygiene / 208 MB node_modules leak (v8.80 §3.1) | P1 (runnable `.gitignore` grep + `find / -name node_modules` pre-attest), P3 (contract `.gitignore` baseline) | scaffold_artifact_leak stays |
| v21 `cd /var/www/` zcp-side execution (v8.80 §3.2a + v17.1 SSH preamble) | P2 (brief declares SSH-only positively), P8 (positive allow-list for commands) | bash_guard middleware stays orthogonally |
| v21 `claude_readme_consistency` dead regex (v8.80 §3.4) | P1 (runnable check per phrase must match real content) | rewrite to pattern-based |
| v21 framework-token purge (v8.80 §3.5) | P6 (atomized, framework-specifics in own atoms) | |
| v21 writer-subagent dispatch gate (v8.80 §3.6d) | P4 (server state drives dispatch ordering) | |
| v21 MCP schema rename hints (v8.80 §3.6e) | P1 (runnable schema-echo) | |
| v21 `dev_server stop` self-kill classification (v8.80 §3.7a) | not principle-relevant | substrate |
| v22 post-writer content-fix subagent dispatch (v8.81 §4.1) | **P1 supersedes** — author-runnable pre-attest eliminates the fix-dispatch loop | v8.81 gate becomes unused under new architecture |
| v22 NATS URL-embedded creds recurrence (v8.81 §4.3) | **P3 (SymbolContract with fix-recurrence rules)** | |
| v22 S3 endpoint `storage_apiUrl` (v8.81 §4.4) | **P3 (fix-recurrence rules)** | |
| v22 dev-start vs buildCommands contract (v8.81 §4.5) | P1 (runnable yaml-vs-command diff), P3 (scaffold contract) | |
| v22 architecture narrative (v8.81 §4.6, rolled back) | not required; archived as anti-goal | narrative is optional editorial |
| v22 Opus 4.7 rubric (v8.81 §4.7) | not principle-relevant | rubric adjustment |
| v22 3-split framework-expert code review (v8.81 §4.8) | not principle-relevant | dispatch-shape choice |
| v23 env-var model + `env_self_shadow` (v8.85) | P1 (runnable self-shadow grep), P3 (contract declares env var names unambiguously) | check stays, enumeration bug (v28/v29) fixed by Fix 5 in v8.94 |
| v25 substep-bypass (v8.90 Fix B de-eager) | **P4 (server state = plan), P6 (atomic so de-eager sequencing is discoverable)** | |
| v25 subagent `zerops_workflow` at spawn + misleading `Run bootstrap first` (v8.90 Fix A + D) | P4 (server-enforced), P8 (positive form of recovery) | |
| v25 subagent tool-use policy (v8.90 Fix C) | P2 (declared in leaf brief) | |
| v26 `recipePlan` stringification (v8.93.2) | not principle-relevant | jsonschema description tag |
| v26 `git init` zcp-side chown (v8.93.1) | P2 (brief declares SSH-only), P8 (positive: container-side single call) | v8.93.1 held in v28 |
| v28 fresh-context writer (v8.94 Fix 1) | P2 (fresh-context brief IS a leaf artifact) | |
| v28 `zerops_record_fact` mandatory (v8.94 Fix 2) | P5 (two-way graph needs both ends populated) | |
| v28 env READMEs substantive (v8.94 Fix 3) | P1 (runnable env-README line-count check), P3 (env tree paths declared) | |
| v28 scaffold pre-flight traps list (v8.94 Fix 4) | **P3 (fix-recurrence rules)** | generalized over v22/v30/v31/v34 |
| v28 `env_self_shadow` enumeration bug (v8.94 Fix 5) | P1 (runnable enumeration test) | |
| v29 `preship.sh` scaffold-phase artifact leak (v8.95 Fix A) | P1 (runnable `find scripts/`), P3 (contract lists forbidden files) | scaffold_artifact_leak check stays |
| v29 env-README Go-template factual drift (v8.95 Fix B) | P1 (runnable template-output-vs-YAML diff at finalize) | Go-source fix orthogonal; tests needed |
| v29 ZCP_CONTENT_MANIFEST structured contract (v8.95 Fix C) | **P5 (two-way graph needs structured manifest)** | expanded under P5 |
| v30 missing worker SIGTERM handler (v8.95 Fix #4 shipped for worker only) | **P3 (fix-recurrence rules applies to BOTH worker SIGTERM + apidev enableShutdownHooks)** | generalized beyond v8.95 scope |
| v31 apidev `enableShutdownHooks()` missing | **P3 (generalized rule)** | |
| v32 close step never completed + premature export (v8.97 Fix 1+2) | P4 (server state enforces ordering) | |
| v32 Read-before-Edit lost across 3 scaffolds (v8.97 Fix 3) | **P2 (leaf brief with byte-identical transmission)** | |
| v32 six platform principles in scaffold MANDATORY (v8.97 Fix 5) | P2 (declared in leaf brief), P3 (contract for structured creds + competing consumer + routable bind + trust proxy) | |
| v32 feature-subagent mirrored MANDATORY (v8.98 Fix A) | P2 | |
| v32 StampCoupling (v8.97 Fix 4) | P1 supersedes — coupling becomes part of pre-attest runnable | |
| v33 phantom `recipe-{slug}/` tree (v8.103 + v8.104 Fix A) | **P8 (positive: canonical tree declared); P2 (dispatch composition separated)** | |
| v33 auto-export (v8.103 revert) | **P4, P8** | NextSteps positive form is empty at close |
| v33 Unicode box-drawing (v8.104 Fix C) | **P8 (positive: ASCII `#` only)** | |
| v33 seed `execOnce ${appVersionId}` burn (v8.104 Fix B) | **P1 (runnable grep for `appVersionId` in seed execOnce)**; guidance lives in `phases/generate/zerops-yaml/setup-rules-dev.md` + `setup-rules-prod.md` | recipe-pattern bug |
| v33 feature-subagent diagnostic-probe cadence (v8.104 Fix D) | **P2 (brief positively declares cadence, e.g. max 5 bash/min)**, **P8 (positive form)** | |
| v33 pre-init git-commit sequencing (v8.104 Fix F) | P2 (brief positively declares post-scaffold re-init), P8 | |
| v34 manifest ↔ content inconsistency (DB_PASS) | **P5 (all routing dimensions)** | |
| v34 cross-scaffold env-var DB_PASS/DB_PASSWORD | **P3 (SymbolContract)** | |
| v34 Fix E convergence refutation | **P1 (author-runnable IS the convergence fix)** | |

**Audit result**: every closed + architecturally-relevant defect class v8.78–v8.104 is covered by ≥1 principle. No orphans. Three classes have multiple principle coverage (v33 phantom tree, v34 manifest inconsistency, v25 substep-bypass) — defense-in-depth, not redundancy.

**Principles with zero trace**: none. All 8 carry ≥1 cited defect class.

---

## 10. Principle-to-enforcement-layer map

Every principle maps to at least one mechanism in exactly one of: atomic content file, runnable check, Go-runtime injection, server-side state enforcement, build-time lint.

| Principle | Primary mechanism | Secondary mechanism |
|---|---|---|
| P1 | runnable check emitted in atom | gate payload shrinks to check-name + command |
| P2 | `briefs/` physical separation | build-time grep guard on forbidden tokens |
| P3 | `SymbolContract` plan field + interpolated JSON in brief | pre-attest runnable env-var diff |
| P4 | server `SUBAGENT_MISUSE` + de-eager + positive step-entry framing | TodoWrite-mirror-only principle atom |
| P5 | expanded `writer_manifest_honesty` (all pairs) + `FactRecord.RouteTo` | code-review brief reads manifest |
| P6 | atomic tree + 300-line cap + version-anchor grep | `recipe_guidance.go` stitching |
| P7 | step-4 + step-5 verification protocol | coverage-table merge gate |
| P8 | positive-form authoring convention + build lint | cold-read simulation audit |

---

## 11. Anti-goals — what the principle set deliberately does NOT mandate

- **Not** "eliminate sub-agent dispatches." Multi-agent pattern is validated (v22 3-split code review, v8.94 fresh-context writer, v28 single-author feature). Principles govern the brief-composition layer, not dispatch existence.
- **Not** "more checks at the gate." v8.96 Theme A + v8.104 Fix E both added richer gate metadata without improving convergence. P1 moves verification pre-attest; fewer gate rounds, not more data per round.
- **Not** "ban all negative rules forever." P8 forbids enumerated-prohibition AS THE PRIMARY RULE. Short counter-examples are permitted after the positive form, for disambiguation. The lint catches orphan prohibitions, not every use of "not."
- **Not** "collapse minimal and showcase into one flow." Two first-class flows sharing atoms. P3 applies to any multi-codebase recipe (some minimals are dual-runtime), not only showcase.
- **Not** "replace `zerops_workflow` as state authority." P4 preserves it as-is.

---

## 12. Open principle-level questions (deferred)

1. **Does P3's SymbolContract live in `plan.Research` or as a separate computed artifact?** Step 3's atomic-layout.md places it inside research-step output; step 4 simulations may surface pressure to separate. Deferred to `atomic-layout.md` + step-4 verification.
2. **Does P5's expanded honesty check run at deploy-readmes completion or close-code-review completion?** Current honesty runs at deploy-complete after readmes. Expanding dimensions doesn't change the trigger; but the code-review brief reading the manifest is a second enforcement point. Deferred to `check-rewrite.md`.
3. **TodoWrite disposition under P4**: `check-off mirror only` per RESUME decision #3. Principle atom enforces that; whether the Go layer also refuses a full-rewrite attempt (structural enforcement) is a subsequent decision. Deferred.
