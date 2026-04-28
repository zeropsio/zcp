# Handoff prompt for fresh instance — run-17 Tranche 0.5 distillation work

Copy the section below verbatim into a fresh Opus 4.7 (1M context)
session running in `/Users/fxck/www/zcp`. It is self-contained — the
fresh instance has no prior conversation context and only the
referenced files to work from.

---

## Prompt

You are taking over the load-bearing distillation work for run-17 of
the zcprecipator3 recipe-authoring engine. The implementation guide,
prep doc, and partial drafts have been authored by a prior instance
that triple-verified them against the codebase. Your job: complete
the reference distillation atoms by combining the drafted content
with verbatim reference extracts you cross-check yourself.

**Read first, in this order**:

1. [docs/zcprecipator3/plans/run-17-implementation.md](docs/zcprecipator3/plans/run-17-implementation.md) — full implementation contract. Pay special attention to §0 (confidence model), §5 (Tranche 0.5 — your work), §6 (Tranche 1, which depends on your atoms), §9 (Tranche 4, which depends on your atoms).
2. [docs/zcprecipator3/plans/run-17-prep.md](docs/zcprecipator3/plans/run-17-prep.md) — the predecessor prep doc with §1-§5 corrections from run-17-implementation.md still pending application; treat the implementation guide as authoritative when they conflict.
3. [docs/spec-content-surfaces.md](docs/spec-content-surfaces.md) — the seven content surfaces. Surfaces 4 (IG), 5 (KB), 7 (zerops.yaml comments) are your atoms' targets.
4. [docs/spec-content-quality-rubric.md](docs/spec-content-quality-rubric.md) — already authored. The 5 criteria your atoms must teach toward.
5. [internal/recipe/content/briefs/refinement/refinement_thresholds.md](internal/recipe/content/briefs/refinement/refinement_thresholds.md) — already authored. The 100%-sure ACT/HOLD encoding your atoms support.

**Drafts directory**: [docs/zcprecipator3/plans/run-17-drafts/](docs/zcprecipator3/plans/run-17-drafts/)

Three drafts to integrate:

- `kb_shapes_fail_section.md` — drop-in FAIL section for `reference_kb_shapes.md`. Verbatim run-16 quotes already cross-checked. You add: the PASS examples from references + the surrounding atom shape.
- `ig_one_mechanism_fail_section.md` — drop-in FAIL section for `reference_ig_one_mechanism.md`. Same pattern.
- `decision_recording_worked_examples.md` — full worked-examples sections (5 + 2) to APPEND to existing scaffold + feature decision_recording atoms.

**Reference recipe roots** (read-only; verbatim source for your distillation):

- [/Users/fxck/www/laravel-jetstream-app/](/Users/fxck/www/laravel-jetstream-app/) — apps repo (CLAUDE.md, README.md, zerops.yaml)
- [/Users/fxck/www/laravel-showcase-app/](/Users/fxck/www/laravel-showcase-app/) — apps repo (CLAUDE.md, README.md, zerops.yaml)
- [/Users/fxck/www/recipes/laravel-jetstream/](/Users/fxck/www/recipes/laravel-jetstream/) — recipe tree (README.md + 6 tier import.yaml + tier README extracts)
- [/Users/fxck/www/recipes/laravel-showcase/](/Users/fxck/www/recipes/laravel-showcase/) — recipe tree, same shape
- [docs/zcprecipator3/runs/16/](docs/zcprecipator3/runs/16/) — the prior dogfood run (apidev/, appdev/, workerdev/ READMEs + environments/) — used for FAIL examples; my drafts already cite verbatim.

---

## Your deliverables

### Group A — three atoms written from scratch

Land each at `internal/recipe/content/briefs/refinement/`:

1. **`reference_voice_patterns.md`** (~150 lines)

   Source authority: [docs/spec-content-surfaces.md §305-330](docs/spec-content-surfaces.md) plus the verbatim references below. Distill into the standard atom shape (Why this matters / Pass examples / Fail examples / The heuristic / When to HOLD).

   Verified-real reference quotes you must cross-check verbatim and use as PASS examples (run `grep -n` on the named files; if any quote has drifted, update to actual file content):

   - `/Users/fxck/www/laravel-jetstream-app/zerops.yaml:28` — `# Feel free to change this value to your own custom domain,`
   - `/Users/fxck/www/laravel-jetstream-app/zerops.yaml:61` — `# Configure this to use real SMTP sinks`
   - `/Users/fxck/www/recipes/laravel-jetstream/3 — Stage/import.yaml:49` — `# Feel free to remove this service, if you wish to stage-test`
   - `/Users/fxck/www/recipes/laravel-jetstream/4 — Small Production/import.yaml` — search for `# Disabling the subdomain access is recommended,`

   FAIL examples: pull from `docs/zcprecipator3/runs/16/environments/4 — Small Production/import.yaml` (run-16 tier-4 import.yaml) where field-restatement preambles are common. The CONTENT_COMPARISON.md §3 names the specific lines.

   Heuristic encoding: list the seven phrasings the rubric counts at Criterion 2, plus the "named signal" requirement.

2. **`reference_yaml_comments.md`** (~150 lines)

   Side-by-side comparison: jetstream tier-4 service blocks (mechanism-first, tight) vs showcase tier-4 service blocks (field-restatement preamble, looser). Both are reference-acceptable; the atom teaches when each shape lands at 8.5 anchor and what would push them to 9.0.

   Source files (cross-check verbatim before quoting):
   - `/Users/fxck/www/recipes/laravel-jetstream/4 — Small Production/import.yaml`
   - `/Users/fxck/www/recipes/laravel-showcase/4 — Small Production/import.yaml`

   FAIL example: run-16 tier-4 `# api in zeropsSetup: prod, 0.5 GB shared CPU, minContainers: 2` (already cited verbatim in run-17-prep.md §3.3 R-17-C11; cross-check current run-16 artifact).

3. **`reference_trade_offs.md`** (~120 lines)

   Two-sided trade-off examples from KB bullets across both reference apps repos. Search for bullets that name a rejected alternative; verified-existing example: `/Users/fxck/www/laravel-showcase-app/README.md` KB section names `predis over phpredis` (chosen + rejected). Find 3-5 such bullets across the two apps repos.

   FAIL example: pull from run-16 KB bullets where only the chosen path is named (most run-16 KB bullets — the dominant miss).

   The 9.0 anchor shape (chosen + rejected + when-to-revisit trigger condition) is rubric-defined; if no reference bullet hits 9.0 verbatim, hand-craft a 9.0 anchor and label it "(hand-crafted; reference recipes don't hit 9.0 on this criterion)".

### Group B — two atoms combining drafts with reference PASS examples

Land each at `internal/recipe/content/briefs/refinement/`:

4. **`reference_kb_shapes.md`** (~200 lines)

   Use [`docs/zcprecipator3/plans/run-17-drafts/kb_shapes_fail_section.md`](docs/zcprecipator3/plans/run-17-drafts/kb_shapes_fail_section.md) as the FAIL examples section verbatim (it's already cross-checked).

   Add a PASS examples section drawing from:
   - `/Users/fxck/www/laravel-showcase-app/README.md` KB section — `**No .env file**` (8.5 symptom-first anchor) and `**Cache commands in initCommands, not buildCommands**` (8.5 directive-tightly-mapped anchor) — both already named in `kb_shapes_fail_section.md` Fail 3 framing.

   Add a 9.0 anchor — adapt the Refined-to suggestions from the FAIL section (e.g. `**ALTER TABLE deadlock under multi-container boot**`) and label as the 9.0 reshape target.

   Wrap with the standard atom shape (Why / Pass / Fail / Heuristic / Hold). The Heuristic section encodes the rubric Criterion 1 signal table.

5. **`reference_ig_one_mechanism.md`** (~200 lines)

   Use [`docs/zcprecipator3/plans/run-17-drafts/ig_one_mechanism_fail_section.md`](docs/zcprecipator3/plans/run-17-drafts/ig_one_mechanism_fail_section.md) as the FAIL examples section verbatim.

   Add a PASS examples section: laravel-showcase apps repo IG sequential H3s. Verify by reading `/Users/fxck/www/laravel-showcase-app/README.md` IG section — there should be ~5 H3s each one platform-forced change. Quote 3 in sequence verbatim.

### Group C — appendable section in two atoms

6. **Append worked examples** to existing atoms:

   - `internal/recipe/content/briefs/scaffold/decision_recording.md` — append the "Worked examples — what good fact-recording looks like" section from [`docs/zcprecipator3/plans/run-17-drafts/decision_recording_worked_examples.md`](docs/zcprecipator3/plans/run-17-drafts/decision_recording_worked_examples.md). Worked examples 1-5 + the "What good/bad Why looks like" sections.

   - `internal/recipe/content/briefs/feature/decision_recording.md` — append the "Worked examples — feature-phase porter_change shapes" section. Examples F1 + F2 + the cross-reference paragraph.

   Before appending, verify the verbatim Why prose in worked examples 1, 2, 3 against the current `internal/recipe/engine_emitted_facts.go` lines 41-105. If the source has drifted (someone landed an unrelated edit), update the worked examples to match the current source. If they haven't drifted, append verbatim.

---

## Verbatim cross-check protocol

Every `> *"..."*` quote in every atom must match the named source byte-for-byte. The discipline:

1. For each quote, run `grep -n` on the named file with a substring of the quote.
2. Confirm the match exists. If it doesn't, either fix the quote or remove the example.
3. Note any quotes that you couldn't verify in a "Verification log" appendix at the bottom of each atom (delete the appendix before final commit; it's a working note).

