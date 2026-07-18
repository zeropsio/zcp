# Group 3: greenfield + recipe sessions

## Session: greenfield-fullstack-multi-runtime
**Task:** Next.js frontend + Node API + Postgres, dev + stage pair on both runtimes.
**Findings:**

1. [RESPONSE-CONFUSING] "Build logs were not captured" misreads as build crash
   - What: Cross-deploy to `appstage` failed at exactly 5s with `buildStatus: BUILD_FAILED` and "Build logs were not captured before the container exited." Agent rewrote `buildCommands` twice before realizing the build never started — the real cause was a fat `.next/` cache being shipped with no `.gitignore`. Quote: "The 'no logs captured' message reads like 'your build crashed early,' but in cross-deploy land it more likely means the platform never reached the build step at all."
   - Layer: `ops/deploy_failure*.go` classifier (FailureClassification should distinguish upload/git-state failure from build failure when no logs captured); atom: cross-deploy preflight knowledge about `.gitignore` for Next.js
   - Cross-session signal: no (specific to first-cross-deploy with bloated workspace)

2. [GUESS-SHAPE] `confirmDestructive` shape not derivable from error
   - What: After `READY_TO_DEPLOY` recovery loop, agent had to send `confirmDestructive={"operation":"import-override","acknowledgedTargets":["appstage"]}` but had to "copy `wouldDestroy.operation` and `wouldDestroy.targets` verbatim." Recovery hint also showed `startWithoutCode: "true"` as a string and agent "read past it."
   - Layer: `tools.DiagnosedDestruction` wire shape — recovery hint should give the EXACT JSON to paste, not a typed-string mimic. Spec: invariant about diagnose-before-destruct gates.
   - Cross-session signal: no (only fires after a failed first cross-deploy)

3. [ATOM-CONTENT] Next.js standalone deployFiles pattern undecided in recipe
   - What: Recipe knowledge for `nextjs-ssr-hello-world` listed the three artifacts (`.next/standalone`, `.next/static`, `public`) but "didn't show a working `deployFiles` block." Agent tried `[./.next/standalone/~]` extraction, then switched to copying static+public INTO `.next/standalone` and deploying it whole. Both work but neither is canonical.
   - Layer: `internal/knowledge/recipes/nextjs-ssr-hello-world.md` — pick the "copy-into-standalone, deploy directory whole" pattern as the recipe default
   - Cross-session signal: no (Next.js specific)

4. [GUIDANCE-MISLEADING] Subdomain-enable fan-out broke on non-deployed services
   - What: `zerops_verify` on `apidev` returned `http_root: fail — subdomain access not enabled` with recovery pointing at `zerops_subdomain action=enable`. Agent ran enable for `apidev` + `appdev` in parallel; `appdev` errored `serviceStackIsNotHttp` because it hadn't been deployed yet (no declared HTTP port). Error doesn't say "deploy first."
   - Layer: `ops.Subdomain.Enable` error message + spec invariant about subdomain auto-enable being the deploy handler's concern. Recovery hint on `http_root` failure should be scoped to the verified service only, not a generic "enable subdomain" that agents fan out across services.
   - Cross-session signal: yes (subdomain auto-enable predicate / mode-allowlist is a recurring theme)

5. [GUESS-SHAPE] `setup` parameter on cross-deploy semantics easy to misread
   - What: Agent had to figure out that `setup="prod"` for cross-deploy names a BLOCK in zerops.yaml, not the target hostname. "It's easy to misread `setup` as referring to the target hostname." Recipe naming convention (`apidev` block for self, `prod` block for cross) made it ambiguous.
   - Layer: `zerops_deploy` tool description / `setup` parameter docstring; atom for cross-deploy scaffold
   - Cross-session signal: yes (recurring confusion in cross-deploy scenarios)

6. [SCOPE-CONFUSION] Deploy serial vs parallel not explicit
   - What: Agent learned the hard way that `zerops_deploy` is "channel-blocking and serial — you can't deploy apidev and appdev in parallel even though they're independent services. I tried once and the second call would have returned 'Not connected.'" Verify+deploy can overlap; deploy+deploy cannot.
   - Layer: tool annotation on `zerops_deploy` (concurrency constraint) and/or workflow guidance
   - Cross-session signal: yes (multi-service first-deploys are common)

