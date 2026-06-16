# Production Lifecycle Plan — Part 2: Pipeline Extension (v2)

**Date:** 2026-05-12
**Status:** Path B (dashboard-driven) confirmed after Phase A spike empirically
showed Path A (programmatic close-loop) is blocked by per-clientUser OAuth.
**Continues:** `plans/archive/production-lifecycle-2026-05-11.md` (Part 1, shipped v9.86.1)
**Anchor:** `plans/archive/production-lifecycle-part2-context-2026-05-12.md`
**Backlog:** `plans/backlog/launch-pipeline-close-loop-oauth.md`

---

## 0. Phase A finding that reshaped this plan

The originally-proposed Path A (programmatic close-loop:
`PutServiceStackExternalRepositoryIntegration` during launch window)
was empirically blocked by Phase A spike (2026-05-12):

- `PUT /service-stack/{id}/external-repository-integration` requires the
  calling clientUser to have a per-user GitHub OAuth grant.
- Launch-window machine token's clientUser does NOT inherit human user's
  OAuth grant — they're separate identities.
- Browser-mediated OAuth handshake for the machine clientUser is fragile
  (SPA consumes code atomically; attribution to clientUser is server-side
  dependent on browser session, not state token).

v1 ships **Path B (dashboard-driven)**: ZCP guides the user to Zerops
dashboard for integration setup; ZCP only verifies via `GetStatus`.
Path A revisit lives in `plans/backlog/launch-pipeline-close-loop-oauth.md`
(triggered when Zerops adds a non-browser OAuth-attachment API).

Full Phase A findings: `docs/spec-launch-production-platform-spike.md §B`.

---

## 1. TL;DR (Path B)

Part 1 leaves prod with a working first deploy via `buildFromGit:` but no
ongoing CD trigger. Part 2 closes the lifecycle by extending
`launch-production` with a 7th status `configuring-pipeline`:

- ZCP enters the status post-launch (after `executeLaunchMutation` returns).
- For each runtime service in the prod bundle, ZCP calls
  `GetServiceStackExternalRepositoryIntegrationStatus`.
- If response is `noExternalRepositoryIntegration` → return a blocker with
  a deep-link to the Zerops dashboard's source-code config page for that
  service (`https://app.zerops.io/dashboard/project/<projectID>/
  service-stack/<svcID>/service-stack-source-code`) plus a structured
  recommendation payload (`repositoryFullName`, `eventType=TAG`,
  `tagRegex=^v\d+\.\d+\.\d+$`, `zeropsYamlSetup=prod`).
- User configures each runtime via dashboard (~3 min for one service;
  uses their existing OAuth grant on their own clientUser).
- User re-runs workflow with same launchKey. ZCP re-calls GetStatus; if
  all runtimes return configured → exit to `launched`. If some still
  missing → blocker iterates.

Same trust boundary as Part 1 — same one-shot launchKey, same
`ProjectAdminClient` interface segregation (extended with one method:
`GetServiceStackIntegrationStatus`), same `defer admin.Close()`, same
audit log.

No new mutation API needed in ZCP. Phased over ~1.5 weeks (smaller than
v1 plan estimate because mutation pipeline collapses).

---

## 2. The user pain (continued from Part 1, refined by Phase A)

After v9.86.1, a user who runs `launch-production` ends up with:

- ✓ Prod project created + populated
- ✓ Managed services HA, runtimes NON_HA + minContainers=2 + DEDICATED
- ✓ First deploy ran from source HEAD
- ✗ **No ongoing build trigger** — push to main / tag release does nothing
- ✗ **User has to manually `zcli push` to prod** to ship new code, OR
  open Zerops dashboard each time, click into the service, hit deploy

Path B v1 cost: user does **one** dashboard config click-through per
runtime service at end of launch (~3 min one-time setup). After that:
push tag → Zerops fires build → prod redeploys, automatically.

Future Path A iteration (backlog): zero-friction; ZCP configures
everything programmatically.

---

## 3. Trust boundary — continuity with Part 1

Inherits P-LP-1..6 verbatim. New invariants:

| ID | Invariant | Pin |
|---|---|---|
| **P-LP-7** | Pipeline integration is verified-only by ZCP; ZCP never PUTs integration config (dashboard-driven design) | `TestPipelineConfig_NoPutCallsByZCP` |
| **P-LP-8** | Pipeline-config-not-yet-done surfaces as blocker on `launched` response, never converts to `failed` status (prod project IS created + deployed; pipeline is recoverable) | `TestPipelineConfig_NotConfiguredKeepsLaunchedWithBlocker` |
| **P-LP-9** | `GetServiceStackIntegrationStatus` 400 with `code: noExternalRepositoryIntegration` maps to canonical `IntegrationState.NotConfigured` — error is not propagated as failure | `TestGetStatus_NoExternalRepoMapsToNotConfigured` |

