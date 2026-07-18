# Delivery-model redesign — foundations verdict (2026-06-09)

**What this is:** the deep evaluation Karel commissioned of Stream B (`plans/open-work-compact-2026-06-09.md`
§B): *is the proposed delivery-model redesign built on the right foundations?* Method: 16-agent workflow
(6 grounding mappers over implementation/corpus/invariants/precedents/evidence/session-state → 5 adversarial
skeptics on the load-bearing premises → 4 lens judges → completeness critic) + an independent fresh Codex
(gpt-5.5) adversarial pass + first-hand verification of repo state. Object evaluated: the target model in
`plans/workflow-response-delivery-eval-2026-06-05.md` §5 AND the reconciled
`plans/workflow-response-delivery-model-2026-06-05.md` (which bakes in the clean-swap + uri=-stays decisions).

**Analysis only. Karel decides.**

---

## 1. Verdict in one paragraph

The redesign aims at the right target (canonical decision contract at the delivery boundary; the wall stops
being the delivery unit) and its end-state is architecture-aligned with the repo's own hardest-won principles —
but **as written it stands on two empirically refuted premises** ("position is the contract" as the causal
mechanism; "guidanceRefs get pulled") **and a baseline that is stale against main** (its headline lever already
shipped and is released). The mechanism layer survives every attack and should be adopted; the calibration
(how far to cut, head syntax) and the envelope-as-wire-format are separable decisions that need a re-baseline
measurement first. Errors-join-envelope should be rejected in favor of a narrow P4 amendment.

---

## 2. What the adversarial pass established (per premise)

### 2.1 REFUTED — "Position is the contract" (the 4%→80% causal story)

The statistical contrast is real (Fisher p=3.3e-10) but the attribution to *position* is confounded:

- The close-mode tell sat at the **same median byte-fraction (~0.11) in BOTH populations**; in popA (4%) the
  tell was structurally EARLIER (atom index 0/19) than popB's DECISION atom (index 1/10).
- Populations are fully disjoint scenario sets (19 fresh-bootstrap vs 9 maintenance/recovery) with
  non-overlapping wall sizes (15.2–28.4KB vs ≤13.5KB).
- The content differed: popA = descriptive prose + generic `<host>` placeholder; popB = imperative
  `### DECISION` heading + paste-ready per-hostname filled calls.
- The dominant observed mechanism: **head-pointer obedience** — 40/50 popA agents' literal next call was the
  head's own `Next: zerops_deploy` (which never named close-mode); 14/20 popB agents copied the DECISION
  atom's exact args.

**Amended premise:** *executable salience* is the contract — an imperative, envelope-filled, paste-ready
decision slot near the head wins the next-call slot; **whatever the executable head omits is omitted at
scale**. This licenses head/nextCall design and head-completeness as the #1 invariant. It does **NOT**
license content removal — removal must be justified separately on context-cost grounds and gated on
tell-compliance metrics. (Caveat the critic flagged: the amended mechanism is itself subject to the same
population confound; only the re-baseline battery discriminates.)

### 2.2 REFUTED — "guidanceRefs[] works" (pull-based delivery)

- The identical mechanism already exists (5 `reference:true` atoms offered as `pull on demand:
  zerops_knowledge uri=…` stubs): **198 offers across 41 runs, 0/198 followed** (re-verified 2026-06-09 by
  re-running the extraction over the 1,433-payload corpus). All 31 observed knowledge pulls were
  agent-NEED-initiated; the only uri-following was 3 query→fetch chains the agent itself started.
- A willing agent **cannot** pull 106/111 atoms today: `resolveAtomURI` hard-rejects inline atoms
  (`internal/tools/knowledge.go:320-326`, pinned by `TestKnowledgeTool_AtomURI_RejectsInline`); 37 atoms carry
  `{hostname}`-class placeholders that pull retrieval structurally cannot substitute — and the filled args ARE
  the measured compliance mechanism (2.1), so re-authoring them placeholder-free destroys the thing that works.

**Amended premise:** guidanceRefs is an **index for agent-initiated retrieval only**. Any atom demoted from
inline must be treated as **deleted from the agent's view** unless (a) its tell independently re-surfaces at a
trigger-time surface (preflight error, failureClassification, verify Recovery), or (b) a trigger-time error
names the atom id (named needs are the only demonstrated pull driver). A per-demoted-atom reachability map
(atom → trigger-time owner | acceptable-loss ruling) is a cut-over precondition; it does not exist yet.

