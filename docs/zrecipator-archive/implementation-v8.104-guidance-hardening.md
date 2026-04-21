# v8.104 — Guidance-Layer Hardening

**Intended reader**: a fresh Opus 4.7 instance implementing this from scratch. Self-contained; no prior conversation required.

**Prerequisite reading (in order)**:

1. [docs/recipe-version-log.md](recipe-version-log.md) §v33 entry — the run that motivates this plan.
2. [docs/implementation-v8.96-author-side-convergence.md](implementation-v8.96-author-side-convergence.md) §§1–3 — the verification-inversion thesis. v8.104 applies the same pattern to authoritative-string guidance.
3. [docs/implementation-v8.97-masterclass.md](implementation-v8.97-masterclass.md) §Fix 3 — the MANDATORY sentinel mechanism. v8.104 extends, does not replace.
4. [internal/content/workflows/recipe.md](../internal/content/workflows/recipe.md) — the file all fixes edit. 3370 lines; every fix below names the exact line range it touches.
5. [internal/workflow/recipe_templates.go](../internal/workflow/recipe_templates.go) — `BuildFinalizeOutput` + `EnvFolder` (authoritative output paths).
6. [CLAUDE.md](../CLAUDE.md) — project conventions (max 350 lines per .go file; RED before GREEN; fix at source; no fallbacks).

**Scope**: one release, six fixes, all in the guidance layer (recipe.md edits + one template.go constant reference). Zero new checks. Zero new tool surfaces. The thesis: **every v33 defect traces to a guidance gap the agent filled with imagination.**

---

## 1. Diagnosis — what v33 revealed about guidance

v33 ran on v8.102.0 (the v8.96 + v8.97 + v8.98 + v8.99 + v8.100 compound) and produced operationally clean numbers (88 min wall, 30s main bash, 0 very-long, 0 MCP schema errors, 18/18 genuine gotchas, env 4+5 yaml at v7-gold standard). Yet six distinct defects shipped — and each one is the agent filling a gap the guidance left open.

### 1.1 Phantom `recipe-nestjs-showcase/` output tree

The writer subagent wrote seven orphan files to `/var/www/recipe-nestjs-showcase/` with paraphrased env names (`0 — Development with agent`, `1 — Development`, `2 — Review`, `3 — Stage`, `4 — Small production`, `5 — HA production`). The canonical tree at `/var/www/zcprecipator/nestjs-showcase/` shipped correct env names (`0 — AI Agent`, `1 — Remote (CDE)`, `2 — Local`, `3 — Stage`, `4 — Small Production`, `5 — Highly-available Production`) from `BuildFinalizeOutput` Go templates.

The main-agent's writer-dispatch prompt (constructed by main from the recipe.md template) contained these literal strings at offset 2161 and 4694:

> - **Output root for env READMEs + root README**: `/var/www/recipe-nestjs-showcase/` (create if missing; the finalize step consumes files from there).
> - `0 — Development with agent` / `1 — Development` / `2 — Review (or similar — check workflow manifest / finalize naming)` / `3 — Stage` / `4 — Small production` / `5 — HA production`

Nothing in recipe.md ever said those strings. The main agent synthesized them from imagination when constructing the writer dispatch prompt. v8.103 added the `{projectRoot}/zcprecipator/{slug}/` rule to the close section at recipe.md:2978, but the writer-brief section at 2358 does NOT pin output paths or env names to authoritative values — so main-agent paraphrase remains the weak link.

**Guidance gap**: the writer-subagent-brief block tells main-agent HOW to construct the writer dispatch but does not bind the output-path strings to a single authoritative source. Main treats them as free parameters; they aren't.

### 1.2 `zsc execOnce ${appVersionId} -- seed` in every recipe since v20

`${appVersionId}` changes every deploy. So `zsc execOnce ${appVersionId} -- seed` means "seed runs once per deploy across replicas" — NOT "once ever." A 10-deploy day runs seed 10 times. The only reason DB doesn't fill is the in-script `if (count > 0) return` short-circuit. That short-circuit is the literal cause of v33 apidev gotcha #7 — `GET /api/search returns 500 Index 'items' not found on the second deploy` — because the Meilisearch `addDocuments(...)` call lives INSIDE the skipped branch.

We document the symptom as a user-facing gotcha while shipping the buggy pattern in our own scaffolds.

