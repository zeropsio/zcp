# zcprecipator3 — changelog

Running log of changes on top of [plan.md](plan.md). Each entry captures what changed, why, and what run-analysis or session surfaced the gap.

---

## 2026-04-25 — run-11-readiness (U / V / M / N / O / P / R / Q / S)

### Context

Run 10 (`nestjs-showcase`, 2026-04-25) was the third v3 dogfood and the second to reach `complete-phase finalize` green. All run-10-readiness workstreams (L / M / N / O / P / Q1..Q4) shipped before the run closed and the tranche 3 brief-hygiene fixes held under load. But the rendered deliverable failed reference parity in two structurally distinct ways: a SourceRoot regression caused stitch to write per-codebase README + CLAUDE to `/var/www/<hostname>/` (no `dev` suffix) — silently no-op'ing M's auto-embed of `<SourceRoot>/zerops.yaml` as IG item #1; and a content-quality audit against [docs/spec-content-surfaces.md](../spec-content-surfaces.md) found that 7 of 15 published codebase KB bullets failed the spec's DISCARD-class litmus tests. Adjacent: a v2 `zerops_record_fact` tool stayed registered alongside the v3 action and out-competed it on description, routing 5 of run-10's hardest-won discoveries (npx ts-node trap, .deployignore-bricks-dist, NATS contract, etc.) to `legacy-facts.jsonl` which the v3 stitch pipeline doesn't read. Full analysis at [docs/zcprecipator3/runs/10/ANALYSIS.md](runs/10/ANALYSIS.md), implementation plan at [docs/zcprecipator3/plans/run-11-readiness.md](plans/run-11-readiness.md).

Run 11 ships in three tranches: foundation (U / V / M) ensures hard-won discoveries reach the deliverable and the engine fails loud at boundaries that previously failed silently; content discipline (N / O / P / R) tightens the routing + style lattice so per-codebase content has engine-enforced quality rather than agent-self-graded; polish (Q / S) adds the apps-repo git-history precondition and the engine-composed finalize brief.

### Foundation (tranche 1) — U / V / M

1. **U-1 — refuse v2 `zerops_record_fact` during a v3 recipe session.** [internal/tools/record_fact.go](../../internal/tools/record_fact.go) `resolveFactLogPath` replaces the silent route-to-`legacy-facts.jsonl` with an error naming the v3 action + slug + schema. Without this, the v3 pipeline never sees facts the agent reaches for v2 to record (run-10 lost 5 hard-won discoveries this way). v2 description prefixed with the v3-redirect note. v2-only callers (no recipe session) keep working unchanged.

2. **U-2 — enrich v3 `FactRecord` schema with `failureMode` / `fixApplied` / `evidence` / `scope`.** [internal/recipe/facts.go](../../internal/recipe/facts.go). The v2 schema captured the natural shape of a deploy-time discovery; v3's terser schema forced agents to flatten the discovery into `symptom` and discard the fix. Add the four fields as optional/JSON-tagged so existing callers work unchanged. Required for V-1's auto-classification.

3. **V-1 — classifier auto-detects self-inflicted from `fixApplied`+`failureMode` shape.** [internal/recipe/classify.go](../../internal/recipe/classify.go). Two regexes pattern-match recipe-source change phrasing in `fixApplied` (`(removed|added|changed) X from Y`, `switched X to Y`); a small platform-mechanism vocabulary list (`Zerops`, `L7`, `balancer`, `VXLAN`, `${...}`, etc.) rules out genuine platform teaching when that vocabulary appears in `failureMode`. When both signals fire, `Classify` overrides the agent's `surfaceHint` to `ClassSelfInflicted`. New `ClassifyWithNotice` returns the warning for record-fact callers; new `RecipeResult.Notice` surfaces it to the agent at recording time.

4. **V-2 — `kb-bullet-paraphrases-cited-guide` validator.** [internal/recipe/validators_kb_quality.go](../../internal/recipe/validators_kb_quality.go) (new file). Spec rule 3 says "if a guide exists, the fact is probably a platform invariant the platform already documents — don't duplicate the guide's content." Per-bullet containment of the bullet's tokens within the cited guide's top-100 most frequent meaningful tokens; > 50% flags. Guide content loaded dynamically via `knowledge.Store.Get` from an explicit `guideKnowledgeSources` map (small, named — `env-var-model` → `guides/environment-variables` + `themes/core`, etc.). Containment, not symmetric Jaccard — bullet sizes are dwarfed by guide bodies. Memoized per process. Deterministic; no LLM grading.

5. **V-3 — `kb-bullet-no-platform-mention` validator.** Bullets with zero platform-side vocabulary (only framework concerns — NestJS controller lifecycle, Express middleware, Svelte mount) are framework-quirk per spec rule 5 → flag. Static base list (Zerops, L7, balancer, subdomain, zsc, execOnce, ${...}) extended at validate-time with `Plan.Codebases` + `Plan.Services` hostnames so recipe-specific service names count as platform mentions.

6. **V-4 — `kb-bullet-self-inflicted-shape` regex validator.** Bullets in first-person/recipe-author voice ("we tried X", "the fix was", "after running") are scaffold-debugging forensics, not porter-facing teaching → flag. Spec rule 4 mechanized at the bullet body.

7. **V-5 — scaffold + feature briefs add `### Self-inflicted litmus` subsection.** [internal/recipe/content/briefs/scaffold/content_authoring.md](../../internal/recipe/content/briefs/scaffold/content_authoring.md), [internal/recipe/content/briefs/feature/content_extension.md](../../internal/recipe/content/briefs/feature/content_extension.md). Three labeled run-10 anti-patterns (`npx ts-node`, `dist`-in-`.deployignore`, `Trust proxy is per-framework`) + the porter-clone-question operational rule. `ScaffoldBriefCap` raised 12 KB → 14 KB; later raised again to 16 KB for O-1 Citation map subsection.

8. **M-1 — stitch hard-fails on non-abs / non-`dev`-suffixed `SourceRoot`.** [internal/recipe/handlers.go](../../internal/recipe/handlers.go). Run 10 closed with `cb.SourceRoot` carrying bare hostnames at finalize stitch time, causing README/CLAUDE to land at cwd-relative paths nothing else reads. `stitchContent` now refuses upfront on any non-absolute SourceRoot or any SourceRoot without a `dev` suffix; error names the codebase + the violation. Test fixtures updated to use `<host>dev` paths.

9. **M-2 — `readCodebaseYAMLForHost` hard-fails on missing yaml when SourceRoot non-empty.** [internal/recipe/assemble.go](../../internal/recipe/assemble.go). Soft-fail-to-empty-string was the reason `injectIGItem1` silently no-op'd in run 10 — when SourceRoot pointed at a yaml-less directory, the read returned `""` and the IG yaml-block injection was skipped. Now returns `(string, error)`; `AssembleCodebaseREADME` propagates so stitch fails loud. Empty SourceRoot keeps returning `("", nil)` for genuinely pre-scaffold renders.

