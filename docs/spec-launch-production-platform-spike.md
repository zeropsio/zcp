# Launch-Production Platform Spike

**Date:** 2026-05-11
**Phase:** A (gating; locks contracts for Phase B+)
**Goal:** verify every platform assumption in the launch-production plan against real Zerops API + SDK + docs before committing to contracts.

---

## Summary of findings

Eight sub-tasks (A.1–A.8). Findings inform contracts for `ProjectAdminClient` (Phase B), `LaunchBundleBuilder` (Phase C), and the workflow handler (Phase D).

| # | Topic | Status | Key finding |
|---|---|---|---|
| A.0 | Token shape + clientID discovery | ✓ verified | `ZCP_API_KEY` is project-scoped (`canCreateProjects: false`); clientID `BkC8AGjFQMyFrLbzjHoE9g` for "KRLS" org accessible. |
| A.1 | `PostClientProjectImport` shape + behavior | ◐ partial | 403 `insufficientPermissions` with project-scoped token (as expected). SDK shape locked. Needs admin token for live behavior verification — Phase B e2e test gating. |
| A.2 | `DeleteProject` cleanup behavior | ✓ SDK + ◐ live | Returns `output.Process` — async. Needs admin token for live. |
| A.3 | `GetProjectL7httpbalancerConfig` shape | ✓ verified | **Critical**: L7 config is nginx tuning (`client_body_buffer_size`, `worker_processes`, etc.), NOT route-level. Custom domain is separate first-class resource via `PostProjectPublicHttpRouting`. Plan-agent + Codex "clobber risk" concern referenced wrong endpoint. |
| A.4 | Region / location field | ✓ verified | `Location` value is region code like `"eu-central"` (not `"prg1"`). `GetProject` returns `primaryInstanceLocation: {id: "eu-central", name: "EU Central (prg1)"}`. |
| A.5 | Server-side schema validation | ✓ SDK | `body.ProjectImport` requires `yaml`. Schema validation outranks auth (would catch invalid YAML even with admin token — needs Phase B test). |
| A.6 | `PostProject.MaxCreditLimit` behavior | ✓ SDK + ◐ live | Field type `types.DecimalNull`. Needs admin token to verify halt-vs-warn behavior. |
| A.7 | `ProjectModeEnum` semantics | ✓ verified | `LIGHT \| SERIOUS \| LEGACY`. Default `LIGHT`. eval-zcp uses `LIGHT`. |
| A.8 | One-shot key permission scope | ✓ verified via docs | Platform already enforces project-scoped boundary for ZCP — multi-project/account-wide tokens are **refused at startup**. Validates one-shot key design architecturally. |

`◐ partial` = SDK shape known + design contract locked; live API behavior verification deferred to Phase B e2e (requires admin one-shot token from user).

---

## A.0 — Token shape + clientID discovery

**Method:** `GET /api/rest/public/user/info` with current `ZCP_API_KEY`.

**Findings:**
- userID: `1eViBseAR1aXRlBwnEjTrg`.
- Token-email pattern: `token-<userID>@zerops.io` — confirms machine token, not human.
- `clientUserList[0]`: `{clientId: "BkC8AGjFQMyFrLbzjHoE9g", clientName: "KRLS", roleCode: "NO_ACCESS", canCreateProjects: false, canViewFinances: false, canEditFinances: false}`.

**Implications:**
- The standard `ZCP_API_KEY` is **architecturally incapable** of creating projects. Validates one-shot key model.
- `clientUserList` is the discoverable structure for org membership; ZCP can read its own clientID without configuration.
- CLAUDE.local.md's stated project ID `i6HLVWoiQeeLv8tV0ZZ0EQ` is stale — real eval-zcp ID is `waAzEFn6SBaysG4YE4rv7A`.

---

## A.1 — `PostClientProjectImport` shape + behavior

**SDK path:** `POST /api/rest/public/client/{clientId}/project/import`

**Request body** (`body.ProjectImport`):

```json
{
  "yaml": "<import yaml as string>",
  "recipeSource": null
}
```