recipe.md:1549 names the `appVersionId` burn-on-failure trap in prose. recipe.md:2288 hardcodes the wrong pattern in the CLAUDE.md template the writer copies: *"On deploy, these run via `initCommands` wrapped with `zsc execOnce ${appVersionId}`."* The guidance both warns about the trap AND teaches the shape that causes it.

**Guidance gap**: no positive example of static-key `execOnce` for once-per-workspace operations (seed, bootstrap, one-shot provisioners). The only example shown is per-deploy.

### 1.3 Unicode box-drawing separators in zerops.yaml comments

v33 shipped zerops.yaml comments decorated with:

```yaml
zerops:
  # ──────────────────────────────────────────────────────────────
  # Development setup — apidev service. The agent iterates here
  # over SSH (nest start --watch).
```

v20–v32 used plain `# Section title` (v25 used `# ----- Title -----` with dashes once). recipe.md:2914 covers comment *voice* (developer-to-developer, WHY-not-WHAT, concrete-not-abstract) but is silent on visual decoration. The agent filled the gap with Unicode box-drawing. Harmless individually; cumulative noise in published content.

**Guidance gap**: comment-style guidance covers voice but not visual decoration.

### 1.4 Feature-subagent 9-minute diagnostic panic burst

At 07:29:00 the feature subagent began sensible `ssh apidev "cat node_modules/meilisearch/package.json"` probes (all successful). At 07:29:14 it pivoted to firing ~80 parallel bash-tool calls (`ls`, `stat`, `echo HELLOWORLD`, `printf END`, `ssh -vvv apidev`, `find / -name config`) — probing whether basic shell + SSH even worked despite having just done both successfully. Latencies on trivial commands hit 300+ seconds (harness queue saturated). Productive feature work started at 07:38:16. ~9 minutes lost to panic-diagnostics.

No host-confusion occurred. No tool actually broke. The agent convinced itself something was wrong and then ran out the clock proving it wasn't.

recipe.md contains no guidance on diagnostic-probe cadence. Subagent briefs teach what to write but not how to behave when an ambiguous signal appears.

**Guidance gap**: no anti-pattern for diagnostic-storm. Agents need a "if something looks wrong, fire ≤3 targeted probes then escalate" rule, not a silence the agent reads as permission to fire 80.

### 1.5 Content-check coupling invisible until failure round 2

Deploy-complete: 3 rounds (fail → fail → pass). Round 1: 6 distinct failures across README fragments + YAML comment-ratio. Round 2: reworded gotchas triggered a NEW `cross_readme_gotcha_uniqueness` Jaccard collision that didn't exist at round 1 — because rewording changed the similarity graph.

Theme A surfaced `readSurface` and `howToFix` per-check, which did help. What it didn't surface: *"fixing this check can cause these OTHER checks to newly fail."* The check-coupling graph lives in the code and in the implementer's head; it reaches the author only through failure rounds.

**Guidance gap**: check briefs describe what's wrong on this surface but not what the fix perturbs on sibling surfaces.

### 1.6 Main-agent post-scaffold git commit assumed `.git/` exists

At 07:19:31 main agent ran `ssh appdev "git add -A && git commit -q -m 'initial scaffold'"` and got `fatal: not a git repository`. Cause: scaffold subagents had deleted `/var/www/.git/` per the v8.96 Fix #4 rule (framework scaffolders auto-create `.git/`; scaffold must delete before returning). Main recovered 6s later with `git init -q -b main && git add -A && git commit`.

recipe.md:321 (block `git-config-mount`) tells main agent to do container-side git config + init + first commit BEFORE scaffolds. But the generate step's scaffold-return commit path (where main commits scaffold work) is NOT explicitly gated on "check `.git/` exists; if not, init first." 6 seconds of cost per run. Minor but the sequencing omission is real.

**Guidance gap**: post-scaffold commit flow assumes `.git/` persists across the scaffold-subagent boundary. It doesn't, by design (Fix #4), but main-agent guidance doesn't acknowledge the handoff.

---

## 2. Root cause — three guidance-layer failure modes

### 2.1 Paraphrase-friendly rules versus copy-friendly rules

recipe.md has two classes of guidance:

- **Prose rules** — expressed as English sentences the agent is expected to read and apply ("keep comments at >30% of total lines"). These get compressed under context pressure. v21 lost the mount-as-write-surface preamble this way; v22 lost the NATS URL-embedded-creds forbid; v32 lost Read-before-Edit across three scaffold subagents.
- **Sentinel-wrapped blocks** — `<<<MANDATORY — TRANSMIT VERBATIM IN AGENT DISPATCH PROMPT>>> … <<<END MANDATORY>>>`. These survive compression because main-agent dispatch-construction rules require byte-identical transmission (recipe.md:845).

The v8.97 sentinel mechanism works. What v33 surfaces: **not every authoritative string is wrapped.** Output paths, env folder names, static-key `execOnce` shape, comment-decoration forbidden-list — all currently prose. All currently lose in main-agent synthesis.

### 2.2 Guidance names traps without prescribing positive shapes

recipe.md:1549 explains the `${appVersionId}` burn-on-failure trap. recipe.md:1553 instructs post-deploy data verification. Neither names the static-key shape that makes seed idempotent-by-construction rather than idempotent-by-short-circuit. The trap description reinforces `${appVersionId}` as the assumed shape; the fix (static key) is absent from the positive examples.

The pattern repeats: comment-style section names voice traps (narrating-what, hedging, passive-voice) but not decoration traps (Unicode separators, emoji, ASCII art). Subagent briefs warn against "diagnostic-storm when something looks wrong" — no, they don't, because that warning doesn't exist yet.

**Rule**: every named trap needs a paired named positive shape. Agents filling gaps between "don't do X" and "I don't know what Y looks like" invent Z.

### 2.3 Cross-subagent handoffs are implicit

`.git/` state across scaffold-subagent → main-agent boundary. Content-check coupling across fix rounds. Writer-subagent output path vs `BuildFinalizeOutput` canonical path. All implicit contracts. Each one broke in v33 — different subagents, same pattern.

v8.96 Theme B shipped `Scope=downstream` for one specific handoff (framework quirks → next subagent). It worked (3 facts recorded with `scope=downstream` in v33). The pattern is right; v8.104 extends it.

**Rule**: every state the receiver's first action depends on should either be carried in the brief or explicitly invalidated in it.

---

## 3. Principle — guidance that cannot be synthesized from imagination

Three design rules for v8.104 and every subsequent guidance edit:

1. **Pin authoritative strings with single-source citation.** Output paths, env folder names, canonical patterns — these are computed by Go code in `recipe_templates.go`. Guidance that references them should NAME the Go constant, not paraphrase its value. Agent paraphrasing is how the phantom tree shipped.

2. **Every trap names its positive shape.** `${appVersionId}` burn-on-failure trap must be adjacent to the static-key shape. Diagnostic-storm anti-pattern must be adjacent to the 3-probe-then-escalate rule. Comment-voice anti-patterns must be adjacent to the decoration forbid-list.

3. **Subagent handoff contracts are explicit.** If action X depends on state Y produced by prior actor Z, name all three. No implicit sequencing.

---

## 4. Non-goals

- No new checks. v8.104 is entirely guidance-layer.
- No new tool surfaces.
- No MANDATORY-sentinel proliferation (sentinels are load-bearing; overuse dilutes them).
- No attempt to prevent agent creativity in general — only pin the strings agents were observed synthesizing incorrectly.
- No revisit of v8.96 Theme B (`Scope=downstream`) — that pattern works and is extended, not changed.

---

## 5. Fix A — pin writer-dispatch output paths to `BuildFinalizeOutput`

**Problem**: recipe.md:2358 (content-authoring-brief block) describes what the writer writes but lets main agent synthesize the output root and env folder names. Main agent confabulated `/var/www/recipe-nestjs-showcase/` + paraphrased env folder names in v33. Seven orphan files shipped.

**Source of truth**: `internal/workflow/recipe_templates.go:31` `EnvFolder(envIndex int) string` returns canonical env folder names. `BuildFinalizeOutput` writes the canonical tree to `{projectRoot}/zcprecipator/{slug}/` (the `{projectRoot}` resolves from `os.Getenv("ZCP_PROJECT_ROOT")` or `/var/www` fallback).

**Fix**:

1. Add a new MANDATORY-sentinel block at the top of the content-authoring-brief section (recipe.md:2358–2441), naming the two axes the writer must respect byte-literally:

   ```
   <<<MANDATORY — TRANSMIT VERBATIM IN AGENT DISPATCH PROMPT>>>

   **Canonical output tree** — the ONLY place you write files is:
   - Per-codebase: `/var/www/{hostname}dev/README.md` and `/var/www/{hostname}dev/CLAUDE.md` (one pair per dev mount)
   - Manifest: `/var/www/ZCP_CONTENT_MANIFEST.json`

   You do NOT write root README, env READMEs, or env import.yaml files. Those come from `BuildFinalizeOutput` at the finalize step from Go templates — writing them would create a parallel orphan tree that the publish CLI ignores and reviewers confuse with the real deliverable.

   If your brief asks for an "output root" or "env folder names," STOP and re-read this block. Those parameters do not exist in your scope.

   <<<END MANDATORY>>>
   ```

2. Delete any lines in the writer-dispatch brief skeleton that reference "output root", "env folder names", or "environments/" paths — main-agent synthesis had nothing to paraphrase from because we were telling it there were parameters here when there aren't.

3. Grep recipe.md for `/var/www/recipe-` / `recipe-{slug}` / `{slug}-output/` — any residual ghost references from v8.103's revert. Delete.

**Test (added to recipe_mandatory_blocks_test.go)**:

```go
func TestWriterBrief_ForbidsOutputRootParameter(t *testing.T) {
    // Read recipe.md
    // Extract the content-authoring-brief block
    // Assert: does NOT contain "output root", "output-root", "env folder", "environments/{"
    // Assert: DOES contain "You do NOT write root README, env READMEs"
}
```

**Failure-mode proof**: re-run v33 with Fix A shipped; main-agent brief construction cannot produce paraphrased env names because the brief it's constructing from names them as forbidden.

---

## 6. Fix B — prescribe static-key `execOnce` for once-per-workspace operations

**Problem**: Every recipe v20–v33 ships `zsc execOnce ${appVersionId} -- seed`. Seed runs every deploy (masked by in-script short-circuit). The short-circuit caused v33 apidev gotcha #7 (Meili `addDocuments` inside skipped branch).

**Source of truth**: `zsc execOnce <key>` is idempotent on any string key. `${appVersionId}` is a convenient default for per-deploy operations; any stable identifier works for once-per-workspace operations.

**Fix**:

1. Add a new subsection to recipe.md §initCommands / zerops-yaml-rules (sibling to the existing `${appVersionId}` burn-on-failure trap at L1549) titled **"Two execOnce keys, two lifetimes"**:

   ```markdown
   #### Two `execOnce` keys, two lifetimes

   `zsc execOnce <key>` gates a command on the literal key value. Two shapes are correct for different jobs:

   - **Per-deploy key** (`${appVersionId}`) — runs once per deploy across replicas. Correct for idempotent-by-design operations that should reconverge state on every deploy: **migrate** (`CREATE TABLE IF NOT EXISTS`, additive column adds, data backfill).

   - **Static key** (any stable string, e.g. `bootstrap-seed-v1`) — runs once per service lifetime, across all deploys. Correct for operations that are NOT idempotent by design and should NOT re-run: **seed** (inserting initial rows), **one-shot provisioners** (create search-engine index, upload initial S3 objects), **bootstrap** (create a default tenant).

   ```yaml
   initCommands:
     - zsc execOnce ${appVersionId} --retryUntilSuccessful -- npx ts-node src/migrate.ts
     - zsc execOnce bootstrap-seed-v1 --retryUntilSuccessful -- npx ts-node src/seed.ts
   ```

   Versioned suffix (`-v1`, `-v2`) lets you force a re-run when the seed data itself changes: bump the key, the next deploy re-runs once under the new key, never again under it.

   Anti-pattern — `${appVersionId}` on seed: seed runs every deploy, and the in-script `if (count > 0) return` guard you'll reach for creates a worse bug — if ANY idempotency-protecting code lives inside the guarded branch (search-index creation, cache warmup), a state mismatch between DB and that sibling system leaves a silent hole. This is the literal cause of `GET /api/search returns 500 Index 'items' not found on the second deploy` (v33 apidev gotcha #7). The fix is the key shape, not a better guard.
   ```

2. Update the CLAUDE.md template inside recipe.md:2288 that currently says *"On deploy, these run via `initCommands` wrapped with `zsc execOnce ${appVersionId}`"* — show the two-key shape explicitly:

   ```markdown
   ## Migrations & Seed

   Run manually over SSH (see Dev Loop): `<migrate command>` then `<seed command>`.
   On deploy, these run via `initCommands`:
   - `migrate` is keyed by `${appVersionId}` — runs once per deploy, reconverges schema.
   - `seed` is keyed by a static string (e.g. `bootstrap-seed-v1`) — runs once per service lifetime. Bump the key suffix when seed data changes.
   ```

