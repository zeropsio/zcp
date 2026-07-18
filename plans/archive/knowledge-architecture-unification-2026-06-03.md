# Knowledge Architecture Unification — phased migration

**Target spec:** `docs/spec-knowledge-architecture.md` (one fact → one owner → computed/pointed delivery).
**Trigger:** the P0c develop-guidance work surfaced that `develop-atom` duplicates `zerops_knowledge uri=`, which exposed a systemic fact-ownership crisis (discovery 2026-06-03: 127 facts, 101 duplicated, 6 conflicting, ~10 channels, zero governance).
**Status:** DESIGN — decision-ready. No execution past Phase 1 until Karel steers the spec.

---

## 0. Scale + posture (read first)

This is a **multi-phase program**, not a single PR. Phases 0–3 are tractable + high-value (the fundament + the acute drift) and should land soon. Phases 4–7 are **large + incremental** — each domain de-duped under the protection of the Phase-2 drift lint, over time, coordinated with the guide-optimality plan. The durable deliverable is the **spec + the governance (Phase 2)**; everything after is safe, gated, incremental cleanup.

**Ordering rationale:** Phase 2 (governance) comes BEFORE the bulk de-dup so that every later consolidation is drift-protected — you cannot safely remove a duplicate until a lint guarantees the survivor is the single owner. Doing bulk de-dup first would just re-fragment.

**Relationship to the in-flight P0c branch (`feat/p0c-develop-guidance-trim`, NOT pushed):** its cross-ref contract IS Phase 0; its `develop-atom` is replaced by Phase 1. So Phase 1 is applied to that branch before it merges; Phases 2+ build on top.

---

## Phase 0 — Cross-reference contract  ✅ DONE (on the P0c branch)

The `references-atoms` (content-dep → inline) / `pointer-atoms` (depth → deferred) distinction + `atom_crossref_contract_test.go` lint. Codex verdict: **fundament, not mask.** This is the atom-tier instance of spec §3.3. Keep as-is. (Eventually fold the lint into the unified atom-lint suite per the no-proliferate-per-topic-tests rule — hygiene, not blocking.)

---

## Phase 1 — Unify the pull retrieval (develop-atom → zerops_knowledge uri=)  ✅ DONE (2026-06-03)

**Shipped:** tool-layer adapter (`internal/tools/knowledge.go::resolveAtomURI`) resolves
`zerops://atoms/<id>` via `workflow.LookupReferenceAtomBody` — reference:true ONLY, inline
atoms rejected (placeholder-leak guard). `referenceStub()` now emits
`zerops_knowledge uri="zerops://atoms/<id>"`. `handleDevelopAtom` + the `develop-atom`
action + its `AcceptedWorkflowActions` entry deleted. `pointer-atoms` resolvability lint
strengthened to full-axis (`atomCoRendersWhenever` — was phase+envelopeDeployStates only)
with the canonical-URI escape hatch. `workflow_develop_atom_test.go` → `knowledge_atom_uri_test.go`.
spec-knowledge-distribution.md §4.6 (delivery=inline|pointer) added.

**Verified:** tools+content+workflow + full `go test ./... -short` 0 failures; full
golangci-lint 0 issues; goldens regenerated (only the 5 stub lines changed, 14 lines total);
teeth: `TestAtomCoRendersWhenever_Teeth` + `TestPointerResolvable_URIEscapeHatch` + an
end-to-end injected narrowing-axis (corpus lint fired, reverted clean). flow-eval
greenfield-node-postgres-dev-stage: **clean run, 0 `develop-atom` calls, new `zerops://atoms/`
URI form rendered in stubs, 0 fetch/placeholder errors.** classic-rust-postgres-standard
flow-eval (2nd gate): clean — classic-route authoring ran smooth, no retrieval friction,
env-var wiring authored correctly (confirms the surviving core.md dedup + mode-tighten don't
regress).

**Goal:** one fetch-by-key operation (spec §4, I3). Removes the bespoke second retrieval API.

