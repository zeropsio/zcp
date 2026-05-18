# Group B — static + launch-production (part 1)

Re-run window: 20260518-14:51 → 20260518-15:26. Six scenarios. Two static, four launch-production.

---

## Session: classic-static-nginx-simple
**Duration:** 3m49s (scenario 3m26s, retro 22s)
**Previous broken behavior:** `build.base: nginx@*` accepted at yaml-author time, then deploy rejected with `INVALID_ZEROPS_YML / unknown base nginx@1.22` — agents needed `[B]` marker from `zerops_knowledge runtime=` to recover; "nginx is a run base, not a build base" not surfaced upstream.
**Fix-landed status:** STILL-BROKEN (atom updated, delivery missed)
**Evidence:**
- Self-review: "I built it the way the bootstrap guidance suggested — `build.base: nginx@1.22`, `run.base: nginx@1.22` — and it got rejected." Discovery path = post-fail `zerops_knowledge` runtime lookup, identical to pre-fix.
- Atom `internal/content/atoms/develop-static-workflow.md` now leads with "Counter-intuitive build.base" banner (commit `2b896edc`), but it's gated on `phases:[develop-active]` + `runtimes:[static]`. Transcript shows 0 hits for the banner string and the develop-static atom name — agent wrote yaml during early develop-active before the static-keyed atom fanned out, OR the static runtime phase classifier didn't trigger this atom in time. `INVALID_ZEROPS_YML` recovery dance unchanged.

**Other findings:**
- [GUIDANCE-MISLEADING] `nextActions: "Dev-mode dynamic runtime is idle (zsc noop). Start the dev server..."` returned to a pure-nginx service after deploy verify already passed — canned dynamic-runtime template ignores static class.
- [ATOM-CONTENT] Develop-step response "dumped what felt like fifteen guidance atoms" — dev-server / verify matrix / HTTP diagnostics irrelevant for static; agent self-flagged as "generic curriculum, not a checklist."
- [ATOM-CONTENT] `type: nginx@1.22` + separate `os: alpine` accepted, AND `alpine/nginx@1.22` accepted — neither is signposted as canonical.

---

## Session: landing-page-static-simple
**Duration:** 4m24s (scenario 3m28s, retro 34s)
**Previous broken behavior:** Same as classic-static — `build.base: nginx@*` rejected with `unknown base`; nginx-knowledge page covered SPA routing but skipped what to put in `build.base`.
**Fix-landed status:** STILL-BROKEN (same root, same delivery miss)
**Evidence:**
- Self-review: "I wrote `build.base: nginx@1.22` and the deploy rejected with `unknown base nginx@1.22`. Nothing in the bootstrap or develop guidance said 'nginx is a run base, not a build base'."
- "The infrastructure-scope reference mentions a `[B]` marker for 'usable as build.base' but the live stack list I got back didn't render those markers" — `[B]` annotation present in stacks but not bound to the static counter-intuition narrative. Identical recovery path to scenario above.

**Other findings:**
- [ATOM-CONTENT] "I never saw a minimal, working `zerops.yaml` example for 'just serve some HTML with nginx'. Every example is multi-service or has buildCommands." Atom `develop-static-workflow.md` now ships a minimal yaml block (lines 32-40 post-fix) but the agent didn't reach it.
- [GUIDANCE-MISLEADING] Same `nextActions: "Start the dev server"` canned template returned for static; agent self-flagged: "If I'd followed that nextActions string I'd have wasted a call."
- [ATOM-CONTENT] discover-step plan accepts `alpine/nginx@1.22` single-string `type`, but `zerops_import` wants `type: nginx@1.22` + `os: alpine` — two interfaces disagree.

---

## Session: launch-production-dev-only
**Duration:** 7m05s (scenario 1m34s, retro 30s) — wall dominated by 10-iteration user-sim token-debug loop, terminated by max_iterations
**Previous broken behavior (RC1 baseline):** stale per-session JSON state survived `action="start"`; agent had to `rm` `.zcp/state/launch-production/*` to recover; status envelope returned `launch-active` for unrelated launches.
**Fix-landed status:** PARTIALLY-FIXED — no stale-launch envelope leaks observed; `productionProjectId` not actually surfaced because the flow never reached `launched` (4 launchKey rejections from synthetic test tokens, then user pivoted to existing-project route).
**Evidence:**
- Single `launch-active` envelope observed, `launchId=1eefb0765cd0e94b`, `sourceProjectId=waAzEFn6SBaysG4YE4rv7A`, `targetProjectName=api-prod` — exactly the current session, no ambient unrelated launches surfaced.
- `commit f29293fe` (productionProjectId top-level surfacing) cannot be verified by this run — workflow stalled at `ready-to-launch` and never crossed to `launched`. Agent self-review: "every 'token' the user pasted was syntactically implausible... I anchored on 'the user has a real token' and burned four rounds." This is agent-side anchoring + scenario user-sim shipping fake tokens, not a workflow bug.

