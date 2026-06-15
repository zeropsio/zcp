# Pre-Internal-Testing Audit — Round 3 (post-Round-2 re-pass)

**Date**: 2026-04-30 (later same day as Round 2)
**Scope**: Independent fresh look at the current tree. NOT a re-verify of Round 1/2 findings — those were triaged and most landed cleanly. This pass hunts what BOTH prior audits missed.
**Out of scope**: `internal/recipe/` and the `zerops_recipe` v3 engine (recipe authoring is a separate workstream). The recipe-USE path (`route="recipe"` bootstrap) IS in scope.

---

## Method

1. Re-read the user's `audit-prerelease-internal-testing-2026-04-30-roundtwo.md` scorecard to know what's marked closed.
2. Verified each Round-2 fix against current code — N4 (git-push axes), N5 (setup-git-push prose), N6 (close-mode-auto-local modes), N7 (spec D2b alignment), C1 (40-file vocab sweep) all correctly landed; new regression test `TestNoRetiredVocabAcrossRepo` is in place.
3. Codex `gpt-5.3-codex-spark` adversarial pass with explicit "don't re-verify, find new" brief.
4. My own fresh greps on:
   - aggregate-atom `{hostname}` placement vs `{services-list:…}` directive boundaries
   - handler dispatch action list vs hint strings in error responses
   - eval scenario PROSE drift (vs preseed data drift, which the new lint catches)
   - cross-atom snippet conflict for overlapping axis coverage

Codex returned 6 findings (F1-F6); my own greps surfaced 2 more (F7-F8). All 8 verified in current code — no speculation.

---

## Findings — sorted by severity

### CRITICAL

#### F1 — `record-deploy` build-status gate skipped on every call AFTER first stamp

**Location**: `internal/tools/workflow_record_deploy.go:104`

```go
if client != nil {
    meta, _ := workflow.FindServiceMeta(stateDir, input.TargetService)
    if meta != nil && meta.FirstDeployedAt == "" {  // ← gate only when EMPTY
        if blocked := recordDeployBuildStatusGate(...); blocked != nil {
            return blocked, nil, nil
        }
    }
}
```

The gate Round-2 added (N2) only runs when `FirstDeployedAt` is empty (i.e., the very first record-deploy on a service). On every SUBSEQUENT record-deploy — exactly the iterating-redeploy case where the bug bites hardest — the gate is skipped entirely.

Worse, the synthetic work-session DeployAttempt at `:140` is appended unconditionally when `firstAt != ""` (always true post-first-stamp), with `Strategy: "record-deploy"` and synthetic `SucceededAt = time.Now()`. So a second/third/Nth git-push iteration:

1. Agent runs `zerops_deploy strategy="git-push"` → push transmits.
2. Agent (impatient or post-compaction) calls `record-deploy` BEFORE `Status=ACTIVE`.
3. Gate skipped because `FirstDeployedAt` already set.
4. Synthetic successful DeployAttempt appended to Work Session.
5. Agent runs `zerops_verify` → may pass against STALE deployed code.
6. Auto-close fires (D2c: every scope service has a succeeded deploy + passed verify).
7. Session closes while build is still running. Agent moves on.
8. Next session opens against still-stale state.

**Failure mode**: corrupts the iterating-redeploy lifecycle for every webhook/actions service. This is exactly the bug C2 was meant to fix — and the fix is gated to first-deploy only.

**Fix size**: small — drop the `meta.FirstDeployedAt == ""` condition, run the gate on every call. The gate itself is idempotent and cheap (one `zerops_events` round-trip). Add a `TestRecordDeploy_GateRunsOnSubsequentCalls` pin.

**Why CRITICAL**: this is the foundational async-deploy lifecycle. Round-2 thought it closed C2 + N2; the gate is half-installed and the iteration path is still broken.

---

### HIGH

#### F2 — Git-push next-action guidance uses `filterServices=` arg that doesn't exist on `zerops_events`

**Location**: `internal/tools/deploy_git_push.go:367` AND `internal/tools/deploy_local_git.go:227`

Both git-push paths emit identical NextActions text:

```go
"Watch the build via zerops_events filterServices=[%q] until Status=ACTIVE, then ack with zerops_workflow action=\"record-deploy\" targetService=%q. ..."
```

Renders as: `zerops_events filterServices=["app"]`.

But the actual events tool (`internal/tools/events.go:13`) only accepts:
```go
ServiceHostname string `json:"serviceHostname,omitempty"`
Limit           int    `json:"limit,omitempty"`
```

So the guidance is wrong on TWO axes:
- arg NAME: `filterServices` → not in schema. MCP client either rejects with schema error or silently drops.
- arg SHAPE: `[…]` (JSON array) → tool takes a single string. Even after rename, shape mismatches.

**Failure mode**: the *handoff point* of the async git-push lifecycle. Right after the C2 fix removed auto-stamp and made the agent depend on this exact bridge, the bridge instruction is wrong.

**Fix size**: small — rewrite both strings to `zerops_events serviceHostname=%q until Status=ACTIVE`. Two-line change.

#### F3 — Tests guarding the git-push guidance only check vague substrings, missing the bad arg name

**Location**: `internal/tools/deploy_ssh_test.go:1262` (similar at `deploy_local_git_test.go:160`)

```go
text := getTextContent(t, result)
for _, want := range []string{"record-deploy", "zerops_events", "Status=ACTIVE"} {
    if !strings.Contains(text, want) { ... }
}
```

The test passes whenever the response mentions all three strings — never validates the actual ARG NAME. So F2's `filterServices` typo locked in green.

**Failure mode**: a green test that doesn't test what users actually do. Future regressions stay hidden.

**Fix size**: small — extend the substring list with the canonical arg form, e.g. `serviceHostname="app"`. Bundle with F2 fix.

#### F4 — Credential-failure classifier matches retired `"push-dev"` strategy string

**Location**: `internal/ops/deploy_failure_signals.go:216`

```go
{
    id:         "transport:zcli-auth-failed",
    phases:     []DeployFailurePhase{PhaseTransport},
    logRegex:   regexp.MustCompile(`(?:invalid token|unauthorized|401|forbidden|403)`),
    strategies: []string{"push-dev"},   // ← retired vocabulary
    requireLog: true,
    build:      transportZCLIAuth,
},
```

This is the LAST live `"push-dev"` reference in non-archive code outside lint files (verified via grep — exactly one hit). Recording call sites (`deploy_ssh.go`) actually use `Strategy: "zcli"` (modern vocab). The signal therefore never matches; zcli auth failures fall through to the network-transport baseline ("check connectivity") instead of the credential-recovery path ("ZCLI_TOKEN missing or invalid; rotate via zerops_env or refresh ~/.netrc").

Round-1's `repo_drift_test.go::TestNoRetiredVocabAcrossRepo` allowlists this specific line via the lint-file exclusion, so the drift gate doesn't catch its own ops-side rule.

