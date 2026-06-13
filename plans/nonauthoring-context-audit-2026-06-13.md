# Non-authoring context surface — audit + phased reduction

**Date:** 2026-06-13
**Status:** BANKED at Batch 3 (Karel's call, "C"). Shipped: Phase 0 + Batch 1 (deploy setup,
eval-proven) + Batch 2 (Plan examples, eval-proven) + Batch 3 (launch fields, deterministic+adversarial) +
the input-schema byte-budget ratchet. `zerops_workflow` 19094→17128 B; full `go test ./... -short` green, lint
clean. **Recommended next phase (NOT done): F2 — dynamic develop-guidance cross-call dedup** (§8b), the biggest
remaining win (per-turn re-dump >> one-time static trims) and cleanly eval-provable. The safe description-trim
well is otherwise ~exhausted (broadly-exercised descriptions proved mostly load-bearing; see §8a meta-finding).
**Codex review:** confirmed §3 SDK constraint + P0.1/P0.3 safety + git-push→Phase-1 deferral. Deltas folded below.
**Scope:** everything that flows into an end-user agent's context when `ZCP_AUTHORING` is OFF — the tool schemas advertised at `tools/list` (static, every session) and what each tool call returns (dynamic, per call).

---

## 1. Ground truth (measured)

In-memory MCP `tools/list` against the non-authoring server (mock platform, embedded knowledge), 19 tools:

| Surface | Bytes | ~Tokens |
|---|---|---|
| full `tools/list` JSON (static, per session) | 50,622 | ~12,655 |
| — descriptions | 8,240 | ~2,060 |
| — input-schemas | 39,558 | ~9,889 |
| `zerops_workflow` alone | 21,078 | ~5,270 (**42% of surface**) |
| server instructions (runtime-only) | ~209 | ~52 |

Dynamic side (per call, can exceed static over a session): one steady-state **develop** turn attaches **16 atoms ≈ 21 KB / ~5,250 tok** of guidance, re-synthesized on **every** `status` call with no cross-call dedup (`KnowledgeTracker` exists but is wired only into bootstrap).

Per-tool schema bytes (sorted): `zerops_workflow` 19,094 · `record_fact` 3,299 · `knowledge` 2,945 · `env` 2,484 · `deploy` 2,256 · `scale` 2,242 · `import` 1,910 · `preprocess` 811 (+786 desc) · rest <800.

## 2. Root cause (one pattern under ~90% of findings)

Violation of ZCP's own doctrine **"validation set ≠ presentation set."** Schemas and descriptions present the *whole validation set* (every field, every accepted value, every workflow's options, full rationale + UI walkthroughs) as the *presentation menu* for the agent's next decision. Maximal info instead of the one curated correct choice.

## 3. Hard constraint discovered (reshapes the audit's recommendations)

**The generated `zerops_workflow` schema sets `additionalProperties: false`** (top-level + nested objects; `jsonschema.For[WorkflowInput]`). The go-sdk validates tool-call **input** against the schema before unmarshal (the documented reason `patchFlexBoolProperty` exists — see `workflow.go:283-294`). Therefore:

> **Deleting any input field breaks backward compatibility** — an installed agent (or saved call / habit) passing the removed field now fails schema validation and the call is rejected before the handler runs.

This invalidates the audit's literal "delete `Variant`" / "remove launch-production fields" as *deletions*. The audit verifier that called Variant-deletion "safe" conflated Go `omitempty` (marshaling) with input-schema validation. **The safe lever is description shortening (field stays), not field removal.** Field-set narrowing per workflow (the bigger win) requires a deliberate schema-variant mechanism, not deletion.

**CONFIRMED by Codex** at the SDK level: ZCP uses the generic `mcp.AddTool` path, which runs `applySchema` (go-sdk `mcp/server.go:315`) and returns a validation-error result *before* the typed handler (line 340); `jsonschema.For` sets `additionalProperties:false` (`infer.go:245`) and unknown props trip `validate.go:509` "unexpected additional properties". (Nuance: the low-level `Server.AddTool` does NOT validate — but ZCP does not use it here.)

