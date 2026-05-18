# Group C: multi-service + delivery sessions (3 scenarios)

Run dir: `/Users/macbook/Documents/Zerops-MCP/zcp/eval/behavioral/runs/20260517-083949/`

Wall times:
- cadence-multiservice-build-run2-replay: **29m12s** (scenario 28m19s)
- cadence-multiservice-spec-run2-replay: **16m44s** (scenario 2m54s + retro)
- delivery-git-push-actions-setup: **2m11s** (scenario 37s)

---

## Session: cadence-multiservice-build-run2-replay (NEW, 29m)
**Task:** Project-management webapp — design spec, build it, deploy it to Zerops. Multi-service (Next.js + Prisma + Postgres etc.), agent built and deployed an 8-service Cadence application end-to-end. User simulator returned `user_sim_satisfied`, so the agent did complete the actual ask — but burned considerable time on six discrete failure modes.

**Findings (by severity):**

1. **[BROKEN] Platform `exec`-wraps `start:` command, silently truncating `&&` chains**
   - What: `start: npx prisma db push --accept-data-loss && npm start` ran `prisma db push` to success, then EXITED instead of executing `npm start`. Container looped (`RUN.START COMMAND FINISHED` → `RUNNING RUN.START COMMAND` every few seconds). Logs do NOT identify the `&&` chain as the cause; agent had to read twice and infer. Either platform should not exec-wrap start commands, or this contract MUST be documented at the atom layer for runtime `start:` authoring.
   - Layer: atom guidance for `internal/content/atoms/` (run-phase / zerops.yaml authoring); possibly `internal/knowledge/recipes/nodejs.md` for prisma-migration pattern; arguably platform behavior itself (exec-wrap is hostile to documented bash idioms).
   - Cross-session signal: medium — affects ANY recipe that chains DB migrations into start command. Likely recurs on Laravel `php artisan migrate && php-fpm`, Rails `rake db:migrate && rails server`, etc.

2. **[BROKEN] `READY_TO_DEPLOY` after first-deploy failure requires undocumented three-call recovery dance**
   - What: First deploy fails → service stuck `READY_TO_DEPLOY` → next deploy refused with `DIAGNOSIS_REQUIRED` → `zerops_import override: true` rejected with second error → must extract `{operation, acknowledgedTargets}` from `wouldDestroy` field in the rejection payload → `zerops_import override: true confirmDestructive: {...}` → redeploy. Agent narrated this as "really a three-call dance" — needed multiple round trips to discover the shape. Recovery is implemented (diagnose-before-destruct from CLAUDE.md), but the diagnose→ack→retry choreography is not surfaced as a single coherent Recovery hint at the failure point.
   - Layer: `internal/ops/events.go::ClassifyDeployFailure` and `internal/tools/import.go` — the FIRST failure response (`DIAGNOSIS_REQUIRED`) should already populate a structured `Recovery` pointing at the exact `zerops_import override+confirmDestructive` call shape, not require the agent to round-trip through a second rejection to discover field names. Atom for `READY_TO_DEPLOY` failure recovery.
   - Cross-session signal: high — this is a generic post-failed-deploy state, will recur on every greenfield with any first-deploy bug.

3. **[BROKEN] Build-phase env vars don't include project-level `${db_*}` refs, but tooling/atoms don't warn**
   - What: Agent placed `npx prisma db push` in `buildCommands`. `DATABASE_URL` only resolves from `run.envVariables` at runtime, NOT build time. "Build container has no access to project env vars by default." Build silently consumed the deploy. Agent only learned by burning a deploy. There's no preflight that flags "buildCommand references env that only resolves at run time."
   - Layer: deploy preflight (`internal/ops/deploy_preflight.go` or wherever zerops.yaml is validated) could detect `${db_*}` / project-env refs inside `buildCommands` strings and warn. Atom for `build.envVariables` vs `run.envVariables` boundary needs explicit "DB ops belong in run phase" pitfall.
   - Cross-session signal: medium — anyone running migrations/seed at build time hits this. Recipe-knowledge candidate for Prisma + Next.js + Postgres.