10. **M-3 — `zcp sync recipe export` reads README/CLAUDE from `<SourceRoot>/`.** [internal/sync/export.go](../../internal/sync/export.go). Pre-§L the writer staged per-codebase content under `<recipeDir>/<appName>/`; post-§L stitch writes them at `<cb.SourceRoot>/` directly. The export overlay was pinned at the old layout. Redirect to read from `appDir` (= SourceRoot) so uncommitted post-stitch markdown reaches the tarball when `exportGitSubtree`'s committed-only walk would otherwise miss them.

### Content discipline (tranche 2) — N / O / P / R

11. **N-1 — tighten `codebaseKnown` to reject slot hostnames + actionable error message.** [internal/recipe/assemble.go](../../internal/recipe/assemble.go), [handlers_fragments.go](../../internal/recipe/handlers_fragments.go). New `validateCodebaseHostname` returns an error naming the Plan codebase list AND the slot-vs-codebase distinction (slot is the SSHFS mount, codebase is the logical name). `record-fragment` surfaces the actionable message so sub-agents retry with the correct id on the first try (run-10 scaffold-app spent 2m37s + 8 zerops_knowledge requeries cleaning up `codebase/appdev/*` ids).

12. **N-2 — scaffold-brief tripwire names the slot-vs-codebase distinction.** Author-time companion to N-1.

13. **O-1 — citations live in prose, not as boilerplate.** [internal/recipe/content/briefs/scaffold/content_authoring.md](../../internal/recipe/content/briefs/scaffold/content_authoring.md). Rewrote KB Good example to drop the `Cited guide:` tail. Added a Citation map subsection that frames citations as author-time signals — call `zerops_knowledge` first, write the rule's prose, don't tell the porter which guide you read. Run-10 ended every bullet with literal `Cited guide: <name>.` boilerplate, with citation noise propagating into env import.yaml comments.