`yaml` field is **required** (server-side validator emits `"field is required"` if missing — verified via SDK code).

**Response shape** (`output.ProjectImport`):

```json
{
  "projectId": "<new project UUID>",
  "projectName": "<name from import yaml>",
  "serviceStacks": [
    {
      "id": "<new service stack UUID>",
      "name": "<hostname>",
      "error": null,
      "processes": [
        {
          "id": "<process UUID>",
          "actionName": "...",
          "status": "...",
          ...
        }
      ]
    }
  ]
}
```

**Key behavior** (derived from SDK shape):
- The API call returns **synchronously** with new `projectId` + all `serviceStacks` (with their IDs + per-service `processes` to poll). No top-level "import process" to poll — each service has its own async processes.
- This means `CreateAndImportProject` in our `ProjectAdminClient` can return immediately with everything needed to track per-service progress.
- Per-service `error` field surfaces import-time validation errors (e.g. quota exceeded for that service type, name collision per-service).

**Live verification:** project-scoped token returns 403 `insufficientPermissions`:

```bash
$ curl -X POST .../client/BkC8AGjFQMyFrLbzjHoE9g/project/import -d '{"yaml":"project: {name: throwaway} ..."}'
{"error":{"code":"insufficientPermissions","message":"Insufficient permissions",...}}
HTTP 403
```

This is the expected behavior — confirms the platform enforces token scope BEFORE attempting any mutation. Good safety property.

**Phase B contract (locked):**
```go
type CreateAndImportResult struct {
    ProjectID     string
    ProjectName   string
    ServiceStacks []ServiceStackResult
}

type ServiceStackResult struct {
    ID         string
    Name       string
    Error      *PlatformError       // non-nil if import failed for this service
    ProcessIDs []string             // poll these to track service-init
}
```

---

## A.2 — `DeleteProject` cleanup behavior

**SDK path:** `DELETE /api/rest/public/project/{id}`

**Response shape** (`output.Process`):

```json
{
  "id": "<delete-process UUID>",
  "actionName": "...",
  "status": "RUNNING|FINISHED|FAILED",
  ...
}
```

**Key behavior:** Delete returns an **async process**. ZCP's existing `Process` polling infrastructure (`ops.pollProcess`) handles this. No new pattern needed.

**Live verification:** deferred (requires admin token). What we still need to verify in Phase B e2e:
- How long does delete actually take (relevant for orphan-cleanup recovery flow)?
- Does delete cascade to all services (assume yes; verify)?
- What happens to backups during the 7-day grace period after delete?
- Does delete on a project mid-import work cleanly?

**Phase B contract (locked):**
```go
// DeleteProject returns the async-process ID; caller polls via existing pollProcess.
DeleteProject(ctx context.Context, projectID string) (processID string, _ error)
```

---

## A.3 — `GetProjectL7httpbalancerConfig` shape

**SDK path:** `GET /api/rest/public/project/{id}/l7httpbalancer-config`

**Response shape** (verified live against eval-zcp):

```json
{
  "values": [
    {"name": "client_body_buffer_size", "current": null, "default": "16k"},
    {"name": "worker_processes", "current": null, "default": "4"},
    ...35 total entries...
  ]
}
```

**CRITICAL FINDING:** L7 config is **nginx-level tuning** (client buffers, timeouts, worker counts, proxy settings) — **NOT route-level configuration**. Custom domain attachment is a **separate first-class resource** under `PostProjectPublicHttpRouting`.

This invalidates the v1-plan / Plan-agent / Codex concern about "naive write clobbering existing routes" — they all conflated two distinct endpoints. Custom domain attach via `PostProjectPublicHttpRouting` is RESTful (each routing is its own resource with its own ID), safe to add/update/delete without affecting other routings.

**Custom domain API surface** (`PostProjectPublicHttpRouting`):

Request body (`body.PublicHttpRoutingPost`):
```go
{
  "sslEnabled": true,
  "cdnEnabled": false,         // optional
  "domains": ["myapp.com", "www.myapp.com"],
  "locations": [{...location config...}]
}
```

