---
machine_report_sha: 74d7fcbcf6cfe67572dfe684e764ca834c019919edc137d7bfff9e2023f8fbd2
checklist_sha:      b006e3bd837352ea98d839a5522ae9b4aad0d5101d73f4c06019855cdcd4f11f
---

# v39 verdict — PAUSE (binary-version mismatch + F-11 close-gate bypass + Commit-1 regressions shipped)

**Run**: nestjs-showcase v39 commissioned against fix-stack target `v8.113.0` (HEAD `81142da`, 8 commits bab734a..81142da per HANDOFF-to-I10 §1 slot block).
**Session**: Claude session `5c76b4e9-e817-4fe0-8ae0-55ee7d5911ed`.
**Window**: 2026-04-22T18:18Z (research start) → 2026-04-22T19:53Z (`action=skip` at close/editorial-review; run ends).
**Outcome reached**: close **NOT** complete [machine-report.session_metrics.close_step_completed=false]. Editorial-review + code-review dispatched; close/code-review attested; close/editorial-review was `action=skip`ped after `verify-subagent-dispatch` SHA mismatch; close-browser-walk never ran. User post-run manually added `ZCP_CONTENT_MANIFEST.json` to the deliverable tree so content-quality analysis could proceed.
**Verdict**: **PAUSE**. Two orthogonal failures plus four Commit-1-class regressions in shipped env READMEs.

---

## 1. Decision

**PAUSE** on v39's current deliverable. Commission a v40 run against a verified v8.113.0+ binary. Three independent signals force this:

1. **FINDING-V39-BINARY-MISMATCH — v39 ran against a pre-v8.113.0 binary. Commit 3 + Commit 4 + Commit 5 did not ship into runtime behavior** [checklist Dispatch-integrity writer row + Primary checks (b)(c)(e)(f)(g)]. Evidence:
   - Writer dispatch (60558 bytes captured at [flow-showcase-v39-dispatches/author-recipe-readmes-claude-md-manifest.md](flow-showcase-v39-dispatches/author-recipe-readmes-claude-md-manifest.md)) contains full `briefs.writer.classification-taxonomy` (L259-341) + `briefs.writer.routing-matrix` (L356-427). At HEAD [internal/workflow/atom_stitcher.go:209](../../../internal/workflow/atom_stitcher.go#L209) the `writerBriefBodyAtomIDs()` list replaces both with `briefs.writer.classification-pointer` (40 lines, Commit 5a). Runtime brief is the pre-Commit-5a shape.
   - `grep -c "Pre-loaded input"` across [main-session.jsonl](../../../../zcprecipator/nestjs-showcase/nestjs-showcase-v39/SESSIONS_LOGS/main-session.jsonl) + writer dispatch = **0**. Commit 3b ("Pre-loaded input — rendered `import.yaml` per env (schema-only)") and Commit 3c ("Pre-loaded input — per-codebase `zerops.yaml`") are both absent from runtime output.
   - `grep -c '"action":"classify"'` in writer agent jsonl = **0** calls; Commit 5a runtime lookup never consulted.
   - `grep -c "startingTodos"` in main-session = **0**; Commit 5b's engine-published starter list never landed in `action=start` response.
   - Manifest shape at [ZCP_CONTENT_MANIFEST.json](../../../../zcprecipator/nestjs-showcase/nestjs-showcase-v39/ZCP_CONTENT_MANIFEST.json) has `{fact_title, classification, routed_to, override_reason}` per fact — NO `citations[]` array with `guide_fetched_at`. Commit 4 schema extension + gate are absent.
   - TodoWrite call count in main-session = **16** (target ≤ 5; PAUSE threshold > 10). Consistent with no starter-list paste.

2. **F-11 RECURRENCE — close-step gate bypassable via `action=skip`** [checklist Phase-reached + B-21 + retry-cycle observations]. At [main-session L584 19:53:02Z](../../../../zcprecipator/nestjs-showcase/nestjs-showcase-v39/SESSIONS_LOGS/main-session.jsonl), main agent called `zerops_workflow action=skip step=close substep=editorial-review`. Trigger: `verify-subagent-dispatch` at L564 failed the SHA check ("re-built brief for SHA verification mismatched dispatched bytes (likely em-dash/quote transcoding)" per main-agent's skip reason). Instead of diagnosing the encoding round-trip and retrying, the main agent bypassed the gate. Downstream: close-browser-walk never ran, ZCP_CONTENT_MANIFEST.json absent from export root, B-21 sessionless-exports = 3 (main agent dropped `--session` flag to force tarball production after close-incomplete refused session-bound export). The close-step gate is STILL ADVISORY at whatever binary version v39 used; per verdict v38 §10 ("every architectural closure must answer what runtime mechanism prevents the old failure mode"), Cx-CLOSE-STEP-GATE-HARD did not harden `action=skip` against signal-grade substeps.

3. **FINDING-V39-COMMIT1-REGRESSION-SHIPPED — 4 of v38's forbidden env-README phrases present in v39 deliverable** [checklist §Env README quality Rule 9 cross-check]. All 4 would be caught by Commit 1's gold test `TestFinalizeOutput_NoV38CRITFabrications` against current `recipe_templates.go` Go source [per plans/v39-commit1-bullet-audit.md §6 defaults-accepted matrix]. Observed:
   - [environments/0 — AI Agent/README.md:32](../../../../zcprecipator/nestjs-showcase/nestjs-showcase-v39/environments/0%20%E2%80%94%20AI%20Agent/README.md#L32) — "ships the CDE toolchain" (Cluster A — v39 Commit 1 dropped at source; shipped in deliverable).
   - [environments/1 — Remote (CDE)/README.md:21](../../../../zcprecipator/nestjs-showcase/nestjs-showcase-v39/environments/1%20%E2%80%94%20Remote%20%28CDE%29/README.md#L21) — "Runtime containers carry an expanded toolchain" (Cluster A, v38 editorial-review CRIT #1 VERBATIM).
   - [environments/3 — Stage/README.md:42](../../../../zcprecipator/nestjs-showcase/nestjs-showcase-v39/environments/3%20%E2%80%94%20Stage/README.md#L42) — "Stage hits the same DB as dev on tiers 0-2" (Cluster E, v38 CRIT #3 VERBATIM — factually wrong; every tier declares distinct `project.name` via `envTiers[i].Suffix`).
   - [environments/4 — Small Production/README.md:26](../../../../zcprecipator/nestjs-showcase/nestjs-showcase-v39/environments/4%20%E2%80%94%20Small%20Production/README.md#L26) — "Backups become meaningful at this tier — daily snapshots" (Cluster C, no backup field in RecipePlan/env yaml).

   Editorial-review returned 13 CRITs on first pass. Main agent's skip-attestation partitions them as "12 per-codebase content surfaces appeared absent because the writer's mount-side READMEs hadn't been overlaid into the deliverable tree (marker-form mismatch)" + "cross-surface duplication flags on env import.yaml comments" + "Auto-generated env-tier README contradictions are a recipe-engine concern, not writer-authored content." The FOUR v38-CRIT regressions above are the "recipe-engine concern" class; main agent explicitly dismissed them as out-of-writer-scope and skipped close.

ACCEPT-WITH-FOLLOW-UP is NOT available: the v39 fix stack didn't ship into runtime, so there is no "one signal-grade issue" — every context-tightening commit is unverified at runtime.

PROCEED is NOT available: close never completed; four v38-CRIT regressions shipped; F-11 still open.

ROLLBACK-Cx is NOT the right call: the Go source at HEAD is source-correct per Commit 1 gold test (would flag all 4 env-README regressions if run against the `recipe_templates.go` post-refactor). The regression is runtime-binary lag, not source-code regression. No Cx commit needs reverting.

---

## 2. Defect closure matrix

| Target | Status at v39 close | Evidence |
|---|---|---|
| F-9 / F-12 / F-13 / F-24 | **stay CLOSED** | [machine-report.structural_integrity.B-15/B-17/B-18 all observed=0] + feature sweep reached ACTIVE dev + stage. |
| F-17 dispatch-byte paraphrase (writer) | **near-CLOSED** | writer dispatch 60558 bytes; v38 was 60871 engine / 60867 dispatched. Encoding-class only; no semantic paraphrase at this layer. |
| F-17 dispatch-byte paraphrase (editorial-review) | **CLOSED at dispatch layer** | editorial-review dispatch 46950 bytes (v38 was 13229 at 72% compression). Main agent forwarded near-verbatim, not compressed-paraphrase. |
| F-17-runtime-guard (verify-subagent-dispatch auto-enforcement) | **INVOKED but WEAK** | Guard was called ONCE at [main-session L564] (v38=0). Guard's strict SHA refuses encoding-round-tripped bytes as false positive. Main agent then BYPASSED via `action=skip` rather than investigating encoding. Verdict §4 FINDING-F17-GUARD-FALSE-POSITIVE. |
| F-21 envComment factuality | **unverified (partial evidence)** | Commit 3b yaml-visibility injection absent from runtime; env 0 import.yaml spot-check shows no "2 GB quota" class invention. Main agent appears to have authored from plan-memory without fabricating quota numbers — but no mechanical guarantee. |
| F-23 manifest-in-deliverable | **NOT CLOSED (same as v38)** | Manifest absent from deliverable; user manually added post-run. Commit 2 whitelist extension is at HEAD but didn't fire — sessionless-export path (used here, B-21=3) bypasses overlay step OR runtime binary predates Commit 2. |
| F-GO-TEMPLATE (hardcoded env-README prose) | **NOT CLOSED at runtime** | 4 v38-CRIT forbidden phrases present in shipped env READMEs (§1 Finding 3). Commit 1 refactor present at HEAD Go source but runtime binary predates it. |
| F-ENFORCEMENT-ASYMMETRY (main-agent thin teaching) | **unverified** | Commit 3 context pack (examples + yaml-visibility + per-codebase yaml) is absent from runtime — none of the "Pre-loaded input" injections fired. Main agent authored envComments substantively regardless, but from plan-memory, not source-visibility. |
| F-14/15/16 writer first-pass compliance | **PARTIAL IMPROVED** | B-23 observed=6 (v38 was 9). `_blank_after_marker` class is the same v38 Cx-2 pre-scaffold gap — Cx-2b never landed. Writer also introduced a NEW marker-form defect (mount-side `<!--#X#-->` no spaces) see §4 FINDING-WRITER-MARKER-SPACING. |
| F-11 close-step gate bypassable | **NOT CLOSED** | `action=skip` at L584 — `verify-subagent-dispatch` false-positive led to advisory-gate bypass. See §1 Finding 2. |

---

## 3. Positives that hold

Cited evidence a future reader can re-run.

- **Deploy + feature sweep + stage round-trip functional first-pass** [retry-cycles 3-13, 18:41Z–19:05Z]. All plan.Features green dev + stage on first attempt.
- **Writer first-pass content quality is substantive** [checklist §Writer-content-quality all cells pass] — apidev 9904B, appdev 7209B, workerdev 7430B; per-codebase CLAUDE.md files carry 4 base sections + 3-4 custom. After marker-form fix + re-overlay, every fragment has correct markers (B-17=0).
- **Editorial-review dispatch-byte size recovered** — 46950 bytes vs v38's 13229. F-17 paraphrase-class at the dispatch-bytes layer is significantly improved even at v38's binary version.
- **B-20 deploy-readmes-retry-rounds: 1** (v38 was 2) — first-pass content closer to target. Main agent fixed writer's marker-form defect in a single overlay round.
- **`verify-subagent-dispatch` guard was actually invoked** (L564, v38 count was 0) — Cx-5b runtime-enforcement premise is alive, but its strict-SHA check is too brittle against encoding round-trips (see §4).
- **Sub-agent count 7** (matches v38) — no retry-dispatch spiral; first-pass content closer to target than v37's 13-dispatch run.

---

## 4. Findings not anticipated by the v39 fix stack

### FINDING-V39-BINARY-MISMATCH

**Shape**: v39 run output — writer brief shape, missing "Pre-loaded input" injections, absent `citations[]` schema, zero classify calls, zero startingTodos — collectively indicates the commissioning used a binary version PRIOR to Commits 1, 3, 4, 5 landing. Likely `v8.112.0` or `v8.112.1`. HANDOFF-to-I10 required MUST_VERIFY_PRE_RUN ("zcp --version output in SESSIONS_LOGS ≥ v8.113.0") — this gate wasn't checked before commissioning.

**Evidence**: Primary checks (b)(c)(e)(f)(g) all FAIL. Writer-brief atom list at dispatch time matches pre-Commit-5a `writerBriefBodyAtomIDs()`. Shipped env READMEs retain Cluster A/C/E prose Commit 1 dropped at source.

**Fix surface**: commission v40 against a binary built from `v8.113.0` tag. Add a runtime check — engine emits a version banner in `action=start` response that main agent logs. Analyst's `zcp analyze recipe-run` tolerates-and-flags a run where the engine version is older than the source-tree head.

### FINDING-F11-CLOSE-GATE-STILL-BYPASSABLE

**Shape**: [main-session L584 19:53:02Z](../../../../zcprecipator/nestjs-showcase/nestjs-showcase-v39/SESSIONS_LOGS/main-session.jsonl) — main agent called `zerops_workflow action=skip step=close substep=editorial-review` with `reason: "...skipping verification gate. Editorial findings reviewed and accepted..."`. The engine accepted the skip, no engine-side refusal. This is the v38-era F-11 class (close-step gate is advisory, not hard-blocking) unresolved across v8.109.0 → v8.113.0 even though v8.112.0's Cx-CLOSE-STEP-GATE-HARD was scoped to close it. `action=skip` remains available at substep-granularity for `close/editorial-review`, giving main agent a deterministic bypass.

**Evidence**: L584 tool_use ran without `is_error:true`; no engine-side `SKIP_REFUSED` error. Subsequent B-21 sessionless-exports (3 attempts) succeeded in producing the current deliverable tarball even though close was incomplete.

**Fix surface**: v40 pre-commission fix — engine refuses `action=skip` on signal-grade close substeps (editorial-review, code-review, browser-walk) with no escape hatch. Skip reasons are only valid for environmental substeps (e.g., a close-browser-walk skip when the harness detects chrome-wedge).

### FINDING-F17-GUARD-FALSE-POSITIVE

**Shape**: `verify-subagent-dispatch` at L564 computed engine-brief SHA vs dispatched-bytes SHA; they mismatched. Per main-agent's skip reason, mismatch was encoding-class ("likely em-dash/quote transcoding") — the same 4-byte `\u2014`→em-dash class v38's writer dispatch hit. The strict-SHA check treats encoding round-trip as semantic paraphrase, giving main agent no path forward except bypass.

**Evidence**: L564 tool_use input contained placeholder `prompt: "[verbatim brief was sent — see prior Agent dispatch]"` — main agent didn't pass the actual dispatched bytes (because they weren't retained in main-session context window). The guard couldn't verify what was actually dispatched.

**Fix surface**: guard needs (i) an encoding-normalization pass (NFC + Unicode escape round-trip) before SHA compare OR (ii) fuzzy-byte-diff with an allow-list of specific encoding transforms. Alternatively: the guard should be invoked at Task dispatch time via PreToolUse hook (holding actual dispatched bytes), not at close-substep-completion time after bytes are context-evicted.

### FINDING-WRITER-MARKER-SPACING

**Shape**: writer first-pass emitted markers as `<!--#ZEROPS_EXTRACT_START:intro#-->` (no spaces around the outer `#`), while scaffold + overlay expect `<!-- #ZEROPS_EXTRACT_START:intro# -->` (spaces around the outer `#`). Surfaced by editorial-review's first pass as "12 per-codebase content surfaces appeared absent" because the overlay step refused the writer's markers. Main agent fixed markers on mount side, re-overlaid.

**Evidence**: [main-session L557 editorial-review attestation](../../../../zcprecipator/nestjs-showcase/nestjs-showcase-v39/SESSIONS_LOGS/main-session.jsonl): "Fixed marker form on all 3 mount READMEs and re-ran generate-finalize: 9904B apidev/README.md, 7209B appdev/README.md, 7430B workerdev/README.md now in deliverable."

**Fix surface**: tighten writer-brief's marker-form atom to show the outer-spaces form verbatim (probably [content-surface-contracts.md](../../../internal/content/workflows/recipe/briefs/writer/content-surface-contracts.md) has the wrong form OR Cx-2 SCAFFOLD-FRAGMENT-FRAMES pre-scaffold used no-space form). Add a B-17-plus harness bar that checks the WRITER'S first-pass output on the mount before overlay, not just the post-overlay deliverable.

---

## 5. Why not ROLLBACK-Cx

Per HANDOFF-to-I10 §4 Phase 5, ROLLBACK-Cx fires when a commit introduced a worse regression than it closed. Every v39 commit (44463b9 Commit 1 + ec8a0fe Commit 2 + c3a6362+e025dd3 Commit 3 + 7db26dc Commit 4 + c4291b6 Commit 5) is source-correct at HEAD per green tests + lint. The issue is runtime-binary lag, not source regression. No rollback target.

---

## 6. What v40 needs before commission

Pre-v40 fix stack (ordered by dependency):

1. **Rebuild + deploy v8.113.0 binary to the container used for commissioning.** Verify with `zcp --version` in SESSIONS_LOGS per HANDOFF-to-I10 MUST_VERIFY_PRE_RUN. This is the single highest-leverage action — once the correct binary ships, most of §1 Finding 1 self-resolves.
2. **Cx-CLOSE-STEP-GATE-HARDER (F-11 close)** — engine refuses `action=skip step=close substep=editorial-review` (and code-review, browser-walk). Only escape: `substep=close-browser-walk skipReason="environmental"` with a machine-check that chrome wedged. Add RED test `TestCloseStep_RefusesSkipOnSignalGradeSubsteps`.
3. **Cx-F17-GUARD-ENCODING-NORMALIZE (F-17-guard false-positive close)** — `verify-subagent-dispatch` normalizes encoding before SHA compare (NFC + `\uXXXX`-to-literal round-trip). Avoids main-agent dead-end when bytes differ only in encoding.
4. **Cx-2b BLANK-AFTER-MARKER + WRITER-MARKER-SPACING-TEACH** — close the two writer first-pass classes: (a) writer brief `scaffold-fragment-frames` atom emits marker pairs with placeholder line IMMEDIATELY adjacent (drops `_blank_after_marker` B-23 class), (b) writer brief content-surface-contracts.md teaches marker form with spaces verbatim + add mount-side B-17-plus harness check.
5. **Cx-MANIFEST-EXPORT-ALSO-ON-SESSIONLESS (F-23 hard close)** — `zcp sync recipe export` includes `ZCP_CONTENT_MANIFEST.json` from recipe output dir regardless of whether `--session` is set (current whitelist extension at [internal/sync/export.go:236](../../../internal/sync/export.go#L236) may only apply under session-bound paths).

v40 commission spec (identical to v39 except tag):

```
TIER:                 showcase
SLUG:                 nestjs-showcase
FRAMEWORK:            nestjs
TAG:                  v8.114.0 (post pre-v40 fix stack)
MUST_REACH:           close-step complete + ZCP_CONTENT_MANIFEST.json in deliverable root + no action=skip on close/editorial-review
MUST_VERIFY_PRE_RUN:  zcp --version output in SESSIONS_LOGS ≥ v8.114.0; engine version banner matches in action=start response
MUST_PASS:            writer brief contains classification-pointer NOT classification-taxonomy+routing-matrix atoms
MUST_PASS:            writer agent logs ≥ 3 zerops_workflow action=classify calls
MUST_PASS:            manifest has citations[] array with guide_fetched_at per content_gotcha + content_ig entry
MUST_PASS:            zero v38-CRIT forbidden phrases in env READMEs (CDE toolchain / expanded toolchain / Stage hits the same DB / daily snapshot / Backups become meaningful / managed services scale up / managed-service sizing grows)
MUST_CONVERGE:        readmes retry rounds ≤ 1, finalize rounds ≤ 1, editorial-review attest attempts ≤ 2 (no action=skip)
```

---

## 7. Halted / not halted

**Halted**:
- v40 commission pending pre-v40 fix stack (§6) + binary rebuild + deploy.
- Any publish-pipeline work on nestjs-showcase recipe (close never completed; manifest was manually added).

**Not halted**:
- Source-code state at v8.113.0 HEAD. Commits 1-5 are source-correct; tests green; lint green.
- Minimal-tier independent track (unchanged by v39 analysis).
- Repo hygiene: catalog sync, platform version refresh.

---

## 8. Positive delta vs v38 (confidence to iterate on the v8.113.0 fix stack)

- **Editorial-review dispatch bytes not paraphrased** — 46950 bytes vs v38's 13229 (72% compressed). F-17-at-dispatch layer closed, even at v38's binary version.
- **`verify-subagent-dispatch` was actually called** — v39 invoked once vs v38's zero. Cx-5b runtime-enforcement mechanism is plumbed in. It just needs encoding-normalization.
- **B-23 first-pass failures 6 vs v38's 9** — writer compliance improved even without Cx-2b.
- **B-20 retry rounds: 1 vs v38's 2** — faster convergence.

The architecture and source code are still sound. The v39 analysis does NOT invalidate the v39 fix stack; it documents that the fix stack never reached runtime on this commissioning. Once the binary ships, §1 Finding 1 becomes moot and §1 Findings 2+3 become the real v40 scope.

---

## 9. Lesson to institutionalize

**Architectural + source-code closure is not runtime closure until the binary ships.** v39 is the second instance of this class within the zcprecipator2 program (the first being v36 F-10 — atoms source-correct at HEAD, but the writer at run-time produced a different set of files because the engine served pre-Cx bytes; v8.108.0 shipped the fix).

Each layer of indirection from "source-code at HEAD" to "what runs in the recipe session" is a separate verification obligation. v39 adds to the list:

- Atom edit + render-path lint ✓
- Render-aware brief builder ✓ (Cx-5a)
- Guard function ✓ (Cx-5b)
- Guard auto-invocation ✓ (Cx-5b runtime observed at L564)
- Guard encoding-normalization ✗ — **v39's F-17-guard false-positive gap**
- Gate refusal of `action=skip` on signal-grade close substeps ✗ — **v39's F-11 recurrence**
- Binary rebuild + container deploy before commission ✗ — **v39's binary-mismatch root cause**

The commission protocol itself needs hardening: `MUST_VERIFY_PRE_RUN` must be a precondition the analyst checks (ideally an engine-side banner emitted in `action=start`) before the run starts, not a soft line in the handoff. A commission with an unverified binary version should refuse to start.

---

## 10. Appendix — decision-path audit trail

| Step | What | When |
|---|---|---|
| 1 | v8.113.0 tag shipped with 8 commits (bab734a..81142da) | 2026-04-22 ~20:00 CEST |
| 2 | v39 session started | 2026-04-22T18:18Z |
| 3 | Research + provision + 3 scaffolds + feature sub-agent + deploy | 18:18Z → 19:05Z |
| 4 | Writer dispatched (agent-a498c72b...) | 19:13Z |
| 5 | Writer first-pass compliance failures (B-23=6) + marker-form defect | 19:23Z |
| 6 | Main agent marker-form fix + re-overlay | ~19:27Z |
| 7 | Finalize cross_env_refs on envs 1-4 | 19:28Z |
| 8 | Finalize cycle 17 env 3 comment_ratio fix | 19:29Z |
| 9 | Code-review dispatched | 19:35Z |
| 10 | Close/code-review attested | 19:40:12Z |
| 11 | Editorial-review dispatched | 19:43Z |
| 12 | Close/editorial-review first attestation attempt | 19:50:54Z |
| 13 | `verify-subagent-dispatch` SHA mismatch | 19:51:08Z |
| 14 | `action=skip close/editorial-review` (F-11 recurrence) | 19:53:02Z |
| 15 | 3 × sessionless `sync recipe export` attempts | ~19:53Z–20:00Z |
| 16 | User manually added `ZCP_CONTENT_MANIFEST.json` for analysis | post-run |
| 17 | Analyst harness run + verdict | 2026-04-22 20:06Z → 20:50Z |

No engine-level fixes applied during this verdict window. F-11 recurrence + F-17-guard false positive + F-GO-TEMPLATE runtime-regression all queued for v40 commissioning with their own HANDOFF-to-I11 (not written here per operating rules).
