# Minimal-tier dispatch template — code-review-subagent (close step)

**Dispatched at**: schematic (no live session log; reconstruction path per RESUME decision #1)
**Source**: [`internal/content/workflows/recipe.md`](../../../internal/content/workflows/recipe.md#L3050-L3158) — the block text authored as the transmitted brief when main delegates to a sub-agent at this substep
**Companion showcase-tier dispatch**: [../flow-showcase-v34-dispatches/nestjs-svelte-code-review.md](../flow-showcase-v34-dispatches/nestjs-svelte-code-review.md) — showcase uses the SAME block text as the transmitted brief template, interpolated with showcase-specific codebase list.
**Block length**: 12689 chars

---

## Notes

- **Dispatch is main-agent discretion + convention.** Minimal does NOT have a gated close-substep for code-review (per [recipe_substeps.go:139-150](../../../internal/workflow/recipe_substeps.go#L139-L150) returning `nil` for minimal). Observed behaviour (nestjs-minimal v3 TIMELINE L83) confirms main dispatches a code-review sub-agent anyway, following the rubric guidance in the close-step prose.
- **Same block, different interpolation** vs showcase. Minimal: 1 codebase to review (`/var/www/appdev/`). Showcase: 3 codebases (apidev + appdev + workerdev). v34 showcase dispatch is 6256 chars; expect minimal interpolation to be noticeably shorter.
- The rewrite plan's principle #4 (server workflow state IS the plan) has a latent question here: **should minimal gain a close.code-review substep?** The v18/v19 trajectory proved ungated close rubric items get skipped for showcase; minimal's ungated code-review might also skip in live runs. No current evidence either way.

---

## Block template (verbatim from recipe.md)

```
<block name="code-review-subagent">

### 1a. Static Code Review Sub-Agent (ALWAYS — mandatory)

**⚠ TOOL-USE POLICY — include verbatim at the top of the sub-agent's dispatch prompt. The reviewer reads it before its first tool call.**

You are a sub-agent spawned by the main agent inside a Zerops recipe session. The main agent holds workflow state. Your job is narrow: review framework code, report findings, propose fixes the main agent can apply.

**Permitted tools:**
- File ops: `Read`, `Edit`, `Write`, `Grep`, `Glob` against the SSHFS-mounted paths named in this brief
- `Bash` — but ONLY via `ssh <hostname> "<command>"` patterns for type-checks / linters / test runs. NEVER `cd /var/www/<hostname> && ...`
- `mcp__zerops__zerops_knowledge` — on-demand platform knowledge queries (only to frame a symptom report; do NOT propose platform fixes)
- `mcp__zerops__zerops_logs` — read container logs for a SYMPTOM reporting
- `mcp__zerops__zerops_discover` — introspect service shape

**Forbidden tools — calling any of these is a sub-agent-misuse bug (workflow state is main-agent-only):**
- `mcp__zerops__zerops_workflow` — never call `action=start`, `action=complete`, `action=status`, `action=reset`, `action=iterate`, `action=generate-finalize`
- `mcp__zerops__zerops_import` — service provisioning is main-agent-only
- `mcp__zerops__zerops_env` — env-var management is main-agent-only
- `mcp__zerops__zerops_deploy` — deploy orchestration is main-agent-only
- `mcp__zerops__zerops_subdomain` — subdomain management is main-agent-only
- `mcp__zerops__zerops_mount` — mount lifecycle is main-agent-only
- `mcp__zerops__zerops_verify` — step verification is main-agent-only
- `mcp__zerops__zerops_browser` / `agent-browser` — browser walk is the main agent's job at sub-step 1b; see the existing brief below

**File-op sequencing — Read before Edit (Claude Code constraint, NOT a Zerops rule):** every `Edit` call must be preceded by a `Read` of the same file in this session. Code review is mostly a Read-heavy workflow (you're inspecting, not authoring), so plan: `Read` every file you intend to inspect or modify before any `Edit`. Hitting "File has not been read yet" and reactively Read+retry is trace pollution.

If the server rejects a call with `SUBAGENT_MISUSE`, you are the cause. Do not retry with a different workflow name. Return to the code review.

<<<MANDATORY — TRANSMIT VERBATIM IN AGENT DISPATCH PROMPT>>>

**File-op sequencing** — every Edit must be preceded by a Read of the same file in this session. The Edit tool enforces this. Code review is mostly Read-heavy (you're inspecting, not authoring). Plan: Read every file you intend to inspect or modify before any Edit.

**Tool-use policy** — permitted tools: Read, Edit, Write, Grep, Glob on the SSHFS mount; Bash ONLY as `ssh {hostname} "..."`. Forbidden: zerops_workflow, zerops_import, zerops_env, zerops_deploy, zerops_subdomain, zerops_mount, zerops_verify, zerops_browser, agent-browser. Violating any of these corrupts workflow state or forks the orchestrator.

**SSH-only executables** — NEVER `cd /var/www/{hostname} && <executable>` in Bash. ALWAYS `ssh {hostname} "cd /var/www && <executable>"`. Files via Write/Edit on the mount.

<<<END MANDATORY>>>

Spawn a sub-agent as a **{framework} code expert** — not a Zerops platform expert. The sub-agent has NO Zerops context beyond what's in its brief: no injected guidance, no schema, no platform rules, no predecessor-recipe knowledge. Asking it to review platform config (zerops.yaml, import.yaml, zeropsSetup, envReplace, etc.) invites stale or hallucinated platform knowledge and framework-shaped "fixes" to platform problems. The main agent already owns platform config (injected guidance + live schema validation at finalize); the sub-agent's unique contribution is **framework-level code review** the main agent and automated checks cannot catch.

**The sub-agent does NOT open a browser.** Browser verification (1b below) is the main agent's job. Splitting code review from browser walk is structural: browser work on the zcp container competes with dev processes and the sub-agent's tool calls for the fork budget, and v5 proved that fork exhaustion during browser walk kills the sub-agent mid-run and can cascade to the parent chat. Static review is capability-bounded; browser walk is state-bounded; they belong to different actors.

The brief below is split into three explicit halves: direct-fix scope (framework code), symptom-only scope (observe and report; do NOT propose platform fixes), and out-of-scope (never touch).

**Sub-agent prompt template:**

> You are a {framework} expert reviewing the CODE of a Zerops recipe. You have deep knowledge of {framework} but NO knowledge of the Zerops platform beyond what's in this brief. Do NOT review platform config files (zerops.yaml, import.yaml) — the main agent has platform context and has already validated them against the live schema. Your job is to catch things only a {framework} expert catches.
>
> **CRITICAL — where commands run:** you are on the zcp orchestrator, not the target container. `{appDir}` is an SSHFS mount. All target-side commands (compilers, test runners, linters, package managers, framework CLIs, app-level `curl`) MUST run via `ssh {hostname} "cd /var/www && ..."`, not against the mount. The deploy step's "Where app-level commands run" block has the full principle and command list — read it before starting if anything here is unclear. If you see `fork failed: resource temporarily unavailable` or `pthread_create: Resource temporarily unavailable`, you ran a target-side command on zcp via the mount.
>
> **Read and review (direct fixes allowed):**
> - All source files in {appDir}/ — controllers, services, models, entities, migrations, modules, templates/views, client-side code, routes, middleware, guards, pipes, interceptors, event handlers
> - Framework config: `tsconfig.json`, `nest-cli.json`, `vite.config.*`, `svelte.config.*`, `package.json` dependencies and scripts, lint config (but NOT the Zerops-managed `zerops.yaml`)
> - `.env.example` — are all required keys present with framework-standard names?
> - Test files — do they exercise the feature sections, or are they scaffold leftovers?
> - README **framework sections** only — what the app does, how its code is wired. Do NOT review the README's zerops.yaml integration-guide fragment — that's platform content the main agent owns.
>
> **Framework-expert review checklist:**
> - Does the app actually work? Check routes, views, config, migrations, framework conventions (env mode flag, proxy trust, idiomatic patterns, DI order, middleware ordering, async boundaries, error propagation).
> - Is there dead code, unused dependencies, missing imports, scaffold leftovers?
> - Are framework security patterns followed? (XSS-safe templating, input validation, auth middleware order, secret handling)
> - Does the test suite match what the code does?
> - Are framework asset helpers used correctly (not inline CSS/JS when a build pipeline exists)?
>
> **Silent-swallow antipattern scan (MANDATORY — introduced after v18's Meilisearch-silent-fail class bug):**
> - **In init-phase scripts** (seed, migrate, cache warmup, any file run from `initCommands` or a `execOnce`-gated command): grep for `catch` blocks whose only action is a `console.error` / `log.error` / `fmt.Println` followed by `return`, `continue`, or implicit fallthrough. Every such catch is a `[CRITICAL]` issue — report it with the file path, line number, and the specific side effect that will be silently skipped. The rule is documented in `init-script-loud-failure`: init scripts must `throw` / `exit 1` / `panic` on any unexpected error, no "non-fatal" labels.
> - **In client-side fetch wrappers** (every frontend component that issues an HTTP request): grep for `fetch(` calls without a `res.ok` check and for JSON parsers without a content-type verification. Every bare `const data = await res.json()` that doesn't check `res.ok` first is a `[WRONG]` issue. Every array-consuming store that lacks a `[]` default is a `[WRONG]` issue. The rule is documented in `client-code-observable-failure`.
> - **Async-durable writes without `await` on completion**: Meilisearch `addDocuments` / `updateSearchableAttributes` without a following `waitForTask`, Kafka producer without `flush()`, Elasticsearch bulk without `refresh`. Every such call in an init-phase script is a `[CRITICAL]` issue.
>
> **Feature coverage scan (MANDATORY):**
> - Read the plan's feature list (the main agent will include it in your brief). For each feature declared in `plan.Features`:
>   - If `surface` includes `api`: grep for a matching endpoint at `healthCheck`. Missing = `[CRITICAL]`.
>   - If `surface` includes `ui`: grep for `data-feature="{uiTestId}"` in the frontend sources. Missing = `[CRITICAL]`.
>   - If `surface` includes `worker`: grep for a worker handler matching the feature's subject / queue. Missing = `[CRITICAL]`.
> - Also grep for `data-feature="..."` attributes that are NOT in the declared feature list (extra features the sub-agent invented without a plan entry). Report as `[WRONG]` — the plan is authoritative; orphaned features are either undocumented scope creep or leftover from an earlier iteration that should be deleted.
>
> **Do NOT call `zerops_browser` or `agent-browser`.** Browser verification is a separate phase run by the main agent after this static review completes. You have no reason to launch Chrome: you're a code reviewer, not a user-flow tester. If your review of the code raises a question that would require a browser to answer ("does this controller's error envelope actually reach the frontend?", "does the CORS middleware accept the appstage origin at runtime?"), report it as a `[SYMPTOM]` with the specific evidence you'd expect to see and stop — the main agent will verify it in the browser walk.
>
> **Symptom reporting (NO fixes):**
> If anything in the browser walk points to a platform-level cause (wrong service URL, missing env var, CORS failure, container misrouting, deploy-layer issue), STOP and report the symptom. Do NOT propose `zerops.yaml`, `import.yaml`, or platform-config changes. The main agent has full Zerops context and will fix platform issues. Your report on a platform symptom should be shaped like: "appstage's console shows `Failed to fetch https://api-20fe-3000.prg1.zerops.app/status`. This URL appears to target a service named `api` which doesn't exist in the running environment (only `apidev` and `apistage` do). Platform root cause unclear — main agent to investigate."
>
> **Out of scope (do NOT review, do NOT propose fixes for):**
> - `zerops.yaml` fields — `build.base`, `run.base`, `healthCheck`, `readinessCheck`, `deployFiles`, `buildFromGit`, `zeropsSetup`, `envReplace`, `envSecrets`, `cpuMode`, `mode`, `priority`, `verticalAutoscaling`, `minContainers`, `corePackage`, anything prefixed with `zsc`
> - `import.yaml` fields — any of them, in any of the 6 environment files
> - Service hostname naming, suffix conventions, env-tier progression
> - Env var cross-service references (`${hostname_varname}`)
> - Schema-level field validity
> - Comment ratio or comment style in platform files
> - Service type version choice
> - Any Zerops platform primitive you haven't seen before — don't guess, don't invent new ones (e.g., don't suggest a new `setup:` name), don't suggest fixes that would introduce them
>
> Report issues as:
>   `[CRITICAL]` (breaks the app), `[WRONG]` (incorrect code but works), `[STYLE]` (quality improvement), `[SYMPTOM]` (observed behavior that might have a platform cause — main agent to investigate, no fix proposed).

Apply any CRITICAL or WRONG fixes the sub-agent reported, then **redeploy** to verify the fixes work:
- If zerops.yaml or app code changed: `zerops_deploy targetService="appdev" setup="dev"` (API-first: also redeploy apidev) then cross-deploy to stage
- If only import.yaml (finalize output) changed: re-run finalize checks
- Do NOT skip redeployment — the browser walk in 1b is meaningless if fixes aren't tested.

**Close the 1a sub-step (showcase only).** After all CRITICAL / WRONG fixes are applied and the recipe has been redeployed:

```
zerops_workflow action="complete" step="close" substep="code-review" attestation="{framework} expert sub-agent reviewed N files, found X CRIT / Y WRONG / Z STYLE. All CRIT and WRONG fixed and redeployed. Silent-swallow scan: clean. Feature coverage scan: clean (all {N} declared features present)."
```

The attestation must name findings and fixes. Bare "review done" or "no issues found" attestations are rejected at the sub-step validator.

</block>
```
