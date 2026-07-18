# Group 5: launch-production sessions (part 2) + failed sessions

## Session: launch-production-new-project-push-mode
**Task:** Launch fresh `myapp-prod` project for a Node service, then set up GitHub Actions push-mode CI/CD on tag `v*.*.*`; user provides launch key and prod token.

**Findings:**

1. [STATE-CONFUSION] In-flight launch session blocks fresh-name intent
   - What: `status` reported an existing `launch-active` session with `targetProjectName="eval-zcp-prod"` and `nextCall` instructing resume — but user wanted `myapp-prod`. Agent ignored the suggested resume and re-called `start` with new inputs; it worked, but "I don't actually know whether under the hood it discarded the old session or replaced it — I didn't verify, and the response gave no hint either way."
   - Layer: `internal/tools/workflow_launch_production.go` (start-vs-resume semantics), `internal/content/atoms/launch-status-recovery.md` (guidance on when nextCall is suggestion vs command)
   - Cross-session signal: yes (mirrors pipeline-not-configured ambiguousChoices issue)

2. [GUESS-SHAPE] `zerops_discover service=X` drops `project.envs` block
   - What: Agent reflexively called `zerops_discover service="appdev" includeEnvs=true includeEnvValues=true` for project-level env values (`GIT_TOKEN`, `SESSION_SECRET`). Response returned only service-level envs and silently omitted project envs. Agent had to read twice before noticing. "Just call discover without `service` when you need project envs."
   - Layer: `internal/tools/discover.go` schema description, or the classify-prompt atom example which lists the correct shape but agent missed it
   - Cross-session signal: no (discover-specific)

3. [GUIDANCE-MISLEADING] `/var/www/<hostname>/` mount missing in eval container
   - What: CLAUDE.md (boot shim) says service code is SSHFS-mounted at `/var/www/{hostname}/`. In the eval container only `/var/www/CLAUDE.md` exists. Agent had to classify env vars from name + value-shape alone, no source verification. "If grep against `/var/www/<hostname>` fails, you're classifying from signals alone — name those signals explicitly to the user."
   - Layer: container boot shim `claude_container.md` (env-scoped) — should document mount-not-available fallback path; or eval seed should provision the mount
   - Cross-session signal: yes (same friction in pipeline-skip)

4. [ATOM-CONTENT] `GIT_TOKEN` classification ambiguous — no bucket fits ZCP-provisioned-but-not-reference envs
   - What: Agent uncertain whether `GIT_TOKEN` is `external-secret` (GitHub PAT bucket per worked examples) or `infrastructure` (managed-service references). `GIT_TOKEN` is minted by ZCP's `git-push-setup`, so dev value shouldn't propagate, but bucket definitions don't cover ZCP-provisioned-but-not-reference envs. Agent guessed `infrastructure` from knowing how `git-push-setup` works.
   - Layer: `internal/content/atoms/launch-classify-platform-envs.md` (bucket definitions need a "ZCP-provisioned" or "regenerate-per-project" category)
   - Cross-session signal: yes (pipeline-skip raised the same ambiguity for `ZCP_API_KEY`)

5. [GUIDANCE-MISLEADING] Route table doesn't mention status-first invariant for launch-production
   - What: CLAUDE.md route table sends "promote dev/stage to separate prod" → `workflow="launch-production"` but doesn't mention calling `action="status"` first to catch stale in-flight session. Agent ran both in parallel only because of discovery-floor rule, caught the stale session by luck. "Recommend always running status first on a launch-production intent specifically, because these are stateless multi-call and very easy to leave half-finished."
   - Layer: CLAUDE.md / atom `launch-intro.md` or route documentation
   - Cross-session signal: yes (status-first invariant relevant to all launch-production scenarios)

---

## Session: launch-production-pipeline-configured
**Task:** Conversational follow-up — "Production launched, ZCP says pipeline is fully configured. How do I deploy a new release?"

**Findings:**