## 4. Verified findings (post-adversarial-verification severities)

| # | Finding | Sev | Kind | Lever |
|---|---|---|---|---|
| F1 | `zerops_workflow` = 42% of surface; ~16 launch-production-only fields (~1,500 tok schema) loaded for every bootstrap/develop user | high | bloat | shorten + narrow-via-discovery (Ph1/2) |
| F2 | Develop guidance re-dumped every `status` call (no cross-call surface-once); `KnowledgeTracker` not wired into `renderGuidance` | high | bloat | dynamic dedup (Ph2) |
| F3 | Field descriptions are essays duplicating atoms/CLAUDE.md (`Plan` 1.6 KB inline schema+examples, `launchKey` 725 B dup of atom, credential contracts) → second owner that drifts | high | drift | shorten (Ph1) |
| F4 | `closeMode` presents `git-push` as a co-equal value (4 prose sites incl. active guidance `router.go:184`), but it's retired → folds to `auto` | med | drift/forgotten | rewrite guidance (Ph1) |
| F5 | `Variant` field = "DEPRECATED + ignored", handler never reads it, ~63 tok every session | med | forgotten | **shorten desc (Ph0)** |
| F6 | Authoring-concept strings in end-user descriptions: `import` (recipe-authoring session) + `knowledge` (recipe-authoring runs / research pipeline, ×3) | med | leakage | import parenthetical (Ph0 cand.) / knowledge relocate (Ph1) |
| F7 | Essay schemas outside workflow: `record_fact` Type (723 B per-type rationale), `scale` (cross-field precedence repeated per field), `deploy` setup (923 B), `knowledge` (2 KB repeated mutual-exclusion) | high–med | bloat | shorten (Ph1) |
| F8 | `workflow` tool description 1,819 B with trigger phrases (incl. Czech) + duplicated action enum | high | bloat | shorten (Ph1) |
| F9 | `action` enum omits dispatchable `prod-ops`, `confirm-production` (drift the other way); enum-drift lint is one-directional (caught no stale value → git-push slipped) | low | drift | correctness (decide) |

**Clean (no fix):** authoring gate at tool-registration holds (no `zerops_recipe`/`zerops_port` leak, no validation-set `Enum` arrays leak); reference atoms pointer-rendered (lazy URI); envelope compute parallelized + corpus `sync.Once`-cached; `knowledge` mode-gating + URI safety boundary correct; `workflow=recipe` redirect is a spec-permitted rejection boundary, not leakage.

## 5. Risk tiering (against the "zero behavior + zero quality change" bar)

- **Phase 0 — provably zero behavior + zero quality (do now).** Only changes where the agent's *actionable* information is provably unchanged, or the touched thing is provably inert, or the change is a test-only guardrail.
- **Phase 1 — high-value, quality-risk (flow-eval-gated).** All description trimming of *live* fields, the git-push guidance rewrite, knowledge authoring-string relocation. Shortening a live description can degrade agent behavior; only flow-eval can confirm parity. Done incrementally behind the Phase-0 ratchet.
- **Phase 2 — dynamic dedup (design + eval).** Cross-call surface-once for develop guidance. Explicit behavior change (atoms stop re-appearing); must not break compaction recovery (a load-bearing invariant). Biggest win, biggest care.

## 6. Phase 0 — the safe subset (execute now)

**P0.1 — Shorten the `Variant` field description. ✅ SHIPPED.** Kept the field (deletion breaks compat per §3). Replaced the 226 B description with `"Deprecated; ignored, no effect."` (full rationale stays in the in-code comment, which does not ship to context). Provably quality-neutral: handler never reads `WorkflowInput.Variant` (grep + Codex confirmed; the `inputs.Variant` reader in `launch_readiness.go:211` is a DISTINCT type, `ops.bundle.Variant` int enum, set internally — not this MCP input field). Fixtures setting `variant:` stay valid (field still exists). `go test ./... -short` green.

