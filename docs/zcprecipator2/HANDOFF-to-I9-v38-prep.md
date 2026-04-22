# HANDOFF-to-I9-v38-prep.md — land v37-surfaced fix stack, commission v38, analyse

**For**: fresh Claude Code instance picking up zcprecipator2 after v37 analysis shipped as PAUSE. Your job is to **land the eight-Cx fix stack**, tag `v8.110.0`, hand back to the user for v38 commission, then **analyse v38** when artifacts land.

**Reading order** (~60 min):
1. This document front-to-back.
2. [`runs/v37/verdict.md`](runs/v37/verdict.md) — why v37 was PAUSE; what F-17 actually is (main-agent paraphrase); why the v8.109.0 atom-level Cx stack had zero runtime effect.
3. [`runs/v37/verification-checklist.md`](runs/v37/verification-checklist.md) — per-file evidence the verdict binds to.
4. [`plans/v38-fix-stack.md`](plans/v38-fix-stack.md) — the implementation plan for v8.110.0. THIS IS THE FILE YOU EXECUTE.
5. [`runs/v37/flow-showcase-v37-dispatches/author-recipe-readmes-claude-md-manifest.md`](runs/v37/flow-showcase-v37-dispatches/author-recipe-readmes-claude-md-manifest.md) — read the first 100 lines and compare against [`../../internal/content/workflows/recipe/briefs/writer/canonical-output-tree.md`](../../internal/content/workflows/recipe/briefs/writer/canonical-output-tree.md). Feel the paraphrase. That compression is the defect.
6. [`spec-recipe-analysis-harness.md`](spec-recipe-analysis-harness.md) — harness Tier 1/2/3 (unchanged from I8; §7 of v38-fix-stack adds v2 patches).
7. [`HANDOFF-to-I8-v37-prep.md`](HANDOFF-to-I8-v37-prep.md) §6 — analysis discipline rules. **Inherited unchanged for v38 analysis.**
8. [`../../CLAUDE.md`](../../CLAUDE.md) — auto-loaded; reread for TDD + operating norms.

**Branch**: `main`. **Previous run**: v37 (v8.109.0, close-complete but deliverable structurally broken due to F-17). **Your target**: v38 against `v8.110.0` once the eight Cx-commits land.

---

## 1. Slots to fill at start

```
FIX_STACK_TAG:            v8.112.0   (bumped from v8.110.0 — remote had already shipped v8.110.0 + v8.111.0 on unrelated work from krls2020 during v38 implementation; our branch was rebased onto origin/main before tagging)
FIX_STACK_COMMITS:        (SHAs refer to post-rebase history on main; each commit independently revertible)
                          Cx-1 WRITER-SCOPE-REDUCTION      c9da867  (F-9 + F-13 at source)
                          Cx-2 SCAFFOLD-FRAGMENT-FRAMES    a185e50  (F-12 at source)
                          Cx-3 ENV-COMMENT-PRINCIPLE       01566ad  (F-21 close)
                          Cx-4 MANIFEST-OVERLAY            abb1bbe  (F-23 close; depends on Cx-1)
                          Cx-5 SUBAGENT-BRIEF-BUILDER      adfebcc  (F-17 close — the headline fix; depends on Cx-1/2/3)
                          Cx-6 VERSION-ANCHOR-SHARPEN      73b9d2a  (F-22 close)
                          Cx-7 BROWSER-RECOVERY-COMPLETE   eb24c52  (F-24 close — ported from v27 archive)
                          Cx-8 HARNESS-V2                  009d729  (four bar patches + close-step signal)
                          fix(analyze) progress.steps     787075d  (Cx-8 retrospective shape fix surfaced by v37 replay)
HARNESS_V2_LANDED:        yes
V37_RETRO_REPORT:         /tmp/v37-retro-after-v112.json
V37_RETRO_EXPECTED_DELTAS_CONFIRMED:
                          B-15 ghost_env_dirs         observed=1  (environments-generated/ caught by depth-1 sibling scan)
                          B-21 sessionless_export     observed=0  (v37 post-close exports filtered via close_step_completed_at)
                          B-23 writer_first_pass      observed=5 fail  (runs against "Author recipe ..." description instead of skip)
                          close_step_completed        true        (progress.steps secondary signal parses the v37 array shape)
V38_COMMISSION_DATE:      <unfilled — user commissions>
V38_SESSION_ID:           <unfilled>
V38_OUTCOME:              <unfilled — post-run>
V38_VERDICT:              <unfilled — post-analysis>
```