1. [GUESS-SHAPE] Deferred-tool ToolSearch pattern not surfaced
   - What: Every Zerops MCP tool was listed as "deferred" at session start — invocations without `ToolSearch select:<name>` first return `InputValidationError`. "If you reflexively try to call `zerops_workflow` directly you'll get an `InputValidationError` and waste a turn."
   - Layer: harness/eval setup (deferred-tool prelude messaging) — likely not a ZCP code issue but a Claude Code MCP-init convention
   - Cross-session signal: low (suite-wide harness issue, not workflow-specific)

2. [STATE-CONFUSION] `status` envelope can lag actual launch state
   - What: `status` returned `kind: "launch-active"` with two entries in `ambiguousChoices`, both at `ready-to-launch`. User said pipeline was already launched. Agent took user's word over status output: "`status` can lag or reflect abandoned launch attempts that were never cleaned up. Don't assume `ready-to-launch` in the envelope means the launch hasn't happened; it might just mean a stale launch record."
   - Layer: `internal/tools/launch_state.go` / `launch_status_recovery.go` — stale-record cleanup or status output needs an "actual-vs-last-attempted" distinction
   - Cross-session signal: yes (pipeline-not-configured saw the same staleness)

3. [GUIDANCE-OVERLOAD] Post-launch deploy command not surfaced as first-class
   - What: Agent guessed at "git tag + git push" as the post-launch deploy command by inferring from `pipelineTagRegex` schema description (`^v\d+\.\d+\.\d+$` default). Knowledge query returned generic deployment-lifecycle docs, no specific "to ship a release through configured pipeline, push a tag" guidance. "The day-2 deployment loop isn't surfaced as prominently as the launch flow itself."
   - Layer: `internal/content/atoms/launch-pipeline-configured.md` (or recipe knowledge for post-launch loop) — needs concrete "git tag + push" example
   - Cross-session signal: no (post-launch-specific)

---

## Session: launch-production-pipeline-not-configured
**Task:** User says "production launched, no CD pipeline integration, there's a deep-link in the response — walk me through configuring it with same launchKey."

**Findings:**

1. [GUIDANCE-MISLEADING] `build-integration` error message misleads about scope
   - What: Agent called `build-integration` with `service="appdev"` (obvious tool given the description says "wires the ZCP-managed CI integration"). Got `SERVICE_NOT_FOUND: Service "appdev" is not bootstrapped`. Real issue is that `build-integration` targets the *source* project's services, not the prod project that `launch-production` spun up. "If you're at `launched` or `configuring-pipeline` and the user asks you to wire CD, don't reach for `build-integration`."
   - Layer: `internal/tools/build_integration.go` error message — should distinguish "service not bootstrapped" vs "wrong-project scope (use launch workflow's configuring-pipeline phase)"; also atom `launch-pipeline-configuring.md` or `launch-pipeline-configure-dashboard.md` should warn against this reach
   - Cross-session signal: no (build-integration-specific)

2. [STATE-CONFUSION] `ambiguousChoices` + sticky top-level `targetProjectName` — which is authoritative?
   - What: `status` returned two in-flight launches (`myapp-prod` + `eval-zcp-prod`) at `ready-to-launch`; top-level `targetProjectName` was set to one but guidance still said "pick one productionProjectName from ambiguousChoices." Agent passed `productionProjectName` explicitly to be safe. "When `ambiguousChoices` is present, always pass `productionProjectName` explicitly on the next call rather than trusting the top-level field to be sticky."
   - Layer: `internal/tools/launch_status_recovery.go` envelope shape — clarify which field is sticky vs hint; atom `launch-status-recovery.md`
   - Cross-session signal: yes (push-mode session saw similar ambiguity)

3. [STATE-CONFUSION] User's narrative state vs platform state diverged
   - What: User said "Production launched" past-tense, but `status` returned `ready-to-launch`. Agent almost walked them through dashboard steps assuming launch was done. "The status field is the source of truth — if it says `ready-to-launch`, the launch hasn't actually advanced past the gate, regardless of what the user thinks."
   - Layer: atom `launch-status-recovery.md` (or a meta-instruction) — agents should prefer status output over user framing for state claims
   - Cross-session signal: yes (pipeline-configured had same tension)

