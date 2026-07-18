# Run-32 iteration-cost fixes — agent-side smartness gaps surfaced by log audit

Author: 2026-05-08. Sibling to `docs/zcprecipator3/runs/32/ANALYSIS.md`.
The ANALYSIS predicate sweep verdicts (3/3 v9.73.0 fixes CONFIRMED,
12/12 prior fixes hold) are unchanged. This spec covers iteration-cost
patterns the predicates don't catch — agents burning round-trips on
validators / close-gates that already teach correctly, but where the
brief-side teaching is incomplete or the close-gate workflow needs a
batch-fix discipline rule.

All edits are brief-side (no engine changes). Two HIGH (root-causable
to brief-incompleteness), three MEDIUM (one-shot ergonomic traps that
fired twice + one tool-scope ambiguity), two LOW (Bash discipline).
Original F-51 (`ssh` PATH discipline) dropped after codex review —
insufficient evidence (N=1 ssh-not-found event) for spec-grade
prescription. F-53 promoted from LOW to MEDIUM after codex flagged
that no brief explicitly bans `zerops_workflow` from recipe authoring,
making it a scope-ambiguity gap not a one-shot anecdote.

## Root-cause taxonomy

| ID | Severity | Cost | Root cause |
|----|----------|------|------------|
| F-47 | HIGH | 9 wasted record-fragment calls on 1 KB entry (cc-worker) | Brief lists 6 verbs + 4 observables vs validator regex's 18 verbs + 12 observables (`internal/recipe/slot_shape.go:116-119` vs `synthesis_workflow.md:404-410`). Brief teaches the agent to *recall* a vocabulary; it should teach the agent to *self-check* its draft stem against the regex's whitelist before recording. |
| F-48 | HIGH | ~9 wasted complete-phase calls on scaffold close-out (api+worker) | Brief says "Repeat until ok:true" — no rule that says fix ALL returned violations before retrying. Agent treats violations like a queue, fixes one, retries, sees N-1, repeats. |
| F-49 | MEDIUM | 2 one-shot retries (feature-backend codebase=apidev, env-content fragmentId env/0/import-comments/apidev) | Refinement brief carries the hostname-vs-bare-name teaching; scaffold/feature/env-content/codebase-content briefs do not. Agents instinctively reach for the slot hostname they've been working with all phase. |
| F-50 | MEDIUM | 4 one-shot retries (env-content body cap 14>8, JetStream framing token, intro 353>350) | Per-tier-authoring brief mentions the 8-line cap once at line 23 and reiterates at line 654; not surfaced where the agent composes per-host blocks. |
| F-53 | MEDIUM | 1 misroute (feature-backend `zerops_workflow workflow=develop` PREREQUISITE_MISSING) | No brief explicitly bans `zerops_workflow` from recipe authoring. The MCP tool description at `internal/tools/workflow.go:149` advertises `develop` as public; the recipe sub-agent prompt at `briefs_subagent_prompt.go:303` says use `zerops_recipe` not Bash, but doesn't say "don't reach for the porter-facing workflow runner." Scope-ambiguity gap, not one-shot anecdote. |
| F-52 | LOW | 1 git-commit exit-1 (scaffold-worker, "nothing to commit") | Briefs teach naked commit shape (`scaffold/content_authoring.md:339`, `phase_entry/scaffold.md:26`, `feature/decision_recording.md:35`) without status-pre-check. Cancels parallel tool calls in the same message as collateral. |
| F-54 | LOW | 1 wrong-path `ls` (refinement) | Same hostname-vs-bare-name trap as F-49, in filesystem shape (`ls /var/www/zcprecipator/<slug>/api/` — mounts live at `apidev/appdev/workerdev`). Refinement brief teaches the trap at fragmentId / `codebase=` level; doesn't carry the dual filesystem-vs-MCP rule. |

Run-32 wall-time was 131 minutes; the iteration cost above adds up to
~5-8 minutes. None are systematic failures by Axis B, F, or H
predicates. They're the residual smartness gap — the engine teaches in
the error response, the agent didn't anticipate.

## F-47 — Bring brief-side stem heuristic into parity with validator regex (HIGH)

