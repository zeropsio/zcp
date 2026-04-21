---
machine_report_sha: 10003228c87797f577cc7f2364d9bbb1996ed7d48e1fdf60bb8c6273de49919d
checklist_sha: 53de9fda03be9aea2699164e662f3763e5219b37bec52dbd216de8746e5c56d7
---

# v37 verdict — PAUSE (fix-stack did not land at runtime; new defect class F-17 surfaced)

**Run**: nestjs-showcase v37 against tag `v8.109.0` (source HEAD `e6c87c0` + 6 Cx commits).
**Session**: `9c9cce67e644ae35` / Claude session `275672d8-56e8-4456-9456-7f8e5dd873c4`.
**Window**: 2026-04-21T19:28Z (session start) → 2026-04-21T21:52Z (final export).
**Outcome reached**: close-complete ([checklist B-25], engine response `toolu_01UgzsfQzogH4WrhfbQxGg8x` at 21:48:39Z confirms `progress.steps[].status="complete"` for all six phases). Deliverable exported via two post-close sessionless exports ([machine-report.session_metrics.B-21]).
**Verdict**: **PAUSE**. Re-commissioning v38 is gated on a new Cx stack (F-9 reshape + F-13 regress + F-17 atoms-to-dispatch integrity + F-21 finalize-comment-factuality + F-23 root-level-staging) plus three harness bar-sharpness patches.

---

## 1. Decision

**PAUSE** on v38 commission. Four independent signals force this:

1. **Cx-ENVFOLDERS-WIRED did not land at runtime** [checklist B-15]. The atom source at HEAD is correct (`{{range .EnvFolders}}`) but the writer dispatch prompt at [flow-showcase-v37-dispatches/author-recipe-readmes-claude-md-manifest.md:57-64](flow-showcase-v37-dispatches/author-recipe-readmes-claude-md-manifest.md) contains hand-interpolated slug names (`ai-agent`, `remote-dev`, `local-dev`, `stage`, `prod`, `prod-ha`). Writer dutifully created 6 slug-named env dirs which now live at `environments-generated/{slug}/README.md` alongside the 6 canonical finalize-emitted dirs. F-9 shape changed; root cause did NOT close [machine-report.structural_integrity.B-15_ghost_env_dirs observed=0 BUT bar scope too narrow].

2. **Cx-STANDALONE-FILES-REMOVED did not land at runtime** [checklist B-18]. Atom source at HEAD removes the standalone bullets (commit `3fca235`) but the writer dispatch prompt at [flow-showcase-v37-dispatches/author-recipe-readmes-claude-md-manifest.md:52-53](flow-showcase-v37-dispatches/author-recipe-readmes-claude-md-manifest.md) still lists `INTEGRATION-GUIDE.md` + `GOTCHAS.md` as canonical per-codebase paths. Writer authored all 6; they staged into the deliverable. F-13 **not closed** [machine-report.structural_integrity.B-18_standalone_duplicate_files observed=6 status=fail].

3. **B-20 worsened vs v36** [machine-report.session_metrics.B-20 observed=5]. v36 had 4 writer-compliance retry rounds; v37 had 5 (plus 2 additional finalize retry rounds). Writer-compliance defects F-14/F-15/F-16 persist and add a new sibling F-21 (finalize comment factuality) [checklist retry-cycle cycle 6-7].

4. **New defect class F-17: envelope content loss** [checklist dispatch-integrity writer-1]. The engine's `dispatch-brief-atom` response for `briefs.writer.canonical-output-tree` at session time returned the pre-Cx atom body (still contained `{{.ProjectRoot}}/{h}/INTEGRATION-GUIDE.md` + `{{index .EnvFolders i}}` literals — see raw tool-result `brepzqpg1.txt`). This means the container's zcp binary was either rebuilt from a pre-Cx source tree, or there is a render-path bug where `LoadAtomBodyRendered` is bypassed. Either way, ALL four atom-level Cx fixes (F-9 wire, F-12 marker form, F-13 standalone removal, plus F-21 self-review) are invisible to the running system.