**Steps:**
1. Tool-layer adapter: `zerops_knowledge uri="zerops://atoms/<id>"` resolves against the atom corpus when the URI is in the `atoms/` namespace; all other URIs go to the knowledge `Store`. Adapter lives at the tool boundary (`internal/tools/knowledge.go`) so `internal/knowledge` gains no dep on `internal/workflow`. **CRITICAL: the adapter resolves ONLY `reference:true` atoms** — inline atoms carry `{hostname}`/`{stage-hostname}` placeholders substituted at synthesis; fetching them raw would leak unsubstituted tokens. Reject non-reference atom URIs with a clear error.
2. `referenceStub()` emits `zerops_knowledge uri="zerops://atoms/<id>"` instead of `zerops_workflow action="develop-atom" atomId=<id>`.
3. Update the `pointer-atoms` contract: retarget "resolvable" to the canonical URI; keep substitution-safety. **Strengthen the resolvability lint** — today it checks phase + envelopeDeployStates only; either check full axis compatibility (the target co-renders under ALL the source's axes) OR require the explicit canonical URI in the source body. (The current weak check could pass a pointer whose target never co-renders.)
4. Delete `handleDevelopAtom` + the `develop-atom` action from the schema + valid-actions + `content.AcceptedWorkflowActions`. Replace `workflow_develop_atom_test.go` with a `zerops_knowledge uri=` test proving every reference atom fetches + that a non-reference atom URI is rejected.
5. Update `spec-knowledge-distribution.md`: `delivery=inline|pointer`; pointer bodies fetched through the knowledge URI. (Also fix its own `zsc noop` drift — see Phase 2a.)

**Gate:** tools+content+workflow tests green; goldens (stub text changes); flow-eval (greenfield + a classic authoring scenario) confirms agents fetch via the new URI when they need depth.
**Risk:** low. `develop-atom` is unreleased (only on the P0c branch). Behavior-equivalent retrieval, different surface.
**Effort:** ~½ day.

---

## Phase 2a — Seed governance + acute drift fixes (FUSED — the fundament)

> **⚠️ FIRST-MOVE ABANDONED — the `dev-dynamic-run-start` fact is an UNRESOLVED platform conflict, not a settled drift (2026-06-03, verified against the live platform + authoritative docs).**
> The plan's first move assumed "OMIT `run.start`" was canonical and the specs had drifted by
> saying `zsc noop`. **Wrong — the opposite is true.** Verified:
> - **Authoritative platform docs** (`../zerops-docs/.../guides/zerops-yaml-advanced.mdx`):
>   *"Dev services use `start: zsc noop --silent` — the agent controls server lifecycle via SSH."*
>   `zsc.mdx`: `zsc noop` = "keep a container alive… as a start command for services that don't
>   have a natural blocking command."
> - **Live dev container** (`appdev` on eval-zcp): runs `zsc noop --silent` (PID alive); the classic
>   develop/bootstrap flow agent **authored** `start: zsc noop --silent` into its dev block.
> - So **`zsc noop` is the platform-true dev-start mechanism.** The "OMIT `run.start`" line is an
>   INTERNAL ZCP convention living in `gate_dev_runtime_no_run_start.go` (recipe-engine, run-52) +
>   the **synced guide copy** `internal/knowledge/guides/zerops-yaml-advanced.md` (which DISAGREES
>   with its own upstream `.mdx`) + the `develop-dynamic-runtime-start-container` atom. It diverged
>   from the platform; whether omitting run.start even keeps a dev container alive is unverified.
> - **My edits "fixing" the specs to OMIT were therefore WRONG and are fully REVERTED** (the specs
>   correctly say `zsc noop` again; the `dev-dynamic-run-start` tripwire fact was removed).
> - **This is a genuine ownership conflict ZCP must reconcile — NOT mine to pick a side.** Surfaced
>   in `## Open items for Karel` + spec-knowledge-architecture §1 (the `zsc noop` row). The platform
>   is the source of truth (spec §2), so the internal "omit" convention is the side that must be
>   reconciled — but that touches the recipe gate + a synced guide + an atom + possibly the recipe
>   app-repos, so it needs the Karel/Aleš decision before any edit.
> - acute-fix **#3 (build.base) NOT a real conflict** — scaffold atom curates correctly; downgraded.
> - acute-fix **#5 (reload-vs-restart) — no clear dup; deferred to P5/P7.**
>
> **Shipped (ONLY platform-neutral, mechanism/dedup work — verified):**
> core.md env-vars "Three Levels" section verbatim-dup deleted (22 lines, content preserved once,
> no fact asserted — kept the copy under Provision Rules); tripwire registry
> `internal/content/fact_owners.go` (`SingleOwnerFact` + `SingleOwnerFacts`) + lint
> `TestSingleOwnerFactsNoDrift` seeded with **the ONE unambiguous fact** (`env-vars-three-levels-section`
> count==1) + teeth test `TestSingleOwnerFactsLintHasTeeth` + end-to-end injection (fired, reverted).
> `go test ./... -short` 0 failures; `make lint-local` clean; both flow-evals clean.
>
> **Also REVERTED (2nd instance of the same error class — caught in audit):** the core.md `mode` L19
> "MANAGED ONLY (NEVER on runtimes)" tighten. The zerops docs (`import.mdx`) show `mode: HA` on
> RUNTIME services (`nodejs@latest`, `php-nginx@8.4` examples) — the platform ACCEPTS mode on
> runtimes. ZCP's core.md L116 ("NEVER for runtimes") + model.md ("runtimes forced HA") already
> diverge from that; my edit amplified the divergence. Reverted to the original (softer, managed-
> scoped-default) comment. **Flagged as discrepancy #2 below — not unilaterally reconciled.**

Codex correction: do NOT build an abstract 127-fact registry before fixing the acute drift. **Fuse** the narrow tripwire lint with the conflict fixes — prove the governance model on the single ugliest drift first, then fix the rest under it.

**~~HIGHEST-LEVERAGE FIRST MOVE~~ — ABANDONED (see the box above).** The proposed
`dev-dynamic-run-start-omitted` first move was based on a wrong fact model: `zsc noop` is the
platform-authoritative dev-start mechanism, not a retired one. The governance mechanism was instead
proven on the unambiguous `env-vars-three-levels-section` verbatim-dup fact. The dev-dynamic conflict
is escalated to Karel/Aleš, not swept.

This single fact proves: the registry shape works, the lint has teeth, and the multi-channel fix protocol (incl. synced recipes + specs) is real.

**Then the tripwire registry + the rest of the acute fixes:**
1. **Tripwire registry** (`internal/content/factowners` or similar) — high-value entries only: `{fact-id, canonical owner (file/symbol), forbidden-duplicate fingerprint(s), allowed reference surfaces}`. Seed: `dev-dynamic-run-start`, env-vars-section, setup-naming, failure-phase-taxonomy. NOT all 127.
2. **Drift lint test**: each registered fingerprint appears only in its owner; cross-source grep + AST for code-owned. Teeth test (catches each when re-introduced).
3. Route code-owned facts through single Go owners where not already: failure-phase taxonomy (one enum, `ops`+`tools` derive), recovery-hint mapping (one registry: check-id → Recovery), adoption-state enum, setup-naming. (`topology.FailureClass` exists — extend the pattern.)
4. **The acute fixes** (each pinned by a registry entry):

| # | fact | fix |
|---|---|---|
| 1 | `core.md` env-vars section duplicated verbatim (L141-161 == L216-236) | delete one copy; single section |
| 2 | `zsc noop` across 42 recipe files (`.md` + `.import.yml`) + specs + guide | the FIRST MOVE above; owner = gate error + `zerops-yaml-advanced` guide |
| 3 | `build.base` multi-base vs scaffold-yaml "runtime-only key" | fix the atom (the line edited in P0c) to match core.md (multi-base valid; build base ≠ run base) |
| 4 | `mode` HA/NON_HA unscoped in core.md schema (L19) | add "managed-services only" to the schema comment |
| 5 | reload-vs-restart / setup-naming intra-tool dup | dedupe within the tool; cite the owner |
| (downgraded) | object-storage region wording | NOT a true conflict (both true). `services.md` owns; `operations.md` references — handled in Phase 5 cross-tier dedup, not acute |

**Gate:** the lint catches each seeded fact when re-introduced (teeth); flow-eval unaffected (reference content, not spine).
**Risk:** medium — registry shape; kept narrow (tripwire, not catalog). #2 touches synced recipes + specs → coordinate with recipe owner.
**Effort:** ~2–3 days incl. the first-move multi-channel fix.

---

## Phase 4 — PULL-tier internal de-dup (= guide-optimality plan)

**Goal:** themes ↔ guides ↔ decisions internal de-dup. **This is `plans/guide-llm-optimality-2026-06-02.md`** — it independently reached the same conclusion ("themes own facts; cut U-content from guides; 0 atom promotions"). Subsume it as this phase of the PULL slice; run it under the Phase-2 drift lint.
**Gate / risk / effort:** per that plan (P1-P4, ~half the guide line-count, sync-rename mechanics the main risk).

---

## Phase 5 — Cross-tier fact de-dup (atoms ↔ knowledge)

**Goal:** the 101 duplicated facts → single owner; atoms carry the state-tactical slice + a pointer, knowledge owns the reference fact (spec §3.1). Per the tally: ~57 → themes, ~21 → atoms, ~16 → guides, ~13 → core.md.
**Mechanism:** domain by domain (deploy / env / build-vs-runtime / lifecycle / managed-services / yaml-structure), under the drift lint. An atom that restates a knowledge-owned fact is trimmed to its tactical slice + a `pointer-atoms` / cite.
**Gate:** drift lint per domain; flow-eval per domain (the agent still authors correctly with the trimmed atom + pull pointer — same bar as P0c).
**Risk:** medium — this is where "lean atom + correct authoring" is re-validated per domain; back off if a trim breaks authoring (the P0c lesson).
**Effort:** large, incremental — one domain per cycle.

---

## Phase 6 — Tool-schema OWNER EXTRACTION (not broad de-dup)

Codex correction: tool-schema prose is **load-bearing control surface** (it drives tool/param selection) — "dedupe 115 descriptions" is a rabbit hole. Reframe narrowly:
**Goal:** derive the *enumerable* + *high-risk* facts from their owners — enum/action lists (deploy modes, adoption states, failure phases) and platform invariants (setup naming, env precedence) → generated-from or compile-cited against the Phase-2a registries. **LEAVE parameter-contract prose + examples intact** (that IS the schema's job + owned content). Facts that today live ONLY in schemas (L7 propagation, deferred-start) get a real owner first, then the schema cites.
**Gate:** drift lint on the extracted facts; descriptions still parse; agent still selects params correctly (annotations tests + a flow-eval).
**Risk:** medium — trim FACTS, never the contract. **Effort:** medium, targeted (not all 115).

---

## Phase 7 — Co-locate failure-guidance strings (NOT a generic service yet)

Codex correction: a generic `(phase,category,context)→guidance` service is gold-plating before the shape is proven.
**Goal (incremental):** co-locate the duplicate deploy-failure `SuggestedAction`/`NextActions`/recovery strings INTO the existing failure-signal registry (`ops/deploy_failure_signals.go` + `topology.FailureClass`), so the response builders + atoms cite one owner instead of re-authoring. Build a generic composed-guidance service ONLY after 2–3 domains prove the same shape.
**Gate:** drift lint; deploy-failure classifier tests; flow-eval on a failure-recovery scenario.
**Risk:** medium — touches the deploy-failure path. **Effort:** medium.

---

## Don't-touch (per spec §6)

Recipe-authoring atoms + `refinement-references`; workspace plumbing (`.mcp.json`/`.claude.json`/SSH/VS Code); REFLOG (historical); the live `schema.Cache`; and the store split itself (no `internal/knowledge` ↔ `internal/content/atoms` merge). The recipe corpus (Phase 3 #2, Phase 4) is **coordinate-with-recipe-owner**, not unilateral.

---

## Sequencing summary

```
Phase 0 (done) → Phase 1 (retrieval unify, ½d) → Phase 2a (FIRST MOVE: dev-dynamic-run-start across all surfaces,
   then seed tripwire lint + acute fixes, ~2-3d)
   → then incremental UNDER the drift lint: Phase 4 (guide-optimality) ∥ Phase 5 (atoms↔knowledge per-domain)
   → Phase 6 (tool-schema owner-extraction) → Phase 7 (failure-guidance co-location)
```

Phases 0-2a = the fundament + acute drift (do soon, ~3-4 days). Phases 4-7 = the long incremental cleanup (drift-protected, per-domain, over time). Nothing pushed until a coherent slice is complete + tested + reviewed (Karel's standing rule).

## Resolved + open items for Karel

1. **`dev-dynamic-run-start` — RESOLVED 2026-06-03 (you confirmed `zsc noop`; fixed everywhere).**
   - **Platform-authoritative = `start: zsc noop --silent`**: public docs (`zerops-yaml-advanced.mdx`),
     `zsc.mdx`, the live `appdev` container, the recipes, and you.
   - **Origin of the bug:** commit `cdbcc0da` (Aleš, 2026-05-20, run-49) + `989faf91` (run-52) "retired
     zsc noop; omit run.start" on the premise "the runtime stays alive without a start command" —
     contradicted by the docs + recipes + live. It spread to ~20 surfaces.
   - **Fixed:** restated `zsc noop --silent` across atoms (4), `core.md` (3), guide, recipe briefs (8),
     recipe workflow content (2), tool/topology docstrings; **deleted** the wrong-enforcing
     `gate_dev_runtime_no_run_start` gate (+ test + registration); kept `gateWorkerDevServerStarted`
     (correct — requires the agent to start the dev server). **This reverts Aleš's deliberate
     "uniform omit" design — cross-owner; you authorized it, but loop Aleš if he had a reason.**
   - **Backward-compat:** the develop/dev_server flow works whether a dev block has `zsc noop` or
     (historically) omits — the agent starts the server either way; `deploy_validate`'s empty-run.start
     warning now correctly nudges a wrongly-omitted dev block toward `zsc noop`.
   - **Optional follow-up (Aleš's engine, deferred):** a new "require `zsc noop` on dynamic dev" gate
     to replace the deleted wrong one — YAGNI for now (briefs teach it; goldens pin the atom text).
2. **DISCREPANCY #2 — `mode` (HA/NON_HA) scoping: ZCP says managed-only, platform docs show it on runtimes.**
   - **Platform** (`import.mdx`): `mode: HA` examples on `nodejs@latest` + `php-nginx@8.4` RUNTIME services
     ("create a `nodejs@latest` service… in `HA` mode"). The platform accepts mode on runtimes (for
     vertical autoscaling / HA).
   - **ZCP** (`core.md` L116 + `model.md`): "NEVER set mode for runtime services… mode is only for
     managed services" / "runtimes forced to HA regardless of input."
   - **Largely resolved by the BE base-string model (2026-06 info):** the base-string `:mod`
     (`:ha`/`:single`) is a managed-service-only decoration — runtimes carry an OS-prefix instead,
     never a `:mod`. So "mode is for managed services" is correct *in the base-string identity sense*,
     and ZCP's core.md is aligned there. The residual nuance is only the legacy import.yaml
     `mode: HA|NON_HA` SCALING field, which the docs show the platform also accepting on runtimes
     (`nodejs`, `php-nginx` examples) — distinct concept. I reverted my over-strong "MANAGED ONLY
     (NEVER on runtimes)" edit; the original softer comment stands. Low urgency, no live breakage.
3. **Recipe corpus + app-repos** (subsumed by #1): 42 recipe files carry `zsc noop --silent`; the live
   `zerops-recipe-apps/*` dev blocks likely do too. If "omit" wins, that's an Aleš migration. If `zsc
   noop` wins, they're already correct. Either way — Aleš's synced scope, not touched.
4. **`zsc noop` as a dev BUILD command is a separate, legitimate fact** — `deploy_validate.go::hasZscNoop`
   endorses it for dev (only warns for stage). Independent of the run.start question above; left alone.

## P4 / P6 / P7 / P5 — execution outcome (2026-06-03)

**P4 (guide-optimality) — DONE + flow-eval-verified.** Reordered to land theme facts FIRST (destinations) so guide cuts were fact-safe. Theme facts verified vs `../zerops-docs` (caught a fabricated fact, see #1 below). 14 guides + 5 decisions cut (conservative, citation-backed, **zero fact loss**) + 3 tool-leak reshapes (portable-mechanism-first) + 2 new guides created (`readiness-health-checks`, `shared-storage-integration`) consolidating triplicated/dense content + 3 source guides repointed. **Key empirical finding: the guide corpus is far leaner than the plan assumed — only ~225 safe lines existed to cut once no-fact-loss was enforced, not "half." Most guide content is genuine topic-depth.** Gate: `api-node-postgres-classic-dev` flow-eval CLEAN — agent authored env-var `${db_*}` wiring + zerops.yaml + postgres correctly, zero friction traceable to the cuts. The `zerops-yaml-advanced → zerops-yaml-run-features` RENAME is deliberately **deferred** (gate-Q3: additive `sync pull` leaves the old upstream file — the one risky sync-mechanic move; do it with the upstream-cleanup flow, not blind).

**P6 (tool-schema owner-extraction) — DONE as a drift-lint.** Codex's constraint: struct tags can't interpolate constants, and there's NO current drift (values are correctly duplicated). So the lowest-risk realization of tell==check is a pinning test, NOT a schema-gen change to a published surface. `TestToolSchemaEnumsPinnedToOwners` + `TestSubdomainSchemaModesPinnedToPredicate` (+ teeth) pin 8 enum surfaces to their owners: closeMode→`validCloseModes`, buildIntegration→`validBuildIntegrations`, bootstrapMode→`workflow.ValidBootstrapModes()` (new helper), envClassification→`topology.SecretClassificationValues()` (new helper), fact type/scope/routeTo→`ops.Known*()` (new helpers), subdomain modes→`modeAllowsSubdomain`. A new owner value can no longer go unsurfaced in the schema prose. **Finding 7 (setup-naming dev/prod/worker) DEFERRED — needs an owner first** (no Go symbol; closest is `core.md:137` theme prose / a `fact_owners` entry). Backlog candidate.

> **UPDATE 2026-06-03 (later): Karel greenlit "finish everything" — P7-full + P5 + the 2 flow-eval frictions are now DONE + verified. See the "FINAL execution" block at the bottom. The paragraphs below are the pre-greenlight state, kept for provenance.**

**P7 (failure-guidance co-location) — increment done + fuller re-sourcing FLAGGED.** Verdict after tracing it: the deploy-failure guidance is **already single-owned where it matters** — `ops.ClassifyDeployFailure` is the primary owner the agent reads FIRST (per the CLAUDE.md `failureClassification` invariant), and `TestDeployFailedResponseFields_NoBuildLogsContradiction` already guards the legacy `Suggestion`/`NextActions` fallback against contradicting it. Codex's "duplication" is a consistency-tested defensive fallback, not a live drift bug. The high-value increment landed: **enriched the classifier's `PhaseInit` baseline** (the owner string the agent reads first) with the live-verified initCommands-gating fact (DEPLOY_FAILED / not-activated / diagnose via appVersion.status not service.status / the config:cache trap). **FLAGGED for Karel (published-surface, his call):** Codex finding 1's fuller re-sourcing (deploy_poll sources `Suggestion`/`NextActions` from the classifier, delete the next_actions phase strings) reshapes the agent-facing deploy-failure RESPONSE and turns the next_actions phase branches into dead-for-known-phases code — published-surface change for marginal gain (no live bug). Findings 2 (events.go hint) + 3 (git-push atom category-tables, which Codex itself flagged as legit walkthrough context) likewise flagged, low priority.

**P5 (cross-tier atom↔knowledge dedup) — FLAGGED as the per-domain incremental tail (NOT a silent cut).** The plan itself scopes P5 as "large, incremental — one domain per cycle" with "back off if a trim breaks authoring." It is platform-fact-sensitive (every atom-trim must verify the surviving fact against the live platform + confirm the agent still authors correctly via a per-domain flow-eval). Doing it rushed at the tail of a marathon session is exactly when the "back off if uncertain" rule bites. Ready per-domain plan: deploy / env / build-vs-runtime / lifecycle / managed-services / yaml-structure — for each, find atoms restating a theme/guide-owned fact, trim to the tactical slice + pointer, register the survivor in `fact_owners.go`, gate on a per-domain flow-eval. Recommend Karel greenlight one domain at a time.

## FINAL execution — full program complete (2026-06-03, post-greenlight)

Karel greenlit finishing everything. Outcome:

- **P7-full** — `deploy_poll` now sources `Suggestion`/`NextActions` from the classifier owner (`ClassifyDeployFailure`); the classifier's `PhaseInit`/`PhasePrepare` baselines were enriched (incl. the live-verified initCommands-gating fact + the appVersion-vs-service-status diagnostic) so no guidance downgraded; the hand-authored `next_actions.go` phase strings are reduced to the no-classification fallback (CANCELED/unknown); tests follow the owner. Single owner, response field shape preserved.
- **P5** — read-only analysis across all 6 domains (deploy/env/build-vs-runtime/lifecycle/managed/yaml-structure). **5/6 returned zero safe trims** — the apparent atom↔knowledge duplication is the by-design push (tactical slice) vs pull (reference depth) split, which the Information-Contract principles say to KEEP. One narrow intra-atom trim applied: `develop-first-deploy-scaffold-yaml` cross-deploy elaboration → tactical slice (the depth is owned by the co-pushed sibling `develop-deploy-modes` + reader-visible core.md §Deploy Semantics). Empirically confirms (like P4 for guides) the "101 duplicated facts" overcounted what's safely dedup-able. Goldens regenerated.
- **Friction 1** (adoptable false-positive mid-bootstrap) — FIXED: `workflow.InFlightBootstrapHostnames` + discover classifies a service planned by an alive bootstrap session as `resumable`, not adoptable. **Flow-eval-verified**: the false warning is gone.
- **Friction 2** (http_root 404 → degraded gated auto-close) — FIXED: `ops.VerifyResult.PassedForLifecycle()` decouples the auto-close gate from a cosmetic http_root 4xx (5xx/conn-error/service_running/other-fail still block); the verify RESPONSE stays honestly degraded. **Flow-eval-verified**: agent saw degraded, recognized cosmetic, auto-close fired WITHOUT a throwaway root route.

All green: `go test ./... -short`, `make lint-local` (0 issues, atom gates). Complete state deployed to the zcp host.

**Only remaining: the SYNC + rename — gated on Karel's review (his standing rule) + an irreversible PUBLIC publish.** `sync push guides` pushes the ~17 guide rewrites + 2 new guides to the PUBLIC `zeropsio/docs` as PR(s). The `zerops-yaml-advanced → zerops-yaml-run-features` rename CANNOT be done by `sync push` alone (additive `sync pull` re-creates the old file → duplicate) — it needs a manual upstream `.mdx` deletion in the same zerops-docs PR. Public-reader fact-preservation: theme-landed facts were verified to exist in zerops-docs canonical pages (features/pipeline.mdx, postgresql/how-to/scale.mdx, references/networking/public-access.mdx, etc.), so cuts don't orphan facts from llms-full.txt — but this warrants a `--dry-run` preview before the public push. Recommend: preview → Karel approves → push + manual rename PR.

## Platform-fact discrepancies surfaced during P4 verification (2026-06-03)

Every theme-edit candidate was verified against `../zerops-docs` + (where it mattered) the live platform before landing. The verification caught real divergences:

1. **`initCommands` failure semantics — LIVE-VERIFIED, docs are WRONG.** The public docs (14 build-pipeline pages) claim initCommands are *best-effort* ("deploy is NOT canceled, app starts regardless"). A live E2E on eval-zcp (probe `nodejs@22`, `initCommands: [echo … && exit 1]` + a real `start`) proved the OPPOSITE: a non-zero init exit **aborts the deploy** — new appVersion → `DEPLOY_FAILED`, `activationDate: null`, never activated; `start` never runs; the previous version keeps serving; the runtime log DOES emit `RUN.INIT COMMANDS FINISHED WITH ERROR` (not invented), and `stack.build` carries `commandExec` / "init command failed" + the failed command + exit code. **Nuance landed in `deployment-lifecycle.md`:** diagnose via `appVersion.status`/`activationDate`, NOT `service.status` (which stays `ACTIVE` on the old version). → ZCP's existing guide fact is CORRECT; **the public Zerops docs should be flagged upstream.** This is the deepest form of spec §2: the LIVE platform outranks even the docs.

2. **Shared-storage `run.mount` — UNDER LIVE VERIFICATION.** ZCP's `services.md` shared-storage card asserts a two-step mount (import `mount:` + zerops.yaml `run.mount`). Both the live `zerops_yml_schema.json` (no `mount` field) and the public spec omit `run.mount`; only import.yaml `mount:` is documented. A platform-verifier run is resolving whether `run.mount` is real. The new `shared-storage-integration` guide was authored conservatively (import `mount:` + `connect-storage`, both verified) pending the verdict; the card may need a fix.

3. **Scaling absolute ceilings — theme-vs-docs mismatch (flagged, not resolved).** Docs (`guides/scaling.mdx`) give vertical ranges CPU 1-8 / RAM 0.125-48 GB / disk 1-250 GB. `operations.md` gives RAM "max 32 GB" / disk "max 128 GB" (step-maxes) + "Max 40 cores" elsewhere. The 48 GB / 250 GB absolute ceilings are NOT theme-owned, so the `scaling.md` Resource-Limits cut was BLOCKED (kept the ceiling row). Needs a live check / reconcile before that block can be cut.

## Theme-edit ledger (§3) — verified outcome (2026-06-03)

| Fact | Verdict | Landed |
|---|---|---|
| build-container envelope (CPU1-5/RAM8GB/disk1-100/60min) | confirmed | `model.md` Build/Deploy Lifecycle |
| PG + MariaDB RAM-min 0.25 GB | confirmed | `services.md` PG + MariaDB cards |
| PG `pg_stat_statements` superuser + restart | nuanced (dropped the fabricated `shared_preload_libraries` clause — not a Zerops surface) | `services.md` PG card |
| direct TCP/UDP port access (L3 balancer, runtime+PG only, IPv6-free/IPv4-paid, GUI not `ports[].protocol`) | nuanced-corrected | `operations.md` Direct Port Access |
| per-port firewall blacklist/whitelist (direct-port only, distinct from nftables) | nuanced-corrected | `operations.md` Direct Port Access |
| NATS storage (40GB mem/250GB file) + `/healthz` on 8222 | confirmed (orphan flagged by P4-P1) | `services.md` NATS card |
| `initCommands`-abort | refuted-by-docs but CONFIRMED-by-live (see #1) | stays in `deployment-lifecycle.md` (NOT duplicated to core.md) |

## Resolved (by "spustíme to celé" greenlight, 2026-06-03)

- **Scope of "complete":** doing 0 → 1 → 2a (fundament) then 4-7 incrementally under the drift lint — confirmed.
- **Tripwire shape:** built as `SingleOwnerFact` (owner / forbidden-fingerprint / scope-files / max-matches / allowed-lines), seeded with 2 verified-drifted facts (not 4 speculative) — grows as real drift is found.
- **Spec editing:** approved; the 3 operational specs fixed.
- **P0c branch:** Phase 1 applied to `feat/p0c-develop-guidance-trim`; whole branch held until the slice is complete + reviewed (nothing pushed/merged).