3. Update the scaffold-subagent brief (recipe.md:947, inside the MANDATORY block for API-codebase scaffolds) to name the key shape:

   > Seed script obeying the loud-failure rule — exit non-zero on any unexpected error. **Do NOT short-circuit on row count**; the correct idempotency mechanism is the `initCommands` key shape (static key, not `${appVersionId}`), documented under "Two execOnce keys, two lifetimes."

**Test**:

```go
func TestZeropsYamlRules_TwoExecOnceKeyShapes(t *testing.T) {
    // Read recipe.md
    // Assert: contains "Two `execOnce` keys, two lifetimes"
    // Assert: contains "bootstrap-seed-v1" as example static key
    // Assert: scaffold-subagent brief forbids "short-circuit on row count" for seed
}

func TestRecipeTemplates_SeedUsesStaticKey(t *testing.T) {
    // If we later add a helper that emits sample zerops.yaml in docs,
    // assert seed lines use static keys, not ${appVersionId}.
}
```

---

## 7. Fix C — comment-decoration forbid-list + single positive shape

**Problem**: recipe.md:2914 covers comment voice; nothing covers visual decoration. v33 invented Unicode box-drawing separators.

**Fix**: extend the "Comment writing style" section (recipe.md:2914) with a **Visual style** subsection:

```markdown
#### Visual style

Plain `# Comment text` per line. No decorators, no dividers, no section banners.

Forbidden:
- Unicode box-drawing (`──`, `═══`, `┌─`, `└─`, any `\u25XX` / `\u2500–\u257F` range)
- ASCII dividers (`# ----`, `# ====`, `# ****`, `# ####` as standalone decoration lines)
- Emoji
- ASCII art

Section transitions are conveyed by a blank-comment line (`#`) followed by the first comment of the new section. That's sufficient for readers; anything more is visual noise that inflates the published deliverable.

Rationale: comments ship to the user as-is in the recipe page. Decoration renders inconsistently across zerops.io's markdown + code-block rendering, and no downstream consumer (ingestor, publish pipeline, documentation tool) benefits from it. The only justification for adding decoration is "it looks nice to the author" — which is the definition of noise.
```

Pair this with the existing voice-style section, not replace it.

**Test**:

```go
func TestCommentStyle_ForbidsBoxDrawing(t *testing.T) {
    // Read recipe.md
    // Assert: "Visual style" subsection exists
    // Assert: explicitly names "Unicode box-drawing", "ASCII dividers" as forbidden
    // Assert: names "plain # Comment text per line" as the only allowed shape
}
```

---

## 8. Fix D — feature-subagent diagnostic-probe cadence rule

**Problem**: v33 feature subagent fired ~80 parallel bash probes over 90 seconds when the first 3 already succeeded. ~9 min lost.

**Fix**: add a new block to the feature-subagent-brief (recipe.md:1685–1712, inside the existing MANDATORY sentinel) titled **Diagnostic-probe cadence**:

```
**Diagnostic-probe cadence** — when a signal is ambiguous (command appears to hang, output looks wrong, tool returns unexpected value):

1. Fire at most THREE targeted probes that each test ONE hypothesis. Each probe must have a predicted outcome before you run it. If the outcome matches, you've confirmed the hypothesis. If it doesn't, that's one data point, not a reason to fire ten more.

2. Do NOT fire parallel-identical probes ("try `ls`, `ls -la`, `ls /var/www`, `stat /var/www`, `echo hello`") — they test the same hypothesis. They look productive; they generate cost without information.

3. If three targeted probes don't resolve the ambiguity, STOP probing and report back to the main agent with what you tried. The main agent has broader context (other subagent state, workflow history) and will either dispatch a specific recovery or declare the state blocking.

