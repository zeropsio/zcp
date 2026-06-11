# Single-token launch lifecycle — one credential from create-project to CI

**Date:** 2026-06-11 (rev 2 — Karel's protocol refinement)
**Origin:** Karel's direction: the launchKey is one-shot only by convention
(manual revoke; platform expiry is roadmap). Use ONE token for both creating
the project AND GitHub Actions. The token can create unlimited projects and
has access ONLY to the projects it created (Karel-confirmed platform
semantics). Integration tokens cannot mint or regenerate tokens
(live-verified). Regeneration is a RECOMMENDATION note, not an enforced step.

**The protocol (Karel, rev 2):** when ZCP receives the token, it immediately
sets it as a SECRET on the source dev service; for the whole integration
window ZCP works with it ONLY through that secret (never through repeated
MCP parameters); when the project is CONFIRMED fully functional, ZCP deletes
the env — which closes the window physically (nothing left to read), not by
policy. The window must stay open through CI/CD setup and the first builds:
problems can surface there and ZCP must still be able to fix them or delete
the whole project and start over (reset).

## Live-verified facts this design rests on (2026-06-11)

| Fact | Evidence |
|---|---|
| Integration token CANNOT create/regenerate/delete tokens — `403 notAllowedForIntegrationToken` before ID resolution | live POST create + PUT regenerate(fake id) |
| Integration token CAN READ token lists (ids+names, never values) — usable for the regenerate deep-link | live GET both list endpoints, 200 |
| Regenerate (GUI): "creates a new token value while preserving all settings (the old token immediately stops working)" | rbac.mdx |
| The launch token reaches the NEW prod project (creator access per Karel; GrantSelfRole(ADMIN) on the token's clientUserId is the belt-and-braces ZCP already does) — same token authenticates `zcli push` there | workflow_launch_production.go:742 + Karel-confirmed semantics; verify creator-access live in T1 e2e |
| Service-scope env writes are ALWAYS Type=SECRET (write-only read-back via API for low-privilege tokens); project-scope `sensitive` does NOT persist | ops/env.go GIT_TOKEN precedent + recon |
| Fresh SSH sessions see a platform env write in ~5-10s (no restart) — the in-container read path | git-push-setup session-auth verify, live-verified 2026-06-10 |
| GitHub repo secrets: write-only, sealed-box, no read audit; write access ≈ read access (workflow-edit exfiltration); environment+required-reviewers mitigation needs public repo or Pro/Team/Enterprise tiers | docs.github.com |
| zcli auth is `zcli login <token>` only | cli.mdx |

## The lifecycle

```
user (GUI, once): create integration token — canCreateProjects
  ↓ launchKey input — the ONLY time the value crosses the transcript
ZCP launch mutation:
  1. create project + import (startWithoutCode) + GrantSelfRole ADMIN
  2. IMMEDIATELY stage the token: EnvSetSecretService(dev service,
     ZEROPS_TOKEN_PROD, launchKey)   ← the protocol's single working copy
  ↓ from here on, EVERY launch-window operation reads the token from the
    staged secret (ZCP server-side via SSHDeployer 'printf %s "$ZEROPS_TOKEN_PROD"',
    local mode via VPN ssh) — pipeline re-check, prod-ops, reset, watch.
    The agent NEVER passes launchKey again; the transcript never re-sees it.
  ↓ delivery wiring (launched response):
  gh secret set ZEROPS_TOKEN_PROD -b "$(ssh dev 'printf %s "$ZEROPS_TOKEN_PROD"')" \
    -R owner/repo   (GH_TOKEN conveyance unchanged) — value goes to GitHub
    without re-entering the transcript
  ↓ first release: tag → pipeline → prod runs
  ↓ WINDOW STAYS OPEN: CI/CD problems, broken builds, reconfiguration, or
    full project delete + restart (reset reads the token from the staged env)
    are all still ZCP-solvable through the staged secret
  ↓ action="confirm-production" (explicit, user-acked; preconditions: first
    release live on the prod runtimes + smoke check done):
  ZCP DELETES the staged env → the window is closed PHYSICALLY — ZCP has
  nowhere to read the credential from; "never works with it again" enforced
  by absence, not policy. Final response carries the regenerate NOTE:
  "recommended: regenerate the token in the dashboard (deep-link) and re-run
  the prepared gh secret set with the new value — kills every copy the
  transcript ever saw; the GitHub secret stays the only live one."
```

## Why this shape

- **One token, one GUI visit**; transcript sees the value exactly once.
- **The staged secret is the single working copy** — repeated operations are
  transcript-safe by construction (the GIT_TOKEN credential-helper pattern,
  already proven for git pushes).
- **Window close = env delete** — elegant physical enforcement. prod-ops after
  close fails for lack of credential, with a message naming the lifecycle
  (fresh prod-scoped key / regenerate) rather than a policy refusal that the
  token would technically still satisfy.
- **Recovery stays first-class until confirmation**: CI/CD issues and the
  delete-and-restart path (reset) keep working through the staged copy — no
  re-prompting the user for the token mid-recovery.
- **Regeneration is a note**, not a gate (Karel): good to do, never nagging.

## Phases

### T1 — staging + conveyance (the protocol core)
- Launch mutation: immediately after project create succeeds, stage the token
  (`ops.EnvSetSecretService(devServiceID, "ZEROPS_TOKEN_PROD", launchKey)`).
  Staging failure = launch failure (the protocol depends on the staged copy).
- `topology.classifyInfrastructureKeys`: add exact key `ZEROPS_TOKEN_PROD`
  (bundle-leak guard — else export/launch bundles carry the value into
  agent-visible YAML).
- prodCD `secret.command`: replace the paste placeholder with the SSH read of
  the staged secret; `secret.source` rewritten to the single-token truth.
- GrantSelfRole stays; verify creator-access semantics live in T1 e2e (if the
  platform grants creator access automatically, the grant is belt-and-braces
  and stays non-fatal; if not, it becomes blocking).
- Tests: staging write in mutation; command shape; classify pin.

### T2 — secret-sourced operations (transcript-safe window)
- `launchKeyFromStage`: pipeline resume, prod-ops, reset, and the first-release
  watch resolve the token by reading the staged secret server-side
  (SSHDeployer exec; local mode over VPN ssh) when `launchKey` is absent.
  Explicit `launchKey` param still accepted (fallback when the env is gone).
- jsonschema + atoms: "pass launchKey ONCE; every later call reads the staged
  secret — do not re-send the value."
- P-LP-1 extension: the staged read is in-request only — never persisted,
  never echoed; pinned like the existing launchKey paths.

### T3 — confirm-production + close
- New `action="confirm-production"` (launch workflow): preconditions = first
  release observed live on prod runtimes (prod-ops listing via staged token) +
  explicit user ack of the smoke check. Effect: DELETE the staged env, stamp
  `launchState.WindowClosedAt` (for honest status messages), return the final
  lifecycle response with the regenerate NOTE + dashboard deep-link (token id
  via the list API) + prepared re-set command.
- prod-ops/resume after close: missing staged secret + no explicit launchKey →
  refuse with the lifecycle message.
- Atoms: launch-intro (single-token lifecycle), launch-delete-key →
  launch-token-lifecycle (staged-secret protocol + confirm step + regenerate
  note), post-checklist final step = confirm-production.

### T4 — GitHub hardening guidance (atom-only, optional)
- Recommendation: move ZEROPS_TOKEN_PROD to a `production` ENVIRONMENT secret
  + required reviewers where the plan allows (public any plan; private needs
  Pro/Team for environments, Enterprise for required reviewers), with the
  honest threat-model line (write access ≈ read access on plain repo secrets).

## Not doing
- No ZCP-side token minting or auto-regenerate (platform forbids both for
  integration tokens; regenerate is the user's GUI act).
- No forced regeneration — note only (Karel rev 2).
- No GitHub API direct writes (one-collect backlog unchanged); no OIDC.

## Resolved questions (Karel rev 2)
1. Window close: NOT at first release — only at explicit confirm-production
   after the project is verified fully functional; closing = deleting the
   staged env (physical, not policy).
2. Regenerate: checklist/closing NOTE only, never a blocker.

---

# Implementation plan (T1–T4)

TDD per phase (RED first); per-item walk + report before "phase shipped".
Working tree carries foreign export-Variant WIP — partial-stage if a shared
file is touched (`workflow.go` jsonschema strings are the likely overlap).

## T1 — staging + conveyance

Order inside `executeLaunchMutation` (workflow_launch_production.go):
key validation (`projectAdminClientFactory`) → **stage** → create+import.
Staging failure aborts BEFORE the irreversible create.

1. `ops` helper reuse: `ops.EnvSetSecretService(ctx, client, devServiceID,
   launchTokenStageKey, input.LaunchKey)` — dev service resolved via
   `ops.LookupService(ctx, client, projectID, state.TargetServiceHostname)`
   (push hostname = meta primary key). New const
   `launchTokenStageKey = "ZEROPS_TOKEN_PROD"` single-owned in tools (or ops)
   — every tell derives from it.
2. `topology.classifyInfrastructureKeys`: add exact key `ZEROPS_TOKEN_PROD`
   (bundle-leak guard). Pin: classify test asserting Infrastructure bucket.
3. prodCD block (workflow_launch_production.go:1841-1852): `secret.command`
   `-b "<paste...>"` → `-b "$(ssh <flags> <devHost> 'printf %s "$ZEROPS_TOKEN_PROD"')"`;
   `secret.source` rewritten (staged-secret truth, no paste). Pin:
   TestProdCDActionsBlock asserts no `<paste` + the nested ssh read.
4. GrantSelfRole stays non-fatal pending the T1 e2e creator-access check
   (operator-assisted: needs a real launchKey-grade token from Karel; if the
   platform does NOT auto-grant creator access, flip to blocking).
5. Existing-project path (launch_existing.go): SAME staging with
   ExistingProdToken (parity — its window has the same recovery needs).
6. Pins: `TestExecuteLaunchMutation_StagesTokenBeforeCreate` (order +
   abort-on-stage-failure), `TestLaunchStaging_KeyNeverInState` (P-LP-1
   extension), classify pin, prodCD shape pin.

## T2 — secret-sourced operations (transcript-safe window)

1. New helper `launchKeyFromStage(ctx, sshDeployer, rt, stateDir, state)
   (string, error)`: SSH-exec `printf %s "$ZEROPS_TOKEN_PROD"` on the push
   hostname (container mode via sshDeployer; local mode same — VPN ssh).
   In-request only; never logged/persisted/echoed (errwire audit).
2. Wire as fallback when `input.LaunchKey == ""` at all four read sites:
   prod-ops (launch_prod_ops.go:92-100), pipeline resume
   (workflow_launch_production.go:337/:531), reset (launch_reset.go:112/:146).
   Empty stage read → today's "launchKey required" error EXTENDED with the
   lifecycle line (window closed / stage deleted).
3. jsonschema `launchKey` (workflow.go:188): "pass ONCE at the mutation; every
   later launch-window call reads the staged secret — do not re-send."
4. Pins: `TestProdOps_ReadsStagedToken` (no launchKey param → staged read →
   factory called with staged value; mock SSH), `TestProdOps_StageEmpty_Refuses`,
   `TestPipelineResume_StagedToken`, `TestLaunchReset_StagedToken`,
   sentinel: staged value never appears in response/state/audit goldens.

## T3 — confirm-production + physical close

1. New `action="confirm-production"` (workflow.go enum + dispatch; launch
   workflow only). Handler:
   a. requires launch state `Status==launched`;
   b. reads prod services via staged token (best-effort liveness: every
      promoted runtime ACTIVE; warn-not-block on unreachable);
   c. requires explicit `confirmFunctional: true` input (user ack of smoke);
   d. DELETES the staged env (`ops.EnvDelete` path used by zerops_env
      action=delete) — delete FIRST, then stamp;
   e. stamps `launchState.WindowClosedAt` (honest status messages only —
      enforcement is the deleted env);
   f. response: closing summary + regenerate NOTE + dashboard deep-link
      (best-effort token-id lookup via new
      `platform.ListIntegrationTokens(clientId)` wrapper over SDK
      GetClientIntegrationTokenList; fallback = generic Settings → Access
      Tokens link) + prepared `gh secret set` re-set command.
2. Post-close behavior: T2's stage-empty refusal carries the lifecycle
   message ("window closed; fresh prod-scoped key or regenerate").
3. Atoms (IDs kept — bodies rewritten): `launch-intro` (single-token
   lifecycle; two-window paragraph replaced), `launch-delete-key` → body
   becomes the staged-secret protocol + confirm step + regenerate note
   (id kept to avoid corpus re-pinning), `launch-post-checklist` final step =
   confirm-production. Goldens regen.
4. Spec: docs/spec-workflows.md §10 — new invariant **P-LP-14** (staged-secret
   protocol: stage before create; window ops read stage only; close = env
   delete at confirm-production; the staged value never crosses the wire
   surfaces) + status row for confirm-production.
5. Pins: `TestConfirmProduction_DeletesStageAndStamps`,
   `_RequiresAck`, `_RefusesBeforeLaunched`,
   `TestProdOps_AfterClose_LifecycleMessage`, golden + PinCoverage updates.

## T4 — GitHub hardening note (atom-only)

- prodCD/atom note: environment `production` secret + required reviewers
  where the plan tier allows (public any plan; private: Pro/Team for
  environments, Enterprise for required reviewers); honest threat-model line
  (plain repo secret readable by any write-access collaborator via workflow
  edit). No code.

## Verification gates
- Per phase: unit + tools + integration short suites, lint-local, goldens.
- After T3: launch flow-eval scenario (read-side; mutation stops at key ask)
  + full -race.
- Operator-assisted live e2e (Karel mints a disposable launchKey-grade token
  on eval org): full T1-T3 chain against a real new project — staging,
  secret-sourced prod-ops, confirm-production close, creator-access check
  (T1 item 4 decision). Project deleted via reset afterwards.

## Effort
~6 files core + atoms + spec; T1 ~200 LOC, T2 ~180, T3 ~280 + atoms, T4 atom
text. Tests ~400 LOC. 1.5–2 dny vč. eval round-tripu.

---

# Ship log (2026-06-11)

T1 `b8f54c91` · T2 `2bcbbade` · T3 `b6fb674f` · T4 `2978cc09` · de-drift `88795a9b`.
All four phases shipped on main; unit + tools + integration + full `-race`
green, `lint-local` clean, atom goldens regenerated.

**Deviation from plan (flagged):** `launchKeyFromStage` reads the staged
secret through the platform API (source-project env store) instead of the
planned SSH `printf` exec. Reason: the SSH deployer does not exist in local
mode (nil — constructed only in-container), so the SSH mechanism could not
deliver the plan's own "local mode same" commitment; the API read also works
with a stopped/broken dev container, which reset recovery needs. The
conveyance to GitHub (prodCD secret.command) remains SSH-based as designed.
Swap back = internals of one helper behind the same boundary.

**Residuals (not in scope, recorded):**
- `zerops_discover includeEnvValues=true` can surface the staged value to the
  transcript on the user's own project — same pre-existing exposure class as
  GIT_TOKEN; candidate for a credential-key value mask in discover.
- `action="reset"` does not delete the staged secret (a relaunch re-stages
  idempotently; an abandoned launch leaves the env behind — delete it
  manually or via confirm-production on the next launch).

**Verification gates — CLOSED (2026-06-11 evening):**
- flow-eval `launch-production-dev-only` (read-side): no regressions; surfaced
  3 stale one-shot-language atoms (fixed, `2054b8fc`) + the adopt-misdirect
  backlog entry.
- Operator-assisted live e2e: **PASS** — `TestE2E_LaunchSingleTokenLifecycle`
  (133s, full chain incl. the empty-remote state, Karel-minted token). Four
  product defects found and fixed by the live run:
  1. ssh client stderr ("Warning: Permanently added …") mixed into parsed
     value reads via CombinedOutput → false-blocked remote-mismatch on an
     exactly-matching URL. Fix: `LogLevel=ERROR` in both ssh arg builders
     (`e300937c`).
  2. The pipeline-first import carried `zeropsSetup` without `buildFromGit`
     → `projectImportInvalidParameter` ("required for use of
     pipelineConfig"). Fix: drop zeropsSetup from the launch import; the
     setup reference lives in the pipeline wiring (`b5b099b3`).
  3. **The platform forbids ZEROPS_-prefixed custom envs** → staging aborted
     (correctly, before create). The staged key is now `ZCP_LAUNCH_TOKEN`;
     the GitHub repo secret keeps `ZEROPS_TOKEN_PROD` (`f12c92e7`).
  4. prod-ops doneBoundary was not family-aware — actions family held
     "bring-up in progress" forever (same J5 suppression the launched
     response already had) (`f12c92e7`).
- **T1 item 4 RESOLVED: GrantSelfRole stays non-fatal.** Live: integration
  tokens cannot manage roles at all ("Insufficient permissions" on
  PutClientUserRoles), and creator access carries every launch-window
  operation — prod-ops listed the new project through the same token with
  no role grant. The warning text now names that expected shape.
- Karel's repo-clean ask done: `eval2` main force-reset to an empty orphan
  commit from the zcp container; the e2e resets it at start AND at the end,
  so the empty state is both the tested precondition and the standing state.
