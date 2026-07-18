# Step 1 — REAL bugs: implementation-ready fix plans (v2, full-breadth verified)

**v2 supersedes the v1 catalog in this file.** Method: 14-agent Workflow fan-out (9 per-bug
full-breadth agents + 5 production-pipeline agents; every new HIGH adversarially verified —
15 CONFIRMED / 3 WEAKENED / 0 REFUTED) + independent Codex pipeline review. Full structured
detail: `/tmp/zcp-response-audit/bugs-pipeline-result.json` (transient).

**v1→v2 corrections (why this pass mattered):** 4 of the v1 prescriptions were wrong or insufficient:
- **B1**: v1's fix (`echo "$GIT_TOKEN" | gh auth login` in the agent's shell) was itself a phantom
  env one level subtler — `GIT_TOKEN` is live ONLY in the push-source container's shell (project
  env lands at container boot; only the push source was restarted), never the agent's. Correct fix
  = SSH exec into the push source.
- **B3**: single-site fix insufficient — the readiness predicate is hand-duplicated in
  `build_plan.go::needsVerify`; fixing only `work_session.go` desyncs gate from blocker rendering.
- **B4**: v1's headline evidence was mis-cited (a classic+startWithoutCode run, not recipe);
  the master-plan P2 stamp condition would miss local-mode recipe flows; Codex's zero-API
  derive-at-render is impossible today (SDK `AppVersionLight` lacks the needed fields).
- **B9**: v1's wording ("responded 200 at http://<hostname>:<port>") would itself ship a new
  tell≠check drift — the probe ran against localhost; a loopback-bound server (Vite/Nest default)
  passes the probe but refuses hostname traffic.

Companion doc: `plans/archive/production-pipeline-review-2026-06-05.md` (journey, handoffs, architecture).

---

## B1 — eval env var in production gh-auth tell (M-small, ~0.5 day)

**Root cause (verified):** born broken in `8201d826` (2026-05-23, "eval-driven fix") — the author
lifted the eval agent's improvised recovery command VERBATIM, including the harness-only env var,
into `actionsConfirmResponse` (`workflow_build_integration.go:309-315`). Eval-env circularity
certified it (the only env it was ever verified in is the one that defines the var). Coverage hole:
`TestHandleBuildIntegration_ActionsConfirmEnrichesResponse` asserts 25+ substrings of this response,
none from ghAuthPrecondition.
**Real failure mode (reproduced):** `echo "" | gh auth login --with-token` does NOT fail fast — gh
falls back to the interactive device-code flow and HANGS (~2 min tool timeout, then an unusable
headless prompt). The response's `failureSymptom` ("HTTP 401: Bad credentials") describes a symptom
that never occurs on this path.
**Scope extension (BI-NEW-1, CONFIRMED):** same defect class in the SAME file's local-mode secret
command — the jq expression hardcodes the WRONG MCP server key; green-pinned at two more surfaces.
Fix in the same commit.