**Failure mode**: agent hits an auth error on first deploy, sees a generic "transport failure" hint instead of the specific credential remedy, wastes iterations on the wrong recovery path.

**Fix size**: small — change `strategies: []string{"push-dev"}` to `strategies: []string{"zcli"}` (or drop the strategy filter — the regex already disambiguates auth errors).

---

### MEDIUM

#### F5 — Two error hints reference `action="mode-expansion"` which the workflow handler doesn't accept

**Location**: 
- `internal/tools/deploy_git_push.go:86`
- `internal/tools/workflow_git_push_setup.go:97`

Both produce hints like:
```
"Run mode-expansion to upgrade ModeDev → ModeStandard (adds a stage half) before configuring git-push: zerops_workflow action=\"mode-expansion\" service=\"appdev\""
```

The handler dispatch at `workflow.go:313` lists 17 valid actions: `start, complete, close, skip, status, reset, iterate, resume, list, route, close-mode, git-push-setup, build-integration, classify, adopt-local, dispatch-brief-atom, record-deploy`. `mode-expansion` is NOT one of them. Real mode-expansion happens via `action="start" workflow="bootstrap"` with `route="adopt"` and `isExisting=true + bootstrapMode="standard" + stageHostname=…` per spec §10 / `develop-mode-expansion.md:19-22`.

**Failure mode**: agent on dev-mode service with git-push intent reads the suggestion, calls `action="mode-expansion"`, gets `Unknown action "mode-expansion"`, has to figure out the bootstrap-with-isExisting flow from scratch.

**Fix size**: small — rewrite both hints to either point at the bootstrap flow or to the `develop-mode-expansion.md` atom.

#### F6 — Eval scenario PROSE drift: `develop-pivot-auto-close.md:56` still claims service runs under "push-dev strategy"

**Location**: `internal/eval/scenarios/develop-pivot-auto-close.md:56` (Czech-language user-prompt text)

```
…(postgres). Služba je adoptovaná a běží pod push-dev strategy.
```

The preseed (`preseed/develop-pivot-auto-close.sh`) correctly writes `closeDeployMode: "auto"` per the post-decomposition vocab. But the user-facing prompt PROSE still uses retired terminology. An eval agent reading the prompt attributes behavior to a non-existent axis; a future LLM updating the scenario takes "push-dev strategy" as authoritative and re-introduces the bug elsewhere.

The new `repo_drift_test.go` lint catches IDENTIFIERS (`DeployStrategy`, `MigrateOldMeta`, etc.) and ACTION strings — but doesn't pattern-match free prose like "push-dev strategy". Hence missed by the automated guard.

**Failure mode**: corrupts eval signal for the auto-close pivot scenario; test passes against wrong mental model.

**Fix size**: trivial — replace "push-dev strategy" with "close-mode=auto" in the Czech prompt text. Consider extending the drift lint with a small phrase list (`push-dev strategy`, `push-git strategy`, `strategy field`, etc.).

#### F7 — `develop-close-mode-auto-deploy-local.md` emits `targetService="{stage-hostname}"` which is empty for all 3 modes it covers

**Location**: `internal/content/atoms/develop-close-mode-auto-deploy-local.md:7` (body), `:6` (modes axis)

Frontmatter:
```yaml
modes: [dev, stage, local-stage]
environments: [local]
multiService: aggregate
```

Body snippet (sole deploy command):
```
{services-list:zerops_deploy targetService="{stage-hostname}"}
```

Per spec E8 invariant + envelope.go:106, `StageHostname` is populated ONLY on the dev half of a standard pair. For the three modes this atom fires for:

| Mode | `{hostname}` | `{stage-hostname}` |
|---|---|---|
| `dev` (single half, no pair) | the service | **EMPTY** |
| `stage` (stage half of standard, in env) | stage hostname | **EMPTY** (stage-half snapshot doesn't carry sibling) |
| `local-stage` (single half) | the service | **EMPTY** |

Renders as `zerops_deploy targetService=""` for every matching service in every supported mode. The handler at `internal/tools/deploy_local.go` rejects empty hostnames immediately — but the agent has been told to run an impossible command.

The body's PROSE even contradicts the snippet: "deploys from your working directory into the linked Zerops stage". The placeholder should almost certainly be `{hostname}`.

**Failure mode**: every local-mode close=auto develop close fails at the deploy step with an empty-target error. Agent has no way to recover from atom guidance alone — must read `develop-close-mode-auto-deploy-container.md` (which is correct) and translate.

**Fix size**: trivial — change `{stage-hostname}` → `{hostname}` on the one line. Add a matrix scenario for local+dev close-mode=auto to pin.

#### F8 — `develop-close-mode-auto-deploy-container.md` snippet is incomplete for standard mode (missing cross-deploy form)

**Location**: `internal/content/atoms/develop-close-mode-auto-deploy-container.md` — frontmatter has NO `modes:` filter, so it fires for all of `[dev, simple, standard]`.

Body prose says (correctly):
> - Self-deploy (single service): `sourceService == targetService`, class is self.
> - Cross-deploy (dev → stage): class is cross — emit `sourceService` and `targetService` separately.

But the actual snippet only handles self-deploy:
```
{services-list:zerops_deploy targetService="{hostname}"}
```

For a standard-mode envelope (dev half + stage half snapshots both in env), this renders as:
```
zerops_deploy targetService="appdev"   # ← OK, self-deploy of dev
zerops_deploy targetService="appstage" # ← VIOLATES DM-2 (self-deploy of stage with stage's narrower deployFiles)
```

DM-2 pre-flight rejects the second line — agent gets `ErrInvalidZeropsYml` after following the atom literally.

The sister atom `develop-close-mode-auto-standard.md` (modes=[standard]) HAS the correct cross-deploy snippet with `sourceService="{hostname}" targetService="{stage-hostname}" setup="prod"`. So in standard-mode envelopes, BOTH atoms fire:
- `-deploy-container` (no modes filter): WRONG snippet for stage half
- `-standard` (modes=[standard]): RIGHT snippet for the whole pair

A careful agent reading both reconciles them; a sloppy one (or one with shorter context window) follows the first match and breaks.

**Failure mode**: redundant + conflicting guidance for standard mode. DM-2 catches the actual command, but the agent paid a tool call + iteration to learn that.

**Fix size**: small — add `modes: [dev, simple]` to `-deploy-container`'s frontmatter. The standard-mode case is fully covered by `-standard`. Add a matrix scenario asserting the standard envelope sees only `-standard`'s snippet, not `-deploy-container`'s.

---

## Severity summary

| ID | Severity | Title | File:line | Fix size |
|---|---|---|---|---|
| F1 | CRITICAL | record-deploy gate skipped on subsequent calls | `workflow_record_deploy.go:104` | small |
| F2 | HIGH | Git-push hint uses non-existent `filterServices=` arg | `deploy_git_push.go:367`, `deploy_local_git.go:227` | small |
| F3 | HIGH | Tests miss F2 — only check loose substrings | `deploy_ssh_test.go:1262` | small |
| F4 | HIGH | Credential signal matches retired `"push-dev"` strategy | `deploy_failure_signals.go:216` | small |
| F5 | MEDIUM | Two hints reference unhandled `action="mode-expansion"` | `deploy_git_push.go:86`, `workflow_git_push_setup.go:97` | small |
| F6 | MEDIUM | Eval scenario prose still says "push-dev strategy" | `develop-pivot-auto-close.md:56` | trivial |
| F7 | MEDIUM | Local close=auto deploy atom emits empty `targetService` | `develop-close-mode-auto-deploy-local.md:7` | trivial |
| F8 | MEDIUM | Container deploy atom over-fires for standard mode | `develop-close-mode-auto-deploy-container.md` (no modes filter) | trivial |

All 8 are SMALL or TRIVIAL fixes. Total estimated change: ~20 lines across ~8 files plus 2-3 new test pins.

---

## Verified OK (suspected, refuted by reading)

| Suspicion | Refutation |
|---|---|
| Per-service axis match collision in multi-service envelopes | `synthesize.go:367-398` plus `synthesize_test.go:512-538` correctly bind axis match to per-service snapshot |
| `record-deploy` build-status switch missing edge cases | `workflow_record_deploy.go:213-237` exhaustively handles ACTIVE / in-flight / failed / unexpected; fail-closed on events fetch error |
| Aggregate-atom `{hostname}` outside `{services-list:…}` (M4 backlog risk) | Greps across all 14 aggregate atoms found no leak. M4 lint still useful as defense-in-depth but no live issue. |
| F5 NEW `develop-close-mode-git-push-needs-setup.md` covers GitPushState gap | Atom exists with `gitPushStates: [unconfigured, broken, unknown]`; sister atom has `gitPushStates: [configured]`. Together they cover all 4 GitPushState values. ✓ |
| Spec post-KT-9 dangling refs to deleted §2.5/§2.6 | `grep '§2\.5.*Generate|§2\.6.*Deploy|Step 3.*Generate'` returns zero hits in atoms + docs (excluding archive). ✓ |
| `develop-flow-enhancements.md` plan archive | Moved to `plans/archive/develop-flow-enhancements-2026-04-20.md`. ✓ |
| Eval `instruction_variants.go` legacy vocab | All `Close-mode: action="close-mode"` usage; no `action="strategy"` survives. ✓ |
| `repo_drift_test.go::TestNoRetiredVocabAcrossRepo` actually catches drift | Properly excludes audit/archive/lint files; correctly catches code-side drift. F4's missed `"push-dev"` is in a lint-allowlisted file (`deploy_failure_signals.go` is NOT in the exclusion list, so this is a true gap not yet swept — F4 is the gap, not the lint). |

---

## Suggested fix order

### One-shot small commit (atomic, all 8 land together — ~20 lines)

1. **F1** — drop the `meta.FirstDeployedAt == ""` condition at `workflow_record_deploy.go:104`. Add `TestRecordDeploy_GateRunsOnSubsequentCalls` covering: (a) first call honors gate, (b) second call after stamp also honors gate when build is in flight, (c) second call passes when build is ACTIVE.
2. **F2 + F3** — rewrite the two `filterServices=[%q]` strings to `serviceHostname=%q`. Extend `deploy_ssh_test.go:1262` and `deploy_local_git_test.go:160` substring lists with `serviceHostname=` to lock the rename.
3. **F4** — change `strategies: []string{"push-dev"}` to `strategies: []string{"zcli"}` at `deploy_failure_signals.go:216`. Update the matching test if any. Verify against a `TestClassifyDeployFailure_ZCLIAuth` pin.
4. **F5** — rewrite both `mode-expansion` hint strings to point at the bootstrap-with-isExisting flow OR the `develop-mode-expansion` atom guidance. Verify the hints render to a real action.
5. **F6** — sweep "push-dev strategy" → "close-mode=auto" in `develop-pivot-auto-close.md:56`. Also grep for any other prose drift: `grep -rn 'push-dev strategy\|push-git strategy\|strategy field' internal/eval`. Likely zero other hits.
6. **F7** — change `{stage-hostname}` → `{hostname}` at `develop-close-mode-auto-deploy-local.md:7`. Add a matrix scenario `7.5 close-mode=auto local+dev` that asserts the rendered snippet has a non-empty `targetService`.
7. **F8** — add `modes: [dev, simple]` to `develop-close-mode-auto-deploy-container.md` frontmatter. Add a matrix scenario asserting standard mode envelope does NOT match this atom.

### Optional follow-up (extend the drift lint)

8. Extend `repo_drift_test.go::retiredVocabAllowlist` to catch free-prose drift like `push-dev strategy`, `push-git strategy`, `strategy field` (Round-2's lint targets identifiers, not phrases). Catches future F6-class regressions.

After these eight fixes land, re-run the matrix simulator (`ZCP_RUN_MATRIX=1 go test ./internal/workflow -run TestLifecycleMatrixDump`); anomaly count should stay at 2 (the briefing-size warnings) and the new local+dev scenario should pass without empty-target rendering.

---

## What remains genuinely OPEN after Round 3

Per Round-2 scorecard + this round:

- **C3** (failureClassification on TimelineEvent — DEFERRED) — backlog plan exists; trigger = live-agent feedback on async build failure diagnosis. Round-2 says "superseded by Round-2 implementation; classifier wired into ops.Events" but the structural propagation to TimelineEvent is still backlog. Verify before closing.
- **H3** (status response token cap — PARTIAL) — multi-service briefing 32.8 KB → 29.2 KB after P5-Lever-A. Still over 25 KB heuristic, under real 28 KB cap. Decide ship-as-is or extend dispatch-brief envelope.
- **L2** (Plan rationale doesn't mention "first deploy ignores close-mode" — OPEN cosmetic) — atom covers it; Plan still doesn't.
- **M1, M2, M4** — backlog with named plans + triggers.
- **deploy-intent-resolver** (H1 structural follow-up — backlog).
- **auto-wire-github-actions-secret** (NEW — backlog).

The one architectural question worth surfacing: **the async git-push lifecycle (push → wait events:ACTIVE → record-deploy → verify → auto-close) is still convention-not-contract**. F1 + F2 are the immediate fixes; the deeper question is whether `record-deploy` should be the bridge at all, or whether `zerops_deploy strategy="git-push"` should block on the build event itself (synchronous from the agent's POV, async from the platform's). The current "agent must poll events then call record-deploy" pattern relies on agent discipline at exactly the post-compaction recovery boundary where discipline is weakest. Worth a separate design discussion before more layers depend on it.

---

## How to re-verify after Round 3 lands

```sh
ZCP_RUN_MATRIX=1 go test ./internal/workflow -run TestLifecycleMatrixDump -count=1
# expect: 2 anomalies (size warnings only)

go test ./internal/tools -run 'TestRecordDeploy_GateRunsOnSubsequentCalls|TestDeployTool_GitPush' -count=1
# expect: new pin tests for F1, F2 land green; F3 substring lists tightened

go test ./internal/content -run TestNoRetiredVocabAcrossRepo -count=1
# expect: zero new violations after F4, F6 land
```

Plus: run Codex `gpt-5.3-codex-spark` adversarial pass after the fix commits to verify nothing regressed on the surface area touched.

---

## ADDENDUM — Second Codex pass (gpt-5.3-codex-spark, after the report above was drafted)

Two independent Codex runs surfaced 7 more findings beyond F1-F8. Six are real (F9-F14); one is refuted as already-fixed (F15). The `develop-build-observe.md:25` placeholder leak (F9) was independently flagged by BOTH runs — strong signal it's a real bug.

### Upgrades to existing findings

#### F1 — UPGRADE: gate is broken in TWO ways, not one

In addition to "skipped on subsequent calls" (this audit's original F1), the gate ALSO fails to correlate `Status=ACTIVE` to the agent's just-pushed build:

**Location**: `internal/tools/workflow_record_deploy.go:201`

The `recordDeployBuildStatusGate` picks the FIRST recent `deploy`/`build` event and proceeds on `ACTIVE` without binding it to the build the agent just triggered. `ops.Events` doesn't expose a build ID the gate can correlate. So even when the gate runs (first record-deploy), it can pass on a STALE ACTIVE event from a previous deploy or — per `ops/progress.go:151` — from a `startWithoutCode` bootstrap appVersion. The progress code knows to skip those; the gate doesn't.

**Combined impact**: a service can ack a record-deploy that points at a build from minutes ago, not the one the agent just pushed. Combined with F1's original "skipped on subsequent calls" angle, the async git-push lifecycle is fragile at every iteration.

**Fix size**: STRUCTURAL — needs build-ID correlation. Either:
- `record-deploy` accepts an explicit `buildId` arg the agent must read from `zerops_events` first (binds the ack to a specific build).
- OR the gate filters events to those POST-DATING `meta.LastPushAt` (requires stamping push timestamps server-side).

**Severity**: keep CRITICAL — both aspects compound.

### NEW findings (F9-F15)

#### F9 — `develop-build-observe.md:25` has bare `{hostname}` outside `{services-list:…}` directive (aggregate atom)

**Location**: `internal/content/atoms/develop-build-observe.md:25` (frontmatter declares `multiService: aggregate` at line 8)

The atom has TWO `{hostname}` references:
- Line 25 (BARE, in a table cell): `tail \`zerops_logs serviceHostname="{hostname}" facility=application since=5m\``
- Line ~30 (inside `{services-list:…}`): `{services-list:zerops_workflow action="record-deploy" targetService="{hostname}"}`

For aggregate atoms, `synthesize.go:118` substitutes bare `{hostname}` with the GLOBAL primary host. In a multi-service git-push build (e.g. webdev + apidev both on close-mode=git-push), the failure-recovery hint tells the agent to tail logs for the global primary (whichever service sorts first lexically) rather than the FAILING service.

**Failure mode**: agent looking for build failure cause reads logs from the wrong service. Wastes diagnostic round-trips.

**Severity**: MEDIUM. Identified by BOTH Codex runs independently — strong signal.

**Fix size**: trivial — wrap line 25 in `{services-list:…}` OR move the failure column out of the per-service rendering.

This is exactly the M4 backlog item (aggregate-atom placeholder lint) catching a real LIVE leak. Upgrade M4 priority — automated lint would prevent regressions.

#### F10 — Default SSH `zerops_deploy` returns no `WorkSessionState`

**Location**: `internal/tools/deploy_ssh.go:217` returns raw `*ops.DeployResult`. `DeployResult` struct (`internal/ops/deploy_common.go:11`) has no `WorkSessionState` field.

Other deploy paths that DO carry it: `deploy_batch.go`, `deploy_git_push.go`, `deploy_local.go`, `deploy_local_git.go`, `verify.go`, `workflow_record_deploy.go`. The DEFAULT container SSH path — the most common deploy in close-mode=auto — is the one missing the F5 lifecycle signal that Round-2 added everywhere else.

**Failure mode**: a successful default SSH deploy can flip auto-close eligibility (via `RecordDeployAttempt` at `:215`); the agent doesn't see this in the response and has to call `action="status"` to discover the session closed. Extra round-trip + breaks the post-compaction recovery story.

**Fix size**: small — add `WorkSessionState *WorkSessionState` to `ops.DeployResult` (or carry in a tools-side wrapper as the other paths do). Mirror the F5 pattern.

**Severity**: HIGH (but borderline — spec P4 says mutations MAY be terse, so this is technically allowed; however the F5 sweep set the precedent that deploy responses carry session state, and the SSH default is the asymmetric outlier).

#### F11 — `bootstrap-provision-local.md:33` dotenv guidance can target a non-existent dev hostname

**Location**: `internal/content/atoms/bootstrap-provision-local.md:33`

Atom says: `zerops_env action="generate-dotenv" serviceHostname="{hostname}"`

For local+standard mode, the same atom's table at line ~16 says: "Standard mode: `{name}stage` only; no dev on Zerops". But bootstrap synthesis emits a dev snapshot from `plan.Targets[].DevHostname` (`bootstrap_guide_assembly.go:153`), and the global picker (`synthesize.go:428`) chooses the first hostname — which is the dev one.

So the agent runs `generate-dotenv serviceHostname="appdev"` against a service that doesn't exist on Zerops in local+standard mode → tool returns service-not-found.

**Failure mode**: local+standard bootstrap can't generate the .env file via the prescribed command.

**Severity**: MEDIUM. Local+standard is an established mode per `develop-close-mode-auto-local.md` axes.

**Fix size**: small — change `{hostname}` → `{stage-hostname}` for the local-standard render path, or add a mode-conditional snippet.

#### F12 — `develop-close-mode-git-push-needs-setup.md` fires for ModeDev, but git-push handler rejects ModeDev

**Location**: `internal/content/atoms/develop-close-mode-git-push-needs-setup.md:5` — frontmatter has NO `modes:` filter; `internal/tools/deploy_git_push.go:81` rejects with `PushSourceModeUnsupported` for ModeDev.

A dev-mode service whose meta is `closeDeployMode=git-push` + `gitPushState=unconfigured` matches this atom. Agent follows the setup walkthrough, runs `git-push-setup`, gets capability provisioned. THEN tries to deploy via git-push and the handler rejects: "git-push target X is in mode dev which does not support push-git". The agent has done all the setup work for nothing.

The downstream rejection's hint says to call `action="mode-expansion"` (which is **F5 above** — that action doesn't exist either). Two bugs cascade.

**Failure mode**: ModeDev users following the atom complete capability setup, then hit a hard rejection with a wrong-action hint. Stuck loop unless the agent reads the spec.

**Severity**: MEDIUM — rare combination (ModeDev + close-mode=git-push), but compounds with F5.

**Fix size**: small — add `modes: [standard, simple, local-stage, local-only]` to the atom (matches `topology.IsPushSource` predicate). Combined with F5 fix (correct mode-expansion guidance), the dev-mode case routes through bootstrap-with-isExisting instead.

#### F13 — `workflow.go:315` "Unknown action" error message lists 17 valid actions but switch handles ≥18

**Location**: `internal/tools/workflow.go:313-315`

Switch cases include `generate-finalize` (visible in switch grep). Error message lists `start, complete, close, skip, status, reset, iterate, resume, list, route, close-mode, git-push-setup, build-integration, classify, adopt-local, dispatch-brief-atom, record-deploy`. Missing from the message: `generate-finalize`. Per Codex pass 1, also missing `build-subagent-brief` and `verify-subagent-dispatch` (handled in pre-switch guards).

**Failure mode**: agent self-diagnosing with the error list won't discover three valid actions. Low impact (only matters when an agent typos one of the listed actions and looks at the recovery hint), but symptomatic of the same drift class as F2's wrong arg name — error string drifted from real implementation.

**Severity**: LOW.

**Fix size**: trivial — append the three missing action names to the error message.

#### F14 — Refute spec §2.6 "dangling references" claim from Codex pass 1

Codex pass 1 flagged `spec-workflows.md` lines 402, 587, 963 as dangling `§2.6` references after KT-9 deletion. Verified — these are CORRECT references to the renumbered §2.6 (Fast Paths, formerly §2.8). Round-2 explicitly updated these as part of KT-9 ("Cross-references: 3× `§2.8` → `§2.6` updated"). Codex misread.

### Updated severity summary

| ID | Severity | Title |
|---|---|---|
| F1 | CRITICAL (upgraded) | record-deploy gate broken in 2 ways: skipped on subsequent calls + no build-ID correlation |
| F2 | HIGH | Git-push hint uses non-existent `filterServices=` arg |
| F3 | HIGH | Tests miss F2 — only check loose substrings |
| F4 | HIGH | Credential signal matches retired `"push-dev"` strategy |
| F10 | HIGH (NEW) | Default SSH `zerops_deploy` returns no `WorkSessionState` |
| F5 | MEDIUM | Two hints reference unhandled `action="mode-expansion"` |
| F6 | MEDIUM | Eval scenario prose still says "push-dev strategy" |
| F7 | MEDIUM | Local close=auto deploy atom emits empty `targetService` |
| F8 | MEDIUM | Container deploy atom over-fires for standard mode |
| F9 | MEDIUM (NEW) | `develop-build-observe.md:25` aggregate-atom `{hostname}` leak (BOTH Codex runs flagged independently) |
| F11 | MEDIUM (NEW) | `bootstrap-provision-local.md` dotenv targets non-existent dev hostname in local+standard |
| F12 | MEDIUM (NEW) | `develop-close-mode-git-push-needs-setup` fires for ModeDev which handler rejects |
| F13 | LOW (NEW) | `workflow.go:315` "Valid actions" list missing `generate-finalize` (+2 pre-switch actions) |
| F14 | REFUTED | Spec §2.6 references already correctly updated by Round-2 KT-9 |

15 findings total: 1 CRITICAL (upgraded), 4 HIGH, 7 MEDIUM, 1 LOW, 1 REFUTED. All small/trivial except F1 which is now structural (build-ID correlation).

### Final fix order

**Round 3A — atomic small commit (~30 lines, ≥7 files)**:

1. **F1** (PARTIAL) — drop the `meta.FirstDeployedAt == ""` condition at `workflow_record_deploy.go:104` AND defer the build-correlation fix to a follow-up plan (structural). Addresses the worse aspect (skip-on-subsequent) immediately.
2. **F2 + F3** — rewrite both `filterServices=[%q]` to `serviceHostname=%q`; tighten test substring lists.
3. **F4** — change `strategies: []string{"push-dev"}` → `[]string{"zcli"}` in `deploy_failure_signals.go:216`.
4. **F5 + F12** — rewrite both `mode-expansion` hints to point at the bootstrap-with-isExisting flow; add modes filter to `develop-close-mode-git-push-needs-setup.md`.
5. **F6** — sweep "push-dev strategy" → "close-mode=auto" in `develop-pivot-auto-close.md:56`.
6. **F7** — `{stage-hostname}` → `{hostname}` at `develop-close-mode-auto-deploy-local.md:7`.
7. **F8** — add `modes: [dev, simple]` to `develop-close-mode-auto-deploy-container.md` frontmatter.
8. **F9** — wrap line 25 of `develop-build-observe.md` in `{services-list:…}` (or restructure the table).
9. **F10** — add `WorkSessionState *WorkSessionState` to `ops.DeployResult`; populate in `deploy_ssh.go` post-`RecordDeployAttempt`.
10. **F11** — fix dotenv hostname placeholder in `bootstrap-provision-local.md:33` for local+standard.
11. **F13** — append `generate-finalize`, `build-subagent-brief`, `verify-subagent-dispatch` to `workflow.go:315` error message.

**Round 3B — structural follow-up (separate plan)**:

12. **F1 (FULL)** — build-ID correlation in `record-deploy` gate. Either explicit `buildId` arg or push-timestamp filter. New backlog plan: `plans/backlog/record-deploy-build-id-correlation.md`.

**Round 3C — promote backlog**:

13. **M4** — automated aggregate-atom placeholder lint (already a backlog plan; F9 proves it's needed). Promote.

After Round 3A lands, the matrix simulator should still pass at 2 anomalies; no new test failures expected.

---

## ADDENDUM 2 — Re-verification with stronger Codex model (gpt-5.3-codex, not spark)

User flagged that the prior pass used `gpt-5.3-codex-spark` (a weaker model) and asked for re-verification with a stronger model. Re-ran the full F1-F13 verification against `gpt-5.3-codex` (non-spark) — 5+ minute deep-read pass. Result: **every finding F1-F13 INDEPENDENTLY CONFIRMED with additional evidence**, plus one new finding (F15) and one expanded scope (F6).

### Corrections to prior round-3 claims

#### C3 is actually CLOSED — round-3 incorrectly listed it as deferred

Round-3's "What remains genuinely OPEN" section flagged C3 (failureClassification on TimelineEvent) as DEFERRED with "Verify before closing." Verified now: `internal/ops/events.go:38-52` shows `TimelineEvent` HAS `FailureClass string` (line 49) AND `FailureCause string` (line 50), with a comment block at lines 30-37 explicitly documenting the C3 closure: *"FailureClass + FailureCause carry the structured classification for failed appVersion events ... C3 closure (audit 2026-04-29 + round-2 follow-up)."*

The atom `develop-build-observe.md:25` references `failureClass` + `failureCause` — those are CORRECT field references, not phantom fields. Round-2's claim "superseded by Round-2 implementation" was accurate, and round-3's hedge was wrong.

**C3 status: ✅ FIXED** (close the backlog plan `plans/backlog/c3-failure-classification-async-events.md`).

### Strong Codex re-verification — verbatim summary

| ID | Status | Strong Codex evidence |
|---|---|---|
| F1 | CONFIRMED | Both aspects: gate skip on subsequent calls (`workflow_record_deploy.go:100-109`) + ops.Events doesn't filter startWithoutCode while PollBuild does (`progress.go:79-90,151-157` vs `events.go:271-290`). Stale ACTIVE can pass. |
| F2 | CONFIRMED | No `filterServices` alias anywhere in codebase. |
| F3 | CONFIRMED | Loose substring tests at `deploy_ssh_test.go:1261-1266` AND `deploy_local_git_test.go:160-162`. No test pins `serviceHostname` in git-push guidance. |
| F4 | CONFIRMED | Modern handlers record `Strategy: "zcli"` (`deploy_ssh.go:171-181,187-197`; `deploy_local.go:125-148`). The signal at `deploy_failure_signals.go:213-218` matches `"push-dev"` — never fires. |
| F5 | CONFIRMED | Both hint sites confirmed; no `mode-expansion` case or alias in handler. Real path is bootstrap-with-isExisting per `develop-mode-expansion.md:16-35`. |
| F6 | **PARTIALLY-CONFIRMED + EXPANDED** | Original cite real, BUT understated. Prose drift exists in MULTIPLE eval scenarios: `develop-pivot-auto-close.md:53-57`, `develop-dev-server-container.md:3`, `develop-close-mode-unset-regression.md:3,22,32,44`. (Plus `close-mode-git-push-setup.md:17-31` references retired vocab in *commentary* — intentional, not drift.) |
| F7 | CONFIRMED | Confirmed against full envelope-construction chain: `compute_envelope.go:206-219+301-318` shows StageHostname only set on dev-keyed standard; for `[dev, stage, local-stage]` it's always empty. |
| F8 | CONFIRMED | Renderer at `synthesize.go:87-96,115-149` emits ALL matching atoms — no supersession logic. Standard envelope sees both atoms; container atom's wrong snippet is rendered. |
| F9 | CONFIRMED | Full chain: `synthesize.go:116-135,417-446` confirms global picker substitutes bare `{hostname}` outside `services-list` directive in aggregate atoms. |
| F10 | CONFIRMED | All wrapper sites enumerated: deploy_local, deploy_batch, deploy_git_push, verify, record-deploy ALL wrap with WorkSessionState; only deploy_ssh returns raw DeployResult. `RecordDeployAttempt` returns only `error`, no state. |
| F11 | CONFIRMED | Full chain: bootstrap synthesis emits dev snapshot first; atom has no service-scoped axes so global picker chooses dev hostname; that hostname doesn't exist on Zerops for local+standard. |
| F12 | CONFIRMED | `IsPushSource` predicate (`predicates.go:75-98`) excludes ModeDev/ModeStage; `PushSourceCheckFor` returns `PushSourceModeUnsupported` (`service_meta.go:228-241`); both git-push handlers reject. Dev-mode service can land at the atom + walk through setup, then hit hard reject. |
| F13 | CONFIRMED | Switch has `case "generate-finalize":` (`workflow.go:259-266`); pre-switch handles `build-subagent-brief` + `verify-subagent-dispatch` (`workflow.go:201-209`); default error message at `:311-315` omits all three. |

### NEW finding from re-verification

#### F15 — `WorkflowInput.Action` jsonschema description ALSO omits the three valid actions (same drift as F13's error message, different surface)

**Location**: `internal/tools/workflow.go:31-32` (the jsonschema field descriptor)

The `Action` field's jsonschema description lists 17 valid actions: `start, complete, skip, status, reset, iterate, resume, list, route, close-mode, git-push-setup, build-integration, classify, adopt-local, dispatch-brief-atom, record-deploy` — the SAME list as the `default:` error message at `:311-315`. Missing: `generate-finalize`, `build-subagent-brief`, `verify-subagent-dispatch`.

**Failure mode**: agent reading the MCP tool schema (which is what the agent's tool catalog shows at session start) doesn't discover the 3 actions. They're effectively undocumented unless the agent stumbles on them via spec or another atom.

**Severity**: LOW — same root as F13; documentation surface drift. Fix together.

**Fix size**: trivial — extend both the jsonschema description AND the default error message in one commit.

### Final tally after re-verification

15 findings:
- 1 CRITICAL (F1, dual-aspect)
- 4 HIGH (F2, F3, F4, F10)
- 7 MEDIUM (F5, F6, F7, F8, F9, F11, F12)
- 2 LOW (F13, F15)
- F14 REFUTED (spec §2.6 references already correct)

PLUS 1 correction: C3 is actually CLOSED, not deferred. Update `plans/backlog/c3-failure-classification-async-events.md` accordingly (likely delete).

### F6 expanded fix

Sweep these eval scenario files instead of just one:

| File | Drift sites |
|---|---|
| `internal/eval/scenarios/develop-pivot-auto-close.md` | line 56 ("běží pod push-dev strategy") |
| `internal/eval/scenarios/develop-dev-server-container.md` | line 3 (description: "Container env + push-dev + dev mode...") |
| `internal/eval/scenarios/develop-close-mode-unset-regression.md` | lines 3, 22, 32, 44 (multiple instances of "strategy" prose) |

The `close-mode-git-push-setup.md` mentions of `action="strategy"` are intentional `forbiddenPatterns` + commentary — leave alone.

Bundle F6 fix into the same commit as F4 (vocab sweep tightening). Also extend `repo_drift_test.go::retiredVocabAllowlist` with the prose phrases (`push-dev strategy`, `push-git strategy`, `strategy field`, `running under .*strategy`) so future drift surfaces automatically.

### Updated fix order — Round 3A (revised)

Same 11 fixes as before, plus:
- F15 → bundle with F13 (one commit, two surfaces).
- F6 → expand to 3 files (was 1).
- Drop "C3 backlog" from Round 3 follow-up — it's closed.

Total estimated change: ~30 lines across ~10 files plus 2-3 new test pins. All confirmed by stronger Codex model; no findings need re-assessment.

---

## Summary for the impatient reader

After three audit rounds, two Codex passes (spark + gpt-5.3-codex), and my own greps:

- **Round-1 + Round-2 closed cleanly** — all the structural fixes (C1 vocab sweep, H1+H2+C10 plan-spec-align, C2 git-push auto-stamp removed, F5 WorkSessionState propagation, KT-1..10 bootstrap-atom Option B purge, lifecycle matrix simulator) landed correctly.
- **Round-3 surfaces 14 NEW bugs both prior rounds missed** — all small/trivial fixes except F1 which is structural (build-ID correlation in record-deploy gate).
- **One round-3 hedge is wrong** — C3 is actually closed (TimelineEvent has FailureClass + FailureCause).
- **F1 is the only CRITICAL** — record-deploy gate broken in 2 ways (skip on subsequent calls + no build correlation, latter pattern documented in `progress.go::PollBuild` for a reason). Compound bug; corrupts iterating-redeploy lifecycle for webhook/actions integrations.
- **F2-F4 are HIGH** — git-push response gives wrong arg name + tests don't catch it + credential classifier matches retired strategy. All in the freshly-refactored async lifecycle handoff.
- **F6 is wider than originally cited** — drift in 3 eval files, not 1.
- **All findings independently confirmed by stronger Codex** with file:line evidence beyond original citations.

Ship Round 3A (~30 lines, ~10 files, atomic commit) before starting internal testing.

---

## ADDENDUM 3 — Independent Opus re-verification (third-eyes pass)

User flagged that the prior pass relied on Codex (gpt-5.3-codex-spark + non-spark). They asked: did I (Opus) also do an independent re-verification with another Opus agent? Answer: no — I'd verified manually but no fresh-context Opus had read the cited code. Spawned an Explore agent on `model: opus` for a third independent pass.

The Opus pass took the same scope (F1-F15) and re-read every cited file. Result: most findings hold, but **two corrections to the audit + 8 NEW findings the Codex passes (and I) collectively missed**.

### Corrections to existing findings

| ID | Original audit claim | Opus correction |
|---|---|---|
| **F1** | CRITICAL | **Downgrade to HIGH** — claim (b) reachability is narrower than written: the `develop-record-external-deploy` atom requires `buildIntegrations: [webhook, actions]`, which agents typically only set on push-source services. Stage in standard mode never uses startWithoutCode (`bootstrap-provision-local.md:22` forbids it). The trap fires only for ModeDev/ModeSimple/standard-dev WITH startWithoutCode AND webhook/actions integration — narrow but real. |
| **F6** | 3 eval scenarios with prose drift | **2 files, not 3.** `develop-close-mode-unset-regression.md` does NOT contain "push-dev" prose anywhere — it references the `develop-strategy-review` atom (which legitimately exists at `internal/content/atoms/develop-strategy-review.md`). Audit conflated "strategy" → "push-dev strategy". Real drift sites: `develop-pivot-auto-close.md:56` + `develop-dev-server-container.md:3`. |
| **F11** | MEDIUM — bootstrap-provision-local dotenv targets non-existent dev hostname | **REFUTED.** `compute_envelope.go::buildSnapshot` populates `envelope.Services` from REAL Zerops services. For local+standard, only the stage svc exists on Zerops (per atom line 16). The picker correctly picks the stage svc — `{hostname}` resolves to e.g. `apistage` which DOES exist. The audit's mental model of "dev snapshot in envelope" was wrong. Smaller real concern: for local+standard with multiple stage services, the picker only emits the dotenv command for ONE of them — that's an axis-coverage gap (LOW), not a "non-existent hostname" bug. |
| **F15** | LOW — Action jsonschema missing 3 actions | **4 missing, not 3.** Audit miscounted. Action jsonschema at `workflow.go:32` lists 16 actions; in addition to the 3 named (`generate-finalize`, `build-subagent-brief`, `verify-subagent-dispatch`), `close` is also missing from the description text. |

Net result: 11 confirmed (F2, F3, F4, F5, F7, F8, F9, F10, F12, F13, F1-downgraded), 2 partial (F6 narrowed, F15 wider), 1 refuted (F11), 1 already-refuted (F14).

### NEW findings from Opus pass (O1-O8)

#### O1 — HIGH — `zerops_deploy_batch` NEVER records DeployAttempts in work session

**Location**: `internal/tools/deploy_batch.go::RegisterDeployBatch:113` calls `ops.DeployBatchSSH(...)` and never invokes `workflow.RecordDeployAttempt` for any per-target result. Single-target paths (`deploy_ssh.go:215`, `deploy_local.go:147,161`, `deploy_local_git.go:65,224`, `deploy_git_push.go:196,364`) all record. Batch's only mutation is `WorkSessionState: sessionAnnotations(stateDir)` (line 131) which READS the session but never WRITES a deploy.

**Smoking gun**: comment at `internal/ops/deploy_batch.go:66` claims "RecordDeployAttempt is protected by workSessionMu in the workflow layer" — implying it gets called somewhere. Grep confirms: `RecordDeployAttempt` is not called from `deploy_batch.go` OR `ops/deploy_batch.go`. Comment is stale and misleading.

**Failure mode**:
- Work-session `Deploys` map is empty after a successful batch deploy.
- `EvaluateAutoClose` (`work_session.go:341`) requires per-host successful attempts → never auto-closes when agent uses batch.
- `HasSuccessfulDeployFor` (`work_session.go:319`) returns false → `DeriveDeployed` (`compute_envelope.go:259-269`) falls back to platform Status; for fresh deploys not yet stamped on meta, envelope reports `Deployed=false` while build actually landed.
- `stampFirstDeployedAt` only fires from `RecordDeployAttempt` → batch deploys never stamp `FirstDeployedAt`.

**Severity**: HIGH. Same severity class as F1 (silent state corruption affecting auto-close + envelope). Both Codex passes and I missed this entirely.

**Fix size**: small — call `workflow.RecordDeployAttempt` per-target after the batch result returns; mirror the single-target pattern. Drop the stale comment.

**Why both Codex passes missed it**: deploy_batch.go is a recipe-flow tool per its docstring. Both Codex passes correctly skipped recipe-authoring scope, but didn't catch that the BATCH TOOL itself (registered globally in MCP server) is reachable from develop-flow agents and breaks Work Session invariants when called.

#### O2 — MEDIUM — `events.go::Events()` async classifier doesn't pass `Strategy`

**Location**: `internal/ops/events.go:257-265` builds `FailureInput{Phase, Status, BuildLogs}` — no `Strategy` field. Strategy-filtered failure signals (`transport:zcli-auth-failed`, `transport:git-auth-failed`, `transport:git-token-missing`) require `Strategy` to match — they can never fire from the events tool path.

**Failure mode**: For BuildIntegration=webhook/actions builds (the primary audience for `develop-build-observe.md`), the agent only sees baseline classification. Credential-class signals (e.g. webhook auth failure during clone — a real build-phase failure) silently fall through to baseline.

**Caveat**: in practice the events.go classifier only runs for build-phase failures (`FailurePhaseFromStatus` returns Build/Prepare/Init), and the named transport signals are tagged `PhaseTransport` — wrong phase, so they wouldn't fire anyway. But the Strategy plumbing is the right pattern to set up before adding a credential signal at PhaseBuild.

**Severity**: MEDIUM (real plumbing gap, narrow practical impact).

**Fix size**: small — pass `Strategy` from the appVersion event into FailureInput. Will need a new field on TimelineEvent or inferred from ZCP convention (webhook builds = git-push strategy, actions = git-push strategy).

#### O3 — MEDIUM — Root cause of F12: `handleCloseMode` doesn't validate `IsPushSource`

**Location**: `internal/tools/workflow_close_mode.go:78-127` accepts `CloseModeGitPush` for ANY mode (only blocks `CloseModeAuto` for `PlanModeLocalOnly` at line 98).

`handleCloseMode` SHOULD also reject `CloseModeGitPush` when `!topology.IsPushSource(meta.Mode)` — that's the structural fix. The atom's missing modes filter (F12) is downstream collateral; the real root cause is the close-mode setter accepting an invalid combination in the first place.

**Failure mode**: same as F12 — agent sets git-push for ModeDev, atom fires, walks setup, deploy rejects.

**Severity**: MEDIUM. Same blast radius as F12 but addresses the root rather than the symptom — better for long-term coherence.

**Fix size**: small — add `IsPushSource` check in `handleCloseMode` before accepting `CloseModeGitPush`. With this fix, F12's atom-level fix becomes redundant (the meta would never reach gitPushState=git-push for ModeDev).

#### O4 — LOW — `recordDeployBuildStatusGate` eats fresh BUILD_FAILED from prior attempt as "did not land"

**Location**: `workflow_record_deploy.go:201-208` picks first deploy/build event by timestamp DESC. If latest event is BUILD_FAILED from a prior failed push and agent retries push successfully but calls record-deploy too early, the events list might still show the prior failure as latest. Status=BUILD_FAILED → gate refuses with "did not land successfully" message that doesn't acknowledge "could be a stale event from prior attempt".

**Failure mode**: agent has to guess "wait longer" — error message at lines 226-229 doesn't suggest retry-after-poll.

**Severity**: LOW. UX bug, not state corruption. Agent eventually figures it out from the atom prose.

**Fix size**: trivial — add "(could be a stale event; poll zerops_events serviceHostname=X until newer ACTIVE appears)" to the error suggestion text.

#### O5 — LOW — `validateDeployStrategyParam` doesn't accept `"zcli"` as alias for default

**Location**: `internal/tools/deploy_strategy_gate.go:21-37` accepts `""` and `"git-push"` as valid strategy parameters; rejects `"manual"` AND `"zcli"` AND anything else.

The internal label "zcli" appears in DeployAttempt records (`Strategy: "zcli"` per `deploy_ssh.go:181`) and atom prose. An agent who tries `strategy="zcli"` thinking it's a valid value gets "Invalid strategy" with message "Valid values: omit (default push) or 'git-push'" — but doesn't mention "zcli" as a known-bad alias.

**Failure mode**: minor agent confusion; atoms don't use this form so the path is rare.

**Fix size**: trivial — either accept "zcli" as silent alias for default, OR mention it in the rejection hint.

#### O6 — LOW — `recordDeployBuildStatusGate` adds platform round-trip to "workflow-less by design" path

**Location**: `workflow_record_deploy.go:103` invokes `recordDeployBuildStatusGate` which fetches `zerops_events` (1 platform API call). The record-deploy handler's docstring at line 50-54 says it's "workflow-less by design" — the gate adds latency the path didn't previously have.

**Severity**: LOW. Acceptable trade-off; defense-in-depth costs one round-trip per call. Documented behavior.

**Fix size**: NONE (intentional trade-off). Worth noting in audit so future maintainers don't strip the gate "to make record-deploy faster".

#### O7 — LOW — `atoms_lint.go` missing rule for `closeDeployModes`-without-`modes:` pattern

**Location**: `atoms_lint.go:187-211` has `staleStrategyViolations` lint that catches atom bodies with retired vocab. It does NOT lint for atoms declaring `closeDeployModes:` without also declaring `modes:` to gate to push-source modes.

F8 + F12 both have this shape: atom matches via close-mode axis but doesn't restrict to push-source modes, then the snippet is wrong for non-push-source services. A new lint rule would catch both classes:

> Atoms declaring `closeDeployModes: [git-push]` MUST also declare `modes:` with values from `topology.IsPushSource` (Standard, Simple, LocalStage, LocalOnly).

**Severity**: LOW (defense-in-depth). Backlog M4 (aggregate-atom placeholder lint) covers a similar gap; this would be a sibling lint.

**Fix size**: small — extend `atoms_lint.go` with the rule. Pin in `atoms_lint_test.go`.

#### O8 — REFUTED claim that `develop-close-mode-unset-regression.md` had drift

(See F6 correction above.) The scenario references `develop-strategy-review` atom (real) and "deploy-strategy-decomposition" (real internal concept). Audit conflated these with retired "push-dev strategy" prose. Not a bug; absorbed into F6 narrowing.

---

### Updated severity tally after Opus pass

| Severity | Count | IDs |
|---|---|---|
| HIGH | 5 | F1 (downgraded), F2, F3, F4, F10, **O1** |
| MEDIUM | 8 | F5, F6 (narrowed), F7, F8, F9, F12, **O2**, **O3** |
| LOW | 5 | F13, F15 (expanded), **O4**, **O5**, **O7** |
| INFORMATIONAL | 1 | **O6** |
| REFUTED | 2 | F11, F14 |

19 actionable findings + 1 informational + 2 refuted. Total fix scope still ~30-40 lines across ~12-14 files in Round 3A.

### Updated Round 3A fix list

Same 11 fixes as before, plus:
- **O1** — call `workflow.RecordDeployAttempt` per-target in `deploy_batch.go::RegisterDeployBatch` after batch result returns. Drop stale comment in `ops/deploy_batch.go:66`. Add `TestDeployBatch_RecordsPerTargetAttempts` pin. **(HIGH severity — bundle into the same Round 3A commit, not deferred.)**
- **O3** — add `IsPushSource` check in `handleCloseMode` before accepting `CloseModeGitPush`. Renders F12 atom-only fix redundant — implement O3 instead. Add `TestHandleCloseMode_RejectsGitPushForNonPushSourceMode` pin.
- **O7** — extend `atoms_lint.go` with the closeDeployModes-without-modes rule. Catches future regressions of the F8/F12 class.
- **F11** — REMOVE from fix list; audit was wrong.
- **F15** — extend fix to add `close` as well as the 3 named actions (4 total missing).
- **F6** — sweep 2 files (`develop-pivot-auto-close.md` + `develop-dev-server-container.md`), not 3.

Defer to Round 3B (separate plan):
- **O2** — Strategy plumbing into FailureInput from events path. Touches the `internal/ops/events.go` build-classifier signature. Worth its own focused plan.
- **F1 (b structural)** — build-ID correlation in record-deploy gate. Already in backlog as `record-deploy-build-id-correlation.md`.

### What this exercise taught

1. **Three independent passes (Codex spark + Codex non-spark + Opus) did not produce identical findings.** Each layer caught what the others missed. Opus caught the most consequential miss (O1), which Codex's whole-system audit and my own greps both walked past.
2. **Audit accuracy degrades over chains of citation.** Round-3 cited Codex citing primary code; one error compounded (F11) and one count was off-by-one (F15). The Opus pass that re-read primary code surfaced both.
3. **The "small finding" trap is real.** F6 reads as cosmetic prose drift, but the `develop-close-mode-unset-regression.md` claim was REFUTED — flag-on-keyword instead of read-the-line. Worth it before fixes land.

User was right to push for the third pass.
