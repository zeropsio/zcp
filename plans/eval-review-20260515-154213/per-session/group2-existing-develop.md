# Group 2: existing/develop sessions

Run dir: `/Users/macbook/Documents/Zerops-MCP/zcp/eval/behavioral/runs/20260515-154213/`

## Session: cross-deploy-stage-promote-from-dev
**Task:** Promote `appdev`'s exact build to `appstage` without rebuilding from source.

**Findings:**

1. **[GUESS-SHAPE] `setup` is required for cross-deploy promotion but no upstream signal lists the available setup names**
   - What: First `zerops_deploy sourceService=appdev targetService=appstage` returned `SSH_DEPLOY_FAILED` / "Cannot find corresponding setup in zerops.yaml, please select with --setup". Tool description says `setup` is required when zerops.yaml has multiple setups — but there is no way to know that ahead of time. "The deploy tool's description mentions `setup` is 'required whenever zerops.yaml declares more than one setup,' but I had no way to know ahead of time how many setup blocks existed."
   - Layer: `internal/tools/deploy_ssh.go` (or its preflight) could probe service zerops.yaml and surface available setups in the error payload; atom `develop-first-deploy-promote-stage.md`
   - Cross-session signal: yes — same surface bites `git-push-setup-with-cicd-method-prompt` ("INVALID_ZEROPS_YML: The setup was not found. Field 'name' (appdev) rejected")

2. **[ATOM-CONTENT] CLAUDE.md container-shim claim of SSHFS mount at `/var/www/{hostname}/` is misleading when mounts aren't active**
   - What: Agent checked `ls /var/www/` and found only `CLAUDE.md`, despite the boot-shim stating "Service code SSHFS-mounted at `/var/www/{hostname}/`." Quote: "those mounts weren't active. If you need to inspect service files, either start a develop workflow and use `zerops_mount`, or just SSH in directly." Agent flagged the cost: "don't burn a tool call on `Glob **/zerops.yaml` hoping to find it locally."
   - Layer: `internal/content/templates/claude_container.md:3` — claim needs a conditional clause ("mounts materialize after develop/bootstrap workflow") or move to develop-scoped guidance
   - Cross-session signal: yes — `git-push-setup-with-cicd-method-prompt` repeats this exact friction ("That's not a broken environment — that's a service ZCP hasn't adopted yet. Don't go hunting for the code"); `develop-add-managed-dep-to-existing` independently flags "`/var/www/appdev` mount doesn't exist until bootstrap completes the provision step"

3. **[GUIDANCE-MISLEADING] `setup` field name is hostname-shaped in shape but refers to zerops.yaml profile name**
   - What: Agent had to pass `setup=prod` (not `setup=stage` or `setup=appstage`). The deploy tool's description does say this, and the failure's `suggestedAction` repeats it, but: "it's easy to skim past on the first read because every other field in the call is hostname-shaped." Implicit pitfall in field naming + adjacent hostname-keyed args.
   - Layer: `deploy_ssh.go` field name / description (rename `setup` → `zeropsYamlSetup` is heavyweight; minimum is reordering the suggestedAction emphasis); atom `develop-first-deploy-promote-stage.md` should call this out as anti-pattern
   - Cross-session signal: yes — `git-push-setup-with-cicd-method-prompt` independently rediscovered same pitfall

4. **[STATE-CONFUSION] `zerops_workflow action="status"` surfaces unrelated stale launch-active envelope and invites resumption**
   - What: Agent got `kind: "launch-active"` for an `api-prod` launch-production workflow mid-flight, completely unrelated to the dev→stage promotion task. "That's completely unrelated to the dev→stage promotion the user asked for — same project, different workflow, abandoned or paused. Don't get pulled into resuming it." Status is meant as recovery primitive but priority ordering surfaces launches over the user's current request.
   - Layer: `internal/tools/launch_status_recovery.go` priority logic; spec `docs/spec-workflows.md §P4` — status response should perhaps separate "stale launches" from "your current track" or annotate that launch-active is not actionable for non-launch requests
   - Cross-session signal: yes — `export-buildfromgit-self-snapshot` reports the exact same launch-active leak ("two stale launch-production workflows that had nothing to do with the current request… launches outrank bootstraps")

---

## Session: delivery-git-push-actions-setup
**Task:** Configure future delivery via GitHub Actions from `main` on `https://github.com/example/weather-app`; no real GitHub secret yet, just prep the path.

