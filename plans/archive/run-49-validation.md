# Run-49 validation — v9.98.0 corpus recalibration vs deliverable

> **Headline: STRUCTURAL-REWRITE-NEEDED — engine-lineage gap.** Run-49 was
> dispatched with `engineVersion: v9.100.1`, but
> `git merge-base --is-ancestor v9.98.0 v9.100.1` is FALSE — the corpus-
> recalibration branch (commits 39d0053b / 2455aa3a / cdbcc0da / cc021a34
> / 0eea18c7 / fcec3818 / 8f8434b8, all dated 2026-05-20) was tagged v9.98.0
> but never merged into the v9.99 → v9.100 mainline lineage that produced
> the engine that drove run-49 on 2026-05-25. As a consequence all four
> corpus corrections are absent from the briefs that fed run-49's sub-
> agents, and the deliverable reproduces all four failure shapes verbatim.
> Codex independently verified the ancestry gap and the four file-level
> regressions (`agentId: a0d15b5f1460c1873`). This is NOT a sub-agent
> stochasticity failure (per `feedback_stochastic_floor`: ~10% drift is
> the irreducible floor and this is 100% regression across all four
> issues across all three codebases) — it's a release-management gap: a
> branch sat un-merged while two minor-version bumps shipped from a sibling
> line.

---

## Per-issue verdict

| # | Issue | Verdict | Codebase scope |
|---|---|---|---|
| 1 | Rolling-deploy mechanism + `maxRam` hallucination | **REGRESSED** verbatim | tier-4 (lines 15-21, 33-37); tier-5 (lines 20, 37) |
| 2 | Cross-service env auto-inject + same-key self-shadow trap | **REGRESSED** verbatim | apidev/zerops.yaml:62-71; workerdev/zerops.yaml:52-61 |
| 3 | `start: zsc noop --silent` on dev runtimes | **REGRESSED** verbatim across all three codebases + enshrined as a KB bullet in appdev | apidev/zerops.yaml:213; workerdev/zerops.yaml:163; appdev/zerops.yaml:86; appdev README KB #2 |
| 4 | execOnce burn-recovery vocab on per-deploy `${appVersionId}` keys | **REGRESSED** — burn-vocab paired with per-deploy keys in yaml comments AND in apidev KB | apidev/zerops.yaml:51-60; workerdev/zerops.yaml:38-51; apidev/README.md:338 |

**No partial-fix evidence anywhere.** Codex spot-checked whether any dev
runtime omits `run.start` entirely (the v9.98.0 target shape per
`cdbcc0da`'s edits to `internal/recipe/content/principles/dev-loop.md` and
`develop-dynamic-runtime-start-container.md`) — every dev `zerops.yaml` in
the run ships `start: zsc noop --silent`.

---

## Issue 1 — Rolling-deploy mechanism + maxRam

### Deliverable surface

`/docs/zcprecipator3/runs/49/environments/4 — Small Production/import.yaml`
lines 15-21:

```yaml
# Run two NestJS api replicas — `minContainers: 2` keeps
# rolling deploys zero-downtime (one container serves traffic
# while the other rebuilds). API session state must stay
# stateless across replicas; check CORS_ORIGINS still resolves
# the active app subdomain. Bump verticalAutoscaling.maxRam
# when monitoring shows containers approaching the current
# ceiling.
```