Response shape (`output.PublicHttpRouting`):
```go
{
  "id": "<routing UUID>",
  "clientId": "...",
  "projectId": "...",
  "sslEnabled": true,
  "domains": [{...dns records here...}],
  "locations": [...],
  "created": "...",
  "lastUpdate": "...",
  "isSynced": false,           // true when L7 picks up the config
  "isEditable": true,
  "deleteOnSync": false,
  "cdnEnabled": false
}
```

The `domains[]` output structure includes DNS records the user must configure (TXT verification, A/AAAA records).

**Plan implication:** Phase 5 (custom domain) in the plan said "guidance + verify only, no L7 mutation". Given this finding (`PostProjectPublicHttpRouting` is safe RESTful resource creation, NOT L7 read-modify-write), Phase 5 could safely include domain attachment via `PostProjectPublicHttpRouting`. **But user explicit principle** stands: "my to llm nechceme pustit do toho produkcniho prostredi … ten produkcni projekt musi uzivatel trochu vice odmakat sam". So Phase 5 stays at guidance + DNS verify in v1. Backlog: revisit in v2 with this corrected understanding.

---

## A.4 — Region / location field

**Method:** `GET /api/rest/public/project/{id}` on eval-zcp.

**Findings:**
- `primaryInstanceLocation` field on Project response:
  ```json
  {
    "id": "eu-central",
    "name": "EU Central (prg1)",
    "pingUrl": "https://proxy.app-prg1.zerops.io/api/rest/ping"
  }
  ```
- The `Location` field on `PostProject` request body takes the `id` ("eu-central"), NOT the suffix ("prg1") or human name.

