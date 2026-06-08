# flow-eval battery — post-consolidation verification (2026-06-04)

**Binary under test:** working tree `main` = f2399c2e (released v9.110.0).
**Purpose:** verify the consolidation ship (R1/R5/R6/R7 + dev-only + MCP
resource-surface removal) didn't regress + the new live gates pass.
**Mode:** observation only — friction → SEPARATE plans/commits, no auto-fix.

Scope: 47 container scenarios + 5 local-mode. 44 runnable; 3 blocked on creds
(LAUNCH_KEY ×2 + EXISTING_PROJECT_ID/PROD_TOKEN ×1 — not in cred.txt).

---

## Scenario verdicts

### ✅ export-buildfromgit-self-snapshot — R7 HARD pre-gate: PASS
suite 20260604-192113 · 2m43s scenario · model opus-4-6

**Gate result:** R7 scaling round-trip is CORRECT. The export bundle's
`services[]` carried `verticalAutoscaling` + min/maxContainers and PASSED live
import-schema validation (→ compose-ready). Verified against live schemas:
- zerops.yml `run` = `additionalProperties:false`, fixed key set, **no**
  `verticalAutoscaling` → platform rejects `run.verticalAutoscaling`. ZCP's
  `ValidateZeropsYAMLStructure` rejection MATCHES the platform (not over-strict).
- import schema carries `verticalAutoscaling`/`minContainers`/`maxContainers`/
  `cpuMode` under `services[]` — exactly where R7 projects them.

The `run.verticalAutoscaling` in the deployed `zerops.yaml` is the fixture's
**intentional** validation-gate trigger (scenario tests `bundle.errors →
validation-failed`). Working as designed.

**Friction surfaced (actionable, observation-only):**
1. **validation-failed → fix path is over-ceremonious.** Export guidance: "Fixing
   live `/var/www/zerops.yaml` requires the develop workflow." Agent ran a full
   develop start→Read→Edit→close cycle to delete one field. Agent's own doubt:
   "not sure the develop workflow was strictly necessary since the mount was
   already live." OPEN QUESTION: is the source tree write-accessible after
   adopt/bootstrap without a develop session, or does develop establish the
   SSHFS mount? If writable → guidance overstates; if not → guidance is correct
   but should say why. Needs a focused check before any edit.
2. **git-remote-URL escape hatch missing.** Export derives `buildFromGit` from
   `git remote get-url origin`. To change it the agent only saw heavy
   `git-push-setup` (needs PAT); the simple `git remote set-url origin <url>`
   via SSH worked and export picked it up ("cache drifted, live value wins").
   Guidance could mention the lightweight path.
3. **dual-purpose atom text noisy.** classify-prompt + validation-failed covered
   in one atom block — when validation-failed fires, the agent first suspects
   mis-classification before reading `errors[]`. Minor.
4. **adopt scope ambiguity not pre-warned.** Two type-matched runtimes
   (`nodejs@22`) → `scope=[appdev,appstage]` returns ErrAdoptPairingChoice. The
   error message is excellent (copy-paste plan templates) but nothing warns up
   front that scope alone won't resolve type-matched pairs → one burned
   round-trip. Minor (self-correcting via the good error).
5. **"close develop before re-export" ordering implicit.** Minor.

**Routing/behavior confirmed GOOD:** agent routed natural language → `workflow=
"export"` (not the legacy `zerops_export` tool — the pre-Phase2 9× misroute is
fixed); walked all three export calls; accepted server `suggestedBucket` for env
classification; pushed back correctly on the git URL (used live, warned on drift).

---

### ✅ Batch 1 — consolidation delta (5/5 OK, 0 harness fail)
suites 193141 / 193736 / 194228 / 194849 / 195359 · all functionally PASS.

| Scenario | Tests | Verdict |
|---|---|---|
| kanban-laravel-minimal-dev-only | R3 dev-only narrow | ✅ narrowed correctly; `recipeNarrow="dev-only"` belongs on `complete step="discover"` |
| recover-failed-buildfromgit-missing-dep | R6 retryCall→ACTIVE | ✅ gate cleared via retryCall; see R6 note below |
| launch-production-from-standard-pair | R7 scaling+transforms | ✅ launch OK; scaling-transform path likely not exercised (default scaling) |
| launch-production-dev-only | R7 + dev-only launch | ✅ launch OK; same caveat |
| resume-after-compaction | R5 status recovery | ✅ recovered; sharpened adopt + closeMode findings |

**No consolidation regression.** dev-only, R6 recovery, R7 launch, R5 recovery all run end-to-end.

---

## Cross-cutting findings (ranked by recurrence × actionability)