If any slot is `<unfilled>` because the upstream phase hasn't run yet, that's expected. Fill slots as phases complete.

---

## 2. The meta-lesson from v37 (most important — different shape than I8)

The I8 meta-lesson was about analysis discipline: the v36 analyst shipped artifact-shape as proxy for depth. The harness fixes that.

**The v37 meta-lesson is different**: six Cx-commits landed at source HEAD, every RED test passed, `make lint-local` green — and **four of six fixes had zero runtime effect**. Not because the commits were wrong, but because the layer they modified (atom content) is not the layer that reaches sub-agents. The main agent reads atoms, then composes its OWN Task() prompt as a compressed paraphrase, and paraphrase-corruption is where the defects live.

Three rules fall out of this:

**Rule A — Atom-source-at-HEAD is not atom-content-at-run.** When a Cx-commit edits an atom, you MUST add at-run verification: a test asserting the final dispatched prompt contains the atom's exact bytes. "Atom is correct at HEAD" says nothing about what the engine served.

**Rule B — The dispatch boundary is the invariant boundary.** Anything between "engine builds the brief" and "sub-agent receives the prompt" must be zero-knowledge forwarding. If the main agent has prompt-composition freedom, the main agent WILL compress + hallucinate. Cx-5 makes this architectural.

**Rule C — Run the harness retrospectively before tagging.** Before v8.110.0 lands, the harness must still surface v37's defects when pointed at the v37 deliverable (observed=still-broken). This proves the harness is measuring the defects, not the fix-stack.

**Your job on v38 analysis: do NOT reproduce v37's mistake of accepting artifact structure as evidence of fix correctness.** Compare the dispatch prompt captured on disk against the Go-source builder output. Byte-diff. If they don't match, Cx-5 failed regardless of what the harness says.

---

## 3. Required reading (in order, ≈ 60 min)

1. **This document** — you're reading it.
2. **[`runs/v37/verdict.md`](runs/v37/verdict.md)** — PAUSE verdict with full cited evidence. Reading this primes the v38 analysis frame.
3. **[`runs/v37/verification-checklist.md`](runs/v37/verification-checklist.md)** — per-file evidence. Read the dispatch-integrity section; that's the shape of what v38 will verify.
4. **[`plans/v38-fix-stack.md`](plans/v38-fix-stack.md)** — the execution plan for Phase 1 (fix-stack land). Execute each Cx in order.
5. **[`spec-recipe-analysis-harness.md`](spec-recipe-analysis-harness.md)** — unchanged from I8; re-read for context of harness-v2 patches in Cx-7.
6. **[`HANDOFF-to-I8-v37-prep.md`](HANDOFF-to-I8-v37-prep.md) §6 (Analysis discipline rules)** — eight rules inherited for v38 analysis. Reread before writing any verdict prose.
7. **[`runs/v36/CORRECTIONS.md`](runs/v36/CORRECTIONS.md)** — context for why I8 existed; the analysis-failure failure mode to keep avoiding.
8. **[`../../CLAUDE.md`](../../CLAUDE.md)** — auto-loaded; re-read for TDD + operating norms.

---

## 4. v37 defect inventory (post-analysis)

Fourteen defect classes are relevant at this hand-off. F-1..F-7 are historical (closed by v8.108.x). F-8..F-13 were v37's fix-stack targets; four of them remain effectively open because of F-17 paraphrase. F-14..F-16 are writer-compliance (observation-class). F-17..F-24 are new or re-surfaced from v37.