**Fix design** (extends the file's own env-aware pattern `ghSecretValueExpr`/`ghSecretSourceHint`):
1. `ops.GitTokenEnvKey = "GIT_TOKEN"` — single owner; switch the 5 raw-literal sites
   (`workflow_git_push_setup.go:409` write, `ops/deploy_git_push.go:44` + `ops/git_auth_probe.go:40-42`
   SSH builders, `ops/env_generate.go:85` denylist).
2. New helpers `ghAuthSetupCommand(rt, pushHost)` + env-aware description/failureSymptom.
   **Container:** SSH exec into the push source (the one shell where `$GIT_TOKEN` is live —
   git-push-setup restarted it for exactly this): standard
   `ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null <pushHost>` form; guarded
   `test -n` against the empty-token device-code hang; idempotent via `gh auth status` short-circuit.
   **Local:** ZCP holds NO GitHub credential by design → honest tell = verify-then-ask-the-user +
   explicit "NEVER generate a token" (wording coordinated with B6's credential contract).
3. Fix the local-mode jq MCP-server key (BI-NEW-1) + atom sentence
   `setup-build-integration-actions.md:20` (container-shaped prose firing in local mode).
4. **Class protection (NEW-4):** `TestNoEvalEnvVarsInAgentFacingStrings` — go/parser walk of string
   literals in non-test, non-live-tag files under `internal/{tools,ops,workflow,content,topology,envclass}`
   + rendered atom bodies; forbid `ZCP_E2E_*`. Plus a live-suite assert (actions-confirm body has no `ZCP_E2E_`).

**RED:** extend the confirm test for both modes (container: asserts ssh-to-pushSource + `$GIT_TOKEN`
+ test-n guard; local: asserts ask-the-user + no env echo) + the evalisms lint + live assert.
**After:** container agent runs the setupCommand verbatim and gh authenticates with the stored PAT
in one non-interactive call; local agent asks the user instead of hanging 2 minutes on a device-code
prompt.

## B2 — recipe corpus teaches schema-invalid yaml (M+, 1–1.5 days; ⚠ Aleš touchpoints)

**Sweep results (class, verified):** beyond `nodejs-hello-world.md:85-89` (run.verticalAutoscaling):
`gleam-hello-world.md` dev buildCommands carry bare `- true` (YAML bool, not string — type-level
instance); **Aleš's recipe-authoring teaching content**: `comment-style-positive.md:19` shows
`deployFiles:` under `run:`; grader example `zerops_yaml_comment_fail_field_narration.md` shows
`run: httpSupport: true` without `ports:` (structurally invalid "Correct shape").
**Deeper roots (NEW-4 CONFIRMED, NEW-5):** (a) `TestRecipeLint`'s hand-mirror structs are
structurally blind to unknown keys (`yaml.Unmarshal` without `KnownFields`) — HOW the corpus gate
missed it; (b) `sync push`'s anti-regression heuristic (`len(new) >= len(existing)` else silently
skip — `push_recipes.go:218-227`) structurally blocks every SHRINKING zerops.yaml push, so the fix
itself needs a manual PR for the app repo.

**Fix design:**
- **A — content:** (A1) DELETE nodejs-hello-world.md:85-89 (scaling intent already lives,
  platform-honored, in the recipe's `.import.yml`); (A2) gleam `- true` → `- 'true'`;
  (A3, **flag to Aleš first, then edit**): comment-style-positive.md deployFiles → `build:`;
  grader example gets valid `ports:` shape (the lesson there is comment style — keep the yaml valid).
- **B — cross-repo publish:** sync push both recipes; nodejs app-repo zerops.yaml needs a MANUAL PR
  (the length guard skips shrinking files); merge → cache-clear → pull → verify.
- **C — class lint (single owner = internal/schema):** new `internal/schema/snippet` package —
  `LintMarkdownYAML(file, markdown, tier)` with Complete|Fragment tiers (placeholder normalization
  for scaffold templates); wired into `recipe_lint_test.go` (Complete tier; keep struct-mirror only
  for semantic checks), new `guide_lint_test.go`, and `atoms_lint_yaml.go` (Fragment tier, allowlist
  mechanism). Also fix the recipe lint's KnownFields blindness.

**After:** the export validation-failed leg (13.6 KB + 36.5 KB knowledge pull + ~10 repair turns
per run) disappears; the next invalid snippet cannot enter the corpus.

## B3 — auto-close temporal ordering (M — was S in v1; one predicate, three sites)

**Scope correction (B3-N1 CONFIRMED):** the gate definition is duplicated — `work_session.go:624`
(serviceAutoCloseReady) AND `build_plan.go:158` (needsVerify) AND the render verify-OK state. One
new predicate, all three call it. **B3-N2:** verify-before-deploy is the NORMAL dev-mode cadence
(dev_server → verify → deploy), not an anomaly — the fix MUST re-open verify after the deploy, which
is exactly the correct semantics.

**Fix design:** single owner `staleVerify(deploys, verifies) bool` in build_plan.go (next to
lastSucceeded): stale ⇔ `lastVerify.At.Before(lastDeploy.At)` — strict Before so a same-second tie
counts CURRENT (1 s RFC3339 granularity; fail-toward-re-verify on partial garbage). Compare deploy
**SucceededAt** (completion), never AttemptedAt. Clock-sound (one per-PID process, UTC). Call sites:
serviceAutoCloseReady (third conjunct), needsVerify (re-opens), renderProgressAndBlockers (verify
line flips to "verify stale (passed before the last deploy — re-verify)"). `verifyRationale`'s
"should not reach here" branch becomes reachable BY DESIGN — reword. Spec/atom sweep (B3-N6): the
gate sentence is restated in 7 hand-authored places — add the ordering clause everywhere
(spec-work-session.md ×3, spec-workflows.md ×3 incl. W5, develop-auto-close-semantics.md, CLAUDE.md bullet).
**RED:** verify t1 + deploy t2 → NOT ready; verify t3 → ready; same-second tie → ready; render test
for the stale-verify blocker line.
**After:** corpus 20260605-040507 shape — deploy at seq 12 no longer auto-closes on the seq 11
verify; the response says "verify stale — re-verify"; the agent re-verifies the NEW container and
only then closes. No more "scope is green" over a dead server.

## B4 — stale `deployed=false` for recipe-buildFromGit (M, ~100-140 LOC; derive-don't-stamp)

**Evidence correction (NF-1):** v1's headline citation was a classic run; correct evidence:
recipe-route runs (e.g. 20260603-062821/greenfield-node-postgres-dev-stage) where import provisioned
via buildFromGit, discover shows ACTIVE, develop-start says `deployed=false` + first-deploy branch.
Bootstrap's own close message ("cloned, built, and DEPLOYED") contradicts develop's branch selection.
**Why not the master-plan P2 stamp (NF-4):** the DiscoveredStatuses-at-close condition misses
local-mode recipe flows (provision check skips the status snapshot there).
**Why not derive-at-render from live ActiveAppVersion (NF-3):** definitively impossible today — SDK
list DTO `AppVersionLight` carries only Id/Status/Os/Base; no Source/startWithoutCode distinguisher.
(Also NF-2: `ops.IsRuntimeNeverDeployed`'s ActiveAppVersion==nil is live-disproven for
startWithoutCode — a separate latent bug to note in the fix commit.)

**Fix design (one owner chain, R3-P4 precedent):**
1. `RuntimeTarget.BuildFromGit string` (derive-only; set by `DeriveRecipePlan` from the parsed
   `RecipeImportShape` in all four target constructions — never agent-authored).
2. `ServiceMeta.ProvisionedFromGit bool`; set in `writeBootstrapOutputs` AND `writeProvisionMetas`
   when route==recipe && !IsExisting && BuildFromGit != ""; `mergeExistingMeta` preserves (OR).
   Covers local-mode automatically (pair meta carries the flag).
3. `DeriveDeployed` gains signal 4 (mirror of the adopted signal): `meta.ProvisionedFromGit &&
   status==ACTIVE → true`.
4. Consolidation: replace `bootstrap_guide_assembly.go:351`'s `strings.Contains(importYAML,
   "buildFromGit")` with plan-derived `planHasBuildFromGit(plan)` — kills the stored-proxy string probe.
**RED:** meta-write tests both routes/modes; DeriveDeployed signal-4 matrix (ACTIVE→true,
READY_TO_DEPLOY→false, classic meta→unchanged); develop-start integration: recipe bootstrap + ACTIVE
⇒ edit-loop branch.
**After:** recipe bootstrap → develop start renders the ~10 KB edit-loop branch ("adapt the running
app") instead of 28 KB "write the application code" for an app already serving.

## B5 — close-mode DECISION axis lock-out + silent head (S/M, ~150-220 LOC, mostly tests/goldens)

**Magnitude correction (B5-N2):** causal mechanism confirmed; honest numbers 2/49 (4%) vs 12/16
(75%) after excluding replay/resume confounders. **New hole (B5-N1):** in the
deployed+verified+closeMode-unset state the head renders NO blocker line AT ALL (early return) —
the agent gets a green-looking head while the gate is blocked.

**Fix design (both layers):**
- **Axis:** DELETE `deployStates: [deployed]` from develop-strategy-review.md (drop, not enumerate —
  `closeDeployModes: [unset]` remains the sole trigger; fires while undecided, self-extinguishes
  once set). Reword the one deploy-state-asserting body line deploy-state-neutral. Trim the
  duplicate tell in develop-first-deploy-intro.md:31-42 to a one-line pointer at the DECISION section.
- **Head:** `renderProgressAndBlockers` computes unset-hosts over env.Services and emits
  `→ DECISION required: close-mode unset on <hosts> — zerops_workflow action="close-mode"
  closeMode={"<host>":"auto"}` — INCLUDING in the pending==0 case (fixes the silent-head hole).
- **One owner for the call example:** `workflow.CloseModeCallExample(hosts)` consumed by render,
  work_session.go:614 Reason, workflow_close_mode.go:276 Hint (today: 5 sites, 2 placeholder syntaxes).
**RED:** render table (unset+pending, unset+green→DECISION line, auto→absent); corpus-coverage
fixture never-deployed+unset; golden scenario regen (3 churned); spec-workflows D4 rows.
**After:** first develop-start carries the DECISION heading in guidance AND the head line; expected
early-compliance shift from ~4% toward the ~75% the DECISION rendering measurably produces.

## B6 — git-push-setup diagnostics + credential contract (3 commits, ~1–1.5 days)

**Scope extensions over master-plan P3:** (B6-N1) the origin-sync failure branch in the SAME
function has the identical stderr swallow — P3 alone re-creates the bug one branch lower;
(B6-N2) "Repository not found" matches NO signal in the shared library — even surfaced stderr won't
classify; (B6-N3 CONFIRMED) agents FABRICATED PATs in 4 independent runs after the generic error —
the credential contract is a REAL safety gap, not cosmetics; (GPS-5/B6-N4) confirm re-call re-runs
the full chain incl. container restart — O3 check-before-mutate violation with corpus evidence.

**Fix design:**
- **B6a (P3 + breadth):** capture `probeOut` from ExecSSH; reuse the deploy path's
  `gitPushErrorDetail` + `classifyTransportError` + `WithFailureClassification` — in BOTH the probe
  AND origin-sync branches; local probe attaches the same classifier. Add ONE shared signal
  `transport:git-repo-not-found` to `ops/deploy_failure_signals.go` (regex
  `remote: Repository not found|fatal: repository '...' not found`; dual-reading cause: URL wrong OR
  token can't see a private repo).
- **B6b (credential contract):** `convertError` appends a credential-contract appendix when
  category==credential: "the token is a user-held secret — surface the failure and ask the user
  (AskUserQuestion); NEVER generate, guess, or mutate a token." Single owner const in errwire.go;
  atom sentence aligned. (Same contract referenced by B1-local.)
- **B6c (idempotency):** confirm short-circuits when the pair is already configured with the same
  canonical remoteUrl (ops.Subdomain check-before-mutate precedent) — no probe, no env write, no restart.
**RED:** probe+origin-sync stderr surfacing tests (containerSSHStub), repo-not-found classification,
credential-appendix presence on token-class errors, idempotent re-call (RestartService call-count 0).
**After:** the worst real run (5 wasted calls + 3 fabricated tokens) becomes: one error naming the
actual cause ("Authentication failed" vs "Repository not found") + the ask-the-user directive; the
agent asks, gets the right PAT, succeeds on call 2.

## B7 — empty-logs dead end + wrong pointers (S→M, ~1 day)

**Scope extensions:** (NF3) two recovery emitters hand agents a `facility` arg the zerops_logs
schema REJECTS (SDK additionalProperties); (NF1) `develop-build-observe.md` teaches
`zerops_events since=<duration>` — parameter doesn't exist; (NF2) build-container output is
structurally unreachable through ANY agent tool for async builds — events' failureClass/Cause is the
only diagnosis surface, so pointers MUST go there; (NF4) `FailedDeployContext.SuggestedReadTool/Args`
are orphan fields (only their own test consumes them) — DELETE; (NF5) logs silently defaults
`since` to 1h — undisclosed filter that fed the groping loop.

**Fix design:** (A) `LogsResult` gains `serviceStatus`, `emptyReason`, `recovery` (omitempty;
populated only when 0 entries, from data FetchLogs already holds) with the status→explanation
matrix (NEW; READY_TO_DEPLOY±prior-attempt; FAILED; STOPPED; ACTIVE→filter echo incl. "1h default"
marker). (B) Re-point import-gate RecoveryHint (`tools/import.go:225-233`) + `destructive_ack.go`
suggestions at `zerops_events`. (C) Remove the phantom `facility` arg (non_running_recovery.go),
the phantom events-since tell (atom), the zerops_logs-facility promises (events.go hint map,
workflow_record_deploy.go); document the 1h default in the Since schema description; delete the
orphan fields.
**RED:** emptyReason matrix unit; import-gate recovery=events pin (flips existing facility pin);
atom lint for the removed phantom params.
**After:** the 30 B dead end becomes a self-explaining response naming WHY it's empty and the ONE
correct next surface; the 2–3-call groping loop disappears.

## B8 — tell≠check drifts + status-token lint (S→M, ~1 day)

**Scope extensions:** (NF-1 CONFIRMED) FOURTH live instance: `launch-status-recovery.md:44`
promises a `wouldDelete` diagnostic — wire key is `wouldDestroy`, and it teaches
`confirmDestructive: true` where the gate validates a structured object. (NF-2) the lint owner must
be the SDK enums (`ServiceStackStatusEnumAllStrings` ∪ AppVersion ∪ Process from zerops-go), NOT
platform/types.go's 6-value local subset (lacks CREATING/STARTING). (NF-3) the phantom
NOT_YET_DEPLOYED actively shipped in 67 payloads. (NF-5) full-sweep: every OTHER enforceable claim
in the corpus verifies TRUE — these four are the complete set.

**Fix design:** (a) bootstrap-verify.md:24 → real vocabulary (RUNNING/ACTIVE dev,
READY_TO_DEPLOY stage); (b) delete the unreachable corrected-JSON promise
(bootstrap-classic-plan-dynamic.md:14); (c) `git rm bootstrap-recipe-match.md` — content absorbed
into the Go injector (`bootstrap_guide_assembly.go` discover branch) with a conditional narrow line
derived from the parsed shape (one owner; the atom was already coverageExempt, zero goldens);
(d) launch-status-recovery.md:44 → `wouldDestroy` + structured confirmDestructive shape;
(e) `statusTokenViolations` lint in atoms_lint.go (peer of staleActionViolations) pinning every
status-shaped token against the SDK enum union — prototyped: 1 hit (the bug), 0 false positives
across 112 atoms; (f) `TestStatusConstants_SubsetOfSDKEnum` pinning platform's local consts against
the SDK.
**After:** close guide names exactly the strings discover prints; no agent burns a reconciliation
turn on a phantom status; the next phantom can't ship.

## B9 — dev_server URL vantage (S, ~40 LOC, one commit)

**Design corrections:** (NF-2) local mode is moot — the tool is container-only (registration gated
on InContainer); (NF-1) wording must stay check-honest: the probe verified localhost, NOT the
hostname bind — Vite/Nest loopback defaults pass the probe but refuse hostname traffic.
**Fix design:** shared `devServerURL(hostname, port, healthPath)` helper; message states the checked
fact + hands the derived pointer: `"Dev server on appdev started — health probe passed (HTTP 200 in
1114ms, probed from inside the container). Reach it at http://appdev:3000/ (requires the app to bind
0.0.0.0, not loopback)."`; `DevServerResult.URL` structured field populated on probe success
(start/restart/status — kills the existing start-vs-status divergence NF-3); tool description +
atom line updated (url in the field list).
**RED:** URL field + hostname form in message + absence of `http://localhost` in success/failure
messages + status parity.
**After:** the agent stops needing the atom-taught ssh-wrapped-curl workaround knowledge to know
where the server actually is; ~50 ssh-curls per battery keep working but the message no longer lies
about the vantage.