4. [GUIDANCE-MISLEADING] User-referenced "deep-link in the response" — but agent never saw the response that contained it
   - What: User said "there's a deep-link in the response" but agent was post-launch (or pre-launch?) and never reached the `configuring-pipeline` phase that emits it. Agent fell back to generic CI/CD guide. "Don't substitute the generic guide for the workflow's own output if you can avoid it — advance the workflow first, read what it says, then walk the user through that."
   - Layer: eval-prompt issue (the brief assumes a response that requires advancing through the workflow first) OR atom `launch-pipeline-configure-dashboard.md` should be loaded before workflow advances
   - Cross-session signal: low (this looks like eval-prompt over-specification)

5. [RESPONSE-CONFUSING] Bare `status` vs status-with-args returns same envelope
   - What: Agent called `status` once with no args (got launch envelope), then re-called with `workflow="launch-production" productionProjectName="myapp-prod"` and got identical envelope. The second call was unnecessary for state — "workflow/productionProjectName args only matter when you're about to mutate." Burned one round-trip on disambiguation that didn't actually narrow anything.
   - Layer: `internal/tools/workflow_launch_production.go` status path — schema description should clarify that args matter only for mutation, not for read
   - Cross-session signal: low (status-call semantics)

---

## Session: launch-production-pipeline-skip
**Task:** Launch production but skip ZCP's pipeline setup (`skipPipelineSetup=true`), user will deploy via `zcli push` from their own CI.

**Findings:**

1. [GUIDANCE-MISLEADING] `/var/www/<hostname>/` mount missing — no fallback path in classify guidance
   - What: Same mount-missing issue as push-mode session. Agent's grep against `/var/www/appdev/` failed; classified env vars from names alone but "presented the classification table as if I'd verified it, with rationales like 'no encrypted state to carry over' that I had no way to know. The user trusted it and approved." Classify-prompt guidance is grep-first with language-specific commands; no fallback when source isn't accessible.
   - Layer: `internal/content/atoms/launch-classify-platform-envs.md` — add explicit downgrade-confidence-when-no-source instructions; OR eval container should provide source mount
   - Cross-session signal: yes (push-mode hit identical friction)

2. [ATOM-CONTENT] `ZCP_API_KEY` classified as `infrastructure` — likely semantically wrong
   - What: Agent classified `ZCP_API_KEY` as `infrastructure` but realized in retrospect: "'Infrastructure' specifically means values resolved from a managed-service reference like `${db_*}`. A standalone API key — even one for Zerops itself — doesn't fit that." Workflow accepted without complaint. "Don't assume silent acceptance means semantically correct — the validator seems to check shape, not meaning."
   - Layer: `internal/content/atoms/launch-classify-platform-envs.md` bucket definitions; OR validator in `internal/tools/launch_envs.go` should warn on common mismatches
   - Cross-session signal: yes (push-mode raised same gap for `GIT_TOKEN`)

3. [RETRY-TOOL] `AskUserQuestion` failed with opaque one-word error
   - What: "`AskUserQuestion` failed twice with a one-word error: `Answer questions?`. No indication whether the schema was wrong, the tool was disabled, or the user declined. I tried adjusting the option label format (the '(Recommended)' suffix) and got the same error. I gave up and used prose."
   - Layer: harness `AskUserQuestion` tool error path — needs distinguishable failure modes; NOT a ZCP issue, this is Claude Code tool
   - Cross-session signal: low (host-tool friction)

4. [GUESS-SHAPE] When `skipPipelineSetup` applies — at `launching` vs `ready-to-launch`?
   - What: Agent figured out via schema that `skipPipelineSetup` is consumed at the `launching` step (with `launchKey` submission), not at the `ready-to-launch` gate. "I'm holding it to pass with the `launchKey` next turn — make sure you do too, don't try to send it earlier." This is correct but agent had to infer ordering from schema text rather than guidance.
   - Layer: `internal/content/atoms/launch-mutation-key-required.md` or `launch-pipeline-skipped.md` — should explicitly state when the flag is consumed
   - Cross-session signal: no (skip-specific)