| # | Defect | Status at v37 close | v38 fix path |
|---|---|---|---|
| F-8  | Close-step gate bypassable via sessionless export | **CLOSED** for live-session; unreached for the only case v37 exercised (post-close export) | No v38 action needed; live-session case will validate if it occurs |
| F-9  | Writer invents env folder names | **NOT CLOSED** — defect mutated: writer wrote to `/var/www/environments/{slug}/` producing `environments-generated/` parallel tree | Cx-1 removes env README prescription from writer atoms entirely; Cx-5 forecloses paraphrase |
| F-10 | Writer markdown not staged into deliverable | **PARTIALLY CLOSED** — per-codebase staged; root-level manifest stranded | Cx-4 adds manifest overlay |
| F-11 | Close-step gate advisory (same surface as F-8) | **CLOSED** at source; post-close advisory path is correct behaviour | No v38 action |
| F-12 | Writer marker form without trailing `#` | **NOT CLOSED in practice** — check catches the failure but writer keeps making it because marker form comes from main-agent paraphrase | Cx-2 pre-scaffolds markers on mount so writer never retypes them |
| F-13 | Writer atoms prescribe standalone INTEGRATION-GUIDE.md + GOTCHAS.md | **NOT CLOSED** — atom at HEAD is clean but main agent paraphrases them back in from memory | Cx-1 atom scope reduction + Cx-5 forecloses paraphrase |
| F-14 | Writer first-pass marker failures | Observation-class — 3 × 3 codebases on v37 round 1. Drops to 0 once F-12 closes at source (Cx-2) | Cx-2 side-effect |
| F-15 | Writer IG items without fenced code blocks | Observation-class — caught by check, writer fix-cycle resolves. Acceptable floor | No v38 action if retry count ≤ 2 |
| F-16 | Writer KB fragment missing `### Gotchas` | Observation-class — same as F-15 | No v38 action if retry count ≤ 2 |
| **F-17** | **Envelope content loss: main agent compresses atoms when composing Task prompt** | **OPEN — v37 headline defect** | **Cx-5 is the close**. Engine builds brief; main agent forwards verbatim; dispatch guard refuses paraphrase |
| F-18 | Main agent hallucinates atom IDs (`briefs.editorial-review.per-surface-checklist`) | **OPEN** — side-effect of F-17 composition freedom | Cx-5 side-effect: once main agent calls `build-subagent-brief`, it no longer requests atoms by name |
| F-21 | Finalize envComment factuality — writer invents numeric claims | **OPEN** — 15 failures across 6 tiers on v37 cycle 6 | Cx-3 adds factuality rule atom + check detail tightening |
| F-22 | `no_version_anchors_in_published_content` catches `bootstrap-seed-v1` style execOnce keys (false positive) | **OPEN** — required v37 key-rename work | Cx-6 sharpens regex to skip fenced-code and compound identifiers |
| F-23 | Root-level writer artifacts not staged (`ZCP_CONTENT_MANIFEST.json` stranded at `/var/www/`) | **OPEN** — partial F-10 closure | Cx-4 manifest overlay |
| **F-24** | **`RecoverFork` pkill pattern does not match Chrome/chromium/headless_shell processes** — every browser-timeout recovery since v27 has been a silent no-op; wedged Chrome processes accumulate across retries; comment at [`browser.go:149-157`](../../internal/ops/browser.go#L149) claiming the pattern reaps `agent-browser-chrome-*` helpers is false per `strings` dump of the actual agent-browser v0.21.4 binary | **RE-OPENED from v27** — diagnosed + spec'd in [`docs/zrecipator-archive/implementation-v27-first-principles.md`](../../zrecipator-archive/implementation-v27-first-principles.md) but fix was never ported. **Cx-7 is the close**. Port the v27 spec: read daemon pidfile + process-group kill + `pkill --exact` fallback against `chrome`/`chromium`/`chromium-browser`/`google-chrome`/`headless_shell` + `ForceReset` input flag + auto-trigger on CDP-timeout step errors |

**Structural vs behavioural split**: F-17 is the v38 structural root cause for the writer-path defects (F-9/F-12/F-13). F-24 is an independent structural root cause for the browser-path wall-time loss (~15 min + user intervention on v37). F-21 / F-22 / F-23 are orthogonal quality-of-implementation fixes. F-14 / F-15 / F-16 are expected floor noise.

---

## 5. Operating order: fix-stack first, then v38 commission, then analyse

### Phase 1 — Land the v38 fix-stack (target: 2–4 days)

Execute [`plans/v38-fix-stack.md`](plans/v38-fix-stack.md) phase-by-phase. In summary:

| Cx | File surface | Closes | Est |
|---|---|---|---|
| 1. WRITER-SCOPE-REDUCTION | 6 writer atoms | F-9 + F-13 + env-slug hallucination | 1–2 h |
| 2. SCAFFOLD-FRAGMENT-FRAMES | `recipe_templates_app.go` + 1 atom | F-12 at source | 2–3 h |
| 3. ENV-COMMENT-PRINCIPLE | 1 atom + 1 check | F-21 | 2–3 h |
| 4. MANIFEST-OVERLAY | `recipe_overlay.go` + finalize wiring | F-23 | 2 h |
| 5. SUBAGENT-BRIEF-BUILDER | `subagent_brief.go` (new) + tool handler + dispatch guard + 4 tests + workflow-guide edits | **F-17 (headline)** | 2–3 days |
| 6. VERSION-ANCHOR-SHARPEN | 1 check + tests | F-22 | 1–2 h |
| 7. BROWSER-RECOVERY-COMPLETE | `internal/ops/browser.go` rewrite + tool description + workflow-guide edits + 5 tests | **F-24 (browser wall-time loss)** | 4–6 h |
| 8. HARNESS-V2 | `internal/analyze/{structural,session}.go` | 4 bar-sharpness issues | 3–4 h |

Cx-5 is the one that takes time and carries the main architectural change. Cx-7 is the second structural change (browser-recovery rewrite). Consider decomposing Cx-5 into 5 sub-commits per the plan. Cx-1/2/3/6/7/8 are safely parallel.

Green gate for each: `go test <package> -race -count=1` + `make lint-local` + the Cx's RED test passes.

Tag as `v8.110.0` after all eight land with green CI.

### Phase 2 — Hand back for v38 commission (15 min)

Once `v8.110.0` is tagged + pushed:

1. Fill `FIX_STACK_COMMITS` slot in this document §1 with the 7 SHAs.
2. Notify the user: fix stack shipped; v38 can be commissioned.
3. **Do not commission autonomously.** The user drives commissioning.

### Phase 3 — v38 commission (≈ 2 h user-driven wall time)

When the user runs v38, the commission spec is:

```
TIER:                 showcase
SLUG:                 nestjs-showcase
FRAMEWORK:            nestjs
TAG:                  v8.110.0
COMMISSIONED_BY:      user
AGENT_MODEL:          claude-opus-4-7[1m]
MUST_REACH:           close-step complete (action=complete step=close with no substep → progress.steps[close].status=complete)
MUST_DISPATCH:        editorial-review, code-review sub-agents via build-subagent-brief path
MUST_RUN:             close-browser-walk — expected to complete cleanly after Cx-7. Soft-pass acceptable ONLY if a new failure mode surfaces (documented F-24 is closed; unknown territory is still unknown territory).
MUST_VERIFY_PRE_RUN:  zcp --version output captured in SESSIONS_LOGS shows v8.110.0 or pinned commit SHA
```

**During-run tripwires** (user + analyst monitor concurrently):

- Any `Write` tool call targeting `/var/www/environments/` → halt + alert. Cx-1 regression (writer should no longer author env READMEs).
- Any `Task()` tool call whose input.description matches writer/editorial/code-review keywords but whose input.prompt doesn't match `LastSubagentBrief[role].PromptSHA` → engine-side guard will refuse with `SUBAGENT_MISUSE`. If the guard does NOT fire when it should → Cx-5 implementation bug; halt.
- Any `dispatch-brief-atom` response containing `{{` → halt. Template rendering failed.
- Any `fragment_marker` or `fragment_marker_exact_form` check failure on first writer pass → Cx-2 failed. Halt.
- Any writer-substep retry cycle count ≥ 3 on readmes → alert. F-14/F-15/F-16 floor exceeded.
- Any `import_factual_claims` failure count > 1 across all 6 env tiers → Cx-3 failed. Halt.
- `action=complete step=close` without a preceding editorial-review + code-review attest → engine-side gate refuses. This is correct behaviour; confirm the gate fires.

**Post-run artifacts required** (handed to analysis instance):
- Deliverable tree at `/Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v38/`
- `SESSIONS_LOGS/` with main + sub-agent JSONLs
- User-authored `TIMELINE.md` describing what the user observed + any manual intervention
- Session-end state via `action=status` captured in the logs

### Phase 4 — v38 analysis (≈ 2 h)

Same discipline as v37 analysis, inherited from [`HANDOFF-to-I8-v37-prep.md §6`](HANDOFF-to-I8-v37-prep.md):

1. **Harness first.** `./bin/zcp analyze recipe-run <v38-dir> <logs-dir> --out runs/v38/machine-report.json` + `generate-checklist`. **Commit these two files immediately as evidence floor**, BEFORE any prose.
2. **Extract flow traces.** `python3 docs/zcprecipator2/scripts/extract_flow.py ... --out-dir docs/zcprecipator2/runs/v38/`. Write `runs/v38/role_map.json` first.
3. **Fill checklist by hand.** Read every writer-authored file. Grade each row-level. No bulk judgments. No "unmeasurable — phase unreached" when close completed.
4. **Diff every dispatch prompt against Go-source builder output.** This is the headline v38 verification. For each captured `runs/v38/flow-showcase-v38-dispatches/*.md`, call `BuildWriterBrief(plan)` (or equivalent) and assert byte-equality. If they don't match, Cx-5 failed.
5. **Draft verdict** with the hook-enforced shape:
   - Front matter: `machine_report_sha:` + `checklist_sha:`
   - Every PASS/FAIL claim cites `[checklist X-Y]` or `[machine-report.<key>]` within 150 chars
   - Soft-keyword limit: ≤ 3 naked uses of success/works/clean/PROCEED
6. **One commit at end**: `docs(zcprecipator2): v38 run analysis + verdict`.

### Phase 5 — Verdict decision

**PROCEED** gate (all must hold):
- Every v38 fix-stack target closes per §4 table.
- Dispatch-integrity byte-diff clean for every role.
- Close-step complete + editorial-review + code-review + close-browser-walk attempted.
- Deliverable tree structurally correct: no ghost env dirs anywhere, correct markers, per-codebase markdown + manifest both in the deliverable.
- Harness machine-report clean.

**ACCEPT-WITH-FOLLOW-UP** gate: one signal-grade issue remains (e.g. a new agent-browser failure mode beyond F-24, or one writer-compliance class > threshold). Document as a targeted patch class for v8.110.x; v39 follows.

**PAUSE** gate: any fix-stack target regressed OR F-17 still visible in dispatch-integrity OR > 2 writer-compliance classes fail. Write HANDOFF-to-I10 + defect stack.

**ROLLBACK-Cx** gate: a specific Cx commit introduced a worse regression than it closed. `git revert <sha>` + patch release v8.110.1 reverting only the bad commit.

---

## 6. Analysis discipline rules (inherited from I8, unchanged)

Re-read [`HANDOFF-to-I8-v37-prep.md §6`](HANDOFF-to-I8-v37-prep.md) before writing any v38 prose. Eight rules:

1. No verdict without machine-report SHA in front matter.
2. No PASS/FAIL claim without evidence citation within 150 chars.
3. No "unmeasured" cells when files are on disk.
4. Read every writer-authored file before grading it.
5. Diff dispatch prompts against Go source (now enforceable via `BuildXxxBrief` — Cx-5 side-effect).
6. Retry-cycle attribution required for every failing check round.
7. No verdict ship before checklist is 100% filled.
8. Tripwire on self-congratulatory language without citation (≤ 3 soft warnings).

The `.githooks/verify-verdict` hook enforces all eight at commit time. Install with `git config core.hooksPath .githooks` if not already.

---

## 7. What v38 is NOT testing

Out of scope for v38:

- **Minimal-tier (v35.5)**: independent track; not blocked by v38.
- **Framework diversity**: v38 uses nestjs for A/B with v34–v37.
- **Multiple concurrent runs**: single commissioned run.
- **CI/scheduled runs**: manual commission.
- **End-to-end publish to zeropsio/recipes**: post-v38.
- ~~**agent-browser reliability**: environmental; soft-pass acceptable.~~ MOVED IN-SCOPE as Cx-7 (F-24 close). v37's browser failures were a Chrome-reap bug in our own code (`RecoverFork` pkill pattern doesn't match Chrome), not environmental.
- **Performance optimization**: wall-time is signal, not gate.