## B10 — NEW: credential-hygiene cluster (S/M; from the pipeline review)

| # | leak | fix |
|---|---|---|
| B10a (**CONFIRMED, security**) | launch source-control gate's remote-mismatch blocker echoes the LIVE `git remote get-url origin` VERBATIM — including a credential-embedded `https://user:PAT@github.com/...` — into the agent-visible payload (`launch_source_control_gate.go:514`); a full PAT leaked in run 20260605-0* | redact userinfo via one URL-sanitizer helper at EVERY echo site (gate blockers, export drift warnings, git-push-setup echoes) |
| B10b (GPS-4) | `validateRemoteURL` + container HTTPS check ACCEPT credential-embedded remoteUrl end-to-end → PAT lands verbatim in meta.RemoteURL + .git/config | reject (or strip-and-warn) userinfo at validation; meta stores credential-free URL (auth lives in GIT_TOKEN) |
| B10c (GPS-8) | container confirm claims "requires HTTPS" but accepts `http://` — would send the PAT plaintext | enforce https scheme (tell==check) |
| B10d (B6-N6) | GIT_TOKEN value dumps in `zerops_discover includeEnvValues=true` | classify EnvSetSensitiveProject-written keys as redact-always (or document the exposure decision) |

**RED:** sanitizer unit (userinfo forms); gate blocker test with credential-embedded origin;
validateRemoteURL rejection; https enforcement; discover redaction.