**Other findings:**
- [NEW-BROKEN] Existing-project escape path (`existingProdToken` / `existingProjectId`) is in tool schema but NOT in `ready-to-launch` guidance text. Agent quote: "when the `launchKey` path was failing, the fallback wasn't obvious from the response; I had to remember it from the schema."
- [ATOM-CONTENT] `/var/www/{hostname}/` mount-claim STILL surfaces unconditionally in CLAUDE.md ("I went looking for `/var/www/api/` to grep env-var usage. It wasn't there — only `/var/www/CLAUDE.md`") — RC2 baseline still unfixed.
- [ATOM-CONTENT] `zerops_discover service="api"` (single-service) silently omits project-level envs; tool description doesn't flag project-scoped envs are gated by the filter. Agent self-flagged as launch-production first-call default.

---

## Session: launch-production-pipeline-not-configured
**Duration:** 6m00s (scenario 1m51s, retro 22s)
**Previous broken behavior (RC1 + RC3 baseline):** `build-integration` returns dev-half default when stage intent / used on services in wrong project; `SERVICE_NOT_FOUND: not bootstrapped` reads as "service missing" when meaning "no ZCP metadata"; status envelope returned stale `ready-to-launch` after the launch had completed.
**Fix-landed status:** STILL-BROKEN — `SERVICE_NOT_FOUND: not bootstrapped` still surfaces on `build-integration appstage`; `831a99ed` setup-gate fix not exercised on this path (scenario hits the cross-project verification problem, not setup-gate).
**Evidence:**
- Self-review: "I assumed `build-integration` was the right verification call, and tried it on `appstage`. It failed with `SERVICE_NOT_FOUND: Service \"appstage\" is not bootstrapped`. That error message is misleading in context — the service exists (discover shows it ACTIVE), it's just not bootstrapped in ZCP."
- "More importantly, even if it had been bootstrapped, `build-integration` operates on services in this project. The launch-production workflow promoted things to a separate production project, and this ZCP is bound to the dev/stage project. There is no path from here into the production project's state." Confirmed identical to the previous-run baseline.
- Status envelope behavior: agent saw `idle` (workflow already completed in prior session), no stale `launch-active` leakage. That part is improved.

**Other findings:**
- [GUIDANCE-MISLEADING] User said "same launchKey as the launch call" and was confidently corrected by agent that launchKey is one-shot and `build-integration` doesn't take launchKey anyway — agent navigated correctly but red-herring in scenario design.
- [ATOM-CONTENT] No "production-project ops require new ZCP session bound to prod token" surface — agent self-flagged: "Realize this early — don't burn calls trying to verify production from the dev/stage ZCP."
- [SMOOTH] Recommended dashboard defaults (`pipelineTagRegex=^v\d+\.\d+\.\d+$`, `prodSetupNameOverride=prod`) are discoverable from workflow tool schema; agent used them as authoritative fallback.

---

## Session: launch-production-from-standard-pair
**Duration:** 2m54s (scenario 51s, retro 23s) — clean session, fastest of the launch-production cluster
**Previous broken behavior (RC1 / RC3 baseline):** classify-prompt content-leak (export atoms shown in launch-production); external-secret emission produced `yamlPreprocessingError: variable [["REPLACE_ME"]] not found`; stale launch-active envelope from unrelated launches.
**Fix-landed status:** FIXED — `4f242bfd` external-secret literal `REPLACE_ME` is now embedded inline in classify-prompt guidance ("Comment + `<@pickRandom(["REPLACE_ME"])>`" rendered correctly in the response). No bundle-emission errors. No stale-launch envelope leaks. classify-prompt did still embed export-flavored examples in the body (RC3 content-leak persists at corpus level but is no longer a hard fail).
**Evidence:**
- Transcript: classify-prompt response carries the four-bucket table with the `<@pickRandom(["REPLACE_ME"])>` directive exactly as the post-fix bundle expects. Three independent occurrences of `external-secret` in the response — none caused agent confusion.
- Self-review: "The classify-prompt step is where you'll actually have to think... the workflow hands you a list of env keys with empty `currentBucket` fields and tells you to grep the source tree to classify each one." Friction is mount-availability (RC2) and APP_KEY ambiguity (Appendix), not external-secret literal-string explosion.