**Findings:**

1. **[GUIDANCE-MISLEADING] `git-push-setup` walkthrough chains config steps as if strictly prerequisite, but actions are independent**
   - What: First call (no `remoteUrl`) returns "walkthrough" text describing: set `GIT_TOKEN` → stamp URL → commit/push → `build-integration`. Agent flagged this reads as a chain but isn't: "in practice `build-integration` is independent — it just generates the workflow YAML and tells you which two GitHub secrets to set. For a 'prep the path, no real secrets' task you can skip everything between the stamp and the build-integration call." Agent ran `build-integration` and `close-mode` in parallel and both worked without `GIT_TOKEN` set.
   - Layer: `internal/tools/workflow_git_push_setup.go` walkthrough text — needs an explicit "these three actions are orthogonal; sequencing matters only at push time"
   - Cross-session signal: yes — `export-buildfromgit-self-snapshot` reports same finding ("steps 1 and 2 are independent and either order works"); `git-push-setup-with-cicd-method-prompt` finds different aspect of same surface

2. **[SCOPE-CONFUSION] `git-push-setup` first call (info-only) vs second call (mutation) discriminator not obvious from envelope**
   - What: First call returns `status: "walkthrough"` with `nextStep` telling to re-call with `remoteUrl`. Agent: "A future agent could easily mis-read this and either (a) try to do the SSH commit and deploy steps unnecessarily, or (b) try to pass `remoteUrl` on the first call." The two-call pattern needs a clearer "first call is read-only probe" signal.
   - Layer: `workflow_git_push_setup.go` response envelope — make the two phases obviously distinct (e.g. `phase: "discovery"` vs `phase: "configured"`)
   - Cross-session signal: low — git-push-setup specific