ROLLBACK-Cx is NOT the right call: the six Cx commits at source HEAD are each individually correct per their RED tests; the failure is downstream of the commits (binary build + dispatch envelope integrity), not in the commits themselves.

ACCEPT-WITH-FOLLOW-UP is NOT available: more than 2 writer-compliance classes still fail [checklist retry-cycle rows 1-5] and fix-stack targets F-9 + F-13 regressed visibly in the deliverable.

PROCEED is NOT available: B-18 FAIL and the B-20 regression each alone disqualify.

---

## 2. Defect closure matrix

| Target | Status | Evidence |
|---|---|---|
| F-8 sessionless-export gate (Cx-CLOSE-STEP-GATE-HARD) | CLOSED-for-live-session / **bar-false-positive** for post-close | [checklist B-21] shows 2 sessionless exports at 21:49Z + 21:52Z AFTER close completed at 21:48:39Z; export.go:109 stderr `note: no session context ... skipping close-step gate` — Cx hard-block correctly skips when LIVE session does not match `OutputDir`. No live-session export attempt was observed in v37. Gate closure unverified at the critical path; F-8 UNREACHED-fix-shipped-for-relevant-case. |
| F-9 ghost env dirs (Cx-ENVFOLDERS-WIRED) | **NOT CLOSED** — mutated | [checklist B-15] observed=0 under `environments/`; 6 slug-named dirs under `environments-generated/` per filesystem walk. Writer dispatch prompt [flow-dispatches/author-recipe-readmes:57-64] hardcodes slug names; dispatch-brief-atom returned pre-Cx atom body (raw tool-result `brepzqpg1.txt`). |
| F-10 writer markdown stranded (Cx-CLOSE-STEP-STAGING) | **PARTIALLY CLOSED** | [checklist B-16] observed=6 — per-codebase README.md + CLAUDE.md present in deliverable. BUT root-level `/var/www/ZCP_CONTENT_MANIFEST.json` authored by writer is MISSING from deliverable — new defect F-23 (see §4). |
| F-11 close-step gate advisory (folded into F-8) | CLOSED-at-source / bar-false-positive | Same evidence as F-8. |
| F-12 marker exact form (Cx-MARKER-FORM-FIX) | **CLOSED** | [machine-report.B-17] observed=0 status=pass. All 9 fragment markers carry trailing `#` per [apidev/README.md:7](../../../Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v37/apidev/README.md) and equivalent positions in 3 codebases. |
| F-13 standalone files (Cx-STANDALONE-FILES-REMOVED) | **NOT CLOSED** | [machine-report.B-18] observed=6 status=fail; writer dispatch prompt still prescribes both paths [flow-dispatches/author-recipe-readmes:52-53]. |
| F-14/F-15/F-16 writer compliance | **NOT CLOSED — regressed by 1 round** | [checklist retry-cycle] shows 5 readmes-substep retry rounds vs v36's 4. Classes: missing/wrong markers, heading level, blank-after-marker, knowledge_base_gotchas, intro_length, authenticity, gotcha-distinctness, cross-readme-uniqueness. |

---

## 3. Positives holding green

The architecture continues to work in the layers the Cx stack did not touch. Each bullet cites raw evidence the reader can re-run.