### 2.3 REFUTED — "Errors join the envelope" (the P4 replacement)

- P4 archaeology: commit 733c1234 (2026-04-26, `plans/plan-pipeline-repair.md` §3+§7) shows
  envelope-on-every-response was **already tried and deliberately revised down** ("over-spec'd"; "carry-along
  envelope creates parallel failure surfaces"), with the error half resolved NO after an independent Codex
  review — three reasons (convertError lacks inputs across 279 call sites; no observed failure mode;
  error-in-error-path has no clean shape). The proposal restores the rejected ambition on the response class
  where inputs are least available, without answering the original cost argument. `ComputeEnvelope`-itself-failed
  is a canonical error case — a lifecycle head there is impossible by construction.
- Two of the five highest-volume "error" surfaces aren't P4-governed convertError errors at all (failed deploy
  → jsonResult with failureClassification; launch blockers → jsonResult with typed Blockers+Recovery); the
  real convertError classes already carry handler-local structured correctives (adopt pairing: 32/32 one-call
  recovery).

**Amended form (S4 + Codex + j2/j3/j4 converge):** errors **stay leaf payloads** — P4's core survives. P4 is
*amended* to require a **typed executable `nextCall` on every error where the handler knows it** (same unified
call type as the success envelope; `choices[]` for ambiguity rejections; `missingInputs` for user-owned values),
populated via the existing `ErrorOption` seam from handler-local/gate-computed data only — no live reads on the
error path. The static `WithRecoveryStatus` pointer retires as a default. This captures the 84/103
decorative-recovery finding at a fraction of the cost and risk.

### 2.4 WEAKENED-but-survives — "No decision-critical tell is lost"

The strongest attack (extract every agent-authored zerops.yaml from 116 transcripts; trace each wall-caused
behavior to its teaching atom and its check) **failed to find an outcome-level regression**: every
artifact-shaping tell (deployFiles=[.], run.envVariables placement, env refs) is triple-fenced at deploy
preflight and the fence demonstrably works TODAY (the wall atom rendered in 42/42 fat walls, agents still
violated DM-2 seven times, the check caught all 7 with one-turn recovery in 6/7); verify-after-deploy (97%) is
co-owned by trailer+head+gate; the most sophisticated compliance is recipe-owned, not wall-owned.

Survives only with three conditions: (a) refs count as deletion, not preservation (2.2); (b) the kept inline
set at never-deployed develop-start is the **~5-block artifact-shaping core ≈ 9.4KB** — not 1–2 atoms
(eval-§5) and not the 4KB inline budget (model doc §3); (c) the cut-over eval gates on preflight-error
fire-rate + turns-to-first-verified-deploy, because the regression currency is turns, not compliance rates.

### 2.5 WEAKENED — "JSON head + markdown tail"

- The escaping-cost fear is a myth: measured corpus-wide, JSON string-escaping of embedded prose costs
  **1.04% of all bytes** (the audit's "44% of responses / 50% of bytes" line was payload composition, not
  escaping). Not a design driver in either direction.
- **No consumer parses the JSON head as machine data** — MCP tool results reach the model as raw text tokens;
  the 80%-compliance lever shipped as a *markdown* head line (`render.go:217-221`). Current evidence favors a
  terse deterministic markdown head with JSON reserved for executable call args; mcp-go v1.5.0 supports
  multi-block content but the repo's own resources-rejection precedent (non-universal client capability)
  cautions against relying on it.
- **Head syntax is an empirical A/B choice at cut-over, not a settled design input.** The schema-pinned,
  deterministic, head-first decision block is what matters; its wire syntax is secondary.

### 2.6 STALE — the evidence baseline vs main

The corpus (2026-06-03..05) predates the B-batch, and **v9.112.0 == HEAD: all 9 bug fixes are released to
every auto-updating user**, including:

- **B5/11ee8a31** = eval-§5 item 3 verbatim — the close-mode DECISION atom axis fix (now `priority: 1`,
  `closeDeployModes:[unset]`, no `deployStates` lockout) AND the head `→ DECISION required` line with
  single-owner `CloseModeCallExample`. **The redesign's headline lever is already banked.**