4. **[GUIDANCE-MISLEADING] Route menu offers "adopt" for services NOT visible in `zerops_discover`**
   - What: "Bootstrap's route menu offered an 'adopt' option for `appdev`/`appstage` but `zerops_discover` only showed `zcp`, I had no clean explanation for the discrepancy." Agent guessed (`isExisting: true`, classic route) and it worked, but flagged: "I was guessing." The two tools disagree about what services exist.
   - Layer: `internal/tools/discover.go` vs `internal/tools/workflow_bootstrap.go` route-menu construction — they read from different sources. Either discover should surface partially-bootstrapped services (with a flag) or the route menu should not offer adopt for services not in discover output.
   - Cross-session signal: yes — prior run (`git-push-setup-with-cicd-method-prompt` and `export-buildfromgit-self-snapshot`) reported same surface inversion: discover hides services that bootstrap can see.

5. **[GUIDANCE-MISLEADING] `npm ci` default in greenfield scaffolds breaks first deploy**
   - What: Atom guidance (or recipe default) recommends `npm ci` in buildCommands. Greenfield scaffolds have no `package-lock.json` committed yet → first build fails. Agent fixed by switching to `npm install`. "I copied the pattern from generic Node deploy guidance without thinking" (also reported in cadence-spec). Generic Node deploy guidance shouldn't lead with `npm ci` for first-deploy scenarios.
   - Layer: `internal/knowledge/recipes/nodejs.md` (or atom for Node first-deploy build); could also be `internal/content/atoms/` first-deploy-scaffold guidance.
   - Cross-session signal: yes — reported by BOTH cadence sessions independently.

6. **[ATOM-CONTENT] `serverExternalPackages` is version-conditional (Next 14 vs 15) and silent fallback misleads**
   - What: Atom or recipe guidance lists `serverExternalPackages` as top-level (Next 15). Agent ended up on Next 14.2 where it's `experimental.serverComponentsExternalPackages`. Dev server warns but still runs, so the misconfiguration silently masks itself until other symptoms (memory thrash) appear.
   - Layer: `internal/knowledge/recipes/` (Next.js recipe needs version branch for this key); not an atom — framework-specific.
   - Cross-session signal: low — Next.js specific.

7. **[ATOM-CONTENT] Dev-mode Next.js needs scaling-up RAM BEFORE first `zerops_dev_server` start**
   - What: Default container RAM too small for Next.js dev mode compilation. First `zerops_dev_server` reports "succeeded" with health-check pass, but logs show "Server is approaching the used memory threshold, restarting..." → unstable, looks like a code bug. Need to scale `appdev` to ~1GB+ BEFORE starting dev server.
   - Layer: atom for Next.js dev-server pre-warm step; possibly `internal/tools/dev_server.go` preflight could flag default RAM for known-heavy runtimes; recipe knowledge for Next.js.
   - Cross-session signal: yes — cadence-spec independently reports same memory ceiling (V8 heap allocation failure after Ready in 2.4s).

8. **[RESPONSE-CONFUSING] `zerops_verify http_root: fail` after dev-server start is a JIT-compile timeout, not a server fault**
   - What: First `zerops_verify` after dev-server start shows `http_root: fail` with timeout. Looks like server broken — actually Next.js dev mode compiles pages on first request, verify timeout is shorter than first compile. Cure: hit URLs with curl to warm compile cache, re-verify.
   - Layer: `internal/tools/verify.go` — when dev-mode runtime is detected (heuristic: very fresh service, dev-mode hostname), either lengthen timeout, retry with backoff, or annotate failure as "may be JIT compile, not server failure"; atom for dev-mode verify expectations.
   - Cross-session signal: yes — cadence-spec reports the SAME compile-timeout pattern verbatim ("Next.js dev mode compiles routes on first request and the verify tool's HTTP timeout is shorter than that first compile").

---

