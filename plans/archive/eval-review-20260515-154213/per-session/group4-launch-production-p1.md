# Group 4: launch-production sessions (part 1)

Run dir: `/Users/macbook/Documents/Zerops-MCP/zcp/eval/behavioral/runs/20260515-154213/`

## Session: launch-production-dev-only
**Task:** Launch a single Node runtime + postgres dev project to a separate `api-prod` project in eu-central; user generates one-shot Zerops API key on demand and deletes after launch.

**Findings:**

1. **[STATE-CONFUSION] Drift-gate rejection loop — stale per-session state files persist across `action="start"`**
   - What: At `ready-to-launch`, baseline of `commitSha/zeropsYamlSha256/projectEnvsDigest/serviceListDigest` is captured. User creating the one-shot API key in the dashboard can bump `projectEnvsDigest`. Re-calling `action="start"` does NOT reset the baseline; agent had to `rm` per-session JSON files under `.zcp/state/launch-production/` (keep `launch-audit-log.json`) before restart actually worked. Quote: "even though the failure response says 'abandon this launch...', just re-calling `action='start'` does NOT reset the baseline. The stale session files persist and the same drift error fires again."
   - Layer: `internal/tools/workflow_launch.go` (start should clear or supersede stale session); `internal/content/atoms/launch-status-recovery.md` (guidance currently reads like a hint, not a hard requirement)
   - Cross-session signal: yes — same staleness pattern recurs in part-2 group (pipeline-not-configured / pipeline-configured)

2. **[GUIDANCE-MISLEADING] `ready-to-launch` → `launching` race window between dashboard token creation and final call**
   - What: User generating the prod token between the two calls bumps env digest and trips the drift gate. Workflow design "walk user through generating the key at ready-to-launch" maximises drift risk. Better flow would be collect token BEFORE start so digest is stable across the run.
   - Layer: workflow design — `internal/tools/workflow_launch.go` ordering; atom `launch-ready-to-launch.md` should warn about dashboard side-effects on env digest
   - Cross-session signal: yes — affects all launch-production scenarios that gate users at ready-to-launch

3. **[ATOM-CONTENT] envClassifications four-bucket model has no slot for ZCP-managed plumbing envs**
   - What: `ZCP_API_KEY` and `GIT_TOKEN` are ZCP-managed (minted by `git-push-setup`, etc.), not user-installed SDK keys. Agent guessed `infrastructure` for `ZCP_API_KEY` and `external-secret` for `GIT_TOKEN`, "reasoning by analogy, not by anything the guidance said directly." Got past classify but couldn't verify in resulting prod project.
   - Layer: atom `launch-classify-platform-envs.md` (bucket definitions) — needs "ZCP-provisioned / regenerate-per-project" category or explicit rule
   - Cross-session signal: yes — same uncertainty appears in eww, fsp, lsc, and part-2 group

4. **[RESPONSE-CONFUSING] `notAuthorized` launch-key error gives nothing actionable**
   - What: First token rejected as placeholder; second rejected because user generated it from project-settings page (project-scoped) not account-level Personal Access Tokens page (account-scoped); "Custom access per project" unchecked also matters. None of those distinctions are in the guidance or the error message. Agent wasted multiple round trips.
   - Layer: handler error message (`internal/tools/workflow_launch.go` token-validation path) — must surface (a) which dashboard page, (b) checkbox state, (c) prefix discriminator; or atom `launch-launch-key-troubleshooting.md`
   - Cross-session signal: yes — token-flow ambiguity also surfaces in existing-project-token, existing-with-webhook

5. **[GUIDANCE-MISLEADING] `zerops_discover hostname=X` silently drops project-level envs**
   - What: classify-prompt guidance says "fetch values via `zerops_discover hostname=...`" but service filter omits project-level envs entirely. Five keys needing classification (`GIT_TOKEN`, `SESSION_SECRET`, etc.) were all project-scoped. Agent had to re-call without filter. Quote: "actively misleading for project-scoped envs — drop the hostname filter when the envs you're classifying are project-level."
   - Layer: atom `launch-classify-discover-shape.md` (or wherever classify-prompt examples live); discover-tool description in `internal/tools/discover.go` should say service filter omits project envs
   - Cross-session signal: yes — recurs in lsc, fsp, and part-2 group (new-project-push-mode flagged identically)