**P0.2 — Input-schema byte-budget ratchet (test-only, zero runtime impact). ✅ SHIPPED** (`internal/tools/schema_byte_budget_test.go`, `TestInputSchemaByteBudget`). **Reshaped from the original "word budget" idea after measuring:** the existing `TestAnnotations_DescriptionWordCount` already caps the *tool-level* Description (60 words); the audit's real cost is the **per-field jsonschema descriptions inside the InputSchema** (essays / inline examples / UI walkthroughs) — the largest AND previously-unguarded surface. So the ratchet measures **marshaled InputSchema bytes per tool** (the exact static per-session cost the audit headlined) against a recorded ceiling; a schema may shrink freely, may not grow past its ceiling without a deliberate bump. New tool with no ceiling → fail (bloat caught day one); stale ceiling for an unregistered tool → fail (clean removals). **Codex variant nuance resolved structurally:** field jsonschema tags are compiled-in (variant-invariant); the only variant-sensitive schema is `zerops_deploy` (SSH > local), so the baseline is measured on the SSH/container-capable variant (`listAllTools(runtime.Info{})`, sshDeployer non-nil) whose per-tool bytes are ≥ local pointwise — one ceiling binds both modes. `zerops_browser` exempt (container+binary-gated, pinned by `TestAnnotations_BrowserTool`). Verified: green-by-construction (3× deterministic — schema marshal sorts map keys), fires on a +1-byte regression with an actionable trim message. Ceilings recorded post-P0.1/P0.3 — they ARE the Phase-1 target list (each trim lowers the number + the ceiling in the same commit).

**P0.3 — Remove the `import` tool-description authoring parenthetical. ✅ SHIPPED.** `import.go:82` "(an active recipe-authoring session also satisfies it)" removed. Gate behavior is code-owned by `RecipeSessionProbe.HasAnySession()` (`guard.go:45`, wired behind the authoring gate in `server.go:168`); removing the string changes no behavior. **Codex confirmed:** not a C1–C6 contract change (C1 is the probe *interface*, not this prose, per `spec-authoring-boundary.md:80`); authoring retains a separate `zerops_import` tell in `agents_authoring.md:18`; safe to ship without the authoring flow-eval. Promoted from "candidate" to firm Phase 0 per Codex §4. `go test ./... -short` green.

**Explicitly deferred from Phase 0:** git-push prose removal (F4 — 4 sites incl. active `router.go:184` guidance reshaping how push delivery is configured → quality-relevant → Phase 1); `knowledge` authoring-string relocation (F6 — guides the maintainer agent; relocation behind the gate needs authoring-flow re-verify); all live-field description trimming (F1/F3/F7/F8); dynamic dedup (F2).

Phase 0 token win is small (~−65 tok/session). Its purpose is correctness (clear retired cruft) + landing the **ratchet** that makes the large Phase-1/2 wins safe and measurable.

## 7. Open questions for Codex review

1. Does `modelcontextprotocol/go-sdk` enforce `additionalProperties:false` on tool-call **input** (rejecting unknown props before the handler)? If not, field deletion becomes available — but "shorten not delete" is safe either way.
2. Is the Variant-shorten + ratchet-lint Phase 0 correctly zero-risk? Any consumer of `WorkflowInput.Variant` beyond the (grep-confirmed inert) handler?
3. Is the git-push → Phase 1 downgrade right, or is there a minimal Phase-0-safe slice (e.g. drop only the `{appdev:git-push}` example in `workflow.go:61` while leaving the value listed)?
4. Is P0.3 (import parenthetical) safe to ship without the authoring flow-eval, given `import` is authoring-used?
5. Is the Phase-0/1/2 boundary sound for a strict "no behavior + no quality regression" bar?

## 7b. Verification protocol — how every Phase-1 change is PROVEN safe

Karel's bar (2026-06-13): a change ships only when we can PROVE it (a) keeps the
directly-affected behavior working, (b) breaks nothing in OTHER situations, (c)
is consistent across the WHOLE of ZCP. Five layers, the last two are exactly
those three requirements:

| Layer | Proves | Mechanism | Cost |
|---|---|---|---|
| A1 build + affected test tiers | no compile / contract break | `go test ./... -short` (tool + integration; e2e where relevant) | deterministic |
| A2 ratchet | the schema actually shrank, didn't grow elsewhere; ceiling lowered same-commit | `TestInputSchemaByteBudget` | deterministic |
| A3 single-owner read | the trimmed content still has an authoritative owner that delivers it | read the atom / structured-error / spec that now owns it | deterministic |
| **B1 consistency sweep** | **(c) consistent across ZCP** — no stale parallel copy; all sites of the concept agree | tree-wide grep for the old wording + parallel-path parity diff | deterministic |
| **B2 eval container** | **(a) directly works + (b) other situations** | flow-eval scenario(s) that EXERCISE the changed surface, on the real eval-zcp container, retrospective + transcript read | ~15 min/scenario, sequential |

Key constraint on B2: a flow-eval only exercises the surface its happy path
touches, so the scenario MUST exercise the changed field. The eval builds +
deploys the current WORKING TREE (`eval/scripts/build-deploy.sh`), so it tests
the uncommitted change directly. Pick scenarios across DIFFERENT flows for the
"other situations" half (req b), not just the one that motivated the change.

## 8. Phase 1 — IN PROGRESS

**Batch 1 — `zerops_deploy` setup field (F7 deploy-setup-1 + L5). ✅ PROVEN SAFE — all 5 layers green.**
Trimmed the ~920 B setup-field description in BOTH `deploy_ssh.go` AND
`deploy_local.go` (parallel paths) to one identical ~290 B rule. The removed
pedagogy (recipe dev/prod/worker naming, cross-deploy examples, convention
notes) has an authoritative owner: `deployPreFlight` returns the structured
`ErrRequiresSetupInput{AvailableSetups,…}` / free-text `Detail` that names the
available setups AND the recipe naming convention on ambiguity
(`deploy_preflight.go:128-149` — its own comment says the typed blocker carries
this "from prose"). Proof status:
- A1 ✅ `go test ./... -short` 27/27 green; A2 ✅ ratchet ceiling lowered
  2533→1908 (−625 B); A3 ✅ owner read + confirmed; B1 ✅ no stale prose
  tree-wide, the two deploy setup fields are now byte-identical (parity — they
  had drifted slightly before), atoms teach concrete `setup="prod"` cross-deploy
  consistent with the rule.