### Evidence

`runs/32/SESSION_LOGS/subagents/agent-ae019d969848c8115.jsonl` (cc-worker).
Sequence of 10 record-fragment calls on `codebase/worker/knowledge-base`,
all targeting the multi-replica-NATS-duplicate-processing entry:

| # | Stem fragment | Result | Why |
|---|---------------|--------|-----|
| 1 | `Every job processed twice after scaling past one replica` | ERR | no HTTP, no quoted, "processed" not in verb list, no observable |
| 2 | `Each job processed N times when minContainers > 1...` | ERR | same |
| 3 | `Processed counter doubles per replica when the worker scales...` | ERR | "doubles" not in verb list |
| 4 | `Every job processed twice (or N times) under multi-replica boot` | ERR | same |
| 5 | `Each replica processes every message — duplicate side effects...` | ERR | "processes" not in verb list |
| 6 | `Worker duplicates side effects on every job...` | ERR | "duplicates" not in verb list |
| 7 | `Job processed twice, processed counter doubles per replica` | ERR | same |
| 8 | `Subscription without queue group breaks load-balancing — every replica processes...` | OK | `breaks` in verb list |
| 9 | `Processed counter doubles per replica, downstream caches double-write under multi-replica boot` | ERR | "double-write" not in verb list |
| 10 | `Missing queue-group option crashes exactly-once delivery — every replica processes...` | OK | `missing` + `crashes` both in verb list |

### Root cause

Codex review confirms verb counts: validator at `internal/recipe/slot_shape.go:113-119` matches against 18 verbs:

```
fails, crashes, corrupts, deadlocks, silently exits, silently stops,
returns null, breaks, drops, rejects, missing, hangs, times out,
panics, leaks, stalls, truncates, drained
```

12 observables:

```
empty body, wrong header, null where, 404 on, 502 on, empty response,
stale data, zero rows, no rows, unbound, undefined, forbidden
```

