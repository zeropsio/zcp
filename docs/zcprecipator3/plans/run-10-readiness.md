# Run 10 readiness — implementation plan

Run 9 (`nestjs-showcase`, 2026-04-24) was the first v3 dogfood to reach `complete-phase finalize` green. All 11 run-9-readiness workstreams (A1/A2/B/E/G1/G2/H/I/K/J/R) shipped cleanly and every §4 acceptance criterion passed against its own plan (15 PASS, 1 partial PASS). But when the rendered deliverable was compared to the reference apps-repo at `/Users/fxck/www/laravel-showcase-app/`, two structural problems surfaced that the run-9 criteria didn't measure: the apps-repo content was written to an invented directory (`<outputRoot>/codebases/<h>/`) that no published recipe carries and no consumer reads, and the content style diverged materially from the reference (yaml comments compressed to single-line run-ons, README 3× shorter, CLAUDE.md 3× longer, KB format stylistically bimodal within one file, IG missing the reference's center-of-gravity "yaml verbatim" item #1).

Run 10 ships the structural + stylistic fixes that bring the rendered deliverable into parity with the reference, on the same target (`nestjs-showcase`). This plan enumerates what a fresh implementation session ships before run 10 so the next dogfood produces a deliverable whose per-codebase trees at `<SourceRoot>/` are directly comparable — length, structure, voice — to `laravel-showcase-app/`.

Reference material (all already written):
- [docs/zcprecipator3/runs/9/ANALYSIS.md](../runs/9/ANALYSIS.md) — run 9 verdict (PAUSE), 16-criterion scorecard, gap L write-up, wall-time breakdown.
- [docs/zcprecipator3/runs/9/CONTENT_COMPARISON.md](../runs/9/CONTENT_COMPARISON.md) — systematic diff of run-9 output vs `/Users/fxck/www/laravel-showcase-app/` reference: length table, yaml-comment style contrast, README-IG structure contrast, KB format bimodal evidence, CLAUDE.md length inversion.
- [docs/zcprecipator3/runs/9/PROMPT_ANALYSIS.md](../runs/9/PROMPT_ANALYSIS.md) — timeline narrative, four sub-agent prompts extracted + graded, 11 smells with fix directions.
- [docs/zcprecipator3/runs/9/TIMELINE.md](../runs/9/TIMELINE.md) — main-agent-authored run 9 build log.
- [docs/zcprecipator3/plans/run-9-readiness.md](run-9-readiness.md) — prior run's plan. §2 workstreams A1–R shipped in v9.5.8 and stay in force.
- [docs/zcprecipator3/CHANGELOG.md](../CHANGELOG.md) — 2026-04-24 entry describes v9.5.8.
- [docs/spec-content-surfaces.md](../../spec-content-surfaces.md) — seven surface contracts + classification taxonomy + anti-patterns.
- [/Users/fxck/www/laravel-showcase-app/](/Users/fxck/www/laravel-showcase-app/) — apps-repo-shape reference (single-codebase Laravel monolith with `README.md` + `CLAUDE.md` + `zerops.yaml` + source all at repo root). Run 10's per-codebase output at `<SourceRoot>/` must match this shape + voice.
- [/Users/fxck/www/recipes/](/Users/fxck/www/recipes/) — ~20 published recipes (laravel-minimal, laravel-showcase, nestjs-hello-world, php-hello-world, etc.). Every single one is: root `README.md` + 6 tier folders, each with `README.md` + `import.yaml`. **Zero have a `codebases/` subdirectory.** This is the recipes-repo canonical shape and the authoritative evidence for why workstream L exists.

Run-9 artifacts: [docs/zcprecipator3/runs/9/](../runs/9/)

---

## 0. Preamble — context a fresh instance needs

### 0.1 What v3 is (one paragraph)

zcprecipator3 (v3) is the Go recipe-authoring engine at [internal/recipe/](../../../internal/recipe/). Given a slug (e.g. `nestjs-showcase`), v3 drives a five-phase pipeline (research → provision → scaffold → feature → finalize) producing a deployable Zerops recipe. The engine never authors prose — it renders templates, substitutes structural tokens, splices in-phase-authored fragments into extract markers, reads committed per-codebase `zerops.yaml` verbatim from the SSHFS mount, classifies facts, and runs surface validators. Sub-agents (Claude Code `Agent` dispatch) author codebase-scoped fragments at the moment they hold the densest context; the main agent authors root + env fragments at finalize. The `<outputRoot>` tree lives under `/var/www/recipes/<slug>/` during a live run (today; this plan changes `<SourceRoot>` writes too — see §2.L). v2 — the older bootstrap/develop workflow engine at [internal/content/](../../../internal/content/) + [internal/workflow/](../../../internal/workflow/) — is frozen at v8.113.0.

### 0.2 Two deliverable shapes, one engine

v3 produces two published artifacts from a single run, intended for two different GitHub repositories:

- **recipes-repo shape** — published to `zeropsio/recipes/<slug>/`. Shape: root `README.md` + 6 tier folders (`0 — AI Agent/` … `5 — Highly-available Production/`), each with `README.md` + `import.yaml`. Verified canonical across ~20 existing published recipes at [/Users/fxck/www/recipes/](/Users/fxck/www/recipes/). Does NOT contain a `codebases/` subdirectory.
- **apps-repo shape** — published to `zerops-recipe-apps/<slug>-<codebase>/` (one repo per codebase). Shape: `README.md` + `CLAUDE.md` + `zerops.yaml` + full source tree, ALL at repo root. Reference: [/Users/fxck/www/laravel-showcase-app/](/Users/fxck/www/laravel-showcase-app/) — a single-codebase monolith with Laravel source (`app/`, `bootstrap/`, `config/`, `database/`, `public/`, etc.) at root alongside the three metadata files.

**Critical constraint for run 10**: the apps-repo shape's source lives at the SSHFS-mounted dev slot path, `/var/www/<hostname>dev/`. In v3 terms this is `cb.SourceRoot`. The engine must write README + CLAUDE to this path directly — NOT to a separate `<outputRoot>/codebases/<hostname>/` subdirectory. Run 9 wrote them to the separate subdirectory; run 10 corrects this.

### 0.3 Where run 9 stopped + classes of defect

Run 9 closed all five phases GREEN on the fourth `complete-phase` finalize call. Every run-9-readiness criterion passed against its own plan. But post-hoc comparison to `/Users/fxck/www/laravel-showcase-app/` surfaced:

**Structural defects** (shape of the deliverable tree is wrong):
- **L** — engine writes apps-repo content to `<outputRoot>/codebases/<h>/` (a directory invented for v3-to-v3 chain resolution that has never fired against real input — no published recipe has the directory). The reference shape wants README + CLAUDE + yaml at the same tree as source, which is `<SourceRoot>/`. See [runs/9/ANALYSIS.md §3 gap L](../runs/9/ANALYSIS.md).
- **M** — README Integration Guide's item #1 is prose describing the yaml in English instead of embedding the yaml verbatim. The reference's IG #1 is a fenced `zerops.yaml` code block with inline comments — the center of gravity of the porting guide. Run 9's IG has 10 prose items and no code block. Porter can't see the config from README alone.

