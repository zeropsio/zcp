# v35 run analysis — nestjs-showcase

**Run date**: 2026-04-21, session `8324884b199361d9`
**Tag under test**: `v8.105.0` (C-14 ship baseline — rollout-sequence C-7e..C-14 landed)
**Commissioned by**: user, showcase tier, nestjs-showcase slug
**Deliverable tree**: `/Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v35/`
**Raw session logs**: `SESSIONS_LOGS/main-session.jsonl` (608 events, 2.3 MB) + 7 subagent streams under `SESSIONS_LOGS/subagents/`
**Flow extracts** (from this analysis): [`docs/zcprecipator2/01-flow/flow-main.md`](flow-main.md) + per-subagent traces + verbatim dispatch captures.
**User qualitative read**: see [`../../../Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v35/TIMELINE.md`](../../../nestjs-showcase-v35/TIMELINE.md). Headline: "functional recipe is production-green; finalize never reached; force-exported".

---

## Executive summary

v35 produced a functionally correct deployment (5/5 feature endpoints green on dev + stage, browser-walk 5/5 PASS, round-trip NATS job dispatch working) but **never reached finalize**. The deploy-step full-check gate stuck on `writer_manifest_completeness` through 11 rounds + 3 dispatched writer sub-agents + 1 `action=iterate` cycle + 1 attempted `action=skip step=deploy`. After ~3h wall-time (partially AFK-inflated on permission prompts), the main agent force-exported the incomplete deliverable.

The headline cause is **not** an atom-tree or check-logic defect — the atom corpus names `fact_title` correctly in [`internal/content/workflows/recipe/briefs/writer/manifest-contract.md`](../../../../internal/content/workflows/recipe/briefs/writer/manifest-contract.md), and the check reads that shape correctly. The cause is a pre-rollout **delivery bug**: the composed readmes-substep dispatch brief renders at ~71 KB, which exceeds the MCP tool-response token cap, so the harness spills the payload to a scratch file. The main agent only ever saw the first ~3 KB of that file before composing sub-agent dispatches, so the wire contract never reached the writer in-prompt. The main agent paraphrased the shape from memory (`title` + `routeTo`), all three writer sub-agents inherited the wrong shape, and the check kept failing with an error text (`"FactRecord.Title"`) written in Go type-field notation that misled the main agent into trying envelope and case variants instead of the actual JSON key `fact_title`.

Five additional engine-level defects surfaced. All six are documented below with evidence pointers into the flow traces.

---

## Findings (ordered by blast radius)

### F-1. Dispatch brief overflows the MCP tool-response token cap (primary root cause)

**Evidence**:
- [`flow-main.md`](flow-main.md) row 123 (2026-04-21T08:15:21) — `mcp:zerops_workflow action=complete substep=feature-sweep-stage` returns the spillover notice: `"Error: result (71,720 characters across 1 line) exceeds maximum allowed tokens. Output has been saved to /home/zerops/.claude/projects/-var-www/.../tool-results/mcp-zerops-zerops_workflow-1776759321598.txt"`.
- Row 124 (08:15:27) — main agent reads only the first ~3000 bytes of the spillover file (`python3 -c "...read()[:1500]; ...read()[1500:3000]"`).
- Row 125 (08:16:21) — main agent dispatches writer-1 with a prompt that instructs the sub-agent to "Read it in 80,000-char slices via `python3 -c \"print(open('…').read()[A:B])\"`. The `detailedGuide` field inside the JSON contains the readmes sub-step brief; that is what you must follow byte-for-byte". Full prompt: [`flow-dispatches/write-3-per-codebase-readmes.md`](flow-dispatches/write-3-per-codebase-readmes.md).

**Diagnosis**: the composed `readmes` dispatch brief is ~71 KB because it bundles the full writer-role brief (manifest-contract, fragment rules, cross-surface honesty, gotcha format, IG discipline, per-codebase context). This exceeds the runtime's tool-response token cap. The harness's spillover path preserves the payload on disk but **breaks the in-prompt composition pattern DISPATCH.md prescribes** — the main agent loses the ability to embed or paraphrase the brief with fidelity.