The brief at `internal/recipe/content/briefs/codebase-content/synthesis_workflow.md:404-410` summarises down to 6 verbs and 4 observable examples (one of those — "missing manifest" — uses a regex-matched verb root but isn't itself a regex-listed observable phrase, illustrating the brief/regex drift).

**The behavioural claim ("agent rephrased within the lossy subset") is partly speculation** — codex pushback. What's verifiable from the run-32 transcript is:

- 7 of 8 attempts use no whitelisted verb / observable / HTTP / quoted token at all.
- Attempts 8 (`Subscription without queue group breaks load-balancing`) and 10 (`Missing queue-group option crashes exactly-once delivery`) both use multiple regex-matched tokens (`breaks`, `missing`, `crashes`).
- Between accepted attempts 8 and 10, the agent backtracked to attempt 9 (`Processed counter doubles per replica, downstream caches double-write`) — no whitelisted tokens. The agent didn't internalise WHY 8 was accepted.

### Fix shape

The fix is **teach the agent to self-check, not memorise**. Edit `synthesis_workflow.md:380-435` to:

1. Replace the lossy heuristic with a **stem-self-check rule**: after drafting a stem, the agent runs the stem text through the four signal classes (HTTP code, quoted token, failure verb, observable) and confirms at least one matches BEFORE calling `record-fragment`. If none match, restate using one of the verbs / observables from the validator's whitelist (listed verbatim below).
2. Include the **complete verb whitelist** (18 verbs) and **complete observable whitelist** (12 phrases) directly from `slot_shape.go` so the brief can be scanned as a vocabulary reference, not a memorise-list.
3. Add a worked BAD/GOOD pair from run-32:
   - **BAD** stem: `**Every job processed twice after scaling past one replica**` — author-claim shape; `processed`/`scaling` aren't whitelisted verbs.
   - **GOOD** rewrite: `**Missing queue-group option crashes exactly-once delivery — every replica processes every message**` — `Missing` + `crashes` both whitelisted; stem names the directive + the failure verb + the porter-observable.

### Test

No new test on the validator (already pinned by `slot_shape_run17_test.go`). A brief-vs-regex drift lint (assert the brief contains every verb / observable the regex matches) is a useful follow-up but out of scope here — the brief is the source of truth for the agent, not a duplicate spec, so a one-way lint (regex-superset check) is the right shape if added later.

## F-48 — Scaffold close-out batch-fix discipline (HIGH)

### Evidence

`runs/32/SESSION_LOGS/subagents/agent-a7ea0e62c4abecd5e.jsonl` (scaffold-api):

| Attempt | factsCount | violations | violations fixed since prev |
|---------|------------|------------|----------------------------|
| 1 | 55 | 8 | — |
| 2 | 62 | 7 | 1 |
| 3 | 63 | 7 | 0 (added 1 fact that didn't satisfy any violation) |
| 4 | 66 | 6 | 1 |
| 5 | 72 | 0 | 6 (final batch fix) |

`runs/32/SESSION_LOGS/subagents/agent-ad3e7b804fd3d1bf3.jsonl` (scaffold-worker): 4 retries, similar pattern (factsCount 23 → 31 → 35 → ok).

### Root cause

`internal/recipe/content/briefs/scaffold/content_authoring.md:222-261` teaches:

> Self-validate before terminating
>
> Before you terminate, call:
>
>     zerops_recipe action=complete-phase phase=scaffold codebase=<your-host>
>
> ...
> If `ok:false` with violations:
> - Violations on `codebase/<host>/{integration-guide,knowledge-base,
>   claude-md/*}` ids → fix in-session via `record-fragment mode=replace`
> - Re-call `complete-phase phase=scaffold codebase=<your-host>` to verify the fix.
> - Repeat until `ok:true`, then terminate.

The "Repeat until `ok:true`" framing reads as queue — fix one, retry,
fix next. There's no rule that says "the violations response carries
EVERY missing rationale path, fix all of them in one batch, retry once."

The validator is pre-tuned to surface every missing rationale per
yaml directive group (`build`, `run.start`, `run.ports`, `run.envVariables`,
`run.initCommands`, `run.healthCheck`, etc.). It already returns the
full list with the exact `FieldPath` suffix the agent needs to attach
on each `record-fact kind=field_rationale`. The agent has all the
information to fix all 8 violations in one batch.

### Fix shape

Codex flagged a drafting risk on the original "issue all fixes in one batch (multiple parallel tool calls)" wording: **don't promise engine-level parallel batching**. Each `record-fact` is a separate MCP call serialised at `FactsLog`'s mutex (`internal/recipe/facts.go:243,274`). Whether the agent issues them in one Claude message or sequentially is irrelevant; what matters is the **complete-batch-before-retry** rule.

Edit `internal/recipe/content/briefs/scaffold/content_authoring.md:235-261` — replace the "Repeat until" paragraph with:

> If `ok:false` with violations:
>
> 1. The response carries EVERY violation discovered on this call.
>    Read the full list before issuing any fix.
> 2. Group violations by fix shape:
>    - `fact-rationale-missing` → `record-fact kind=field_rationale
>      fieldPath=<suffix from violation>` (one fact per violation).
>    - surface-shape failures (KB stem, IG body cap, etc.) →
>      `record-fragment mode=replace` with corrected body.
>    - yaml-comment violations → ssh-edit the yaml file directly.
> 3. Issue ALL fixes for the violations you saw THIS call, then call
>    `complete-phase phase=scaffold codebase=<your-host>` ONCE more.
>    Whether you batch fixes in a single Claude message (parallel tool
>    calls) or issue them sequentially is a discipline choice — the
>    rule is "complete batch before retrying complete-phase."
> 4. If the second call surfaces NEW violations (a fix introduced one,
>    or a violation depended on prior fix), repeat. The expected steady
>    state is two complete-phase calls per codebase: first surfaces
>    everything; second confirms.
>
> Anti-pattern (cost: 4-5 round-trips per codebase): fix one violation,
> retry complete-phase, see N-1 violations, fix one, retry, ... The
> validator returns the FULL list every call; treating violations as a
> queue wastes round-trips on information already provided.

Same edit applies — reduced — to:

- `internal/recipe/content/briefs/scaffold/decision_recording_slim.md` (slim variant for terse agents).
- `internal/recipe/content/briefs/feature/decision_recording.md` (feature-phase close-out — run-32 feature-backend hit a similar pattern with `scaffold-yaml-leaked-comment` post-feature).

### Test

A fixture-based subagent simulator could exercise close-gate
batch-fix vs queue-fix patterns and assert the round-trip count. Out
of scope for this fix; the brief edit is sufficient.

## F-49 — Pre-empt codebase=hostname trap on every brief that takes `codebase=` (MEDIUM)

### Evidence

Two agents on run-32 hit the trap on first use:

- `runs/32/SESSION_LOGS/subagents/agent-a4eef6522ad6a0d8b.jsonl` (feature-backend): `complete-phase phase=feature codebase='apidev'` → engine error "unknown codebase 'apidev' (Plan codebases: [api app worker]); if you used a slot hostname like 'appdev'/'appstage', use the bare codebase name (e.g. 'app')". Recovered on next call with `codebase='api'`.
- `runs/32/SESSION_LOGS/subagents/agent-ac8c6915c7eb8fefc.jsonl` (env-content): `record-fragment fragmentId='env/0/import-comments/apidev'` → engine error "unknown fragmentId". Recovered with `fragmentId='env/0/import-comments/api'`.

### Root cause

`internal/recipe/content/briefs/refinement/synthesis_workflow.md:30` carries the teaching:

> If the engine returns "unknown codebase 'workerdev' (Plan codebases:
> [api app worker])", drop the slot suffix and retry with the bare name.

But this teaching is only in the refinement brief. The scaffold,
feature, env-content, and codebase-content briefs do NOT carry the
same pre-empt. Agents working all phase against `apidev`/`appstage`
slot-hostnames instinctively reach for those names when an MCP tool
asks for `codebase=`.

### Fix shape

Codex pushback: brief duplication across 4 files is the wrong default — `internal/recipe/briefs.go:386,389` already exposes `readAtom("principles/<file>.md")` as the include mechanism, and the scaffold composer (`briefs.go:461-498`) + codebase-content composer (`briefs.go:643-660`) already include lists of `principles/*.md` atoms. The right shape is one shared principle file referenced by all 4 composers.

**Create** `internal/recipe/content/principles/codebase-name-vs-slot-hostname.md`:

> # Codebase name vs slot hostname
>
> Plan.Codebases[].Hostname is the bare codebase name
> (`api`, `app`, `worker`) — that's what these recipe-MCP parameters
> consume:
>
> - `complete-phase codebase=<bare>`
> - `fragmentId=codebase/<bare>/{integration-guide,knowledge-base,zerops-yaml,claude-md,intro}`
> - `fragmentId=env/<N>/import-comments/<bare>`
>
> The slot hostnames you see in `zcli`, SSHFS mount paths
> (`/var/www/<slot>`), and cross-service refs (`${<peer>_*}`) —
> `apidev`/`apistage`, `appdev`/`appstage`, `workerdev`/`workerstage`
> — are deploy-slot identifiers. Two slots map onto one codebase
> (`apidev`+`apistage` → `api`).
>
> Filesystem paths use the slot hostname (`ls /var/www/apidev/src`).
> Recipe-MCP parameters use the bare codebase name. When you see
> "unknown codebase 'workerdev' (Plan codebases: [api app worker])",
> drop the `dev`/`stage` suffix and retry.

**Append** the new principle to the 4 composers — codex flagged that
the integration mechanism is **non-uniform** across composers:

- **scaffold** (`internal/recipe/briefs.go:450-498`) — `atoms []string`
  slice + `readAtom` loop. Add `"principles/codebase-name-vs-slot-hostname.md"`
  to the slice.
- **feature** (`internal/recipe/briefs.go:649-660`) — same atoms-slice
  pattern as scaffold. Add the principle to the slice.
- **codebase-content** (`internal/recipe/briefs_content_phase.go:467-483`,
  `appendCodebaseContentAtoms`) — uses `appendAtomIfFound(b, parts, "principles/...")`
  per-atom calls. Add one new call referencing the principle.
- **env-content** (`internal/recipe/briefs_content_phase.go:154-219`) —
  uses direct `readAtom` blocks per atom. Add a new direct-readAtom
  block.

Each composer's exact integration shape differs; the spec lands one
edit per composer (~3-6 lines each, not one line each). The principle
file content is identical; the include sites diverge.

Refinement brief (`refinement/synthesis_workflow.md:30`) keeps its
inline teaching since refinement-specific instructions wrap the
principle with refinement-action guidance.

### Test

Existing `assemble.go:694` engine error remains the safety net. Brief
edit shifts cost from "engine catches + agent retries" to "agent never
makes the call". A simple grep test (`atoms_lint_test.go` or
equivalent) can assert the principle is included in the 4 expected
composer atom-lists, but isn't strictly required — the composer's
include pattern is straightforward.

