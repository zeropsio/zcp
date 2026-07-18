# Recipe content-quality iteration — handoff for fresh Claude Code session

**Paste this entire file into a fresh Claude Code session at `/Users/fxck/www/zcp`. The instance reading this drives Steps 2 and 3 until the recipe content is above the human-golden bar.**

---

## Context (read first — don't skim)

We're fixing the recipe authoring engine `zcprecipator3`. The most recent dogfood run (run-32) shipped recipe content the user identified as "absolute shit" on eyeball read despite the engine's own validators passing. After analysis we identified **11 specific patterns** of content-quality drift, mapped each to the brief that teaches it, and edited the briefs to fix them.

**The previous session dispatched a subagent to make all 11 brief edits in place; you are picking up after that.** Verify by:

```bash
cd /Users/fxck/www/zcp
git status   # should show modified files under internal/recipe/content/briefs/
git diff --stat   # roughly 6-12 files changed, no commits
```

If git status shows clean working tree, the subagent didn't run / changes were committed already / you're in the wrong repo. Ask the user before continuing.

## The bar (this is critical)

**The goldens are ORIENTATION, not CEILING:**

- `/Users/fxck/www/laravel-jetstream-app/{README.md,zerops.yaml}` — human golden, primary reference
- `/Users/fxck/www/recipes/laravel-jetstream/{README.md,*/import.yaml}` — recipes-side reference

Jetstream is human-written, not perfect. The user is aiming **above** jetstream's bar — tighter scope, sharper structure, every word earning its place.

**The actual target: a porter who clones this recipe's apps repo gets a working starting point for their own app on Zerops, with content that:**

1. Helps them understand WHAT they're looking at (intro = standalone-app description, framework + capability)
2. Tells them WHAT THE PLATFORM REQUIRES to deploy (IG = platform-forced integration steps; not conventions, not framework tutorials)
3. Names the TRAPS specific to this framework on this platform (KB = platform×framework intersection, with real URLs to docs when relevant)
4. Justifies the YAML decisions THIS recipe made (yaml comments = decision rationale, not field documentation)

