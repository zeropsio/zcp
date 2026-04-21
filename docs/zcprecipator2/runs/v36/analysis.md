# v36 analysis — nestjs-showcase against v8.108.0 + Cx-PLAN-NIL hotfix

**Run**: nestjs-showcase v36; session `7743c6d8c8a912fd` (workflow) / `8271b0d3-57d1-4dac-a17c-a36181150071` (Claude)
**Wall**: 2026-04-21 14:15:05 → 16:04:30 UTC (1:49:25 total; ~1:45 active-work after excluding >300 s gaps)
**Tag under test**: v8.108.0 (Cx-BRIEF-OVERFLOW, Cx-CHECK-WIRE-NOTATION, Cx-ITERATE-GUARD, Cx-GUIDANCE-TOPIC-REGISTRY, Cx-KNOWLEDGE-INDEX-MANIFEST) + v8.108.1 hotfix (Cx-PLAN-NIL-GUIDANCE) serving live
**Outcome**: finalize-complete; close step never called; exported via `zcp sync recipe export` without session context

---

## Executive summary

v36 is the **first post-zcprecipator2 showcase run to reach finalize-complete**. v35 stalled on the deploy-step `writer_manifest_completeness` gate; v36 passed deploy, passed finalize on second attempt (one round of cosmetic fixes), and then bypassed the close-step gate by exporting without `--session`. All five Cx-commits from HANDOFF-to-I6 closed their target defects or landed cleanly where UNREACHED. No ROLLBACK-class signal. The gating observation is that close step + editorial-review + code-review remained **unexercised** — the architecture's close-phase promises stay unmeasured by this run.

**Verdict class**: ACCEPT-WITH-FOLLOW-UP. Decision doc at [`verdict.md`](verdict.md). Three follow-ups before v37:
1. Enforce the close-step gate in `zcp sync recipe export` (or make `--session` behavior explicit — see F-8).
2. Commission v37 as a second-confirmation run — same tier, ideally commissioned after §7a/§8 close-phase exercise.
3. Minimal-tier commission (v35.5) unblocks — showcase close-phase coverage gap doesn't block minimal Path B validation.

---

## Per-defect verification

### F-1 (brief overflow, Cx-BRIEF-OVERFLOW) — **PASS**

**Closure mechanism** (per [`../implementation-notes.md §Cx-BRIEF-OVERFLOW`](../implementation-notes.md)): readmes substep emits an envelope listing atom IDs when composed brief > 28 KB; main agent retrieves each atom via `zerops_workflow action=dispatch-brief-atom`.

**Evidence**:
- [`flow-showcase-v36-main.md`](flow-showcase-v36-main.md) row 151 (`2026-04-21T15:21:47`): feature-sweep-stage complete response includes readmes envelope with 15 atomIds.
- Rows 152–166: 15 `dispatch-brief-atom` calls in sequence (10 body atoms + 5 principles).
- Per-response sizes: mandatory-core 2 883 B, fresh-context-premise 2 363 B, canonical-output-tree 2 499 B, content-surface-contracts 11 532 B (largest), classification-taxonomy 6 371 B, routing-matrix 5 705 B, citation-map 6 327 B, manifest-contract 4 823 B, self-review-per-surface 6 901 B, completion-shape 2 837 B, principles.file-op-sequencing 1 754 B, principles.tool-use-policy 2 294 B, principles.fact-recording-discipline 4 565 B, principles.comment-style 2 191 B, principles.visual-style 1 429 B (extractor reports 0 — see §S-1; actual body non-empty).
- Max single response: **11 532 B** (briefs.writer.content-surface-contracts). Cleanly under the 32 KB `dispatch-brief` per-response ceiling (B-9 gate).
- Writer-1 dispatch prompt at row 168 = 16 842 chars; includes the stitched brief body (no reference to any spillover file). Compare v35's writer-1 dispatch which was 28 KB with a spillover reference.
- Writer-1 + 1 fix pass completed in 2 round-trips (rows 168 → 170 → 171). v35 needed 3 writer dispatches for the same subject.

**Regression signals**: none. No tool-result > 32 KB. No spillover file referenced in any sub-agent dispatch prompt. Envelope stitch order honored.

---

### F-2 (check Detail Go notation, Cx-CHECK-WIRE-NOTATION) — **PASS**

**Closure mechanism**: check `Detail` strings name wire contracts by JSON key (`fact_title`), not Go struct.field (`FactRecord.Title`). Lint test at [`internal/tools/workflow_checks_lint_test.go`](../../../internal/tools/workflow_checks_lint_test.go) enforces this build-time; v36 runtime evidence should show zero Go-notation in any check Detail.