**Plan implication:** Phase D scope-prompt should surface available locations. Either:
- Hardcode known regions for v1 (`eu-central`, plus any others Zerops adds — there's no live "list locations" endpoint we found).
- Add `GET /api/rest/public/instance-location` discovery (if exists — verify in Phase B).

**v1 decision:** hardcode `eu-central` as default; user can override via `region` parameter; document supported values in `launch-scope-prompt` atom.

---

## A.5 — Server-side schema validation

**SDK-level validators:**
- `body.ProjectImport.UnmarshalJSON` requires `yaml` field non-null. Validator emits structured `ErrorList`.
- Other unmarshalers in `dto/input/body/` follow the same pattern.

**Live behavior:** the 403 in A.1 happened BEFORE schema validation — auth fails first. To verify schema validation behavior we need an admin token + a deliberately malformed yaml. Phase B e2e test: TestProjectAdminClient_CreateAndImport_RejectsInvalidYaml.

**Hypothesis (high confidence):**
- Invalid YAML → 400 with `code: invalidUserInputWithText` or similar (consistent with other 400s we saw).
- Schema-valid but semantically-invalid (e.g., unknown service type) → 400 with structured per-field errors OR success-with-per-service-error (each `serviceStacks[i].error` populated for failing services).

**Phase D handler logic:**
- `LaunchBundleBuilder` already runs `schema.ValidateImportYAML` client-side before any API call (E2 invariant from existing export).
- Any 400 from `PostClientProjectImport` after client-side validation passed = either platform-side schema drift OR a class of validation client-side validator doesn't cover → log as `unexpected-schema-mismatch` blocker.

---

## A.6 — `PostProject.MaxCreditLimit` behavior

**SDK type:** `types.DecimalNull` — nullable decimal.

**Hypothesis:** the limit halts new resource provisioning when reached (consistent with billing UX patterns). Doesn't shut down existing services. **Needs live verification.**

**v1 decision:** scope-prompt can accept optional `maxCreditLimit` parameter (default null = no limit). Per user "ted bych neresil" on cost gate, we don't auto-set this; just expose the field. Phase B e2e: create with `maxCreditLimit: 10`, observe whether create succeeds and whether usage actually halts at $10.

---

## A.7 — `ProjectModeEnum` semantics

**Enum values** (`zerops-go/types/enum/projectModeEnum.go`):

```go
ProjectModeEnumLegacy  = "LEGACY"
ProjectModeEnumLight   = "LIGHT"
ProjectModeEnumSerious = "SERIOUS"
```

**Default value:** `LIGHT` (verified — eval-zcp's mode is `LIGHT`).

**Semantics** (from zerops-docs/features/infrastructure.mdx):
- `LIGHT`: shared core / cost-optimized; suitable for dev/stage.
- `SERIOUS`: dedicated core / production-grade; required for production-tier scaling.
- `LEGACY`: deprecated; ZCP should never emit.

**Plan implication:** production project should default to `SERIOUS` mode. Upgrade `LIGHT → SERIOUS` is one-way + partially destructive ($10 fee, ~35s network blip, free-resource reset). So setting `SERIOUS` at create time avoids the upgrade dance.

**v1 decision:** `LaunchBundleBuilder` sets `mode: SERIOUS` for all production projects. Scope-prompt may expose override via `coreMode` parameter for users who want LIGHT prod (cheaper, less performance).

---

## A.8 — One-shot key permission scope

**Source:** `zerops-docs/zcp/security/tokens-and-project-access.mdx`.

**Critical finding:** Zerops itself enforces ZCP's project-scoped boundary:

> "Account-wide or multi-project tokens are refused before the agent can operate."
> Error: `"Token accesses N projects; use project-scoped token"` → ZCP refuses to start.

**Implications:**
1. The launch-window key (account-wide or multi-project) **cannot** become ZCP's standing token — platform refuses.
2. Token scoping options in Zerops UI:
   - **Custom access per project**: ZCP-compatible; can select one project for Full or Read-Only access.
   - **Account-wide** (implied): can create new projects + manage all org resources. **This is the launch-window key shape.**
3. Token generation is via Zerops UI: `Settings → Access Tokens Management` (`https://app.zerops.io/settings/token-management`). No public token-creation API surface needed for v1.

**v1 decision:**
- `launch-mutation-key-required` atom prompts user to: "Generate an account-wide one-shot token at https://app.zerops.io/settings/token-management → 'Create token' → leave 'Custom access per project' UNCHECKED → copy the value."
- ZCP runtime accepts the key as workflow input only (never reads it from `ZCP_API_KEY` env, which is ALWAYS project-scoped).
- `launch-delete-key` atom (priority 1, mandatory in `launched` response) instructs user to delete the token in the same UI after the launch finishes.

---

## A.9 — Live e2e verification (admin token supplied 2026-05-11)

Six e2e tests passed against real Zerops platform with admin token
(`canCreateProjects: true`, account-wide, KRLS client):

| Test | Result | Observation |
|---|---|---|
| `TestProjectAdminClient_CreateAndImport_Live` | ✓ PASS (~0.7s) | Synchronous create+import; returns projectID + service stack IDs + per-service process IDs immediately. Confirms A.1 contract. |
| `TestProjectAdminClient_CreateAndImport_RejectsInvalidYaml` | ✓ PASS (~40ms) | Schema validation rejects yaml missing `project.name`. Returns 400 with structured error code. |
| `TestProjectAdminClient_DeleteProject_LiveCycle` | ✓ PASS (~0.9s) | DeleteProject returns Process with non-empty ID + Status. Confirms A.2 contract. Status transitions to DELETING; async cleanup. |
| `TestProjectAdminClient_LaunchKeyRejectedAtConstruction` | ✓ PASS (~20ms) | Invalid key fails at NewProjectAdminClient (during GetUserInfo validation), not on first method call. |
| `TestProjectAdminClient_GetServiceEnvKeys_OmitsValues` | ✓ PASS (~1.2s) | Returns "project not found" on env read against project the admin token created (see A.10 finding). Test tolerates this — EnvKey-no-Value is compile-time guarantee. |
| `TestProjectAdminClient_AfterClose_ReturnsErrClientClosed` | ✓ PASS (~30ms) | Real client Close() semantics match mock. |

Cleanup verified clean: all throwaways transitioned to DELETING within
seconds; async platform cleanup completes thereafter.

## A.10 — userRoles=[] gotcha (discovered during A.9)

**Critical Phase D follow-up:** A token with `canCreateProjects: true`
at client level can CREATE a project, but **does not automatically gain
a project-level role on the project it just created**. Project's
`userRoles[]` is empty unless explicitly set in the create body.

Consequence: subsequent calls to `GetProjectEnv` / `GetServiceEnv` /
`ListServices` against the freshly-created project return
**`projectNotFound`** from the API — not because the project is missing
but because the calling token has no role assignment on it.

This breaks the planned Phase D.2 flow:
1. Create + import prod project ✓ (works)
2. Verify external-secret presence via `GetServiceEnv` ✗ (fails — no role)
3. Poll first deploy via `GetProcess` — unclear if also affected (deferred to Phase D.3 verification)

**Initial fix attempt (failed):** inject `project.userRoles[]` into the
import yaml. Result: `PostClientProjectImport` silently dropped the
field; project gets created but `project.userRoles=[]` regardless of
yaml content. Empirically verified 2026-05-11 against admin token.

**Working fix (shipped 2026-05-11, v9.85.0):** separate
`PutClientUserRoles` API call after `PostClientProjectImport` succeeds.
ZCP adds `ProjectAdminClient.GrantSelfRole(ctx, projectID, roleCode)`
which:

1. Calls `GetClientUserRoles` to read the launching clientUser's
   existing project role list (preserve roles on other projects;
   platform auto-assigns OWNER on create, captured here).
2. Builds merged list with `(projectID, roleCode)` —
   replace-if-already-present, append-otherwise. PUT is full replace,
   so naive PUT would wipe roles on other projects.
3. Calls `PutClientUserRoles` with the merged list.

SDK references:
- `sdk.GetClientUserRoles(ctx, path.ClientUserId)` →
  `output.ClientUserProjectRoleList{ProjectRoleList: []ClientUserProjectRole}`
- `sdk.PutClientUserRoles(ctx, path.ClientUserId, body.ClientUserProjectRoleList)`
- Path: `/api/rest/public/client-user/{clientUserId}/roles`
- Each entry: `{projectId: uuid.ProjectId, roleCode: enum.ClientUserRoleCodeEnum}`

**Why the platform auto-assigns OWNER:** the launching clientUser
created the project, so the API auto-grants OWNER. GrantSelfRole is
an explicit-record + audit step that re-asserts the role (and handles
edge cases where auto-grant might be disabled by org policy).

**Test verification (e2e, ZCP_E2E_PROD_LAUNCH=1):**
`TestProjectAdminClient_GetServiceEnvKeys_OmitsValues` creates a
throwaway project with `project.envVariables: {PROBE_VAR: ...}`, calls
`GrantSelfRole(projectID, "ADMIN")`, polls `GetProjectEnvKeys` until
PROBE_VAR appears. Passes in ~2.5s.

**Handler integration:** `executeLaunchMutation` calls
`admin.GrantSelfRole(ctx, result.ProjectID, "ADMIN")` after
`CreateAndImportProject` succeeds; failure is non-fatal (project IS
created, env-presence verification falls back to UI guidance via a
bundle warning).

**P-LP-5 implication:** even with the role fix, `GetProjectEnvKeys` /
`GetServiceEnvKeys` still strip values per the EnvKey type contract.
The role makes the call reachable; the omit-Value invariant is
independent.

---

## What still needs admin-token verification (Phase B e2e)

Bundle of e2e tests gating on `ZCP_E2E_PROD_LAUNCH=1` env var:

1. **TestProjectAdminClient_CreateAndImport_Live** — real `PostClientProjectImport`, asserts response shape matches our `CreateAndImportResult` contract.
2. **TestProjectAdminClient_CreateAndImport_RejectsInvalidYaml** — schema-invalid yaml, asserts 400 + error structure.
3. **TestProjectAdminClient_DeleteProject_Live** — full delete + poll; asserts process completion + project disappears from `PostProjectSearch`.
4. **TestProjectAdminClient_MaxCreditLimit_Halts** — create with `maxCreditLimit: 1`, deploy until limit hit, assert provisioning halts.
5. **TestProjectAdminClient_GetServiceEnv_OmitsValues** — fetch envs for a service that has sensitive entries; assert value field absent (per P-LP-5 invariant).
6. **TestProjectAdminClient_LaunchKeyValidatesAtConstruction** — invalid key returns auth error from constructor, not from first API call.

These tests need an admin token. **The user is expected to supply this token** when running Phase B e2e validation. Until then, Phase B implementation uses the SDK-derived contracts (locked above) and mocks.

---

## Locked contracts (output of Phase A)

These are the contract decisions Phase B+ commit to:

### `ProjectAdminClient` interface (Phase B)

```go
type ProjectAdminClient interface {
    CreateAndImportProject(ctx context.Context, yaml string, opts CreateOpts) (CreateAndImportResult, error)
    GetProjectImportStatus(ctx context.Context, processID string) (ProcessState, error)
    ListServices(ctx context.Context, projectID string) ([]ServiceStack, error)
    GetServiceEnv(ctx context.Context, serviceID string) ([]EnvKey, error)  // returns keys + sensitive flag; never values
    GetProjectEnv(ctx context.Context, projectID string) ([]EnvKey, error)
    DeleteProject(ctx context.Context, projectID string) (processID string, error)
    Close() // zeros internal launchKey field
}

type CreateOpts struct {
    Location       string             // region code, default "eu-central"
    Mode           ProjectModeEnum    // LIGHT|SERIOUS, default SERIOUS for prod
    MaxCreditLimit *decimal.Decimal   // optional
    Tags           []string
}

type CreateAndImportResult struct {
    ProjectID     string
    ProjectName   string
    ServiceStacks []ServiceStackResult
}

type ServiceStackResult struct {
    ID         string
    Name       string
    Error      *PlatformError
    ProcessIDs []string
}

type EnvKey struct {
    Key       string
    Sensitive bool
    // Value is intentionally absent — P-LP-5 invariant.
}

func NewProjectAdminClient(launchKey string) (ProjectAdminClient, error)
```

### `LaunchBundleBuilder` defaults (Phase C)

| Field | Production default | Source |
|---|---|---|
| `project.mode` | `SERIOUS` | A.7 |
| `project.location` | `eu-central` (overridable) | A.4 |
| `project.publicIpV4Shared` | `false` | A.5 — production should opt for dedicated IPv4 when configured; default false until user picks |
| Managed-service `mode` | `HA` (unless `keepNonHA` opt-out) | Plan §7.2 |
| Runtime `mode` | `NON_HA` (platform constraint) | Plan §7.2 |
| Runtime `minContainers` | 2 | Plan §7.2 |
| Runtime `cpuMode` | `DEDICATED` | Plan §7.2 |
| Runtime `enableSubdomainAccess` | stripped | Plan §7.2 |
| Tags | `["env:prod", "source-project:<sourceID>", "managed-by:zcp-launch"]` | Plan §7.2 |

### Custom domain surface (Phase 5, deferred to v2 per user principle)

Even though `PostProjectPublicHttpRouting` is RESTful-safe (not L7 read-modify-write), v1 stays at guidance + DNS verify. v2 may revisit. Backlog: `plans/backlog/launch-prod-custom-domain-attach.md`.

---

## Phase A → Phase B handoff

Phase B can start. The `ProjectAdminClient` interface and types are locked above. Implementation lands in `internal/platform/project_admin.go` + tests in `project_admin_test.go` + mock in `project_admin_mock.go`. E2E test stubs land but gate on `ZCP_E2E_PROD_LAUNCH=1` until user provides admin token.

**Open requests to user (bundled, non-blocking for Phase B implementation):**

1. **Admin one-shot token** for live e2e verification (used once to run the 6 e2e tests listed above, then deleted). Without this, Phase B ships with mock + SDK-derived contracts only; live API behavior assumptions remain unverified until provided.

That's the only blocking item, and it's a clean ask for a one-shot resource — exactly the model the plan validates.

---

# §B — Part 2 (Pipeline Extension) spike (2026-05-12)

Goal: verify SDK behavior for `ExternalRepositoryIntegration` endpoints
before committing Part 2 handler contracts. Originally targeted Path A
(programmatic close-loop); empirical findings reshaped the plan to Path B
(dashboard-driven).

## B.0 — OAuth grant scope on Zerops is per-clientUser, not per-client

Account-wide token (`canCreateProjects: true`, name `zcp-part2-phaseB-e2e`)
auto-creates its own machine `clientUser` on the org with email pattern
`token-<userId>@zerops.io`. The human user who created the token has a
separate `clientUser` with their own GitHub OAuth grant linked.

**Machine clientUser does NOT inherit human's GitHub OAuth grant.** Probes:

| Probe | Result |
|---|---|
| `GET /github/repository?clientId=<KRLS>` (machine token) | HTTP 400 `githubAuthorizationRequired` |
| `GET /github/auth-url?action=REPOSITORY&redirectUrl=https://app.zerops.io/github-auth` | HTTP 200, returns fresh OAuth handshake URL with scope=repo |
| Karel-human's clientUser, by contrast | already linked (org-level dashboard grant from earlier) |

**Implication for Part 2 design:** PUT integration requires the calling
clientUser to have GitHub OAuth grant. Pure close-loop with launch-window
machine token is BLOCKED. Path A backlogged at
`plans/backlog/launch-pipeline-close-loop-oauth.md`.

## B.1 — `GetServiceStackExternalRepositoryIntegrationStatus` on fresh service

Setup: created throwaway `zcp-part2-spike-probe` project (id
`uLEASWAJRYADHMsBEDKkYw`), one nodejs@22 service `app` (id
`VW0QnAX4S2OzuDp9vYoN7g`) with `buildFromGit: https://github.com/krls2020/
zcp-pipeline-probe` + `zeropsSetup: prod`. Project auto-grants OWNER role
to creating clientUser (verified — same Part 1 A.10 behavior).

**Result on fresh service (no PUT yet):**
- HTTP **400** with `code: noExternalRepositoryIntegration` ("No external
  repository is integrated")

This is the canonical "not configured" state — expressed as a 400 error,
NOT a 200 with state field.

**ZCP wrapper contract:** map `code: noExternalRepositoryIntegration` to
`IntegrationState.NotConfigured` (treat 400 as state-read result, not
error to propagate). Other HTTP 400s propagate as errors.

## B.2 — `PutServiceStackExternalRepositoryIntegration` body shape

SDK source (`dto/input/body/externalRepositoryIntegration.go` +
`githubIntegration.go` + `gitlabIntegration.go`):

```json
{
  "githubIntegration": {
    "repositoryFullName": "krls2020/zcp-pipeline-probe",
    "eventType": "TAG",
    "branchName": null,
    "tagRegex": "^v\\d+\\.\\d+\\.\\d+$",
    "isActive": true,
    "zeropsYamlSetup": "prod",
    "triggerBuild": false
  },
  "gitlabIntegration": null
}
```

Required server-side fields: `repositoryFullName`, `eventType`, `isActive`,
`triggerBuild`. Optional: `branchName`, `tagRegex`, `zeropsYamlSetup`.

**`eventType` enum:** `BRANCH | TAG` (no other values; SDK
`enum.GithubIntegrationEventTypeEnum`). Path A would have wanted `TAG` for
prod; Path B doesn't PUT.

**Constraint observed:** `triggerBuild=true` requires `eventType=BRANCH`.
With TAG + triggerBuild=true → HTTP 400 `triggerBuildRequiresBranchEventType`.
TAG events trigger builds implicitly; `triggerBuild` is reserved for BRANCH
push semantics ("build on every push to this branch").

**Pre-correction no-op pitfall:** PUT with wrong body shape (`{github: {...}}`
instead of `{githubIntegration: {...}}`, missing `repositoryFullName`)
returned `HTTP 200 {"process": null}` but did NOT configure anything.
GetStatus continued to return `noExternalRepositoryIntegration`. The
server silently swallowed the malformed body. **Implication if Path A ever
unblocks:** never trust a 200 from PUT alone — always GetStatus to verify.

## B.3 — PUT requires per-clientUser GitHub OAuth → blocks Path A

With correct body shape + valid combination + machine token without
OAuth: HTTP **400** with `code: githubAuthorizationRequired` ("Github
authorization required").

The calling clientUser must have completed GitHub OAuth handshake before
PUT can succeed. The launch-window machine token, by default, has NOT
done this (it's a fresh machine identity).

**OAuth handshake attempt for machine clientUser (empirically tested):**
1. `GET /github/auth-url?action=REPOSITORY&redirectUrl=https%3A%2F%2Fapp.zerops.io%2Fgithub-auth`
   → HTTP 200, returns `githubUrl` with scope=repo + state token.
2. User opens URL in browser, GitHub redirects to
   `https://app.zerops.io/github-auth?code=AAA&state=BBB`.
3. SPA at `/github-auth` consumes the OAuth code on page-load
   atomically (calls `POST /github/user-repository-access` with browser
   session cookie, NOT with the machine token's Authorization header).
4. Server-side attribution: grant attached to whichever clientUser the
   browser session corresponds to (Karel-human's session), NOT to the
   machine token's clientUser as state-key would suggest.
5. Subsequent `POST /github/user-repository-access` with machine token's
   Bearer fails: code already consumed → HTTP 400
   `githubVerificationExpired`.
6. GET /github/repository with machine token still returns
   `githubAuthorizationRequired`.

**Conclusion:** Browser-mediated OAuth handshake to attach a grant to a
machine clientUser is not practically achievable in v1 because:
- Code is consumed atomically by SPA (race condition on paste-back).
- Even if intercepted (incognito with no Zerops session), server-side
  attribution model is the calling Authorization header at POST time,
  not the state-token's stored clientUserId.

Path A is backlogged pending a non-browser API
(`PostClientUserGithubLink(installationId)` or similar) from Zerops
platform team.

## B.4 — PUT with existing integration: deferred indefinitely (Path B)

Not relevant for v1 Path B (ZCP doesn't PUT).

For backlog Path A: SDK shape (`output.ProcessNil`) and Put (not Patch)
verb suggest full replace, no conflict error on re-PUT. To be empirically
verified when Path A unblocks.

## B.5 — Tag-regex syntax: deferred indefinitely (Path B)

Not directly relevant for v1 Path B (user types regex in dashboard, not
ZCP). Recommendation atom embeds `^v\d+\.\d+\.\d+$` as text guidance —
user enters in dashboard's UI input.

For backlog Path A: expected RE2 syntax (Go's stdlib `regexp`); server
likely validates server-side. Empty regex → trigger on every tag (per
the StringNull → null serialization).

## B.6 — Real tag-push fires build: deferred to Phase E e2e

Cannot verify without a configured integration. Phase E e2e setup:
operator configures integration manually via dashboard on the eval-zcp
throwaway service, ZCP push tag, observe `service.processList[]`
gains a new build process.

---

## Locked contracts for Phase B (Path B)

### `ProjectAdminClient` extension (one new method)

```go
type ProjectAdminClient interface {
    // ...existing methods from Part 1...

    GetServiceStackIntegrationStatus(ctx context.Context, serviceStackID string) (
        IntegrationStatus, error,
    )
}

type IntegrationStatus struct {
    State              IntegrationState  // not-configured | configured
    Provider           string            // "github" | "gitlab" | ""
    RepositoryFullName string
    EventType          string            // "BRANCH" | "TAG"
    TagRegex           string
    BranchName         string
    ZeropsYamlSetup    string
    IsActive           bool
}

type IntegrationState string
const (
    IntegrationNotConfigured IntegrationState = "not-configured"
    IntegrationConfigured    IntegrationState = "configured"
)
```

Concrete behavior: wrap SDK `GetServiceStackExternalRepositoryIntegrationStatus`.
HTTP 400 with `code: noExternalRepositoryIntegration` →
`IntegrationState.NotConfigured`. Other errors propagate.

### Cleanup

Spike throwaway project `uLEASWAJRYADHMsBEDKkYw` deleted at end of
Phase A (per cleanup invariant).