**What's NOT a recipe's job:**
- Framework tutorial (the framework's own docs do that)
- Platform tutorial (Zerops docs do that)
- Coding-style guide (a recipe is a starting point, not a style manual)
- Cross-language reference (a NestJS recipe doesn't teach Python adapt-paths)

When the judges score, they ask: "would a porter who clones this find it useful, or wading through padding to find what they need?" Match jetstream's tightness as a floor, exceed it on substance.

## The 11 patterns we fixed (judge against these)

| # | Pattern | What's wrong | What good looks like |
|---|---------|--------------|----------------------|
| 1 | Tier name-drops on codebase surfaces | "tier 0", "HA tier" mentioned in `apidev/README.md` or `apidev/zerops.yaml` | Codebase surfaces don't mention tiers. Tier vocabulary belongs to env-content surfaces only. |
| 2 | Recipe-engine corpus slug names leak into porter content | Run-32 references `init-commands`, `env-var-model`, `managed-services-nats`, etc. — internal zerops_knowledge slug IDs. Porter never interacts with the knowledge corpus; these names are recipe-authoring vocabulary, not porter docs. | **Inline Zerops platform knowledge** in-body (no redirect). **Link to FRAMEWORK docs** with real URLs (e.g. `[Laravel documentation](https://laravel.com/docs/...)` — what jetstream golden does). **Internal slug names** (`init-commands`, `env-var-model`, etc.) NEVER appear in porter-facing content. The previous brief edit replaced "the X guide on Zerops docs covers Y" with "[X](url) covers Y" — that's still slug-name leakage with a URL bolted on. The deeper rule: porter content doesn't reference Zerops's internal knowledge organization at all. |
| 3 | KB as flat bullets always | Substantial mechanism stories crammed into one-line `- **Stem** —` bullets | KB shape matches substance: short trap → flat bullet; multi-paragraph mechanism + porter recovery → H3 sub-heading + paragraphs + optional callout `> [!CAUTION]` block + optional shell example |
| 4 | yaml comment over-density (56-63%) | Every directive gets a 4-7 line essay defense | ~35% comment density. Comment the non-obvious. If the YAML is obvious, the comment is noise. |
| 5 | IG items are conventions, not platform-forced | "Alias cross-service envs under your own keys" as IG #4 (it's a convention; deploy works without it) | Each IG item teaches what the platform REQUIRES. If the porter could ignore your IG #N and the deploy still works, push to yaml comment or KB. |
| 6 | Intro leaks recipe-internal wiring | "Mounts under /api; JWT-ready via JWT_SECRET" / "Owns the items schema, worker owns audit_log schema" | Intro describes the standalone app — framework + capability. No mount path, no env-var alias names, no port number, no inter-codebase coordination. |
| 7 | Cross-language adapt-paths in NestJS recipe | IG #2 lists Python uvicorn calls, Go http.ListenAndServe in a NestJS recipe | Adapt-paths stay within the codebase's language family. NestJS → Node only (Express, Fastify). Laravel → PHP only. |
| 8 | Service-type/runtime-base mismatch (FACTUALITY) | Stage `import.yaml` declares app `type: nodejs@22` but app's prod runtime is `base: static` | Engine derives `services[].type` from corresponding codebase's prod `run.base`. This is a factuality bug. |
| 9 | Root taxonomy drift | "Include Coding Agents" / "Include Cloud IDE" instead of canonical names | Canonical: "AI agent", "Remote (CDE)", "Local", "Stage", "Small Production", "Highly-available Production". |
| 10 | IG without concrete repo-file anchors | Generic prose IG steps; no source-file links | IG items link to specific files (`composer.json`, `vite.config.ts`, `app.module.ts`) using markdown links. |
| 11 | Missing concept-bridge between IG and KB | apidev README jumps straight from IG end to KB markers | After last IG item, include `## Understand Zerops Core Concepts` linking to the framework's Zerops tutorial. |

## Step 2 — Judge the BRIEF (do this first)

**Goal:** confirm the brief edits actually teach the corrected rules. Cheap pre-check before paying the cost of agent dispatches.

```bash
cd /Users/fxck/www/zcp
go build -o /tmp/zcp-recipe-sim ./cmd/zcp-recipe-sim
go test ./internal/recipe/... -short   # must pass
make lint-local   # must pass
/tmp/zcp-recipe-sim emit -run docs/zcprecipator3/runs/32 -out /tmp/sim-step2
```

Then dispatch ONE judge subagent with this prompt (use Agent tool, `subagent_type=general-purpose`, `model=opus`):

```
You are a senior engineer reviewing a recipe-authoring brief to check whether it teaches the corrected rules. The brief gets handed to sub-agents who author per-codebase recipe content. If the brief teaches the wrong shape, the output is wrong by construction.

BRIEFS TO READ (end-to-end, don't skim):
- /tmp/sim-step2/briefs/api-prompt.md
- /tmp/sim-step2/briefs/app-prompt.md
- /tmp/sim-step2/briefs/worker-prompt.md
- /tmp/sim-step2/briefs/env-prompt.md

GOLDEN REFERENCES (orientation, not ceiling — recipes should AIM ABOVE this bar):
- /Users/fxck/www/laravel-jetstream-app/{README.md,zerops.yaml}
- /Users/fxck/www/recipes/laravel-jetstream/{README.md,3 — Stage/import.yaml}

THE 11 PATTERNS the brief is supposed to teach against, with corrected teaching:

[paste the table above into the dispatch]

For each of the 11 patterns:
1. Find where in the brief the corrected teaching should land.
2. Read that section. Does it actually teach the corrected rule, or does it still teach the old defective shape?
3. Are the worked examples in the brief consistent with the corrected rule, or do they contradict it?

**Pattern #2 specific check (likely residual defect):** the prior brief fix kept the shape "[`<slug>`](url) covers Y" — which is still slug-name leakage with a URL bolted on. The CORRECTED rule is: no internal corpus slug names (`init-commands`, `env-var-model`, `managed-services-nats`, `rolling-deploys`, etc.) in porter content at all. Either inline the platform knowledge in-body, OR link to FRAMEWORK docs (laravel.com, expressjs.com, vitejs.dev, etc.). Check the brief AND the rubric Criterion 3 anchors AND the citation atom for residual slug-name shape; flag any instance where a slug ID appears as porter-facing content.

Also report:
- Anything muddled, contradictory, or missing in the brief that would steer an agent wrong.
- Any defect NOT in the 11 patterns that you'd notice as a senior engineer reading this brief.

OUTPUT format: per-pattern Y/N (does the brief teach the corrected rule) + evidence (file:line in brief). Then a "Other findings" section. Be terse but specific. Under 1500 words.

CRITICAL: judge by INTELLIGENCE, not grep. The agent might have renamed "guide on Zerops docs covers" to "Zerops's X reference covers" — same defect, different surface. You're checking if the BRIEF TEACHES THE RIGHT THING in the right shape, not if specific words appear or don't appear.
```

**Step 2 outcomes:**
- Judge says all 11 patterns Y → move to Step 3.
- Judge says any pattern N → identify which brief atom owns the defect, edit the brief in `internal/recipe/content/briefs/`, re-run sim emit, re-judge. Iterate until clean.
- Judge surfaces a NEW defect → decide: is this fixable with a brief edit (do it), or does it need engine work (escalate to user)?

## Step 3 — Full sim + judge the OUTPUT

**Only run Step 3 after Step 2 says briefs look right.**

```bash
cd /Users/fxck/www/zcp

# Re-emit if you made brief changes during Step 2 iteration:
/tmp/zcp-recipe-sim emit -run docs/zcprecipator3/runs/32 -out /tmp/sim-step3

# Now dispatch the per-codebase + env sub-agents. Run all 4 in parallel
# (single message with 4 Agent tool calls):
```

For each `<host>` in `[api, app, worker]` and for `env`, dispatch with `subagent_type=general-purpose`, `model=opus`:

```
You are the recipe-authoring sub-agent for codebase <host> (or env).
Read /tmp/sim-step3/briefs/<host>-prompt.md end-to-end and follow its replay-adapter instructions verbatim.

The brief tells you to write fragments under a specific directory; write to
/tmp/sim-step3/fragments-new/<host>/ per the adapter.

Author every fragment the brief lists. Don't commit, don't run tests; just write the fragment files.
Report back when done.
```

After all 4 dispatches return:

```bash
/tmp/zcp-recipe-sim stitch -dir /tmp/sim-step3
/tmp/zcp-recipe-sim emit-finalize -dir /tmp/sim-step3
# Dispatch finalize sub-agent against /tmp/sim-step3/briefs/finalize-prompt.md (same shape as above)
/tmp/zcp-recipe-sim emit-refinement -dir /tmp/sim-step3
# Dispatch refinement sub-agent against /tmp/sim-step3/briefs/refinement-prompt.md
/tmp/zcp-recipe-sim stitch -dir /tmp/sim-step3
/tmp/zcp-recipe-sim validate -dir /tmp/sim-step3
```

Then dispatch ONE judge with this prompt (Agent, `subagent_type=general-purpose`, `model=opus`):

```
You are a senior engineer reading recipe content to find what's below the bar. The author claims this recipe is a deployable starting point a porter clones to launch their own app on Zerops.

CANDIDATE (the recipe under review — read end-to-end):
- /tmp/sim-step3/apidev/{README.md,zerops.yaml,CLAUDE.md}
- /tmp/sim-step3/appdev/{README.md,zerops.yaml,CLAUDE.md}
- /tmp/sim-step3/workerdev/{README.md,zerops.yaml,CLAUDE.md}
- /tmp/sim-step3/environments/README.md
- /tmp/sim-step3/environments/{0 — AI Agent,1 — Remote (CDE),2 — Local,3 — Stage,4 — Small Production,5 — Highly-available Production}/{README.md,import.yaml}

GOLDEN (orientation, not ceiling — recipe should AIM ABOVE this bar):
- /Users/fxck/www/laravel-jetstream-app/{README.md,zerops.yaml,CLAUDE.md}
- /Users/fxck/www/recipes/laravel-jetstream/{README.md,*/import.yaml}

THE BAR — what makes recipe content good:
1. Intro orients the porter: framework + capability. NOT mount path, env-var aliases, port, schema-ownership coordination.
2. IG teaches what the PLATFORM REQUIRES. Not conventions ("alias your own keys"), not framework tutorials, not cross-language adapt-paths.
3. KB names framework×platform traps. Real URLs when redirecting; complete teaching when not. Shape matches substance (short trap → flat bullet; multi-paragraph mechanism + recovery → H3 sub-heading + callout block).
4. yaml comments justify decisions THIS recipe made. Not field documentation. Not paragraph defenses. ~35% comment density.
5. Tier name-drops on codebase surfaces are wrong (tiers are env-content territory).
6. Root README uses canonical taxonomy ("AI agent", "Remote (CDE)", etc.).
7. import.yaml service `type` matches the codebase's prod runtime base (factuality).
8. Concept bridge `## Understand Zerops Core Concepts` between IG and KB.

YOUR JOB: read both end-to-end, find what's wrong with the candidate vs the bar.

For each defect:
- candidate file:line + golden file:line counter-evidence (or "no counter-evidence; rule:<which one>")
- defect description in plain English
- severity: critical (factuality bug, would mislead porter), major (style or scope drift visible to any reader), minor (polish)

**Specific defect to check (HIGH priority — likely residual from prior brief fix):**
Recipe-engine internal slug names (`init-commands`, `env-var-model`, `managed-services-nats`, `rolling-deploys`, `cross-service-refs`, `object-storage`, etc.) appearing in porter content. These are zerops_knowledge corpus IDs the recipe-authoring engine uses — porters never interact with that corpus. Even with a markdown link `[init-commands](url)`, the slug name itself is leakage. Correct shape: inline the platform knowledge in-body, OR link to FRAMEWORK docs (laravel.com, expressjs.com, vitejs.dev, etc.) — like the jetstream golden does. Flag every instance.

Find anything else too. The 8 above are starting points, not the whole defect surface. Be terse but specific. Don't grade, don't score, don't issue grades — just enumerate defects.

Under 2000 words.
```

**Step 3 outcomes:**
- Judge finds zero meaningful defects → CONVERGED. Report to user with summary; user can commit the brief edits + run a fresh dogfood (run-33).
- Judge finds defects → identify the worst one, find the brief atom that owns it, edit the brief, loop back to Step 2 (judge the brief again before another full sim).

## Iteration discipline

**STOP and report to user if:**
- Step 2 judge surfaces a defect that requires engine work (not a brief edit) — escalate.
- Step 3 judge finds a NEW pattern that's hard to fix with brief edits — escalate.
- You hit iteration N=5 without convergence — calibration may be wrong; escalate.
- Any test or lint fails after a brief edit — fix it before continuing or escalate.
- You can't tell whether a defect is real or stylistic preference — escalate.

**Don't do:**
- Don't commit anything. Leave changes uncommitted; the user reviews + commits after convergence.
- Don't add validators in `internal/recipe/*.go` to bypass judgment. Catalog-drift forbidden by `system.md §4`.
- Don't grep-score. Both judges (Step 2 and Step 3) use intelligence, not pattern-counting.
- Don't iterate past N=5 silently. Surface progress to the user.
- Don't treat goldens as the ceiling. Aim ABOVE jetstream — tighter, sharper, every word earned.

## Iteration budget

| Iteration | Step 2 (brief judge) | Step 3 (output judge) | Wall time |
|-----------|---------------------|----------------------|-----------|
| 1 | ~10 min | ~30 min if Step 2 passes | ~40 min |
| 2 (if Step 2 failed iter 1) | ~10 min | ~30 min | ~40 min |
| ... | | | |
| Budget | 5 iterations max before escalation | | ~3.5 hours |

If converged < 3 iterations, the brief edits were sharp. If 4+ iterations needed, the calibration upstream (research doc / spec / rubric) probably needs work — escalate to user with specific findings.

## Final report

When you stop (converged or escalation), produce a final summary at `/tmp/sim-final-report.md`:
- Iteration count
- Per-iteration: which brief atoms edited, what changed, judge verdict
- Final per-pattern Y/N for the 11 patterns (did each drop into above-bar territory)
- Novel defects discovered + their resolution (brief edit / escalated / accepted)
- Recommendation: commit + dogfood, OR rethink calibration upstream
