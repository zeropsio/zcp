---
id: launch-production-existing-project-token
description: |
  User already has a production Zerops project (created manually some time ago)
  and wants to seed the code from this dev/stage project INTO it — not create a
  new project. The EXISTING-project path is entered by INPUT PRESENCE, not a
  project-mode prompt: supplying `ExistingProjectID` + `ExistingProdToken`
  (instead of a launchKey) routes to the existing-project mutation. The two
  paths are mutually exclusive — supplying both a launchKey and the existing-
  project pair is rejected.

  Critical safety gate (P-LP-12 scope validation, enforced BEFORE any mutation):
   - `GetUserInfo` with `ExistingProdToken` succeeds (token is valid)
   - the token scopes to EXACTLY 1 project
   - that one project ID equals the user-declared `ExistingProjectID`
  Any failure (multi-project token, no access, scope mismatch) refuses with a
  structured error — no mutation attempted.

  After validation, the existing-project import is services-only: the composed
  yaml carries NO `project:` block (the services-only endpoint rejects one).
  Hostname-conflict preflight (P-LP-12): if an about-to-import hostname already
  exists on the target project, ZCP does NOT advance — it surfaces
  `existing-project-conflict-prompt` with one blocker per colliding hostname,
  each carrying a `mergeStrategy` choice (skip = additive / replace = requires
  `confirmDestructive`). The launch token is still staged as the source push
  service's `ZCP_LAUNCH_TOKEN` secret (P-LP-14, single-token lifecycle).

  Delivery family is DERIVED from the source pair's BuildIntegration and
  surfaced in the launched `firstRelease` block — not chosen via a prompt.
seed: deployed
fixture: fixtures/nodejs-standard-deployed.yaml
tags: [launch-production, existing-project, token-scope-gate, services-only-import, hostname-conflict, single-token, P-LP-12, P-LP-14]
area: launch
retrospective:
  promptStyle: briefing-future-agent
userPersona: |
  You're an SRE with an existing production Zerops project (created months ago
  via the dashboard). You want to seed the code from your current dev/stage into
  that production project — without recreating it. You provide:
   - the existing prod project's UUID (fetched from the dashboard URL)
   - a fresh project-scoped token with Full access to JUST that prod project
     (Custom access per project, Full).

  You will NOT give ZCP an account-wide token. Supplying the existing-project
  pair (`ExistingProjectID` + `ExistingProdToken`) is what routes ZCP onto the
  existing-project path — there is no "New vs Existing" menu to pick from. If
  ZCP refuses because your token scopes to multiple projects or to a different
  project than you declared, you regenerate a properly scoped one. Token
  validation MUST happen before any mutation.

  After import, you expect ZCP to tell you the DERIVED production delivery (from
  your source pair's integration) in the launched response — not ask you to pick
  a delivery method.
notableFriction:
  - id: input-presence-selects-existing-path
    description: |
      The existing-project path is entered by supplying `ExistingProjectID` +
      `ExistingProdToken`, not by answering a project-mode prompt. Surfaces
      whether the agent collects both fields up front and re-calls, or stalls
      waiting for a "New vs Existing" elicitation that does not exist.
  - id: token-scope-validation-gate
    description: |
      Before any mutation, ZCP validates `ExistingProdToken` via `GetUserInfo`,
      requires it to scope to EXACTLY 1 project, and requires that project to
      equal `ExistingProjectID` (P-LP-12). Surfaces whether the gate is honored:
      a multi-project token must refuse; a token valid for a DIFFERENT project
      must refuse with a scope-mismatch error. The agent must read the refusal
      and ask for a correctly scoped token rather than retrying the same one.
  - id: services-only-import
    description: |
      The existing-project import yaml carries NO `project:` block (the
      services-only endpoint rejects one — the project already exists). Surfaces
      whether the agent is confused if the compose preview omits the project
      block, or treats its absence as a bug.
  - id: hostname-conflict-preflight
    description: |
      If an about-to-import hostname collides with an existing service on the
      target project, ZCP returns `existing-project-conflict-prompt` with one
      blocker per colliding hostname (P-LP-12). Each needs a per-host
      `mergeStrategy` (skip = additive drop / replace = requires
      `confirmDestructive`). Surfaces whether the agent reports the collision
      and asks the user per-host, or blindly retries.
  - id: single-token-staged
    description: |
      Even on the existing-project path, the launch token is staged as the
      source push service's `ZCP_LAUNCH_TOKEN` secret and read server-side by
      later window ops (P-LP-14). Surfaces whether the agent expects to re-paste
      the token for prod-ops or understands the staged-secret lifecycle.
---

I have an existing production Zerops project — I'll give you its UUID. I want to deploy this dev/stage code INTO it, not create a new project. I'll provide a fresh project-scoped token with Full access to ONLY that prod project. If my token scopes to anything else, or to a different project than the UUID I gave you, refuse — I'll regenerate. After import succeeds, tell me how production will be delivered.