---

## 8. Traps and operating rules (inherited + new for v38)

Inherited from I8:
1. **Analysis is NOT implementation.** If you find new defects during analysis, document them; fix-stack comes from a separate commissioning.
2. **Cite evidence `file:line` or `row:timestamp`.** Never "probably".
3. **One commit at end** for analysis. Code changes land in separate commits per CLAUDE.md.
4. **Stop at verdict.** Do not start HANDOFF-to-I10 autonomously.

New for v38:
5. **Rule A enforced**: after Cx-5 lands, there should be zero main-agent prompt composition for writer/editorial/code-review. If v38 analysis finds ANY dispatch prompt that diverges from the engine-built brief, that's a Cx-5 bug — the fix wasn't sound. PAUSE + diagnose Cx-5.
6. **Rule B enforced**: the dispatch guard is an architectural invariant, not a warning. If the guard fires during v38, the run failed the v38 gate regardless of what else passed. Log the event and PAUSE.
7. **Trust-but-verify retrospective.** Before tagging v8.110.0, run the harness against the v37 deliverable tree. Expected behaviour: the same set of defects still shows (v37's tree didn't get the fixes), but the v2 bar-sharpness patches now surface `environments-generated/` (B-15), ignore post-close exports (B-21), and recognise the writer dispatch description (B-23). If any of those fail, Cx-7 is not sound.