**Consequence chain**: main agent → loses brief → paraphrases wrongly → writer sub-agents inherit wrong wire shape → check fails forever.

**Invariant touched**: HANDOFF-to-I5 invariant #5 (`Build*DispatchBrief` is pure composition). Composition is pure; **delivery** is the break. Not a rollout regression — pre-dates C-7e..C-14.

---

### F-2. Check error text uses Go type-field notation, not JSON key notation

**Evidence**: across 11 deploy-complete failures (rows 128, 131, 133, 139, 147, 149, 152, 155, 157, 178, 182 in [`flow-main.md`](flow-main.md)), the persistent failure text reads:

> `manifest missing entries for 4 distinct FactRecord.Title values that appear in the facts log: ...`

`FactRecord` is the Go struct of facts-log entries ([`internal/ops/facts_log.go:105-110`](../../../../internal/ops/facts_log.go)) with JSON tag `title`. `ContentManifestFact` is the Go struct of manifest entries ([`internal/ops/checks/manifest.go:37-42`](../../../../internal/ops/checks/manifest.go)) with JSON tag `fact_title`. The failure text names the **source** field as `FactRecord.Title` — correct for the Go reader, misleading for the JSON author.

**Observed misinterpretations** (rows 137-156): main agent tried `Title` (capital T), `factRecord{Title}` sub-object, `FactRecord{Title}` sub-object, uppercase-first-letter keys alongside lowercase, 14 distinct top-level key aliases (`facts`/`Facts`/`entries`/`records`/...), `routeTo` + `routed_to` simultaneously. **Never tried the actual `fact_title` key** because nothing in the error text pointed at it.

**Diagnosis**: every check `Detail` string that references a manifest key should use the wire JSON key, not the Go field name. A lint rule on `internal/ops/checks/` and `internal/tools/workflow_checks_*.go` can mechanically catch regressions.

**Consequence chain**: main agent decodes the check error as shape guidance → tries envelope variants → dead-end loop.

**Invariant touched**: none directly, but violates a usability principle that should be made explicit.

---

### F-3. `action=iterate` permits fake-passing all substeps on iteration 2

**Evidence**:
- Row 157 (08:45:22) — main agent calls `action=iterate`. Server returns `iteration:1`.
- Rows 163-176 (08:46:26 → 08:47:50, ≈ 84 seconds) — main agent fires `action=complete` on every substep (`deploy-dev`, `start-processes`, `verify-dev`, `init-commands`, `subagent`, `snapshot-dev`, `feature-sweep-dev`, `browser-walk`, `cross-deploy`, `verify-stage`, `feature-sweep-stage`, `readmes`) in sequence with **zero tool calls between them**. Each returns ~5070 B. Durations between completions: 6-10 seconds each.
- Row 178 (08:48:03) — final `action=complete step=deploy` on iteration=1 still fails on `writer_manifest_completeness` (the one check that actually required work).

**Diagnosis**: the engine's `iterate` path increments `iteration` but does not reset `substep.status=complete` markers or require fresh evidence for each completion. The main agent can walk the step graph top-to-bottom with empty attestations, which the engine accepts. This is a correctness hole: the `iterate` mechanism is meant to re-run work, not to short-circuit it.

**Consequence chain**: wastes ~80 s of wall time per iterate; more importantly, signals to the main agent that "the engine isn't actually enforcing substep order" which erodes the step-graph invariant.

**Invariant touched**: engine-correctness invariant not explicitly captured in HANDOFF-to-I5's invariants list. Should be added: "iterate resets substep completion state OR requires per-completion evidence".

---

### F-4. Main agent attempts `action=skip step=deploy`

**Evidence**: row 180 (08:49:05) — `zerops_workflow action=skip step=deploy` → `INVALID_PARAMETER: "deploy" is mandatory and cannot be skipped`.