14. **O-2 — `kb-cited-guide-boilerplate` + `env-yaml-cite-meta` validators.** [internal/recipe/validators_kb_quality.go](../../internal/recipe/validators_kb_quality.go), [internal/recipe/validators_root_env.go](../../internal/recipe/validators_root_env.go). KB validator regex-flags bullets ending with `Cited guide: <name>` boilerplate; env validator regex-flags env import.yaml comments containing `(cite x)` or `Cited guide:` meta phrasing. Two distinct violation codes so authors can act on the right one (V-2's paraphrase-overlap catch is separate).

15. **P-1 — rewrite `.deployignore` paragraph in `themes/core.md`.** [internal/knowledge/themes/core.md](../../internal/knowledge/themes/core.md). The "Recommended to mirror `.gitignore` patterns" recommendation was the root cause of run 10's three scaffold sub-agents reflexively authoring `.deployignore` — and worker scaffold listing `dist/` in it, bricking cross-deploy for ~20 minutes. Replace with: most projects don't need it, `.git` is auto-excluded, editor metadata belongs in `.gitignore`, never list `dist/` or `node_modules/`.

16. **P-2 — scaffold-brief tripwire forbids reflexive `.deployignore` authoring.** Author-time companion to P-1's atom rewrite.

17. **P-3 — `zerops_deploy` lints `.deployignore` for trap patterns.** [internal/ops/deploy_deployignore.go](../../internal/ops/deploy_deployignore.go) (new file). New `LintDeployignore` returns hard-reject errors for `dist`/`node_modules` (deploy artifacts; listing them bricks the runtime), warnings for `.git`/`.idea`/`.vscode`/`*.log` (typically redundant — Zerops builder excludes `.git`, rest belongs in `.gitignore`). `DeployLocal` now blocks on errors and surfaces warnings up the existing channel.

18. **R-1 + R-2 — IG validator + scaffold-brief unify on `### N.` header shape.** [internal/recipe/validators_codebase.go](../../internal/recipe/validators_codebase.go). Run 10 closed with the validator enforcing plain ordered-list while the scaffold brief instructed `### N.` headers — finalize had to rewrite IG fragments to satisfy. Pick canonical: `### N.` headers (matches the engine-generated item #1 and the laravel-showcase reference). Validator now requires heading shape and flags plain ordered-list with `codebase-ig-plain-ordered-list`; brief already mandates `### 2. <title>`. Removed unused `numberedItemRE`.

### Polish (tranche 3) — Q / S

19. **Q-1 + Q-2 — scaffold mandates `git init` at close + feature mandates per-feature commits.** [internal/recipe/content/briefs/scaffold/content_authoring.md](../../internal/recipe/content/briefs/scaffold/content_authoring.md), [internal/recipe/content/briefs/feature/content_extension.md](../../internal/recipe/content/briefs/feature/content_extension.md). Apps-repo publish path needs each codebase's SourceRoot to have `.git/` initialized + at least one commit. Run 10 scaffold sub-agents wrote source + `zerops.yaml` to `/var/www/<h>dev/` correctly but never ran `git init`; doing it post-hoc loses per-feature commit shape. `FeatureBriefCap` raised 10 KB → 12 KB.

20. **Q-3 — `zcp sync recipe export` warns on missing `<SourceRoot>/.git/`.** [internal/sync/export.go](../../internal/sync/export.go). Stderr warning when an `appDir` lacks `.git/` — informational, doesn't block, but gives the agent + user a visible signal that the git-init mandate (Q-1) was skipped.

21. **S-1 — engine-composed `briefKind=finalize` wrapper.** [internal/recipe/briefs.go](../../internal/recipe/briefs.go), [internal/recipe/content/briefs/finalize/](../../internal/recipe/content/briefs/finalize/). New `BuildFinalizeBrief` derives codebase paths, managed-service list, and fragment-count math from `Plan`; embeds finalize-specific validator tripwires (porter voice, citation-noise ban, IG-shape mandate, self-inflicted litmus). New `BriefFinalize` kind + `content/briefs/finalize/{intro, validator_tripwires}.md` atoms. Replaces the hand-typed wrapper main agent used in run 10 (math errors and obsolete paths compounded across runs). `FinalizeBriefCap` = 12 KB.

22. **S-2 — `phase_entry/finalize.md` documents the dispatch option.** [internal/recipe/content/phase_entry/finalize.md](../../internal/recipe/content/phase_entry/finalize.md). Document the choice — main agent direct authoring (low count) vs sub-agent dispatch (high count, mechanical) — and point at `zerops_recipe action=build-brief briefKind=finalize` for engine-composed wrapper. Hand-typed wrappers explicitly out.

### Open questions resolved at implementation

- **V-1 override behavior**: log-only warning via `RecipeResult.Notice`. The fact still records; the override only affects publish-time routing. Agent gets the chance to course-correct on the next call.
- **V-2 metric**: containment (asymmetric), not symmetric Jaccard. Bullet ~20 tokens, guide ~thousands; symmetric Jaccard would never approach 0.5. Top-100 most frequent guide tokens form the keyword set; threshold 0.5.
- **M-2 hard-fail boundary**: hard-fail at stitch-time (M-1 + M-2 propagate); empty-SourceRoot still returns `("", nil)` for pre-scaffold paths.
- **R-1 strict vs both-shapes**: strict `### N.` only. No published v3 recipes break; the engine's own item-#1 emits heading shape so the validator must require it.

### Not yet addressed (post-run-11 scope)

- Run 11 dogfood itself — execution is the user's call.
- Forensic re-litigation of the run-10 SourceRoot regression's exact cause. M-1 + M-2 force loud failure regardless of which of the three plausible causes was real.
- Chain-resolution redesign — still deferred until `nestjs-minimal` gets a v3 re-run.
- Automated click-deploy verification harness — still manual.
- `verify-subagent-dispatch` SHA check — still deferred.

---

## 2026-04-24 — run-10-readiness (L / M / N / O / P / Q1..Q4)

### Context

Run 9 (nestjs-showcase, 2026-04-24) was the first v3 dogfood to reach `complete-phase finalize` green on all 11 run-9-readiness workstreams. Every run-9 criterion passed against its own plan — but when the rendered deliverable was compared directly to the reference apps-repo at `/Users/fxck/www/laravel-showcase-app/`, two structural problems and a cluster of stylistic divergences surfaced that run-9's criteria didn't measure: apps-repo content was written to an invented `<outputRoot>/codebases/<h>/` directory no published recipe carries, README item #1 of the Integration Guide described the yaml in English prose instead of embedding it verbatim, yaml comments were single-line run-ons stuffed with causal words, README knowledge-bases were stylistically bimodal within one file, and CLAUDE.md files were 3× longer than the reference. Full analysis at [docs/zcprecipator3/runs/9/ANALYSIS.md](runs/9/ANALYSIS.md), diff against reference at [CONTENT_COMPARISON.md](runs/9/CONTENT_COMPARISON.md), engine-brief hygiene at [PROMPT_ANALYSIS.md](runs/9/PROMPT_ANALYSIS.md).

### Shape fixes (tranche 1) — blockers for run 10

1. **L — redirect apps-repo content from invented subdirectory to `<cb.SourceRoot>`.** `stitchContent` now writes each codebase's README.md + CLAUDE.md directly to `cb.SourceRoot` (the SSHFS-mounted dev slot at `/var/www/<h>dev/`), the same tree that already holds the scaffold-authored zerops.yaml + source. Matches the reference apps-repo shape where README + CLAUDE + zerops.yaml + source all live at repo root. Deleted `copyCommittedYAML` entirely (the duplicate copy had no reader; scaffold already authored the yaml at SourceRoot). Validator `codebasePaths` resolves codebase surfaces to `<cb.SourceRoot>` so validators read from the same tree stitch writes to. Net LoC negative. Sources: [ANALYSIS.md §3 gap L](runs/9/ANALYSIS.md), [CONTENT_COMPARISON.md §1](runs/9/CONTENT_COMPARISON.md). See `internal/recipe/handlers.go`, `validators.go`.

2. **M — auto-embed `<cb.SourceRoot>/zerops.yaml` as IG item #1.** `AssembleCodebaseREADME` now rewrites the rendered README's integration-guide extract block to open with an engine-generated `### 1. Adding \`zerops.yaml\`` heading, a one-or-two-sentence intro *derived from the yaml body* (setups declared, whether initCommands run migrations/seeding/search-index, whether readinessCheck/healthCheck ship), and a fenced yaml code block carrying the yaml verbatim with inline comments preserved. Fragment-authored items follow at `### 2.`+ per the updated scaffold brief. The missing-fragment gate still fires when the sub-agent didn't author items #2+. Matches the reference pattern where the porter sees the full config shape at a glance without opening a second file. Sources: [CONTENT_COMPARISON.md §4](runs/9/CONTENT_COMPARISON.md). See `internal/recipe/assemble.go`, `content/briefs/scaffold/content_authoring.md`.

### Style fixes (tranche 2) — inside the now-correct shape

3. **N — loosen yaml-comment causal-word check from per-line to per-block.** `validateCodebaseYAML` groups adjacent `#` comment lines into blocks (bare `#` stays in-block as paragraph separator per the reference), then checks each block — not each line — for a causal word / em-dash. Label blocks (every line ≤40 chars after stripping `#`) pass unconditionally. A block without rationale emits exactly one violation, not one per line. The previous per-line rule pressured sub-agents into single-line run-ons stuffed with `because`/`so that`/`otherwise` on every line; reference-style multi-line prose blocks now pass cleanly. `yaml-comment-style.md` atom rewritten to teach the block model. Sources: [CONTENT_COMPARISON.md §3](runs/9/CONTENT_COMPARISON.md). See `internal/recipe/validators_codebase.go`, `content/principles/yaml-comment-style.md`.

4. **O — unify KB format as `**Topic** — prose`; ban `**symptom**:` triple.** New validator `codebase-kb-triple-format-banned` flags KB bullets opening with `**symptom**:` / `**mechanism**:` / `**fix**:`. Run 9's api README shipped bimodal (8 triple entries from scaffold + 6 Topic entries from feature — same file, two personalities). Debugging runbooks belong in `codebase/<h>/claude-md/notes`; the porter-facing KB uses `**Topic**` + em-dash + prose so a reader scanning topic names can find the entry. Both `content_authoring.md` (scaffold) and `content_extension.md` (feature) teach the Topic format with good/bad examples. Sources: [CONTENT_COMPARISON.md §5](runs/9/CONTENT_COMPARISON.md). See `internal/recipe/validators_codebase.go`, scaffold + feature brief atoms.

5. **P — cap CLAUDE.md at ≤60 lines; ban cross-codebase subsections.** Deleted `claude-md-too-few-custom-sections` (pressured authors to ADD sections — wrong direction). Added `claude-md-too-long` (flags >60 lines; reference is 33 lines, run-9 shipped 99-line files) and `claude-md-forbidden-subsection` (flags cross-codebase operational headings `Quick curls`, `Smoke test(s)`, `Local curl`, `In-container curls`, `Redeploy vs edit`, `Boot-time connectivity` — identical across every codebase in a recipe, belong in the recipe root README). Sources: [CONTENT_COMPARISON.md §6](runs/9/CONTENT_COMPARISON.md). See `internal/recipe/validators_codebase.go`, scaffold + feature brief atoms.

### Engine-brief hygiene (tranche 3)

6. **Q1 — gate the `## HTTP` section on `role.ServesHTTP`.** `BuildScaffoldBrief` strips the `## HTTP` platform-obligations section from the composed brief when the codebase's role has `ServesHTTP=false` (worker / job-consumer). The atom marks the section with `<!-- HTTP_SECTION_START -->` / `<!-- HTTP_SECTION_END -->` comments; the composer removes everything between them for non-HTTP roles. Dropped the `(ServesHTTP=true)` header annotation — it was a hint that the section was gated; now that it actually is, the annotation is noise. Sources: [PROMPT_ANALYSIS.md §2.2 smell S4](runs/9/PROMPT_ANALYSIS.md). See `internal/recipe/briefs.go`, `content/briefs/scaffold/platform_principles.md`.

7. **Q2 — rename scaffold-brief header to `# Behavioral gate`.** The `preship_contract.md` atom renders as `# Behavioral gate` instead of `# Pre-ship contract`. The phrase "pre-ship contract" stays on the forbidden-voice list. Previously the same phrase was both a structural header in the brief AND a forbidden voice-leak in the content-authoring rules, so the sub-agent had to hold a mental partition between authoring vocabulary and output vocabulary. Sources: [PROMPT_ANALYSIS.md §2.2 smell S5](runs/9/PROMPT_ANALYSIS.md). See `content/briefs/scaffold/preship_contract.md`.

8. **Q3 — port finalize-time validator rules into the scaffold brief.** `content_authoring.md` adds a "Validator tripwires" section surfacing six author-time rules that finalize gates reject on: IG item #1 is engine-owned, IG 2+ must not name scaffold-only filenames, env READMEs use porter voice, env READMEs target 45+ lines, yaml comment blocks need one causal word per block (§N), KB uses Topic format only (§O), CLAUDE.md is 30–50 lines with no cross-codebase runbooks (§P). Run-9 burned ~15 minutes of finalize wall time iterating on rules it could have seen up front. `yaml-comment-style.md` compressed to keep the scaffold brief under the 12 KB cap — regression-pinned by `TestBrief_Scaffold_UnderCap_WithValidatorTripwires`. Sources: [PROMPT_ANALYSIS.md §3 smells S11](runs/9/PROMPT_ANALYSIS.md), run-9 finalize round 1 violation list. See `content/briefs/scaffold/content_authoring.md`.

9. **Q4 — extend init-commands-model.md with arbitrary-static-key shape.** Third key shape: `<slug>.<operation>.<version>` (e.g. `nestjs-showcase.seed.v1`). Same once-per-service-lifetime semantics as the canonical static key, but the `.v1` suffix is a documented re-run lever (bump to `.v2` to force re-trigger). Run 9's feature sub-agent queried `zerops_knowledge` five times with rephrased variants asking about this exact case because the atom didn't cover it. `content_extension.md` points at key shape #3 when adding an initCommand so the sub-agent sees the answer without a lookup. Sources: [PROMPT_ANALYSIS.md §2.2 smell S4-ish](runs/9/PROMPT_ANALYSIS.md), [agent-adb7.jsonl knowledge-loop evidence](runs/9/SESSION_LOGS/subagents/agent-adb75d19d2006e0db.jsonl). See `content/principles/init-commands-model.md`, `content/briefs/feature/content_extension.md`.

### M follow-up — dynamic IG item #1 intro

The item-#1 intro sentence is derived from the yaml body via stanza-name substring probes on the canonical Zerops shape — the yaml is never re-parsed. Before: every recipe's IG opened with the same generic "The main configuration file" sentence regardless of contents. After: the intro names the setups declared and the behaviors present (initCommands with migrations / seed / search-index, readiness + health checks). The porter learns what this yaml does before reading the code block.

### Brief cap pressure

Tranche 3 adds the Validator-tripwires section (~400 bytes) + the third execOnce key shape (~250 bytes). Compressed `yaml-comment-style.md` + the Voice and Classify sections of `content_authoring.md` to fit. Scaffold brief stays under 12 KB across all three synthetic codebases; feature brief stays under 10 KB. Pins: `TestBrief_Scaffold_UnderCap_WithValidatorTripwires`, existing `TestBriefCompose_ScaffoldUnderCap` and `TestBriefCompose_FeatureUnderCap`.

### Not yet addressed (post-run-10 scope)

- Chain-resolution redesign. `chain.go::loadParent` still reads `<parentDir>/codebases/<h>/` — that path was a no-op against v2 parents (they never had it) and, after §L, is also a no-op against v3 parents (they don't have it either). Redesign to read apps-repo-shaped parents is deferred until `nestjs-minimal` gets a v3 re-run and the real inheritance flow becomes testable.
- Automated click-deploy verification harness — still manual for criterion 10.
- `verify-subagent-dispatch` SHA check — still deferred.
- Per-surface `validate-surface` action (collapses finalize "wall of red") — useful authoring affordance; not blocking.
- Auto-inject scaffold-phase facts into feature brief (the `${broker_connectionString}` trap propagation was hand-assembled by main agent in run 9) — automatable but not blocking.

---

## 2026-04-24 — run-9-readiness (A1 / A2 / B / E / G1 / G2 / H / I / K / J / R)

### Context

Run 8 (nestjs-showcase, 2026-04-24) cleared research → provision → scaffold → feature but aborted at `stitch-content` on `{API_URL}` inside a fragment body (A1), shipped no per-codebase `zerops.yaml` because `SourceRoot` was never populated on live runs (A2), routed feature-phase facts through v2's `zerops_record_fact` so classifier + validators never saw them (B), broke first-call fact records on `surface_hint` snake-case mismatch (E), collapsed dev vs prod process model into "same start command, different deployFiles" (G1), ran `npm install` + `tsc` + `nest build` against the SSHFS mount (G2), decorated scaffold yaml with ASCII divider banners (H), leaked authoring-phase references ("the scaffold ships…", "pre-ship contract item 5") into committed source comments + intro fragments (I), serialized scaffold sub-agent dispatch losing ~23 minutes of parallelizable wall time (K), and returned byte-identical envelopes from 22 `record-fragment` calls (J). Full analysis at `docs/zcprecipator3/runs/8/ANALYSIS.md`.

### Engine-delivery fixes (tranche 1)

1. **A1 — scope unreplaced-token scan to engine-bound keys only.** `checkUnreplacedTokens` now filters `{UPPER_SNAKE}` matches against a fixed `engineBoundKeys` set (`SLUG`, `FRAMEWORK`, `HOSTNAME`, `TIER_LABEL`, `TIER_SUFFIX`, `TIER_LIST`). Anything outside that set (JS template literals `${API_URL}`, Svelte `{#if}`, Vue `{{template}}`, Go `{{ .Field }}`, Handlebars `{FILENAME}`) is fragment-authored code and passes. Errors now name the surface (`assemble codebase/api README: …`). See `internal/recipe/assemble.go`.

2. **A2 — populate `Codebase.SourceRoot` at `enter-phase scaffold`.** Convention-based: empty `SourceRoot` becomes `/var/www/<hostname>dev`. Explicit values (chain resolver, custom mounts) are preserved. `copyCommittedYAML` flips from soft-fail (silent NO-OP) to hard-fail with a message that names the root cause ("scaffold did not run or was skipped"). Exposed as `DefaultSourceRoot(hostname)` for tests + future call sites. See `internal/recipe/handlers.go`.

3. **B — feature brief routes facts through v3 tool + browser-verification FactRecord.** `content_extension.md` gains a "Recording feature-phase facts" section naming `zerops_recipe action=record-fact` (not legacy `zerops_record_fact`) + the camelCase schema. `phase_entry/feature.md` step 7 rewires the browser-walk to record one FactRecord per `zerops_browser` call with `surfaceHint: browser-verification`. Classifier maps the new hint to `ClassOperational` (publishable). See `internal/recipe/content/briefs/feature/content_extension.md`, `content/phase_entry/feature.md`, `classify.go`.

4. **E — normalize `surfaceHint` casing.** Engine error message + `facts_test.go` literals + `fact_recording.md` prose all spoke `surface_hint`, but `FactRecord`'s JSON tag was `surfaceHint`. Two out of three scaffold sub-agents failed their first `record-fact` on the mismatch. Normalized to `surfaceHint` everywhere. See `internal/recipe/facts.go`, `facts_test.go`, `content/briefs/scaffold/fact_recording.md`.

### Content-pipeline fixes (tranche 2)

5. **G1 — dev-loop principles atom (unconditional injection).** New `content/principles/dev-loop.md` ports v2's `zsc noop --silent` pattern + `zerops_dev_server` tool + dev-vs-prod process model + `deployFiles: .` self-preservation rule. Injected into every scaffold brief (the original plan gated on `anyCodebaseIsDynamicRuntime`, but Laravel-with-Vite frontends need the dev-server even when the backend is php-nginx — so injection is unconditional and the atom explains the implicit-webserver carve-out inline). `phase_entry/scaffold.md` adds a step 5 prompting `zerops_dev_server action=start` before the preship contract. See `internal/recipe/content/principles/dev-loop.md`, `briefs.go`.

6. **G2 — mount-vs-container execution-split atom.** New `content/principles/mount-vs-container.md` — editor tools run on the SSHFS mount, framework CLIs (`npm install`, `tsc`, `nest build`, `artisan`, `curl localhost`) run via `ssh <hostname>dev "..."`. Cites two reasons: correct environment (runtime version, package-manager cache, platform env vars) and avoiding FUSE-tunneled file IO. Injected unconditionally in scaffold + feature briefs. See `internal/recipe/content/principles/mount-vs-container.md`, `briefs.go`.

7. **H — yaml-comment-style atom + divider-banner validator.** New `content/principles/yaml-comment-style.md` — ASCII `#` only, no dividers, no banners, section transitions use a single bare `#`. Engine-side: `yamlDividerREs` (one regex per decoration character since Go's RE2 has no backreferences) flags any comment line containing a run of 4+ `-`/`=`/`*`/`#`/`_`. New violation code `yaml-comment-divider-banned`, emitted before the causal-word check so the author sees the right diagnostic first. See `content/principles/yaml-comment-style.md`, `validators.go`, `validators_codebase.go`.

8. **I — porter-voice rule + source-code comment scanner.** Voice rule prepended to both `content_authoring.md` (scaffold) and `content_extension.md` (feature): the reader is a porter cloning the apps repo, never another recipe author. Never write "the scaffold", "feature phase", "pre-ship contract item N", "showcase default", "we chose", "grew from". Always describe the finished product. Engine-side: `gateSourceCommentVoice` in `gates.go` walks every `Codebase.SourceRoot`, scans `.ts`/`.tsx`/`.js`/`.svelte`/`.vue`/`.go`/`.php`/`.py`/`.rb` files (skipping `node_modules`/`vendor`/`dist`/etc.), and flags forbidden phrases inside comment lines. Registered as a `FinalizeGates` entry. Skips codebases with empty or missing `SourceRoot` silently. See `validators_source_comments.go`, `gates.go`.

9. **K — scaffold parallel-dispatch directive.** `phase_entry/scaffold.md` now prescribes: dispatch every codebase sub-agent in a single message with parallel `Agent` tool calls. Each sub-agent's `zerops_deploy` + `zerops_verify` queue naturally at the recipe session mutex — serializing dispatch serializes all the parallelizable work (file authoring, `ssh` / `npm install`, `zerops_knowledge` lookups) for no gain. Net savings on a 3-codebase scaffold: 15-30 min. See `content/phase_entry/scaffold.md`.

10. **J — `record-fragment` response echoes `fragmentId` + `bodyBytes` + `appended`.** `RecipeResult` gains three omit-when-empty fields. `recordFragment` returns `(bodyBytes int, appended bool, err error)` so the handler populates them. Append-class ids (`codebase/*/integration-guide`, `knowledge-base`, `claude-md/*`) return `appended=true` + combined-body size on the second+ write; overwrite ids return the last body's size with `appended=false`. Refactor-only split: `recordFragment` + `applyEnvComment` + `isAppendFragmentID` + `isValidFragmentID` + `parseTierIndex` + `serviceKnown` + `fragmentIDRoot` moved from `handlers.go` (was 733 lines, past the 350 advisory) into `handlers_fragments.go`. No behavior change. See `handlers.go`, `handlers_fragments.go`.

### Regression fixture (tranche 3)

11. **R — e2e assemble fixture covering every code-block token shape.** Single `TestAssemble_FragmentBody_CodeTokens_E2E` exercises root / env / codebase README / codebase CLAUDE.md with fragment bodies carrying `${API_URL}`, `{FILENAME}`, `{{template}}`, `<slot />`, `{#if cond}…{/if}`, `{{ .FieldName }}`, and `` `${PLACEHOLDER}` ``. Pins the A1 invariant: fragment bodies with legitimate `{UPPER}` or `${UPPER}` syntax never trip the token scanner. See `assemble_test.go`.

### Brief cap pressure

Run-8-readiness raised the scaffold brief cap from 3 KB → 5 KB. Tranche-2 adds three principle atoms (dev-loop, mount-vs-container, yaml-comment-style) on top of existing scaffold content + mount-vs-container + yaml-comment-style on the feature side. Raised scaffold cap to 12 KB and feature cap to 10 KB. Both composers stay under their caps on a nestjs-showcase-shaped plan. Pin: `TestBrief_Scaffold_UnderCap_WithDevLoop`.

### Not yet addressed (post-run-9 scope)

- Automated click-deploy verification harness — still manual for criterion 10.
- `verify-subagent-dispatch` SHA check of brief integrity — still deferred (was stretch for run 8).
- Warn-lint at Bash pre-call hook to catch local `npm install` against the mount — harness concern, not recipe-engine.
- Rehydrate path for feature-phase facts sent through legacy `zerops_record_fact` — brief-only fix is strictly sufficient for run 9; revisit if a future run bypasses the brief.

---

## 2026-04-23 — v9.5.3 + follow-ups

### Context

Run 3 and run 4 dogfood (see `runs/3/RAW_CHAT.md`, `runs/4/RAW_CHAT.md`) surfaced three categorical engine defects — none was caught by fixture tests because they only materialize against a live agent/platform.

### Fixes shipped

1. **`RecipeInput.Plan/Fact/Payload` typed structs** (v9.5.1) — `json.RawMessage` fields generate MCP schemas with `type: ["null", "array"]`, rejecting JSON objects. Replaced with `*Plan`, `*FactRecord`, `map[string]any`. See `internal/recipe/handlers.go`.

2. **`zerops_knowledge` tool description owns the recipe-authoring exclusion** (v9.5.1) — schema-level "ALWAYS use this field" imperatives were out-competing markdown-level "Do NOT call" prohibitions. Rewrote the tool description to refuse recipe-authoring use at the schema layer; the research atom now cites the tool's own description for mutual reinforcement.

3. **`gateEnvImportsPresent` moved out of `DefaultGates()`** (v9.5.3) — was firing at every `complete-phase` including research, forcing the agent to emit all 6 `import.yaml` files before it knew what comments to write. Now only fires at `PhaseFinalize` close, after the writer sub-agent has populated comments. `emit-yaml` now also writes `<outputRoot>/<tier.Folder>/import.yaml` to disk so the gate can actually pass. See `internal/recipe/gates.go`, `internal/recipe/phase_entry.go`, `internal/recipe/workflow.go`.

### Gap identified but not yet fixed — provision/deliverable YAML shape + env-var lifecycle

**Background**: plan §3 stays-list says v3 reuses v2's YAML emitter and secret-forwarding rules. Plan §13 risk watch says *"v3's `yaml_emitter.go` wraps v2's yaml emitter, does not replace it."* v3 ignored both and wrote `internal/recipe/yaml_emitter.go` from scratch (296 LoC), losing v2's captured knowledge:

- **Two distinct YAML shapes**: v2 separates the *workspace import* (provision-time, agent-authored from atoms per `workflow/phases/provision/import-yaml/workspace-restrictions.md` — services-only, `startWithoutCode: true` on dev, no `project:`, no `buildFromGit`, no `zeropsSetup`, no preprocessor expressions) from the six *deliverable imports* (finalize, Go-generated via `recipe_templates_import.go::GenerateEnvImportYAML` — full `project:` + `envVariables` + `buildFromGit` + `zeropsSetup`). v2 enforces the distinction via a validator (`internal/tools/workflow_checks_finalize.go:208-215`) that refuses `startWithoutCode` in deliverables.

- **Three env-var timelines**:
  1. *Provision (live workspace)*: real secret values set via `zerops_env project=true action=set variables=["APP_KEY=<@generateRandomString(<32>)>"]` — preprocessor runs once, actual value lands on the project. Cross-service auto-inject keys cataloged via `zerops_discover includeEnvs=true`.
  2. *Scaffold (per-codebase `zerops.yaml`)*: `run.envVariables` references the discovered cross-service keys (`DB_HOST: ${db_hostname}`) — never raw values.
  3. *Finalize (6 deliverable yamls)*: `projectEnvVariables` is structured per-env input to `generate-finalize`. Envs 0-1 (dev-pair) carry `DEV_*` + `STAGE_*` URL constants; envs 2-5 (single-slot) carry `STAGE_*` only with hostnames `api`/`app` instead of `apistage`/`appstage`. Shared secrets re-emit as `<@generateRandomString>` templates so each end-user gets a fresh value. `${zeropsSubdomainHost}` stays literal — end-user's project substitutes at click-deploy.

**Why it matters**: the recipe is a template that produces a reproducible click-deploy. Conflating author-workspace state with deliverable yaml breaks security (every end-user inherits the author's APP_KEY), URL resolution (author's subdomain baked in instead of `${zeropsSubdomainHost}`), and provision itself (workspace yaml with `buildFromGit` tries to clone empty repos before scaffold has pushed them).

**What v3 has now**:
- `yaml_emitter.go` emits one shape — deliverable-shape — for all 6 tiers. No workspace shape exists.
- `Plan.ProjectEnvVars map[string]map[string]string` field exists but nothing populates it, no atom teaches it, emitter doesn't distinguish per-env shapes.
- Provision atom tells the agent to emit tier-0 yaml + `zerops_env` secrets simultaneously — conflicting state.
- Writer completion_payload has `env_import_comments` but no `project_env_vars` key.
- `stitch-content` is a stub that saves the writer blob as `.writer-payload.json` — doesn't regenerate deliverable yamls with writer-authored comments + env vars, doesn't write per-codebase READMEs or CLAUDE.md.
- No atom mentions `zerops_discover includeEnvs=true` for cross-service key discovery.
- No awareness of `${zeropsSubdomainHost}` as a literal template.

### Fix shipped in the same session — workspace/deliverable split + real stitch

1. **Split YAML emitter** (`internal/recipe/yaml_emitter.go`):
   - Added `Shape` type (`ShapeWorkspace` | `ShapeDeliverable`).
   - New `EmitWorkspaceYAML(plan)` — services-only, dev+stage pairs per
     codebase, dev runtimes `startWithoutCode: true`, stage runtimes omit
     it, no `project:` block, no `buildFromGit`, no `zeropsSetup`, no
     preprocessor expressions. Never written to disk; returned inline for
     `zerops_import content=<yaml>`.
   - Renamed `EmitImportYAML` → `EmitDeliverableYAML` (old name kept as
     a thin delegate for back-compat).
   - Enforcement by construction — the workspace path never emits the
     forbidden fields; no runtime validator needed.

2. **`emit-yaml` action takes `shape`** (`internal/recipe/handlers.go`):
   - `shape=workspace` returns yaml inline, does NOT write to disk
     (provision submits via `zerops_import content=<yaml>`).
   - `shape=deliverable` writes `<outputRoot>/<tier.Folder>/import.yaml`
     so the finalize gate can verify presence.
   - Default is `deliverable` when omitted.

3. **Real `stitch-content`** (`internal/recipe/handlers.go`):
   - Archives the writer payload at `.writer-payload.json` (gate reads).
   - Merges `env_import_comments` → `plan.EnvComments`.
   - Merges `project_env_vars` → `plan.ProjectEnvVars`.
   - Regenerates all 6 deliverable yamls to disk with writer-authored
     comments + project env vars.
   - Writes root `README.md`, env `<tier.Folder>/README.md`, per-codebase
     `codebases/<hostname>/README.md` (IG + KB fragments with markers),
     per-codebase `codebases/<hostname>/CLAUDE.md`.

4. **Atoms rewritten**:
   - `phase_entry/provision.md` — explains workspace vs deliverable
     distinction, tells the agent to `emit-yaml shape=workspace` + pass
     inline to `zerops_import content=`, then `zerops_env project=true`
     for secrets + `zerops_discover includeEnvs=true` for cross-service
     keys. No disk write.
   - `phase_entry/finalize.md` — explains the template model (shared
     secrets as `<@generateRandomString>`, URLs with
     `${zeropsSubdomainHost}` literal, per-env shape for `project_env_vars`).
   - `briefs/writer/completion_payload.md` — adds `project_env_vars` as a
     first-class key with per-env shape + leak rules.
   - New `principles/env-var-model.md` — single-source explanation of
     the three timelines (workspace / scaffold / deliverable) and the
     leak rule from timeline 1 into timeline 3.

5. **Tests pin the contract**:
   - `TestEmitWorkspaceYAML_ShapeContract` — workspace yaml forbids
     `project:`, `buildFromGit:`, `zeropsSetup:`, preprocessor, and
     requires `startWithoutCode: true`.
   - `TestDispatch_StitchContent_MergesEnvFieldsAndRegenerates` — the
     full stitch pipeline: payload merge → deliverable regeneration →
     content surface writes, with `${zeropsSubdomainHost}` preserved as
     literal (template-leak canary).

### Still not captured (conscious defer)

- `codebase_zerops_yaml_comments` splicing into per-codebase
  `zerops.yaml` files at their anchors — the `zerops.yaml` lives on the
  Zerops service mount, not in the output tree. Deferred until Commission
  B surfaces a concrete anchor-splice mechanism.
- `verify-subagent-dispatch` — still not implemented; scaffold atom
  acknowledges this and tells the main agent not to paraphrase briefs.
- Chain-resolution diff-aware yaml emission (plan §7 "engine renders
  import.yaml for showcase tiers by diffing against parent's env
  import.yaml"). Current emitter emits full yaml per tier; delta mode is
  Commission C.

---

## 2026-04-23 later — v9.5.5: workflow-context gate + CLAUDE.md teach recipe flow

### Context

Run 5 dogfood (`runs/5/RAW_CHAT.md`) with v9.5.4. Progress was clean
through research + provision-yaml emit, then regressed on step 2 of
the provision atom. The agent called `zerops_import content=<yaml>`
verbatim as the atom instructs, but got:

```
{"code":"WORKFLOW_REQUIRED","error":"No active workflow. This tool
requires a workflow context.","suggestion":"Start a workflow:
workflow=\"bootstrap\" or workflow=\"develop\"."}
```

The agent then followed the error suggestion + the project CLAUDE.md's
"Bootstrap first when there are no services yet" guidance and started a
full bootstrap workflow, abandoning the recipe flow entirely. Two root
causes, both engine-side.

### Root causes

1. **Workflow-context gate didn't know about v3 recipe sessions.**
   `internal/tools/guard.go::requireWorkflowContext` guards
   `zerops_import` and `zerops_mount`; its comment promised it would
   accept "bootstrap/recipe session OR an open work session" but the
   implementation only checked v2's engine. A live v3 `recipe.Store`
   session wasn't recognized as valid context.

2. **CLAUDE.md template taught two entry points, not three.**
   `internal/content/templates/claude.md` instructed agents to start
   `zerops_workflow bootstrap` when there were no services yet — the
   exact reflex that derailed run 5. The template had zero mention of
   `zerops_recipe` so the agent had no frame for "this is a recipe run,
   not infra work."

### Fixes shipped

1. `recipe.Store.HasAnySession()` — new public predicate. Returns true
   if at least one recipe session is open in the store.

2. `requireWorkflowContext(engine, stateDir, recipeProbe
   RecipeSessionProbe)` — third argument is a nil-safe interface probe.
   `internal/tools/guard.go` declares `RecipeSessionProbe` (avoids a
   hard cross-package import of `internal/recipe`); `*recipe.Store`
   satisfies it. An active recipe session now satisfies the guard.

3. `RegisterImport` + `RegisterMount` in `internal/tools/` plumb the
   probe through; `server.go` passes the single `recipeStore` instance.

4. Error message updated to list `zerops_recipe action="start"` as the
   first option so an agent that hits the guard in a recipe context
   sees the recipe path explicitly.

5. CLAUDE.md template rewrite — "Starting a task" section becomes
   "Three entry points — pick the right one", with recipe authoring as
   option 1. Explicitly tells the agent **not** to start bootstrap or
   develop workflows during recipe authoring. Points at
   `zerops_recipe action="status"` for recovery.

### Adoption gate — next problem surfaced

`requireAdoption` (`internal/tools/guard.go:38`) gates deploy-related
tools (`zerops_deploy` variants) on ServiceMeta entries under
`stateDir/services/`. Recipe-provisioned services don't write
ServiceMeta — so once run 5's fix lets `zerops_import` pass, the next
call (`zerops_mount`, then `zerops_deploy` at scaffold phase) will fail
the adoption gate. Currently gated to activate only after
`stateDir/services/` exists (migration path), so fresh zcp installs
bypass it, but any install with prior bootstrap state will block.

Two options to fix when it bites:
- Have `zerops_recipe complete-phase provision` write ServiceMeta for
  every plan hostname (coupling v3 to v2's state shape).
- Extend `requireAdoption` to ALSO accept recipe-session hostnames as
  adopted (cleaner — mirror the guard split used above).

Deferred until a dogfood run hits it.

---

## 2026-04-23 even later — v9.5.6: zerops_knowledge scope + scaffold consults-before-writing

### Context

Run 6 dogfood (`runs/6/`) with v9.5.5. Research + provision + scaffold
dispatch + three sub-agent deployments + preship all green — the core
pipeline is now unblocked end-to-end. But the sub-agents surfaced four
runtime "gotchas" that all classify as self-inflicted / framework-quirk
per `docs/spec-content-surfaces.md` — none are platform traps, all are
agent discovery errors corrected at deploy time:

- nats.js v2 config takes structured `{servers, user, pass}` fields;
  sub-agent composed `nats://user:pass@host:port` URL and got rejected.
- Object-storage endpoint is `https://`; sub-agent wrote `http://` and
  hit 301-to-HTML-parse failure on S3 SDK v3.
- NestJS worker uses `createMicroservice`, not `create()`; sub-agent
  used the HTTP factory first.
- Vite preview log format doesn't match verify `startup_detected` regex
  (engine-side false negative, not gotcha material).

Per `spec-content-surfaces.md` classification taxonomy:
- "Framework quirk" → **DISCARD** (framework docs, not Zerops recipe)
- "Self-inflicted" → **DISCARD** (our code had a bug; reasonable porter
  won't hit it)

All four would be correctly refused at editorial-review even if
recorded. Fixing `zerops_record_fact` to accept v3 sessions (the
obvious next-like-v9.5.5 move) would record more discardable garbage,
not solve the content-quality problem.

### Root cause

None of the three scaffold sub-agents called `zerops_knowledge` during
their runs. They worked from framework training + trial-and-error at
deploy. The reason: v9.5.1's `zerops_knowledge` description rewrite
said **"NOT for authoring a new recipe via zerops_recipe"**. That
exclusion was meant to stop the MAIN agent during RESEARCH from
substituting zerops_knowledge for its framework knowledge (picking
services/versions). Over-broadened in v9.5.1: scaffold / feature /
writer sub-agents read "recipe authoring" as covering their phase too,
and skipped the one tool that would have told them "nats uses
structured fields" and "object-storage is https".

### Fixes shipped

1. **`zerops_knowledge` description narrowed** (`internal/tools/knowledge.go`)
   — exclusion scoped to `zerops_recipe` *research phase* only;
   sub-agents explicitly encouraged to consult for managed-service
   connection patterns before writing client code. Word count stays
   under the 60-word annotation cap.

2. **Scaffold `platform_principles.md` adds "Before writing client
   code" section** — every scaffold sub-agent's brief now tells it to
   call `zerops_knowledge runtime=<type>` or
   `zerops_knowledge query="<service> connection"` for each managed
   service BEFORE writing setup. Names the exact self-inflicted bugs
   that come from skipping (nats URL composition, object-storage
   scheme). Fits the 3 KB scaffold brief cap — earlier draft blew past
   and had to be tightened.

### Deeper lesson

The KB surface captures ONLY platform×framework intersections — not
agent self-inflicted bugs. The "four lost gotchas" framing from run-6
analysis was wrong: the right fix is upstream (stop generating
self-inflicted bugs by making sub-agents consult authoritative sources
first), not downstream (record more of them so editorial-review can
discard them).

### Still deferred

- `zerops_record_fact` + `zerops_workspace_manifest` still gate on v2's
  `engine.SessionID()`. Will bite at finalize (writer reads
  `workspace_manifest`). Same one-file fix pattern as v9.5.5's
  workflow-context probe — deferred until finalize actually hits it.
- `requireAdoption` gate on recipe-provisioned services (see v9.5.5
  section).

---

## 2026-04-24 — run-8-readiness: writer dispatch out, in-phase fragment authorship in

### Context

Run 7 closed 5 phases with trivial gates — structural only, prose
content never validated. The writer sub-agent reconstructed reasoning
from committed files that scaffold + feature already had in hand. That
reconstruction is both the efficiency hole and the quality hole:
stale, guessed causality on the reader-facing surfaces.

Plan: [plans/run-8-readiness.md](plans/run-8-readiness.md). Seven
commits in the order E → A1 → A2 → F → B → C → D, each green on local
tests + `make lint-local` before the next.

### Workstreams shipped

**E — deferred gate plumbing** (feat(recipe): route record_fact +
workspace_manifest under recipe session). `RecipeSessionProbe` gains
`CurrentSingleSession()`; the two v2-shaped tools resolve their target
paths from the single open recipe session's outputRoot instead of
erroring. v2 facts land in `legacy-facts.jsonl`; v3's `facts.jsonl`
stays reserved for `zerops_recipe action=record-fact`.

**A1 — templates + Plan.Fragments schema + assembler + record-fragment**
(refactor(recipe): replace writer dispatch with in-phase fragment
authorship). Engine owns structural templates (`content/templates/*.tmpl`,
string-replace tokens + fragment markers); fragments slot in via
`record-fragment` at the moment the agent holds the densest context.
Writer brief + examples + completion payload deleted. `stitchContent`
now walks surface templates, returns a missing-fragments list callers
gate on.

**A2 — two-root deliverable split + committed-yaml copy**. Per-codebase
`zerops.yaml` is copied verbatim from `Codebase.SourceRoot` (scaffold
sub-agent's workspace) into `outputRoot/codebases/<hostname>/zerops.yaml`,
so inline comments written at decision-moment survive byte-identical
into the published deliverable.

**F — content-authoring briefs + init-commands concept port**. New
`content/principles/init-commands-model.md` (ported from v2's
seed-execonce-keys.md), `briefs/scaffold/content_authoring.md`,
`briefs/feature/content_extension.md`. Engine-side `CitationMap`
(`citations.go`) replaces the deleted writer's citation_topics. Brief
caps raised from 3 KB/4 KB to 5 KB/5 KB — the original caps were set
before F's content was scoped.

**B — phase atom completeness** (feat(recipe): phase atom completeness).
Scaffold atom adds cross-deploy dev→stage + init-commands verification
(success-line attestation + post-deploy data check + burned-key
recovery). Feature atom adds seed step + browser-walk + cross-deploy
dev→stage. Finalize atom adds the single-question test per surface
from spec-content-surfaces.md. Wrapper-discipline refinement clarifies
what the main agent decides vs what the sub-agent discovers.

**C — classification pre-routing** (feat(recipe): engine-side fact
classification as safety net). `Classify` maps surface hints +
citation to the seven-class taxonomy from spec-content-surfaces.md.
`ClassifyLog` partitions publishable from DISCARD-class facts; the
safety net ensures framework-quirk / self-inflicted / library-
metadata records never reach a surface body even if mis-tagged.

**D — spec validators** (feat(recipe): spec validators per surface +
cross-surface uniqueness). Seven per-surface `ValidateFn`s wired via
`RegisterValidator` + `gateSurfaceValidators` on `FinalizeGates`:
root README factuality + deploy-button count + length; env README
meta-agent-voice + tier promotion verb; import-comments causal-word +
templated-opening; codebase IG numbered items + no-scaffold-filenames;
codebase KB bold symptom + citation-map guide references; CLAUDE.md
size floor + custom sections; zerops.yaml causal comments. Cross-
surface uniqueness on fact Topic ids.

### §7 open questions resolved

- **Q1 template format** — string-replace with `{TOKEN}` sigils + post-
  render unreplaced-token scan. Rationale: single substitution engine
  (markers + tokens share the same replace pass), no accidental
  parse failures on fragment bodies containing `{{`, templates diff
  cleanly against the reference `laravel-showcase/README.md`.
- **Q2 marker naming** — kept upstream's `#ZEROPS_EXTRACT_START:NAME#`
  (matches `zcp sync push recipes` extractor).
- **Q3 seed script location** — moot. Atom corpus stays framework-
  neutral per the no-framework-specific-atoms rule; seed shape is the
  sub-agent's framework-expertise call.
- **Q4 browser verification artifact** — FactRecord with
  `Type=browser_verification`, console + screenshot path in Evidence.
  Reuses the facts-log pipeline; no new schema.
- **Q7 committed-yaml-comment validator scope** — validate the WHOLE
  committed file (not just scaffold-authored stanzas). Rationale: that
  file IS the deliverable porters read; authorship origin is not the
  porter's concern.
- **Q9 validator failure → main-agent edit vs re-dispatch** — main-
  agent edits allowed. Iteration via `record-fragment` + re-stitch;
  no scaffold/feature re-dispatch. Preserves densest-context
  authorship the earlier phase already paid for.

### Non-goals / still deferred

- Chain-resolution delta yaml emission (§5.2 of plan) — defer until
  nestjs-minimal gets re-run via v3.
- Automated click-deploy verification — acceptance check 10 stays
  manual at run-8 start.
- `verify-subagent-dispatch` SHA check — real dispatch-integrity
  concern but separate from content-quality; ship after run 8
  confirms the content pipeline works.
- `requireAdoption` fix for recipe-provisioned services — inherited
  from v9.5.5.

### What run 8 proves

1. Every codebase has both dev and stage deploys green.
2. Browser verification recorded as a `browser_verification` fact per
   feature tab.
3. Seed ran once; GET /items returns ≥ 3 items before the agent
   manually creates anything.
4. Stitched output has canonical structure (root README, 6 tier
   READMEs + import.yamls, per-codebase README + CLAUDE.md +
   zerops.yaml).
5. Every finalize-phase validator passes — prose content, not just
   structure.
6. Fragments were authored in-phase; facts log shows `record-fragment`
   calls by scaffold + feature sub-agents, no writer dispatch.