## Session: cadence-multiservice-spec-run2-replay (NEW, 16m44s wall / 2m54s scenario)
**Task:** Same project-management spec as build variant, but agent only ran a 2m54s scenario (followed by ~13m retrospective). Spec design + build + deploy to Zerops still happened; the agent terminated faster because some build-path friction did NOT recur (e.g. no Prisma `&&` chain trap — different DB stack chosen — see finding 4).

**Findings (by severity):**

1. **[BROKEN] `zerops_dev_server` reports `reason: health_probe_timeout` when actual cause is V8 OOM in `logTail`**
   - What: Tool surfaces a misleading top-level `reason` field. The agent went hunting at the network/probe layer and only fixed it after reading `logTail`: "V8 heap allocation failure after Next.js had already printed `Ready in 2.4s`." Cure: scale RAM 1.5–4 GB; the default container size is too small. Reason code lies; the real cause is reachable but only via secondary field.
   - Layer: `internal/tools/dev_server.go` / `internal/ops/dev_server.go` — when `logTail` contains heap allocation / OOM signal, top-level `reason` should be `oom_during_startup` or similar, not `health_probe_timeout`. The current reason is technically true (probe timed out) but actively misleading.
   - Cross-session signal: yes — cadence-build also reported memory ceiling but from a different surface (dev server "succeeded" then thrashed).

2. **[BROKEN] `zerops_verify http_root: context deadline exceeded` after dev-server "Ready" is a JIT-compile timeout**
   - What: Same surface as cadence-build finding 8 — verify timeout shorter than Next.js first-request compile (3-4s for board page). Agent had to warm both routes via `ssh appdev curl localhost:3000/...` before verify passed. Pattern is reproducible across both cadence sessions.
   - Layer: `internal/tools/verify.go` — same fix as cadence-build #8.
   - Cross-session signal: yes — see cadence-build #8 above.