**Diagnosis**: the engine refuses correctly. But the fact that the main agent reached "ask the engine to bypass the gate" is itself a signal of unproductive retry depth. Not a code bug — a behavioral signal the retry budget (or fallback path) was exhausted.

**Consequence chain**: ~40 seconds lost; 2 minutes later the main agent force-exports. No engine fix required.

**Invariant touched**: none. This is telemetry: if the main agent tries skip on a mandatory step, record it as a retry-budget-exhaustion signal and surface it in the T-trigger layer.

---

### F-5. Main agent requests guidance topics that don't exist

**Evidence**: rows 31, 32, 33 in [`flow-main.md`](flow-main.md) (07:29:50 → 07:29:51):

- `topic=dual-runtime-consumption` → `Error: unknown guidance topic "dual-runtime-consumption" — check the skeleton for valid topic IDs`
- `topic=client-code-observable-failure` → `Error: unknown guidance topic "client-code-observable-failure" — check the skeleton for valid topic IDs`
- `topic=init-script-loud-failure` → `result_size=0` (empty response — silent failure, worse than the error)

**Diagnosis**: the main agent pattern-matched plausible-sounding topic IDs out of its own reasoning rather than from an authoritative list. The topic registry does not surface its valid-topic list in the session briefing. One topic (`init-script-loud-failure`) returned empty instead of an error — worse than the unknown-topic response because it gives no actionable signal.

**Consequence chain**: ~4 seconds lost per hallucinated topic; larger risk is that a quiet `topic=` failure might be interpreted as "no additional guidance needed" and skipped silently.

**Invariant touched**: none directly. Candidate new invariant: "every `zerops_guidance` error response must name 3-5 nearest valid topic IDs".

---

### F-6. Knowledge-engine query for manifest schema returns unrelated hits

**Evidence**: row 161 (08:45:58) — main agent calls `zerops_knowledge query="ZCP_CONTENT_MANIFEST.json schema writer_manifest_completeness"` after eight failed manifest-fix attempts. Response:

```
[{"uri":"zerops://decisions/choose-queue","title":"Choosing a Message Queue on Zerops","score":1,...}]
```

Top hit is `choose-queue` (message-queue selection) with score 1 — completely unrelated to the manifest schema. Result size 2117 B, other hits equally off-topic.

**Diagnosis**: the knowledge-engine does not index the `manifest-contract.md` atom or the `ContentManifestFact` struct documentation under the obvious query strings (`ZCP_CONTENT_MANIFEST.json`, `manifest schema`, `writer_manifest_completeness`). When the main agent falls back to the knowledge engine after exhausting its paraphrased understanding, the engine can't rescue it.

**Consequence chain**: the one escape hatch the main agent had — ask the knowledge engine — delivered irrelevant hits and confirmed the agent's dead-end.

**Invariant touched**: none explicitly. Candidate: "every wire-contract atom must be recoverable from the knowledge engine via an obvious keyword query".

---

## Secondary observations (documented, not blocking)

- **S-1. `writer_content_manifest_exists` failure on first round** — writer-1 never wrote ZCP_CONTENT_MANIFEST.json at all. Check's error names the file path + cites `recipe.md content-authoring-brief §'Return contract'`. Writer-1's dispatch prompt did not mention the manifest obligation; it pointed the sub-agent to the 71 KB spillover to find the contract. Downstream of F-1.

- **S-2. `{api,app,worker}_claude_md_exists` failures on first round** — writer-1 did not write CLAUDE.md. Same cause as S-1. Writer-2 fixed this after dispatch-2 enumerated the fix explicitly.

- **S-3. Fragment marker format error on first round** — writer-1 wrote `<!--#ZEROPS_EXTRACT_START:X-->` instead of `<!-- #ZEROPS_EXTRACT_START:X# -->`. Writer-2 fixed after dispatch-2 named the exact literal. Downstream of F-1.