Part 2 reuses Part 1's `ProjectAdminClient` (same constructor, same
`P-LP-2` structural-grep lint). The interface is extended with ONE
read-only method, not the two writes the original plan proposed.

---

## 4. SDK surface (verified by Phase A spike, locked)

Phase A B.1: `GET /api/rest/public/service-stack/{id}/external-repository-integration-status`
- **Fresh service (no integration)** → HTTP 400 `{code: noExternalRepositoryIntegration}`.
  Treat as canonical "not configured" state in ZCP wrapper.
- **Configured service** → HTTP 200 `{githubIntegration: {...}, gitlabIntegration: {...}}`
  (one of the two will be non-null; see B.4 in spike doc for shape).

Phase A B.2: `PUT /api/rest/public/service-stack/{id}/external-repository-integration`
- ZCP does NOT use this endpoint in v1. Reserved for future Path A.

Phase A B.6 (deferred but documented): pushing tag to source repo with
configured integration fires Zerops build automatically. End-to-end
verified by setting integration manually via dashboard then pushing
tag — bundle of Phase E e2e tests does this.

OAuth setup model:
- v1: user-side OAuth (per Karel's clientUser, already done at Zerops
  org level) handles all integration mutations from dashboard.
- ZCP doesn't initiate or manage OAuth grants.

---

## 5. Design (Path B)

### 5.1 The 7th status

```
scope-prompt → classify-prompt → ready-to-launch → launching →
                                                       │
                                            ┌──────────┴────────┐
                                            │                   │
                                  configuring-pipeline      failed
                                            │
                                         launched
                                  (blockers[] may be present if
                                  pipeline still needs user action)
```

`configuring-pipeline` semantics:

- **Reached when:** existing launch path completed
  (CreateAndImportProject + GrantSelfRole + pollImportedServices done).
- **Action taken:** for each runtime service in the bundle that has
  `buildFromGit` set, call `GetServiceStackIntegrationStatus`.
- **Exit to `launched`:** when all runtimes either:
  - Return `configured` from GetStatus, OR
  - Are explicitly skipped (per `skipPipelineSetup=true`).
- **Exit to `launched` with blockers[]:** when one or more runtimes are
  still `not-configured` AND user did not skip. Blockers list deep-links
  + recommended config. User can re-run workflow after completing
  dashboard setup to recheck.
- **Never exits to `failed`** — pipeline config is post-launch recoverable.

### 5.2 ProjectAdminClient extension (minimal)

```go
// internal/platform/project_admin.go — add ONE method
type ProjectAdminClient interface {
    // ...existing methods from Part 1...

    GetServiceStackIntegrationStatus(ctx context.Context, serviceStackID string) (
        IntegrationStatus, error,
    )
}

type IntegrationStatus struct {
    State           IntegrationState  // not-configured | configured
    Provider        string            // "github" | "gitlab" | ""
    RepositoryFullName string         // populated when configured
    EventType       string            // "BRANCH" | "TAG"
    TagRegex        string
    BranchName      string
    ZeropsYamlSetup string
    IsActive        bool
}

type IntegrationState string
const (
    IntegrationNotConfigured IntegrationState = "not-configured"
    IntegrationConfigured    IntegrationState = "configured"
)
```

Concrete behavior:
- Wraps `GetServiceStackExternalRepositoryIntegrationStatus`.
- On HTTP 400 with `code: noExternalRepositoryIntegration` →
  `IntegrationState.NotConfigured` (treats error as state read).
- On HTTP 200 → parse output, populate fields, return
  `IntegrationState.Configured`.
- On other errors → propagate.

### 5.3 Handler flow (configuring-pipeline branch)