---

## Batch order (v2)

1. **B1** (+ BI-NEW-1 + evalisms lint) — user-facing breakage, S/M, self-contained.
2. **B10a-c** — security redaction cluster (small, high stakes).
3. **B3** → **B5** → **B8** — lifecycle/tell trio (each one commit).
4. **B6** (3 commits) → **B7** — error-path diagnosis pair.
5. **B4** — derive chain.
6. **B9** — message vantage.
7. **B2** — content + cross-repo + class lint (⚠ A3 items: **flag Aleš before editing**
   comment-style-positive.md + grader example; rest is core scope).

Each bug: RED → GREEN → goldens → `make lint-local`; after the batch: flow-eval round-trip on the
touched scenarios (git-push-setup-with-cicd-method-prompt, recover-failed-buildfromgit-missing-dep,
export-buildfromgit-self-snapshot, existing-standard-appdev-only-reminders, one recipe-route run).
NEVER `make release` / `make install` without explicit ask.

## Superseded / unchanged

- Master plan P2/P3 are SUPERSEDED by B4/B6 here (deeper scope, corrected conditions). P1/P4/P5/P6
  of `flow-eval-fix-master-plan-2026-06-05.md` remain scheduled there.
- PT-7 stays no-action (fixed in tree; canonical regression evidence).