- B3/F60 (verify ordering), B4/F11-half (derive deployed for recipe-buildFromGit), B6 (stderr +
  credential contract = the 14-wasted-turns fix), B7 (empty logs), B8, B9, B10.

Any ROI claim for the cut-over is invalid until re-measured. Still live on main (verified): Router-1
(launch status returns bootstrap route-menu), Router-2 (`workflow.go:504` classify unguarded),
**deploy-order** (`failureClassification` serializes LAST below ~2KB of logs, `ops/deploy_common.go:52` —
disproportionately important because trigger-time surfacing is the load-bearing mechanism of the whole
demotion strategy), T4 (ACTIVE masking), T7, and the remaining T1 generalization.

---

## 3. Architecture rulings (j2 + m1/m4 + Codex converge)

- **The envelope is achievable as an EXTENSION, not a new layer:** every head field has a typed in-repo owner
  (Phase + live ServiceSnapshots in StateEnvelope; nextCall in `Plan/NextAction` from pure `BuildPlan` —
  whose JSON tags are dead on the wire today; refs in reference stubs + Mode-5 uri fetch). The model doc's
  §3 "one owner / one builder" rule is right and **must be binding**: written as a wire schema that 104
  `jsonResult` handlers populate by hand, AgentResponse becomes the **ninth** structured next-call shape
  (m4 catalogued eight predecessors, three already force-merged into `topology.Recovery`).
- **nextCall owner = extended BuildPlan** (pure over envelope + gate verdicts), covering the currently
  abdicated export/launch/strategy phases; unified call type promoted to `topology`; kill the second
  `Recovery.Action` dialect in the same change.
- **guidance[] selection owner = Synthesize over an explicit decision-tier front-matter axis** (not handler
  discretion, not bare top-N-by-priority — 37 atoms sit at p2, 17 at p1, 61 match develop-active; priority is
  an ordering signal with no top-N selection semantics today). Lint pins tier-population per phase×axis slot.
  guidance/guidanceRefs = **one partition of one match set** (tier × pullability); lint pins
  no-atom-in-both + every-ref-resolvable. The cross-ref contract (`references-atoms` = same-payload presence,
  `atom.go:49-66`) must be rewritten to "co-selected OR ref-resolvable" or selection silently breaks tells.
- **Surface-once ledger (model doc migration phase 5) is structurally refuted** (m6): cancel-and-retry
  (the knowledgeCache contract), compaction unobservability, and shared-PID subagents all break delivery
  memory. The only compaction-safe dedup is **state-axis self-extinguishing** — atoms expire when observable
  state records the decision (the shipped `closeDeployModes:[unset]` pattern), which also carries essentially
  ALL the redundancy reduction (launch live-row gating, export exportStatus axes, one-emission-site trailer).
  Kill the ledger phase; replace with state-axis gating. P2/P3 purity and `action=status` full-reorientation
  stay intact by construction.
- **Live-truth generalization pinned as OR-compose-with-stamps** (the B4 `DeriveDeployed` precedent,
  `compute_envelope.go:316-336`) — derivation-as-replacement would re-inflict the F11 bug class on
  transiently-non-ACTIVE services.
- **Clean swap is correct** (j3): response shapes are ephemeral wire content; tool names + input schemas
  (the `mcp__zerops__*` allowlist seam) don't change; the one disk surface describing response shapes
  (AGENTS.md managed doctrine, `agents_shared.md:37-41`) self-refreshes at server startup provided the
  template lands in the same commit. Migration-cheapness of the swap itself is NOT the risk; the calibration is.

---

## 4. What the evaluation could NOT settle (and how to settle it)

1. **How much problem remains on main.** The single decisive check: **re-run the popA fresh-bootstrap
   scenario set via flow-eval against v9.112.0** (~3h wall). Pre-registered metrics:
   close-mode-set-before-first-deploy rate, wasted-retry count (identical error payload re-received),
   bytes/run + per-type p50, turns-to-first-green-verify, head-follow rate. Decision rule:
   **compliance ≥~60-70% on main → the salience fix captured the lever; the envelope demotes to a severable
   format-unification decision** (kills the removal premise; the cheap batch + §3 rulings are the work).
   **Compliance still ≤~10-20% → first real evidence that wall volume itself drowns salient tells; the
   cut-over is re-armed.**