**Stylistic defects** (voice + formatting diverge from reference):
- **N** — yaml comments are single-line run-ons stuffed with `because`/`so that`/`otherwise`. The reference uses multi-line natural prose wrapped at ~65 chars with bare `#` separators. Driver: the `yaml-comment-missing-causal-word` validator fires per-line, so sub-agents pack every line with a causal word rather than letting a block carry the logic.
- **O** — README knowledge-base is stylistically bimodal within one file: scaffold-authored entries use `**symptom**: X. **mechanism**: Y. **fix**: Z.` debugging-runbook triples; feature-authored entries use `**Topic** — explanation` matching the reference. Same file, two personalities.
- **P** — CLAUDE.md is inverted in length: reference is 33 lines (one-fact-per-line operator cheat sheet); run 9's is 99 lines (tutorial-length with multiple sub-sections). The `claude-md-too-few-custom-sections` validator pressures sub-agents to ADD sections during finalize iteration, which is the wrong direction.

**Engine-brief hygiene** (the composed prompt has internal contradictions + noise):
- **Q** — four low-cost fixes to the engine-composed brief identified in [runs/9/PROMPT_ANALYSIS.md §2.2](../runs/9/PROMPT_ANALYSIS.md): (1) HTTP section emitted unconditionally regardless of `role.ServesHTTP`; (2) `# Pre-ship contract` header is simultaneously structural AND a forbidden voice-leak phrase; (3) four finalize-time validator rules have no author-time equivalent in the scaffold brief, causing ~15 min of iteration burn; (4) `execOnce` edge case (arbitrary-static-string key) triggered a 5-query knowledge loop in run 9's feature sub-agent because the injected atom doesn't cover it.

### 0.4 Workstream legend (L / M / N / O / P / Q)

Each workstream maps to one class of defect above. Tranche structure sequences them by dependency.

| Letter | Scope | Tranche |
|---|---|---|
| L | Redirect apps-repo content from `<outputRoot>/codebases/<h>/` to `<SourceRoot>/`; delete `copyCommittedYAML` | 1 |
| M | Auto-embed `<SourceRoot>/zerops.yaml` as IG item #1 during stitch | 1 |
| N | Loosen `yaml-comment-missing-causal-word` from per-line to per-block | 2 |
| O | Unify KB format as `**Topic** — explanation`; ban `**symptom**:` triple in KB | 2 |
| P | Invert `claude-md-too-few-custom-sections` to `claude-md-too-long`; cap ~50 lines | 2 |
| Q | Engine-brief hygiene (HTTP gating, header rename, validator rules ported to brief, execOnce atom edge) | 3 |