## F-50 — Surface env-content caps where the agent composes per-host blocks (MEDIUM)

### Evidence

`runs/32/SESSION_LOGS/subagents/agent-ac8c6915c7eb8fefc.jsonl`:

- `record-fragment fragmentId='env/0/import-comments/project'` rejected
  with "≤ 8 lines; got 14".
- `record-fragment fragmentId='env/0/import-comments/broker'` rejected
  with "body invokes JetStream framing (token matched: JetStream)
  without an attesting fact".
- `record-fragment fragmentId='env/0/intro'` rejected with
  "353 chars > 350-cap".

All recovered on retry, but each round-trip burns ~10s.

### Root cause

`internal/recipe/content/briefs/env-content/per_tier_authoring.md:23`
mentions the 8-line cap exactly once:

> author `env/<N>/import-comments/<host>` (≤ 8 lines per block)

The 350-char intro cap and the JetStream framing rule live in
separate sections (intro_authoring.md and one of the principles
files). When the agent is composing tier 0 import-comments and
broker-block specifically, none of these caps are in front of them.

### Fix shape

Edit `internal/recipe/content/briefs/env-content/per_tier_authoring.md`:

1. Promote the structural caps to a top-level "Surface caps to
   self-check before record" section, listed once with surface +
   cap + spec anchor:

   ```
   - root/intro: 1 sentence, 500 chars
   - env/<N>/intro: 1-2 sentences, 350 chars
   - env/<N>/import-comments/project: ≤ 8 lines
   - env/<N>/import-comments/<host>: ≤ 8 lines
   ```