6. **[GUIDANCE-MISLEADING] `/var/www/{hostname}/` source mount empty — grep-the-source fallback unusable**
   - What: CLAUDE.md (boot shim) says service code is mounted at `/var/www/{hostname}/`, but mount was empty in this session. Agent had to classify purely from naming convention. The grep-the-source flow the docs prescribe doesn't actually work.
   - Layer: container boot shim `claude_container.md` should document mount-not-available fallback; eval seed should provision mount if expected
   - Cross-session signal: yes — same friction in eww, fsp, lsc, and part-2 group

---

## Session: launch-production-existing-project-token
**Task:** Deploy dev/stage code into existing prod project (user provides UUID + project-scoped token with Full access to ONLY that prod project). Refuse if token scopes elsewhere. After import, set up GitHub Actions push-mode CI/CD.

**Findings:**

1. **[GUIDANCE-MISLEADING] CLAUDE.md route table sends "deploy dev/stage to prod" → `launch-production` even when user owns prod project**
   - What: CLAUDE.md decision table maps "deploy dev/stage to prod" / "go live" → `launch-production`, with a parenthetical that it promotes to a *separate* prod project. User's natural phrasing "deploy this dev/stage code into prod" matches keywords exactly. Quote: "the user's natural phrasing matches the trigger keywords exactly, so it's very easy to fire `launch-production` reflexively. I almost did. Future agent: read past the keywords and check whether the user is asking for project *creation* or project *targeting*."
   - Layer: CLAUDE.md route table phrasing; atom `route-decision.md` or whichever doc owns "launch-production vs develop/bootstrap into existing"
   - Cross-session signal: yes — launch-production naming itself confuses across the family

2. **[SCOPE-CONFUSION] Token scope (account-wide vs project-scoped) interaction with workflow undocumented**
   - What: CLAUDE.md says `launch-production` accepts one-shot account-wide token at ready-to-launch; user offered project-scoped token tied to existing project. "Not obvious from the docs whether `launch-production` will even accept a project-scoped token, or whether feeding it one will fail mid-pipeline at `configuring-pipeline`." Agent never tested.
   - Layer: atom `launch-ready-to-launch.md` (token-scope expectations); handler may need explicit refusal at token-acceptance step with structured error
   - Cross-session signal: yes — token scope/type confusion shared with dev-only

3. **[SCOPE-CONFUSION] ZCP bound to dev project — can't `zerops_discover` target prod project**
   - What: `zerops_discover` only shows the bound project's services. No tool surface for "look at a different project I'm not bound to." Agent is "working blind on the target side until the workflow starts driving it." Recommendation: collect target project's service layout from user up front (hostnames, types, deploy target).
   - Layer: tool `zerops_discover` (could accept project-id override for read-only inspection); atom should note this blind-spot
   - Cross-session signal: yes — same friction in eww and part-2 sessions

4. **[GUIDANCE-OVERLOAD] Deferred-tool ToolSearch round-trip per distinct tool**
   - What: Every `zerops_*` tool surfaces with no schema; first call needs `ToolSearch select:<name>`. Easy to forget and try to call directly, wasting a turn. Fix: batch the `select:` for all expected tools (discover, workflow, env, logs, verify) in one `ToolSearch` early.
   - Layer: harness/MCP-init convention (not ZCP code); could be addressed in CLAUDE.md by listing recommended pre-load set
   - Cross-session signal: yes — also flagged in eww and part-2 (pipeline-configured)

---

## Session: launch-production-existing-with-webhook
**Task:** Existing prod project UUID provided. Seed code in. After launch, use Zerops native webhook for CI/CD (NOT GitHub Actions). Walk user through dashboard OAuth flow with exact source-code config URL. User re-runs launch with same launchKey to verify pipeline state.

**Findings:**