- **Deploy + feature sweep + stage round-trip all functional** [checklist env-import]. 5/5 features green dev + stage per [TIMELINE.md:105-166](../../../Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v37/TIMELINE.md); worker round-trip NATS→apidev→worker→db UPDATE in <500 ms (TIMELINE:133).
- **Step gates catch real issues mid-run** — v37's finalize caught 15 factual_claims + comment_ratio failures [machine-report.retry_cycle_attributions cycle 6], agent fixed them, re-finalize passed on cycle 7 (CLOSED by inline correction [machine-report.retry_cycle_attributions cycle 7]).
- **Multi-agent orchestration + Symbol Contract** — 3 scaffolders + feature + writer + 3 writer-fixes + 3 editorials + code-review + close-static-review = 13 sub-agents, wire-format and API-shape consistent across app/api/worker per code-review's 0-CRIT verdict (TIMELINE:239-245; [machine-report.session_metrics.sub_agent_count=13]).
- **Import.yaml content quality** — [env 4/import.yaml:1-80](../../../Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v37/environments/4%20%E2%80%94%20Small%20Production/import.yaml) carries substantive per-service comments explaining decision-why, not field narration. Quality bar holds [checklist env-import-yaml].
- **Finalize-emitted canonical env READMEs** — [env 0/README.md:1-43] 42 lines, proper sections (Who/First-tier/Promotion/Operational). [env 5/README.md:1-47] 46 lines, terminal-tier framing correct [checklist env-readmes].
- **Per-codebase README + CLAUDE.md content quality** — [apidev/README.md:1-383](../../../Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v37/apidev/README.md) 6 IG items with fenced code + 6 concrete gotchas citing http-support / init-commands / object-storage / readiness-health-checks. [workerdev/README.md:1-333] 4 IG + 6 gotchas including both required showcase supplements (queue-group, SIGTERM drain). Writer output reached acceptable quality [checklist writer-readmes] after 5 retry rounds (not ideal, but the content shipped is substantive).
- **Code-review caught a real defect** — Meilisearch read-after-write gap on `/api/search` surfaced by code-review; fixed inline + redeployed per TIMELINE:239-245.
- **Editorial review self-corrected** — editorial-1 returned noisy CRITs (false positives at TIMELINE:217-219: "no 'Stage hits same DB' claim, no 'cache' hostname in local-dev"); fixer verified reviewer misreads; editorial-re-dispatch returned clean (0 CRIT / 0 WRONG / 0 STYLE, citation coverage 13/13).

---

## 4. New defects not anticipated by the fix stack

### F-17 — Envelope content loss: atom source ≠ dispatched brief

**Shape**: the zcp engine's `dispatch-brief-atom` returned an atom body that does NOT match the source-tree atom at HEAD. Writer-1's call sequence fetched 15 atoms via envelope pattern (canonical-output-tree, content-surface-contracts, etc.). Every returned body contained literal `{{.ProjectRoot}}` + `{{index .EnvFolders i}}` template syntax and pre-Cx content (INTEGRATION-GUIDE.md bullet present). The writer dispatch prompt was then composed by the main agent ad-hoc with slug-substituted values.

**Evidence**:
- Raw tool_result for atomId `briefs.writer.canonical-output-tree` (preserved at `/Users/fxck/.claude/projects/-Users-fxck-www-zcp/e5090c81-494a-433f-ba03-6aa29b011bf3/tool-results/brepzqpg1.txt`) contains `{{.ProjectRoot}}/{h}/INTEGRATION-GUIDE.md` + `{{index .EnvFolders i}}`. Current atom [canonical-output-tree.md:9-12,18-20](../../../internal/content/workflows/recipe/briefs/writer/canonical-output-tree.md) at HEAD removes standalones + uses `{{range .EnvFolders}}`.
- Main-agent error at 2026-04-21T21:15:14Z: `dispatch-brief atom "briefs.editorial-review.per-surface-checklist" unknown or unreadable` [flow-showcase-v37-main.md:224] — atom ID was hallucinated by main agent; no such file exists in [internal/content/workflows/recipe/briefs/editorial-review/](../../../internal/content/workflows/recipe/briefs/editorial-review/).

**Root-cause hypotheses** (ordered by likelihood):
1. Container's zcp binary was NOT rebuilt from v8.109.0 source before v37; embedded atoms are pre-Cx. Verifiable by capturing the binary's `-version` flag in SESSIONS_LOGS — NOT captured in v37.
2. `LoadAtomBodyRendered` is wired in `handleDispatchBriefAtom` only for some call-paths; the writer envelope uses a different code path that returns raw body via `LoadAtomBody`. Verifiable by grep across [internal/tools/workflow.go](../../../internal/tools/workflow.go).
3. Main agent paraphrased atoms when composing Task prompts instead of stitching bytes. Evidence: no literal `{{...}}` in the dispatched brief, suggesting template expansion happened client-side somewhere. Strong evidence against this hypothesis: the main agent would not have substituted slug names that aren't in the source set.