The same yaml then sets only `minRam: 0.5` + `minFreeRamGB: 0.25` (no
`maxRam` field) at lines 28-31 — the maxRam adapt-knob points at a field
the yaml does not declare, exactly as the spec called out in
`plans/run-49-stale-knowledge-findings.md §"Issue 1" item 4`. The app
tier-4 block at lines 33-46 carries the same wrong story ("same rolling-
deploy story as the api ... bump verticalAutoscaling.maxRam if asset-
serve concurrency saturates the current ceiling"). Tier-5 (`HA
Production/import.yaml`) reproduces "Bump verticalAutoscaling.maxRam" at
lines 20 + 37 in the api + app blocks.

### What's missing

- Zero mention of `temporaryShutdown: false` as the actual zero-downtime
  mechanism.
- Zero "capacity / crash-tolerance" framing for `minContainers ≥ 2`.
- The em-dash welding two distinct thoughts (lines 15-21) — the exact
  voice complaint flagged in the spec.

### Codex confirmation

> "The comment text is present at `import.yaml:15-21`; the yaml block at
> lines 28-31 has no `maxRam` field."

---

## Issue 2 — Cross-service env auto-inject + self-shadow

### Deliverable surface (apidev)

`/docs/zcprecipator3/runs/49/apidev/zerops.yaml` lines 62-71:

```yaml
# Cross-service auto-injects renamed to framework-friendly
# own-keys (DB_HOST, NATS_PASS, S3_ENDPOINT, MEILI_HOST,
# ...) so the application code reads stable names and a
# managed-service swap stays a yaml-only edit. Pick own-
# key names DIFFERENT from the platform side — same-key
# (`db_hostname: ${db_hostname}`) self-shadows because the
# per-service write runs after the auto-inject and the
# literal token wins. Project-level envs (FRONTEND_URL,
# APP_SECRET) auto-propagate under their own names and are
# deliberately NOT re-declared here.
```

### Deliverable surface (workerdev)

`/docs/zcprecipator3/runs/49/workerdev/zerops.yaml` lines 52-61:

```yaml
# Every managed-service credential is aliased to a
# framework-friendly own-key — DB_HOST, NATS_HOST,
# MEILI_HOST, CACHE_HOST. Application code reads only
# these names so swapping a managed service later is a
# yaml-only edit, no app rebuild. The own-key on the
# left MUST differ from the platform name on the right;
# declaring `db_hostname: ${db_hostname}` self-shadows
# because the per-service envVariables write runs after
# the platform auto-inject and overwrites the resolved
# value with the literal token string.
```

This is verbatim the architectural error documented in `plans/audit-env-
vars-20260515/AUDIT.md §B3` (under porter-default `envIsolation: service`,
cross-service refs do NOT auto-inject; explicit declaration in
`run.envVariables` is the only path). Run-49 still teaches:

1. The auto-inject premise as universal platform behavior
2. The "swap a managed service later" framing as the motivation for
   aliasing (the spec calls this "framing aliasing as optional
   convenience" — wrong; aliasing is the only path)
3. The "same-key self-shadow trap" as if it were a real failure mode
4. Project-level vars "auto-propagate" (this half is correct — but
   bundled with the wrong cross-service framing)

### Stakes — apidev README:197 surfaces a KB+IG citation

```
The full project / cross-service env model — including the build-vs-
runtime container split — is covered in the [per-key env shape and
cross-service aliases](https://docs.zerops.io/zerops-yaml/specification#envvariables-).
```

This citation points at the canonical spec but the surrounding prose
teaches the wrong model. Porters who follow the IG and then click
through to the spec will hit a disagreement.

### What's missing

- The explicit "you MUST declare cross-service refs in `run.envVariables`
  or your app does not see the value" framing.
- The clean project-level / cross-service distinction without bundling
  them into a single "auto-inject" word.
- Zero `envIsolation` token leakage to porter prose — this part is
  correctly absent in run-49, so the purge directive ("porters should
  not learn legacy mode exists") was at least partially respected. But
  the underlying model the corpus teaches is the wrong half of the
  isolation split, presented as if it were universal.

### Codex partial pushback

Codex flagged that my initial citation `workerdev/zerops.yaml:75-84` was
the wrong line range — content lives at 52-61. **The content claim
stands; the line citation was sloppy.** Corrected above.

---

## Issue 3 — `start: zsc noop --silent` on dev runtimes

### Deliverable surface — all three codebases ship the pattern

`/docs/zcprecipator3/runs/49/apidev/zerops.yaml:205-213`:

```yaml
# `zsc noop --silent` keeps the container alive without
# binding the runtime to a foreground process — the dev
# container is a remote-development workspace. SSH in, run
# `npm run start:dev` (Nest's watch script) by hand, and
# edits over the SSHFS mount rebuild in place with no
# redeploy cycle. Liveness comes from the listening port
# plus `curl /api/health`, not a pidfile, because the
# watcher rotates its child PID on every rebuild.
start: zsc noop --silent
```

`/docs/zcprecipator3/runs/49/workerdev/zerops.yaml:158-163` and
`/docs/zcprecipator3/runs/49/appdev/zerops.yaml:81-86` reproduce the same
narration + start line for each codebase.

### Stakes — appdev enshrines the stale pattern as a KB bullet

`/docs/zcprecipator3/runs/49/appdev/README.md:199-210` is titled
**"Dev container starts but the Vite dev server doesn't"** and teaches:

> "The `dev` setup uses `start: zsc noop --silent` — the container is
> alive but no Node process is running. That's deliberate so SSHFS-
> mounted source edits drive Vite's HMR rebuilds in place instead of
> forcing a redeploy on every keystroke. SSH in and run the dev server
> by hand..."

This is the worst single content regression in the run — not just a
pattern in yaml but a knowledge-base bullet that crystallizes the stale
mechanism as a porter-facing teachable lesson, complete with
ssh-and-run instructions and subdomain-shape narration.

### Smoking gun — facts log + brief still encode the convention

`/docs/zcprecipator3/runs/49/environments/facts.jsonl` carries multiple
`worker_dev_server_started` / `field_rationale` entries quoting
`start: zsc noop --silent` as the reason. The cached brief at
`environments/.briefs/scaffold-app-1779701093614434216.md` still teaches
`**dev** — start: zsc noop --silent, NO healthCheck, ...` at line 489.
Both are downstream of `internal/recipe/content/principles/dev-loop.md`
which v9.98.0 commit `cdbcc0da` rewrote — but that commit is not in the
v9.100.1 lineage that produced the brief used here.

### Codex confirmation

> "All three dev files have `zsc noop --silent` at the stated lines.
> Your secondary claim — that a dev runtime omits `run.start` entirely —
> is contradicted: grep shows all dev `zerops.yaml` files have `run:` and
> `start:`. No partial-fix evidence from omission."

---

## Issue 4 — execOnce burn-recovery vocab on per-deploy keys

### Deliverable surface (apidev yaml)

`/docs/zcprecipator3/runs/49/apidev/zerops.yaml:51-60`:

```yaml
# Schema migration runs once per deploy across the rolling
# group — `${appVersionId}-api-migrate` is unique to this
# deploy version + this codebase, so exactly one container
# in the group executes migrate.js and the others see the
# key taken and skip. `--retryUntilSuccessful` covers the
# boot window before Postgres accepts connections. The
# script is idempotent (CREATE TABLE IF NOT EXISTS, seeds
# via WHERE NOT EXISTS) so a burned key on a failed deploy
# converges on the next version.
```

Same conflation in `/docs/zcprecipator3/runs/49/workerdev/zerops.yaml:38-51`.

### Deliverable surface (apidev KB)

`/docs/zcprecipator3/runs/49/apidev/README.md:338`:

> "Without `--retryUntilSuccessful`, the very first deploy on a fresh
> project races Postgres's accept loop; the migrator gets `ECONNREFUSED`,
> the key burns, and that container never tries again."

This is the spec's exact failure shape — burn-recovery vocabulary
(`the key burns`) paired with `${appVersionId}-api-migrate` per-deploy
key. Per `internal/recipe/content/principles/init-commands-model.md:44-60`,
burn-recovery is a static-key concern; per-deploy keys regenerate every
deploy so the burn-recovery concept doesn't apply.

### What was NOT regressed

Run-49's codebases ship single-key init commands (one `migrate` per
codebase, no `seed` split) — so the specific cross-deploy "failed seed
doesn't burn the migrate key" failure shape from the original feedback
isn't reproduced. What IS reproduced is the burn vocabulary on per-
deploy keys, which is the underlying conflation.

### Codex confirmation

> "${appVersionId} + burn/converges vocabulary present in apidev:51-61
> and workerdev:38-51. README carries `ECONNREFUSED` + 'key burns' near
> line 338."

---

## Root-cause analysis — engine-lineage gap

### Git ancestry (codex-verified independently)

```
$ git merge-base --is-ancestor v9.98.0 v9.100.1; echo $?
1   # NOT an ancestor

$ git tag --sort=-creatordate | head
v9.100.1
v9.100.0
v9.99.0
v9.98.0     ← the four fix commits live here, on a side branch
v9.97.0
...
```

The four fix commits (`39d0053b`, `2455aa3a`, `cdbcc0da`, `cc021a34`,
plus `0eea18c7`/`fcec3818`/`8f8434b8`) all dated 2026-05-20 sit on the
`corpus-recalibration-2026-05-20` branch, tagged v9.98.0. Two
minor-version bumps later, v9.99.0 → v9.100.0 → v9.100.1 shipped from a
sibling line that never picked up the recalibration.

`plans/run-49-stale-knowledge-findings.md` was the TRIGGER for v9.98.0
(its commit messages all close with `(close R49-I1)`/`-I2`/`-I3`/`-I4`).
Run-49 the deliverable was dispatched 5 days AFTER v9.98.0 was tagged
(session date 2026-05-25 vs tag 2026-05-20) — but with an engine from
the wrong lineage. The fixes that closed R49 issues never reached the
engine that produced what we call "run-49 the output."

### Why the briefs don't propagate the fixes

The brief snapshot at
`environments/.briefs/scaffold-app-1779701093614434216.md:489` still
teaches the pre-v9.98.0 dev-loop principle. Each sub-agent reads the
brief as authoritative; the brief is regenerated from
`internal/recipe/content/principles/dev-loop.md` at engine startup. The
v9.100.1 engine bundles the pre-v9.98.0 dev-loop principle.

### What's NOT root-cause

- ~10% stochastic drift (`feedback_stochastic_floor`): if drift were the
  driver, we'd see one of three codebases drifting on one of four
  axes — not 100% regression across all twelve cells.
- Sub-agent disobedience: every cited regression traces to either a
  brief sentence or a workflow-recipe.md sentence that wasn't rewritten.
- Brief priming gap: the briefs WERE primed for these failure shapes —
  the priming files just weren't updated.

---

## Cross-cutting observations

### Engine-version stamping is load-bearing — and disconnected

Per the CLAUDE.md invariant *"Engine version stamps the plan"* —
`plan.engineVersion = v9.100.1` is correctly stamped. The downstream
gate that refuses on engine-version mismatch (`TestGateEngineVersionStamped_*`)
fires on mismatch within a session, not on whether the engine itself
carries the latest corpus corrections. Nothing in the gate architecture
catches "wrong release lineage."

### `worker_dev_server_started` fact attestation still emitted everywhere

`environments/facts.jsonl` lines 6, 10, 15, 18, 23, 31, 59, 63 all emit
the fact with prose justifying `zsc noop --silent` as the gate-clearing
rationale. v9.98.0 commit `cdbcc0da` refactored
`internal/recipe/gate_worker_dev_server.go` to predicate on dynamic-
runtime base rather than the literal start-string — but the run-49
engine still uses the pre-refactor gate that demands the `worker_dev_
server_started` attestation alongside `zsc noop`. The fact attestations
in the log are correct relative to the engine that produced them; they're
just structurally tied to an obsolete platform pattern.

### appdev KB carries the worst single content regression

The KB bullet at `appdev/README.md:199-210` titled "Dev container starts
but the Vite dev server doesn't" doesn't merely USE the stale pattern —
it formalizes it as a teachable porter trap with SSH ceremony. If the
v9.100.1 → v9.101+ release picks up v9.98.0's edits, this KB bullet
should not survive without a full rewrite or DROP.

### The corpus rewrite WAS comprehensive — it's just stranded

`git show --stat v9.98.0` touched 15+ files for Issue 1 alone, 30+ for
Issue 2 including golden test fixtures, 33 for Issue 3 including the
worker-dev-server gate refactor. The diff is real, careful, and
internally consistent. The problem is purely release-mechanical.

---

## Recommendation — STRUCTURAL-REWRITE-NEEDED, but the rewrite already exists

1. **Merge `corpus-recalibration-2026-05-20` (v9.98.0) into mainline**
   before the next dispatch. The four fix commits + the three
   gate/test/theme commits are the load-bearing payload; rebasing onto
   v9.100.1's head likely needs care around `internal/recipe/
   gate_worker_dev_server.go` (which the recalibration refactored
   substantially) and `internal/content/workflows/recipe.md` (which
   probably saw orthogonal edits in v9.99/v9.100).

2. **Add a release-gate test** that fails the next minor bump if the
   release lineage doesn't include known-fix branches. This is one tier
   above the engine-version stamp — the stamp catches per-session
   mismatch, not release-lineage drift. A simple approach: a CI check
   that `git merge-base --is-ancestor v$PREV_FIX_TAG HEAD` for any tag
   matching a known-fixes whitelist.

3. **Defer running another nestjs-showcase recipe pass** until the merge
   lands. Running v9.100.1 → v9.100.2 dispatch with the same corpus will
   reproduce the same four failures with 100% probability.

4. **After the merge, run a single confirmatory dispatch** of nestjs-
   showcase with the merged corpus. The four failure shapes should all
   be ABSENT; the KB at `appdev/README.md:199-210` should not exist;
   the dev `start:` line should be omitted entirely; the tier yaml
   rolling-deploy block should cite `temporaryShutdown: false` as the
   mechanism with `minContainers ≥ 2` framed as capacity/crash-
   tolerance; the env-block comment should teach explicit declaration
   as the only path; the burn vocabulary should be absent from per-
   deploy key prose.

### What does NOT need rework

- Run-49's substrate operations (refinement-2 manifest counting, citation-
  display gates from v9.93.0) are operating cleanly within their scope.
  This is a release-management failure, not a substrate failure.

### What this is NOT

- This is **not** a content-quality regression caused by sub-agent
  drift — `feedback_stochastic_floor` rules that out at 100% recurrence
  across 12 cells.
- This is **not** a corpus-authoring miss — the v9.98.0 diffs are
  comprehensive and structurally correct.
- This is **not** a verifier ground-truth gap — the audit in
  `plans/audit-env-vars-20260515/AUDIT.md §B3` provides live `printenv`
  evidence for Issue 2; Issue 1 is documented at `docs.zerops.io/
  features/pipeline`; Issue 3 is owner-authoritative; Issue 4 is
  internal-consistency.

---

## Codex validation references

| Finding | Codex verdict | Agent ID |
|---|---|---|
| All four issues regress verbatim; root cause is engine-lineage gap (v9.98.0 not ancestor of v9.100.1); no partial-fix evidence in any dev runtime omission | CONFIRMED — independently verified ancestry, file-level evidence, no run.start omissions; pushed back on one of my line citations (workerdev Issue 2 content is at 52-61 not 75-84) | `a0d15b5f1460c1873` (warm; SendMessage to continue) |

---

## Verdict — STRUCTURAL-REWRITE-NEEDED (release-mechanical, not corpus-authorial)

Run-49 reproduces all four failure shapes that v9.98.0 was authored to
close. The root cause is that v9.98.0 (tagged 2026-05-20 from the
`corpus-recalibration-2026-05-20` branch) was not an ancestor of v9.100.1
(the engine version that produced run-49 on 2026-05-25). The merge gap
swallowed seven careful corpus commits and rendered five days of correct
authoring invisible to the next dispatch.

Three actions in order: (1) merge v9.98.0 into mainline, (2) add a
release-gate check for known-fix-branch inclusion, (3) re-dispatch
nestjs-showcase with the merged engine and confirm the four failure
shapes are absent. Skip a stage and the next run reproduces this
verbatim.
