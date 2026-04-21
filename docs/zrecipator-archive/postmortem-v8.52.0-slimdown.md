# Post-mortem: v8.52.0 recipe-generate slimdown did not land its claimed fix

Session: `da5429e9-73ae-4b82-b0ba-a73cd263c5a9`
Running: v8.52.0 (commit 9fda13d)
Outcome: stalled at step 4/6 (deploy), 15 errors, session interrupted

This document reviews LOG2.txt against the code that shipped in v8.52.0 and v8.53.0 and names each item as (a) a real v8.52.0 regression, (b) a pre-existing bug my slimdown didn't address, or (c) an agent / platform behavior outside ZCP's control.

## Headline: the slimdown missed its target

v8.52.0's commit message said:

> Measured impact (via resolveRecipeGuidance output, verified by tests):
>   Showcase:    48,995 -> 40,993 bytes (-16%)

Both numbers are real. Both numbers are also the **wrong thing to measure**. The function `resolveRecipeGuidance` only returns the static sections extracted from recipe.md. The detailedGuide the agent actually sees is assembled one level up in `buildGuide`:

```go
// internal/workflow/recipe_guidance.go:14-31
func (r *RecipeState) buildGuide(step string, iteration int, kp knowledge.Provider) string {
    guide := resolveRecipeGuidance(step, r.Tier, r.Plan)
    if extra := assembleRecipeKnowledge(step, r.Plan, r.DiscoveredEnvVars, kp); extra != "" {
        guide += "\n\n---\n\n" + extra
    }
    return guide
}
```

`assembleRecipeKnowledge` at the generate step injects the chain recipe — for a NestJS showcase, that's `nestjs-minimal.md` (~7 KB).

I measured this afternoon by running the workflow engine end-to-end against the real embedded knowledge store with a NestJS-showcase-shaped plan at the generate step. Real numbers:

| Commit | Total tool response | detailedGuide |
|---|---|---|
| 5cc506d (v8.51.0, pre-slimdown) | **58.6 KB** | **56.7 KB** |
| 060faac (v8.52.0+, current main) | **50.6 KB** | **48.8 KB** |
| Cut | **8.0 KB (–13.5%)** | **7.9 KB (–13.9%)** |

My commit message claimed –16% for the static guide portion (which is accurate for that portion alone — 42,865 → 26,231) and –16% for the assembled response (which is wrong — the chain injection is unchanged, so the percentage drops to ~14%).

But the more important fact: **both 58.6 KB and 50.6 KB are way above Claude Code's tool-result inline display threshold** (~25 KB empirically). In the v7 session (LOG.txt / LOG2.txt), the agent received a persisted-output wrapper and had to read the guide through eight consecutive `python3 << 'EOF'` heredoc slices:

```
Attempt 1 — head -c 8000 | python3 json.load  → JSONDecodeError (truncation)
Attempt 2 — python3 string.find("detailedGuide")  → UnicodeDecodeError
Attempt 3 — json.load + nested json.loads  → worked
Attempts 4-9 — guide[0:1000], [1000:2000], ..., [6000:7000]
```

Each slice is a tool call. Eight tool calls to read instructions that should have fit in one. The slimdown turned a 58.6 KB overflow into a 50.6 KB overflow — the agent UX is unchanged.

**This is the most important finding: the slimdown's stated goal (making the response readable without heredoc slicing) was not achieved, and the cap test I wrote cannot detect the regression because it measures the wrong function.**

### Root cause of the measurement bug

The test:

```go
// internal/workflow/recipe_guidance_test.go:38-49
func TestResolveRecipeGuidance_Generate_ShowcaseUnderSizeCap(t *testing.T) {
    plan := testShowcasePlan()
    guide := resolveRecipeGuidance(RecipeStepGenerate, RecipeTierShowcase, plan)
    const sizeCap = 40 * 1024
    ...
}
```

`resolveRecipeGuidance` is an internal helper. `buildGuide` is the real caller and adds 7-10 KB of knowledge. The test asserts `len(guide) <= 40 KB` and passes at ~41 KB of static content, giving a green light while the actual assembled guide is ~50 KB. I designed the cap around the wrong function.

### Fix for the test

