# Auto-wire webhook integration via Zerops API

**Surfaced**: 2026-05-19 — during live Phase 3 webhook verification on
eval-zcp standard pair. User asked "doesn't the API/SDK support setting
this without the dashboard click-through?" Live-tested all 4 plausible
auth shapes against the relevant endpoints.

## What Zerops API supports

SDK has full surface area (`github.com/zeropsio/zerops-go@v1.0.18`):

- `GET /api/rest/public/github/auth-url` — initiate OAuth flow (any auth)
- `POST /api/rest/public/github/user-repository-access` — complete OAuth
  callback, store user→GitHub bond
- `GET /api/rest/public/github/repository` — list bonded user's repos
- `GET /api/rest/public/github/repository-branch?repositoryFullName=…`
  — list branches for a repo
- `PUT /api/rest/public/service-stack/{id}/external-repository-integration`
  — write CI integration (`GithubIntegration` / `GitlabIntegration` DTO
  with `repositoryFullName`, `eventType`, `branchName`, `tagRegex`,
  `zeropsYamlSetup`, `isActive`, `triggerBuild`)
- `GET /api/rest/public/service-stack/{id}/external-repository-integration-status`
  — read integration state
- `POST /api/rest/public/service-stack/{id}/github-webhook` — register
  webhook URL on the GitHub repo
- `PUT /api/rest/public/service-stack/{id}/trigger-external-repository-integration`
  — manual deploy trigger (= "Trigger once after activation" UI tick)

## Why the auth shape blocks ZCP

Live-tested 2026-05-19 against three caller types in the same Zerops
client `KRLS`:

| Caller | Auth shape | `user/info` | `github/repository` | `PUT external-repo-integ` |
|---|---|---|---|---|
| ZCP MCP (`ZCP_API_KEY`, project-scoped PAT, token-bound user `zerops-zcp-zcp`) | `Bearer zps_…` | ✅ 200 | ❌ `githubAuthorizationRequired` | ❌ |
| `zcli` on the zcp host container (token-bound user `xy`, role `NO_ACCESS` on KRLS) | `Bearer zps_…` | ✅ 200 | ❌ `githubAuthorizationRequired` | ❌ |
| Dashboard user-session JWT (Karel's account-level OAuth-bonded user) | `Bearer eyJ…` | ✅ 200 | ✅ 200 | ✅ 200 |

The empirical conclusion: **OAuth bond is bound to user ID, not client
ID**. Karel's dashboard OAuth bond lives on Karel's account user; ZCP's
`zerops-zcp-zcp` token-bound user (auto-issued when a project-scoped PAT
is created) is a separate user in the same `KRLS` client, with its own
empty OAuth state and `NO_ACCESS` role. Karel's bond is not "shared"
through the client.

The dashboard's interceptor (`frontend-legacy/libs/zef/src/auth/auth-token.interceptor.ts:176`)
hits `Authorization: Bearer eyJ…` with a user-session JWT from
`localStorage[@zerops/zef/auth]`. No PAT format is accepted on the
GitHub endpoints.

## Why deferred

Three plausible paths, all blocked or worse than the status quo:

**Path A — status quo** (shipped after Phase 3 in
`plans/git-push-deploy-flow-redesign-2026-05-19.md`): webhook setup is
one dashboard click ("Activate pipeline trigger" panel on the build
target's `/service-stack/{id}/deploy` page). The fix landed the correct
URL slug, the structured prompt contract, the MANDATORY-setup-field
warning, and account-level OAuth wording. For GitHub repos with a
permissive PAT, the actions integration sidesteps the dashboard entirely.

**Path B — one-shot dashboard JWT pass-through**: user extracts the
session JWT from DevTools (`localStorage["@zerops/zef/auth"]`) and
passes it to ZCP as `autoConfigJwt=<token>`. ZCP would call
`GET /github/repository` → present a structured choice to the agent →
`PUT external-repo-integration` → forget. Technically works (the JWT
is precisely what the dashboard uses), but: (1) extracting the token
from DevTools is uglier than clicking Activate; (2) JWT lifetime is
short (refreshed at `/api/auth/refresh` mid-session), so a pasted token
may have already expired by the time the agent uses it; (3) plain-text
paste of a token carrying full user identity is a worse trust surface
than the existing flow.

**Path C — backend feature request**: Zerops backend adds an account-
or client-scoped PAT-callable variant of the GitHub endpoints. This
would make Path B unnecessary by giving ZCP a service-account-grade
credential that respects "user X in client Y delegated repo-bond
operations to PATs scoped to client Y". Out of ZCP control; needs to
be raised with Zerops backend team.

## Trigger to promote

Either:

- Real-world feedback that the one dashboard click on the Activate
  panel becomes friction in observed agent runs (e.g. eval scenarios
  consistently fail before reaching the panel), OR
- Zerops backend ships PAT-callable variants of the GitHub endpoints
  (path C), at which point this becomes a small handler update with
  no auth-shape juggling.

## Sketch (for the Path B variant, if/when prioritized)

```
zerops_workflow action="build-integration" service="<dev-host>" \
  integration="webhook" \
  autoConfigJwt="<dashboard-session-jwt>" \
  repositoryFullName="krls2020/eval2" \
  branchName="main" \
  triggerBuild=true
```

Handler resolves `(buildTarget, buildSetup)` via
`anticipatedBuildTarget(meta)`, parses repo from `meta.RemoteURL` or
input, then:

1. Optional: `GET /api/rest/public/github/repository` with the JWT to
   confirm bond + return repo list (if `repositoryFullName` omitted,
   surface structured prompt to agent).
2. `PUT /api/rest/public/service-stack/{buildTargetServiceId}/external-repository-integration`
   with `{repositoryFullName, eventType:"BRANCH", branchName,
   zeropsYamlSetup:buildSetup, isActive:true, triggerBuild}`.
3. `GET /api/rest/public/service-stack/{buildTargetServiceId}/external-repository-integration-status`
   to verify.
4. Drop the JWT reference. Never persist.

Mirrors the `launchKey` pattern in `launch-production` workflow
(one-shot account-level credential, in-memory only).

## Risks

- JWT paste in a transcript is a worse trust surface than the project
  PAT model. Atom guidance must call this out — opt-in only, with
  explicit warning.
- JWT lifetime is short. Handler must classify a 401 specifically as
  "token expired" and tell the agent to grab a fresh one.
- Account-level OAuth bond can be revoked from the user side; the
  flow must classify revocation errors distinctly from token
  expiration.

## Refs

- Spec: `docs/spec-local-dev.md` §3 (project-scoped PAT model)
- Pattern: `plans/archive/strategy-decomp/SHIP-WITH-NOTES.md` +
  `launch-production` launchKey design (one-shot account credential)
- Live-test evidence: this file's "Why the auth shape blocks ZCP"
  table, recorded against eval-zcp project
  `waAzEFn6SBaysG4YE4rv7A`, services `appstage` (post-OAuth,
  deleted) and `webhooktest` (fresh, deleted).
- SDK: `github.com/zeropsio/zerops-go@v1.0.18/sdk/{PutServiceStackExternalRepositoryIntegration,GetGithubRepository,…}.go`
- Frontend interceptor: `frontend-legacy/libs/zef/src/auth/auth-token.interceptor.ts:176`
- Related backlog: `plans/backlog/auto-wire-github-actions-secret.md`