```
Entry (post-launched-mutation-pipeline, admin still in defer-Close window)
  │
  ├─ if input.SkipPipelineSetup → state.PipelineConfigurations marked all
  │                               as skipReason="user-opted-out",
  │                               blocker[pipeline-skipped-user-opted-out],
  │                               jump to launched
  │
  ├─ for each runtime in bundle.RuntimeServices with buildFromGit:
  │     │
  │     ├─ admin.GetServiceStackIntegrationStatus(svc.ID)
  │     │     │
  │     │     ├─ state=not-configured →
  │     │     │     blocker[pipeline-not-configured] with:
  │     │     │       - deepLink: <dashboard URL for this service>
  │     │     │       - recommendation: {
  │     │     │           repositoryFullName: <derived from buildFromGit>,
  │     │     │           eventType: TAG,
  │     │     │           tagRegex: ^v\d+\.\d+\.\d+$ (or input override),
  │     │     │           zeropsYamlSetup: prod,
  │     │     │           triggerBuild: false (because TAG)
  │     │     │       }
  │     │     │     state.PipelineConfigurations[svc.Name].deepLink saved
  │     │     │
  │     │     └─ state=configured →
  │     │             state.PipelineConfigurations[svc.Name] = {
  │     │               configured: true,
  │     │               currentConfig: {...readback from GetStatus...}
  │     │             }
  │
  ├─ Audit log entry per runtime
  │
  └─ Compose final launched response:
       - terminal atom: launch-delete-key (priority 1, mandatory)
       - terminal atom: launch-pipeline-status (configured/needs-dashboard)
       - terminal atom: launch-post-checklist
       - per-service blockers from above (user reads, acts in dashboard,
         re-runs workflow to recheck)
```

### 5.4 Defaults

| Field surfaced as recommendation | Default | Override |
|---|---|---|
| `eventType` | `TAG` | Not exposed in v1 |
| `tagRegex` | `^v\d+\.\d+\.\d+$` | `WorkflowInput.PipelineTagRegex` |
| `zeropsYamlSetup` | `prod` | Not exposed in v1 |
| `triggerBuild` | `false` (TAG events trigger build implicitly) | Not exposed in v1 |
| `branchName` | n/a (tag-trigger has no branch) | Not exposed |

### 5.5 New `WorkflowInput` fields

```go
type WorkflowInput struct {
    // ...existing from Part 1...

    SkipPipelineSetup    bool   `json:"skipPipelineSetup,omitempty"`     // v1 escape hatch
    PipelineTagRegex     string `json:"pipelineTagRegex,omitempty"`      // override default
}
```

No `OverwritePipeline` — ZCP doesn't PUT, so no clobber risk.

### 5.6 State file extension

`.zcp/state/launch-production/{launchID}.json` gains:

```json
{
  "...existing Part 1 fields...": "...",
  "pipelineCheckedAt": "2026-05-12T14:30:00Z",
  "pipelineConfigurations": {
    "app": {
      "configured": false,
      "deepLink": "https://app.zerops.io/dashboard/.../service-stack-source-code",
      "recommendation": {
        "repositoryFullName": "krls2020/myapp",
        "eventType": "TAG",
        "tagRegex": "^v\\d+\\.\\d+\\.\\d+$",
        "zeropsYamlSetup": "prod"
      }
    },
    "api": {
      "configured": true,
      "currentConfig": {
        "provider": "github",
        "repositoryFullName": "krls2020/myapp-api",
        "eventType": "TAG",
        "tagRegex": "^v\\d+\\.\\d+\\.\\d+$"
      }
    }
  }
}
```

Same `0o600`, same append-only audit log.

### 5.7 Multi-runtime handling

Each runtime gets independent GetStatus + own blocker. State file records
per-service result. Partial config (api configured, frontend not) is
explicit in state + blocker list.

### 5.8 Idempotent re-check (resume after dashboard setup)

When user has configured a runtime via dashboard and re-calls
`launch-production` action="publish" with same launchKey:
- Handler re-reads pipeline state from existing state file.
- For configurations recorded as `configured: false`, re-calls GetStatus.
- Updates state file with current truth.
- Blockers list shrinks as user resolves.
- Once all `configured: true` or all skipped → launched with no blockers.

---

## 6. Atoms

### 6.1 New atoms (in `internal/content/atoms/`)

- **`launch-pipeline-configuring.md`** — emitted while in
  `configuring-pipeline` status; explains what ZCP is checking
  (per-service integration verification); 5-7 lines.

- **`launch-pipeline-configure-dashboard.md`** — recovery atom for
  not-configured blocker; instructs user to open the deep-link, configure
  via Zerops dashboard with these values: repo, eventType=TAG,
  tagRegex, zeropsYamlSetup=prod. Mentions that GitHub OAuth is already
  done at user-level (no action needed beyond clicking through).

- **`launch-pipeline-skipped.md`** — emitted when
  `skipPipelineSetup=true`. Notes future configuration paths
  (manual dashboard).

- **`launch-pipeline-configured.md`** — emitted when all runtimes are
  configured; brief confirmation, points at how to test
  (`git tag v1.0.0 && git push --tags`).

### 6.2 Extended atoms

- **`launch-post-checklist.md`** — append a "Pipeline trigger" section
  listing tag-format expectation if pipeline configured.

### 6.3 Pinning