**Evidence**:
- grep of main-session.jsonl for `"detail":"[^"]*FactRecord\.[A-Z]"` → **0 matches**.
- grep of `FactRecord\.[A-Z]` or `ContentManifest[A-Za-z]*\.` across the full JSONL → only 2 matches, both inside atom body prose of `briefs.writer.manifest-contract`:
  - `"fact_title — copy the FactRecord.Title value character-for-character from the facts log"` — pedagogic naming (JSON key + Go source-of-truth reference).
  - `"Honor the recorded route when present. FactRecord.RouteTo may be set by the recording sub-agent"` — pedagogy.
- No check detail contains Go struct dot-notation. The lint-protected surface is clean.

**Regression signals**: none. Atom content naming both JSON key and Go struct field in explanatory prose is **intentional** and reviewer-reviewed (the atom's purpose is to teach the writer the wire→source mapping). F-2 was scoped to check detail strings, which remain JSON-only.

---

### F-3 (iterate fake-pass, Cx-ITERATE-GUARD) — **UNREACHED (fix-shipped)**

**Closure mechanism**: `action=iterate` sets `RecipeState.AwaitingEvidenceAfterIterate=true`; substep-complete calls rejected with `MISSING_EVIDENCE` until a successful `zerops_record_fact` clears the gate.

**Evidence**:
- grep of main-session.jsonl for `"action":"iterate"` → **0 matches**. The agent never hit an iteration retry loop.
- grep for `MISSING_EVIDENCE` → 0 matches. Guard text was never surfaced.
- Deploy checks failed at one substep (`feature-sweep-dev` cache returning 400) and one step (`deploy` second-attempt read-me checks). Both were resolved by Edit+redeploy, not by iterate. The writer's second pass was a fresh Agent dispatch, not an engine iterate.

**Regression signals**: none. F-3 was not exercised because the agent never chose to use `action=iterate`. The build-time guard test (`TestRecipeComplete_AwaitingEvidenceAfterIterate`) stands in for runtime evidence.

---

### F-4 (skip-attempt on mandatory step, telemetry-only) — **PASS (gate held; recovery path clean)**

**Closure mechanism** (telemetry only; engine refusal was already correct pre-rollout): any `action=skip` on `deploy`/`generate`/`provision`/`research` steps is diagnostic of upstream defect pressure (F-1/F-2/F-3).

**Evidence**:
- [`flow-showcase-v36-main.md`](flow-showcase-v36-main.md) row 122 (`2026-04-21T15:09:38`): `zerops_workflow action=skip step=deploy substep=browser-walk` → `INVALID_PARAMETER: Recipe skip failed: ... "deploy" is mandatory and cannot be skipped, suggestion: Only the close step can be skipped`.
- Agent trigger: 4 consecutive agent-browser timeouts against the dev subdomain (rows 112, 113, 115, 117) — the tool was environmentally broken on the zcp container (see §S-2), and the agent tried to skip past the substep.
- Agent recovery: after the skip refusal, agent pivoted to Bash curl sweep (row 134 confirming HTTP=200 on appdev dev subdomain), recorded a `platform_observation` fact about agent-browser instability (row 136), and completed the browser-walk substep with soft-pass attestation citing the alternative evidence (row 137).
- This is the *intended* F-4 shape: skip refused, agent finds an alternative evidence path, records the observation so downstream fact consumers see the caveat, completes.

**Regression signals**: none. Skip-attempt count: 1. Upstream defect pressure that drove it: environmental (agent-browser / zcp CDP issue), not a recipe-workflow defect. Recorded as S-2 secondary observation.

---

### F-5 (unknown-topic hallucinations, Cx-GUIDANCE-TOPIC-REGISTRY + Cx-PLAN-NIL-GUIDANCE) — **PASS**

**Closure mechanism**: unknown topics return top-3 Levenshtein matches; predicate-match-empty surfaces `TOPIC_EMPTY`; `RecipeResponse.GuidanceTopicIDs` lists closed universe on start. **Hotfix v8.108.1** (Cx-PLAN-NIL-GUIDANCE): at research step with nil plan, tier-only predicates evaluate against a synthetic `{Tier: state.Recipe.Tier}` plan so showcase-gated topics resolve.

**Evidence**:
- grep of main-session.jsonl for `does not apply` → **0 matches**.
- grep for `unknown guidance topic` → **0 matches**.
- grep for `TOPIC_EMPTY` → **0 matches**.
- [`flow-showcase-v36-main.md`](flow-showcase-v36-main.md) row 5 (`2026-04-21T15:15:23`): `zerops_guidance topic=recipe-types` at research step (plan still nil) → 1 937 B content. Confirms Cx-PLAN-NIL-GUIDANCE serving live: `recipe-types` is gated on `isShowcase` predicate, which would have returned false against a nil plan pre-hotfix.
- Row 6: `zerops_guidance topic=showcase-service-keys` at research → 1 349 B content. Same confirmation.
- Rows 21–23 (post-provision, plan submitted): `dashboard-skeleton` 2 695 B, `dual-runtime-urls` 8 646 B, `worker-setup` 897 B. All resolved.
- Rows 183, 184, 186 (finalize): `env-comments` 8 469 B, `project-env-vars` 3 417 B, `env-comments-example` 3 926 B. All resolved.
- Total `zerops_guidance` calls: 9. Total unknown/empty/does-not-apply responses: 0.

**Regression signals**: none. v8.108.1 closed the v36-attempt-1 regression that would have fired here (see F-7 below).

---

### F-6 (knowledge engine misses manifest atoms, Cx-KNOWLEDGE-INDEX-MANIFEST) — **UNREACHED-no-query (fix-shipped)**

**Closure mechanism**: wire-contract atoms (`manifest-contract`, `routing-matrix`, `classification-taxonomy`, `content-surface-contracts`, `citation-map`) routed via explicit keyword synonyms at `synonymBoostScore=100` in `internal/knowledge/wire_contract_synonyms.go`.

**Evidence**:
- grep for `zerops_knowledge` tool_use in main-session.jsonl → 1 call total (row 4 at research: `recipe=nestjs-minimal`, 7 378 B — the predecessor recipe reference load). **Zero calls for manifest-schema / routing / classification / etc.**
- Writer obtained manifest-contract content via `dispatch-brief-atom` envelope (row 159, 4 823 B), not via `zerops_knowledge`. The envelope-delivery path short-circuited the knowledge-query path.
- Synonym index was never exercised at runtime for v36. Build-time test `TestKnowledgeEngine_WireContractSynonyms` stands in for runtime evidence.

**Regression signals**: none. The fix is shipped and the build-time test holds. v36 doesn't prove the runtime synonym resolution works — but it also doesn't show the agent needing to fall back to knowledge-query, because envelope delivery was sufficient. Future run that does pull manifest-schema via knowledge (e.g. the writer reaches for context fallback) would exercise F-6's runtime closure.

---

### F-7 (predicate-gated topic on nil plan) — **closed pre-run via Cx-PLAN-NIL-GUIDANCE (v8.108.1)**

**Class**: when `state.Recipe.Plan == nil` at research step (before `action=complete step=research` populates it), predicate-gated topics evaluate against nil and return false, surfacing a misleading "Topic %q does not apply to your recipe shape." message.

**Evidence chain**:
- v36-attempt-1 (session `43814d9c5e09e85d`, pasted by user 2026-04-21 ~13:45 UTC): 4 consecutive `zerops_guidance` calls for `showcase-service-keys`, `dashboard-skeleton`, `worker-setup`, `recipe-types` at research step — all returned "Topic X does not apply to your recipe shape." despite `tier=showcase` passed to `action=start`.
- Root cause: [`internal/tools/guidance.go:73-74`](../../../internal/tools/guidance.go#L73-L74) — the predicate branch runs `topic.Predicate(plan)` where plan is nil; `isShowcase(nil)` returns false; the error text conflates "shape mismatch post-research" with "plan not yet submitted at research".
- Fix landed: commit `c512757` "fix(zcprecipator2): Cx-PLAN-NIL-GUIDANCE — tier-only topics resolve pre-research (v36 F-7 close)" at 2026-04-21 14:00 UTC. Released as v8.108.1 at 14:06 UTC. v36 run started at 14:14 UTC against the hotfixed binary.
- v36 run (session `7743c6d8c8a912fd`): tier-only topics (`recipe-types`, `showcase-service-keys`) resolved at research step with substantial content. Confirmed the patch worked under live conditions.
- Shape-dependent topics (`worker-setup`) still emit a *distinct* "plan not yet submitted" message when plan is nil — preserves agent's ability to fetch after plan submission. No shape-dependent topic was needed at research in v36, so this path wasn't exercised live.

**Grade**: PASS-pre-run. The defect is real, the hotfix is live, and v36 proves the fix works for the tier-only path. Registry row 16.7 captures the class.

---

### F-8 (close-step gate bypass via sessionless export) — **signal-grade open defect**

**Class**: `zcp sync recipe export` with no `--session` argument and no `$ZCP_SESSION_ID` env var skips the close-step gate by emitting `"note: no session context (--session unset, $ZCP_SESSION_ID unset); skipping close-step gate."` and proceeding. The agent can elect to force-wrap a recipe without exercising code-review or editorial-review.

**Evidence**:
- [`flow-showcase-v36-main.md`](flow-showcase-v36-main.md) row 204 (`2026-04-21T16:02:29`): `zcp sync recipe export "/var/www/zcprecipator/nestjs-showcase" --app-dir /var/www/appdev --app-dir /var/www/apidev --app-dir /var/www/workerdev --include-timeline` — explicitly no `--session`.
- Tool response (row 204 output text): `"note: no session context (--session unset, $ZCP_SESSION_ID unset); skipping close-step gate."` + TIMELINE.md missing prompt.
- Row 206: agent re-ran export after writing TIMELINE.md — same "skipping close-step gate" note, exit success, produced `nestjs-showcase-zcprecipator.tar.gz` (249 259 bytes).
- Workflow state at this point: finalize complete, close step = in_progress (row 198 response: "Recipe step 6/6: close"), no `action=complete step=close` ever called. Close step formally unrun; code-review + editorial-review + close-browser-walk sub-agents never dispatched.
- TIMELINE.md §6: "Pending on explicit user call. No code-review sub-agent run yet; no export/publish yet." — but export DID run (rows 204–206). Timeline text reflects the intended story, not the actual session.

**Impact on v36 analysis**:
- Editorial-review efficacy on live showcase run: **UNMEASURED**. v34 remains the only data point.
- Code-review catch rate: **UNMEASURED** for v36.
- Cross-surface honesty check that re-runs at close: **UNMEASURED**.
- §7a editorial-review + §8 close-browser-walk bars in calibration sheet: permanently unmeasurable for v36.

**Severity**: signal-grade for v36 (run still reached the deepest point of any zcprecipator2 showcase). Warrants Cx-CLOSE-STEP-GATE patch before v37 — either make `--session` required for export, or have the export CLI refuse to run if a live session exists for the target directory and `--session` is unset, or make the export tool call the close-step complete (with attestation) if session context is provided.

---

## Secondary observations

### S-1. Extract-flow parser miscounts base64 / unicode response sizes as 0

Row 166 (dispatch-brief-atom for `principles.visual-style`) reports `result_size=0` in [`flow-showcase-v36-main.md`](flow-showcase-v36-main.md). Raw JSONL inspection shows the response body is the full ~1 429-byte `visual-style.md` content — the atom body includes Unicode em-dashes which may have tripped the extractor's naïve length count.

Not a runtime defect. Flag for [`scripts/extract_flow.py`](../scripts/extract_flow.py) maintenance: size measurement should count decoded bytes, not raw source characters.

### S-2. agent-browser persistent CDP timeout on zcp dev container

4 consecutive `zerops_browser` calls (rows 112, 113, 115, 117) against `https://appdev-21c5-5173.prg1.zerops.app/` timed out at 60 s each. A 5th call against `https://example.com` (row 131) succeeded fully (open + get-text), then immediately retrying the dev subdomain timed out again (rows 132, 133, 135). HTTP layer confirmed healthy (curl HTTP=200 in 64 ms, row 114). This is an environmental issue with agent-browser on the zcp dev container, not a recipe-workflow defect. Recorded as `platform_observation` fact by the agent (row 136). Flag for later: re-commission v36.5 after agent-browser reliability work.

### S-3. Close step and its sub-agents never exercised

TIMELINE.md explicitly says "Pending on explicit user call" but the agent DID export (rows 204–206) via sessionless export, bypassing the gate (§F-8). Net effect: close-phase of zcprecipator2 architecture remains unmeasured since v34. v37 should commission with explicit `--session` enforcement or force the agent to call `action=complete step=close` before any export.

### S-4. Writer fix pass scope cleaner than v35

v35 writer-2-fix and writer-3-fix both dealt with structural issues (manifest shape, marker placement). v36 writer-2-fix handled cosmetic issues only: intro rewrites (>1-3 lines), `#ZEROPS_EXTRACT_END:intro#` marker trailing hash, YAML comment ratio below 30 %, explicit `process.on('SIGTERM')` block in workerdev integration guide. No wire-contract drift; no manifest shape issue. This is a clean signal that F-1 + F-2 closure let the writer produce correct structure on pass 1, leaving only style/tone for the fix pass.

### S-5. `zerops_record_fact` response sizes inconsistent

3 of the 10 `zerops_record_fact` calls returned 0-byte response per extractor (rows 85, 107, 150). Each corresponds to a fact that was successfully recorded (subsequent flow progressed and the fact appears in the manifest). Likely the same extractor-parser issue as S-1, not a runtime defect. Build-time test confirms the tool handler returns content.

---

## Cross-check against invariants

Per HANDOFF-to-I7 §"Invariants that must still hold post-v36":

| # | Invariant | v36 status |
|---|---|---|
| 1 | 121-atom tree lint-clean | green (verified post-hotfix: `make lint-local` passed during v8.108.1 release) |
| 2 | Gate↔shim one-implementation | not exercised (close step unreached) |
| 3 | §16a dispatch-runnable contract (editorial-review) | not exercised (close step unreached) |
| 4 | Fix D ordering (code-review needs editorial-review complete) | not exercised (close step unreached) |
| 5 | `BuildWriterDispatchBrief` pure composition (envelope byte-identical to inline) | **holds** — writer-1 dispatch prompt (row 168) stitches the 15 atoms in envelope order; prompt is 16 842 chars; independent unit test at [`internal/workflow/dispatch_brief_envelope_test.go`](../../../internal/workflow/dispatch_brief_envelope_test.go) covers byte-identity |
| 6 | StepCheck post-C-10 shape `{name, status, detail, preAttestCmd, expectedExit}` | **holds** — raw JSONL check responses show only these keys; no legacy fields |
| 7 | Dispatch-brief delivery ≤ 32 KB per response (B-9) | **holds** — max 11 532 B |
| 8 | Check `Detail` strings use JSON-key notation (B-10) | **holds** — see F-2 |
| 9 | `iterate` resets substep completion state (Cx-ITERATE-GUARD) | not exercised (no iterate calls) |
| 10 | `zerops_guidance` unknown → top-3 matches; no zero-byte valid-topic | **holds** — see F-5 |
| 11 | Wire-contract atoms recoverable via canonical keyword queries in `zerops_knowledge` top-3 | not exercised (no knowledge queries for manifest atoms) |

Invariants 2, 3, 4, 9, 11 are all "not exercised". None failed; they remain unmeasured.

---

## Calibration coverage gap list

Bars that v35 couldn't measure and v36 now can:

| Bar | v35 | v36 | Observed value |
|---|---|---|---|
| §3 scaffold-contract-symmetry | unreachable (scaffold passed but not post-v8.105 measurement) | measurable | holds (3 scaffolds coordinated via symbol contract) |
| §4 runtime-integrity | unreachable (deploy stuck at checker) | measurable | 5/5 features green dev + stage; round-trip verified in <500 ms |
| §5 writer-brief-integrity (Cx-BRIEF-OVERFLOW) | unreachable | measurable | envelope fired; max 11 532 B; writer needed 1 fix pass |
| §6 finalize-integrity | unreachable (never reached) | measurable | holds; 2-round (1 cosmetic fix) finalize-complete |
| §9 B-9..B-14 dispatch-integrity | partially unreachable | measurable | B-9 max 11 532 B, B-10 zero Go-notation, B-11 N/A (no iterate), B-12 zero unknown/empty, B-13 N/A (no knowledge query), B-14 one skip-attempt (caught by gate) |

Bars that v36 still can't measure:

| Bar | Reason |
|---|---|
| §7 close-integrity | close step unreached |
| §8 post-close export | close step bypassed; export ran but without session gate |
| §11a editorial-review wrong-surface CRIT | editorial-review never dispatched |
| §11a reclassification_delta (T-12) | editorial-review never dispatched |
| `writer_manifest_honesty` cross-surface re-check at close | close step unreached |

---

## Appendix A — key timestamps

| Time (UTC) | Event |
|---|---|
| 14:00:17 | v8.108.1 released (Cx-PLAN-NIL-GUIDANCE hotfix) |
| 14:14:?? | v36 run commissioned (per TIMELINE.md header) |
| 14:15:12 | `action=start workflow=recipe tier=showcase` — session `7743c6d8c8a912fd` |
| 14:15:23 | `zerops_guidance topic=recipe-types` at research step (plan nil) → 1 937 B content — Cx-PLAN-NIL-GUIDANCE confirmed live |
| 14:16:58 | `action=complete step=research` accepted (first try) |
| 14:19:50 | `action=complete step=provision` — project imported (14 services FINISHED) |
| 14:33:13 | `action=complete step=generate` first attempt → env_self_shadow check failure |
| 14:34:11 | generate re-check passed (run.envVariables cleared of self-shadows) |
| 14:39:09 | `deploy-dev` substep complete (3 dev containers built, all healthy) |
| 14:42:04 | execOnce bare-key gotcha recorded; distinct keys committed |
| 14:45:56 | Feature sub-agent dispatched (prompt 12 572 chars) |
| 14:58:30 | `subagent` substep complete (5/5 features in one author session) |
| 15:01:35 | feature-sweep-dev cache returned 400 without `?key` arg — agent fixed, re-sweep green |
| 15:09:38 | **F-4 fire**: `skip=deploy browser-walk` → engine refused |
| 15:17:19 | agent-browser fact recorded (environmental issue) |
| 15:19:28 | `verify-stage` substep complete (stage round-trip 8 ms db-timestamp delta) |
| 15:21:47 | feature-sweep-stage complete → readmes envelope emitted |
| 15:21:55–15:22:04 | 15 `dispatch-brief-atom` calls in 9 s (max 11 532 B) |
| 15:24:20 | Writer-1 dispatched (prompt 16 842 chars) |
| 15:48:26 | Writer-1 complete; `readmes` substep reported 4 cosmetic failures |
| 15:49:41 | Writer-2-fix dispatched (prompt 8 257 chars) |
| 15:56:20 | `action=complete step=deploy` accepted (second round of readmes fixes) |
| 15:59:02 | `action=generate-finalize` emitted 16 files (6 env import.yaml + 6 env READMEs + 3 per-codebase READMEs + root README) |
| 15:59:14 | finalize first attempt → 3 failures (env 4 minContainers mismatch + v2/v3 version-anchors) |
| 16:00:20 | generate-finalize re-emitted with corrected env 4 + scrubbed version anchors |
| 16:00:27 | `action=complete step=finalize` accepted — **finalize complete** |
| 16:02:24 | `zcp sync recipe export --help` — agent inspects export CLI |
| 16:02:29 | **F-8**: first sessionless export attempt → prompts for TIMELINE.md |
| 16:04:02 | TIMELINE.md written |
| 16:04:09 | **F-8**: second export → `.tar.gz` produced; close-step gate bypassed |
| 16:04:30 | Session last event |

---

## Appendix B — sub-agent dispatch prompt lengths and wall times

| Role | Dispatch prompt (chars) | Dispatch → return wall | Flagged events |
|---|---|---|---|
| scaffold-appdev | 6 188 | 59 s | 1 errored (curl output ANSI decoding) |
| scaffold-apidev | 10 663 | 33 s | 0 errored |
| scaffold-workerdev | 7 694 | 2 errored (curl cancellation cascade) |
| feature | 12 572 | 12 m 34 s | 2 errored (dev-server restart cycle) |
| writer-1 | 16 842 | 24 m 6 s | 0 errored |
| writer-2-fix | 8 257 | 2 m 53 s | 1 errored (minor read issue) |

Writer-1 wall is noticeably longer than v35's (v35 writer-1 ~15 m). Feature sub-agent wall is ~12 m vs v35's ~20 m. Scaffold subagents all under 1 minute, on par with v35. No sub-agent took an anomalously long time that would trigger T-8 wall-time blowout.

---

## Appendix C — files emitted to deliverable tree

Total files at `/var/www/zcprecipator/nestjs-showcase/`:
- 1 root `README.md`
- 1 root `ZCP_CONTENT_MANIFEST.json` (12 fact entries, confirmed per TIMELINE.md §4 readmes)
- 6 environment folders (0 — AI Agent through 5 — Highly-available Production), each with `import.yaml` + `README.md`
- 6 environment templates (e.g. `local-validator/`, `small-prod/`) — generated by finalize
- 3 per-codebase directories (`apidev/`, `appdev/`, `workerdev/`) each containing: `README.md`, `INTEGRATION-GUIDE.md`, `GOTCHAS.md`, `CLAUDE.md`, `zerops.yaml`, full source tree

Exported archive: `/var/www/nestjs-showcase-zcprecipator.tar.gz` (249 259 bytes). Per F-8, exported without close-step gate.