3. **[GUIDANCE-MISLEADING] `npm ci` for first scaffold build (re-occurrence of build-session #5)**
   - What: "Obvious in hindsight — `npm ci` requires a committed lockfile — but I copied the pattern from generic Node deploy guidance without thinking. Use `npm install` for the first deploy."
   - Layer: same as cadence-build #5 (`internal/knowledge/recipes/nodejs.md` / first-deploy atom).
   - Cross-session signal: yes — two independent reproductions in the same run.

4. **[SMOOTH] Drizzle + Postgres connection-string composition warning is load-bearing and worked**
   - What: Bootstrap envelope explicitly flagged that `${db_connectionString}` omits `/${dbName}` and that Drizzle needs the explicit composed URL. Agent followed "verbatim" and reports no connection-string drift. This is an example of guidance that landed and saved a debugging round.
   - Layer: bootstrap envelope text (likely `internal/tools/workflow_bootstrap.go` or atom for postgres env vars) — keep, don't touch.
   - Cross-session signal: positive — useful when comparing to other sessions where missing warnings cost time.

5. **[SMOOTH-NEGATIVE] Tradeoff surfacing gap (agent self-flag)**
   - What: Agent chose HTML5 native drag-and-drop over `@hello-pangea/dnd` to avoid React 19 / SSR compatibility unknowns. Worked, but: "I didn't surface the tradeoff to the user — I should have." Agent-level discipline finding, not platform-level — flagged for completeness only.
   - Layer: agent prompt / persona — not platform.
   - Cross-session signal: no — agent-discipline observation.

---

## Session: delivery-git-push-actions-setup (RETURNING, 2m11s)
**Task:** PHP app already working; user wants future delivery via GitHub Actions from `main`, repo `https://github.com/example/weather-app`, no real GitHub secret yet. The 2m11s duration (vs ~prior-run's multi-call walkthrough) suggests prior-run guidance changes landed and made this nearly trivial.

**Findings (by severity):**

1. **[GUIDANCE-MISLEADING] `git-push-setup` walkthrough still chain-frames step 1 → step 2 → step 3, despite the prior run flagging this**
   - What: "Read naively, you might think you have to do step 1 before step 2. You don't. The stamp call (step 2) is pure metadata — it validates the URL shape and writes `GitPushState=configured` and `RemoteURL` to service meta. No token, no network call to GitHub." Agent self-corrected ("If you read the walkthrough as a strict sequence you might stop after step 1 and tell the user you're blocked"), but the surface STILL invites this misread. Prior-run group2 finding #1 raised exact same point.
   - Layer: `internal/tools/workflow_git_push_setup.go` walkthrough text — needs an explicit "these three steps are independent (stamp is pure metadata; build-integration is independent of git-push)" callout. Step ordering implication was identified as fix-needed in prior run; this run shows it has NOT been fixed.
   - Cross-session signal: yes — re-occurrence of prior-run #1.

2. **[GUIDANCE-MISLEADING] `build-integration=actions` does NOT require `GitPushState=configured`, but walkthrough implies prerequisite ordering**
   - What: "build-integration=actions does NOT require GitPushState=configured first. They're orthogonal capabilities — git-push is the zcli-push-from-container path, GitHub Actions runs zcli push from CI and doesn't touch the in-container git remote at all." Agent did the stamp anyway because remote URL belongs on the service either way, but: "a future agent could reasonably skip directly to build-integration." Surface still reads as serial.
   - Layer: same as prior-run group2 #1 — both findings point at same walkthrough copy.
   - Cross-session signal: yes — same surface as #1 above.

3. **[RESPONSE-CONFUSING] `build-integration` response surfaces TWO workflow files (`workflowFile` + `alternateWorkflowFiles[0]`); default ambiguity persists**
   - What: "There's a default `workflowFile` (setup-aware, installs zcli, passes `--setup`) and an `alternateWorkflowFiles[0]` (single-setup, uses the `zeropsio/actions@v1.0.2` marketplace action). The response's own `nextStep` says 'Keep the default setup-aware zcli workflow unless you are certain the repository has only one setup.'" Agent went with default. Future agent could silently pick marketplace-action variant for visual cleanliness. Prior-run group2 #3 flagged this exact pattern at lines 195-228 of `workflow_build_integration.go`.
   - Layer: `internal/tools/workflow_build_integration.go:195-228` — fix from prior run did not land or is incomplete. Restructure response to lead with single primary file + clearly-labeled "alternates only when X."
   - Cross-session signal: yes — re-occurrence of prior-run #3.

4. **[SMOOTH] `ZEROPS_TOKEN` secret-reuse note is load-bearing and the agent caught it**
   - What: "`ZEROPS_TOKEN` should reuse the same Zerops PAT that ZCP itself uses (`$ZCP_API_KEY` in the container env), not a freshly minted one. One credential, one rotation surface. If you generate a new Zerops token for CI you've doubled your rotation work for no reason. I almost glossed over that line." Guidance is present but easy-to-skim; nearly-missed but caught. Worth promoting visually.
   - Layer: `internal/tools/workflow_build_integration.go` response — could surface the secret-reuse hint more prominently.
   - Cross-session signal: low.

5. **[SMOOTH] `gh secret set -b "$ZCP_API_KEY"` shell-expansion pattern correctly handed off**
   - What: "The literal token value never crosses the MCP wire and I don't need it to produce the command. A future agent should resist the urge to ask the user for the token 'just to fill it in' — the commands are designed to be handed off as-is." Agent correctly recognized this and did not ask for the secret. Good behavior; worth pinning in atom guidance so it survives.
   - Layer: atom for git-push-setup / build-integration final-step delivery — promote the "do not solicit literal secret" rule.
   - Cross-session signal: low.

6. **[SMOOTH] No `SERVICE_NOT_FOUND: not bootstrapped` pain in this run**
   - What: PHP app was already adopted (task prompt: "the PHP app is already working"). Did NOT trip on `SERVICE_NOT_FOUND` error surface that hit `export-buildfromgit-self-snapshot` and `git-push-setup-with-cicd-method-prompt` in the prior run. Possibly because pre-adopted; not necessarily fixed.
   - Layer: N/A — context-dependent absence of friction.
   - Cross-session signal: see prior-run group2 patterns — this finding's absence here doesn't mean the surface is fixed.

7. **[SMOOTH] No `closeMode` shape-guessing this time**
   - What: Prior-run group2 #4 reported `closeMode` JSON shape ambiguity (`{hostname:value}` flat map). This agent did not need to call `close-mode` at all (build-integration alone was sufficient for the task), so the shape ambiguity didn't surface. Not a fix — just a non-trigger.

---

## Group-level patterns

- **Next.js dev-mode JIT-compile vs. `zerops_verify` timeout is the single highest-friction surface across both cadence sessions** (`cadence-build` #8 and `cadence-spec` #2 report it identically). Two independent reproductions in the same run = consistent reproducer. Fix in `internal/tools/verify.go`: when probing a dev-mode runtime, either lengthen HTTP timeout for first probe or treat first-probe timeout as "warm-cache-then-retry" rather than hard fail.

- **Build-phase env-var resolution boundary (build.envVariables vs run.envVariables) trips agents writing migrations** — cadence-build #3 burned a deploy. The boundary is documented at the CLAUDE.md invariant level (`run.envVariables is the canonical setup-entry env-var location`), but the failure mode of "DB op in `buildCommands` references an env that only resolves at run time" is not preflight-detected. Deploy preflight could detect `${db_*}` / project-scope refs inside `buildCommands` strings and warn at submit time, not at deploy time.

- **`npm ci` default in Node first-deploy guidance is wrong twice in one run** (cadence-build #5 and cadence-spec #3). Generic Node recipe leads with `npm ci`; greenfield scaffolds have no lockfile yet → first build fails. Either default to `npm install` in first-deploy contexts, or warn at preflight time when no `package-lock.json` is in the deployed tree.

- **Dev-mode RAM ceiling (Next.js specifically, possibly others) is undiscoverable until container thrashes** — cadence-build #7 and cadence-spec #1 both report it. The signals differ (cadence-build saw thrash AFTER "success"; cadence-spec saw a probe timeout that was actually OOM in logTail). Both end with "scale to 1+ GB before first dev start." `internal/tools/dev_server.go` could pre-scale for known-heavy runtimes, or pre-flight current scale and warn.

- **Recovery from `READY_TO_DEPLOY` (post-failed-deploy stuck state) requires a multi-call dance that surfaces ad-hoc** — cadence-build #2 narrated the three-call ceremony. The first failure response should populate a single structured Recovery hint pointing at the exact `zerops_import override+confirmDestructive` shape (read `wouldDestroy` payload), not require the agent to discover field names by round-tripping through rejection responses. This is "diagnose-before-destruct" surfacing the gate but not the choreography.

## Comparison to previous run (delivery-git-push-actions-setup only)

- **What is the same (not fixed):** prior-run findings #1 (chain-framing of three orthogonal actions), #3 (default-vs-alternate workflow file ambiguity) BOTH re-surface in this run with the same observations. The walkthrough copy and the response structure at `workflow_git_push_setup.go` and `workflow_build_integration.go:195-228` appear to have NOT changed — prior recommendations have not landed.

- **What got faster:** 2m11s wall time (37s scenario) suggests the agent took 1-2 calls fewer than prior — likely because of the user prompt's wording ("no real GitHub secret yet") which made agent comfortable stopping after `build-integration` without `git-push-setup` chain. The structural surfaces are unchanged; the agent just adapted to them more confidently. Not a fix — agent skill.

- **What did NOT recur:** prior-run finding #2 (`SCOPE-CONFUSION: first call info-only vs second call mutation`) and finding #5 (`CLAUDE.md routing-table suggests develop session for platform-side config`) are absent here. Possibly because the agent went straight to `build-integration` action without doing the two-call probe at `git-push-setup` first; routing-table call-out absent because the user prompt narrowly scoped to "GitHub Actions setup." Surface coverage shifted; not a clear fix signal.

- **What is new in this run:** finding #4 (secret-reuse `ZCP_API_KEY` as `ZEROPS_TOKEN`) and finding #5 (`gh secret set` shell expansion) — both NEW positive notes ("smooth but easy to skim"). Both could be promoted from "guidance present but glossed-over" to "guidance highlighted" via atom-level surfacing for next run's protection.
