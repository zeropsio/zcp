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

## Phase 1 — Unify the pull retrieval (develop-atom → zerops_knowledge uri=)

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

Codex correction: do NOT build an abstract 127-fact registry before fixing the acute drift. **Fuse** the narrow tripwire lint with the conflict fixes — prove the governance model on the single ugliest drift first, then fix the rest under it.

**HIGHEST-LEVERAGE FIRST MOVE** (do this one end-to-end before anything else in this phase):
**`dev-dynamic-run-start-omitted`** — the dev-mode dynamic runtime convention (`run.start` omitted, NOT `zsc noop`). It has drifted across the MOST surfaces. Create one tripwire-registry entry + fix every surface it catches:
- active specs (`spec-workflows.md`, `spec-content-surfaces.md`, `spec-knowledge-distribution.md` still say `zsc noop`)
- guide (`zerops-yaml-advanced.md`) + the gate error message (the enforcement owner)
- **42 recipe files** (`32 .md + 10 .import.yml`) still carrying `zsc noop` → coordinate with recipe owner
- any tool-schema / response prose mentioning it

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

## Open decisions for Karel

1. **Scope of "complete":** do Phases 0 → 1 → 2a now (fundament + acute drift + the P0c correction) as the first shippable slice, then 4-7 incrementally under the drift lint? Or a different cut?
2. **The tripwire-registry shape** (Phase 2a) — high-value-only `{owner, forbidden fingerprint, allowed referencers}`, seeded with ~4 facts (not 127) + the `factowners` location: agree the approach before building?
3. **Recipe + spec coordination** (Phase 2a first-move): `zsc noop` is in 42 recipe files + 3 active specs + a guide — synced corpora + authoritative specs. Confirm the coordinate-with-owner protocol + that I may edit the specs.
4. **P0c branch:** apply Phase 1 to it + merge it as the first slice, or hold the whole branch until more phases land?