**Fix surface**: Cx-ENVELOPE-INTEGRITY. Likely needs (a) version-capture at run start (print zcp `--version` to SESSIONS_LOGS), (b) a contract-test asserting `dispatch-brief-atom` output equals `LoadAtomBodyRendered` output, (c) a runtime assertion that no `{{...}}` survives after render.

### F-21 — Finalize envComment factuality: writer invents numbers

**Shape**: writer's `env-comment-set` payload contained specific numeric claims (2 GB / 20 GB / 100 GB quota, `minContainers: 1/3`) that contradicted platform-auto-generated YAML (`objectStorageSize: 1`, `minContainers: 2`). Finalize-factual-claims checks caught all 6 envs on cycle 6 [machine-report.retry_cycle_attributions cycle 6 failing_checks]. Agent rewrote comments with aspirational phrasing per TIMELINE:200-207.

**Fix surface**: Cx-ENV-COMMENT-FACTUAL — writer atom should require numeric claims to match the rendered YAML in the same block (principle-not-number rule) or the check should allow aspirational phrasing from the start.

### F-22 — version anchor regex catches initCommand execOnce keys

**Shape**: `bootstrap-seed-v1` execOnce key flagged as a version anchor by `no_version_anchors_in_published_content` check [TIMELINE:202-207]. Key was renamed to `bootstrap-seed` across source files + zerops.yaml. This is a false positive: `-v1` inside a code-example execOnce key is not a recipe-run version string.

**Fix surface**: sharpen the version-anchor regex to only trigger on `v\d+` in prose (not inside fenced code blocks or as part of a multi-word identifier).

### F-23 — Root-level writer artifacts not staged into deliverable

**Shape**: writer authored `/var/www/ZCP_CONTENT_MANIFEST.json` + `/var/www/README.md` per session Write tool calls (agent-a42d46a31a4f05a2c). Per-codebase files staged successfully (B-16 pass), but root-level `ZCP_CONTENT_MANIFEST.json` is absent from `/Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v37/` per `find` walk. Root README.md IS present, so not a blanket root-level problem — the staging path special-cases some root files but not others.

**Fix surface**: widen Cx-CLOSE-STEP-STAGING to cover ZCP_CONTENT_MANIFEST.json (and any future root-level writer artifacts by pattern). Stage every file the writer Write'd under `/var/www/` at depth 1, not just those under recognized codebase subdirs.

---

## 5. Harness bar-sharpness issues uncovered

The harness did its job — it constrained this verdict to citable bars and forced me to check every on-disk file — but three bar definitions need sharpening for v38:

- **B-15 scope**: restrict to `environments/` only is too narrow. Should also flag any sibling directory matching `environments*` (regex `^environments[-_]?.*/$`). Current v37 result observed=0 is structurally misleading given `environments-generated/` exists one level up.
- **B-21 post-close filter**: current measurement fires on ANY `zcp sync recipe export` without `--session`. The Cx-CLOSE-STEP-GATE-HARD semantics are "block when LIVE session's OutputDir matches recipeDir" — post-close exports are legitimate. Bar needs to correlate export timestamp against workflow close-complete timestamp and ignore exports ≥5s after close (or match session state).
- **B-23 writer-detection**: observed `status=skip reason="no writer Agent dispatch observed"` [machine-report.session_metrics.B-23_writer_first_pass_failures.status=skip] despite `agent-a42d46a31a4f05a2c` description being `"Author recipe READMEs + CLAUDE.md + manifest"`. Bar lookup is probably matching on older role-keyword that no longer matches the description string.
- **close_step_completed false negative** [checklist B-25]: harness requires a `checkResult.passed=true` on `complete step=close` call. Actual close response returns `progress.steps[].status="complete"` without a `checkResult` object. Harness needs secondary-signal detection (`progress.steps[close].status="complete"` OR `postCompletionSummary` present).

None of these invalidate the PAUSE verdict — they're harness v2 work, not v37 regressions.

---

## 6. Why not ROLLBACK-Cx