---

## 9. Success definition (when is this handoff "done")

- [ ] Phase 1 eight Cx-commits merged to main; `v8.110.0` tagged + pushed.
- [ ] Green full test suite + `make lint-local` at HEAD.
- [ ] Harness v2 retrospective against v37 deliverable reflects the expected new-bar behaviour.
- [ ] Slot block in §1 filled with commit SHAs.
- [ ] User commissioned v38.
- [ ] `runs/v38/{machine-report.json, verification-checklist.md, verdict.md}` all present; pre-commit hook passes.
- [ ] Verdict decision (PROCEED / ACCEPT-WITH-FOLLOW-UP / PAUSE / ROLLBACK-Cx) shipped with full evidence binding.
- [ ] User decides what's next.

If verdict is PROCEED: zcprecipator2 architecture is validated under live end-to-end conditions. Remaining work is publish-pipeline + framework diversity. Handoff-to-I10 may or may not be needed depending on user priorities.

If verdict is PAUSE or ROLLBACK-Cx: Handoff-to-I10 lands with the new defect stack. The architecture has another layer to surface. Thesis not invalidated; fix-stack cadence not over.

---

## 10. Context from v37 — what you should feel reassured about

Not everything v37 showed was bad news. Validated positives (evidence in [`runs/v37/verdict.md §3`](runs/v37/verdict.md)):

