# HANDOFF-to-I6.md — zcprecipator2 engine-level fix stack (post-v35 PAUSE)

**For**: fresh Claude Code instance picking up zcprecipator2 after the v35 showcase run commissioned against `v8.105.0` stalled on `writer_manifest_completeness` and was analysed into six engine-level defect classes.

**Branch**: `main`. Last tag: `v8.105.0`. The v35-analysis commit (landing flow traces + analysis narrative + verdict + this handoff) is pending.

**Repo state**: clean; the analysis window added only `docs/` changes. C-7e..C-14 from HANDOFF-to-I5 remain shipped in `v8.105.0`.

**Verdict in force**: **PAUSE**, not ROLLBACK. Full reasoning in [`runs/v35/verdict.md`](runs/v35/verdict.md). The rollback-criteria T-triggers do not cleanly fire on v35, and even if T-1 were tightened to fire on `checkResult.passed=false` count, reverting C-7e..C-14 would fix **zero** of the six v35 defects. The defects sit in the runtime layer the rollout did not touch.

---

## The story in one screen

v35 deployed successfully — 5/5 features green on dev + stage, stack healthy — but never reached finalize. The deploy-step full-check gate stuck on one check (`writer_manifest_completeness`) through 11 attempts + 3 dispatched writer sub-agents + 1 `iterate` cycle + 1 rejected `skip` attempt. Root cause:

1. At `08:15:21` main agent completes the `feature-sweep-stage` substep. Server response (composed dispatch brief for the next substep, `readmes`) is 71,720 chars. **Exceeds MCP tool-response token cap.** Harness spills to scratch file.
2. Main agent reads only first ~3 KB of the spillover. **Loses the wire contract.**
3. Main agent dispatches writer-1 with a prompt pointing at the spillover file. Writer-1 ships broken output (wrong fragment markers, no CLAUDE.md, no manifest).
4. Main agent dispatches writer-2 + writer-3 with progressively more directive fixes — using `title` + `routeTo` in the manifest template. **Those keys are wrong.** The wire contract is `fact_title` + `routed_to` ([`internal/ops/checks/manifest.go:37-42`](../../internal/ops/checks/manifest.go#L37-L42)).
5. Check error text says "missing entries for 4 distinct `FactRecord.Title` values". `FactRecord` is the Go *facts-log* struct, whose JSON tag is `title`. `ContentManifestFact` is the Go *manifest* struct, whose JSON tag is `fact_title`. **The error names the source field in Go notation; the JSON author needs the destination field in JSON notation.** Main agent tries `Title`, `factRecord.Title` sub-object, case variants, 14 top-level aliases — never `fact_title`.
6. Main agent calls `action=iterate` to re-run; **the engine lets it fake-pass all 12 substeps** with zero tool calls between completions, advancing to a meaningless iteration 1.
7. Main agent tries `action=skip step=deploy`. Engine rejects (correct). Main agent force-exports.

**Six engine-level defects, none of them in the rollout commits:**

| # | Short name | Layer | See |
|---|---|---|---|
| F-1 | Dispatch brief overflows tool-response cap | harness/engine | [`05-regression/defect-class-registry.md §16.1`](05-regression/defect-class-registry.md) |
| F-2 | Check `Detail` text uses Go-struct-field notation | check authoring | [§16.2](05-regression/defect-class-registry.md) |
| F-3 | `action=iterate` permits fake-pass of all substeps | engine | [§16.3](05-regression/defect-class-registry.md) |
| F-4 | Main agent tries `skip` on mandatory step | telemetry | [§16.4](05-regression/defect-class-registry.md) |
| F-5 | Hallucinated guidance topics; some silent-empty | knowledge-engine | [§16.5](05-regression/defect-class-registry.md) |
| F-6 | Knowledge-engine misses manifest-contract atom on obvious queries | knowledge-engine | [§16.6](05-regression/defect-class-registry.md) |

---

## Required reading (in order — ~30 min)

1. **[`runs/v35/analysis.md`](runs/v35/analysis.md)** — the narrative post-mortem. Six fundamentals with evidence pointers into the flow traces. Appendices A + B list every critical timestamp and sub-agent wall time. Load-bearing for picking up this work.
2. **[`runs/v35/verdict.md`](runs/v35/verdict.md)** — why PAUSE, not ROLLBACK; the three measurement-definition tightenings T-1 / T-8 / T-9 need before any future run can be arbitrated. The verdict also lists what's halted and what's not.
3. **[`05-regression/defect-class-registry.md §16.1-16.6`](05-regression/defect-class-registry.md)** — one structured row per defect. Each carries a candidate fix name (Cx-…), a test scenario, and a calibration bar.
4. **[`runs/v35/flow-main.md`](runs/v35/flow-main.md)** — skim the Flagged events section + the rows around the timestamps in v35-run-analysis Appendix A. Full main-session is 192 tool calls.
5. **[`runs/v35/flow-dispatches/write-3-per-codebase-readmes.md`](runs/v35/flow-dispatches/write-3-per-codebase-readmes.md)** — verbatim writer-1 dispatch prompt. Critical: shows that the main agent told the sub-agent to excavate the wire contract from a spillover file. This is the F-1 smoking gun.
6. **[`HANDOFF-to-I5.md`](HANDOFF-to-I5.md)** — the handoff you're superseding. Its invariants list + operating rules still hold. Its Fronts A/B/C/D are reordered (see §"Fronts, reordered" below) but still valid work.
7. **[`../../CLAUDE.md`](../../CLAUDE.md)** — auto-loaded; re-read if you drift. TDD non-negotiable.

---

## Raw session artifacts (for when you need to dig)

- Main session JSONL: `/Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v35/SESSIONS_LOGS/main-session.jsonl` (608 events, 2.3 MB)
- Sub-agent streams: `SESSIONS_LOGS/subagents/` (7 agents)
- Role map: [`runs/v35/role_map.json`](runs/v35/role_map.json)
- TIMELINE.md (user-authored): `/Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v35/TIMELINE.md`
- Extraction tools already exist: [`scripts/extract_flow.py`](scripts/extract_flow.py) + [`../../eval/scripts/timeline.py`](../../eval/scripts/timeline.py). Regenerate traces via `python3 docs/zcprecipator2/scripts/extract_flow.py /Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v35 --tier showcase --ref v35 --role-map docs/zcprecipator2/runs/v35/role_map.json --out-dir <path>`.

---

## The fix stack — five Cx-commits above the rollout-sequence

These are **net-new commits** above anything in [`06-migration/rollout-sequence.md`](06-migration/rollout-sequence.md). They do not replace C-15 — C-15 still eventually deletes `recipe.md`, but it is gated on Cx-BRIEF-OVERFLOW landing first (deleting the monolith while brief-delivery is broken risks losing language the atom tree hasn't fully captured).

Recommended order (dependencies flow left-to-right):

```
Cx-CHECK-WIRE-NOTATION ─┐
Cx-ITERATE-GUARD ───────┤
Cx-BRIEF-OVERFLOW ──────┴──▶ (v36 commissionable) ──▶ C-15 ──▶ v35.5 minimal
Cx-GUIDANCE-TOPIC-REGISTRY
Cx-KNOWLEDGE-INDEX-MANIFEST
```

Cx-CHECK-WIRE-NOTATION + Cx-ITERATE-GUARD + Cx-BRIEF-OVERFLOW are the **gate-to-PROCEED** set. The other two are high-value but not strictly blocking.

### Cx-CHECK-WIRE-NOTATION (smallest, do first)

**Goal**: every check `Detail` string names wire contracts by JSON key, not Go struct field.

**Approach**:
1. RED: Add a test in `internal/ops/checks/` (or a new `check_detail_wire_notation_test.go`) that walks every `StepCheck{Detail: …}` literal and fails on any regex match against `\b(FactRecord|ContentManifestFact|StepCheck|ManifestFact)\.[A-Z]\w+\b`. Seed list of Go struct names comes from `grep -n "^type.*struct" internal/ops/checks/*.go internal/ops/facts_log.go`.
2. Run the test. Collect every failure. Expected hits: at least `writer_manifest_completeness` detail in [`internal/ops/checks/manifest.go`](../../internal/ops/checks/manifest.go); probably others in `internal/tools/workflow_checks_*.go`.
3. GREEN: rewrite each failing `Detail` to use JSON keys in backticks. Example transform:
   - Before: `"manifest missing entries for N distinct FactRecord.Title values"`
   - After: `"manifest missing N entries with \`fact_title\` matching values from the facts log"`
4. Re-run test. All pass.
5. Commit with message: `feat(zcprecipator2): Cx-CHECK-WIRE-NOTATION — check Detail strings use JSON keys, not Go notation (F-2 close)`.

**Blast radius**: small — only check `Detail` strings change; no behavior change. Mechanical rewrites.

**Gate↔shim invariant preserved**: both paths surface the same corrected text (same Go code produces both gate result and `zcp check` output).

**Verification**: test added; run passes. Re-running v35 analysis manually against the new text should make the main agent's dead-end path unreachable.

### Cx-ITERATE-GUARD

**Goal**: `action=iterate` resets substep-completion state so the subsequent `action=complete` pass must produce actual work.

**Approach**:
1. RED: Write a test in `internal/workflow/` that sets up a recipe state mid-deploy with all 12 substeps marked complete, calls `iterate`, then calls `complete` on each substep **without any intervening tool calls** and expects the second-pass `complete` calls to fail with `MISSING_EVIDENCE` (or `ALREADY_COMPLETED_WITHIN_ITERATION`, pick a naming convention).
2. Locate the iterate handler in `internal/workflow/engine_recipe.go` (or wherever `action=iterate` dispatches). Current behavior: increments `iteration`, leaves substep markers as-is.
3. GREEN: on iterate, walk `recipe.Steps[currentStep].Substeps` and flip any `status=complete` back to `pending`. Persist state. Simpler than the alternative (server-side evidence requirement per completion), and preserves the step-graph invariant by design.
4. Add a second test: iterate → complete substep-A with a real tool-call in between → passes.
5. Commit: `feat(zcprecipator2): Cx-ITERATE-GUARD — action=iterate resets substep completion markers (F-3 close)`.

**Blast radius**: medium — changes engine state semantics. Confirm no legitimate flow relies on iterate preserving substep completions (grep `RecipeIterateAction` callers; expected: none rely on preservation).

**Watch out for**: the test harness `MockClient` may emit `iterate` in fixtures that assume completion markers survive. Update those fixtures if they fail.

### Cx-BRIEF-OVERFLOW (largest, gate item)

**Goal**: the composed readmes-substep dispatch brief fits within the MCP tool-response token cap (32 KB soft, 71 KB observed overflow in v35).

**Approach — not prescribed; choose one**:

**Option A (split-and-reference)**: restructure the brief response to return a **short envelope** (listing which atoms compose the full brief, plus the `ProjectRoot` + `FactLogPath` + tier) and have the main agent read the atoms directly via their paths under `internal/content/workflows/recipe/briefs/writer/`. Keeps atoms verbatim (DISPATCH.md invariant preserved) while fitting the envelope in < 5 KB. Main agent's dispatch prompt composition picks up each atom and embeds verbatim into the sub-agent prompt (tried-and-true pattern).

Downside: main agent's `Read` tool calls expand. Upside: keeps in-prompt composition working as designed.

**Option B (paginate response)**: return the composed brief in segments; the MCP tool supports multi-part responses OR the main agent calls `action=brief-page N` to retrieve each. More engine work; less main-agent change.

**Option C (stream the brief via `zerops_guidance`)**: change the readmes dispatch brief to be a list of guidance topics the main agent must pull individually. Each topic stays < 10 KB. Re-uses the guidance path already in place.

**Recommended**: Option A — it preserves the atom-verbatim invariant and reuses the existing atom-tree infrastructure. Option C is tempting but splits the brief across multiple tool round-trips (latency + composition complexity).

**TDD approach for Option A**:
1. RED: extend `zcp dry-run recipe` to fail if any composed-brief response exceeds 32 KB for any tier × substep combination. Run against the showcase tier; identify the readmes substep as the culprit.
2. Design the envelope shape. Likely: `{"briefAtomPaths": ["briefs/writer/manifest-contract.md", "briefs/writer/fragment-markers.md", ...], "projectRoot": "/var/www", "factLogPath": "/tmp/zcp-facts-…jsonl", "tier": "showcase", ...}`.
3. GREEN: modify `atom_stitcher.go` (or wherever the readmes substep composes its brief) to emit the envelope instead of the full inline brief for the readmes substep specifically. Other substeps keep their current composition if they fit under 32 KB.
4. Update `DISPATCH.md` to describe the envelope pattern: main agent Reads each atom, embeds verbatim into sub-agent dispatch prompt.
5. Re-run `zcp dry-run recipe` harness — golden diff should reflect new envelope shape.
6. Commit: `feat(zcprecipator2): Cx-BRIEF-OVERFLOW — readmes substep emits atom-reference envelope (F-1 close)`.

**Blast radius**: large. Touches stitcher + dispatch pattern. `zcp dry-run recipe` golden diffs update. Sub-agent prompt shape changes.

**Watch out for**:
- HANDOFF-to-I5 invariant #5 ("`Build*DispatchBrief` pure composition") — composition stays pure; envelope is a delivery layer above it. Verify no short-circuit is introduced in the stitcher.
- Every affected dispatch site (`composeDispatchBriefForSubStep`) must be updated in lockstep.
- Tier-gating (showcase vs minimal) still lives at dispatch sites; the envelope shape is tier-agnostic but the list of atoms it names is tier-specific.

### Cx-GUIDANCE-TOPIC-REGISTRY

**Goal**: no hallucinated guidance topics in a session; no silent-empty responses.

**Approach**:
1. RED: test that `zerops_guidance topic="bogus-topic-name"` returns a response containing at least 3 nearest-match topic IDs from the registry (not just `"unknown guidance topic"`).
2. RED: test that no valid topic returns a zero-byte response.
3. GREEN: extend the guidance handler in `internal/knowledge/` (or `internal/tools/guidance_*.go`) to (a) normalize unknown-topic responses to include suggestions via Levenshtein-or-embedding top-3 from the registry, (b) treat zero-byte valid-topic responses as a hard error with `TOPIC_EMPTY` sentinel.
4. Additionally: include the full valid-topic ID list in the initial `action=start workflow=recipe` response so the main agent has a closed universe.
5. Commit: `feat(zcprecipator2): Cx-GUIDANCE-TOPIC-REGISTRY — unknown topics return top-3 matches; zero-byte responses rejected (F-5 close)`.

**Blast radius**: small-medium. Only guidance handler + initial session response change.

### Cx-KNOWLEDGE-INDEX-MANIFEST

**Goal**: canonical wire-contract atoms are discoverable via obvious keyword queries.

**Approach**:
1. RED: test in `internal/knowledge/` that `zerops_knowledge query="ZCP_CONTENT_MANIFEST.json schema"` returns `briefs/writer/manifest-contract.md` in top 3 hits. Repeat for 4 other canonical queries (`fact_title format`, `writer_manifest_completeness`, `manifest routing`, `routing matrix`).
2. Identify why the current embedding-based ranking misses them — likely because `manifest-contract.md` is short prose with few keyword repetitions, so embedding scores are low against literal-keyword queries.
3. GREEN: add an explicit synonym/keyword index alongside embeddings. For each wire-contract atom, register its canonical keywords (`fact_title`, `routed_to`, `ZCP_CONTENT_MANIFEST.json`, etc.) and route these to the atom when a query substring-matches any. Query handler returns union of synonym hits + embedding hits, synonym hits boosted to top.
4. Commit: `feat(zcprecipator2): Cx-KNOWLEDGE-INDEX-MANIFEST — wire-contract atoms routed via explicit keyword synonyms (F-6 close)`.

**Blast radius**: medium. Changes knowledge engine ranking.

**Watch out for**: don't break embedding-based queries for non-wire-contract lookups. The synonym index is additive, not replacement.

---

## Fronts, reordered from HANDOFF-to-I5

| Front (I5) | What it was | Status (I6) |
|---|---|---|
| A. measurement evaluators | fill `extract_calibration_evidence.py` stubs | **Demoted to non-urgent**. Still valuable — implements T-1 tightening, T-8 per-call measurement, T-9 AFK-adjustment from [`runs/v35/verdict.md §3`](runs/v35/verdict.md#3-measurement-definition-tightening--required-before-next-run-arbitrates). But won't catch F-1..F-6; those are engine-layer. Do after the fix stack if bandwidth. |
| B. T-1..T-12 trigger scripts | pair bars with rollback actions | **Held.** v35 analysis shows the T-trigger measurement definitions themselves need tightening before trigger scripts are useful. §3 of the verdict doc drives this work. |
| C. C-15 (delete recipe.md monolith) | deletion after v35 validates C-5 cutover | **Held on Cx-BRIEF-OVERFLOW.** Don't delete the monolith while the brief-delivery path is broken. |
| D. v35.5 minimal commission | operational — user runs minimal tier | **Held on fix stack.** Minimal is Path B (main-inline writer) so F-1 doesn't directly apply, but we want clean fix-stack footing before exercising the minimal path. |

**New Front E: the Cx fix stack** (this document). Ordered by dependency.

---

## Operating rules (unchanged from I5)

1. **TDD non-negotiable** — every Cx commit has a failing test first. The defect-class-registry rows 16.1–16.6 each name a `test_scenario` as the seed test.
2. **After each commit**: `go test ./... -count=1` + `make lint-local`. Both green before advancing. `recipe_atom_lint` is part of `lint-local` — any atom-tree edit must stay lint-clean.
3. **User-review gates** — none scheduled until the fix stack lands (3-of-5 minimum). Then pause for verdict before commissioning v36.
4. **Each commit appends to [`implementation-notes.md`](implementation-notes.md)** — same pattern as C-7e..C-14: LoC delta, what landed, verification, breaks-alone consequence, ordering deps, known follow-ups.
5. **Pre-existing working-tree modifications are NOT your concern** — handle platform-catalog drift as separate `chore:` commit as before.

---

## Invariants a post-v35 cleanup MUST NOT break (amended from I5)

Unchanged:

1. **121-atom tree lint-clean**.
2. **Gate↔shim one-implementation**. Cx-CHECK-WIRE-NOTATION preserves this — both paths get the corrected `Detail` text from the same Go source.
3. **§16a dispatch-runnable contract** (editorial-review).
4. **Fix D ordering** (code-review needs editorial-review complete).
5. **`Build*DispatchBrief` pure composition**. Cx-BRIEF-OVERFLOW preserves this — the composition is pure; the envelope is a delivery-layer addition, not a stitcher short-circuit.
6. **`StepCheck` post-C-10 shape**.

**Added by this handoff** (candidate invariants the fix stack introduces):

7. **Dispatch-brief delivery fits tool-response cap**. Composed brief for any substep × tier serializes to ≤ 32 KB. Enforced by `zcp dry-run recipe` post-Cx-BRIEF-OVERFLOW.
8. **Check `Detail` strings use JSON-key notation**. No Go-struct-field dot-notation. Enforced by lint/test post-Cx-CHECK-WIRE-NOTATION.
9. **`iterate` resets substep completion state**. Enforced by test post-Cx-ITERATE-GUARD.
10. **`zerops_guidance` unknown-topic responses include top-3 matches; no zero-byte valid-topic responses**. Enforced by test post-Cx-GUIDANCE-TOPIC-REGISTRY.
11. **Wire-contract atoms recoverable via canonical keyword queries in `zerops_knowledge` top-3**. Enforced by test post-Cx-KNOWLEDGE-INDEX-MANIFEST.

---

## Expected pace

- **Cx-CHECK-WIRE-NOTATION**: ~1 hour. Smallest surface; mechanical rewrite.
- **Cx-ITERATE-GUARD**: ~1-2 hours. One handler + 2 tests.
- **Cx-BRIEF-OVERFLOW**: ~3-4 hours. Stitcher change + envelope design + golden-diff updates + DISPATCH.md update.
- **Cx-GUIDANCE-TOPIC-REGISTRY**: ~1-2 hours.
- **Cx-KNOWLEDGE-INDEX-MANIFEST**: ~2-3 hours. Knowledge-engine internals + synonym index.

**Total fix-stack**: ~8-12 focused hours. Then pause before v36 commission.

Measurement-evaluator work (HANDOFF-to-I5 Front A, tightened per verdict §3): additional ~3-4 hours when scheduled.

---

## Starting action

1. **Baseline check**: `git log --oneline -3` should show the v35-analysis commit on top of `3a38e1b` (v8.105.0 baseline) or — if the analysis commit is already pulled — the handoff commit on top of it. `git tag -l 'v*' --sort=-v:refname | head -1` shows `v8.105.0`. `go test ./... -count=1 -short` + `make lint-local` both green.
2. **Read the verdict** ([`runs/v35/verdict.md`](runs/v35/verdict.md)) first, then the analysis ([`runs/v35/analysis.md`](runs/v35/analysis.md)). Skim flow traces as needed for specific evidence.
3. **Start with Cx-CHECK-WIRE-NOTATION** — smallest surface, fastest feedback loop, gives you a sense of the check-authoring surface area. Commit before moving on.
4. **Then Cx-ITERATE-GUARD** — touches engine state; good warm-up for Cx-BRIEF-OVERFLOW.
5. **Then Cx-BRIEF-OVERFLOW** — the load-bearing one. Spend time on the envelope design before coding. Read [`DISPATCH.md`](DISPATCH.md) carefully for the composition contract the envelope must preserve.
6. **Stop and report** after 3-of-5 Cx-commits (specifically: CHECK-WIRE-NOTATION + ITERATE-GUARD + BRIEF-OVERFLOW). That's the gate-to-PROCEED-commission-v36 set. The user reviews before you proceed to the remaining two or to commissioning.

---

## If something is unclear, ask the user

The six defects are grounded in evidence but the fix approach (especially Cx-BRIEF-OVERFLOW Option A vs B vs C) is a design call. If your read of the atom tree + stitcher suggests a better option than Option A, propose it with reasoning before implementing.

The v35 session logs are the source of truth for the defect-class rows; if you disagree with any characterization in the analysis, say so before acting on the disagreement.

Good luck.