Per HANDOFF §5 Phase 5 decision tree, ROLLBACK-Cx fires when a Cx-commit introduced a worse regression than the defect it was supposed to address [machine-report.schema_version=1.0.0]. v37 shows the opposite:

| Cx-commit | Intent | v37 observation |
|---|---|---|
| `8fa9d1b` Cx-ENVFOLDERS-WIRED | wire `.EnvFolders` → render | Source at HEAD is rendered-ready (RED-test green at [machine-report.B-22]). But dispatch-brief-atom returned pre-Cx body → binary or envelope path is stale. Source commit did not regress anything. |
| `d5f0e02` Cx-MARKER-FORM-FIX | exact-form regex | CLOSED per [machine-report.B-17]. No regression. |
| `3fca235` Cx-STANDALONE-FILES-REMOVED | remove INTEGRATION-GUIDE/GOTCHAS | Atom file at HEAD has both bullets removed [machine-report.B-22 pass]. But dispatch-brief-atom returned pre-Cx body → same binary-staleness root cause as F-17. Source commit did not regress anything. |
| `b301941` Cx-CLOSE-STEP-STAGING | stage writer output | **Partially closed** [machine-report.B-16 pass]. Gap on root-level manifest (F-23). Not a regression — an incomplete closure. |
| `e6c87c0` Cx-CLOSE-STEP-GATE-HARD | refuse sessionless export when live session exists | Source is correct for live-session case (untested in v37). Advisory-skip fires for closed session as designed [checklist B-21 bar-false-positive note]. No regression. |
| `8eb7c29` mechanical analysis harness | measurement floor | Functional; bar sharpness gaps documented above [checklist B-15, B-21, B-23]. No regression. |

All six commits are source-correct. ROLLBACK-Cx has no target. The fix is an ADDITIONAL Cx stack addressing F-17, not a revert.

---

## 7. What v38 needs before commission

**Pre-v38 fix stack** (ordered by dependency):

1. **Cx-ENVELOPE-INTEGRITY** (F-17 root cause). Three sub-commits:
   - a. Emit zcp binary version + build SHA to main-session.jsonl at workflow start.
   - b. Contract test: assert `dispatch-brief-atom` response for every envelope atom equals `LoadAtomBodyRendered(id, ctx)` byte-for-byte.
   - c. Runtime assertion: any `{{...}}` surviving in dispatch-brief-atom response fails the response.
2. **Cx-ROOT-STAGING-COMPLETE** (F-23). Extend close-step staging to include every file under `/var/www/*.{md,json}` at depth 1, not just recognized codebase subdirs.
3. **Cx-ENV-COMMENT-PRINCIPLE** (F-21). Atom revision: writer env-comment-set brief requires aspirational-phrasing ("sized for moderate throughput") not numeric claims unless the number can be grep-verified against the same YAML block.
4. **Cx-VERSION-ANCHOR-SHARPEN** (F-22). Restrict regex to prose contexts (outside fenced code blocks, outside identifiers that include the `-vN` suffix as part of a compound name).
5. **Cx-HARNESS-V2** (3 bar-sharpness patches + close-step signal).

**v38 commission spec** (identical to v37 except for tag):

```
TIER:             showcase
SLUG:             nestjs-showcase
FRAMEWORK:        nestjs
TAG:              v8.110.0
MUST_REACH:       close-step complete
MUST_DISPATCH:    editorial-review, code-review sub-agents
MUST_RUN:         close-browser-walk (soft-pass acceptable)
MUST_VERIFY_PRE_RUN:  zcp --version output captured in SESSIONS_LOGS shows v8.110.0
```

**v38 during-run tripwires** (per HANDOFF-to-I8 §5 Phase 3):

- Any Write to `environments*/{slug}/` where slug not in `CanonicalEnvFolders()` → halt.
- Any dispatch-brief-atom response containing `{{` → halt.
- Any writer-substep retry cycle count ≥ 3 → alert.
- `complete step=close` without a preceding editorial-review + code-review attest → block (engine-side; confirmed working in v37 per the SUBAGENT_MISUSE error at 21:09:44Z).

---

## 8. Halted / not halted