- B2 scenario 1 ✅ `cross-deploy-stage-promote-from-dev` (eval-zcp, suite
  20260613-122749): run **succeeded** (`subtype:success`, 13 turns — "appdev
  build cross-deployed to appstage, no rebuild, healthy HTTP 200, DB
  connected"). The deploy call was `{sourceService:appdev,
  targetService:appstage}` — **setup= correctly OMITTED**, and `zerops_verify`
  confirmed `classified HTTP runtime from deployed setup "prod"`: auto-resolution
  picked `prod` for the stage half by role, exactly as the trimmed field states
  ("the deploy resolves it"). No setup error, no extra round-trip — the agent
  did NOT need the removed recipe-pedagogy. The only friction was an UNRELATED
  pre-existing adopt-route issue (§ incidental below).
- B2 scenario 2 ✅ `greenfield-node-postgres-dev-stage` (eval-zcp, suite
  20260613-123822; different flow: greenfield classic bootstrap + dev/stage):
  run **succeeded** (`subtype:success`, 39 turns, both services healthy,
  session auto-closed). This exercised BOTH branches of the trimmed field: the
  dev first-deploy `{targetService:appdev}` **omitted setup** (auto-resolved),
  and the cross-deploy `{sourceService:appdev,targetService:appstage,setup:"prod"}`
  **passed setup="prod" EXPLICITLY** — both correct first-try, no setup error,
  no recovery round-trip. The self-review reports zero setup= friction (its only
  "setup" mention is the dev `run.start` idle, a dev_server topic).
- **Verdict: across two different flows (adopt+cross-deploy, greenfield
  bootstrap+dev/stage) the agent navigated BOTH setup= branches (omit→auto-resolve
  AND explicit pass) flawlessly using only the trimmed ~290 B field. (a) directly
  works ✅ (b) other situations ✅ (c) consistent across ZCP ✅. The removed
  pedagogy was provably unnecessary; its owner (structured preflight error) fires
  in ANY ambiguous-setup deploy regardless of flow, closing the residual gap.**

**Incidental finding (NOT this change — surfaced during B2, for backlog):** the
bootstrap **adopt** route's discover step has contradicting guidance priority —
step-level says "omit plan, pass scope=[...]" but when two adoptable runtimes
share a runtime base (`appdev` ubuntu/nodejs@22 + `appstage` alpine/nodejs@22 →
both nodejs@22) the scope-only call returns `INVALID_PARAMETER` with two
ready-to-paste `plan=[...]` templates. The agent recovered cleanly (one
round-trip) and the error response is excellent, but the dev/stage-same-stack
case should steer the agent to an explicit plan up front. This is the
`route=adopt` pairing logic (`InferServicePairing` / `ErrAdoptPairingChoice`),
entirely separate from the deploy-setup trim. Candidate for `plans/backlog/`.

**Batch 2 — `zerops_workflow` Plan field, inline Examples block (F4-adjacent / F3). ✅ A-layers + B1 green, eval running.**
Cut ONLY the inline `Examples: single dev container = […]; dev/stage pair = […]` block (~266 B) from the
Plan field; kept ALL structural rules (shape grammar, `bootstrapMode REQUIRED`, the nesting/hard-reject rule,
resolution enum, stageHostname rule, route=adopt/recipe rules). Owner of the examples: the bootstrap discover-step
atoms that FIRE in the same response BEFORE plan authoring — `bootstrap-classic-plan-dynamic.md:16-31` carries the
full nested standard example (+dependencies/resolution), `bootstrap-mode-prompt.md` names the modes + nested shape,
`bootstrap-adopt-discover.md` for adopt — all test-pinned by blessed golden renders. A1 ✅ (27/27) · A2 ✅
(ceiling 18900→18634) · A3 ✅ (atoms read + confirmed richer than the inline examples) · B1 ✅ (Plan field is
single-site, no parallel copy, no stale reference). B2 attempt 1 ⚠️ INCONCLUSIVE: `api-node-postgres-classic-dev` + `greenfield-node-postgres-dev-stage`
both **succeeded** (25/28 turns, apps healthy) BUT both agents chose `route="recipe"` and submitted
**plan:none** (accepted the recipe-derived shape) — so neither run exercised the classic plan-AUTHORING path
where the inline Examples are read. The runs prove the recipe/common paths are unaffected, but NOT the cut
surface. **New finding: the classic plan-authoring path is LOW-EXPOSURE** — a hello-world recipe exists for
nearly every runtime (37 in corpus incl. rust/go/node), so any single-runtime stack recipe-matches and omits
the authored plan. B2 attempt 2 ✅ DECISIVE — `greenfield-fullstack-multi-runtime` (eval-zcp suite 20260613-132055):
run **succeeded** (`subtype:success`, 36 turns, "all services up and verified, session auto-closed,
fullstack app live"). The agent took **route="classic"** (no recipe matches a multi-runtime fullstack) and
authored a complex nested multi-target plan **first-try** on a single `action=complete step=discover`:
`[{"runtime":{"devHostname":"appdev","stageHostname":"appstage","type":"nodejs@22","bootstrapMode":"standard"},
"dependencies":[{...,"resolution":"CREATE"}]},{"runtime":{"devHostname":"apidev",...,"bootstrapMode":"standard"},
"dependencies":[{...,"resolution":"SHARED"}]}]` — correct runtime-nesting, bootstrapMode, stageHostname,
resolution CREATE/SHARED. Exactly the dev/stage example shape that was REMOVED from the schema — authored from
the discover-step atom (`bootstrap-classic-plan-dynamic.md`, which the response body carried in full) instead.
Verified: only ONE complete/discover attempt (no flatten-reject + retry); the 2 "hard-reject" tokens in the
transcript are the atom's nesting-rule TEXT, not a rejection of the agent's plan.
B2 attempt 3 ✅ `weather-dashboard-classic-dev` (new scenario per Karel, suite 20260613-133636): run
**succeeded** (22 turns, dashboard live + healthy). The custom "weather dashboard" prompt did NOT recipe-match
(no hello-world fits a custom app) → **route="classic"**, and the agent authored a valid plan **first-try**:
`[{"runtime":{"devHostname":"weather","type":"nodejs@22","bootstrapMode":"simple"},"dependencies":[]}]` —
single runtime, **dependencies:[] (no DB added — persona's "no DB" respected)**, correct nesting, 1
complete/discover attempt. (Agent chose `simple` not `dev` — the "build me X that ends at a URL" default; a
valid mode choice, a third plan shape.) This confirms the boundary of the low-exposure finding: hello-world-
shaped requests recipe-match, but CUSTOM app requests go classic + author a plan — and that authoring works
fine without the inline examples.

**VERDICT: Batch 2 Plan-examples cut PROVEN SAFE (full 5-layer protocol).** (a) directly works ✅ — the classic
plan-AUTHORING path produced valid nested plans first-try in BOTH classic runs (standard dev/stage pairs +
single simple), authored from the discover-step atom owner, never the removed inline examples; (b) other
situations ✅ — 3 flows / 5 scenario runs (recipe×2, classic multi-runtime, classic single); (c) consistent
across ZCP ✅ — single-site field, owner atoms test-pinned, full suite green. The new scenario
`weather-dashboard-classic-dev` is kept as a permanent eval (it fills a real gap: custom-app classic-authoring,
which all other single-runtime scenarios miss by recipe-matching).

## 8a. Adversarial-vetting outcome + META-FINDING (reshapes Phase 1 scope)

A workflow ran adversarial cut/keep verification on the two broadly-exercised candidates. Result:

- **`zerops_knowledge` mutual-exclusion text — REFUTED, do NOT cut (load-bearing).** The 6× "Use alone —
  combining with X/Y/Z is rejected" repetition is NOT redundant: its only "owner" (the modeCount>1 rejection)
  is a REJECT-AND-RECOVER diagnostic that fires AFTER the agent mis-composes, and `describeKnowledgeModes`
  lists only the modes actually passed (a 2-item list), never the full forbidden set. The introducing commit
  (`knowledge.go:21-26`) says this text was added to "eliminate the v31 5-retries pattern." No forward owner
  exists (atoms carry zero mode-exclusivity rule). Cutting it would re-introduce the documented retry regression.
  **The adversarial pass caught this at the cheap deterministic layer — before any eval spend.**
- **`zerops_workflow` Plan field — mostly load-bearing.** Of ~8 segments, only 3 are owner-backed cuttable
  (Examples, route=adopt rule, stageHostname rule); the refutation OVERTURNED 2 analyzer cut-claims
  (route=recipe rule → owner-wrong; resolution enum → must-keep). Batch 2 takes only the safest (Examples).

**Meta-finding:** the broadly-exercised, eval-provable descriptions are mostly LOAD-BEARING — battle-tested
wording correlates with load-bearing-ness (it survived because removing it regressed something). The audit's
big raw numbers are NOT freely trimmable:
- The ~1,500-token **launch-production-only fields (F1)** can't be removed (strict `additionalProperties:false`
  ⇒ deletion breaks installed callers) and can't be hidden behind a narrowed published schema without either
  a compat break or loosening strict validation (losing FlexBool coercion). The big win is **compat-locked**
  unless a deliberate schema-variant mechanism is designed + eval-proven — a Phase-2-class effort, not a trim.
- The remaining safe trims are SMALL (Batch 1 deploy-setup ~625 B; Batch 2 Plan-examples ~266 B) and live in
  redundant-pedagogy / parallel-copy drift, not in the load-bearing core.
- Rarely-exercised fields (`record_fact`, `scale`) have plausible owners but LOW eval coverage (standard
  scenarios don't call them), so they can't be eval-proven under Karel's bar without bespoke scenarios.

Decision lever for Karel: keep harvesting small safe trims (each eval-proven, diminishing returns), OR pivot
to the schema-variant mechanism for the launch-only win (bigger, compat-constrained, Phase-2), OR declare the
safe description-trim ceiling reached and bank Phase 0 + Batches 1-2.

## 8c. (b) Launch-only fields — analysis DONE, eval BLOCKED (Karel decision needed)

Reframed: the launch-only win is a DESCRIPTION TRIM, not a schema-variant mechanism. Fields can't be deleted
(strict `additionalProperties:false` ⇒ compat break), but their ~5,475 B of descriptions (~1,369 tok) largely
restate the launch ATOMS that fire in the launch flow. Loosening to `additionalProperties:true` to drop the
fields entirely would recover only ~360 structural tok at the cost of strict validation (typo fields pass
silently) — NOT worth it; rejected.

**Adversarial cut/keep analysis (3 agents + refutation) DONE.** Net safe trim after honoring refutation
overturns + keeping security contracts ≈ **2,000–2,100 B (~500–525 tok)**. Key results:
- Trimmable (owner = launch atom, verbatim): launchKey −424 B (dashboard walkthrough + staging owned by
  `launch-mutation-key-required.md:17-27`), confirmFunctional −209, prodOperation −207, skipBuildIntegration
  −206, managedDeps −172, runtimeScaling −135, region −102, keepNonHa −84, etc.
- **Kept (load-bearing, refutation-confirmed): the token SECURITY CONTRACTS** ("never persisted to
  state/audit/response" — no atom owner, schema-only, pinned by `TestExistingProdToken_NeverInResponse`),
  structural positioning, scope-validation. Refutation OVERTURNED ~6 cuts (corePackage SERIOUS/LIGHT,
  prodSetupNameOverride, launchKey staged-fallback, existingProdToken dashboard, skipPipelineSetup, prodOperation
  token-read) → kept.

**BLOCKER — not eval-provable under the bar.** `launch-production-dev-only` (eval-zcp, suite 20260613-194427)
ran but **blocked at the source-control gate**: launch needs the source pushed to a real git repo + a PAT (the
dev-only fixture has no git remote), and the mutation needs an account-wide project-creation token (the eval's
key is project-scoped). The agent exercised only `region` + `productionProjectName` before halting to ask for a
GitHub URL + PAT. The launch path is structurally unreachable in the clean eval loop without real infra
(token + git repo + a real prod project to create/delete). So the ~500-tok trim has a STRONG deterministic +
adversarial owner-proof but NO behavioral confirmation — except `region` (the one field exercised pre-block).

**Decision for Karel** (the analysis is banked + reusable either way):
1. **Authorize a real-launch eval** — provide an account-wide project-creation token + a git-repo-backed
   standard-pair fixture; the eval does a real launch (creates + deletes a real prod project). Proves the full
   trim behaviorally. Cost: real prod-project churn + token handling + cleanup; Karel owns the token.
2. **Accept on deterministic + adversarial proof** — ship the ~500-tok trim WITHOUT behavioral eval (justified:
   the launch path is structurally eval-unreachable, the launch atoms are the dominant guidance owner, security
   contracts kept). A deliberate one-time relaxation of the eval bar for the unreachable path. Karel's bar to relax.
3. **Ship only `region`** (eval-covered, −102 B) + defer the rest.
4. **Defer the whole launch trim** — analysis stays in this doc for when launch eval infra exists.

## 8b. Phase 1/2 remaining sketch (not executed yet)

- **Ph1:** per-field description rewrites to TRIGGER+ACTION (≤~1–3 lines), defer walkthroughs/examples/enum-rationale to atoms; git-push guidance rewrite to the auto+git-push-setup model; relocate knowledge authoring strings behind the gate. Each batch behind a flow-eval (develop, launch, export) confirming parity, ratchet driven down per batch.
- **Ph2:** wire `KnowledgeTracker` (or a per-session seen-set) into develop `renderGuidance` for surface-once across turns, with an explicit carve-out so compaction recovery (`action="status"`) still re-emits the full envelope. Target the plan budget from `bootstrap-restore-2026-06-02` (≤~12 KB first call, ≤~4 KB subsequent).
- **Schema-variant mechanism (enables real F1 narrowing):** investigate publishing a workflow-narrowed input schema (or a discovery-time amendment) so launch-production fields don't load for bootstrap/develop — the only way to actually drop the ~1,500 launch-only tokens without a compat break.