---

## Session: greenfield-node-postgres-dev-stage
**Task:** Team-notes dashboard, Node + Postgres, dev + stage pair.
**Findings:**

1. [GUIDANCE-MISLEADING] Recipe-vs-classic route-pick has no signal for custom-app intent
   - What: Bootstrap route menu offered nodejs-hello-world + nestjs-minimal recipes, both `fit: "exact"`. Agent picked nodejs-hello-world reflexively. Then "rewrote `migrate.ts`, `app.ts`, and the health check path anyway" — only kept ~30 lines of scaffolding. Recipe value (zerops.yaml + lifecycle) was orthogonal to app code (which was hello-world demo). Quote: "if the user's intent describes a *specific custom app* and the recipe is just 'Node + Postgres,' seriously consider classic."
   - Layer: `bootstrap-route-options.md` atom + `workflow_bootstrap` route-pick heuristic — "exact fit" on infra should not imply "exact fit" for app intent; signal whether recipe content is template-only or contains demo features the agent will need to rip out
   - Cross-session signal: yes (recipe-vs-classic tension also visible in greenfield-website-from-brief)

2. [STATE-CONFUSION] `zerops_workflow status` silently switched to stale workflow after auto-close
   - What: Final `zerops_workflow action="status"` returned `kind: "launch-active"` for someone else's prior `launch-production` workflow (`api-prod`, `myapp-prod`) — nothing about the develop session just finished. Agent had to reason "if my session were still open, status would surface it; auto-close fired, status fell through to the next active workflow." Took two reads.
   - Layer: `zerops_workflow action="status"` response shape — when scope falls through to an unrelated active workflow, emit an explicit marker (e.g. `previousSessionAutoClosed: true` or a short note distinguishing "your session" vs "next active workflow on project"). Spec: status as lifecycle recovery primitive.
   - Cross-session signal: maybe (multi-tenant project state pollution)

3. [GUIDANCE-OVERLOAD] develop-start response buries actionable "Next" under reference walls
   - What: "Wall of pre-loaded guidance — five or six 'Pre-load tool schemas in one batch' sections, repeated platform rules, mode matrices, etc. The actually-useful 'Next' line is buried at the bottom. On a first read it's intimidating."
   - Layer: `internal/content/workflows/develop/` guidance composer; route briefing trim. Spec: spec-workflows §guidance composition.
   - Cross-session signal: yes (greenfield-website-from-brief reports same overload pattern)

4. [RESPONSE-CONFUSING] First-dev-deploy is "successful" but container idle — 502 between deploy and dev-server-start
   - What: "On the first deploy to a dev-mode container, `zerops_deploy` finishes successfully but the container is still idle (`zsc noop`) — `zerops_verify` will report HTTP 502 if you run it before starting the dev server. The guidance does say this, but it's easy to miss in the wall of text."
   - Layer: develop guidance composer should surface deploy→dev_server_start→verify ordering compactly (currently buried). Possibly the `zerops_deploy` response itself should include `nextAction: "start dev server, then verify"` for dev-mode runtime types.
   - Cross-session signal: yes (dev-mode lifecycle confusion is recurring)

---

## Session: greenfield-website-from-brief
**Task:** One-page Czech sports-club informational site from a designdotmd brand-style guide, "make analysis first."
**Findings:**

1. [RETRY-TOOL] `ToolSearch select:` prefix ambiguity
   - What: First call with `mcp__zerops__zerops_*` prefix worked. Shortened to `select:zerops_import,zerops_process` returned "No matching deferred tools found" with no hint about prefix. Worked around via keyword search. Quote: "a future agent should just always use the exact name as written in the deferred list."
   - Layer: `ToolSearch` tool error message — when `select:` lookup fails, hint that names must match the deferred-list exact form (with prefix). Not ZCP-owned but observed friction.
   - Cross-session signal: no (Claude Code framework concern)

