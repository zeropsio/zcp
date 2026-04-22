---
machine_report_sha: d756a9726cdd4f03b11994b745af47f3154cd47f6943cc0f20a6d681909bcca2
checklist_sha:      ccddfd4a97e1c3b64f070a691dfa928c02184030d0eb102b6f7be6318f3d7195
---

# v38 verdict — PAUSE (F-17 still visible at runtime; F-23 still open; two Cx-class architectural gaps)

**Run**: nestjs-showcase v38 against tag `v8.112.0` (source HEAD `b57464b`; pre-rebase target was `v8.110.0`, remote `krls2020` shipped v8.110.0+v8.111.0 on unrelated export-workflow work during v38 implementation, fix-stack rebased and shipped as `v8.112.0`).
**Session**: zerops `68a94a363294a796` / Claude session `37c13fa8-f6a2-43e5-866e-baaf4588510a`.
**Window**: 2026-04-22T09:01Z (session start, main-session.jsonl L31 research complete) → 2026-04-22T12:32Z (final export).
**Outcome reached**: close-complete [machine-report.session_metrics.close_step_completed=true]. Seven sub-agents dispatched [machine-report.session_metrics.sub_agent_count=7], every editorial-review + code-review returned without re-dispatch.
**Verdict**: **PAUSE**. Re-commissioning v39 is gated on a two-commit fix stack (Cx-5b dispatch-guard auto-enforcement + Cx-4b export-whitelist extension) plus one harness case-insensitivity patch.

---

## 1. Decision

**PAUSE** on v39 commission. Four independent signals force this:

1. **F-17 still visible in dispatch-integrity for editorial-review** [checklist B-24, dispatch-integrity editorial-review]. Engine built a 47205-byte brief via `BuildSubagentBrief(plan, SubagentRoleEditorialReview, ...)`; main agent dispatched 13229 bytes — **a 33,976-byte (-72%) compression**. First divergence at byte 6988: engine says "### Minimal tier" with 4 bullet lines, dispatched says "### Showcase tier" with 1 line. Main agent dropped the full "Minimal tier" block, 6 "Pass/Fail" explainer paragraphs (one per surface), the "Why independent" preamble, and the 32-line "The seven classes" elaboration (replaced with one line). Main agent also ADDED a "Recipe output root: ..." line not in the engine brief. This is the same paraphrase pattern v37 surfaced for raw atoms [checklist dispatch-integrity editorial-review, engine-editorial-review.txt vs dispatch-editorial-review.txt]. engineSHA `1b63737b...285a3bf3` ≠ dispatchedSHA `54e20167...039474d0`. Architecturally, `BuildSubagentBrief` + `VerifySubagentDispatch` exist and are byte-sound — the regression is not in the Go code but in the runtime wiring (see §4 F-17 diagnosis).

