# Delivery model + goal contracts — final integrated plan (2026-06-09)

**What this is:** the final recommended plan for Stream B, integrating three sources:
1. `plans/delivery-model-foundations-verdict-2026-06-09.md` — the adversarial evaluation (16-agent
   workflow + fresh Codex + first-hand repo verification) of the delivery-model redesign.
2. `plans/zcp-goal-contracts-concept-2026-06-09.md` — the goal-contracts concept (control half:
   declarative requirements replace step machines; backed by the construct ledger over the 428-run corpus).
3. Current main state: the B1–B10 batch is **shipped and released** (v9.112.0 == HEAD).

**Status:** plan for Karel's go/no-go. No implementation shipped.

---

## 1. The unifying architecture (one sentence per half)

- **Control half (goal contracts):** the agent declares a goal; every response recomputes
  `status = f(goal, inputs, LIVE state)` → unmet requirements (each naming its live check owner +
  evidence + structured fix), gates only at genuinely-destructive/impossible points of action.
- **Delivery half (decision envelope):** every response opens with a deterministic decision head —
  ONE executable `nextCall` derived from the same predicates the gates consume, typed blockers,
  `choices[]` for ZCP-unresolvable decisions, hard-budgeted inline guidance, depth behind refs.

Both halves operationalize the same repo principle: **one owner per concept, tell==check, derive don't
stamp, validation-set ≠ presentation-set.** The codebase already migrated halfway there (auto-close
derived; launch/export stateless narrowings; adopt plan derivation; topology.Recovery) — this plan
finishes a migration in progress, it does not start a new one.

## 2. Corrections baked in (where the source docs were wrong — evidence in the verdict doc)

