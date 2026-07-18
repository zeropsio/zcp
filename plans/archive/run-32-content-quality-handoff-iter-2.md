# Recipe content-quality iteration — handoff #2 (post-Pattern-#2 sim)

**Status as of end of session (2026-05-08 PM):**
- Pattern #2 brief edits applied across 5 files; tests + lint clean.
- Step 2 brief re-judge: 11/11 patterns Y.
- Step 3 full sim ran end-to-end (4 codebase + env sub-agents → finalize → refinement → stitch → validate → output judge).
- Validator caught 7 mechanical refusals (6 IG cap overruns, 1 self-inflicted KB).
- Output judge surfaced **12 critical / 16 major / 7 minor defects**.
- User flagged 3 systemic issues that brief edits cannot fix.

**Sim output:** [docs/zcprecipator3/simulations/32-pattern2-fix-1/](../docs/zcprecipator3/simulations/32-pattern2-fix-1/) — browsable end-to-end.

**Nothing committed.** All edits live in working tree; user reviews + commits.

---

## What was done this session

### Pattern #2 (slug-name leakage) brief edits — 5 files modified

| File | Change |
|---|---|
| [docs/spec-content-quality-rubric.md](../docs/spec-content-quality-rubric.md) | Criterion 3 anchors: new 7.0 variant explicitly marks `[init-commands](url)` as forbidden middle ground; 8.5 demoted to descriptive-labeled link variant + in-body completion (preferred); 9.0 retains corollary phrasing. Criterion 6 NATS anchor: `[managed-services-nats](url)` → `[Zerops managed NATS service](url)`. |
| [internal/recipe/content/phase_entry/codebase-content.md](../internal/recipe/content/phase_entry/codebase-content.md) | "Slug citations" rule: 4 forbidden shapes (backticked slug, topic-name handwave, slug-as-link-text, section heading) + 2 acceptable shapes (in-body completion preferred for Zerops mechanics, descriptive-labeled link). Test pin retained: literal phrase "Slug citations" preserved. |
| [internal/recipe/content/briefs/codebase-content/synthesis_workflow.md](../internal/recipe/content/briefs/codebase-content/synthesis_workflow.md) | Citation-map section rewritten with the rule + 8.5/9.0 anchors. MinIO/object-storage example updated to descriptive label. **Trimmed elsewhere to fit `PerPartTokenCeiling = 22000`** — friendly-authority "authoring-tool words leak" PASS example tightened; "Friendly-authority voice scope" worked example collapsed. |
| [internal/recipe/content/briefs/refinement/embedded_rubric.md](../internal/recipe/content/briefs/refinement/embedded_rubric.md) | Criterion 3 anchors with new 7.0 slug-link-text variant + 8.5 in-body + 8.5 descriptive-link. NATS Criterion 6 anchor descriptive-label updated. **Trimmed to fit refinement-brief shrink-cap (60 KB)** — collapsed 8.5/9.0 body duplications. |
| [internal/recipe/content/briefs/refinement/synthesis_workflow.md](../internal/recipe/content/briefs/refinement/synthesis_workflow.md) | Action 4 inline-citation rule extended: slug-name link text counts as forbidden middle ground. NATS worked example + X-Cache CORS example updated. |
| [internal/recipe/content/principles/yaml-comment-style.md](../internal/recipe/content/principles/yaml-comment-style.md) | GOOD example trimmed from 58% density (8 comment lines / 5 yaml) → 38% (3 / 5) so the example matches the 35% rule it teaches. |

### Engine-side files (untracked from prior session — not edited this session)

- [internal/recipe/prod_runtime_base.go](../internal/recipe/prod_runtime_base.go) + `_test.go` — Pattern #8 (service-type/run.base mismatch) is **engine-resolved here**. Parses each codebase's prod `run.base` into `Codebase.ProdRuntimeBase`; emitter at [yaml_emitter.go:500](../internal/recipe/yaml_emitter.go#L500) uses it for `services[].type`. Agent never authors this field. Step 2 judge's "Pattern #8 N" verdict was a false-negative (judge didn't know the engine handles it).

### Test regressions healed

- `TestBuildCodebaseContentBrief_MultiFile_RealSlug_PartsUnderCap` — synthesis_workflow.md exceeded 22000 token cap after Pattern #2 expansion. Trimmed.
- `TestBuildRefinementBrief_BodyUnderShrinkTarget` — refinement brief exceeded 60 KB after embedded_rubric expansion. Trimmed.
- `TestBuildCodebaseContentBrief_PreWarnsTopRejectionPatterns` — test pins literal phrase "Slug citations" in brief body. Preserved.

### Build status

- `go test ./internal/recipe/... -short` — pass.
- `make lint-local` — 0 issues.