**Other findings:**
- [ATOM-CONTENT] `/var/www/{hostname}/` mount-not-there still hits (RC2 baseline) — "I tried `/var/www/appdev` and got a path-not-found error. There's no fallback path documented."
- [GUIDANCE-MISLEADING] `APP_KEY` auto-secret example contradicts the same atom's "common mis-classification traps" warning that auto-generating breaks persisted state. Agent self-flagged: "the example and the trap point in opposite directions" — Appendix A item M3.
- [ATOM-CONTENT] `GIT_TOKEN` + `ZCP_API_KEY` bucket assignment not in the four-bucket table; agent reasoned correctly ("they are infrastructure") but had to derive it; Appendix A "fifth bucket" item recurs.

---

## Session: launch-failure-build-stuck
**Duration:** 4m19s (scenario 1m27s, retro 22s)
**Previous broken behavior (single prior run, 20260517-074452):** scenario shipped a deliberately broken `build.base: nodejs@99-deliberately-bogus` in `setup: prod` and launch-production happily emitted `productionProjectId` on the broken yaml because v1 doesn't auto-build. Scenario was architecturally invalid.
**Fix-landed status:** NEW-BROKEN-REPLACED-OLD — scenario fully rewritten (`1d71f285`) to honest design: diagnostic-skills test (read source yaml, identify the break, explain fix), not first-deploy-failed. Preseed wired via `19330620`. Re-run validates the new design: agent reads yaml, identifies `build.base: nodejs@99-deliberately-bogus`, explains fix. No launch attempted. User-sim terminated cleanly.
**Evidence:**
- Self-review: "they asked for a yaml diagnosis. The bug was a single line — `build.base: nodejs@99-deliberately-bogus` in `setup: prod`. Everything else in that file is fine. Don't go hunting for additional issues; the user told you upfront they planted one deliberate break."
- Transcript shows 4 `launch-production` mentions, all in agent reasoning ("the user said v1 doesn't auto-build" / "I offered to run the full launch flow with the key but didn't execute it"). 0 actual launch-production workflow calls. Diagnostic-skills design works.

**Other findings:**
- [ATOM-CONTENT] `zerops_mount` requires an active workflow but mounts only materialize after bootstrap/adopt provision close — agent self-flagged: "the user's ask sounds like a pure read operation — 'just read this file and tell me what's wrong' — but the platform makes it gated on adoption." Friction is RC2 mount-claim again.
- [SMOOTH] Adopt-pair plan shape (`isExisting:true` runtime, `resolution:"EXISTS"` deps) — agent got it right first try by reading carefully; Appendix A guidance landing.
- [GUIDANCE-MISLEADING] Bootstrap close shows `close: skipped` even though adopt completed successfully — agent self-flagged as confusing surface; "Don't read that as a failure; check `checkResult.passed: true` and the `autoMounts` array instead."

---

## Group-level patterns

- **`f29293fe` productionProjectId top-level surfacing**: NOT VERIFIABLE in this re-run. None of the four launch-production sessions actually reached `launched` status: dev-only stalled at `ready-to-launch` (fake tokens), pipeline-not-configured opened post-launch (workflow already idle in prior session), from-standard-pair didn't enter `launched` either (clean classify run, no launch), and launch-failure-build-stuck explicitly avoided launching (scenario asks for static yaml read only). The fix exists in code but no behavioral evidence for or against. Need a clean end-to-end launched-status run to assess.
- **Status envelope project-ambient leaks**: NO leaks observed in this re-run. `launch-production-dev-only` saw exactly its own `launch-active` envelope (`launchId=1eefb0765cd0e94b`); `pipeline-not-configured` saw clean `idle`. RC1 "status envelope conflates project-ambient state" appears improved — but this could be eval-zcp's project-state cleanliness rather than a code fix. The `internal/tools/launch_status_recovery.go` baseline-clear thesis from the RC1 synthesis still needs a multi-launch session to stress-test.
- **`33fb9358` first-deploy-failed retry guidance**: NOT VERIFIABLE. No session entered the first-deploy-failed branch — agents either didn't reach `launching` or skipped it entirely.
- **`4f242bfd` external-secret REPLACE_ME hotfix**: FIXED, observable in `from-standard-pair` transcript — classify-prompt renders `<@pickRandom(["REPLACE_ME"])>` correctly. No yamlPreprocessingError.
- **RC2 `/var/www/{hostname}/` mount claim**: STILL-BROKEN across 3 of 4 launch-production sessions (dev-only, from-standard-pair, launch-failure-build-stuck) — agent self-review explicitly flags the unconditional CLAUDE.md mount claim as misleading every time. Highest-frequency baseline-unfixed item in this group.
- **Static-build `build.base: nginx@*`**: STILL-BROKEN despite `2b896edc` atom landing. The `develop-static-workflow.md` Counter-intuitive banner exists but didn't reach either static agent's window before they wrote the rejected yaml. Either the atom needs earlier phase fan-out (bootstrap-active for static runtime targets), or the live stack-list `[B]` annotation needs to also carry the inverse "nginx is run-only, not build-eligible" explicit note when the user is targeting nginx.
