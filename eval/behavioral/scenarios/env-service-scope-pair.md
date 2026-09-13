---
id: env-service-scope-pair
description: |
  Existing Node.js standard pair (appdev/appstage) with a managed
  Postgres. The user asks for a service-scope env var (a feature flag)
  on both halves of the pair, and expects the running processes to
  actually observe it — not just the platform's stored env record.
seed: deployed
fixture: fixtures/nodejs-standard-deployed.yaml
tags: [develop, env, service-scope, standard-pair, node]
area: develop
retrospective:
  promptStyle: briefing-future-agent
verification:
  mode: required
  spec: spec-workflows.md §4
  containerCheck:
    - {service: appdev, cmd: "printenv FEATURE_X", expect: "on"}
    - {service: appstage, cmd: "printenv FEATURE_X", expect: "on"}
  toolResult:
    - {tool: zerops_env, contains: restartedServices}
  unchanged: [appdev, appstage]
  noFailedProcesses: true
  toolArg:
    - {never: "zerops_manage{action=reload}"}
  never: ["zerops_import{override=true}", "zerops_delete"]
notableFriction:
  - id: restart-not-redeploy
    description: |
      A service-scope env var takes effect on restart, not a rebuild —
      surfaces whether the agent reaches for zerops_env (which restarts
      the affected services itself) rather than a manual reload/manage
      call or an unnecessary redeploy.
  - id: both-halves-of-the-pair
    description: |
      The user says "both dev and stage" explicitly — the flag must
      reach appdev AND appstage; db is a dependency, untouched.
  - id: verified-not-assumed
    description: |
      "Make sure the running processes actually see it" — the agent
      should confirm the live value (e.g. reading it back), not just
      report the env write as done.
---

## Starting state

An existing Node.js `appdev`/`appstage` pair with a managed `db`,
already deployed and working.

## Task

Turn on the feature flag `FEATURE_X=on` for this app, on both dev and
stage, and make sure the running processes actually see it.

## Resources

No credentials needed. `db` should not be touched by this change.