---

## Step 3 sim output state — head-to-head vs jetstream golden

Full comparison done by output judge. Reference goldens:
- `/Users/fxck/www/laravel-jetstream-app/{README.md,zerops.yaml}`
- `/Users/fxck/www/recipes/laravel-jetstream/{README.md,3 — Stage/import.yaml}`

### Where candidate exceeds jetstream

1. **KB has actual Zerops × NestJS intersections.** 5 real symptom-anchored traps (NATS URL-embedded creds, MinIO 403 without `forcePathStyle`, TypeORM `synchronize: true` × `minContainers: 2` race, Valkey `${cache_user}` undefined, Meilisearch `ECONNRESET` on `https://`). Jetstream's KB is mostly Laravel-canonical maintenance-mode content, not Zerops-specific.
2. **yaml comments justify decisions, not document fields.** Decision rationale (`npm ci` vs `npm install`, `deployFiles` narrowing, `npm prune --omit=dev` AFTER build, `ts-node` on dev, `os: ubuntu` for SSH tooling). Jetstream often defers to URLs (`Read more about it here:` × 4).
3. **execOnce decomposition teaching.** Two keys (`${appVersionId}-migrate` / `${appVersionId}-seed`) decouple migration vs seed lifecycles. Jetstream just runs `php artisan migrate --isolated --force` with no rationale.

### 19 known defects vs jetstream bar