2. [GUESS-SHAPE] Static-runtime yaml: `run:` block over-specified in guidance
   - What: Static-runtime example in guidance shows BOTH `build` and `run: { base: nginx@1.22 }`. Agent's service type is `static`, not `nginx`. Agent "omitted `run` entirely on a hunch from one line that said 'nginx auto-serves on Zerops's managed port' — and that worked. So the example in the guidance is overspecified for service type `static`; you do not need a `run.base` at all."
   - Layer: atom `bootstrap-classic-plan-static.md` (or equivalent) — minimum static yaml is build-only; current example misleads agents into including a dead `run.base`
   - Cross-session signal: no (static-specific)

3. [GUIDANCE-MISLEADING] Self-deploy destruction warning misframed for static
   - What: Destruction warning for self-deploy with narrow `deployFiles` is framed around "source files destroyed." For a static site with a Node build step, "the more practical concern is just 'don't ship `node_modules` to the static server.'" Agent solved with `rm -rf node_modules package-lock.json` as last buildCommand + `deployFiles: [.]`. A future agent reading the warning might try `deployFiles: [./dist]` which preflight rejects (DM-2).
   - Layer: `validateZeropsYml` error text for DM-2 — should suggest "clean unwanted files at end of buildCommands" as the canonical static-runtime pattern, not just "use `[.]`."
   - Cross-session signal: no (static-specific framing)

4. [RESPONSE-CONFUSING] `workSessionState: { status: "none" }` during active deploy
   - What: Deploy response in active develop session returned `workSessionState: { status: "none", note: "No active develop session" }`. Deploy worked, verify worked, agent set close-mode and got `{status: "updated"}`. Quote: "I'm not 100% sure the session ever actually tracked the deploy. A future agent should treat that `workSessionState.status: "none"` field as worth investigating."
   - Layer: work-session state injection on deploy response — either the session was genuinely not tracking, or the response inaccurately reported `none` when an active session existed. Likely root: per-PID Work Session lookup keyed wrong. Spec: spec-work-session.md (compaction survival, auto-close).
   - Cross-session signal: maybe (Group 3 has TWO session-state confusions: this one and the stale launch-active fall-through)

5. [ATOM-CONTENT] `designdotmd` slug semantics not a placeholder
   - What: "`my-brand` is not a magic placeholder — the CLI literally tries to fetch a public registry entry named `my-brand` and fails with 'not found.'" Agent learned to run `npx designdotmd list` first to surface catalog matches. This is meta-tooling friction outside ZCP scope but agent flagged it in retrospective.
   - Layer: out of scope (not ZCP) — flag but no action
   - Cross-session signal: no

6. [GUIDANCE-OVERLOAD] Discover dump irrelevant to single static service
   - What: "Bootstrap 'discover' step dumps a wall of guidance covering managed services, env var wiring, cross-deploy semantics, dev/stage pairs, and shadow-loop pitfalls — almost none of which applies to a single static service with no deps. The relevant section ('Static runtime plan') is one short paragraph buried in the middle."
   - Layer: bootstrap-discover-local / bootstrap-classic-plan-static composition — scope the dump to detected runtime class, not the universe. Spec: spec-knowledge-distribution.
   - Cross-session signal: yes (same overload reported in greenfield-node-postgres-dev-stage)

---

## Session: recipe-laravel-minimal-standard
**Task:** Laravel app on Zerops, dev environment + staging slot for build validation before promote.

NOTE: This session has NO `self-review.md` — the retrospective was never written. Transcript analysis only.

**Eval-infra finding (counts as a finding):**

1. [EVAL-PROMPT] Retrospective phase never executed
   - What: Session terminated cleanly with the "Everything is up and running" success message (turn 12 of the final sub-session), but no retrospective.jsonl, no self-review.md. The retrospective-prompt.txt exists but the agent was never invoked against it. This is an eval-infra bug — every other session in the run produced a retrospective.
   - Layer: `eval/behavioral/flow-eval.sh` (or the runner that fires the retrospective resume). The transcript shows 4 distinct `system.init` events in one transcript file, which suggests the runner restarted the session multiple times mid-task; the retrospective resume likely keyed off the wrong session_id.
   - Cross-session signal: no (infrastructure-only)