| Source-doc claim | Corrected to | Why |
|---|---|---|
| "Position is the contract" (4%→80% causal story) | **Executable salience is the contract**: imperative framing + paste-ready filled args + head completeness. Whatever the executable head omits is omitted at scale (40/50 head-obedience). | Tell sat at the same byte-fraction in both populations; populations disjoint; popA tell was structurally earlier. The *fix shape* (DECISION slot / `choices[]`) survives; the *removal license* does not. |
| "Pull-first knowledge" / guidanceRefs as delivery channel | **Inline-decision-critical + refs as depth-index only.** Demoted = deleted from the agent's view unless the tell re-surfaces at a trigger-time surface or a trigger-time error names the atom id. | 0/198 pushed-ref follow rate (re-verified); all 31 observed pulls self-initiated; 106/111 atoms not pullable today (placeholder substitution boundary). |
| "Errors join the envelope" (replace P4) | **Narrow P4 amendment:** typed executable `nextCall` on ErrorWire via the existing ErrorOption seam, handler-local data only; errors stay leaf, zero live reads on the error path. | P4 archaeology (733c1234): envelope-everywhere already tried + deliberately revised down; ComputeEnvelope-failed is itself an error case; 2 of 5 top error surfaces aren't even convertError errors. |
| Inline budget 4096 B / session-start 2–5 KB | **~9.4 KB operating point** (the ~5 decision-critical artifact-shaping blocks; popB's 88%-compliance point). Cut deeper only on re-baselined evidence. | No corpus support for compliance below ~10 KB; the filled-args content IS the measured mechanism. |
| Per-session surface-once ledger (even "LAST") | **Killed. State-axis self-extinguishing only** (atoms expire when observable state records the decision — the shipped `closeDeployModes:[unset]` pattern). | Delivery memory breaks cancel-and-retry (knowledgeCache contract), compaction unobservability, shared-PID subagents. State-axis gating carries ~all the redundancy reduction anyway. |
| §6 bug lists (both docs) | **Mostly shipped+released** (BI-1=B1, F60=B3, F11-half=B4, DEV-2=B5, BOOT-3=B8, PT-3=B7, DS-1=B9, LX-3=B2 content). Open: Router-1, Router-2, deploy-order, T4, T7, T1-completion. | Verified against main; v9.112.0 == HEAD. |
| JSON head as settled format | **Head syntax = A/B at cut-over** (terse deterministic markdown head vs JSON head; JSON certain only for call args). | Escaping cost is 1–2.5% (not a driver); no consumer parses the head as machine data; all compliance evidence was measured on markdown heads. |

## 3. What is adopted unamended (convergent across all sources)

- Head completeness as the **#1 invariant**: the structured head names EVERY gate input with an
  executable filled-args call.
- `nextCall` single owner: derived from `BuildPlan`/gate predicates (pure over envelope + verdicts),
  never hand-authored per handler. Unified call vocabulary (`AgentCall`) promoted to `topology`,
  killing the 8-shape next-call drift (incl. the second `Recovery.Action` dialect).
- `choices[]` for ZCP-unresolvable decisions, each option carrying a ready-to-execute call (close-mode
  decision, adopt pairing — the pairing rejection round-trip dissolves by construction).
- Requirements name their **check owner** + carry live evidence (tell==check made structural);
  dependencies are edges enforced at the point of action.
- Gates limited to the §3.4 list of the concept doc (destructive ops, DM-2, launch source-control
  evidence, schema/preflight at mutation, locks). "Wrong step" refusals become unmet requirements
  with a structured fix.
- Live-truth completion as **OR-compose-with-stamps** (B4 `DeriveDeployed` precedent); unmask ACTIVE
  (T4); bootstrap live statuses (T7).
- Knowledge section-addressable (URI grammar over the existing H2 section parser).
- Clean swap of response shapes (ephemeral wire content; tool names + input schemas + disk surfaces
  untouched; AGENTS.md doctrine template lands in the same commit — it self-refreshes at startup).
- Guided mode = rendering flag over the same contract (weak-model rail; never a second engine).
- Anti-pattern rules from concept §4 are binding (semantic requirement names, no ordinal cursors,
  nextCall stays advice, done requires evidence).

## 4. Phases

### Phase 0 — Re-baseline + metric harness (decisive, ~1 day incl. ~3h battery wall time)
Run the fresh-bootstrap (popA-class) flow-eval scenario set against current main (v9.112.0).
Commit the corpus extractor (today transient `/tmp/zcp-response-audit/extract.py`) as
`eval/behavioral/metrics/` with **pre-registered per-run metrics**: close-mode-before-first-deploy
rate, wasted-retry count (identical error payload re-received), bytes/run + per-type p50,
turns-to-first-green-verify, head-follow rate, knowledge-pull rate, CONSTRUCT_INDUCED error share
(re-run the construct-ledger classification on the fresh transcripts). This turns flow-eval from
observation into a per-phase gate (per its own README it is currently NOT a gate) AND prices what
the released B-batch already bought.

**Decision rules:**
- Close-mode compliance ≥~60% on main → the salience fix captured the behavioral lever; phases 3+
  are justified by size/structure/turns evidence, not compliance promises.
- CONSTRUCT_INDUCED step-ceremony share still high (the 62/64 + 32/32 classes recur) → the control
  half (Phase 4) is confirmed as the dominant remaining problem.
- Also verify here: the 428-corpus transport-ceiling stat (50% of develop:starts over the 24 KB
  budget, max at the 32 KB ceiling) — the strongest size-cut argument independent of compliance.

### Phase 1 — Remaining bug batch (S/M, independent, ~1–2 days)
Router-1 (launch status falls through to bootstrap route-menu), Router-2 (unguarded `classify` verb),
**deploy-order** (`failureClassification` serializes below ~2 KB of logs — prerequisite: trigger-time
surfacing is the load-bearing mechanism of every demotion decision), T4 ACTIVE unmask, T7, T1
completion. Each pinned.

### Phase 2 — Vocabulary + one builder (the keystone, ~3–5 days)
`AgentCall` + `DecisionHead` + typed `Blockers` in `topology`; ONE shared response builder with
`ComposeUnderBudget` inside (so budget cannot exist on one path and be absent on a parallel one);
`nextCall` projection from `Plan.Primary`/`Blocker.Recovery` only. **Errors:** the narrow P4
amendment (typed nextCall via ErrorOption; spec row + `agents_shared.md` doctrine line updated in the
same commit; `errwire` contract test rewritten RED→GREEN). **Stream A dependency:** LP-8/J3/F43
land ON this vocabulary — sequence launch/export family work after Stream A's P0/P1 to avoid
same-file collisions (render/synthesize/launch owners).

### Phase 3 — Develop/status cut-over (~1 week)
develop:start + lifecycle status onto the decision envelope at the **9.4 KB operating point**:
decision-tier front-matter axis owned by Synthesize (lint pins tier population per phase×axis slot;
guidance/refs = one partition of one match set; cross-ref contract rewritten to "co-selected OR
ref-resolvable"); state-axis self-extinguishing dedup; close-mode as `choices[]`;
**per-demoted-atom reachability map as a cut-over precondition** (each demoted atom → trigger-time
owner | acceptable-loss ruling); head syntax A/B (markdown vs JSON head) inside the phase gate.
Golden tests: field order (head before guidance), nextCall == typed Plan.Primary, budget enforced.
Gate: Phase-0 metrics — compliance + turns must hold or improve; bytes are secondary.

### Phase 4 — Control half: bootstrap → `provision` goal contract (~1.5–2 weeks)
The concept's §3.2 requirement set replaces step gates; pairing/route as `choices[]`;
`availableStacks` only where a type is actually authored (classic); step-complete ceremonies and
the develop:start adopt-ceremony rejection dissolve into unmet requirements with structured fixes.
**Gated on Phase 0's CONSTRUCT_INDUCED confirmation** + recipe-route regression suite (recipe-sim +
recipe flow-eval green — bootstrap route=recipe is core work, no Aleš coordination needed, but his
corpus consumption must stay intact). Biggest blast radius of the plan; phased internally
(requirement evaluation first behind the existing dispatch, ceremony removal last).

### Phase 5 — Launch/export onto the shared envelope + knowledge chunking (~1 week)
Mostly delivery work (active-blockers-only rows, exportStatus axes, taxonomy→refs, classify
auto-resolution of `IsClassifyInfrastructure` keys); knowledge chunking anytime (zero behavior risk).
Sequenced after Stream A P0/P1 (same owners).

## 5. Answers to the open decisions (concept §8 / model doc §8)

| Decision | Answer |
|---|---|
| Adopt the concept? | **Yes, amended per §2** — full sequence, each phase independently valuable + eval-gated; Phase 4 (control half) additionally gated on Phase 0's construct-metric confirmation. |
| P4 invariant change (errors join envelope) | **No.** Narrow amendment: typed executable nextCall on leaf ErrorWire (Phase 2). |
| Inline budget | **9.4 KB** for develop session-start; deeper cuts only on re-baselined evidence. |
| Guided-mode default | Decide at Phase 3 with eval data from ≥2 runtimes (all current behavioral evidence is Claude-only). |
| BI-1 / bug batches | Shipped+released; remaining trio in Phase 1. |

## 6. What was discarded (and why)

| Originally proposed | Verdict | Reason |
|---|---|---|
| Errors-join-envelope (P4 replacement) | rejected | already tried + revised down (733c1234); impossible-by-construction sites; leaf+nextCall captures the value |
| 2–5 KB session-start / 4 KB inline budget | rejected | zero corpus support below ~10 KB; filled-args content is the measured mechanism |
| guidanceRefs as load-bearing delivery | rejected | 0/198 follow rate; substitution boundary blocks 106/111 atoms |
| Surface-once session ledger | rejected | breaks cancel-retry/compaction/subagents; state-axis gating supersedes |
| "Position is the contract" as design driver | reframed | executable salience + head completeness; the causal story was confounded |
| Tuning ComposeUnderBudget as the develop fix | rejected (both audits agree) | transport backstop, not a selection owner — selection moves to the tier axis; the builder carries the budget |
| Timing as a redesign axis | demoted (both audits agree) | two isolated bugs (fixed); live-truth stays as guardrail invariant |

## 7. Effort + risk summary

~5–6 weeks of focused work end-to-end; phases 0–2 (~1.5 weeks) are low-risk and valuable standalone.
The two riskiest items are Phase 3's atom demotion (mitigated by the reachability map + 9.4 KB floor +
compliance gate) and Phase 4's bootstrap rewrite (mitigated by the construct-metric gate + internal
phasing + recipe-route regression suite). Stop-early points: after Phase 2 (vocabulary + error
nextCall alone kill the 8-shape drift + the 84/103 decorative recoveries), after Phase 3 (delivery
fixed, control untouched).