5. [GUIDANCE-MISLEADING] CLAUDE.md vs schema disagreement on `targetService` pair half
   - What: CLAUDE.md says `targetService` accepts "either half of a standard pair" but tool schema says "pair-keyed dev-half hostname; passing a stage-half surfaces a scope-prompt blocker." Agent passed `appdev` (worked). "Probably safe to default to the dev half if you have a choice."
   - Layer: CLAUDE.md (boot shim or route table) should match `workflow.go:100` schema text
   - Cross-session signal: no (single docs drift, but worth fixing once)

---

## Failed sessions (eval-infra)

### existing-simple-mode-node-add-endpoint
- Error: `seed: wait for api: process BYlSq2KvRTAKovAxGyCBfg failed: unknown`
- Interpretation: Seed phase failed during initial Zerops setup — specifically the `wait for api` step on a process called `api`, which returned status `unknown` (not the expected terminal states). The process ran for ~57s before the seed gave up, suggesting either a Zerops platform transient (process stuck, status-poll timing race) or a malformed seed import that the platform couldn't classify. No agent turn ever ran; the scenario was aborted in pre-agent setup.
- Layer: eval seed pipeline — `eval/behavioral/internal/seed.go` (or similar) and Zerops API status-poll path (`internal/platform/poll.go` if reused, or eval-local poller). Worth checking whether `failed: unknown` is platform-classified or eval-injected.

### existing-standard-appdev-only-reminders
- Error: `seed: seed imported: cleanup: cleanup delete services: poll delete "api": process oij44O67QEONhVAHGAxIFg canceled`
- Interpretation: Seed phase succeeded the import step but failed during cleanup — the post-seed teardown tried to delete service `api` and the delete process was `canceled` (likely by a parallel operation, manual abort, or timeout). Duration was 3.47s, so this was fast-fail. Probably a side-effect of concurrent test runs in `eval-zcp` clobbering each other's resources, or a deliberate abort signal arriving during cleanup.
- Layer: eval seed pipeline cleanup path — same source area as above; also `eval-zcp` project hygiene (background cleaners or parallel-run isolation).

Both failures predate any agent turn, so they're characterized purely from `meta.json`. No agent friction findings exist for these sessions.

---

## Group-level patterns

- **State-staleness around `launch-active` / `ambiguousChoices`** (3/4 sessions). `status` envelope can hold abandoned `ready-to-launch` records, and the relationship between top-level `targetProjectName` and `ambiguousChoices` is unclear. Agents reached for defensive disambiguation (pass `productionProjectName` explicitly) but had to infer the rule. Likely fix: `internal/tools/launch_status_recovery.go` + atom `launch-status-recovery.md`.
- **Env-classification bucket definitions don't cover the realistic edge cases** (push-mode + pipeline-skip). `GIT_TOKEN` (ZCP-provisioned) and `ZCP_API_KEY` (Zerops's own API key) don't fit `external-secret` or `infrastructure` cleanly. Workflow validator accepts silently. Likely fix: `internal/content/atoms/launch-classify-platform-envs.md` needs an explicit "ZCP-provisioned-regenerate-per-project" bucket or a guidance note that the validator doesn't check semantics.
- **Source-code mount missing breaks classify-prompt's grep-first guidance** (push-mode + pipeline-skip). `/var/www/<hostname>/` is documented but eval containers only expose `/var/www/CLAUDE.md`. No fallback path in classify guidance for "source not inspectable." Agents either classified from names alone (push-mode, owned it) or presented classifications as if verified (pipeline-skip, didn't own it). Likely fix: container boot shim and/or eval-seed should align — either provide the mount or document the fallback.
- **Day-2 / route-table coverage gap for "launch is already done, what now"** (pipeline-configured + pipeline-not-configured). Post-launch deploy story isn't in any first-class atom; agents had to infer "git tag + push" from schema descriptions, and one reached for `build-integration` (wrong scope) before discovering it doesn't target prod-side services. Likely fix: a `launch-post-checklist.md` extension or new atom covering "release loop after launch completes."
- **Eval-infra seed brittleness** (2 failed sessions in the same suite). Both `existing-*` scenarios failed in seed phase with platform-process errors (`failed: unknown` and `canceled` during cleanup). Worth checking whether eval-zcp has stale resources accumulating, parallel-run collisions, or whether seed retry logic should distinguish "platform transient" from "import malformed."
