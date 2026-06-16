# Live-platform end-to-end test suite + flow shape polish

**Origin**: Karel's manual launch-production test on 2026-05-16 (session
transcript `/tmp/karel-launch-prod.jsonl`). Three release-blocking issues
surfaced: `<@pickRandom(["REPLACE_ME"])>` preprocessor bug (fixed v
`4f242bfd`), `setup: prod` gate UX (backlog), first-deploy-failed recovery
(backlog). Plus Karel's session NEVER reached `status: launched` — 5×
`failed`, so terminal-state atoms (cicd handoff, delete-key) never
exercised in any real session.

**Rule from Karel**: NO push of v9.92 until end-to-end tests verify the
four flows work against the real Zerops platform — not just call-shape
conformance, but real mutation + verified outcome.

---

## 1. Flow shape — what's allowed to stay LLM-driven, what gets atom polish

Karel: "porad se tam nechava prostor pro to aby llm operativne resila
detaily dle stavu projektu" — keep LLM judgment over project-state-dependent
details. ZCP provides the rails (atoms, recovery hints, composer output);
LLM threads context. So flow polish is **atom + handler-emission level**,
not "automate the agent away."

### a) Token-to-project — kept mostly as-is

The two-path mutation (LaunchKey for new project, ExistingProdToken for
existing project) is already in Phase 2. Karel: "vícemíň OK." Polish only:

- Validation error messages list which client/project the token resolved
  to vs. what was requested (already partial — `ErrTokenScopeMismatch`
  message identifies both IDs).
- `P-LP-1 sentinel` enforced (already pinned in 3 tests).

**No structural change needed. Skip.**

### b) Git PAT + repo — atom that didn't fire in Karel's session

Current state:
- `setup-git-push-container.md` exists with fine-grained PAT scopes table
  (Contents R+W, Secrets, Workflows). Reachable only via
  `action=git-push-setup`.
- `setup-build-integration-actions.md` exists with workflow YAML +
  `gh secret set` commands. Reachable only via `action=build-integration`.
- Both fire on the `gitPushStates`/`buildIntegrations` axis values for the
  current service — they target ADAPTING an existing source service, not
  PRODUCING a new prod project's CI/CD setup.

**Karel's expectation**: When user says "deploy to production", ZCP should
guide them through git-PAT + repo BEFORE the launch attempt, so when the
new prod project is created it can pull source code via a working
buildFromGit URL with auth (where needed) or via push-mode CI/CD already
wired.

Two missing pieces:

1. **Pre-launch git-PAT inquiry**: handler should detect when source has
   `GIT_TOKEN`-style envs (auth for buildFromGit) and route the agent
   through PAT collection BEFORE mutation. Atom:
   `launch-source-git-auth-required` (new in Phase 6a).
   - Trigger: `envclass.ClassifyProjectEnv` finds env matching credential
     pattern AND name shape suggests git auth (`GIT_TOKEN`, `GITHUB_TOKEN`,
     `GITLAB_TOKEN`) AND `keepNonHA` doesn't drop it.
   - Action: agent collects PAT, classifies as plain-config (NOT
     external-secret — the value WILL flow into the new project's env).

2. **Post-launch CI/CD handoff**: when `status=launched`, ZCP emits the
   prod-side cicd handoff atom carrying workflow YAML + secret command.
   Atom: `launch-cicd-actions-handoff` (already in Phase 1c composer
   output; Phase 6b handler wiring promotes it).

**For v9.92 minimum**: atom #1 above promoted from Phase 6a. Atom #2
deferred to v9.95 (Phase 6b) because it requires terminal-phase reshape.

### c) Failure recovery — agent reaches for logs automatically

Currently `first-deploy-failed` blocker is a leaf. No structured Recovery
on it. No suggestion to pull build logs.

**Karel's expectation**: when launch fails post-mutation (build crashed),
ZCP returns a blocker that points at the exact next call to debug:

```json
{
  "id": "first-deploy-failed",
  "severity": "block",
  "message": "appdev build process FAILED — pipeline never started.",
  "recovery": {
    "tool": "zerops_logs",
    "args": {
      "service": "appdev",
      "source": "build",
      "projectId": "<target>"
    }
  },
  "diagnostics": {
    "buildProcessId": "<svjB...>",
    "appVersionStatus": "WAITING_TO_BUILD",
    "pipelineStart": null,
    "hint": "Builder never reached pipeline. Likely platform queue / git auth preflight. Retry via push or wait."
  }
}
```

Same pattern needed on:
- `create-import-failed` (preprocessor errors etc.) — point at composer
  metadata + suggest re-classify
- `source-control-required` — point at `zerops_workflow action=git-push-setup`

**For v9.92 minimum**: add `Recovery` field on `first-deploy-failed` +
`create-import-failed` (the two Karel hit). 30 LOC.

---

## 2. Test suite — four real-platform tests, increasing complexity

New directory: `e2e/live/` with build tag `e2e_live` (separate from
`e2e_live` is reserved; reuse default `e2e` tag with env-var gating per
existing pattern).

Common harness:

- Env gates: `ZCP_API_KEY` (eval-zcp scope), `ZCP_E2E_LAUNCH_KEY`
  (account-wide LaunchKey for new-project mutations), `ZCP_E2E_GITHUB_PAT`
  (fine-grained PAT on test repos)
- Source: `eval-zcp` project (already provisioned)
- Per-test seed: provision required source state inline OR rely on
  pre-seeded eval-zcp
- Per-test cleanup: delete created Zerops projects + restore source state
- Verification mode: real platform API queries (services, processes, logs,
  events) — not mock assertions

### Test 1 — Export (simplest, read-only)

`e2e/live/export_test.go::TestLiveExport_LaravelShowcase`

- Setup: ensure eval-zcp has the laravel-showcase fixture deployed
  (services: appdev php-nginx, appstage php-nginx, db postgresql, cache
  valkey, storage object-storage)
- Run: invoke `ops.BuildBundle` via export handler pipeline against the
  real source state
- Verify:
  - YAML schema-valid
  - No SYSTEM envs in `project.envVariables`
  - USER envs classified per envclass bias
  - Storage entry has `objectStorageSize`, no `mode:`
  - buildFromGit URL matches live `git remote get-url origin`
- No mutation; no cleanup needed beyond fixture restoration if seeded for
  this test only

**Run time**: ~30s (mostly API reads).

### Test 2 — Git-push-setup (atom emit + meta update)

`e2e/live/git_push_setup_test.go::TestLiveGitPushSetup_Container`

- Setup: eval-zcp appdev service exists with no git-push configured
- Run: `zerops_workflow action=git-push-setup service=appdev
  remoteUrl=https://github.com/krls2020/eval2`
- Verify:
  - Response carries `setup-git-push-container` atom body
  - Response includes the fine-grained PAT scopes table
  - ServiceMeta on disk: `GitPushState=configured`,
    `RemoteURL=https://github.com/krls2020/eval2`
  - No platform mutation (just meta + atom emit)
- Cleanup: restore `GitPushState=unconfigured` on eval-zcp appdev

**Run time**: ~10s.

### Test 3 — Build-integration / CI/CD (composer + atom + secret command)

`e2e/live/build_integration_test.go::TestLiveBuildIntegration_Actions`

- Setup: eval-zcp appdev with `GitPushState=configured` (post Test 2)
- Run: `zerops_workflow action=build-integration service=appdev
  integration=actions`
- Verify:
  - Response carries `setup-build-integration-actions` atom body
  - Workflow YAML in response uses raw `zcli` (not zeropsio/actions
    marketplace)
  - Secret-set command has `-R krls2020/eval2 ZEROPS_TOKEN`
  - PAT recommendation prose lists Contents/Secrets/Workflows scopes
- Cleanup: restore `BuildIntegration=none` on eval-zcp appdev

**Run time**: ~10s.

### Test 4 — Launch-production (full real mutation, end-to-end)

`e2e/live/launch_production_test.go::TestLiveLaunchProduction_FullCycle`

- Setup: eval-zcp with laravel-showcase fixture (re-uses Test 1's setup)
- Run, programmatic:
  1. Construct `WorkflowInput{Workflow: launch-production, TargetService:
     appdev, ProductionProjectName: phase2-live-<timestamp>}`
  2. Invoke handler → scope-prompt → classify-prompt → ready-to-launch
  3. Supply real LaunchKey → invoke handler → status check loop
  4. Wait up to 10 min for `status=launched` OR terminal failure
- Verify (on launched):
  - New Zerops project exists in Muad org
  - 4 services created: app, db, cache, storage with correct shape (F19/
    F20/F21 fixes verified against real platform)
  - At least one service has subdomain accessible (HTTP probe to
    `https://<host>.zerops.app` returns 2xx)
- Verify (on failed via build-stuck):
  - `first-deploy-failed` blocker carries the Recovery hint (per §1c
    above)
  - Build logs are pullable via `zerops_logs source=build` (tests the
    recovery pointer works)
- Cleanup: delete new prod project regardless of pass/fail

**Run time**: 8-12 min (real build + deploy or until timeout).

---

## 3. Implementation order

1. **Plan + atoms first**: write atom `launch-source-git-auth-required.md`
   + add Recovery field on the 2 blockers (§1b + §1c above) — ~150 LOC.
2. **Harness** `e2e/live/harness.go` — env-gated client construction, test
   project lifecycle, log/event helpers, HTTP probe.
3. **Test 1 (export)** — implement + verify.
4. **Test 2 (git-push-setup)** — implement + verify.
5. **Test 3 (build-integration)** — implement + verify.
6. **Test 4 (launch-production)** — implement + verify against eval-zcp.
   Iterate on the recovery diagnostic until the platform-build-stuck
   scenario surfaces meaningfully to the agent.
7. **Verify all four green**, then push v9.92 (currently 13 unpushed
   commits + this work).

Multi-session work. Each test commits independently so progress doesn't
get lost on session compaction.

## 4. Skipped explicitly

- **Q1 method prompt** (Phase 6b §6.2 — awaiting-project-mode-choice
  status before scope-prompt). Karel's session manually picked
  new-project path via LaunchKey; the prompt UX is Phase 6b scope.
- **Webhook cicd path** (§4.6). Karel's flow targets Actions push-mode;
  webhook stays as alternative but not tested in this round.
- **Bootstrap + develop scenarios**. Already in behavioral matrix
  (passing); this work specifically targets the prod-transition surface.
- **Pre-build platform clone preflight** (the SVjB0... root cause).
  Platform-side concern; not a ZCP issue.