### F1 — Adopt-discover guidance makes a FALSE "plan derived for you" promise ★ TOP
**4 scenarios** (export, launch-from-pair, launch-dev-only, resume-after-compaction).
Guidance verbatim: *"You do not hand-write the nested adopt plan — `scope` is just
the hostname list; the plan is derived for you."* This is **flatly wrong for two
same-base-type runtimes** — the handler refuses (`ErrAdoptPairingChoice`, by design
per "pairing is never guessed") and demands an explicit plan. Classic **tell-vs-check
drift / single-owner violation**: the TELL promises unconditional derivation, the
CHECK refuses for same-type pairs. The discover error itself is excellent (paste-ready
templates). **Fix (separate plan):** adopt-discover guidance must condition the
promise — "two same-type adoptable runtimes → submit an explicit plan; the discover
error hands you the templates." Also clarify `scope` semantics on `start` vs
`complete step="discover"` (agents pass it on both, unsure which acts).

### F2 — closeMode / auto-close discoverability (R5)
**3 scenarios** (recover-failed, launch-dev-only? , resume-after-compaction). After
verify passes, session stays auto-close-blocked because `closeMode` is unset; the
instruction is "buried in a wall of guidance." Agents risk leaving dangling sessions.
**Fix (separate plan):** surface the closeMode requirement at the moment verify
passes (in the verify/develop response), not only in the general guidance wall.

### F3 — zerops_knowledge / guidance verbosity buries the relevant block (R1 below-cap)
recover-failed: dev-mode setup block drowned among production Gunicorn/migration
content. **This is the deferred R1 below-cap-relevance concern with live evidence**
(`plans/backlog/r1-context-relevance-below-cap.md`). Trigger to promote may be met.

### F4 — dev-mode 502 is by-design but diagnosed too late
resume-after-compaction: dev setup has no `run.start` (idles on `zsc noop`); the 502
+ "check your app is on the right port" reads as misconfig. The clarifying fact lives
in the service CLAUDE.md, only readable AFTER SSHFS mount post-bootstrap-close.
**Fix (separate plan):** surface "502 on dev-mode = `zerops_dev_server action=start`,
not a bug" in the verify response / develop guidance, earlier than the mounted file.

### F5 — `outOfScope` vs `scope` semantics counterintuitive
resume-after-compaction: standard pair auto-includes both halves; to work on appdev
only you pass appstage in `outOfScope` and do NOT list it in `scope`. Explained only
after the call; topology locks at start. Minor-medium.

### F6 — validation-failed recovery over-ceremony + git-remote escape hatch (export)
See export section above (F-export-1/2). OPEN: is the tree writable post-adopt
without a develop session?

### N1 — ToolSearch deferred-tool dance + MCP reconnect (HARNESS/ENV, not ZCP)
Every scenario: `select:zerops_workflow` (short name) fails → needs `mcp__zerops__…`;
MCP server cycles and tool schemas must be reloaded. This is Claude-Code deferred-tool
+ MCP-connection behavior, **not a ZCP handler bug**. Only ZCP-authorable mitigation:
the `claude_container.md` boot shim could instruct "load all zerops tools upfront with
fully-qualified names." Low priority (self-corrects).

### R6 note — retryCall completeness
recover-failed: agent said `diagnosedFailureClass` was NOT in the emitted retryCall
template and added it from the diagnosis. It's `?`-optional so the gate likely cleared
without it — but verify in transcript whether retryCall should pre-fill it for a
truly one-call clear. TODO: transcript check.

---

### ✅ Batch 2 — classic routes (7/7 PASS: go, bun, nginx, php-maria, python-pg, rust-pg, api-node)
suites 200145/200617/201620/202031/202802/203155/204822. All functionally PASS.
(5 initially build-FAILED on Karel's mid-save `internal/tools/` WIP — `handleLaunchReset`
ctx-refactor caught between signature+call-site edits; re-ran clean once tree compiled.
Karel then stopped editing → no more race.)

**New findings (sharpened across all 7):**

### F7 — develop-active response is a WALL OF TEXT ★★ NEW TOP (universal)
**All 7 + Batch 1.** "300+ lines", "2000+ words", "wall of atoms"; the `Next:` line is
buried at the very bottom; "most of it doesn't apply." Every runtime class + deploy
mode + caveat dumped regardless of the actual scenario. Agents cope by ctrl-F-ing the
one relevant section. **This is R1 below-cap-relevance with overwhelming live evidence**
(`plans/backlog/r1-context-relevance-below-cap.md`) — the develop-active render is THE
top noise source. Promote trigger clearly met: demote-below-cap by DecisionContext
(runtime class, mode, first-deploy vs iterate). The single highest-leverage fix.