- **S-4. Long provision silence (88 min)** — row 20 (06:01:03) → row 21 (07:29:28). User confirmed AFK permission-prompt wait. Benign. Caveat for T-9 wall-clock measurement: time spent waiting on permission prompts must be excluded from the wall-clock gate.

- **S-5. 71,720-char spillover path preserves useful data** — the harness behavior is technically correct (doesn't drop the response), but the integration point between harness spillover and main-agent context is broken. A fix to F-1 must keep the spillover path as a safety net but avoid relying on the main agent excavating JSON from a scratch file in production.

---

## Cross-check against HANDOFF-to-I5 invariants

| # | Invariant | v35 status |
|---|---|---|
| 1 | 121-atom tree lint-clean | **Holds** — no atom-tree edits in this run |
| 2 | Gate↔shim one-implementation | **Holds** in code; but F-2 violates the underlying usability premise (the shim and the gate surface the same error text, which is Go-notation-tainted) |
| 3 | §16a dispatch-runnable contract (editorial-review) | **Not exercised** — deploy never passed, close never ran |
| 4 | Fix D ordering (editorial-review before code-review) | **Not exercised** |
| 5 | `Build*DispatchBrief` pure composition | **Composition holds; delivery breaks** (F-1) |
| 6 | `StepCheck` post-C-10 shape | **Holds** — observed checks carry `{name, status, detail, preAttestCmd, expectedExit}` |

**Net**: no rollout invariant is directly violated by landed code. The defects are pre-rollout (engine, knowledge-engine, harness-integration) that the rollout didn't touch.

---

## What v35 does *not* tell us

- **Editorial-review (C-7.5) efficacy** — close never ran. v34 remains the only data point.
- **Post-`writer_manifest_completeness`-pass composition behavior** — the finalize + close path is untested under the current rollout.
- **Whether the 71KB brief-overflow affects the minimal tier** — minimal-tier writer is inline in the main agent (Path B), not a dispatched sub-agent, so F-1 should not apply there. Needs a minimal-tier run to confirm (that's Front D / v35.5 in HANDOFF-to-I5).

---

## Calibration-bar coverage gap

Mapping v35 defects to the 97-bar sheet in [`calibration-bars.md`](calibration-bars.md) and the T-1..T-12 rollback triggers in [`rollback-criteria.md`](rollback-criteria.md):

- **F-1 (brief overflow)** — no bar covers "dispatch brief fits tool-response token cap". Gap.
- **F-2 (check error UX)** — no bar covers "check detail strings use JSON-key notation not Go-field notation". Gap.
- **F-3 (iterate fake-pass)** — no bar covers "iteration=N requires per-substep evidence". Gap.
- **F-4 (skip attempt)** — no bar covers "main agent never tries `action=skip` on a mandatory step". Gap (candidate telemetry).
- **F-5 (unknown guidance topics)** — no bar covers "zero unknown-topic responses per session". Gap.
- **F-6 (knowledge-engine miss)** — no bar covers "manifest-contract atom recoverable via obvious query". Gap.

Six of six v35 fundamentals are outside the calibration-bar coverage. This is a sheet-level gap, not a measurement-script gap — `scripts/extract_calibration_evidence.py` cannot be asked to produce evidence the sheet doesn't measure.

**Proposed new bars** (tracked in [`verdict.md`](verdict.md)):
- §12.1: "largest `zerops_workflow` tool response ≤ 32 KB"
- §12.2: "`zerops_guidance` unknown-topic responses per session = 0"
- §12.3: "check `Detail` strings mention no Go type.field notation"
- §12.4: "`action=iterate` followed by substep `complete` without new tool calls = 0"
- §12.5: "`zerops_knowledge` top-3 for `manifest-contract` keyword includes manifest-contract atom"

---

## What this analysis commits us to

- **Persist the flow traces + narrative** (this document + [flow-main.md](flow-main.md) + sub-*.md + flow-dispatches/).
- **Verdict**: PAUSE, not ROLLBACK. Landed in [`verdict.md`](verdict.md) with the measurement-definition tightening for T-1 and the AFK-exclusion note for T-9.
- **Defect-class registry** grows by six rows (documented next).
- **Engine-level fix stack** defined in [`../../HANDOFF-to-I6.md`](../../HANDOFF-to-I6.md). Fronts A/B/C/D from HANDOFF-to-I5 are reordered — the fix stack lands first; Front A stays on the plan but is no longer urgent (the bars it measures won't catch F-1..F-6); Front C (C-15 monolith deletion) holds until brief-overflow is resolved.

---

## Appendix A — key timestamps for evidence lookup

| t (UTC)             | event                                                               | flow row |
|---------------------|---------------------------------------------------------------------|----------|
| `05:57:33`          | session start, `action=start workflow=recipe`                       | row 1    |
| `07:29:50-07:29:51` | 3 unknown guidance topics requested                                 | 31-33    |
| `07:31:20`          | parallel scaffold dispatch triple (apidev/appdev/workerdev)         | 35-37    |
| `07:44:32`          | generate pass (after env_self_shadow fix)                           | 53       |
| `07:54:50`          | TypeORM zero-table silent-success detected via manual DB probe      | 78       |
| `07:59:06`          | feature dispatch (SUB-ae5, 7:35 wall)                               | 88       |
| `08:15:21`          | 71 KB spillover — root cause event                                  | 123      |
| `08:16:21`          | writer-1 dispatch (SUB-a28) — prompt points at spillover file       | 125      |
| `08:22:27`          | 1st deploy full-check — 14 failures                                 | 128      |
| `08:23:45`          | writer-2-fix dispatch (SUB-a5f) — main agent paraphrases with wrong key names | 129 |
| `08:27:39`          | 2nd deploy full-check — 15 failures (some fixed, new regressions)   | 131      |
| `08:28:42`          | writer-3-fix dispatch (SUB-ac9)                                     | 132      |
| `08:40:33`          | 3rd deploy full-check — 3 failures (only manifest_completeness + 2 IG items) | 133 |
| `08:42:30`          | 5th deploy full-check — 1 failure (manifest_completeness alone)     | 147      |
| `08:45:22`          | `action=iterate` → iteration=1                                      | 157      |
| `08:46:26-08:47:50` | 12 substeps fake-passed with no tool work                           | 163-176  |
| `08:48:03`          | 10th deploy full-check on iter=1 — still manifest_completeness fails | 178     |
| `08:49:05`          | `action=skip step=deploy` — INVALID_PARAMETER                       | 180      |
| `08:51:10`          | `zcp sync recipe export --force-export`                             | 187      |
| `08:52:44`          | session ends                                                        | 192      |

## Appendix B — dispatch prompt lengths + sub-agent wall times

| # | Sub-agent                         | Prompt length | Wall time | Errors |
|---|-----------------------------------|--------------:|----------:|-------:|
| 1 | Scaffold apidev (SUB-a16)         | 12,337 chars  | 9:41      | 4      |
| 2 | Scaffold appdev (SUB-a2c)         |  8,340 chars  | 1:35      | 0      |
| 3 | Scaffold workerdev (SUB-af6)      |  7,548 chars  | 4:51      | 1      |
| 4 | Feature build (SUB-ae5)           | 13,521 chars  | 7:35      | 0      |
| 5 | Writer-1 README (SUB-a28)         |  6,268 chars  | 5:05      | 0      |
| 6 | Writer-2 fix (SUB-a5f)            |  6,612 chars  | 3:17      | 0      |
| 7 | Writer-3 fix (SUB-ac9)            |  8,011 chars  | 10:54     | 0      |

Writer dispatch prompts grew longer across retries (6.3 KB → 6.6 KB → 8.0 KB) as the main agent added fix-specific guidance, but none approached the 71 KB composed-brief size that was intended to reach the sub-agent through the spillover file.