| # | Defect | candidate file:line | jetstream counter |
|---|---|---|---|
| 1 | IG over-explains, drifts to framework tutorials | apidev/README.md:217-220, 241-245, 283-285, 327-332 — "Adapt path" Express/Fastify/Plain-Express | jetstream IG #2 is 4 lines: `composer require` + edit one file path |
| 2 | No "Production vs. Development" upgrade map | candidate scatters across 6 tier intros + import-comment blocks | jetstream README.md:241-247 — 3 bullets: HA db, minContainers≥2, real SMTP |
| 3 | KB uses flat bullets where shape demands H3+callout | apidev/README.md:341 — TypeORM synchronize is multi-paragraph mechanism crammed into one bullet | jetstream README.md:253-270 — `### Maintenance Mode` + `> [!CAUTION]` callout + fenced shell |
| 4 | Slug-name verbalization in link text (Pattern #2 *not eliminated* — reduced from raw slug to slug-as-English) | apidev/README.md:343,346 `[managed NATS service]` `[Zerops object-storage service]`; workerdev/README.md:312-314,370,390-394 `[Zerops env-var model]` `[Zerops rolling-deploys guide]` × 3 | Jetstream uses `[Laravel Jetstream]`, `[step-by-step tutorial]`, `[zsc health-check]`, `[Laravel documentation]`, `[multi-container setups]` — every label is a real concept, none verbalize an internal slug ID |
| 5 | IG #4 + #5 are conventions, not platform-forced | apidev/README.md:248-332 — "Alias cross-service envs", "Read CORS from project-scope constants" | Jetstream demonstrates aliasing in yaml without promoting it to an IG step |
| 6 | Root README missing 3-codebase orientation | root README.md:24 lines — never tells porter `apidev/`, `appdev/`, `workerdev/` are 3 separate codebases | n/a (jetstream is single-codebase) |
| 7 | Wrong URL on rolling-deploys link (factuality) | workerdev/README.md:390-394 → `docs.zerops.io/features/scaling-ha` | scaling-ha is HA replication; rolling deploys is a separate concept (memory `feedback_horizontal_scaling_vs_ha`) |
| 8 | Wrong concept-bridge target for Svelte recipe | appdev/README.md:160 → NodeJS build-pipeline page | Svelte/Vite recipe sent to NodeJS tutorial. Wrong framework. |
| 9 | dev-tier migration race (factuality) | apidev/zerops.yaml:145-147 — dev runs migrate+seed on every reboot via initCommands | jetstream dev `initCommands` are `composer install` + `npm install` only |
| 10 | Inconsistent S3 own-keys across sibling codebases (factuality) | apidev S3_ACCESS_KEY_ID/S3_SECRET_ACCESS_KEY vs workerdev S3_KEY/S3_SECRET | n/a — same recipe, two conventions for the same secret |
| 11 | KB shape inconsistency across siblings | apidev = no header, workerdev = `## Knowledge base`, appdev = `### Gotchas` | jetstream uses `## Tips and Others` consistently |
| 12 | `zerops_dev_server` leakage in tier yaml comment | environments/1 — Remote (CDE)/import.yaml:23 — `under zerops_dev_server for hot-reload editing` | flow source: facts.jsonl carries 30+ records with this token |
| 13 | Tier intros leak yaml-field jargon | environments/3 — Stage/README.md:6 — `minFreeRamGB: 0.25 adds a spike buffer` | jetstream-side equivalents are plain English |
| 14 | Validator caught 6 IG cap overruns | apidev IG #4 (40 lines), apidev IG #5 (53), worker IG #2 (34), worker IG #3 (49), worker IG #4 (56), worker IG #5 (70) — cap is ≤30 | Jetstream's IG #2 is 4 lines |
| 15 | Validator caught self-inflicted KB bullet | appdev — teaches around recipe's own deployFiles narrowing | n/a |
| 16 | CLAUDE.md ships in deliverable | apidev/CLAUDE.md, appdev/CLAUDE.md, workerdev/CLAUDE.md | n/a — user position: should not be in scope at all |
| 17 | Duplicate `#ZEROPS_EXTRACT_*` markers in CLAUDE.md | All 3 codebases | Stitching artifact (separate from #16) |
| 18 | Cross-framework adapt-path drift in IG (8+ instances despite explicit brief rule) | api/worker/app — Express/Fastify/Plain-Express/Webpack/Next/Astro/SvelteKit teaching | jetstream's IG never lists adapt paths |
| 19 | Worker `DB_HOST`/`DB_PORT`/etc. wired but unused | workerdev/zerops.yaml:53-57 | workerdev/CLAUDE.md says worker doesn't talk to Postgres — env block contradicts |

### Bottom-line verdict on the sim

**Substance:** above jetstream (KB, yaml comments).
**Scope discipline:** well below jetstream (IG length, slug verbalization, cross-framework drift, CLAUDE.md scope).

The recipe reads as a generalized teaching kit ("any Node HTTP framework", "every S3 SDK", "Webpack/Next/Astro/SvelteKit") instead of a NestJS-on-Zerops scaffold a porter clones for their own NestJS app. Strip the adapt-path paragraphs, collapse IG #4 + #5 into yaml comments + KB, fix the slug verbalizations, and the readme shrinks to ~half its current length while teaching more.

---

## 3 structural questions awaiting direction

These cannot be fixed with brief edits.

### Q1: facts.jsonl token sanitization

Authoring-tool tokens (`zerops_dev_server`, `the agent`, `zcli`) flow into porter content via `facts.jsonl` consumed by codebase-content / env-content / finalize sub-agents. Brief teaches "don't" but the facts pile carries the contamination as INPUT.

**Smoking gun:** `environments/1 — Remote (CDE)/import.yaml:23` ships comment *"under zerops_dev_server for hot-reload editing"*. Source: `facts.jsonl` has 30+ `kind=porter_change` records whose `why:` text reads *"the long-running process via zerops_dev_server"*, *"agent owns the watcher"*.

**Two options:**
- (a) Strip authoring-tool tokens from facts BEFORE downstream sub-agents read them.
- (b) Forbid those tokens at scaffold/feature `record-fact` time (refuse the fact).

Either is engine work, not brief work.

### Q2: CLAUDE.md scope

User position: "they are not to be considered at all, not simulated, not read, completely irrelevant to the recipe."

Currently CLAUDE.md is in scope at multiple layers:
- Live pipeline: Phase 4b dispatches `claudemd-author` sub-agent ([internal/recipe/briefs_content_phase.go:328](../internal/recipe/briefs_content_phase.go#L328)).
- Sim emit: copies source codebase's CLAUDE.md into `fragments-new/<host>/codebase__<host>__claude-md.md`.
- Stitch: writes `apidev/CLAUDE.md`, `appdev/CLAUDE.md`, `workerdev/CLAUDE.md` to deliverable.
- Output judge: read it and used it as evidence.

**Decision needed:** drop Phase 4b + sim copy step + stitch write + judge inclusion entirely? Or keep authoring but stop treating CLAUDE.md as deliverable content?

### Q3: Refinement scope reduction

Refinement currently does two jobs:
- **Job A** (legitimate): cross-surface dedup, single-slot URL rewrite, validator gates — things you can only see once stitched.
- **Job B** (problematic): re-teach authoring rules via 7.0/8.5/9.0 anchors that hardcode run-N-specific failures (NestJS X-Cache CORS, NATS URL-cred-stripping, @Controller routing).

**The recognitional-not-generative diagnosis** — proven in this run:
- Refinement caught 4 things its anchors literally describe (wrong NATS URL, NATS body restating IG, two tier-promotion narrative violations).
- Refinement MISSED: all 7 slug-leakage instances, all cross-framework adapt-paths, the migration race, the inconsistent S3 keys, the wrong rolling-deploys URL, the wrong concept-bridge target, the KB shape inconsistency.

For a Django/Rails/Phoenix recipe, the NestJS-shaped anchors will mis-fire or no-op, and any new framework-specific failures get zero anchor coverage.

**Decision needed:** strip refinement of Job B (let codebase-content own positive teaching with framework-neutral worked examples), OR redesign anchors as per-recipe overlays?

---

## Pattern #2 leakage — what brief edits achieved vs didn't

The brief now explicitly forbids `[init-commands](url)` as link text. Agents COMPLIED letter-for-letter: they shipped `[managed NATS service](url)`, `[Zerops env-var model](url)`, `[Zerops rolling-deploys guide](url)`. These are the corpus slugs verbalized into English — rule followed, intent (don't surface recipe-engine taxonomy at all) not internalized.

The agents read the brief's example `[per-deploy initCommands reference]`, recognized it as "descriptive label", then produced English-cased translations of every slug they were told to look up via `zerops_knowledge`. This validates the user's earlier diagnosis: **the agents are doing recognitional pattern-matching rather than generative reasoning.**

**Next-step options for Pattern #2:**
- (a) Add a generative pre-check at codebase-content time: "before recording a fragment, ask if any link text could be read as the slug name spelled differently — if yes, inline".
- (b) Strip the citation-guides list from the brief entirely (currently ~600 bytes of slug-IDs the agent reads as input — even though the brief warns against using them, the agent has them top-of-mind).
- (c) Move all docs links to inline-completion only; ban link-to-Zerops-docs from the brief entirely.

---

## Polish items still pending (from Step 2 judge "Other findings")

Lower-impact items not yet addressed:
- **Pattern #3 polish:** add a FAIL example for "multi-paragraph mechanism wrongly flat-bulleted" so the shape-choice rule has both sides of the FAIL/PASS pair.
- **Pattern #8 explicitness:** add one-line "do NOT author `services[].type` — the engine derives it from prod `run.base`" to the env brief (currently implicit via "engine-emitted at finalize" framing).
- **Citation-guides list lead-in:** one-line note clarifying that the slug list at the bottom of the brief is for `zerops_knowledge` lookup, not link text.

These are minor brief polish; deferring until structural questions are answered.

---

## Pointers for the next instance

### Working tree state to verify on entry

```bash
cd /Users/fxck/www/zcp
git status   # 8 modified, 4 untracked (this iteration's edits + prior session's engine code + 2 plans)
go test ./internal/recipe/... -short   # must pass
make lint-local   # must pass — 0 issues
```

### Sim path

`docs/zcprecipator3/simulations/32-pattern2-fix-1/` — fully stitched + validated. Open `apidev/{README.md,zerops.yaml}` etc. to read the candidate.

### Re-running the sim (if needed)

```bash
go build -o /tmp/zcp-recipe-sim ./cmd/zcp-recipe-sim
/tmp/zcp-recipe-sim emit -run docs/zcprecipator3/runs/32 -out docs/zcprecipator3/simulations/<N>
# dispatch 4 sub-agents (api, app, worker, env) with their respective prompts
# then: stitch → emit-finalize → finalize agent → emit-refinement → refinement agent → stitch → validate
```

The sim's CLI help says `-out` typically lives at `docs/zcprecipator3/simulations/<N>` — *NOT* `/tmp/`. The original handoff's `/tmp/sim-step3` was wrong; the convention is in [cmd/zcp-recipe-sim/emit.go:27](../cmd/zcp-recipe-sim/emit.go#L27).

### Key files to read first

- This handoff (you're reading it).
- [docs/zcprecipator3/pipeline-actor-map.md](../docs/zcprecipator3/pipeline-actor-map.md) — full pipeline with byte budgets and atom load sets per phase.
- [plans/run-32-content-quality-handoff.md](run-32-content-quality-handoff.md) — original kickoff doc that defined the 11 patterns.
- [docs/spec-content-quality-rubric.md](../docs/spec-content-quality-rubric.md) — the rubric that drives refinement scoring.

### Do NOT

- Don't run a fresh sim before the structural questions are answered. The pattern across this iteration is that brief edits land local fixes for systemic problems; another sim cycle without structural change will produce another 12-critical / 16-major report.
- Don't commit anything. Working tree is the user's review surface.
- Don't skip Step 2 (brief judge) before Step 3 (sim). The brief judge is cheap and catches teaching-vs-rule mismatches before paying for sub-agent dispatch.

### User's stance

- "I don't plan full run 33 anytime soon, keep improving then" — no live dogfood imminent; iterate on briefs / structure.
- "(b) is anything" — option (b) from earlier (revert and redesign refinement scope) is on the table.
- Strong reactions to: sim location in `/tmp`, `zerops_dev_server` leakage, CLAUDE.md scope. These three flagged as systemic, not editorial.