Per `spec-knowledge-distribution.md §11`: pin via
`scenarios_test.go::TestScenariosFixture` — new scenario
`ConfiguringPipelineActive` emits the relevant atom set.

---

## 7. Phased implementation (revised, smaller than v1)

### Phase A — SDK behavior spike — **DONE** (2026-05-12)

Findings appended to `docs/spec-launch-production-platform-spike.md §B`.

### Phase B — ProjectAdminClient extension (1-2 days)

- Add `GetServiceStackIntegrationStatus` method.
- Wire to SDK `GetServiceStackExternalRepositoryIntegrationStatus`.
- Map error code `noExternalRepositoryIntegration` (HTTP 400) → state
  `not-configured` (NOT propagated as failure).
- `IntegrationStatus` struct + enum in `internal/platform/integration.go`.
- Mock implementation in `internal/platform/project_admin_mock.go`.
- Unit tests including the error-to-state mapping.
- E2E test gated on `ZCP_E2E_PROD_LAUNCH=1`.

Verification: `go test ./internal/platform/... -race`.

### Phase C — Handler extension (2-3 days)

- Add `LaunchStatusConfiguringPipeline` to topology.
- Add `WorkflowInput` fields (SkipPipelineSetup, PipelineTagRegex).
- Extend handler at `workflow_launch_production.go`:
  - New `executeLaunchPipelineCheck(ctx, admin, bundle, input, state)` func
    called after `executeLaunchMutation` completes.
  - Per-runtime loop per §5.3.
  - Helper: `deriveRepositoryFullName(buildFromGitURL)` extracts owner/repo.
  - Helper: `deriveDashboardDeepLink(projectID, serviceID)`.
  - State file extension per §5.6.
- Pinning tests:
  - `TestHandleLaunchProduction_FullSequence_HappyPath` extended.
  - `TestPipelineConfig_NoPutCallsByZCP` (P-LP-7).
  - `TestPipelineConfig_NotConfiguredKeepsLaunchedWithBlocker` (P-LP-8).
  - `TestGetStatus_NoExternalRepoMapsToNotConfigured` (P-LP-9).
  - `TestPipelineConfig_MultiRuntimePartialConfigured`.
  - `TestPipelineConfig_SkipFlagBypassesCheck`.
  - `TestPipelineConfig_IdempotentResume`.

### Phase D — Atoms + pinning (1-2 days)

- Author 4 new atoms per §6.1.
- Extend `launch-post-checklist.md` per §6.2.
- Pin in `internal/workflow/scenarios_test.go` —
  add `ConfiguringPipelineActive` scenario.
- Verify atom-lint passes.

### Phase E — Eval scenarios + release (1-2 days)

Behavioral evals:
1. `launch-production-pipeline-not-configured.md` — operator skips
   dashboard setup, expects launched-with-blocker, then completes
   dashboard, re-runs, expects launched-clean.
2. `launch-production-pipeline-skip.md` — `skipPipelineSetup=true`,
   verifies launched without GetStatus calls.
3. `launch-production-pipeline-multi-runtime.md` — partial-configured
   case.

One sibling local-mode scenario.

Final lint-local + race tests + `make release-patch` (v9.87.0).