This rule exists because v33's feature subagent fired ~80 parallel diagnostic probes (`ls`, `stat`, `echo`, `ssh -vvv`) when the first three succeeded. Nine minutes of session wall time lost to probing a system that was working. The agent was pattern-matching on "something is weird, I should gather more evidence" without asking "what hypothesis would this new probe distinguish?"
```

**Test**:

```go
func TestFeatureSubagentBrief_DiagnosticProbeCadence(t *testing.T) {
    // Read recipe.md
    // Assert: feature-subagent-brief MANDATORY block contains "Diagnostic-probe cadence"
    // Assert: mentions "at most THREE targeted probes"
    // Assert: explicitly forbids "parallel-identical probes"
}
```

**Calibration**: v34 feature subagent should produce ≤10 diagnostic Bash calls in any 30s window. If a window has >10, the rule is being violated and the v8.105 fix is stricter gate wording.

---

## 9. Fix E — content-check coupling disclosure in check failure payload

**Problem**: v8.96 Theme A emits `readSurface`, `howToFix`, `coupledWith`, `nextRoundPrediction` on failing checks. `coupledWith` names SIBLING FILES that move together (e.g. apidev/zerops.yaml ↔ apidev/README.md's embedded YAML block). What it doesn't name: SIBLING CHECKS whose pass state depends on this check's fix.

Example from v33:
- Round 1: `app_gotcha_distinct_from_guide` fails for 3 gotchas restating IG items.
- Fix: reword those 3 gotchas.
- Round 2: `cross_readme_gotcha_uniqueness` now fails because the rewording created a Jaccard-similar stem to apidev's gotcha.
- Round 2 was not predictable from round 1's failure payload alone.

**Fix**: extend `StepCheck` with an optional `PerturbsChecks []string` field listing sibling checks whose pass state is likely to change when this check is fixed. Populate it at the check-emission sites where the coupling is known in the check code.

Concrete sites:
- `gotcha_distinct_from_guide` perturbs `cross_readme_gotcha_uniqueness` (rewording gotchas changes cross-README similarity).
- `comment_ratio` (embedded YAML) perturbs `comment_ratio` (on-disk yaml) if author mistakes which file to edit.
- `factual_claims` (env yaml) perturbs `cross_env_refs` (env yaml) if author edits comments across multiple files.

**Implementation**: no new machinery — extend the existing `StepCheck` struct, populate at emission, surface in failure-payload formatter.

```go
// internal/workflow/bootstrap_checks.go
type StepCheck struct {
    Name               string
    Status             string
    // ... existing fields ...
    ReadSurface        string   // v8.96
    Required           string   // v8.96
    Actual             string   // v8.96
    CoupledWith        []string // v8.96
    HowToFix           string   // v8.96
    NextRoundPrediction string  // v8.96
    PerturbsChecks     []string // v8.104 — sibling checks whose pass state fixing this check is likely to flip
}
```

Payload text:

```
Check gotcha_distinct_from_guide failed for appdev/README.md.
  ReadSurface: appdev/README.md — #knowledge-base gotcha stems vs #integration-guide H3 headings
  Required: zero Jaccard-similar (>0.4) pairs between the two surfaces
  Actual: 3 pairs above threshold (stems line 193, 194, 196 vs headings line 116, 137, 164)
  HowToFix: reword the 3 gotcha stems to name symptom-first (HTTP code, error string, observable misbehavior) rather than restating the integration-guide action
  PerturbsChecks (fixing this may flip): cross_readme_gotcha_uniqueness (rewording changes similarity to sibling codebases' gotchas)
```

**Test**: assert that known-coupled check pairs emit `PerturbsChecks` entries; assert that the human-readable failure-text formatter includes the "PerturbsChecks" line.

---

## 10. Fix F — explicit post-scaffold git-state invalidation

**Problem**: scaffold subagent deletes `/var/www/.git/` per Fix #4 (v8.96). Main agent's post-scaffold commit flow assumes `.git/` exists. Main's first `git add -A && git commit` fails with `fatal: not a git repository`, main recovers with `git init && git add && commit`. 6s cost per run.

**Fix**: recipe.md block `git-config-mount` (L321) currently instructs main agent to do container-side git config + init + first commit BEFORE scaffolds. But the generate step's scaffold-return commit (where main commits the scaffold output) is a DIFFERENT commit, not covered by the pre-scaffold init. Add explicit guidance at the scaffold-return-commit site:

```markdown
After the scaffold subagent returns, the `/var/www/.git/` directory has been deleted (per the scaffold-subagent's cleanup rule — framework scaffolders create their own `.git/` which we delete so the canonical init in the git-config-mount block wins).

Your next commit on each mount therefore requires `git init -q -b main` BEFORE `git add -A && git commit`:

```bash
ssh {hostname} "cd /var/www && git init -q -b main && git add -A && git commit -q -m 'initial scaffold: {description}'"
```

Do NOT run `git add` before `git init` expecting an earlier `.git/` to persist — the pre-scaffold init's `.git/` was removed by the scaffold's cleanup. This is not a bug; it's the sequencing guarantee that prevents framework-scaffolder `.git/` residue from colliding with the canonical tree.
```

**Test**: assert recipe.md contains both the cleanup rule (scaffold-subagent) AND the re-init rule (main-agent post-scaffold commit), and that they name each other explicitly.

---

## 11. Implementation sequence

Phase 1 — recipe.md edits (RED via the new tests above failing):

1. Fix A — writer-dispatch output-path MANDATORY block.
2. Fix B — two-execOnce-keys section + scaffold brief update + CLAUDE.md template fix.
3. Fix C — Visual style subsection.
4. Fix D — Diagnostic-probe cadence in feature-subagent MANDATORY.
5. Fix F — scaffold-return git-init sequencing note.

Phase 2 — code edits:

6. Fix E — `PerturbsChecks` field on `StepCheck`, populate at emission sites, surface in failure text.

Phase 3 — tests:

7. Write tests for Fixes A/B/C/D/F (structural asserts on recipe.md content).
8. Extend `TestPerturbsChecksEmitted` for Fix E; assert known coupling pairs.
9. `make lint-local`; `go test -race ./...`.

Phase 4 — version bump + catalog:

10. Bump `internal/knowledge/testdata/active_versions.json` to v8.104.0.

---

## 12. v34 calibration bars

What must be true in the next recipe run to call v8.104 a success:

1. **Zero phantom output directories.** `find /var/www -maxdepth 2 -type d -name 'recipe-*'` returns nothing. Writer output lives ONLY at per-codebase README/CLAUDE.md paths.
2. **Seed uses static key.** Every published `initCommands` has `zsc execOnce bootstrap-*` or similar static-key shape on seed. `grep 'execOnce ${appVersionId}.*seed' zerops.yaml` returns nothing. Migrate may still use `${appVersionId}`.
3. **No Unicode box-drawing in zerops.yaml.** `grep -rP '\p{Box_Drawing}' apidev/zerops.yaml appdev/zerops.yaml workerdev/zerops.yaml` returns nothing. No `# ====` / `# ****` / `# ####` standalone decoration lines.
4. **Feature-subagent diagnostic cadence.** ≤10 diagnostic Bash calls in any 30s window. No `echo HELLOWORLD` class probes when prior commands already succeeded.
5. **Content-check convergence.** ≤2 deploy-complete rounds, ≤1 finalize-complete round. If still 3, Fix E's `PerturbsChecks` needs expansion.
6. **Zero pre-init git-commit failures.** `grep -c 'fatal: not a git repository' session.jsonl` returns 0.
7. **All v8.90 + v8.93 + v8.94 + v8.95 + v8.96 + v8.97 + v8.98 + v8.99 + v8.100 + v8.103 calibration items still hold.**

If (1)–(3) pass but (4)–(6) don't, v8.104 closed the authoritative-string gaps but not the cadence/coupling gaps — v8.105 scope is (4)–(6) with stricter wording.

---

## 13. Rollback criteria

If v34 regresses on operational or content axes versus v33 despite (1)–(3) passing, revert Fixes A/C/D and keep only B + E + F. B/E/F are structural (wrong pattern → right pattern, check payload extension, sequencing gate); A/C/D are prose additions to briefs and could be dropping other load-bearing content under context pressure.

Measure: if v34 main bash > 45s (v33: 30s) or MCP schema errors ≥ 2 (v33: 0) while Fixes A/C/D are live, that's the signal to roll them back. Keep the tests as regression guards.

---

## 14. Mental model for the implementer

Every v33 defect is an answer the agent invented to a question recipe.md didn't answer. The phantom tree is an invented output path. The box separators are invented comment decoration. The diagnostic storm is invented recovery. The `${appVersionId}` seed is a pattern inherited from imagination of "how execOnce works" without the positive-example anchor.

v8.104 doesn't add machinery. It closes answer-gaps. Six gaps, six closures. The test for each fix is not "does the guidance now say X" but "can an agent reading only recipe.md and facing the v33 situation still produce the wrong answer?" If yes, the fix isn't done.