- **Deploy + feature sweep + stage round-trip all functional** — 5/5 features green dev + stage, worker round-trip < 500 ms.
- **Step gates catch real issues mid-run** — finalize caught 15 factual-claims failures; agent fixed them; re-finalize passed on cycle 7.
- **Multi-agent orchestration + Symbol Contract** — 13 sub-agents, wire-format + API-shape consistent across app/api/worker per code-review's 0-CRIT verdict.
- **Close-step architecture reached for first time** — editorial-review self-corrected through 3 dispatches; code-review surfaced a real Meilisearch read-after-write gap that got fixed inline + redeployed.
- **Finalize-emitted canonical env READMEs + import.yaml comments are production-grade** — 40–46 lines each, proper sections, substantive per-decision comments.

The **architecture works**. The failure is in one specific layer — main-agent prompt composition — and Cx-5 closes it. v38's job is to confirm.

---

## 11. Starting action

1. Read all 8 items in §3.
2. Verify state is clean on main + tests green before touching anything.
3. Begin Phase 1, Cx-1 per [`plans/v38-fix-stack.md`](plans/v38-fix-stack.md).
4. Commit each Cx separately with its RED-GREEN tests; green `make lint-local` + `go test -race` each time.
5. Tag v8.110.0 after Cx-7 lands with green CI.
6. Fill §1 slots. Notify user.
7. Wait for user to commission v38. **Do not commission autonomously.**
8. Post-commission, do analysis per Phase 4 discipline rules.
9. Ship v38 verdict.
10. Stop. Hand back. User decides next.

Good luck. v37 proved the architecture but surfaced the paraphrase invariant. v38 proves the invariant holds — or points at the next layer.