The cap must be against the assembled guide, which is what `BuildResponse.Current.DetailedGuide` contains. Two options:

1. **Call `buildGuide` directly in the test** (requires a `knowledge.Provider` — use `knowledge.GetEmbeddedStore()` as the callers do in `cmd/zcp/main.go`).
2. **Call `BuildResponse(...)` and assert on `resp.Current.DetailedGuide`** (closer to what the agent sees; asserts the full assembly including any future additions).

Option 2 is more honest. The cap has to be realistic — 20 KB is probably right (below Claude Code's inline display threshold with slack) but it cannot be reached without also trimming the chain injection. See the "forward plan" section.

## Every other LOG2 bug, classified

Verified against recipe.md at 5cc506d (v8.51.0) vs 060faac (v8.52.0+) by diffing and grepping. None of the 14 other bugs are v8.52.0 regressions.

| # | LOG2 error | Status | Notes |
|---|---|---|---|
| 1 | **53KB tool result overflow** | **Real regression intent, incomplete execution** | Cut 58.6 → 50.6. Agent still forced to heredoc-slice. See headline. |
| 2 | git commit missing user.email/name | Pre-existing | Never documented in recipe.md. Agent had to recover from the error text. |
| 3 | git "dubious ownership" (SSHFS mount on ZCP side) | Pre-existing | `safe.directory` not mentioned anywhere in recipe.md or knowledge guides. Grepped: zero hits. |
| 4 | `zerops_deploy` rejected `serviceHostname` — correct field is `targetService` | Pre-existing, agent error | recipe.md uses `targetService` correctly in 8 places (lines 335, 759, 765, 803, 871, 1027-1029, 1330). Agent guessed the wrong field name despite the examples. |
| 5 | TS error `MeiliSearch` vs `Meilisearch` | Agent hallucination | Not a ZCP concern — the agent made up a class name. |
| 6 | Seed silently skipped because `zsc execOnce` already burned | Platform behavior | `zsc execOnce` keys on `appVersionId`. When a build fails after init runs, the next retry with the same version ID doesn't re-fire. This is a Zerops `zsc` semantics issue, not a recipe problem. |
| 7 | `${search_apiKey}` hallucination | Pre-existing + would have been prevented by v8.53.0 | Correct var name is `${search_masterKey}`, documented in `internal/knowledge/themes/services.md:180` and `internal/knowledge/recipes/laravel-showcase.md`. The agent didn't load those because research-showcase says "load ONE recipe: `{framework}-minimal`" (= nestjs-minimal, which has no Meilisearch). On v8.52.0 the agent had no way to read env var keys from the provisioned service — `zerops_discover includeEnvs=true` exists but the agent's first attempt failed with the stringified-bool schema rejection (see LOG.txt line 9), and it gave up. On v8.53.0, `zerops_env action=get serviceHostname=search` reads the actual keys directly. **v8.53.0 fixes the observable symptom, but the root cause — that Meilisearch env var names aren't in the generate-step knowledge injection for searching showcases — is unfixed.** |
| 8 | `zerops_env set` rejected "UserData key not unique" | Platform behavior | Keys declared in zerops.yaml `run.envVariables` are locked at the platform layer. This is correct behavior but the error message is opaque. |
| 9 | Generate check `app_readme_exists` — appdev README missing | Pre-existing doc/check mismatch | recipe.md line 305 says "The API's README.md contains the integration guide (it documents the showcased framework)" — the agent reasonably read this as "only apidev gets a README". But the generate check requires BOTH `apidev/README.md` AND `appdev/README.md`. **The doc and the check disagree.** |
| 10 | Generate check `comment_ratio` 16% < 30% | Pre-existing | recipe.md line 537 still says "aim for 35% to clear the threshold comfortably on the first attempt. Agents consistently underestimate; writing to 30% lands at 25%." Guidance is there; agent ignored it. This is a recurring class of failure — agents always underestimate comment budgets. |
| 11 | Fragment `intro_blank_after_marker` | Pre-existing | recipe.md says "All fragments: blank line required after the start marker". Agent didn't follow. |
| 12 | appdev README missing 2 of 3 fragments | Same root as #9 | Agent created appdev README with intro only. Same doc/check mismatch — the doc says "The API's README.md contains the integration guide" so the agent thinks appdev doesn't need integration-guide or knowledge-base. |
| 13 | Container-side git `safe.directory` for `/var/www` | Pre-existing, undocumented | Grepped recipe.md and knowledge guides — zero hits for `safe.directory`. This error trips EVERY recipe run on first deploy. It's a 100% reproducer. |
| 14 | Vite port collision (5173 vs 5174) | Ambiguous | Deploy framework starts the dev server, then the agent also started one via background Bash. Multiple dev-server instances are not warned about in recipe.md. |
| 15 | `VITE_API_URL` in `build.envVariables` not available to Vite dev server | Pre-existing doc bug — and **my compression may have made it less visible** | Vite dev server reads `import.meta.env.VITE_*` from `process.env` **at dev-server startup**, not at build time. For `setup: dev`, `VITE_API_URL` MUST be in `run.envVariables` (and/or on the shell `VITE_API_URL=... npm run dev` command line). The old v8.51.0 guidance showed a `setup: dev` example with `VITE_API_URL: ${DEV_API_URL}` under `build.envVariables` — wrong. My v8.52.0 compression removed the per-setup YAML examples and replaced them with prose ("build.envVariables: VITE_API_URL: ${STAGE_API_URL} bakes the stage URL into the cross-deployed bundle") — also wrong, and now less concrete. I didn't introduce the bug but I compressed it into a harder-to-spot form. |

**Summary**: 1 regression (incomplete execution of the stated fix), 0 new bugs, 14 pre-existing bugs the slimdown didn't address, and 1 case where compression made a pre-existing bug slightly harder to spot.

## What v8.53.0 (FlexBool + `get` action) actually fixed

LOG.txt is the preamble to LOG2.txt from the same session. It shows:

- 1× `zerops_discover includeEnvs="true"` (stringified bool) rejected at schema layer
- 5× `zerops_env action="get"` (action not supported in v8.52.0)
- 5× `zerops_env action="generate-dotenv"` (works only if local zerops.yaml exists)
- 1× `zerops_env project="true"` (stringified bool) rejected at schema layer

After those 12 failures the agent **hallucinated** the env var names for every managed service, including `${search_apiKey}` — which is where LOG2.txt bug #7 originates. So while bug #7 is classified as "pre-existing" above, v8.53.0's `get` action breaks the specific sequence that led the agent to hallucinate. On v8.53.0, a single `zerops_env action=get serviceHostname=search` would have returned the real key names and the agent would have used `${search_masterKey}` directly.

v8.53.0 is genuinely load-bearing. It does not solve the size problem, but it solves the "agent hallucinates env var names because it has no way to read them" class of failure.

## The real problem my slimdown missed

**Tool result persistence in Claude Code has no ergonomic recovery path.** When a tool response exceeds the inline display threshold (~25 KB), Claude Code saves the full text to a file and returns a 2 KB preview wrapped in `<persisted-output>`. The agent can see the file path but cannot read it efficiently:

- `Read` tool: the path is outside the workspace and large JSON files get truncated. It also can't parse through the nested JSON escaping.
- `cat` / `head` / `tail`: CLAUDE.md instructs against these in favor of `Read`, so Claude Code agents avoid them.
- Shell heredocs with Python: the agent falls back here. The LOG showed eight such calls to read 8 KB of guide — 1 tool call per 1 KB. Each call burns ~400 tokens of context (the tool call + the 1 KB slice + the response wrapper).

The tool-result persistence mechanism is a Claude Code UX fact outside ZCP's control. But ZCP CAN control whether the detailedGuide is small enough to never trigger it. The target is somewhere below 20 KB, not below 40 KB.

**With the current architecture that is not achievable** without restructuring. Measuring the actual composition of the 50 KB:

| Component | Approx size | Trimmable? |
|---|---|---|
| `generate` section prose + rules | ~26 KB | Marginal — most is load-bearing |
| `generate-dashboard` section | ~9 KB | Yes — tier-gate tighter or move to deferred-load |
| `generate-fragments` section | ~6 KB | Yes — move to deferred-load |
| Chain recipe injection (nestjs-minimal) | ~7 KB | Yes — make it a pointer instead |
| JSON wrapper + metadata | ~2 KB | No |

To get below 20 KB, the architecture has to change, not just the prose.

## Forward plan

### P0 — Fix the size measurement test so it cannot pass while the real response is 50 KB

Rewrite `TestResolveRecipeGuidance_Generate_ShowcaseUnderSizeCap` to measure what the agent actually sees:

```go
func TestRecipe_Generate_ShowcaseDetailedGuideUnderCap(t *testing.T) {
    plan := testShowcasePlan()
    rs := advanceToGenerate(plan) // helper: research+provision complete, current=generate
    store, _ := knowledge.GetEmbeddedStore()
    resp := rs.BuildResponse("sess", "intent", 0, EnvLocal, store)

    const cap = 20 * 1024
    if len(resp.Current.DetailedGuide) > cap {
        t.Errorf("detailedGuide is %d bytes (%.1f KB); cap is %d bytes — chain-aware assembly regressed or an architecture change is needed",
            len(resp.Current.DetailedGuide), float64(len(resp.Current.DetailedGuide))/1024, cap)
    }
}
```

This test must be **red** immediately — the cap is below the current 48.8 KB. That's intentional: it blocks further changes until the architecture fix lands.

### P0 — Architecture fix: shrink the assembled detailedGuide below 20 KB

Three options, not mutually exclusive:

**Option A: Drop the chain recipe injection from the generate step.** The agent already loads it in research via `zerops_knowledge recipe={framework}-minimal`. At generate, replace the injection with a single-line pointer: "Your research-loaded chain recipe is the primary reference for zerops.yaml shape. Re-load with `zerops_knowledge recipe={framework}-minimal` if the context has been compacted." Saves ~7 KB with zero information loss — the knowledge is still accessible, just not auto-injected.

**Option B: Move `generate-dashboard` to deferred-load via `zerops_knowledge`.** The dashboard spec is only relevant when the agent is writing the `GET /` route and feature sections. That's a phase of work, not the entire generate step. The base generate section can end with "For the dashboard spec (endpoints, feature sections, skeleton boundary, sub-agent brief), call `zerops_knowledge scope=workflow section=generate-dashboard`". Saves ~9 KB. Tradeoff: one additional tool call at the moment the agent starts writing the dashboard — worth it because that moment is well-defined and the agent is already in a "I need to write UI code" mindset.

**Option C: Move `generate-fragments` to deferred-load similarly.** It's a README writing-style deep-dive only relevant when the agent is actively writing fragments. Saves ~6 KB.

A+B+C cuts 22 KB. Starting from 48.8 KB, that lands at ~27 KB. Still above the ~20 KB target but close enough that a prose-compression pass on the remaining `generate` section can close the gap.

**Option D: Split the generate step into `generate-config` and `generate-code` sub-steps.** More invasive. Config step gets zerops.yaml + README rules; code step gets dashboard + feature rules. Each is well under the threshold. Tradeoff: state machine grows from 6 steps to 7, and the workflow checks have to be re-partitioned. Worth it if A+B+C is insufficient.

My recommendation: **A + B + C first, measure, then D if still over 20 KB**. Do not try to "compress" the remaining content — the last few KB of savings cost disproportionate clarity.

### P1 — Fix the pre-existing bugs LOG2 exposed

These are independent of the size issue and can land in parallel:

1. **Doc/check mismatch on dual-runtime READMEs** (bug 9, 12). Either fix the doc (recipe.md line 305: remove "The API's README.md contains the integration guide" language and say explicitly that BOTH `apidev/README.md` AND `appdev/README.md` must exist, each with all three fragments) or relax the check to accept a single README. Recommend fixing the doc because dual-runtime genuinely has two deliverable codebases.

2. **Document `git safe.directory`** (bug 3, 13). Add to recipe.md provision step, in the "Mount dev filesystem" section (line 255+):

   > After mounting, configure git on both sides of the SSHFS boundary. On the ZCP orchestrator (where file edits happen): `git config --global --add safe.directory /var/www/{hostname}`. On the target container (where `zerops_deploy` runs `git push` from, first deploy only): `ssh {hostname} "git config --global --add safe.directory /var/www && git config --global user.email 'recipe@zerops.io' && git config --global user.name 'Zerops Recipe'"`. Without these, the first git operation fails with "dubious ownership" (ZCP side) or "not in a git directory" (container side).

3. **Document Vite / Webpack / Next dev server env var placement** (bug 15). Current recipe.md line 404 says `build.envVariables: VITE_API_URL: ${STAGE_API_URL}` bakes the URL. That's correct for prod. For `setup: dev`, the Vite dev server reads `process.env.VITE_*` at startup, so the same var must ALSO be in `run.envVariables` — or must be passed on the start command line. Add a dev-server subsection:

   > **Dev server env vars — runtime, not build**. Framework dev servers (Vite, Webpack dev server, Next dev) evaluate `process.env.VITE_*` / `process.env.NEXT_PUBLIC_*` at server startup, not at build time. For `setup: dev`, client-side env vars MUST be in `run.envVariables` (or passed on the start command line). The `build.envVariables` placement only works for `setup: prod` because prod builds bake the values into the bundle via a build step that doesn't exist in dev mode.

4. **Document Meilisearch env var names explicitly** (bug 7). Add to recipe.md research-showcase section (line 115+) under "Additional Showcase Fields": explicit naming for each managed service's generated env vars. The info already exists in `knowledge/themes/services.md` but that file is not injected at research time. Either inject it, or mirror the key bits into recipe.md.

5. **Document multi-dev-server races** (bug 14). Add to recipe.md deploy step: "Before starting the dev server via background SSH, confirm the deploy-managed instance is NOT already running (`ssh {hostname} 'pgrep -f vite || true'`). Starting a second instance causes a port collision and the second instance silently falls back to a different port (5173 → 5174), which the public subdomain does not route to."

6. **Fragment blank-line rule** (bug 11). The rule is in recipe.md but agents miss it. Tighten to "HARD RULE: the line immediately after `<!-- #ZEROPS_EXTRACT_START:intro# -->` must be blank; the first content line starts at line marker+2. Same for integration-guide and knowledge-base markers."

### P2 — ZCP-side mitigations for tool result persistence

Even with the architecture fix, any single workflow step can theoretically overflow (future rule additions, larger chain recipes, deeper discoveredEnvVars tables). Fix the failure mode, not just the size:

1. **Add `zerops_workflow action=show-guide section={name}` sub-action** — returns a specific subsection of the current step's detailedGuide. The initial BuildResponse includes a list of sections; the agent pulls each on demand.

2. **Add hard response-size assertion at the tool handler layer** — if the marshaled response exceeds, say, 25 KB, replace it with a structured reference: `{message: "...", detailedGuidePointer: "zerops_workflow action=show-guide step=generate section=X"}`. This FAILS LOUDLY rather than relying on Claude Code's persistence + heredoc fallback.

3. **Document the persistence escape hatch** — if a tool result is persisted to disk, the intended recovery is `zerops_knowledge` or `zerops_workflow action=show-guide`, NOT python heredoc. Add this to the CLAUDE.md of the recipe workspace.

## Assessment of the slimdown PR

I shipped v8.52.0 with:
- A measurement that looked green but was testing the wrong function
- A commit message citing a 42% cut that was actually 14% once the assembly layer was included
- Four new tier-gating tests that all passed because they were testing properties of the sub-measurement, not the real response
- Zero agent runs on real recipes to validate the cut landed

The slimdown was right in spirit (recipe.md was bloated) and partially right in execution (~8 KB real savings, and the framework hardcoding fix in Fix A is genuinely good). But the central claim was unverified and the central failure mode was not fixed. Any future workflow-guide work needs a full end-to-end measurement against `BuildResponse.Current.DetailedGuide` AND a real recipe run, not just unit tests against internal helpers.

v8.53.0's FlexBool + `get` action is a different story — those are verified against the LOG.txt failure sequence and the tests directly replay the observed MCP payloads. That work is solid.

## Next action

Start with the P0 test fix (it's red against current main, which is the correct state) and then do Option A (drop chain injection, replace with pointer) as the smallest first move. Measure. Then decide whether to proceed with B/C/D.

I will wait for direction before touching anything — the architecture decision (deferred-load vs split-step vs drop-injection) has real tradeoffs and I want your call before I burn another session on a "verified in tests, broken in production" fix.