3. **[RESPONSE-CONFUSING] `build-integration` returns both `workflowFile` and `alternateWorkflowFiles`; default is buried**
   - What: Agent had to re-read the response to figure out which was the default. "The `workflowFile` field is the recommended one (the setup-aware `zcli push` variant); `alternateWorkflowFiles` is the simpler `zeropsio/actions@v1` variant that only works when `zerops.yaml` has a single setup." The `nextStep` text does clarify ("Keep the default setup-aware zcli workflow unless you are certain the repository has only one setup") but is at bottom of response.
   - Layer: `internal/tools/workflow_build_integration.go:195-228` — restructure to lead with single primary file + clearly-labeled "alternates only when X"
   - Cross-session signal: yes — `git-push-setup-with-cicd-method-prompt` flagged actively-misleading default value (see that session #3)

4. **[GUESS-SHAPE] `closeMode` JSON shape ambiguous between `{hostname:value}` map and nested-value form**
   - What: Schema says `additionalProperties: {type: string}`. Tool description lists `closeMode` as `{hostname:value}`. Agent guessed flat `{"app": "git-push"}` and it worked. "It's just a flat `{hostname: 'auto'|'git-push'|'manual'}` map — easy once you've done it once."
   - Layer: `internal/tools/workflow.go:46` jsonschema annotation — example value is too terse; add concrete `{"appdev":"git-push"}` example
   - Cross-session signal: low — but a one-line schema fix

5. **[GUIDANCE-MISLEADING] CLAUDE.md routing table suggests develop session for any platform-side configuration**
   - What: Agent noticed they could (correctly) skip the develop session for pure deploy-config plumbing. "The CLAUDE.md routing table sends 'build/edit/scaffold' to develop, but these config actions are designed to be called standalone. A future agent might reflexively start a develop session; you don't need to."
   - Layer: routing-table prose in `claude_container.md` / boot-shim; could explicitly call out the three orthogonal config actions as session-free
   - Cross-session signal: low — this agent self-corrected; recurrence unknown

---

## Session: develop-add-managed-dep-to-existing
**Task:** Add Redis cache to project and wire to `appdev` for caching `/api/dashboard` heavy Postgres query; don't touch `appstage`.

**Findings:**

1. **[EVAL-PROMPT] Scenario brief references endpoint and behavior that doesn't exist in the deployed code**
   - What: User's brief described `/api/dashboard` endpoint with heavy Postgres query; actual `app.ts` had only `GET /` and no relevant table. Agent had to fabricate the entire setup: "I made up an `orders` table, seeded 500 rows, wrote an aggregation query, and called that the 'heavy query.'" This means the eval mostly measured "agent invents convincing fixture" rather than "agent adds Redis caching to existing endpoint."
   - Layer: `eval/behavioral/scenarios/fixtures/` for this scenario — fixture needs to actually contain the endpoint the prompt references
   - Cross-session signal: no — fixture-specific

2. **[GUESS-SHAPE] `bootstrapMode` for "add new managed dep to existing standard-mode pair" is undocumented**
   - What: Existing `appdev`/`appstage` pair is in standard mode. Agent submitted `bootstrapMode: "dev"` with `isExisting: true` because user said "don't touch appstage." Plan accepted, but agent flagged guess. "The docs talk about `bootstrapMode` as if it sets the mode on a fresh runtime, and don't directly cover 'I'm adding a new dependency to an existing standard-mode runtime, what bootstrapMode do I lie about.'" Confirmed in transcript — passed `bootstrapMode: "dev"`.
   - Layer: atom `bootstrap-mode-prompt.md` or new atom for "adding deps to existing pair" case; possibly the handler should infer mode from existing pair when `isExisting: true`
   - Cross-session signal: yes — `export-buildfromgit-self-snapshot` and `git-push-setup-with-cicd-method-prompt` both report adopt-pair shape inference as undocumented

3. **[SCOPE-CONFUSION] Bootstrap route menu offers `adopt` and `classic`; intent "add new managed dep" maps unclearly when existing services also need adoption**
   - What: Agent was offered `adopt` (because `appdev`/`appstage` lacked ZCP metadata) and `classic`. "The menu doesn't make it obvious that `adopt` is wrong when your primary goal is to *create* a new service even if there are also un-adopted runtimes hanging around. Pick `classic` whenever you're creating new infra; `adopt` is for when the only outstanding work is attaching metadata."
   - Layer: atom `bootstrap-route-options.md` — explicit "primary intent: create-new → classic, even if adoption also needed" rule
   - Cross-session signal: yes — `git-push-setup-with-cicd-method-prompt` independently rediscovered "adopt is for attaching metadata to existing services"

4. **[ATOM-CONTENT] `/var/www/<host>/` mount appears only after bootstrap provision close**
   - What: Agent tried `Read`/`Glob` on `/var/www/appdev/` before bootstrap completed, got "directory does not exist." "That's by design — the auto-mount fires on bootstrap close — but if you're used to `/var/www/<host>` always being readable, you'll bump into this."
   - Layer: `claude_container.md` boot-shim — conditional must be explicit (see also cross-deploy finding #2)
   - Cross-session signal: yes — same surface as cross-deploy #2 and git-push-cicd #6

5. **[RESPONSE-CONFUSING] Env-var catalog doesn't visually flag `isReference: true` vs literal values**
   - What: `cache` service exposes `connectionString` (reference) + `hostname` + `port`. Agent: "the doc table doesn't flag which keys are references vs literal values; you have to notice the `isReference: true` flag in the `zerops_discover` output. Worth a glance." If using a client that wants a URL, `connectionString` exists but isn't surfaced.
   - Layer: `zerops_discover` response presentation (or atom `bootstrap-env-var-discovery.md`) — visual marker for reference-typed vars
   - Cross-session signal: low — discover-output specific

6. **[ATOM-CONTENT] Migration `ON CONFLICT DO NOTHING` on SERIAL with no unique constraint silently re-seeds every deploy**
   - What: Agent's own seed migration used `INSERT … SELECT FROM generate_series(1, 500) ON CONFLICT DO NOTHING` on `orders.id` SERIAL — which has no other unique constraint, so the conflict guard is a no-op. `zsc execOnce ${appVersionId}` only deduplicates once-per-deploy, not once-ever. Agent flagged but didn't fix. Recipe-knowledge candidate.
   - Layer: recipe knowledge for nodejs/Postgres seed migrations (`internal/knowledge/recipes/`) — not an atom (framework-specific)
   - Cross-session signal: low — gotcha from agent's own fabricated fixture

---

## Session: export-buildfromgit-self-snapshot
**Task:** Switch `app` from direct push to buildFromGit on `https://github.com/example/teamapi`; export self-snapshot import.yaml.

**Findings:**

1. **[RESPONSE-CONFUSING] `SERVICE_NOT_FOUND: Service "X" is not bootstrapped` reads as "service missing" but means "no ZCP ServiceMeta"**
   - What: `git-push-setup` on `appdev` returned `SERVICE_NOT_FOUND: Service "appdev" is not bootstrapped`. Service plainly exists (discover lists it, deploys work). "What 'bootstrapped' actually means here is 'has a ZCP ServiceMeta record,' not 'exists in the project.'… The fix is fast but the error message reads like the service is missing."
   - Layer: `internal/tools/workflow_git_push_setup.go:81`, `workflow_build_integration.go:78`, `workflow_close_mode.go:95` — error message should say "has no ZCP metadata; run `bootstrap route=adopt` first" instead of `SERVICE_NOT_FOUND`
   - Cross-session signal: yes — `git-push-setup-with-cicd-method-prompt` independently rediscovered same trap

2. **[GUESS-SHAPE] Adopt-pair discover plan shape is assembled from inference**
   - What: Agent had to construct `[{runtime: {devHostname, stageHostname, type, bootstrapMode: "standard", isExisting: true}, dependencies: [{hostname, type, resolution: "EXISTS"}]}]`. "The tool schema describes each field but the exact combination — `isExisting: true` on the runtime, `resolution: 'EXISTS'` on the dep, `bootstrapMode: 'standard'` for the pair — I assembled from inference. Got lucky on the first try; a future agent might not."
   - Layer: atom `bootstrap-adopt-discover.md` (currently 26 lines, lacks an end-to-end example of pair adoption); possibly a fixture example in the atom
   - Cross-session signal: yes — `develop-add-managed-dep-to-existing` and `git-push-setup-with-cicd-method-prompt` reported same inference gap

3. **[STATE-CONFUSION] `zerops_workflow action="status"` surfaces stale launch-active envelope and invites resumption**
   - What: Status returned `kind: "launch-active"` for two stale launch-production workflows unrelated to the current request. "The envelope wants you to resume them; the user wanted nothing of the sort. Don't trust status to tell you about the workflow you're trying to start — it surfaces *any* active workflow in priority order, and launches outrank bootstraps."
   - Layer: `internal/tools/launch_status_recovery.go` priority logic / spec `docs/spec-workflows.md §P4`
   - Cross-session signal: yes — identical observation in `cross-deploy-stage-promote-from-dev`

4. **[GUIDANCE-MISLEADING] `git-push-setup` walkthrough ordering implies sequence dependency that doesn't exist**
   - What: Walkthrough order: (1) set `GIT_TOKEN`, (2) stamp `remoteUrl`, (3) commit/push. Agent called step 2 before step 1 successfully. "The 'do these in order' framing is wrong; steps 1 and 2 are independent and either order works. Useful to know if you're trying to parallelize."
   - Layer: `workflow_git_push_setup.go` walkthrough copy — strip ordering implications
   - Cross-session signal: yes — same finding in `delivery-git-push-actions-setup`

5. **[GUIDANCE-MISLEADING] git-push-setup walkthrough culminates in `zerops_deploy strategy="git-push"` (manual posture) when user often wants `close-mode: git-push` (automatic-on-close)**
   - What: "If the user wants `close-mode: git-push` (which is what 'switch to git-push deploy flow' usually means), the close-mode action is what triggers pushes — on develop session close, automatically. The walkthrough doesn't acknowledge close-mode at all, so you can finish the setup and not realize you've configured a manual-push posture instead of an automatic-on-close one."
   - Layer: `workflow_git_push_setup.go` walkthrough text or atom `setup-git-push-container.md` — needs explicit "manual vs auto-on-close decision" branch with reference to `close-mode`
   - Cross-session signal: yes — `delivery-git-push-actions-setup` independently flagged the three-action orthogonality

6. **[ATOM-CONTENT] Export response returns live secret values verbatim with no classification or warning**
   - What: User asked for self-snapshot. "The export returns live secret values verbatim — `APP_KEY`, `JWT_SECRET`, `ZCP_API_KEY`, `VSCODE_PASSWORD`, anything in `project.envVariables` and `envSecrets`. No classification hints, no `<@generateRandomString>` substitution, no warning in the response." Agent flagged asymmetry with launch-production which has `envClassifications` step. Note: actual `export` workflow does have classify-prompt path — but agent never routed through it (used `bootstrap` workflow instead), so the asymmetry is at the routing level, not the handler.
   - Layer: `internal/tools/workflow_export.go` — should be the routed entry for "export this project to re-importable import.yaml"; routing table / atoms need to make `workflow="export"` discoverable for this user request. Atom: maybe new atom for "snapshot-with-secrets" warning, or surface in `export-classify-envs.md`
   - Cross-session signal: medium — points at workflow routing / discoverability rather than specific to export

---

## Session: git-push-setup-with-cicd-method-prompt
**Task:** Set up git push AND auto-deploy stage on every push to main, in ONE flow. GitHub Actions push-mode using raw `zcli push` (not marketplace action), with project-scoped Zerops token.

**Findings:**

1. **[GUIDANCE-MISLEADING] No upstream signal that `git-push-setup`/`build-integration`/`close-mode` must reference the dev half of a pair**
   - What: First call `service="appstage"` was rejected with "stage is the build target, never push source." "Nothing upstream tells you that. The bootstrap response says 'stage: appstage' under 'Stage:' but doesn't say 'and by the way, git-push lifecycle calls always reference the dev half.'" Source error at `workflow_git_push_setup.go:90`.
   - Layer: atom set (`setup-git-push-container.md`, `develop-close-mode-git-push.md`) — explicit "lifecycle calls reference dev half only" invariant; bootstrap close envelope could surface "use dev for lifecycle config" hint
   - Cross-session signal: yes — adjacent to cross-deploy finding #3 (hostname-shape confusion)

2. **[GUIDANCE-MISLEADING] `build-integration action=actions` returns a workflow file that defaults to dev half, even when session intent is stage**
   - What: Agent flagged this as "the most actively misleading thing in this flow." Default workflow deploys to **appdev** with `--setup appdev` even when user's stated session intent was "auto-deploy stage on every push to main." Confirmed in `workflow_build_integration.go:198-200,305` — `setupName` defaults to the runtime hostname (dev half). Agent had to hand-write workflow targeting appstage's service-id with `--setup prod`. "If the user wants stage on push, do not copy the workflow file the tool returns."
   - Layer: `internal/tools/workflow_build_integration.go` — handler should accept a target half hint (or default to stage half when meta has a stage half configured); spec-workflows §4.3 needs a stage-vs-dev CI/CD target rule
   - Cross-session signal: low — actions-specific but high-severity for any "deploy stage on push" use case

3. **[RETRY-TOOL] `zerops_deploy strategy=git-push` requires `setup` even though it's a no-build operation**
   - What: First call failed `INVALID_ZEROPS_YML: The setup was not found. Field 'name' (appdev) rejected.` — tool tried to default `setup=appdev` against a zerops.yaml with only `prod`/`dev` setup blocks. "For a strategy that doesn't build anything, this validation is surprising. Pass `setup=dev` (or whatever block matches the dev half) explicitly and you're fine."
   - Layer: `internal/tools/deploy_git_push.go` preflight — either don't preflight `setup` for git-push strategy, or default smarter from zerops.yaml content (read available setup names; pick the one matching `BuildRole=dev`)
   - Cross-session signal: yes — same surface as cross-deploy #1 (setup-required preflight without setup-name discovery)

4. **[ATOM-CONTENT] `ci-cd` knowledge doc is biased toward marketplace action; raw zcli appears only under GitLab CI**
   - What: User explicitly requested raw `zcli push` on GitHub Actions. "Its GitHub example uses `zeropsio/actions@v1.0.2`. The raw `zcli push` form only appears under the GitLab CI heading. If the user asks for raw zcli on GitHub Actions (this user did), don't trust the doc layout — you assemble the workflow from the GitLab snippet plus the `--service-id`/`--setup` flag table at the bottom." Confirmed at `internal/knowledge/guides/ci-cd.md:22, 60`.
   - Layer: `internal/knowledge/guides/ci-cd.md` — add raw-zcli GitHub Actions section explicitly; restructure so CI-system axis (GitHub/GitLab) is orthogonal to method axis (marketplace-action/raw-zcli)
   - Cross-session signal: low — knowledge-doc layout specific

5. **[RESPONSE-CONFUSING] `SERVICE_NOT_FOUND: not bootstrapped` blocking lifecycle is the only signal that adopted services lack ZCP metadata**
   - What: Agent went straight at `git-push-setup`, got "not bootstrapped," then had to start `bootstrap action=start workflow=bootstrap route=adopt`. "The user said 'my first deploy just succeeded' — meaning the services exist and are running — but that wasn't the same as ZCP knowing about them. If you see services in `zerops_discover` that came from a recipe `buildFromGit`, assume they need `route=adopt` first. Don't try `git-push-setup` to probe; just adopt."
   - Layer: `zerops_discover` could surface `bootstrapped: false` prominently per service so agent doesn't probe via lifecycle calls; atom `idle-adopt-entry.md` should explicitly call this out
   - Cross-session signal: yes — same finding in `export-buildfromgit-self-snapshot` #1

6. **[ATOM-CONTENT] `/var/www/{hostname}/` mount appears only after bootstrap-adopt provision step**
   - What: "The CLAUDE.md promise that 'service code is SSHFS-mounted at `/var/www/{hostname}/`' is conditional. Those dirs didn't exist when I started; they materialized after bootstrap-adopt's provision step finished (`autoMounts` in the response confirms it). If you `ls /var/www/appdev` cold and get 'No such file or directory,' that's not a broken environment — that's a service ZCP hasn't adopted yet."
   - Layer: `internal/content/templates/claude_container.md:3` — same fix as cross-deploy #2
   - Cross-session signal: yes — three of five sessions in this group bumped into this

7. **[GUIDANCE-OVERLOAD/ATOM-CONTENT] `GIT_TOKEN` placeholder value validated only at push time; no cheap upstream probe**
   - What: `GIT_TOKEN=ghp_1a2b3c4d5e6f7g8h9i0j1k2l3m4n5o6p7q8r` looked plausible, agent treated it as real. Failure only surfaced at full push ceremony. "`gh auth login --with-token` returning `HTTP 401: Bad credentials` is the cheap probe — run it before doing the full commit + `zerops_deploy strategy=git-push` ceremony."
   - Layer: atom `setup-git-push-container.md` — add cheap-probe step (e.g. validate via GitHub API ping before commit/push ceremony)
   - Cross-session signal: low — placeholder-detection specific

---

## Group-level patterns

- **`/var/www/{hostname}/` mount-existence claim in `claude_container.md` is unconditionally true in the boot-shim but conditional in reality** — three of five sessions (cross-deploy, develop-add-managed-dep, git-push-setup-with-cicd) each independently bumped into "the mount isn't there yet" and wasted at least one tool call. Single highest-ROI fix: rewrite `internal/content/templates/claude_container.md:3` to make the post-bootstrap-provision dependency explicit.

- **`SERVICE_NOT_FOUND: not bootstrapped` is a misleading error code on three lifecycle handlers** (`git-push-setup`, `build-integration`, `close-mode`). All three emit identical wording at handler files lines 81/78/95. Reads as "service is missing" rather than "no ZCP metadata yet, run adopt." Recurs in two sessions (`export-buildfromgit-self-snapshot`, `git-push-setup-with-cicd-method-prompt`); both agents had to discover by trial. Either rename the error code (`ERR_NO_ZCP_METADATA`?) or change message wording + add explicit `nextStep` pointing at `bootstrap route=adopt`.

- **`setup` field on deploys requires zerops.yaml setup-name knowledge that no upstream tool surfaces** — cross-deploy (`SSH_DEPLOY_FAILED`/setup-required) and git-push-cicd (`INVALID_ZEROPS_YML`/setup-not-found) both tripped on this. Available setup names are extractable from the source service's zerops.yaml at preflight time — handler could read it and either default smarter or surface available names in the error.

- **Stale launch-active envelope from `zerops_workflow action="status"` invites resumption regardless of current request** — two of five sessions (cross-deploy, export-buildfromgit) hit this. Status priority logic in `launch_status_recovery.go` puts launches above bootstraps unconditionally; for a non-launch user request, surfacing a stale launch derails. Priority should consider current intent or annotate "not relevant to your current request."

- **Cross-cutting guidance gap: deploy-config three orthogonal actions (`close-mode`/`git-push-setup`/`build-integration`) are framed as a chain in walkthrough text** — both `delivery-git-push-actions-setup` and `export-buildfromgit-self-snapshot` independently flagged that walkthrough order implies prerequisite chains that don't exist. Walkthrough text in `workflow_git_push_setup.go` needs an explicit "these three are orthogonal" callout; also `close-mode` is missing from the walkthrough entirely yet often is what the user wants.

- **Adopt-pair `discover` plan shape (isExisting + resolution=EXISTS + bootstrapMode=standard) is assembled by inference in three of five sessions** — atom `bootstrap-adopt-discover.md` does not include an end-to-end pair-adopt example. Even succeeding agents flag "got lucky on the first try."