2. **Envelope now vs deferred** — j1/j2/j3 (adopt-amended now, it also fixes the 3-format bimodality + the
   8-shape drift) vs j4 (defer behind the re-baseline; ship the cheap-80% batch first). Both grounded; the
   re-baseline decision rule above is the tiebreaker.
3. **Head syntax** (markdown vs JSON) — A/B at cut-over; all current compliance evidence was measured on
   markdown heads.
4. **The 428-run corpus** (`plans/response-audit-corpus/`) was never re-opened by this evaluation; its
   transport-ceiling stat (50% of develop:starts exceed the 24KB ComposeUnderBudget budget, max 32.3KB)
   is the strongest remaining argument that develop-start NEEDS a size cut independent of compliance —
   verify it during re-baseline. Single-agent-family caveat: every behavioral number is Claude-Code-only;
   the cross-agent (codex/grok) motivation for a machine head has zero corpus evidence either way.

---

## 5. Recommended disposition (answers to the model doc's §8 open decisions)

| # | Open decision | Evaluation's answer |
|---|---|---|
| 1 | Adopt the target model? | **Adopt-amended.** Mechanism layer now (head completeness, one-owner nextCall via BuildPlan→topology, state-axis gating, live-truth completion, sectioned knowledge, error nextCall). Envelope wire-format = severable decision gated on the re-baseline rule (§4.1). |
| 2 | Errors join the envelope (replace P4)? | **No.** Narrow P4 amendment: typed executable nextCall on ErrorWire via ErrorOption, handler-local data only, errors stay leaf. |
| 3 | BI-1 standalone? | Moot — shipped + released (B1, f6aa498d). |
| 4 | Inline budget (Codex ~4KB vs intermediate)? | **9.4KB operating point** (the popB-evidenced ~5 decision-critical blocks). Reject 2–5KB / 4KB-inline as evidence-free; cut deeper only on re-baselined pull-rate + compliance evidence. |

**Sequencing:**
0. **Re-baseline battery on v9.112.0 + commit the metric extractor** (turn flow-eval observation into a real
   per-phase gate — per its own README it is currently not a gate). Nothing claims credit before this.
1. **Independent bug batch:** Router-1, Router-2, **deploy-order** (failureClassification first in
   DeployResult — prerequisite for the trigger-time strategy), T4 unmask, T7, T1-completion. All S/M.
2. **Vocabulary first:** AgentCall/DecisionHead + typed Blockers promoted to `topology`, ONE builder derived
   from Plan/Recovery; Stream A's LP-8/J3/F43 land ON that type (converts the Stream A collision into a
   dependency — sequence launch/export family migration after Stream A's P0/P1).
3. **Develop-start cut to ~9.4KB** via decision-tier selection + state-axis gating + per-demoted-atom
   reachability map; gate on compliance + turns metrics, never byte targets alone.
4. **Envelope cut-over** (if §4.1 re-arms it, or as deliberate format-unification): head-first, A/B head
   syntax, goldens assert field order + nextCall==Plan.Primary; spec P4/P5 amendments in lockstep with
   `agents_shared.md` doctrine.
5. Knowledge chunking (URI grammar over existing `sections.go` H2 parser) — anytime, zero behavior risk.

---

## 6. Provenance

- Workflow run `wf_7745d06e-6ec` (16 agents / ~1.8M tokens): 6 mappers (pipeline, atoms, invariants,
  precedents, evidence re-verification incl. the 0/198 pull-rate measurement, session-state), 5 skeptics
  (S1 position REFUTED-as-attributed, S2 pull REFUTED, S3 regression WEAKENED-survives, S4 errors-envelope
  REFUTED, S5 format WEAKENED), 4 judges (agent-cognition, architecture, migration, greenfield — all four:
  adopt-amended; greenfield: defer envelope), completeness critic.
- Independent fresh Codex pass (`/tmp/codex-out-1781030466-38204-5628.md`): same verdict shape — "directionally
  right on live truth/salience/live-gating/chunking; wrong as written on removal-behind-refs and
  errors-envelope"; proposed status-as-canonical + delivery classes + receipts (the receipts half refuted by m6).
- First-hand verified: B-batch on main + released (v9.112.0==HEAD), P4 archaeology (733c1234,
  plan-pipeline-repair §3/§7), `develop-strategy-review.md` current axes, `render.go:216` head DECISION line,
  ComposeUnderBudget wiring (status+bootstrap only), `workflow.go:504` Router-2 still live.