1. **[GUIDANCE-MISLEADING] discover/scope phase silently accepts `existingProjectId` but doesn't acknowledge path; ready-to-launch returns wrong guidance**
   - What: discover/scope happily accepts `existingProjectId` and advances — no acknowledgment that you're on the existing-project path, no schema hint that you also need `existingProdToken`. Then `ready-to-launch` returns guidance walking user through generating account-wide `launchKey` as if creating new project. Quote: "That guidance is wrong for your path... The tool description does mention `existingProjectId` and `existingProdToken` are mutually exclusive with `launchKey`, but the runtime guidance text never reflects which path you're on."
   - Layer: handler `internal/tools/workflow_launch.go` — guidance should branch on existing-project vs new-project path; atoms `launch-ready-to-launch.md` need path-aware variants
   - Cross-session signal: yes — same path-confusion latent in existing-project-token

2. **[ATOM-CONTENT] `/var/www/appdev/` mount didn't exist — only `/var/www/{.claude,.vscode,.zcp,CLAUDE.md}` present**
   - What: SSHFS mount missing. Agent fell back to classifying from env names + value patterns. Worked because names were conventional (`JWT_SECRET`, `APP_KEY`, `SESSION_SECRET`).
   - Layer: container boot shim (env-scoped); eval container seed
   - Cross-session signal: yes — recurs across dev-only, fsp, lsc, part-2

3. **[ATOM-CONTENT] envClassifications buckets don't fit ZCP-managed envs; agent guessed `infrastructure`**
   - What: Quote: "`GIT_TOKEN` and `ZCP_API_KEY` are genuinely ambiguous — I called both `infrastructure` on the theory that ZCP re-provisions them in the new project, but the bucket guidance defines `infrastructure` as values resolving from a managed-service `${...}` reference, which neither does. They might equally well be `external-secret`. The guidance doesn't have a bucket for 'ZCP-managed platform envs' specifically."
   - Layer: atom `launch-classify-platform-envs.md` — add ZCP-provisioned bucket
   - Cross-session signal: yes — dev-only, fsp, lsc, part-2 all flagged this

4. **[ATOM-CONTENT] Value-string embedded hints (`do-not-rotate-without-warning`) not surfaced as classification signal**
   - What: `SESSION_SECRET` value contained `existing-session-secret-also-keep`; `JWT_SECRET` value contained `existing-jwt-secret-do-not-rotate-without-warning`. Guidance frames auto-secret continuity around framework conventions (Laravel APP_KEY), not "read the value itself for embedded warnings." Agent bucketed `auto-secret` because fresh prod project with no existing encrypted state, but if migrating users/sessions, decision would be wrong.
   - Layer: atom `launch-classify-platform-envs.md` — add "scan values for human-readable hints" rule
   - Cross-session signal: low — content-pattern specific

5. **[RESPONSE-CONFUSING] `availableRuntimes` listing both `appdev` and `appstage` reads as "pick one"**
   - What: scope-prompt response's `sourceContext.availableRuntimes` listed both dev/stage halves as separate entries. Quote: "read as 'pick one.' The guidance below it then explained that for standard-mode pairs, 'either half is accepted as input — the handler normalizes internally.' Easy to miss on first read; I almost asked the user which half to target."
   - Layer: scope-prompt response shape — collapse dev/stage pair into one entry with `pair=` annotation; or atom warning
   - Cross-session signal: yes — fsp and lsc are also standard-pair scenarios

---

## Session: launch-production-from-standard-pair
**Task:** Dev + stage working. Launch to a separate `myapp-prod` project in eu-central. User generates one-shot Zerops API key on demand, deletes after.

**Findings:**

1. **[RETRY-TOOL] `action="classify"` retry — classify-prompt naming collides with recipe workflow's `factType` classification**
   - What: After workflow returned `status="classify-prompt"`, agent called `action="classify"` with `envClassifications`. Rejected with `factType is required` and suggestion list (`gotcha_candidate`, `ig_item_candidate`, etc.) — that action belongs to the recipe workflow's fact-record classification. Confirmed in transcript: `{"code":"INVALID_PARAMETER","error":"factType is required for action=classify"...}`. Correct mechanic: call `zerops_workflow` again with `envClassifications` set, no special action.
   - Layer: handler — `action="classify"` should branch on workflow type, or workflow_launch should accept `action="classify"` as advance-with-classifications; or atom `launch-classify-prompt.md` should explicitly state "no action arg, just envClassifications"
   - Cross-session signal: yes — naming collision is shared infra