Total: ~1.5 weeks (smaller than v1 plan's 2 weeks because mutation drops).

---

## 8. Test plan (revised, smaller)

| Layer | Files | Estimated tests |
|---|---|---|
| Unit (`internal/topology/`) | extend `launch_production_status_test.go` | +3 |
| Unit (`internal/platform/`) | extend `project_admin_test.go` + new `integration_test.go` | +8 (status mapping, error-to-state, mock behavior) |
| Tool (`internal/tools/`) | extend `workflow_launch_production_test.go` | +8 (P-LP-7/8/9, multi-runtime, skip, idempotent resume) |
| Integration | extend `launch_production_test.go` | +2 |
| E2E (gated) | extend `launch_production_e2e_test.go` | +1 (GetStatus on configured + not-configured live) |
| Behavioral eval | 3 scenarios + 1 local | 4 |
| Scenarios fixture | extend | +1 |

Total: ~21 unit, ~2 integration, ~1 e2e, 4 evals.

---

## 9. Risks (revised after Phase A)

| Risk | Likelihood | Mitigation |
|---|---|---|
| Dashboard config friction (3 min per runtime) | Medium | Clear deep-link + structured recommendation in blocker; user copies values rather than typing. Path A backlog promotes when this matters more. |
| User types wrong tag-regex / wrong setup name | Medium | Blocker payload prefilled; atom guidance includes the exact values. v1 idempotent resume means typo → re-config; no permanent damage. |
| Existing integration on service unexpectedly | Low | ZCP only GETs, never PUTs; existing configs preserved by design. |
| Race: user configures in dashboard, ZCP re-checks too soon → cached stale state | Low | Force fresh GetStatus on every re-call; no caching by ZCP. |
| Multi-runtime: user configures one, forgets others | Medium | Per-service blocker; clear list in response. |
| Launch-window key expires mid-config | Low | Key lifetime exceeds typical dashboard session; even if expires, user generates fresh key and re-runs workflow. |
| Unsupported provider (Gitea, etc.) | Low | GetStatus would return `noExternalRepositoryIntegration` indefinitely; v1 doesn't support → recommend manual zcli push from CI. |

---

## 10. Scope cuts — explicit non-goals

- **Programmatic PUT integration during launch window** (Path A).
  Backlog: `plans/backlog/launch-pipeline-close-loop-oauth.md`.
- **GitHub Actions workflow file writes** — out.
- **Branch-push trigger** — out for v1; tag-only per Zerops docs prod recommendation.
- **Multi-repo prod** — out for v1; single git remote URL from Part 1.
- **`branchName` / `eventType` overrides** — out for v1; tag-trigger default.
- **Pipeline rollback / disable tool** — user disables via dashboard.
- **Migration / empty-DB atom edits** — out per 2026-05-12 user decision.
- **Auto-detection of GitLab repos** — out for v1; default to GitHub provider
  in recommendation, user picks GitLab in dashboard if their repo is there.

---

## 11. Documentation

### Spec extensions

- `docs/spec-launch-production-platform-spike.md` — promoted §B (Phase A
  Part 2 findings, including Path A blocker rationale).
- `docs/spec-workflows.md` — extend §10 with the 7th status + Path B
  invariants P-LP-7/8/9.

---

## 12. Files affected (revised)

New:
- `internal/content/atoms/launch-pipeline-configuring.md`
- `internal/content/atoms/launch-pipeline-configure-dashboard.md`
- `internal/content/atoms/launch-pipeline-skipped.md`
- `internal/content/atoms/launch-pipeline-configured.md`
- `internal/platform/integration.go` (IntegrationStatus type + enum)
- `eval/behavioral/scenarios/launch-production-pipeline-{not-configured,skip,multi-runtime}.md`
- `eval/behavioral/scenarios-local/launch-production-pipeline-local.md`
- `plans/backlog/launch-pipeline-close-loop-oauth.md` (already created)

Modified:
- `internal/platform/project_admin.go` (interface + concrete + mock)
- `internal/platform/project_admin_mock.go`
- `internal/topology/launch_production_status.go` (7th status)
- `internal/tools/workflow_launch_production.go` (handler extension)
- `internal/tools/launch_state.go` (state file shape)
- `internal/tools/workflow_types.go` (WorkflowInput fields)
- `internal/tools/workflow_launch_production_sequence_test.go`
- `internal/content/atoms/launch-post-checklist.md`
- `internal/workflow/scenarios_test.go`
- `docs/spec-launch-production-platform-spike.md` (§B promote)
- `docs/spec-workflows.md` (§10 extend)

Estimated LOC delta: ~250-400 (smaller than v1 plan's 400-600 because
mutation pipeline collapses to read-only verify).

---

## 13. Verification — definition of done

After Phase E:

```
zerops_workflow workflow="launch-production" \
  productionProjectName=myapp-prod \
  region=eu-central \
  envClassifications={...} \
  launchKey=<one-shot key>
```

…receives `launched` response containing:
- ✓ Prod project URL
- ✓ Per-service deployment status
- ✓ Per-service pipeline status (`pipelineConfigurations` map)
- ✓ Mandatory `launch-delete-key` atom
- ✓ For each unconfigured runtime: blocker with dashboard deep-link +
  recommended config values
- ✓ `launch-pipeline-configuring` / `-configured` / `-configure-dashboard`
  atom as appropriate
- ✓ `launch-post-checklist` atom with "Push a tag to deploy" guidance

User completes dashboard setup, re-runs `launch-production` with same
launchKey, gets clean `launched` (no blockers).

Subsequently, `git tag v1.0.0 && git push --tags` fires Zerops build
automatically.

Three eval scenarios pin the not-configured + skip + multi-runtime
cases. One e2e test pins live SDK behavior. P-LP-7/8/9 invariants
pinned by unit tests.

---

## 14. Phase A → Phase B handoff

Phase A complete. Phase B can start now. The `ProjectAdminClient`
interface extension is locked above (one method). Implementation lands
in `internal/platform/project_admin.go` + tests +
`internal/platform/integration.go` (new types).

---

**End of Part 2 plan (Path B v2). Ready for Phase B.**
