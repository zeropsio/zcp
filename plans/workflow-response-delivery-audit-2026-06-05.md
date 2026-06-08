# ZADÁNÍ — Workflow Response Delivery Audit (ground-up)

**Status:** brief / not started. Drives a post-compaction analysis session (workflows + agents + Codex).
**Motivation (Karel, 2026-06-05):** the 12-round flow-eval battery produced 63 frictions; the
proposed fixes are mostly leaf-level atom-wording (cosmetic). The real question is one layer down:
**what — and how — does ZCP actually deliver to the agent in every workflow response?** "Let's say
it works somehow now, but I want to look at what is really sent and passed there, and evaluate the
whole thing." We have a goldmine of empirical data to do this from the floor.

## The deeper root (from the leaf-fix review — the evaluative LENS)
Codex named it: *"Information contract includes REACHABILITY and TIMING. A correct fact buried
below the fold is not a correct tell, and a boolean that was true before the last mutation is not
live truth."* This audit evaluates the response-delivery model against that, plus the existing
Information-Contract principles in CLAUDE.md (one owner; tell==check; recommend-don't-enumerate;
source of truth is the live platform, not a stored proxy; validation-set ≠ presentation-set).

## Object of study — every workflow response payload ZCP sends the agent
Not the code paths in the abstract — the ACTUAL bytes the agent receives, per response type/phase:
- **bootstrap** (route-menu; discover/provision/close × routes classic/recipe/adopt)
- **develop** (first-deploy; iterate; strategy-review; close-mode; the develop-active wall)
- **launch-production** (scope-prompt; source-control gate; classify; configuring-pipeline; launched; status-recovery)
- **export** (scope/variant prompts; classify-prompt; validation-failed; compose-ready; publish-ready)
- **verify**, **status envelope** (plan + progress + blockers + nextActions)
- **per-tool responses** (zerops_deploy failureClassification; zerops_discover; zerops_env; zerops_import; zerops_logs; zerops_dev_server; zerops_subdomain; zerops_knowledge)

For each: the **components delivered** — envelope, rendered guidance atoms, structured fields,
nextActions/Next line, status, recovery hints — and their anatomy.

## The empirical base (the "obrovské množství dat")
- **~60 eval transcripts** from the just-run battery: `eval/behavioral/runs/2026060*/<scenario>/transcript.jsonl`.
  Every `tool_use_result` block IS a real ZCP response payload, captured in a live agent run across
  every workflow/route/phase. This is thousands of actual response payloads — the ground truth of
  "what we send." (Plus the older waves' runs in the same dir.)
- **The rendering code** (the "how"): `internal/workflow/render.go` (RenderStatus / renderGuidance /
  renderProgressAndBlockers), `synthesize.go` (atom selection), `envelope.go` / `compute_envelope.go`,
  `compose.go` (ComposeUnderBudget), the atom corpus `internal/content/atoms/*.md`, and the per-tool
  response builders in `internal/tools/`.
- **The goldens** `internal/workflow/testdata/atom-goldens/` — the canonical rendered responses.

## Evaluation questions (the 7 dimensions — answer each WITH the real-payload evidence)
1. **WHAT** — per response type, the exact content blocks delivered (anatomy + an annotated real example).
2. **HOW MUCH** — size distribution (bytes / atom-count / lines) per response type; the signal-to-noise
   split (decision-critical vs reference/boilerplate). Where are the heavy responses.
3. **REACHABILITY** — is the agent's actual next-action info in the decision-HEAD (the part it acts on),
   or below the fold? Measure: where the load-bearing instruction physically sits vs where the agent
   needs it. (F7/R1 lens.)
4. **TIMING / LIVE-TRUTH** — does each response reflect the LIVE platform state at send-time, or a
   stale snapshot/bool? Enumerate every field derived from a stored proxy vs a live read. (F11/F60 lens.)
5. **REDUNDANCY / DRIFT** — what is re-dumped every turn (surface-once violations)? what tells drift
   from their checks (tell==check violations)? what's repeated across responses.
6. **RIGHT-INFO-RIGHT-TIME** — at each agent decision point, is the ONE correct next thing surfaced
   (recommend-don't-enumerate), or is the agent handed a menu/firehose to filter (validation-set
   presented as choice-set)?
7. **STRUCTURED vs PROSE** — what is machine-readable structured (status, nextCall, fields) vs prose
   the agent must parse? What SHOULD be structured (e.g. a `nextCall` the agent executes, not reads)?

## Method (workflows + agents + Codex — staged, each phase a separate Workflow run)
- **Phase 0 — EXTRACT corpus.** A Workflow/agent pass mines all ~60 transcripts → a structured
  corpus of real response payloads, keyed by tool/workflow/route/phase, with size + component
  breakdown. Output: a machine-readable index + per-type representative samples. (This is the
  empirical spine everything else cites.)
- **Phase 1 — ANALYZE.** Fan-out: one agent per workflow-type (bootstrap/develop/launch/export/
  verify/per-tool) AND/OR per dimension (reachability/timing/redundancy/structure), each reading the
  real payloads + the rendering code, scoring the 7 questions with cited evidence. Adversarial-verify
  the sharp claims (a "buried instruction" claim must quote the byte offset; a "stale bool" claim must
  name the field + its live source).
- **Phase 2 — CODEX architectural critique.** Is the response-delivery MODEL right? Should it be
  redesigned (decision-head + structured nextCall + live-truth derivation at render-time + below-cap
  relevance demotion)? Codex takes a position on the TARGET delivery architecture, independent of the
  per-type findings.
- **Phase 3 — SYNTHESIZE.** A ground-up evaluation: (a) the real delivery anatomy per workflow,
  (b) the systemic problems ranked (reachability / timing / redundancy / structure), (c) a proposed
  TARGET delivery model, (d) whether the leaf-fix plan is validated, reframed, or subsumed.

## Deliverable
`plans/workflow-response-delivery-eval-2026-06-XX.md` — the ground-up evaluation + proposed target
model. Then Karel decides: redesign the delivery model, keep the leaf-fixes, or both.

## Scope / constraints
- **Analysis first — no implementation.** This evaluates; Karel decides changes.
- The leaf-fix plan (`flow-eval-fix-master-plan-2026-06-05.md`) + friction report + Codex review are
  INPUTS/context, NOT the focus. The audit may subsume them.
- Empirical: every systemic claim cites a real payload (transcript) + the rendering owner (code).
- NEVER make release / make install (Karel's explicit decision). eval-zcp for any live check.
- Recipe-generation code (Aleš's scope) is read-only here unless flagged.

## Inputs on disk (for the fresh session)
- THIS brief.
- `plans/flow-eval-friction-report-2026-06-05.md` — verified 63-finding report (symptoms).
- `plans/flow-eval-battery-2026-06-04.md` — raw battery log (per-scenario evidence).
- `plans/flow-eval-fix-master-plan-2026-06-05.md` — the leaf-fix plan (now an input).
- `plans/codex-review-floweval-plan-2026-06-05.md` — Codex's reachability+timing root.
- `eval/behavioral/runs/2026060*/*/transcript.jsonl` — the real-response corpus.
- `CLAUDE.md` — Information-Contract invariants.
