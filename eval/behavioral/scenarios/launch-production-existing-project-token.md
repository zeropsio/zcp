---
id: launch-production-existing-project-token
description: |
  User already has a production Zerops project (created manually some
  time ago) and wants to deploy code from this dev/stage project INTO
  it — not create a new project. The redesigned launch flow surfaces a
  Q1 project-mode prompt; user picks Existing and provides:
   - `ExistingProjectID` (UUID from prod-project dashboard)
   - `ExistingProdToken` (project-scoped token created on that project)

  Critical safety gate (P-LP-12): ZCP validates the token scope BEFORE
  mutation:
   - `GetUserInfo` succeeds (token is valid)
   - `ListProjects(clientID)` returns EXACTLY 1 project
   - That one project ID equals user-declared `ExistingProjectID`

  If any check fails (multi-project token, no project access, scope
  mismatch), ZCP refuses with a structured error. No mutation attempted.

  After validation passes, ZCP runs the split-mutation path (P-LP-13):
   - `CreateProjectEnv` per project-level env (one call each)
   - `PostProjectServiceStackImport` with services-only yaml (no
     `project:` block — would be rejected by the API).

  Preflight (P-LP-14): if any about-to-import hostname already exists
  on the target project, ZCP refuses with hostname-conflict blocker.
seed: deployed
fixture: fixtures/nodejs-standard-deployed.yaml
tags: [launch-production, project-mode-existing, token-scope-gate, P-LP-12, P-LP-13, P-LP-14, services-only-import]
area: launch
retrospective:
  promptStyle: briefing-future-agent
userPersona: |
  You're an SRE with an existing production Zerops project (created
  months ago via the dashboard). You want to seed the code from your
  current dev/stage into that production project — without recreating
  it. You'll provide:
   - The existing prod project's UUID (you'll fetch from dashboard URL)
   - A fresh project-scoped token with Full access to JUST that prod
     project (Custom access per project, Full).

  You will NOT give ZCP an account-wide token. If ZCP refuses because
  your token scopes to multiple projects, you'll regenerate a properly
  scoped one. Token validation MUST happen before any mutation.

  After launch, set up CI/CD via Actions (same flow as a new-project
  launch — Actions composer is shared).
notableFriction:
  - id: project-mode-q1-existing
    description: |
      First MCP call returns status `awaiting-project-mode-choice`.
      Agent picks Existing and re-calls with `LaunchProjectMode=Existing`
      + `ExistingProjectID=<uuid>` + `ExistingProdToken=<value>`.
      Surfaces whether the agent collects both required fields up front
      or makes a round-trip per field.
  - id: token-scope-validation-gate
    description: |
      ZCP must validate token scope = exactly 1 project AND matches
      ExistingProjectID before any mutation. Surfaces whether the
      validation is wired or skipped. Multi-project token must refuse.
      Scope-mismatch (token works but for a DIFFERENT project) must
      refuse with structured error pointing at scope-mismatch.
  - id: split-mutation-path
    description: |
      Existing-project path uses CreateProjectEnv (per env) +
      PostProjectServiceStackImport (services-only yaml). The yaml
      sent has NO `project:` block — API rejects yaml with project block
      on the services-only endpoint. Surfaces whether the composer
      strips the project block when ProjectMode=Existing.
  - id: hostname-conflict-preflight
    description: |
      Before mutation, ZCP runs ListServices on the existing project
      and refuses if any about-to-import hostname conflicts with an
      existing service. Surfaces whether the agent reads the refuse
      message and reports hostname collision to the user instead of
      blindly retrying.
  - id: no-launchKey-no-delete-atom
    description: |
      Existing-project path doesn't use LaunchKey (it uses
      ExistingProdToken, which is project-scoped not account-wide).
      The terminal response omits `launch-delete-key` atom — user
      keeps the project-scoped token for ongoing operations OR
      rotates it via dashboard. Surfaces whether the agent expects
      the same revoke flow as new-project launch.
---

I have an existing production Zerops project (UUID I'll provide). I want to deploy this dev/stage code into it — not create a new project. I'll provide a fresh project-scoped token with Full access to ONLY that prod project. If my token scopes to anything else, refuse — I'll regenerate. After import succeeds, set up CI/CD via GitHub Actions push-mode.