**Halted**:
- v38 commission pending the Cx stack above.
- Any downstream publish-pipeline work on nestjs-showcase recipe (deliverable structurally incomplete).

**Not halted**:
- Minimal-tier (v35.5) commission — independent; F-23 root-staging fix will benefit it too but not block.
- Harness v2 bar sharpening — prerequisite for v38 analysis fidelity.
- Atom corpus cleanups surfaced by F-21 / F-22.
- Repository hygiene: catalog sync, platform version refresh.

---

## 9. Positive delta vs v36 (confidence to continue)

- **Close step reached** [checklist B-25] for the first time. All three close substeps attested (editorial-review, code-review, close-browser-walk). v36 stalled at finalize + sessionless export; v37 ran close-phase content even if the atom-level Cx stack was invisible to it.
- **Code-review surfaced a real defect** (Meilisearch waitForTask read-after-write gap) and main agent applied inline fix + redeploy. The architecture pays off when the fix stack actually runs.
- **Editorial review self-corrected** through 3 dispatches. editorial-1 noise → editorial-2 JSON-form → editorial-re-dispatch clean. The recovery path is tractable.
- **Finalize-emitted canonical env READMEs + import.yaml comments are high quality** [checklist env-import, env-readmes]. Independent of writer output.
- **Deploy + feature + stage all work** [TIMELINE:120-166]. The integration is solid; the atom-envelope layer is what needs attention.

The architecture is salvageable. The v37 failure is not in the hard parts (multi-agent, wire contracts, deploy orchestration) — it's in the envelope integrity between atom corpus and sub-agent dispatch. Bounded engineering.

---

## 10. Lesson to institutionalize

**Atom-source-at-HEAD is not atom-content-at-run.** Every Cx commit that edits atoms must include an at-run verification: either a binary-version capture in the session log, or a dispatch-brief-atom golden-file regression test, or both. "The atom is correct at HEAD" says nothing about what the engine served during the run [machine-report.dispatch_integrity all=unverified]. F-17 is the v37 analogue of v36's "unmeasured — phase unreached-style" alibi [checklist dispatch-integrity]: the shortcut is assuming the tested layer is the layer running.

**The harness caught what intuition would have missed** — B-18 fail at observed=6 when my first instinct would have been "great, standalones removed" based on Cx-STANDALONE-FILES-REMOVED being merged. Without the harness's mechanical `find` + `Read`, I'd have written an ACCEPT verdict. The mechanical floor is load-bearing.

**The harness missed what intuition saw** — B-15 observed=0 is technically correct given its scope but structurally misleading: `environments-generated/` sits one level up. Bars must evolve with the defect shapes they measure. F-9 mutated; B-15 needs to mutate with it.

---

## 11. Appendix — decision-path audit trail

| Step | What | When |
|---|---|---|
| 1 | v8.109.0 tag shipped with 6 Cx commits + harness (source HEAD `e6c87c0`) | 2026-04-21 ~19:10 CEST |
| 2 | v37 session started | 2026-04-21T19:28Z |
| 3 | Research + provision + generate + deploy complete | 20:46Z |
| 4 | Writer-1 dispatched (`agent-a42d46a31a4f05a2c`) at 20:36Z | 20:36Z |
| 5 | 5 rounds of readmes-substep retries | 20:47Z → 20:59Z |
| 6 | Finalize cycle 1 + 2 (factual_claims + comment_ratio) | 21:01Z → 21:05Z |
| 7 | Editorial-1 → editorial-2 → editorial-re-dispatch (3 passes) | 21:13Z → 21:39Z |
| 8 | Code-review + inline Meilisearch fix | ~21:29Z → 21:45Z |
| 9 | Close step complete (all 6 phases) | 21:48:39Z |
| 10 | User commissioned post-run export (2 sessionless calls) | 21:49Z → 21:52Z |
| 11 | Analyst harness run + this verdict | 2026-04-21 23:56Z → 2026-04-22 00:18Z |

No engine-level fixes applied during this verdict window. F-17 + F-21 + F-22 + F-23 are queued for a v38 commissioning with their own HANDOFF-to-I9.