This is the load-bearing verification. Skip it and the rubric grading downstream becomes unreliable.

---

## Self-grading gate (Tranche 0.5 closure)

Before committing, read every atom you authored back-to-back in one sitting. Grade the set:

- Does each atom teach a single criterion clearly? (Pass: yes; Fail: scope-creeps across criteria.)
- Are the pass examples convincingly above the fail examples? (Pass: yes, the contrast is visible at a glance.)
- Are the heuristics actionable — could a sub-agent encode them as deterministic rules? (Pass: yes; Fail: heuristics require LLM judgment we haven't given them.)
- Do the hold-cases meaningfully constrain the heuristic — would the sub-agent know to hold on a borderline case? (Pass: yes; Fail: hold-cases are token gestures.)

The implementation guide §5.4 names this self-grade as the Tranche 0.5 gate. Below 8.5 average, second pass before commit.

---

## Triple-verification report (mandatory at handoff back)

When you're done, produce a §12-style report (the prep doc has the format) with these sections:

1. **Atom inventory** — every file you created or modified, line count, location.
2. **Verbatim quote check** — for each `> *"..."*` quote, the source file:line you verified against. Group by atom.
3. **Self-grade per atom** — your honest score on the four self-grading questions above.
4. **Anomalies found** — any quotes that didn't verify, any references you couldn't find, any drift between the implementation guide's claims and the actual codebase you noticed during this work.
5. **Confidence on Tranche 0.5 closure** — HIGH / MEDIUM / LOW with reasoning.

The triple-verification standard is the same as the prep doc's §12 protocol — verify, don't assume. The prior instance held its own work to this bar (after being prompted; it wasn't automatic). Hold yourself to it.

---

## Constraints

- You write only the atoms named above. Do not edit the implementation guide, the prep doc, the rubric, or refinement_thresholds.md. Those are upstream contracts.
- You do not implement Tranche 1 engine code, Tranche 4 composer, etc. Your scope ends at the embedded atom corpus.
- If a reference quote you need has been moved or modified in the source files since the prep doc was written, update the atom to match current source AND flag the drift in the triple-verification report.
- If you discover that the rubric or refinement_thresholds.md has a shape that prevents your atoms from being clear, raise that in your report rather than working around it. The upstream contracts are correctable.

---

## Estimated effort

- Atoms 1-3 (from scratch): 4-5 hours each, careful work
- Atoms 4-5 (combine drafts + add PASS): 2-3 hours each
- Append worked examples (Group C): 1 hour
- Verbatim cross-check: 1-2 hours across all atoms
- Self-grade + triple-verification report: 1-2 hours

Total: 15-20 hours of careful authored work. Quality of distillation determines run-17 quality. Take the time.