2. **F-23 not CLOSED — `ZCP_CONTENT_MANIFEST.json` absent from deliverable** [checklist manifest-integrity]. Writer authored at `/var/www/zcprecipator/nestjs-showcase/ZCP_CONTENT_MANIFEST.json` (four Write/Edit ops in agent-a62e64... log, 10:26Z–10:27Z). Cx-4 MANIFEST-OVERLAY correctly staged the file into the recipe output directory — [main-session.jsonl toolu_01GcSKXWTCs23Q47iSE2TVbf 10:39:51Z] returned a 17-file list including `ZCP_CONTENT_MANIFEST.json` with message "ZCP_CONTENT_MANIFEST.json overlaid from /var/www/zcprecipator/nestjs-showcase/." But `zcp sync recipe export` at [internal/sync/export.go:236](../../../internal/sync/export.go#L236) whitelists only `TIMELINE.md` and `README.md` as root-level files — the manifest is dropped at tarball creation. `find /Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v38/ -name "*MANIFEST*"` returns 0 results. Cx-4 is a **partial fix**: overlay landed, export whitelist was not extended.

3. **B-23 writer first-pass failures at 9** [machine-report.session_metrics.B-23 observed=9 status=fail]. Same count as v37. Cx-2 SCAFFOLD-FRAGMENT-FRAMES pre-scaffolded markers (B-17 pass confirms exact-form holds), but the `_blank_after_marker` class still failed on first pass because the placeholder line between markers needs no blank gap either side — pre-scaffold fixed the marker glyphs, not the spacing invariant. Writer-round-2 at [retry-cycle 15, 10:29:49Z] cleared fragment failures; writer-round-3 at [retry-cycle 16, 10:35:26Z] cleared remaining api_comment_specificity + worker_drain_code_block. v38 converged in 2 readmes-retry rounds [machine-report.session_metrics.B-20 observed=2 status=pass (at threshold)] vs v37's 5 — regression-lite, not regression-hard.

4. **F-21 ENV-COMMENT factuality still fires on first pass** [retry-cycle 17, 10:40:07Z]. Cx-3 ENV-COMMENT-PRINCIPLE atom adds the factuality rule, but writer still invented "2 GB quota" on the object-storage envComment across all 6 tiers on the first finalize pass (6 × `import_factual_claims` failures). The tightened check caught the mismatch; agent re-ran `generate-finalize` with qualitative phrasing ("modest quota suitable for agent iteration") per [main-session.jsonl 10:41:27Z tool-result message]; cycle 18 passed. **Cx-3 check enforcement works; Cx-3 atom-teaching prevention is partial.**

ROLLBACK-Cx is NOT the right call [machine-report.session_metrics.sub_agent_count=7 vs v37's 13]: none of the eight Cx commits regressed the run relative to v37 — every v37-targeted defect is materially better or closed at the filesystem layer (F-9, F-12, F-13 closed per [machine-report.structural_integrity.B-15_ghost_env_dirs observed=0] + [machine-report.structural_integrity.B-17_marker_exact_form observed=0] + [machine-report.structural_integrity.B-18_standalone_duplicate_files observed=0]; F-24 browser-chaos did not recur per [checklist retry-cycle-20]). The failures are downstream of the commits — F-17 needs guard auto-enforcement, F-23 needs export-whitelist extension.

ACCEPT-WITH-FOLLOW-UP is NOT available [checklist B-24]: two architectural defects exceed the "one signal-grade issue" gate, and F-17 is the headline issue Cx-5 explicitly targeted.

PROCEED is NOT available [machine-report.session_metrics.B-23 observed=9 status=fail] + [checklist B-24]: B-24 FAIL on all three guarded roles alone disqualifies per HANDOFF-to-I9 §5 Phase 5 decision tree ("ANY fix-stack target regressed, OR F-17 still visible in dispatch-integrity (even one byte-diff divergence)").

---

## 2. Defect closure matrix

| Target | Status at v38 close | Evidence |
|---|---|---|
| F-9 ghost env dirs (Cx-1 WRITER-SCOPE-REDUCTION) | **CLOSED** | [machine-report.B-15 observed=0 status=pass]. Canonical 6 env folders present; no `environments-generated/` parallel tree (v37 had 6 slug-named dirs there). |
| F-10 writer markdown staged per-codebase (Cx-CLOSE-STEP-STAGING) | **CLOSED** for per-codebase | [machine-report.B-16 observed=6 status=pass]. All 3 codebases have README.md + CLAUDE.md. Root-level ZCP_CONTENT_MANIFEST.json is separate — see F-23. |
| F-12 marker exact form (Cx-2 SCAFFOLD-FRAGMENT-FRAMES marker glyphs) | **CLOSED** | [machine-report.B-17 observed=0 status=pass]. All 9 fragment markers carry trailing `#` across 3 codebases. |
| F-13 standalone INTEGRATION-GUIDE.md + GOTCHAS.md (Cx-1 atom scope reduction) | **CLOSED** | [machine-report.B-18 observed=0 status=fail→pass reversal vs v37's 6]. Writer brief no longer prescribes these paths; writer authored none. |
| F-14/F-15/F-16 writer compliance (Cx-2 pre-scaffold) | **PARTIALLY CLOSED — regression-lite** | [machine-report.B-23 observed=9 status=fail] matches v37's 9, but [machine-report.B-20 observed=2] vs v37's 5. Pre-scaffold helped marker-glyph class (F-12 CLOSED) but not blank-after-marker class. |
| **F-17 envelope content loss — editorial-review** | **NOT CLOSED — headline v38 defect** | [checklist dispatch-integrity editorial-review Status=divergent]. engine 47205 B → dispatched 13229 B (72% compression); first divergence byte 6988, 460-line diff at [dispatch-integrity/diff-editorial-review.txt]. Guard is opt-in, never invoked. |
| F-17 envelope — writer | **NEAR-CLOSED** | [checklist dispatch-integrity writer Status=divergent-encoding]. 4-byte `\u2014`→em-dash delta at byte 60513; all 60,513 preceding bytes byte-identical. No semantic paraphrase. Tight SHA check would still fail. |
| F-17 envelope — code-review | **EFFECTIVELY CLOSED** | [checklist dispatch-integrity code-review Status=divergent-trivial]. 1-byte trailing-newline delta; 17656 bytes byte-identical. |
| F-21 env-comment factuality (Cx-3 ENV-COMMENT-PRINCIPLE) | **PARTIALLY closed** [checklist retry-cycle-17] | [checklist retry-cycle-17]: writer invented "2 GB quota" across 6 tiers on first pass; check caught it, retry re-ran with qualitative phrasing, [checklist retry-cycle-18] passed. Check enforcement holds; atom prevention partial. |
| F-22 version anchor false-positive (Cx-6) | **closed** [checklist retry-cycle rows 15-18 no version_anchor failures] | No `no_version_anchors_in_published_content` failures in any retry cycle [checklist retry-cycle-rows-all]. `bootstrap-seed-v1` execOnce key did not trigger the check. |
| F-23 root-level writer artifacts staged (Cx-4 MANIFEST-OVERLAY) | **NOT closed — Cx-4 partial** [checklist manifest-integrity] | Overlay landed per [main-session.jsonl 10:39:51Z]; export at [internal/sync/export.go:236](../../../internal/sync/export.go#L236) whitelists only TIMELINE.md + README.md so manifest dropped. |
| F-24 browser recovery (Cx-7 BROWSER-RECOVERY-COMPLETE) | **closed** [checklist retry-cycle-20] | [checklist retry-cycle-20] no failing checks on close-browser-walk. No ~15-minute chaos as v37; no user intervention needed. Cx-7 port from v27 archive held. |

---

## 3. Positives holding green

The architecture continues to work in the layers Cx addressed correctly. Each bullet cites raw evidence a future reader can re-run.

- **Deploy + feature sweep + stage round-trip all functional** [retry-cycles 3-13, 09:25Z–10:12Z, no failing checks]. All plan.Features green dev + stage on first feature-sweep.
- **Step gates catch real issues mid-run** — finalize caught 6 × `import_factual_claims` failures on cycle 17; agent re-ran with corrected phrasing; cycle 18 passed [main-session.jsonl 10:41:27Z tool-result].
- **Multi-agent orchestration** — 7 sub-agents dispatched [machine-report.session_metrics.sub_agent_count=7] vs v37's 13. Absence of writer-fix + editorial-re-dispatch cycles indicates first-pass content was closer to target than v37.
- **Close-step architecture reached second-time cleanly** — editorial-review returned first-pass clean [retry-cycle 18], code-review returned first-pass clean [retry-cycle 19], close-browser-walk reached [retry-cycle 20]. F-24 browser chaos did not recur per absence of RecoverFork-related failures.
- **Cx-5 `BuildSubagentBrief` architectural path is byte-sound** — code-review dispatch is 17656/17657 byte-identical modulo trailing-newline [checklist dispatch-integrity code-review]; writer dispatch is 60867/60871 byte-identical modulo 1 `\u2014`→em-dash encoding artifact [checklist dispatch-integrity writer]. The function produces the right bytes; the guard-function exists and is correct. Only editorial-review was paraphrased semantically.
- **Writer-authored per-codebase content is production-grade** — [apidev/README.md 323 lines, 5 IG items with fenced code, 3 concrete gotchas with platform-topic citations; appdev/README.md 169 lines, 4 IG, 4 gotchas; workerdev/README.md 199 lines, 3 IG, 3 gotchas with both showcase-required supplements (queue-group + SIGTERM drain)]. Every gotcha has concrete symptom + mechanism + topic citation [checklist writer-content-quality apidev/appdev/workerdev README.md all pass].
- **Writer-authored CLAUDE.md files are high-quality operator notes** — all three have 4 base sections (Dev loop / Migrations / Container traps / Testing) plus 2+ custom sections; all include repo-specific traps not in platform docs [checklist apidev/appdev/workerdev CLAUDE.md narrative-sign-off pass]. Harness auto-report `status=fail` is a case-sensitivity false-positive, not a content defect (see §5).
- **Finalize-emitted env READMEs + import.yaml comments are production-grade after F-21 correction** — [env 0/README.md:41 "initCommands do NOT fire automatically at this tier" — the AI-Agent tier distinction stated correctly]. [env 0/import.yaml:11-14 project comment explains APP_SECRET + DEV_/STAGE_ URL constants; service comments explain decision-why, not field narration].
- **F-24 browser recovery held end-to-end** — [retry-cycle 20 close/close-browser-walk] no failing checks; no wall-time chaos; no user intervention. Cx-7 port from v27 archive resolved the `RecoverFork` pkill-pattern miss that v37 suffered.

---

## 4. New findings not anticipated by the fix stack

### F-17-runtime — dispatch guard is opt-in, not auto-enforced

**Shape**: Cx-5 shipped two functions — `workflow.BuildSubagentBrief` (engine stitches + renders + SHA-hashes the brief) and `workflow.VerifySubagentDispatch` (compares a proposed Task.prompt against the last-built brief's SHA for the detected role). Both are implementation-correct. But `VerifySubagentDispatch` is ONLY invoked when the main agent explicitly calls `zerops_workflow action=verify-subagent-dispatch` — which the v38 main agent never did. There is no `PreToolUse` hook matching `Agent` in [.claude/settings.json:28-40] (only `Bash` is hooked). The engine cannot intercept a Task dispatch because Task is Claude Code's own tool, not an MCP tool — there is no server-side opportunity to gate the prompt.

**Evidence**:
- `build-subagent-brief` called 3 times [main-session.jsonl L591 writer, L725 code-review, L750 editorial-review], each followed by an `Agent` dispatch [L609, L728, L753] with the matching role-keyword description.
- Zero `verify-subagent-dispatch` calls anywhere in main-session.jsonl.
- Zero `SUBAGENT_MISUSE` errors in `is_error:true` tool_results (14 string matches exist, all inside instructional `detailedGuide` text teaching sub-agents about the error code).
- Writer + code-review dispatches are near-verbatim forwarding; editorial-review dispatch is 72%-compressed paraphrase.

**Root cause**: Cx-5 closes the *architectural* surface for F-17 (deterministic engine-built brief with SHA) but leaves the *runtime enforcement* surface open — there is no mechanism that fails a Task dispatch whose prompt doesn't match the last-built brief SHA. Main agent paraphrase still reaches sub-agents unopposed when the main agent chooses to paraphrase.

**Fix surface**: Cx-5b — either (a) `PreToolUse` hook in `.claude/settings.json` matching `Agent` that invokes `zerops_workflow action=verify-subagent-dispatch` against the current session state and blocks mismatched dispatches, or (b) post-hoc retroactive detection: when the main agent calls `zerops_workflow action=complete substep=readmes` (or equivalent), the engine scans the recent main-session for Agent dispatches whose description matched a guarded role-keyword and whose prompt didn't hash to `LastSubagentBrief[role].PromptSHA` — fails step completion with `SUBAGENT_MISUSE` and a remediation sentence. (a) is cleaner if the hook contract supports MCP round-trips; (b) is a fallback if it doesn't. Neither option exists at v8.112.0.

### F-23-runtime — Cx-4 overlay staged but export whitelist didn't grow

**Shape**: Cx-4 added `OverlayManifest` in [internal/workflow/recipe_overlay.go:66-87](../../../internal/workflow/recipe_overlay.go#L66) which reads `/var/www/zcprecipator/<slug>/ZCP_CONTENT_MANIFEST.json` and adds it to the `files` map written to `state.Recipe.OutputDir`. This worked [main-session.jsonl 10:39:51Z]. But `zcp sync recipe export` at [internal/sync/export.go:236](../../../internal/sync/export.go#L236) only passes `TIMELINE.md` and `README.md` through the root-file whitelist loop; the manifest lands in the output directory but is not included in the tarball.

**Fix surface**: Cx-4b — extend the root-file loop at `export.go:236` to include `ZCP_CONTENT_MANIFEST.json` (and by pattern any `*.json` / `*.md` root file the finalize overlay produces). One-line change plus RED test `TestExportRecipe_IncludesRootManifest`.

### B-16-harness — CLAUDE.md base-sections case-sensitivity

**Shape**: harness [machine-report.writer_compliance.apidev/CLAUDE.md.base_sections_present=["Migrations","Testing"]] reports 2-of-4 base sections found, status `fail`, across all three CLAUDE.md files. Reading the files shows all four sections present with lowercase subheadings ("Dev loop", "Container traps") — the harness match regex is case-sensitive for "Dev Loop" / "Container Traps" and misses the lowercase variants.

**Fix surface**: Cx-8b — case-insensitive match in the CLAUDE.md sectional audit (likely one regex flag flip in `internal/analyze/writer_compliance.go`). Not a writer regression — the files are correct. Same shape as v37's B-21/B-23 bar-sharpness patches.

---

## 5. Why not ROLLBACK-Cx

Per HANDOFF-to-I9 §5 Phase 5 decision tree, ROLLBACK-Cx fires when a specific commit introduced a worse regression than the defect it addressed. v38 shows the opposite:

| Cx-commit | Intent | v38 observation |
|---|---|---|
| `c9da867` Cx-1 WRITER-SCOPE-REDUCTION | remove root/env/standalone prescriptions | F-9 + F-13 closed [machine-report.structural_integrity.B-15_ghost_env_dirs observed=0] + [machine-report.structural_integrity.B-18_standalone_duplicate_files observed=0]. No regression. |
| `a185e50` Cx-2 SCAFFOLD-FRAGMENT-FRAMES | pre-scaffold markers | F-12 closed [machine-report.structural_integrity.B-17_marker_exact_form observed=0]. Partial close on blank-after-marker class (see F-14/15/16 row §2). No regression. |
| `01566ad` Cx-3 ENV-COMMENT-PRINCIPLE | factuality rule + check tightening | Check tightening holds (caught 6 × factual_claims at [checklist retry-cycle-17]); atom prevention partial. No regression. |
| `abb1bbe` Cx-4 MANIFEST-OVERLAY | stage writer manifest into finalize output | Overlay landed [main-session 10:39:51Z]. Export whitelist gap (F-23 not closed [checklist manifest-integrity] — see §4). No regression — incomplete closure. |
| `adfebcc` Cx-5 SUBAGENT-BRIEF-BUILDER | engine-built briefs + dispatch guard | Architectural surface correct [checklist dispatch-integrity code-review]; runtime enforcement gap (F-17-runtime [checklist dispatch-integrity editorial-review] — see §4). No regression — incomplete closure. |
| `73b9d2a` Cx-6 VERSION-ANCHOR-SHARPEN | skip fenced code + compound identifiers | No false-positives observed [checklist retry-cycle rows 15-21 no version_anchor failures]. No regression. |
| `eb24c52` Cx-7 BROWSER-RECOVERY-COMPLETE | port v27 pidfile + pkill --exact | F-24 closed [checklist retry-cycle-20 close/close-browser-walk no failing checks] — no browser chaos. No regression. |
| `009d729` Cx-8 HARNESS-V2 | four bar patches | [machine-report.structural_integrity.B-15 observed=0] (canonical set); [machine-report.session_metrics.B-21 observed=0 status=pass]; [machine-report.session_metrics.B-23 observed=9] (runs, not skip); close-step detection holds. No regression. |
| `787075d` fix(analyze) progress.steps shape | retrospective shape fix | Sound at HEAD [machine-report.session_metrics.close_step_completed=true]. No regression. |

Every commit is source-correct. The two open fronts (F-17-runtime, F-23-runtime) are follow-on work that Cx-5 and Cx-4 did not fully ship. ROLLBACK-Cx has no target.

---

## 6. Harness issues surfaced (not v38 regressions)

- **B-16 CLAUDE.md base-sections case-sensitivity** (see §4 B-16-harness). All three CLAUDE.md files falsely report `status=fail`. File Cx-8b regex fix.
- **B-24 dispatch_integrity not auto-populated** — harness reports `status=""` on machine-report; analyst populated by hand in the checklist. Expected per the spec — the byte-diff requires plan reconstruction that depends on per-run state. Consider adding a `analyze dispatch-diff --run v38` subcommand for v39 analysis that reads the captured dispatches + BuildSubagentBrief and emits divergences automatically.
- **close-browser-walk detection** — harness returned false on `close_browser_walk_attempted` [machine-report.session_metrics.close_browser_walk_attempted=false]. TIMELINE.md narrates the walk was attempted and returned per retry-cycle 20. Harness needs a secondary-signal detector for close-substep completion (similar to v37's B-25 bar-scope issue).

None of these invalidate the PAUSE verdict.

---

## 7. What v39 needs before commission

Pre-v39 fix stack (ordered by dependency):

1. **Cx-5b — DISPATCH-GUARD-AUTO-ENFORCE** (F-17-runtime close). Either PreToolUse hook OR retroactive detection in engine step-completion. Without this, any main-agent paraphrase still reaches guarded sub-agents unopposed — exactly the v37 failure mode Cx-5 was designed to foreclose. **Headline fix.**
2. **Cx-4b — EXPORT-ROOT-FILE-EXTEND** (F-23-runtime close). Extend [internal/sync/export.go:236](../../../internal/sync/export.go#L236) root-file whitelist to include `ZCP_CONTENT_MANIFEST.json` (and by pattern any `*.json` / `*.md` root file the finalize overlay produces).
3. **Cx-2b — BLANK-AFTER-MARKER** (B-23 reduction). Writer pre-scaffold should emit marker pairs with the placeholder line IMMEDIATELY adjacent — no blank line between marker and REPLACE-THIS comment. Would drop B-23 first-pass count.
4. **Cx-3b — ENV-COMMENT-PRINCIPLE-ATOM-STRENGTHEN** (F-21 atom-prevention). The atom teaching survived the dispatch (it's in the writer brief verbatim), but writer still invented numbers. Either strengthen the wording ("DO NOT use any integer followed by GB/MB/TB unless that exact string appears in the YAML below") or add a second-pass self-review instruction.
5. **Cx-8b — HARNESS CASE-INSENSITIVITY** (CLAUDE.md base-sections false positive).

v39 commission spec (identical to v38 except for tag):

```
TIER:                 showcase
SLUG:                 nestjs-showcase
FRAMEWORK:            nestjs
TAG:                  v8.113.0 (or higher; depends on how many Cx land)
COMMISSIONED_BY:      user
AGENT_MODEL:          claude-opus-4-7[1m]
MUST_REACH:           close-step complete + ZCP_CONTENT_MANIFEST.json in deliverable + byte-identical dispatches for all 3 guarded roles (enforced at runtime, not only at SHA-check-time)
MUST_VERIFY_PRE_RUN:  zcp --version output in SESSIONS_LOGS ≥ v8.113.0
```

---

## 8. Halted / not halted

**Halted**:
- v39 commission pending the two-Cx fix stack above.
- Any downstream publish-pipeline work on nestjs-showcase recipe (manifest absent from deliverable).

**Not halted**:
- Minimal-tier (v35.5) commission — independent; Cx-4b + Cx-5b would benefit it too but not block.
- Framework-diversity planning (laravel-showcase, python-showcase) — post-v39.
- Repository hygiene: platform version refresh, catalog sync.

---

## 9. Positive delta vs v37 (confidence to continue)

- **F-9, F-12, F-13, F-24 all closed** [machine-report.structural_integrity.B-15_ghost_env_dirs observed=0] + [machine-report.structural_integrity.B-17_marker_exact_form observed=0] + [machine-report.structural_integrity.B-18_standalone_duplicate_files observed=0] + [checklist retry-cycle-20] — four of v37's six headline defects are gone. Architecture validated for filesystem-level invariants.
- **Sub-agent count 7 vs v37's 13** — first-pass content much closer to target; no writer-fix + editorial-re-dispatch cycles.
- **Writer content quality is production-grade** across every surface read (§3 bullet 6-7); only first-pass compliance checks fired, not content-quality checks.
- **F-24 browser chaos did not recur** — Cx-7 port from v27 archive resolved the `RecoverFork` pkill-pattern miss; zero wall-time burned on Chrome wedging.
- **Cx-5 architectural path is byte-sound** — when the main agent forwards verbatim (as it did for writer + code-review), the engine brief reaches the sub-agent. The failure mode is narrower than v37's: not "main agent paraphrases atoms", but "main agent paraphrases even the engine-stitched brief when the guard is opt-in".

The architecture is still salvageable. The v38 failure is two-dimensional (one runtime-enforcement gap, one export-whitelist gap), both narrow and one-commit-each to close.

---

## 10. Lesson to institutionalize

**Architectural fixes must ship with runtime enforcement.** Cx-5's `VerifySubagentDispatch` function is correct; it just isn't wired to fire. v38 is the third instance of the same class of failure:
- v36 F-10 / v37: atom source correct at HEAD, but the writer at run-time produced a different set of files because the engine served pre-Cx bytes.
- v37 F-17: atoms correct at HEAD, but the main agent paraphrased them before dispatch because no guard refused the paraphrase.
- v38 F-17 runtime: guard function correct at HEAD, but the main agent skipped calling it because no PreToolUse hook forced invocation.

Each layer is one level of indirection away from "the code at HEAD". Each fix must ship with a runtime-enforcement pair:
- Atom edit + render-path lint ✓ (Cx-MARKER-FORM-FIX era)
- Render-aware brief builder ✓ (Cx-5a path)
- Guard function ✓ (Cx-5b path)
- **Guard auto-invocation ✗ — v38's gap** (Cx-5b extension needed)
- Export whitelist sync ✗ — v38's second gap (Cx-4b)

**Every architectural closure must answer: "what runtime mechanism prevents the old failure mode from reaching a sub-agent / a tarball / the user?"** If the answer is "the main agent is supposed to cooperate", that's not enforcement.

---

## 11. Appendix — decision-path audit trail

| Step | What | When |
|---|---|---|
| 1 | v8.112.0 tag shipped with 8 Cx commits + shape-fix (source HEAD `b57464b`) | 2026-04-22 ~09:00 CEST |
| 2 | v38 session started | 2026-04-22T09:01Z |
| 3 | Research + provision + 3 scaffolds + feature sub-agent + deploy | 09:01Z → 10:12Z |
| 4 | Writer dispatched (agent-a62e64...) | 10:26Z |
| 5 | Writer 2 readmes-retry rounds (cycles 15-16) | 10:29Z → 10:35Z |
| 6 | Finalize F-21 correction (cycle 17 → 18) | 10:40Z → 10:41Z |
| 7 | Code-review + editorial-review first-pass clean | 11:00Z → 11:01Z |
| 8 | Close-browser-walk + close-step attest | 11:03Z |
| 9 | User commissioned post-run export (2 sessionless calls) | 12:30Z → 12:32Z |
| 10 | Analyst harness run + this verdict | 2026-04-22 14:46Z → 15:15Z |

No engine-level fixes applied during this verdict window. F-17-runtime + F-23-runtime are queued for v39 commissioning with their own HANDOFF-to-I10.