**Transcript-derived friction signals:**

2. [RETRY-TOOL] `AskUserQuestion` permission-denied — bootstrap forced route prompt
   - What: At the route-pick step of `workflow_bootstrap`, agent emitted `AskUserQuestion` listing laravel-minimal / laravel-showcase / classic options. Permission was DENIED ("Answer questions?" / `is_error: true`). Agent recovered by re-asking the same three options as plain text in the next turn (which the runner answered automatically by re-issuing the task prompt or similar). Quote (transcript turn 2): "The user is saying they didn't see my question. Let me just ask them directly in text instead of using the AskUserQuestion tool."
   - Layer: eval harness `Claude Code` permission config — `AskUserQuestion` is not in the allowlist for eval runs. Either: (a) allow `AskUserQuestion` and have the runner answer it programmatically, (b) eval-prompt should pre-pick the route ("use laravel-minimal recipe") to avoid the prompt, OR (c) bootstrap should auto-pick when only one recipe in the catalog is a clean match. Currently agent burns one turn and one permission-denial signal on a recovery dance.
   - Cross-session signal: yes (any scenario where bootstrap finds multiple recipe options will hit this)

3. [no-friction-observed] Rest of session ran cleanly
   - What: After the AskUserQuestion recovery, the agent followed canonical flow: bootstrap → recipe import (laravel-minimal) → discover → close → develop → deploy appdev (DEPLOYED, 1m11s, subdomain auto-enabled) → SSH `npm run build` (per recipe knowledge for Vite manifest) → verify appdev (healthy, http_root pass) → cross-deploy appstage (DEPLOYED, 57s) → verify appstage (healthy) → close-mode auto. No retries, no `is_error`, no shape guessing visible in tool calls.
   - Layer: none — pinpoint of recipe knowledge ("Vite manifest missing on dev after fresh deploy" already in `laravel-minimal.md`) worked correctly.
   - Cross-session signal: positive — Laravel recipe knowledge is well-tuned.

---

## Group-level patterns

- **Guidance-overload on bootstrap/discover/develop** — three sessions out of four (everything except laravel, which had its retrospective lost) explicitly call out that workflow-start responses are "walls of text" with the actually-actionable `Next:` line buried. Same complaint hits both single-static (one runtime) and multi-runtime fullstack scaffolds. Composer-level fix: gate atoms by detected scope and surface `Next:` first.

- **Cross-deploy + `setup`-block naming friction** — both `setup="prod"` naming in fullstack (agent had to learn it's a yaml block, not hostname) and the laravel cross-deploy `setup: prod` flow show that the `setup` parameter on `zerops_deploy` is consistently a source of guess-the-shape friction. Tool docstring + atom for cross-deploy scaffold could disambiguate by example.

- **Recipe-vs-classic route-pick lacks "is recipe code template or demo?" signal** — node-postgres agent picked nodejs-hello-world reflexively, then gutted 90% of it because the recipe was a demo (greetings table, hello endpoint) not a template. Bootstrap route-options atom should distinguish "recipe = production-shaped scaffold you can build on" from "recipe = working demo you'll need to strip." Same pattern surfaces in the website-from-brief session where the recipe's value was the yaml + lifecycle, not the app code.

- **State-sentinel confusions in deploy/status responses** — `workSessionState: { status: "none" }` mid-active-session (website), stale `launch-active` fall-through after auto-close (node-postgres), and the silent scope-switch on `zerops_workflow action="status"` all point at one root: when the session-state lookup misses or falls through, the response carries an ambiguous payload that the agent has to reason about rather than read. Both spec-work-session.md and the status-handler envelope shape could carry an explicit transition marker ("your session auto-closed at $time, next active workflow is X" vs "no session ever existed").

- **One eval-infra failure (laravel retrospective) and one permission-config friction (laravel AskUserQuestion denied)** — both surface during the same scenario. Worth checking whether the runner restarts mid-task (4 system.init events in one transcript) are the cause of the missing retrospective.