2. **[GUIDANCE-MISLEADING] classify-prompt response embeds example with `workflow="export"`, not `workflow="launch-production"`**
   - What: Quote: "The guidance in the classify-prompt response shows the call shape but uses `workflow='export'` in the example, which made me second-guess whether the same pattern applied to launch-production. It does, but the example threw me." Confirmed in transcript: classify-prompt guidance includes 4 instances of `workflow=\"export\"`.
   - Layer: atom or handler text generating classify-prompt guidance — example must match current workflow
   - Cross-session signal: yes — recurs in lsc

3. **[GUESS-SHAPE] `productionProjectName` and `region` phase consumption undocumented**
   - What: Tool description lists them as launch-production-only fields but doesn't say which phase consumes them. Agent put them on `start`. Workflow ignored `productionProjectName` and defaulted to `eval-zcp-prod` (source name + `-prod` suffix); user wanted `myapp-prod`. Quote: "`productionProjectName` is consumed at the `launching` step, not at `start`. Pass it when you submit the `launchKey`, not before."
   - Layer: tool description in `internal/tools/workflow_launch.go` — phase consumption per input field; or atom `launch-input-phases.md`
   - Cross-session signal: yes — phase consumption affects every launch-production caller

4. **[RESPONSE-CONFUSING] After erroring `action="classify"`, status returned `ready-to-launch` — looked like the errored call advanced the workflow**
   - What: Quote: "The status response after I (incorrectly) tried `action='classify'` was briefly confusing because it showed `status='ready-to-launch'`, which made it look like the classify call had somehow advanced the workflow despite erroring. It hadn't... I couldn't tell from the envelope whether my classifications had been recorded." Recommendation: status reflects current phase, not last-call effect.
   - Layer: status envelope (`internal/workflow/engine.go` status output) — needs explicit "envClassifications: present/absent" or "lastAdvance" marker
   - Cross-session signal: yes — same staleness/lag pattern across all launch sessions

5. **[GUIDANCE-MISLEADING] classify-prompt heavy on grep-source workflow that doesn't work when mount missing**
   - What: classify-prompt guidance prescribes grep-the-source-tree workflow with ripgrep tables per language, but `/var/www/{hostname}/` isn't mounted in ZCP container by default. Quote: "If the mount isn't there, don't waste time trying — surface the proposed classification to the user and ask them to sanity-check."
   - Layer: atom `launch-classify-discover-shape.md` should add explicit fallback path
   - Cross-session signal: yes — dev-only, eww, lsc, part-2 all hit this

---

## Session: launch-production-laravel-showcase
**Task:** Launch Laravel full-stack (php-nginx app + postgres + valkey + object storage) to a separate `myapp-prod` project in eu-central. User generates one-shot API key on demand, deletes after.

**Findings:**

1. **[RESPONSE-CONFUSING] Service-level discover omits project-level envs that classify-prompt needs**
   - What: `zerops_discover hostname=appdev` returned only `APP_KEY`, `APP_DEBUG`, platform-injected stuff. But classify-prompt `classifications` array listed five DIFFERENT keys (`GIT_TOKEN`, `SESSION_SECRET`, `ZCP_API_KEY`, `JWT_SECRET`, `APP_KEY`) — project-level envs not surfaced by service-scoped discover. Agent classified from naming convention, not grep evidence.
   - Layer: `zerops_discover` — should surface project envs by default OR classify-prompt should bundle the project-env values directly so a second discover call isn't needed; atom should warn
   - Cross-session signal: yes — dev-only and part-2 (new-project-push-mode) flagged identically

2. **[GUESS-SHAPE] Input persistence across workflow calls undocumented**
   - What: Agent re-passed `productionProjectName` and `region` defensively on the classify follow-up because guidance "just said 'call back with `envClassifications`' but didn't say whether the earlier inputs persist." Re-passing worked; response's `inputs` block echoed them back so it probably would have been fine either way.
   - Layer: tool description for `zerops_workflow workflow="launch-production"` — explicit "inputs persist across calls" statement
   - Cross-session signal: yes — same uncertainty in fsp

3. **[GUESS-SHAPE] `envClassifications` JSON shape (string vs object) undocumented**
   - What: Guidance showed it inline as `{"KEY":"bucket",...}` but didn't spell out whether that's a stringified arg or a real object. Agent passed real object and got accepted. Quote: "worth knowing so you don't waste a round-trip stringifying."
   - Layer: tool description — explicit JSON-object vs JSON-string discriminator for `envClassifications` arg
   - Cross-session signal: low — single-tool shape clarity