Tranches run serially: Tier 1 unblocks the shape (without L+M, run 10's output is still structurally wrong); Tier 2 fixes content style inside the new correct shape; Tier 3 is author-time polish that reduces finalize-iteration cost. Run 10 can ship Tier 1+2 and be viable; Tier 3 is strongly recommended but not structurally blocking.

---

## 1. Goals for run 10

A recipe run of `nestjs-showcase` that, compared directly to `/Users/fxck/www/laravel-showcase-app/`:

1. **Writes per-codebase `README.md` + `CLAUDE.md` + `zerops.yaml` into `<cb.SourceRoot>/`**. A porter running `git init && git add -A && git commit && git push` from `/var/www/apidev/` gets a shape-equivalent repo to `laravel-showcase-app/` (modulo framework-specific source).
2. **Does not write `<outputRoot>/codebases/<h>/` at all.** The invented directory disappears from the output tree.
3. **README Integration Guide item #1 is a fenced `yaml` code block** containing the committed `zerops.yaml` verbatim, with inline comments preserved. Subsequent IG items (authored via fragments) explain app-side code changes.
4. **YAML inline comments read as multi-line natural prose.** Sub-agents author comments that wrap across 2–4 lines with one causal word per block, matching the reference. Per-line causal-word pressure is gone.
5. **README knowledge-base uses one consistent format** — `**Topic** — explanation` across scaffold and feature entries. No `**symptom**: / **mechanism**: / **fix**:` triples in KB.
6. **CLAUDE.md is ≤ 60 lines per codebase.** Cross-codebase operational notes ("Quick curls", "Smoke tests") do not live in codebase-specific CLAUDE.md; codebase-specific service facts + dev-loop + notes do.
7. **Engine brief omits the HTTP section on `ServesHTTP: false` roles**, renames `# Pre-ship contract` to `# Behavioral gate`, ports the four validator rules as "Validator tripwires" author-time guidance, and pre-answers the `execOnce` arbitrary-static-key case in the feature brief.

Stretch: criterion 10 from run-9-readiness (click-deployable end-to-end, manual validation) becomes directly testable because the apps-repo content is now at `<SourceRoot>` — the tooling that creates `zerops-recipe-apps/<slug>-<codebase>` repos can push that tree as-is.

---

## 2. Workstreams

### 2.0 Guiding principles

Three invariants the implementation session must hold:

1. **No architectural work.** Every gap below is a small patch. None of them justifies redesigning state, renaming types, or splitting packages.
2. **Delete before adding.** L deletes an invented directory + a redundant copy function. Tier-2 style fixes change validator scope (loosening rules, inverting thresholds) rather than adding new constraints. Tier-3 closes gaps via small edits to the brief composer.
3. **Reference is authority.** When in doubt about shape / style / length, the rule is "does it match `laravel-showcase-app/`?" — not "does it match run-9 output?". If a test or validator disagrees with the reference, the test/validator is wrong and gets updated.

### 2.L — redirect apps-repo content from invented subdirectory to `<SourceRoot>`

**What run 9 showed** ([runs/9/ANALYSIS.md §3 gap L](../runs/9/ANALYSIS.md), [CONTENT_COMPARISON.md §1](../runs/9/CONTENT_COMPARISON.md)): rendered tree at run close contained:

```
<outputRoot>/codebases/api/
├── CLAUDE.md       ← written by stitch-content
├── README.md       ← written by stitch-content
└── zerops.yaml     ← copied verbatim from /var/www/apidev/zerops.yaml

<cb.SourceRoot> = /var/www/apidev/
├── src/ scripts/ nest-cli.json ... (NestJS source)
└── zerops.yaml     ← authored by scaffold (byte-identical to the copy above)
```

Gap evidence:
- **`find /Users/fxck/www/recipes -name codebases -type d`** returns zero matches across ~20 published recipes.
- **[chain.go:73](../../../internal/recipe/chain.go#L73)** `loadParent` reads `<parentDir>/codebases/<h>/` — the only consumer. The `if entries, err := os.ReadDir(codebasesDir); err == nil` guard falls through silently on every real parent (no published v2 parent has the directory). The feature has never fired against real input.
- **Reference `/Users/fxck/www/laravel-showcase-app/`** has `README.md` + `CLAUDE.md` + `zerops.yaml` + source all at repo root. No `codebases/` subdirectory anywhere.

**Root cause (named files)**:
- [handlers.go:447](../../../internal/recipe/handlers.go#L447) — `cbRoot := filepath.Join(outputRoot, "codebases", cb.Hostname)` in `stitchContent`.
- [handlers.go:458–475](../../../internal/recipe/handlers.go#L458) — `copyCommittedYAML` reads `<SourceRoot>/zerops.yaml` and writes `<outputRoot>/codebases/<h>/zerops.yaml`. The write target is unused (no reader); the source-side file is already correct.
- [validators.go:213](../../../internal/recipe/validators.go#L213) — `resolveSurfacePaths` codebase case returns `filepath.Join(outputRoot, "codebases", cb.Hostname, leaf)`. Points validators at the invented directory instead of the source.

**Fix direction** — three edits + five test updates:

1. **[handlers.go](../../../internal/recipe/handlers.go) `stitchContent` codebase-write loop**: replace `cbRoot := filepath.Join(outputRoot, "codebases", cb.Hostname)` with `cbRoot := cb.SourceRoot`. README + CLAUDE land at `<SourceRoot>/README.md` + `<SourceRoot>/CLAUDE.md`. No change to how fragments assemble — only the write target.
2. **Delete `copyCommittedYAML` entirely**. The scaffold authored `<SourceRoot>/zerops.yaml` during its phase; validator N will read from there. No copy needed.
3. **[validators.go](../../../internal/recipe/validators.go) `resolveSurfacePaths` codebase case**: return `filepath.Join(cb.SourceRoot, leaf)` so validators read from the same tree the stitch writes to.
4. **[chain.go:73](../../../internal/recipe/chain.go#L73)**: leave untouched. Chain resolution is already a no-op against every real parent; we don't fix it in this run. See §5 non-goals.

**Changes**:
- [handlers.go](../../../internal/recipe/handlers.go) — ~5 LoC redirect + `copyCommittedYAML` deletion (~20 LoC removed).
- [validators.go](../../../internal/recipe/validators.go) — ~3 LoC path redirect.
- Update 5 tests that pin the old path:
  - [handlers_test.go:307](../../../internal/recipe/handlers_test.go#L307)
  - [assemble_test.go:499, 568](../../../internal/recipe/assemble_test.go)
  - [phase_entry_test.go:413–414, 428](../../../internal/recipe/phase_entry_test.go)

**Net LoC**: negative (delete more than add).

**Test coverage** (new tests in [handlers_test.go](../../../internal/recipe/handlers_test.go)):
- `TestStitch_WritesCodebaseReadmeToSourceRoot` — fixture with `<SourceRoot>` = temp dir; after stitch, `<SourceRoot>/README.md` and `<SourceRoot>/CLAUDE.md` exist with correct content.
- `TestStitch_NoOutputRootCodebasesDirectory` — after stitch, `<outputRoot>/codebases/` does not exist. This is the regression guard.
- `TestValidate_CodebaseSurface_ReadsSourceRoot` — validators find surface content at `<SourceRoot>/README.md`.

**Watch**: if a future A-series workstream redesigns chain resolution, the parent-side path in `chain.go` will need updating to read apps-repo-shaped parents. Not run-10 scope.

### 2.M — auto-embed `<SourceRoot>/zerops.yaml` as IG item #1 during stitch

**What run 9 showed** ([runs/9/CONTENT_COMPARISON.md §4](../runs/9/CONTENT_COMPARISON.md)): all three codebases' IG item #1 is a prose paragraph named `**`zerops.yaml` layout** — ...` describing the yaml in English. Example from [runs/9/environments/codebases/api/README.md:16](../runs/9/environments/codebases/api/README.md):

> 1. **`zerops.yaml` layout** — The committed `zerops.yaml` declares two setups: `dev` (deploys the full source tree with `start: zsc noop --silent` ...) and `prod` (compiles TypeScript, ships `dist/` + production `node_modules` + `package.json`, runs the compiled entry under Node with `readinessCheck` + `healthCheck` gated on `/health`). ...

The reference at [/Users/fxck/www/laravel-showcase-app/README.md:16–308](/Users/fxck/www/laravel-showcase-app/README.md) uses a completely different pattern:

```markdown
## Integration Guide

<!-- #ZEROPS_EXTRACT_START:integration-guide# -->

### 1. Adding `zerops.yaml`

The main configuration file — place at repository root. It tells Zerops how to build, deploy and run your app.

\`\`\`yaml
zerops:
  # Production — optimized build, compiled assets, framework caches,
  # full service connectivity (DB, Redis, S3, Meilisearch).
  - setup: prod
    build:
      base:
        - php@8.4
        - nodejs@22
      ...  (283 lines of yaml with multi-line inline comments)
\`\`\`

### 2. Trust the reverse proxy
...
```

Item #1 is a fenced yaml code block containing the FULL `zerops.yaml` verbatim (with inline comments). Items #2 onwards teach the app-side code changes (`trust proxies`, Redis client, S3, Meilisearch). That yaml block is the center of gravity of the porting guide — it's the config the porter will paste into their own project.

**Root cause** — the scaffold brief's IG guidance ([content_authoring.md](../../../internal/recipe/content/briefs/scaffold/content_authoring.md)) says `codebase/<h>/integration-guide` is "porter-facing numbered items" with no mandate about what item #1 must contain. The finalize validator `codebase-ig-first-item-not-zerops-yaml` checks that item #1 *mentions* the string `zerops.yaml` — satisfied by a prose paragraph that names it. Nothing requires a code block.

**Fix direction** — the stitch engine auto-generates item #1 as a code block; sub-agents' fragments become items #2 onwards.

1. **[handlers.go](../../../internal/recipe/handlers.go) `assembleCodebaseREADME`** (or the equivalent assemble path): between the intro marker and the IG extract-start marker, prepend a template:

   ```markdown
   ### 1. Adding `zerops.yaml`

   The main configuration file — place at repository root. It tells Zerops how to build, deploy and run your app.

   \`\`\`yaml
   {{CONTENTS OF <cb.SourceRoot>/zerops.yaml, verbatim}}
   \`\`\`

   ```

   Inject BEFORE the `codebase/<h>/integration-guide` fragment's content. The fragment's items are renumbered starting from 2 when rendered (or simply appended after the engine-generated item 1, with the sub-agent's numbering preserved as `2`, `3`, `4`, ... in the authored fragment).

2. **[content_authoring.md](../../../internal/recipe/content/briefs/scaffold/content_authoring.md)** — rewrite the IG guidance: "Item #1 is auto-generated by the engine (full yaml block + intro sentence). Your authored `codebase/<h>/integration-guide` fragment contains items #2 onwards — porter-facing app-side changes (e.g. `trust proxy`, client library, env-var wiring). Start item #2 with a heading like `### 2. Trust the reverse proxy`."

3. **[validators_codebase.go](../../../internal/recipe/validators_codebase.go)** — the `codebase-ig-first-item-not-zerops-yaml` check stays in place as a safety net. After M ships, the engine itself produces item #1, so the check is trivially satisfied; it becomes a regression guard against accidental template changes.

4. **Handle feature-phase extensions**: the feature sub-agent's `record-fragment` appends to `codebase/<h>/integration-guide`. Its append semantics stay the same; items land as #N+1, #N+2 where N is the last scaffold-authored item number. No change needed.

**Changes**:
- [handlers.go](../../../internal/recipe/handlers.go) — prepend yaml-block item to IG during assemble. ~30 LoC + one read of `<cb.SourceRoot>/zerops.yaml`.
- [content_authoring.md](../../../internal/recipe/content/briefs/scaffold/content_authoring.md) — rewrite IG guidance. ~15 lines.
- New test pin.

**Test coverage** (new tests in [handlers_test.go](../../../internal/recipe/handlers_test.go) or [assemble_test.go](../../../internal/recipe/assemble_test.go)):
- `TestStitch_IGFirstItemIsEmbeddedYaml` — after stitch, `<SourceRoot>/README.md` IG section opens with `### 1. Adding \`zerops.yaml\`` followed by a fenced yaml block whose content matches `<SourceRoot>/zerops.yaml` byte-for-byte.
- `TestStitch_IGSubsequentItemsArePorterItems` — after stitch, item #2 onwards come from the `codebase/<h>/integration-guide` fragment.
- `TestStitch_IGWorksWithEmptyFragment` — if the sub-agent never recorded the IG fragment, the engine's item #1 still renders cleanly (graceful fallback).

**Watch**: the yaml block's rendering must preserve inline `#` comments exactly. If the scaffold-authored yaml has comments formatted per workstream N (multi-line prose blocks), those get visible in the README IG — which is the whole point.

### 2.N — loosen `yaml-comment-missing-causal-word` from per-line to per-block

**What run 9 showed** ([runs/9/CONTENT_COMPARISON.md §3](../runs/9/CONTENT_COMPARISON.md)): sub-agent-authored yaml comments are single-line run-ons packing 2–3 causal clauses into one line. Example from [runs/9/environments/codebases/api/zerops.yaml:2](../runs/9/environments/codebases/api/zerops.yaml):

```yaml
  # Dev setup — deploys the full source tree so that SSH sessions and `zerops_dev_server` can drive `nest start --watch` without a rebuild. `zsc noop --silent` keeps the container idle so that an external watcher owns the long-running process, otherwise every code edit would force a redeploy.
  - setup: dev
```

One line, ~280 characters, 3 `so that`/`otherwise` clauses stacked. Contrast with [/Users/fxck/www/laravel-showcase-app/zerops.yaml:56–68](/Users/fxck/www/laravel-showcase-app/zerops.yaml):

```yaml
      # Config, route, and view caches MUST be built at runtime.
      # Build runs at /build/source/ but the app serves from
      # /var/www/ — caching during build bakes wrong paths.
      #
      # Migrations run exactly once per deploy via zsc execOnce,
      # regardless of how many containers start in parallel.
      # Seeder populates sample data on first deploy so the
      # dashboard shows real records immediately.
```

Multi-line block, wraps at ~65 chars, bare `#` between paragraphs, one em-dash carries the causal weight for the whole first paragraph, no causal word in most individual lines.

**Root cause**: [validators.go::yamlCommentRE](../../../internal/recipe/validators.go) matches every comment line individually. The causal-word check at [validators_codebase.go:132–156](../../../internal/recipe/validators_codebase.go#L132) iterates line-by-line and rejects any line whose content (after stripping the `#`) doesn't contain one of `because`/`so that`/`otherwise`/`trade-off`/em-dash. Reference-style multi-line comments fail at every non-causal line; sub-agents under pressure compress everything to one line with all causal words.

**Fix direction** — validate the BLOCK (run of adjacent `#` comment lines), not the line.

1. **[validators_codebase.go](../../../internal/recipe/validators_codebase.go) `validateCodebaseYAML`** — rewrite the causal-word loop:
   - Parse the yaml into "comment blocks" = runs of consecutive lines that start with `#` (possibly with leading whitespace), stopping at a blank line or non-comment line.
   - For each block, check if ANY line in the block contains a causal word / em-dash.
   - Lines shorter than 40 characters after stripping `#` pass unconditionally (treated as labels).
   - Bare `#` lines (section transitions) pass unconditionally (existing H behavior, preserved).
   - Emit one violation per block-without-causal-word, not one per line.

2. **Violation message update** — name the block's first line + suggest "add a `because` / `so that` / `otherwise` / em-dash in any line of this comment block".

3. **[content/principles/yaml-comment-style.md](../../../internal/recipe/content/principles/yaml-comment-style.md)** — update the atom to teach the reference style: "Wrap comments at ~65 chars. Use multi-line blocks for anything longer than a label. One causal word per block is enough. Use bare `#` as section separator." Add 2 good/bad examples matching the reference.

**Changes**:
- [validators_codebase.go](../../../internal/recipe/validators_codebase.go) — causal-word loop rewrite. ~40 LoC.
- [validators.go](../../../internal/recipe/validators.go) — new helper `parseYamlCommentBlocks(body) [][]string` if useful. ~20 LoC.
- [principles/yaml-comment-style.md](../../../internal/recipe/content/principles/yaml-comment-style.md) — content update. ~30 bytes of new atom prose.

**Test coverage** (new + updated tests in [validators_test.go](../../../internal/recipe/validators_test.go)):
- `TestValidateCodebaseYAML_MultiLineBlockWithOneCausalWord_Passes` — block of 6 lines where only line 2 has `because`; validator emits zero violations.
- `TestValidateCodebaseYAML_MultiLineBlockNoCausalWord_OneViolationPerBlock` — block of 4 lines with no causal word anywhere; validator emits exactly one violation (not four).
- `TestValidateCodebaseYAML_ShortLabelComment_Passes` — `# Base image` alone passes (short label, no causal word required).
- `TestValidateCodebaseYAML_BareHashTransitionAllowed` — a single bare `#` between blocks passes (no regression from H).
- Update existing tests that assert per-line failures — they now assert per-block.

**Watch**: the reference yaml has some purely-descriptive single-line comments ("php-nginx serves via Nginx + PHP-FPM — no explicit start command needed...") that satisfy the per-line rule via em-dash. Those still pass. The rule change only LOOSENS; nothing previously passing starts failing.

### 2.O — unify KB format as `**Topic** — explanation`; ban `**symptom**:` triple

**What run 9 showed** ([runs/9/CONTENT_COMPARISON.md §5](../runs/9/CONTENT_COMPARISON.md)): [runs/9/environments/codebases/api/README.md:72–127](../runs/9/environments/codebases/api/README.md) knowledge-base is stylistically bimodal:

Scaffold-authored entries (first 8 bullets):
```markdown
- **symptom**: 502 from the project L7 balancer on every request even though `curl http://localhost:3000/health` succeeds from inside the container. **mechanism**: NestJS's `app.listen(port)` defaults to binding `127.0.0.1`; the balancer routes to the container's VXLAN IP and there is no listener there. **fix**: pass `'0.0.0.0'` as the second argument to `app.listen()`. Cited guide: `http-support`.
```

Feature-authored entries (last 6 bullets):
```markdown
- **Expose X-Cache via CORS** — a cross-origin `fetch` against the api only sees headers listed under `Access-Control-Expose-Headers`. The cache-demo tab needs the `X-Cache` value, so `app.enableCors()` passes `exposedHeaders: ['X-Cache']` in `main.ts`. Without it, `res.headers.get('X-Cache')` returns `null` in the browser even though curl sees the header.
```

Reference style at [laravel-showcase-app/README.md:349–355](/Users/fxck/www/laravel-showcase-app/README.md) matches the feature entries:

```markdown
- **No `.env` file** — Zerops injects environment variables as OS env vars. Creating a `.env` file with empty values shadows the OS vars, causing `env()` to return `null` for every key that appears in `.env` even if the platform has a value set.
```

Root cause: the scaffold brief's preship-contract section implicitly teaches the symptom/mechanism/fix trio via its own 5-item behavioral structure. Sub-agents generalize the trio into KB entries. Feature brief has no such pressure, defaults to reference style.

**Fix direction** — make `**Topic** — explanation` the one format; move debugging triples to CLAUDE.md.

1. **[content_authoring.md](../../../internal/recipe/content/briefs/scaffold/content_authoring.md)** — rewrite the KB format guidance: "Each KB bullet opens with `**<concise topic>**` then an em-dash then natural-prose explanation, 2–5 sentences. Do NOT use `**symptom**:` / `**mechanism**:` / `**fix**:` triples in KB — that format belongs in `codebase/<h>/claude-md/notes` as a debugging runbook. A porter reading the README wants to scan topic names to find the entry that matches their problem."

   Add 2 good/bad examples showing a platform-trap entry in both formats.

2. **[content_extension.md](../../../internal/recipe/content/briefs/feature/content_extension.md)** — reinforce the same rule (feature sub-agent has a looser brief on this; pin it).

3. **New validator `codebase-kb-triple-format-banned`** in [validators_codebase.go](../../../internal/recipe/validators_codebase.go) — scan every KB bullet in `codebase/<h>/knowledge-base`. Regex: `^\s*[-*]\s*\*\*symptom\*\*:` or similar. Emit violation with message "KB entries use `**Topic** — prose` format; `**symptom**:` triples belong in `claude-md/notes`".

**Changes**:
- [content_authoring.md](../../../internal/recipe/content/briefs/scaffold/content_authoring.md) + [content_extension.md](../../../internal/recipe/content/briefs/feature/content_extension.md) — ~400 bytes of new guidance + 2 good/bad pairs each.
- [validators_codebase.go](../../../internal/recipe/validators_codebase.go) — new regex + scan function. ~30 LoC.

**Test coverage**:
- `TestValidateKB_AllTripleFormat_FlagsAll` — KB with 3 `**symptom**:` entries; validator emits 3 violations.
- `TestValidateKB_AllTopicFormat_Passes` — reference-style KB; no violations.
- `TestValidateKB_MixedFormat_FlagsOnlyTriples` — bimodal KB (like run-9 output); only triple entries flagged.
- `TestBrief_Scaffold_KBGuidanceMatchesTopicFormat` — the brief body contains the new guidance.

**Watch**: the scaffold sub-agent's "symptom/mechanism/fix" thinking is valuable — it produces high-quality debugging content. This workstream moves that content to the right surface (CLAUDE.md/notes), doesn't eliminate it. The scaffold brief should reinforce that CLAUDE.md/notes IS the right home for runbook-style entries.

### 2.P — cap CLAUDE.md length; invert `claude-md-too-few-custom-sections`

**What run 9 showed** ([runs/9/CONTENT_COMPARISON.md §6](../runs/9/CONTENT_COMPARISON.md)):

| File | Reference | Run 9 (api) | Ratio |
|---|---|---|---|
| CLAUDE.md | 33 lines | 99 lines | **3.0× LONGER** |

Reference [laravel-showcase-app/CLAUDE.md](/Users/fxck/www/laravel-showcase-app/CLAUDE.md) is one-fact-per-line operator cheat sheet:
- 3-line intro paragraph.
- `## Zerops service facts` — 4 bullets.
- `## Zerops dev (hybrid)` — one paragraph + HMR fork + "Do NOT" rule.
- `## Notes` — 5 bullets.

Run 9 [runs/9/environments/codebases/api/CLAUDE.md](../runs/9/environments/codebases/api/CLAUDE.md) is tutorial-length:
- 10+ service-facts bullets (each 3–6 lines with code-block examples).
- Multiple Notes sub-sections: Dev loop, Redeploy vs edit, Quick curls, In-container curls, Framework CLIs, Cross-deploy, Boot-time connectivity.
- Seed + re-seed operations prose block.
- Local curl smoke test code block.

Driver: [validators_codebase.go](../../../internal/recipe/validators_codebase.go) currently enforces `claude-md-too-few-custom-sections` (minimum 2 custom sections beyond template). Round 1 of run-9 finalize tripped this on `app/CLAUDE.md`; author added sections to satisfy. The pressure is wrong-direction.

**Fix direction** — invert the validator; cap target length.

1. **[validators_codebase.go](../../../internal/recipe/validators_codebase.go)**:
   - Delete `claude-md-too-few-custom-sections`.
   - Add `claude-md-too-long` — emits violation if `len(lines(body)) > 60`.
   - Add `claude-md-forbidden-subsection` — emits violation if CLAUDE.md contains any of these cross-codebase sections: "Quick curls", "Smoke test", "Smoke tests", "Local curl", "In-container curls", "Redeploy vs edit", "Boot-time connectivity". Rationale: these are recipe-level operator notes, same across all codebases; they belong in the recipes-repo root README or a single dev-loop guide, not repeated per codebase.

2. **[content_authoring.md](../../../internal/recipe/content/briefs/scaffold/content_authoring.md)** — update CLAUDE.md guidance: "Target 30–50 lines. One fact per line for service-facts; multi-line only when a fact carries a code-example. Do NOT include: Quick curls, smoke tests, redeploy instructions, or other content identical across codebases — those belong in the recipe's root guidance, not here. Use the reference `laravel-showcase-app/CLAUDE.md` (33 lines) as the shape benchmark."

3. **[content_extension.md](../../../internal/recipe/content/briefs/feature/content_extension.md)** — reinforce the length cap for feature extensions.

**Changes**:
- [validators_codebase.go](../../../internal/recipe/validators_codebase.go) — delete one validator, add two. ~40 LoC net change.
- [content_authoring.md](../../../internal/recipe/content/briefs/scaffold/content_authoring.md) + [content_extension.md](../../../internal/recipe/content/briefs/feature/content_extension.md) — ~300 bytes each.

**Test coverage**:
- `TestValidateCLAUDE_TooLong_Flagged` — 70-line CLAUDE.md triggers `claude-md-too-long`.
- `TestValidateCLAUDE_UnderCap_Passes` — 45-line CLAUDE.md passes.
- `TestValidateCLAUDE_ForbiddenSubsection_Flagged` — CLAUDE.md with a `## Quick curls` heading triggers `claude-md-forbidden-subsection`.
- `TestValidateCLAUDE_NormalSections_Pass` — CLAUDE.md with `## Zerops service facts` + `## Notes` passes.
- Remove tests that asserted `claude-md-too-few-custom-sections` triggers.

**Watch**: the forbidden-subsection list might over-reject. If a future codebase legitimately has its own "Boot-time connectivity" concern distinct from the recipe-wide one, allow an opt-out via a `recipe-level-acknowledged` comment or similar. Not run-10 scope; add if needed later.

### 2.Q — engine-brief hygiene (four sub-items, Tier 3)

**What run 9 showed** ([runs/9/PROMPT_ANALYSIS.md §2.2 + §3 smells S4, S5, S11](../runs/9/PROMPT_ANALYSIS.md)):

**Q1 — HTTP section emitted regardless of role.** [runs/9/SESSION_LOGS/main-session.jsonl line 75](../runs/9/SESSION_LOGS/main-session.jsonl) scaffold-worker prompt (engine-composed portion, lines 11–17) says:

```
## Role contract
- ServesHTTP: false
- RequiresSubdomain: false
- ProcessModel: job-consumer

# Platform obligations

## HTTP (ServesHTTP=true)

- Bind `0.0.0.0`, read `PORT` — loopback is unreachable
- Trust `X-Forwarded-*` headers (L7 balancer sets them)
- `zerops.yaml run.ports: { port: <PORT>, httpSupport: true }`
```

The HTTP section's header `(ServesHTTP=true)` reads like a conditional annotation but is emitted unconditionally. Worker sub-agent has to mentally skip the section.

**Q2 — `# Pre-ship contract` is both structural AND forbidden.** Same brief, line 42 header `# Pre-ship contract`. Line 83 forbidden-phrase list: `"pre-ship contract"`, `"pre-ship contract item N"`. Sub-agent holds a mental partition between authoring-vocabulary and output-vocabulary.

**Q3 — Four validator rules have no author-time equivalent.** Run-9 finalize round 1 surfaced:
- `codebase-ig-first-item-not-zerops-yaml` (all 3 codebases)
- `codebase-ig-scaffold-filename` (api + worker, mentioning `main.ts`)
- `meta-agent-voice` (env 0 + env 5, containing "agent" / "zerops_knowledge" / "sub-agent" / "scaffolder")
- `env-readme-too-short` (all 6 env READMEs at 11 lines each; target 40)

None of these rules appear in the scaffold brief or finalize atom as author-time guidance. Sub-agents write content, fail validators, iterate. ~15 min of finalize wall time burned.

**Q4 — `execOnce` arbitrary-static-key edge case.** Feature sub-agent ([agent-adb7.jsonl](../runs/9/SESSION_LOGS/subagents/agent-adb75d19d2006e0db.jsonl) early section) queried `zerops_knowledge` five times with rephrased queries all about "can I use a static arbitrary key for seed that fires once per service lifetime, not per deploy?". The injected atom [principles/init-commands-model.md](../../../internal/recipe/content/principles/init-commands-model.md) covers the two canonical key shapes but not the "arbitrary static string like `nestjs-showcase.seed.v1`" case. ~1–2 minutes of agent cycle waste.

**Fix direction** — four small edits:

**Q1 — gate HTTP section on `role.ServesHTTP`**:
- [briefs.go](../../../internal/recipe/briefs.go) `BuildScaffoldBrief` — emit the "## HTTP" section only when `role.ServesHTTP == true`. Drop the `(ServesHTTP=true)` annotation from the header (it was a hint that the section was gated; once it's actually gated, the annotation is noise).

**Q2 — rename `# Pre-ship contract` → `# Behavioral gate`**:
- [briefs.go](../../../internal/recipe/briefs.go) or [content/briefs/scaffold/preship_contract.md](../../../internal/recipe/content/briefs/scaffold/preship_contract.md) (wherever the header lives) — rename.
- Keep `"pre-ship contract"` in the voice-rule forbidden list. With the header gone, the sub-agent's authoring vocabulary uses "behavioral gate"; the forbidden phrase is no longer a conceptual collision.

**Q3 — port validator rules to author-time**:
- [content_authoring.md](../../../internal/recipe/content/briefs/scaffold/content_authoring.md) — new "Validator tripwires" section with four rules + good/bad pairs:
  - "IG item #1 is the `zerops.yaml` code block (engine-generated; your items start at #2)."
  - "IG items 2+ describe app-side changes. Never name scaffold-only filenames like `main.ts`, `app.module.ts` — porters bring their own code and don't have those filenames."
  - "Env READMEs use porter voice. Never use the words: 'agent', 'sub-agent', 'scaffolder', 'zerops_knowledge'."
  - "Env READMEs target 45+ lines (the validator threshold is 40; leave margin)."

**Q4 — pre-answer `execOnce` arbitrary-static-key case**:
- [content/principles/init-commands-model.md](../../../internal/recipe/content/principles/init-commands-model.md) — add a third key-shape:

  ```
  Three key shapes:
  1. `${appVersionId}` — re-fires every deploy. Use for idempotent operations
     (migrations that check IF NOT EXISTS) where re-running is harmless.
  2. Canonical static strings (seed, bootstrap) — fire once per service lifetime.
     Use for one-shot non-idempotent ops (initial seed data insertion).
  3. Arbitrary static strings like `<slug>.<operation>.<version>` — same
     semantics as #2 (once per lifetime), but the <version> suffix lets you
     deliberately re-run by bumping it (`v1` → `v2`). Use when you want both
     once-per-lifetime semantics AND a documented way to re-trigger.
  ```

- [content_extension.md](../../../internal/recipe/content/briefs/feature/content_extension.md) wrapper-note affordance — add a one-line pointer: "If you need a seed that runs exactly once (not per deploy), use key shape #3 from init-commands-model.md: `<slug>.<operation>.v1` (static arbitrary string). Bump the `.v1` suffix to re-run."

**Changes**:
- [briefs.go](../../../internal/recipe/briefs.go) — HTTP section gating conditional. ~10 LoC.
- [preship_contract.md](../../../internal/recipe/content/briefs/scaffold/preship_contract.md) or wherever — rename header. 1 line.
- [content_authoring.md](../../../internal/recipe/content/briefs/scaffold/content_authoring.md) — new "Validator tripwires" section. ~400 bytes.
- [init-commands-model.md](../../../internal/recipe/content/principles/init-commands-model.md) — add third key shape. ~150 bytes.
- [content_extension.md](../../../internal/recipe/content/briefs/feature/content_extension.md) — one-line pointer to init-commands-model.md. 1 line.

**Test coverage**:
- `TestBrief_Scaffold_OmitsHTTPSectionForNonHTTPRole` — plan with `ServesHTTP: false` codebase; brief body does NOT contain `## HTTP`.
- `TestBrief_Scaffold_IncludesHTTPSectionForHTTPRole` — plan with `ServesHTTP: true` codebase; brief body DOES contain `## HTTP`.
- `TestBrief_Scaffold_HeaderIsBehavioralGate` — brief body contains `# Behavioral gate`, does NOT contain `# Pre-ship contract`.
- `TestBrief_Scaffold_ContainsValidatorTripwires` — brief body contains the four tripwire rules (grep for each rule key phrase).
- `TestPrinciples_InitCommandsCoversArbitraryStaticKey` — atom body contains "arbitrary static strings" + `<slug>.<operation>.<version>` example.

**Watch**: the validator-tripwires section adds ~400 bytes to the scaffold brief. Current cap is 12 KB (raised for run-9 tranche). Run 9's full brief was ~11.5 KB; Q adds pressure. Add a `TestBrief_Scaffold_UnderCap_WithValidatorTripwires` pin.

---

## 3. Ordering + commits

Dependencies:
- **L** is foundational — without it, M, N, O, P all write to the wrong target. Land first.
- **M** depends on **L** (`<cb.SourceRoot>/zerops.yaml` is the embed source; after L, the README that embeds it also lives at `<SourceRoot>`).
- **N, O, P** are independent of each other; all three depend on L (validators now read from `<SourceRoot>`).
- **Q** sub-items are independent; can land before or after Tier 2. Recommend alongside Tier 2.

### Commit order

Tranche 1 — shape (must land first):

1. **refactor(recipe): redirect apps-repo content from `<outputRoot>/codebases/<h>/` to `<SourceRoot>`** (L) — handlers.go, validators.go path redirects + `copyCommittedYAML` deletion + 5 test updates. Net LoC negative.
2. **feat(recipe): auto-embed `zerops.yaml` as IG item #1 during stitch** (M) — handlers.go prepend template + content_authoring.md rewrite + new test pins.

Tranche 2 — style (after tranche 1):

3. **fix(recipe): loosen yaml-comment causal-word check from per-line to per-block** (N) — validators_codebase.go + yaml-comment-style.md atom + new tests.
4. **feat(recipe): unify KB format as `**Topic** — explanation`; ban triple in KB** (O) — content_authoring.md + content_extension.md + new validator + new tests.
5. **refactor(recipe): cap CLAUDE.md length; invert `too-few-sections` to `too-long`** (P) — validators_codebase.go + brief guidance + new tests.

Tranche 3 — brief hygiene (parallelizable with tranche 2):

6. **chore(recipe): gate HTTP section on `role.ServesHTTP`** (Q1) — briefs.go + test.
7. **refactor(recipe): rename `# Pre-ship contract` header to `# Behavioral gate`** (Q2) — one-liner in briefs or preship_contract.md.
8. **docs(recipe): port finalize-time validator rules to author-time scaffold brief** (Q3) — content_authoring.md "Validator tripwires" section + tests.
9. **docs(recipe): extend init-commands-model.md with arbitrary-static-key shape** (Q4) — principles atom + feature wrapper pointer.

Final milestone commit: **docs(recipe): run-10-readiness CHANGELOG entry** — update [CHANGELOG.md](../CHANGELOG.md) with the story, keep shape fixes (L, M) separately grouped from style fixes (N, O, P) and brief-hygiene (Q) so the narrative matches the tranche structure.

Between every commit: `go test ./... -count=1 -short` green + `make lint-local` green. CLAUDE.md's "Max 350 lines per .go file" still applies — handlers.go is 688 lines (pre-existing) + handlers_fragments.go from run 9; do not grow further.

---

## 4. Acceptance criteria for run 10 green

Run 10 is "reference parity" when, against a fresh `nestjs-showcase` dogfood:

### Inherited from run 9 (continue to hold)

1. Stage deploys green on every codebase.
2. Browser verification FactRecords recorded per feature tab.
3. Seed ran once; `GET /items` returns ≥ 3 items.
4. Stitched output has canonical structure — root `README.md` + 6 tier folders, each with `README.md` + `import.yaml`. Per-codebase files live at `<SourceRoot>/{README.md, CLAUDE.md, zerops.yaml}`.
5. Factuality lint passes.
6. Fragments authored in-phase.
7. Citation map attachment on KB gotchas.
8. Cross-surface uniqueness.
9. Finalize gates all pass on the full deliverable.
10. Recipe click-deployable end-to-end.
11–16. (Tier 11–16 from run-9-readiness — scaffold yamls dev-vs-prod, no dividers, porter voice, parallel dispatch, record-fragment response echoes, feature facts in `facts.jsonl`.) All continue to pass.

### New for run 10

17. **Apps-repo shape at `<SourceRoot>/` matches reference.** After run, `/var/www/apidev/`, `/var/www/appdev/`, `/var/www/workerdev/` each contain `README.md` + `CLAUDE.md` + `zerops.yaml` at tree root alongside source. Shape-equivalent to `/Users/fxck/www/laravel-showcase-app/`.
18. **`<outputRoot>/codebases/` does not exist.** The invented subdirectory is gone from the rendered tree.
19. **README Integration Guide item #1 is a fenced yaml code block.** The code block's content is byte-identical to `<SourceRoot>/zerops.yaml`. Subsequent items #2+ are fragment-authored porter-facing app-side changes.
20. **YAML inline comments read as multi-line natural prose.** At least one comment block in each codebase's zerops.yaml spans 3+ lines with one causal word per block. No validator rejection on block-level check.
21. **README knowledge-base uses one consistent format** — every KB bullet opens with `**Topic**` followed by em-dash followed by prose. Zero `**symptom**:` triples in any KB fragment.
22. **Per-codebase CLAUDE.md is ≤ 60 lines.** No forbidden sub-sections ("Quick curls", "Smoke test", "Local curl", "Redeploy vs edit", "Boot-time connectivity").
23. **Engine brief omits HTTP section for non-HTTP roles.** Scaffold-worker's composed prompt (main-session.jsonl Agent dispatch event) does NOT contain `## HTTP`.
24. **Engine brief uses `# Behavioral gate` header, not `# Pre-ship contract`.**
25. **Author-time "Validator tripwires" section** appears in scaffold brief. Finalize round 1 surfaces <5 violations total (vs run-9's ~50).
26. **Feature sub-agent makes at most 1 `zerops_knowledge` call for `execOnce` key shape** (down from run-9's 5). The atom now answers the arbitrary-static-key case directly.

---

## 5. Non-goals for run 10

Keep out of scope, ship separately or deferred:

- **Chain resolution redesign.** [chain.go:73](../../../internal/recipe/chain.go#L73) `loadParent` still looks for `<parentDir>/codebases/<h>/`. After L ships, no v3-produced recipe has that directory either. The chain resolver remains a no-op (it was already a no-op against v2 parents). Redesign to read apps-repo-shaped parents is deferred until `nestjs-minimal` gets a v3 re-run and the real inheritance flow becomes testable. Document the no-op state in [plan.md](../plan.md) so future work doesn't re-discover it.
- **Automated click-deploy verification harness.** Criterion 10 stays manual.
- **`verify-subagent-dispatch` SHA check.** Still deferred from run-8-readiness.
- **Per-surface `validate-surface` action.** Useful authoring affordance (collapses finalize "wall of red"); not blocking for run 10.
- **`build-subagent-prompt` action.** Engine-composed wrapper notes (eliminates ~800 tokens of drift-risky hand-duplication); optimization for run 11+.
- **Auto-inject scaffold-phase facts into feature brief.** The `${broker_connectionString}` trap propagation was hand-assembled by main agent in run 9; automatable but not blocking.
- **Validator message quality improvements.** `factuality-mismatch` should name the offending substring; `env-readme-too-short` should say "add one paragraph". Mechanical; defer.
- **Source-log credential redaction.** Platform concern, not v3.
- **Warn-lint at Bash pre-call hook for local `npm install` against mount.** Harness concern; G2's content-side atom solved the original problem.

---

## 6. Risks + watches

- **L changes the write target without migrating historical runs.** Anyone inspecting `<outputRoot>/codebases/<h>/` in a run directory (humans, other tooling, run-9-analysis grep commands) will find it empty / absent. No persistence concern — each run is a fresh tree — but document the change in CHANGELOG entry loudly.
- **M's yaml-embed renders the committed file's current state at stitch time.** If the main agent hand-edits `<SourceRoot>/zerops.yaml` during finalize iteration (as happened in run 9 when `yaml-comment-missing-causal-word` fired), the re-stitch picks up the edits. This is correct behavior (single source of truth). But: the fragment-authored IG items are stored immutably; if the sub-agent's IG prose references the OLD yaml content, it desynchronizes. Mitigation: the new item-#1 template's intro sentence is generic ("The main configuration file — place at repository root..."), not yaml-specific. The specific content is only the yaml block itself, which re-renders correctly.
- **N could loosen too far on pure-label comment blocks.** If a sub-agent writes a 10-line comment block of pure labels (no causal word anywhere), the block passes. Guard: block must have at least one line longer than 40 chars to trigger the causal-word requirement in the first place; if all lines are labels, block is label-only and passes, which is the desired behavior.
- **O's triple-ban validator might surface historical content.** Any existing fragment carrying `**symptom**:` pattern (not yet encountered in published recipes, but possible in test fixtures or in-flight runs) will start failing. Search [internal/recipe/content/](../../../internal/recipe/content/) for the pattern during implementation; update if found.
- **P's forbidden-subsection list is conservative.** "Boot-time connectivity" and similar might be legitimate in a codebase where boot behavior is meaningfully unique. If that case surfaces, allow opt-out. Not run-10 scope.
- **Q3's Validator-tripwires section pushes scaffold brief toward 12 KB cap.** Pin `TestBrief_Scaffold_UnderCap_WithValidatorTripwires`. If pressure surfaces, first compress by merging duplicate content between `platform_principles.md` + `content_authoring.md` (likely ~1 KB of overlap).
- **Workstream interaction: M + N together.** M embeds `<SourceRoot>/zerops.yaml` into README IG. N changes how the yaml's comments are validated. If M ships before N, the embedded yaml in the README might contain single-line-run-on comments (current run-9 style). If N ships before M, the yaml looks reference-style but isn't embedded anywhere readable. Both shipping in the same run is required for run 10 to show reference-parity content — order L → M → N is correct, but mention the interaction in commit messages.
- **Run 10 uses `nestjs-showcase` as target AGAIN.** Run-9 state on the workspace (the `zcprecipator-nestjs-showcase` project, id `rl73JE06S1S0HFyy39dBAQ`) may persist. Either delete the workspace before run 10 (cleanest) or run 10 against a fresh slug (e.g. `nestjs-showcase-run10` or tear down first). Document the pre-run requirement.

---

## 7. Open questions

1. **M — renumber fragment-authored items automatically, or rely on the sub-agent to number from 2?** Current sub-agents number from 1. After M, engine produces item #1; the sub-agent's "1." prefixes become "2." visually-but-actually-"1." in the authored fragment text. Two options:
   - (a) sub-agent is told via brief update (Q3 Validator tripwires section) that its items start at #2.
   - (b) engine renumbers on render (post-process the fragment to find `1. `, `2. `, ... and shift by +1).
   - Lean (a) — brief update is simpler and the sub-agent already follows the brief. (b) is a band-aid. Decide at implementation.

2. **N — treat the bare `#` transition as a block boundary or in-block?** Bare `#` lines could be: (a) block-ending (so `# Comment\n#\n# Next comment` is two blocks), (b) in-block (so the same text is one block). The reference uses bare `#` INSIDE large comment blocks as paragraph separators. Lean (b) — treat bare `#` as in-block, so the containing block's causal-word check still applies to the whole multi-paragraph region. Test fixture should exercise both cases.

3. **O — migrate the existing `**symptom**:` triples to CLAUDE.md, or force sub-agents to re-author from scratch?** Run-9 api README has 8 triple entries that SHOULD live in CLAUDE.md/notes. Options:
   - (a) manual migration by a content sweep (no code change; touches content atoms only).
   - (b) engine post-process: on stitch, detect triple bullets in KB and move them to CLAUDE.md/notes.
   - Lean (a) — one-time content sweep is cleaner than a parse-and-move engine feature. Run-10 will re-author anyway (fresh `nestjs-showcase` run); existing run-9 content doesn't migrate.

4. **P — when the `claude-md-too-long` violation fires, what does the author DO?** Cut content, but which? The validator message should suggest: "CLAUDE.md is a codebase-scoped cheat sheet (30–50 lines target). Move operational runbooks (`Quick curls`, `Smoke tests`, cross-codebase guidance) to the recipe-level root `README.md`'s Operations section; keep only codebase-specific service facts + dev loop + notes here." Make the suggestion explicit in the violation message.

5. **Q2 — rename to `# Behavioral gate` or `# Dev verification`?** Both are candidates. "Behavioral gate" maps to what the 5-item check DOES (verifies behavior before declaring scaffold complete). "Dev verification" maps to WHEN it runs (against the dev deploy). Lean "Behavioral gate" — more specific about the purpose. Not critical; decide at implementation.

6. **Q3 — should the Validator-tripwires section ALSO include the N/O/P new rules** (block-level causal-word, KB `**Topic**` format, CLAUDE.md length cap)? Those are new validators; sub-agents don't know about them. Yes — add all six rules to the tripwires section (four old + three new). Keep each entry to one line + one good/bad pair.

7. **Chain resolution: declare it a no-op in `plan.md` explicitly, or leave silent?** After L ships, no v3-produced recipe has `<outputRoot>/codebases/<h>/`, so `chain.go::loadParent` is now ALSO a no-op against v3 parents (it was already a no-op against v2). Either document ("chain resolution deferred until parent shape is redesigned") or leave. Lean: document — otherwise a future run will try to chain-resolve against what it thinks is a v3 parent and get silent empty. Add a one-paragraph note to [plan.md](../plan.md) §7 risks or §3 chain-resolution sections.