2. Add a "Forbidden tokens that need attesting facts" sub-section
   listing the slot-shape regex outputs the agent should pre-empt:
   - `JetStream` (NATS) — needs a `nats-jetstream-framing` fact
   - any framework-specific token names that have appeared
     historically (the validator-regex source is the authority).

This is a presentation reorg, not new content.

### Test

No new test. Existing slot-shape tests pin the validator behaviour;
the brief edit shifts the teaching to where the agent reads it
mid-composition.

## F-51 — DROPPED after codex review

Original draft proposed a brief edit teaching defensive `/usr/bin/ssh`
absolute-path use after `ssh: command not found` errors in
feature-frontend. Codex flagged this as N=1 log anecdote, not
spec-grade evidence — only one sub-agent on one run hit the trap and
self-recovered after 2 retries. The 44 subsequent `ssh ...` calls in
the same agent succeeded, so the proposed fix is solving a problem
the data doesn't establish. **Dropped from spec.** Re-open if a future
run reproduces.

## F-52 — `git status` pre-check before `git commit` (LOW)

### Evidence

`runs/32/SESSION_LOGS/subagents/agent-ad3e7b804fd3d1bf3.jsonl` (scaffold-worker):
`ssh workerdev "git add -A && git commit -q -m '...' && git log..."`
returned exit 1 "nothing to commit, working tree clean". The commit
failed (no diff to commit), the parallel `complete-phase` call was
cancelled as collateral.

### Root cause

Brief teaches `git add -A && git commit -m '...'` as the standard
commit shape; doesn't teach the empty-diff guard.

### Fix shape

Edit the relevant scaffold brief that teaches git commits — gate the
commit on a non-empty diff:

> **Pre-check before commit.** Run `git status --porcelain` first.
> If empty, skip the commit (nothing to commit; exit-1 from
> `git commit` cancels parallel tool calls in the same message).
> Shape: `ssh <slot> "cd /var/www && [ -n \"\$(git status --porcelain)\" ] && git add -A && git commit -m '...' || echo 'no changes to commit'"`.

LOW priority; one-shot bash discipline.

## F-53 — Clarify which MCP tools belong to recipe authoring (MEDIUM)

### Evidence

`runs/32/SESSION_LOGS/subagents/agent-a4eef6522ad6a0d8b.jsonl` (feature-backend):
`mcp__zerops__zerops_workflow {"action": "start", "workflow": "develop"}` →
`PREREQUISITE_MISSING: No bootstrapped services found`. Engine returned
a recovery hint pointing at `route=adopt`/`workflow=bootstrap`, neither
of which is correct for the feature phase. Agent didn't follow the hint
and moved on without using `zerops_workflow` at all (correct outcome).

### Root cause (codex pushback: MEDIUM, not LOW)

`internal/tools/workflow.go:149` advertises `develop` as a public
workflow alongside `bootstrap` / `adopt` — visible to any agent. The
recipe sub-agent prompt at `internal/recipe/briefs_subagent_prompt.go:303`
teaches "use `zerops_recipe` MCP, not Bash" but doesn't say "don't reach
for the porter-facing workflow runner from inside recipe authoring."

The feature brief at
`internal/recipe/content/briefs/feature/content_extension.md:92` teaches
v3 `zerops_recipe` over legacy `zerops_record_fact`, but doesn't address
the unrelated `zerops_workflow` namespace. Scope-ambiguity gap, not
one-shot anecdote — N=1 occurrence is incidental; the brief permits the
mistake by not naming the boundary.

### Fix shape

Edit `internal/recipe/briefs_subagent_prompt.go::writePromptRecipeContext` (or the
sub-agent system prompt template) — add a one-paragraph scope note:

> **Recipe-authoring MCP scope.** The recipe MCP is `zerops_recipe`
> only. Use it for: fragment recording (`record-fragment`), fact
> recording (`record-fact`), phase progression (`complete-phase`),
> self-validation, and dispatch helpers. Other `mcp__zerops__*` tools
> are platform / porter facing:
>
> - `zerops_import` / `zerops_subdomain` / `zerops_env` / `zerops_logs` /
>   `zerops_events` — platform operations the recipe needs, allowed.
> - `zerops_workflow` (any `workflow=` value: `develop`, `bootstrap`,
>   `adopt`) — porter-facing high-level workflow runner. Recipe
>   authoring agents do NOT invoke this; if you find yourself reaching
>   for `workflow=develop`, you're trying to run the porter's dev loop,
>   which is not a recipe-authoring action.
> - `zerops_browser` — browser walk + screenshot for feature-phase
>   verification, allowed.
>
> When unsure, `zerops_recipe` is the recipe-authoring tool; everything
> else is platform infrastructure or porter-facing.

MEDIUM priority — sample size is N=1 but the brief gap is structural
(no positive scope rule).

## F-54 — Refinement Bash codebase-vs-slot-hostname trap (LOW)

### Evidence

`runs/32/SESSION_LOGS/subagents/agent-a1cfd12f3300716d4.jsonl` (refinement):
`ls -la /var/www/zcprecipator/nestjs-showcase/api/ /var/www/zcprecipator/nestjs-showcase/app/ /var/www/zcprecipator/nestjs-showcase/worker/` →
exit 2 "No such file or directory". Codebases are mounted at
`apidev/appdev/workerdev`, not `api/app/worker`. Same trap as F-49 in
Bash shape.

### Root cause

`internal/recipe/content/briefs/refinement/synthesis_workflow.md:30`
teaches the trap for fragmentId / codebase= MCP parameters, not for
filesystem paths. The mount points use the SLOT hostname; the
codebase name is for MCP only.

### Fix shape

Extend the existing teaching at
`refinement/synthesis_workflow.md:30` to cover the dual nature:

> **Codebase name vs slot hostname (filesystem AND MCP).** The bare
> codebase name (`api`/`app`/`worker`) is the MCP parameter:
> `codebase=`, `fragmentId=codebase/<host>/...`. The slot hostname
> (`apidev`/`apistage` etc.) is the filesystem mount: SSHFS mounts
> live at `/var/www/<slot>dev` (and `<slot>stage` is the deployable).
> When you `ls` / `cat` source files, use the slot hostname; when
> you `record-fragment` / `complete-phase`, use the bare codebase
> name.

LOW priority; one-shot Bash recovery.

## Verification predicates

Each fix has a brief edit + one verification path. The collective verify
runs at run-33 dispatch:

| Fix | Brief edit predicate | Run-33 dispatch verify |
|-----|---------------------|------------------------|
| F-47 | `synthesis_workflow.md` carries the 18-verb + 12-observable lists verbatim from `slot_shape.go` and a stem-self-check rule + BAD/GOOD example | KB-stem record-fragment thrash drops to ≤2 retries per agent (vs run-32's 7) |
| F-48 | `scaffold/content_authoring.md` (+ slim + feature variants) carry the complete-batch-before-retry rule | Scaffold close-phase round-trips drop to ≤2 per codebase (vs run-32's 4-5) |
| F-49 | New `principles/codebase-name-vs-slot-hostname.md` exists and is included by 4 composer atom-lists | Zero `unknown codebase` / `unknown fragmentId` engine errors on first call |
| F-50 | `per_tier_authoring.md` has a top-level "Surface caps" + "Forbidden tokens" section | Zero one-shot env-content cap rejections |
| F-52 | Scaffold + feature briefs that teach git commit shape carry the `git status --porcelain` pre-check | Zero "nothing to commit" git-commit failures cancelling parallel tool calls |
| F-53 | `briefs_subagent_prompt.go::recipeContext` carries the recipe-authoring MCP scope note | Zero `zerops_workflow workflow=develop` invocations from sub-agents |
| F-54 | `refinement/synthesis_workflow.md:30` extended to cover filesystem-vs-MCP duality | Zero wrong-path `ls` calls in refinement |

## Order of edits + landing strategy

- 7 fixes (F-47, F-48, F-49, F-50, F-52, F-53, F-54). F-51 dropped.
- All edits brief-side. F-49 also adds one composer-side line per
  brief that includes the new principle (4 single-line additions to
  atom-include lists). No new tests.
- Land all 7 in one commit titled `recipe: run-32 iteration-cost fixes
  — brief-side teaching parity (F-47, F-48, F-49, F-50, F-52, F-53, F-54)`.
- Validate by running `go test ./... -short` and `make lint-local`.
- The atom-tree gate applies to the new `principles/codebase-name-vs-slot-hostname.md`
  atom — confirm it lints cleanly under `internal/content/atoms_lint.go`
  rules (most likely it's brief-side `principles/` not atom-corpus,
  but verify).

## Out of scope for this spec

- F-46 (multi-file dispatch self-attestation gate) is in the run-32
  ANALYSIS as a MEDIUM follow-up; that's an engine change tracked
  separately.
- Brief-vs-validator drift detection — adding a lint that asserts
  `synthesis_workflow.md` contains every verb the regex matches.
  Useful but a separate maintenance investment; not blocking.

## Summary

Seven brief-side fixes after codex review (one dropped for insufficient
evidence, one promoted from LOW to MEDIUM):

- **HIGH (2)**: F-47 cc-worker KB-stem self-check vocabulary, F-48
  scaffold batch-fix discipline.
- **MEDIUM (3)**: F-49 hostname-vs-bare-name shared principle, F-50
  env-content cap visibility, F-53 recipe-MCP scope clarity.
- **LOW (2)**: F-52 git-status pre-check, F-54 refinement filesystem-vs-MCP duality.

No engine code changes. F-49 adds one shared `principles/*.md`
referenced by 4 composer atom-lists; everything else is brief-text
edits. Collective effect on run-33 is ~5-8 minutes of wall-time saved
and ~20 fewer wasted tool calls — below the F-track predicate radar
but visible in agent-discipline log traces.