4. **[GUIDANCE-OVERLOAD] classify-prompt guidance overkill for simple Laravel case**
   - What: classify-prompt guidance is "very long and very focused on grep-the-source-tree workflows, with detailed ripgrep tables per language. For this scenario — a small Laravel app with five obviously-named project envs, three of which are framework conventions and two of which are ZCP-internal — that whole apparatus is overkill." The "if genuinely ambiguous, default to plain-config" escape hatch matters more.
   - Layer: atom `launch-classify-discover-shape.md` — restructure with fast-path (recognize convention, classify, move on) at top, grep-tables in expandable detail section
   - Cross-session signal: yes — fsp also commented on the guidance being too long

5. **[GUIDANCE-MISLEADING] `ready-to-launch` is a hard stop, but agent felt temptation to keep calling tools**
   - What: Quote: "when the workflow returns `status='ready-to-launch'` it's a hard stop waiting on a user-supplied `launchKey`. Don't try to be clever and proceed... it's worth saying because the temptation when you're on a roll is to keep calling tools." Agent followed correctly but noted as cross-session risk.
   - Layer: atom `launch-ready-to-launch.md` should emphasize hard-stop semantics with explicit anti-pattern; current guidance reads as advisory
   - Cross-session signal: yes — relevant for all launch-production sessions

---

## Group-level patterns

- **envClassifications four-bucket model has no home for ZCP-managed envs.** `GIT_TOKEN`, `ZCP_API_KEY`, and similar ZCP-provisioned plumbing don't cleanly fit `infrastructure`/`external-secret`/`auto-secret`/`plain-config`. Every session in this group flagged this and guessed. Fix likely needs a fifth `zcp-managed` (or `regenerate-per-project`) bucket plus explicit examples for the two common keys. Layer: `internal/content/atoms/launch-classify-platform-envs.md`.

- **classify-prompt guidance is overloaded and contains stale example.** It prescribes grep-the-source-tree workflows with ripgrep tables per language (overkill for typical small apps), uses `workflow="export"` in an example for what is actually `launch-production`, and doesn't say "drop the hostname filter for project-level envs." Three sessions (fsp, lsc, dev-only) commented on this from different angles. Fix is one atom edit + example update. Layer: atom + handler text path.

- **Drift gate, stale-session persistence, and dashboard-side-effects form a single dangerous loop.** ready-to-launch captures baselines; user opening dashboard to mint API key bumps `projectEnvsDigest`; re-calling `action="start"` does NOT reset state; agent has to physically `rm` per-session files. Two part-1 sessions hit this; part-2 group sees same staleness pattern in `status` envelope. Fix has two prongs: (1) `action="start"` should supersede stale session by default, (2) workflow should collect token BEFORE start, not at ready-to-launch, eliminating the race window. Layer: `internal/tools/workflow_launch.go` ordering + state-reset logic.

- **Existing-project path is half-built — accepted by discover/scope but not reflected in downstream guidance.** `existingProjectId` is silently accepted; ready-to-launch returns new-project guidance (account-wide launchKey) anyway. Route-decision phrasing in CLAUDE.md ("deploy dev/stage to prod") matches existing-project intent but routes to new-project workflow. Two sessions (eww, existing-project-token) almost mis-routed. Fix needs path-aware guidance branching plus a route-decision clarification on creation-vs-targeting. Layer: handler guidance branching + CLAUDE.md route table.

- **`/var/www/{hostname}/` source mount missing across the eval container fleet.** Atoms tell agents to grep source to verify env-var usage; the mount is consistently empty. Four of five sessions in this group flagged it. Either provision the mount in eval seed, or atom must lead with the "if mount missing, classify from naming convention + ask user to sanity-check" fallback path. Layer: container boot shim + eval seed.

- **Tool-response/state staleness is a recurring confusion shape.** Status envelope can show `ready-to-launch` after a classify advance, a classify error, or an abandoned prior session — all three were observed across the group. Agents can't tell from the envelope what their last call actually did. Layer: status envelope needs an explicit "lastTransition" or "envClassificationsAccepted" field, not just current-phase reporting.