### F11 — recipe buildFromGit → `deployed=false` disconnect ★ (Information-Contract)
**rust-pg, api-node, python-pg.** After recipe bootstrap, buildFromGit has cloned+built+
deployed the app (container ACTIVE, mount populated), but develop reports `deployed=false`
and drops the agent into the FIRST-DEPLOY branch (scaffold zerops.yaml, write code, first
deploy) — none of which applies. Agent must run a deploy "purely to flip the workflow's
internal tracking bit"; a literal agent re-scaffolds existing code. **The workflow asserts
a stale stored intent over live reality** — the exact Information-Contract violation ("source
of truth is the live platform, not ZCP's stored copy"). Candidate real handler fix: detect
buildFromGit-deployed state → skip first-deploy scaffold guidance. HIGH.

### F9 — build.base ≠ service type for implicit-webserver runtimes
php-maria: `php-apache@8.5` rejected → needs `php@8.5`. nginx static: `build.base` must be
`nodejs@22`, not `nginx`/`static`. Agents set build.base = run.base = type, get rejected;
the `[B]` build-eligible marker in the stacks listing goes unnoticed; the fix is one
`zerops_knowledge` lookup away but consulted AFTER the failure. Surface correct build.base
at scaffold time (schema knows build-eligibility). MED-HIGH.

### F8 — plan JSON shape for `complete step=discover` is guessed from examples
go, bun, nginx, rust-pg: no explicit simple-mode / static plan template; agents infer
"simple = standard minus stageHostname" + `dependencies:[]`. Confirms backlog
`plan-schema-author-friction.md` with live evidence. MED.

### F10 — `setup` param on deploy for multi-setup recipes not surfaced
rust-pg, php-maria: zerops.yaml has dev+prod setups; deploy guidance omits `setup`; agent
infers `setup=dev`. The workflow KNOWS the setup count → should tell the agent. MED.

### F13 — `recipeNarrow="dev-only"` has no schema enum (tell==check)
python-pg, api-node: param is `type:string` no enum; agent guesses the exact token
(`dev-only` vs `devOnly`/`dev_only`) + placement. Small single-owner fix: drive the schema
enum from `workflow.RecipeNarrowDevOnly`. SMALL/concrete.

### F12 — dev-mode 502 = by-design (reconfirm F4)
python-pg, api-node, rust-pg: idles on `zsc noop`, nothing listening → 502 reads as failure;
must `zerops_dev_server action=start` before verify. Surface earlier than the mounted file.

### F15 — closeMode/auto-close discoverability (reconfirm F2)
php-maria, python-pg: `autoCloseStatus: gated` until closeMode set; buried.

### F14 — rust recipe: stale `target/debug` from buildFromGit poisons dev deploy (SIGSEGV)
rust-pg: `cargo run` debug build hit SIGSEGV in crc32fast from a stale `target` inherited
from the release buildFromGit env. RECIPE knowledge finding → rust recipe gotchas
(`rm -rf target/debug` before first dev deploy). Not an atom.

### F16 — self-deploy `deployFiles:[.]` rationale unclear for compiled langs (minor)
go: agent followed the [.] rule (correct, DM-2) but wanted a compiled-artifact path; the
"why [.] even for a self-contained binary" rationale could be clearer.

---

### ✅ Batch 3 — greenfield + recipe (8/8 PASS, 0 races)
gf-node-pg, gf-fullstack, gf-website, landing, recipe-laravel-{min,showcase},
recipe-nestjs, recipe-nextjs. All functionally PASS.

**Reconfirms:** F7 (wall-of-text: gf-node-pg/website/landing — "firehose, ~20% relevant"),
F8, F12, F15, F16 (self-deploy [.]: gf-website "whole-dir IS the artifact, costs a deploy").

**New findings:**

### F17 — two-phase bootstrap start (route-menu→commit) is non-obvious ★ (universal)
**Every scenario.** First `action="start"` returns `kind="route-menu"`, NO session; must
call start AGAIN with `route=` to commit. Agents pattern-match "start = started" → try
`complete` on a non-existent session. Documented but surprising; universal friction.
Ease: louder route-menu signal or AGENTS.md routing note. MED (recovers, but everyone hits it).

### F22 — bootstrap close: two guidance surfaces CONTRADICT on verify ★ (single-owner)
rcp-nestjs: post-close MESSAGE says "run zerops_verify"; close-step DETAILED guide says
"do NOT run zerops_verify here (probes app layer, only after develop writes code)." Direct
contradiction; the detailed guide is right (verify at bootstrap = 502 noise). Concrete
fixable inconsistency — two surfaces, one owner needed. MED.

### F19 — `run.start` uses exec semantics, not shell (cost a deploy)
gf-fullstack: `HOSTNAME=0.0.0.0 node server.js` → `exec: HOSTNAME=0.0.0.0: not found`;
fix `env HOSTNAME=... node server.js`. Guidance flags HOSTNAME reserved but not exec
semantics for run.start (dev_server docs mention exec but agent didn't connect it). MED-HIGH.

### F18 — `setup` deploy example uses literal "prod" that won't match hostname-named setups
gf-node-pg: guidance shows `setup="prod"` but agent's zerops.yaml used `setup: appstage`;
literal "prod" would fail. Guidance should say "match YOUR zerops.yaml setup name," not a
literal. Relates F10. MED.

### F23 — recipe simple-mode vs user "dev+stage" intent not reconciled
rcp-nextjs: user asked dev+staging; recipe is simple (single app); `planMode:"simple"`
bred doubt + near-miss submitting a standard plan (rejected). Discover guidance could state
"this recipe's simple mode IS the dev+stage story." LOW-MED.

### F20 — Next.js dev OOM; minRam guidance has no number (recipe)
gf-fullstack: "set minRam high enough" w/o a number → OOM → scaled 2GB + NODE_OPTIONS.
RECIPE knowledge (nextjs gotchas: dev needs ≥2GB). Not an atom.

### F21 — verify `degraded` on root non-2xx cosmetic but alarming (mostly handled)
gf-fullstack/rcp-nestjs: detail text now says "accept as cosmetic" → guidance present. LOW.

### F24 — service type displays OS-prefixed (`alpine/php-nginx@8.4`) vs imported bare (cosmetic)
rcp-laravel-showcase: same service; programmatic compare would trip. CanonicalBareForm
handles validation; only display differs. LOW.

---

## Running tally (after 4 rounds launched)
**Rounds 1-3 = 20/20 functional PASS, 0 consolidation regressions, 0 build races (post-Karel-stop).**
Top findings by impact: **F7** (develop wall-of-text = R1 promote) · **F11** (recipe
buildFromGit→deployed=false, Information-Contract) · **F17** (two-phase start universal) ·
**F22** (verify guidance contradiction) · **F19** (run.start exec) · **F9** (build.base≠type) ·
**F1** (adopt "plan derived for you" false promise) · **F2/F15** (closeMode discoverability).

### ✅ Batch 4 — develop+env+existing (6/7 PASS; 1 transient seed-fail)
develop-loop, add-managed-dep, env-cross-ref, cross-deploy-promote, appdev-reminders,
kanban-std-pair PASS. `existing-simple-mode-node-add-endpoint` FAILED in seed
(`wait for api: process … failed: unknown` — fixture pre-deploy died on platform, not ZCP)
→ RE-RUN needed. Build now v9.111.0/f7692619 (Karel's launch_reset committed).

**Reconfirms:** F7 (develop-loop "brutal S/N ~10%", appdev "200+ lines"), F17, F2/F15,
**F1 now ~6× rock-solid** (cross-deploy-promote + appdev-reminders both hit same-type adopt
rejection + praised the error), F21.

**New findings:**

### F25 — develop "Next:" action LIES for code-only edits ★★ (Information-Contract)
develop-loop + add-managed-dep: prominent `Next: zerops_deploy` is generated from session
state (`deployed=true` → wants another deploy), NOT from intent. For a code-only edit deploy
is WRONG (deploy = zerops.yaml changes only; code-only = edit mount + `dev_server restart`).
Agent almost ran an unnecessary deploy. **The headline action misdirects** because it derives
from a state bool, not the work intent. HIGH.

### META — "guidance derived from a state-bool proxy, not live reality / intent" ★★
**F25 + F11 share one root:** nextActions/branch-selection keys on `deployed` (a stored bool)
instead of (a) what the user actually intends or (b) what the platform actually shows
(buildFromGit already deployed). This is the Information-Contract principle exactly — record
& reflect live truth, don't re-assert a stale proxy. Candidate consolidation root for a
follow-up plan (NOT this session — observation only).

### F27 — `zerops_env` guidance pseudo-code ≠ actual schema (tell==check)
kanban-std-pair: guidance shows `zerops_env action="set" scope="project" key=X value=Y`;
real call is `project:"true" variables:["X=Y"]` (array). Hand-authored example drifted from
the tool schema. Single-owner fix (derive example from schema). MED.

### F30 — adopt plan must be authored BLIND (zerops.yaml unmountable pre-adopt-close)
cross-deploy-promote: SSHFS mount appears only after adopt closes, so the agent can't read
zerops.yaml to inform the adopt plan — picks dev/stage from intent alone, then reads yaml for
the setup name. Compounds F1 (must author the same-type-pair plan AND can't see the yaml). MED.

### F26 — `outOfScope` semantics misunderstood (reconfirm F5)
add-managed-dep: outOfScope hostnames must ALREADY be in scope (demote a pair-half), not
exclude an unrelated service. "dev-only work on a standard pair" guidance misleads. MED.

### F32 — "add managed dep to existing service" has no clear plan path
add-managed-dep: plan schema is greenfield-shaped; bolting one managed service onto an
existing app requires inferring isExisting:true + resolution:CREATE + bootstrapMode:dev.
Common case, no dedicated path. Relates plan-schema-friction backlog. MED.

### F29/F31/F28 (minor) — SHARED-dep plan shape unclear (env-cross-ref); cross-runtime HTTP =
internal-DNS literal buried (env-cross-ref); `attestation` valid-content unspecified (kanban).

---

## Running tally (after 5 rounds launched)
**26/27 functional PASS** (1 transient seed-fail to re-run), **0 consolidation regressions**,
**0 build races since Karel stopped editing.**

**Headline findings by impact:**
1. **F7** develop-active wall-of-text — universal → R1 below-cap relevance PROMOTE.
2. **META (F11+F25)** guidance keyed on state-bool not live-reality/intent — Information-Contract root.
3. **F1** adopt "plan derived for you" false promise (same-type pairs) — ~6×, tell-vs-check.
4. **F17** two-phase bootstrap start — universal surprise.
5. **F2/F15** closeMode/auto-close discoverability — universal.
6. **F22** bootstrap-close verify guidance contradiction; **F19** run.start exec; **F9** build.base≠type;
   **F27** zerops_env example≠schema; **F18** setup="prod" literal trap.

### ✅ Batch 5 — adopt/discover + git-push (7/7 PASS, 0 races)
adopt-pair, discover-adopted, discover-resumable, delivery-gitpush, gitpush-then-actions
(GITHUB_PAT ✓), gitpush-cicd-prompt, verify-subdomain. All PASS.

**Reconfirms:** **F1 now ~10× — THE dominant finding** (every same-type-pair adopt:
adopt-pair, discover-resumable, gitpush-then-actions, gitpush-cicd-prompt). verify-subdomain
confirms scope-only DOES work for SINGLE-service adopt — so the false promise is specifically
multi-service same-base pairs. N1 ToolSearch select-short-name-fails (discover-adopted explicit:
CLAUDE.md uses short names, `select:` needs `mcp__zerops__` prefix).

**New findings:**

### F34 — build-integration defaults to STAGE even when user explicitly said DEV ★ (META)
gitpush-then-actions: user said "GitHub Actions builds na appdev"; build-integration set
buildTarget=appstage/buildSetup=prod (standard-pair CI→stage convention). User corrected.
**Third META instance** (with F11, F25): default/convention applied over the user's explicit
intent, no discrepancy surfaced. Agent should flag stated-target vs defaulted buildTarget. MED.

### F35 — resumed session, hollow discover step → deadlock + misleading error ★ (R5 recovery)
discover-resumable: session died mid-bootstrap; resume shows discover=complete but plan
payload lost; completing provision fails `BOOTSTRAP_NOT_ACTIVE "requires plan from discover"`
with message "Start bootstrap first" (WRONG — a session exists with a hollow step). No way to
re-complete a completed step; only escape is `reset`. Concrete error-message fix + maybe a
re-complete-discover path. MED-HIGH.

### F39 — httpSupport / L7 routing established by DEPLOY, not nginx/port tools ★
verify-subdomain: 502 persisted after enabling subdomain + fixing in-container nginx
(localhost:80=200) because `httpSupport=false` until a real `zerops_deploy` runs the build
pipeline with zerops.yaml. No tool/guidance said what controls httpSupport — agent inferred
it. Knowledge gap on the verify-subdomain-recovery surface. MED.

### F36 — git-push-setup probe error is a catch-all (exit 128 / GIT_TOKEN_INVALID)
gitpush-cicd-prompt: doesn't distinguish bad-token / no-repo-scope / repo-missing / network;
agent dropped to raw curl to triage (401 vs 404 vs 200). Could classify like deploy-failure. MED.

### F37 — no dashboard-only git-push path; token MUST pass through git-push-setup
gitpush-cicd-prompt: dashboard-set GIT_TOKEN can't complete the flow (handler requires the raw
token param; no probe+stamp-only mode). If user won't share token in chat → stuck. MED (design).

### F33/F38/F41 (minor) — build-integration↔git-push-setup ordering undiscoverable until error
(delivery-gitpush); git-push-setup `inputsRequired` groups integration as if same call (F38);
F9 extended — static run.base=nginx@1.22 ≠ service type `static`, build.base=nodejs@22 (verify-subdomain).

---

## Running tally (after 6 rounds launched)
**33/34 functional PASS**, 0 consolidation regressions, 0 build races since Karel stopped.

**THE top findings (by recurrence × impact):**
1. **F1** adopt "plan derived for you" false promise — **~10×, every same-type-pair adopt** — tell-vs-check, THE finding.
2. **F7** develop-active wall-of-text — universal → R1 below-cap relevance PROMOTE.
3. **META (F11+F25+F34)** guidance/default keyed on state-bool/convention, not live-reality/intent — Information-Contract root.
4. **F17** two-phase bootstrap start; **F2/F15** closeMode discoverability — both universal.
5. Concrete fixables: **F35** hollow-resume deadlock+wrong msg · **F22** verify-guidance contradiction ·
   **F19** run.start exec · **F9** build.base≠type · **F27** zerops_env example≠schema · **F36** git-push probe catch-all.

### ✅ Batch 6 — launch-production variants (7/7 PASS, 0 races)
existing-token, existing-webhook, laravel-showcase, new-project-push, pipeline-{configured,
not-configured,skip}. **R7 POSITIVE: zero scaling friction reported** (no minContainers/
maxContainers/cpuMode issues in any) — launch scaling transforms clean from the agent's POV.
The full launch friction is adopt-precondition + source-control gate + post-state-query, NOT
the R7 projection I shipped.

**Reconfirms:** **F1 — ALL 7 hit it; now ~16× total, every adopt-of-pair** (incl. php-nginx
in laravel-showcase). Beyond doubt THE single highest-value fix. **F36** git-push probe
catch-all (~4×: existing-token, existing-webhook, pipeline-not-config). **F48** wall-of-text
(source-control-required dumps full blocker table; actual blockers in a small bottom array).

**New findings:**

### F43 — launch-production has NO post-completion state query ★ (R5 gap)
pipeline-configured: after `launched`, `action="list"`→empty, `action="status"`→`phase:idle`;
no way to retrieve which pipeline integration was configured. User: "ZCP told me it's
configured" — agent can't verify, "flying blind." Launch state isn't persisted/queryable.
MED-HIGH — relates to R5 lifecycle/status-recovery.

### F42 — launch-production `inputs` accumulator silently drops existing-project params on call 1
existing-token, existing-webhook: `existingProjectId`+`existingProdToken` on first call →
`inputs:{}` + productionProjectName blocker; "looks like your params were ignored" (they
register but aren't echoed until scope completes). Misleading accumulator. MED.

### F45 — source-control gate is an UNLISTED multi-round preamble
new-project-push, pipeline-skip: the documented phase list (`scope-prompt→classify-prompt→
ready-to-launch→launching→configuring-pipeline→launched`) omits the source-control gate,
which is a 3-5 re-call preamble (git-push-setup → dirty-tree → head-not-pushed → build-int).
Agents don't expect the shape. MED.

### F44 — bootstrap-adopt is a HARD precondition for launch-production, not in routing
new-project-push (+launch-dev-only): first launch call → `service-not-bootstrapped`; must
adopt first. AGENTS.md routing omits this. Check adoptionState → adopt → launch. MED.

### F46 — two-token model (source PAT vs production CI auth) unexplained
existing-webhook: user asked dashboard-OAuth, didn't get why a PAT is needed; git-push-setup
PAT (GIT_TOKEN on source) vs production CI/CD auth are separate, never distinguished. MED.

### F50/F47/F49 (minor) — build-integration warn "ask user configure/skip" even after user
stated preference (existing-webhook, relates F34); build-integration targets SOURCE not prod,
unstated (pipeline-not-config); sourceContext promotionHeadline/targetServiceCanonical naming
inversion (existing-token, laravel-showcase).

---

## Running tally (after 7 rounds launched)
**40/41 functional PASS**, **0 consolidation regressions**, **R7 launch+export both live-clean**,
**0 build races since Karel stopped.**

**THE top findings (final-ish ranking):**
1. **F1** adopt "plan derived for you" false promise — **~16×, EVERY same-type-pair adopt** — single highest-value fix.
2. **F7** develop-active wall-of-text (+F48 source-control table) — universal → R1 below-cap relevance PROMOTE.
3. **META (F11+F25+F34+F50)** guidance/default keyed on state-bool/convention, not live-reality/intent.
4. **F17** two-phase start · **F2/F15** closeMode — universal.
5. **R5 gaps:** **F35** hollow-resume deadlock+wrong msg · **F43** launch no post-state query.
6. Concrete fixables: **F36** git-push probe catch-all (~4×) · **F22** verify-guidance contradiction ·
   **F19** run.start exec · **F9** build.base≠type · **F27** zerops_env example≠schema · **F45** unlisted launch preamble.

### ✅ Batch 7 — multi-service replays (2/3 PASS)
cadence-spec, pm-byty PASS. `cadence-multiservice-build-run2-replay` FAILED =
`scenario spawn: timeout: context deadline exceeded` (largest 8-service build replay
exceeded the harness deadline — scenario size, not a ZCP bug). RE-RUN.

**POSITIVE:** both replays are env-var-audit-replay scenarios; **neither reported
HOSTNAME / `${db_connectionString}` friction** → the Wave-0-4 env-var deflections HELD.

**Reconfirms (elevated):**

### F16↑ — recipe zerops.yaml ships narrow cross-deploy deployFiles that BREAK self-deploy ★
cadence-spec (strong) + gf-website: recipe YAML uses cherry-picked `deployFiles`
(`.next/standalone/~`, etc.) authored for the buildFromGit→runtime CROSS-deploy. When the user
enters the SELF-deploy path (replace code, push from mount, no sourceService), Zerops enforces
`deployFiles:[.]` (DM-2) and the narrow patterns are rejected (`INVALID_ZEROPS_YML`/DM-2). The
change cascades (start path moves: `server.js`→`.next/standalone/server.js`). develop guidance
buries this in "Self-deploy destruction risk"; the `Next: zerops_deploy` footer implies you can
just deploy. MED — develop guidance should flag "self-deploying a recipe service → set
deployFiles:[.] + fix start path" when source==target on a recipe-origin yaml.

Reconfirms F7 (cadence-spec "wall of atoms"), F12 (pm-byty 502 + "run.start empty" warning noise).
Framework gotchas (Drizzle relations, dnd-kit version, NextAuth v5, ts-node `.js` ext) are
app-code knowledge, NOT ZCP/atoms (note for recipe gotchas where a recipe owns the stack).

---

## Running tally (after 8 rounds launched)
**42/44 functional PASS** (1 transient seed, 1 harness-timeout — both re-running), **0
consolidation regressions**, **R7 export+launch live-clean**, **env-var deflections held**.

### ✅ Round 9 — deferred + re-runs (3/4; LAUNCH_KEY confirmed working)
launch-failure-build-stuck ✓, launch-with-existing-cicd ✓ (**create-new token IS a valid
LaunchKey** — both passed), cadence-build ✓ (prior timeout was transient load).
`existing-simple-mode-node-add-endpoint` FAILED AGAIN, identical `seed: wait for api: process …
failed: unknown` → **consistently broken FIXTURE** (its "api" pre-deploy reliably fails on the
platform; eval-infra issue, not the consolidation). Quarantine/fix fixture separately.

**New findings:**

### F51 — mounted `.git` is a SHALLOW clone that cannot `git push` ★★ HIGH
launch-existing-cicd (big time sink): `/var/www/appdev/.git` is a shallow clone from Zerops'
build pipeline — `status/log/commit` work but `git push` fails (`did not receive expected
object`); unshallow/repack/rebase all fail. Agent burned many calls; fix was clone-fresh-to-/tmp
→ copy → push → swap .git. Guidance says "commit+push" / "use zerops_deploy strategy=git-push"
but never warns the mounted .git is structurally unpushable. **Pairs with the earlier finding
that direct `git push` also fails because `.netrc` lives only inside the container** — TWO
reasons, ONE rule: never direct-push from the mount, always `zerops_deploy strategy=git-push`.
The git-push guidance must state this. HIGH (costly, recurring across git-push scenarios).

### F52 — LaunchKey creates but cannot DELETE a project; no teardown path ★ (+action)
launch-existing-cicd: launch key has "Allow creating projects" not delete; `zerops_delete` is
service-scoped; post-launch checklist says delete the KEY not the PROJECT. Agent could NOT tear
down the test prod project. **⇒ a stray prod project now exists on the account** (see action
item). If create-and-teardown is a supported flow, the workflow must surface teardown or warn
it's dashboard-only. MED.

### F53 — classify-prompt `envClassifications` shown as CLI syntax, not MCP tool-call shape
launch-existing-cicd: reconfirms F27 (env-param guidance pseudo-code ≠ tool schema).

**Reconfirms:** **F1 ~18×** (launch-fail-stuck, launch-existing-cicd). **F16** (cadence-build
Vite `dist/~`→`[.]`). **F30** mount hollow pre-adopt (launch-fail-stuck "burned two find cmds").
F2/F15, F49. F22's close-step guide CONFIRMED correct (cadence-build followed it) — the
contradiction is only the post-close MESSAGE.

### ⚠ ACTION ITEMS (Karel)
1. **Stray prod project** from launch-with-existing-cicd — eval agent couldn't delete it
   (LaunchKey lacks delete perm). Delete from dashboard, or give me a delete-capable token + the
   project name and I'll remove it.
2. **existing-simple-mode-node-add-endpoint fixture** reliably fails seed (api pre-deploy) —
   needs a fixture fix (eval-infra), separate from the consolidation.

---

### ✅ Round 10 — variance re-runs (5/5 PASS) — DETERMINISM CONFIRMED
adopt-pair-v2, recover-failed-v2, export-v2, kanban-devonly-v2, launch-pair-v2.

**Variance verdict: the top findings are DETERMINISTIC, not LLM-noise.**
- **F1** reproduced in ALL adopt scenarios (adopt-pair-v2, export-v2, launch-pair-v2 — verbatim
  "plan derived for you" → INVALID_PARAMETER). Confirmed across 2 independent samples.
- F45/F48/F44 (launch stateless re-call, blocker-wall, adopt-precondition) all reproduced.
- F-export validation-failed develop-ceremony reproduced. R6 retryCall reproduced (works).

**New findings (incl. one in MY R5 work):**

### F54 — export `compose-ready` status is UNDOCUMENTED in the status table ★ (R5 self-drift)
export-v2: the export guidance documents `classify-prompt → publish-ready`, but the actual
flow is `classify-prompt → validation-failed → compose-ready`. **`compose-ready` (the R5 terminal
I shipped) was NOT added to the documented status table** — agents pattern-matching on the table
are confused. Clean single-owner fix: add compose-ready to the export status table. CONCRETE,
self-inflicted by the consolidation → worth fixing.

### F55 — adopt plan needs OS-qualified type only available from discover, NOT the catalog ★ (sharpens F1)
adopt-pair-v2: the plan templates use `ubuntu/nodejs@22` / `alpine/nodejs@22`, but the stacks
catalog shown at session start lists bare `nodejs@22`. So even an agent that WANTS to author the
plan proactively can't — the OS-qualified type only comes from `zerops_discover` output, forcing
the error-retry loop. F1's fix must DERIVE/PROVIDE the plan (templates already in the error), not
merely "warn earlier." HIGH (compounds the #1 finding).

### F56 — runtimeClass derived from deployed zerops.yaml, not service type (misclassifies pre-deploy)
recover-failed-v2: `ubuntu/python@3.12` reported `runtimeClass: worker` pre-deploy, `dynamic`
post-deploy (HTTP ports declared). dev-server triage keys off runtimeClass → reading it pre-deploy
mis-routes. Classify after first deploy. MED.

**Reconfirms:** F30 (mount gated on provision-COMPLETE not service-ACTIVE — recover-failed-v2,
adopt-pair-v2), F2/F15, F7, F17. Minor: run.prepareCommands needs addToRunPrepare (recover-v2);
Laravel CSRF on routes/web.php 419 + npm-run-build-after-deploy (kanban-v2, recipe knowledge);
zerops_knowledge `ubuntu/*` runtime briefing near-useless → use scope=infrastructure.

---

## Running tally (after 11 rounds launched)
**52/55 functional PASS** (2 fixture/harness fails + 1 broken-fixture re-fail), **0 consolidation
regressions**, **R7 export+launch live-clean**, **F1 deterministic across 2 samples**.

### ✅ Round 11 — variance batch 2 (7/7 PASS) — 2 new + strong reconfirms
gf-fullstack, recipe-nextjs, develop-loop, discover-resumable, gitpush-cicd, env-cross-ref,
delivery-gitpush.

**New findings:**

### F57 — verify FALSE-GREEN for client-side-broken apps (browser-reachable env) ★
gf-fullstack: agent set `NEXT_PUBLIC_API_URL` to internal `http://apidev:3000` — the BROWSER
can't resolve that (internal DNS is server-side only). `zerops_verify` does a server-side
`GET /` → sees the HTML shell → PASSES, but a real user's client-side fetches fail. verify
can't exercise client JS, and nothing warns that `NEXT_PUBLIC_*`/browser-reachable URLs need the
PUBLIC subdomain, not the internal hostname. Silent false-green. MED-HIGH (verify gives false
confidence; pairs with the internal-DNS-literal guidance F31 which is correct ONLY server-side).

### F58 — recipe route can't override `bootstrapMode:simple`→standard; dev+stage forces a redo ★
recipe-nextjs (sharpens F23): a simple recipe can't be expanded to a dev/stage pair via the
recipe route (discover plan renames/marks-EXISTS but can't change mode). User asking dev+stage →
must abandon recipe + redo via classic. (Note: my R3 dev-only narrowing handles standard→dev, NOT
simple→standard.) Discover guidance should route dev+stage-on-simple-recipe → classic. MED.

### F59 (minor) — "dev" terminology overload: bootstrapMode=standard (pairing) vs dev-mode (runtime supervision)
recipe-nextjs: standard pairing's dev-half still behaves dev-mode (unsupervised until deploy);
"not durable" verify note persists even post-deploy with a real run.start — slightly misleading.

**Reconfirms (strong):** **F35 — 3rd verbatim hit** (discover-resumable: hollow-resume
`BOOTSTRAP_NOT_ACTIVE`, misleading "start bootstrap first", iterate→generic-stop, only `reset`
works — deterministic + concrete). F1 (discover-resumable verbatim). F12, F21.
**POSITIVE:** develop-loop confirms the `DATABASE_URL` + `/${db_dbName}` callout SAVED the agent
(connectionString-omits-dbname guidance holds — a prior wave fix that sticks).

---

### ✅ Round 12 — final coverage (7/7 PASS) — 4 new findings
cross-deploy-promote, add-managed-dep, appdev-reminders, discover-adopted, launch-pipeline-not-
config, php-maria, gf-website.

### F60 — auto-close fires on deploy-success but dev-mode service is 502 until dev_server restart ★ (R5)
existing-standard-appdev-only-reminders: `zerops_deploy` replaces the dev container (kills the
running dev server); auto-close already fired off a PRE-deploy verify, so the session auto-closes
while the service returns 502. "auto-closed ≠ currently serving" in dev-mode. Touches R5 auto-close
derivation (the counted verify predates the deploy that invalidated serving state). MED.

### F61 — `build-integration` checks SOURCE git-push, not the PRODUCTION pipeline (sharpens F47)
launch-pipeline-not-config: user configured a webhook on the PRODUCTION dashboard, asked to verify;
`build-integration` (sounds right) checks SOURCE appdev git-push → `needsGitPushSetup`, which reads
as "production pipeline broken." Status doesn't clarify source-vs-target. MED.

### F62/F63 — zerops_knowledge has no php-nginx hello-world (falls back to php-apache); cross-deploy
deployFiles cherry-pick has no template for non-build apps (plain PHP) — agent constructs the list
blind, wrong list fails only at stage verify. Knowledge/guidance gaps (relate F9/F16). LOW-MED.

**Reconfirms:** F7 (volume tax), F9 (gf-website build.base≠type), N1 (MCP flapping).

---

## ✅ BATTERY COMPLETE — 12 rounds, ~60 runs (+ 3 from earlier sessions)
**Functional PASS: 59/62.** 3 non-PASS = 1 transient seed (existing-simple-mode, then a
2nd identical fail → BROKEN FIXTURE), 1 harness-timeout (cadence-build, re-ran green).
**0 consolidation regressions. R7 export+launch live-clean. F1 deterministic across 2 samples.**
63 raw findings (F1-F63) logged above. Next: deep synthesis verifies the top ~15 against code +
classifies + ranks → final report. (Observation only — fixes ship in SEPARATE plans.)

ACTION ITEMS for Karel: (1) stray prod project from launch-with-existing-cicd; (2) broken
existing-simple-mode fixture. Blocked (un-run): launch-to-existing-prod-project (no existing-proj creds).
