---
id: recover-build-failed
description: |
  Single dev-only Node.js runtime, deployed and mounted. The preseed
  script (preseed/recover-build-failed.sh) breaks the mounted source
  (adds a dependency that cannot resolve) and triggers a self-deploy
  from INSIDE appdev over SSH — never through a zerops_deploy tool call
  — then polls until that build's process reaches FAILED. seed.expect
  asserts, before the agent is spawned: appdev is still ACTIVE (the
  earlier appVersion keeps serving) and the latest stack.build process
  against appdev is FAILED. The agent's job is to find the break in the
  mounted source and ship a working deploy, without recreating the
  service.

  This row pins the known gap from batch `merge-ready-1`: zcp has no
  structured failureClass surfaced today for a FAILED build — the
  toolResult row on zerops_events is expected to fail until that lands.
seed:
  mode: deployed
  fixture: fixtures/nodejs-dev-deployed-mounted.yaml
  expect:
    services:
      - {hostname: appdev, status: [ACTIVE]}
    processes:
      - {service: appdev, action: stack.build, status: FAILED}
preseedScript: preseed/recover-build-failed.sh
tags: [recovery, failed-state, mount, node, known-gap]
area: recovery
retrospective:
  promptStyle: briefing-future-agent
verification:
  mode: required
  spec: spec-workflows.md §8
  liveness: {service: appdev, marker: "nodejs"}
  noFailedProcesses: true
  allowFailed: [appdev]
  containerCheck:
    - {service: appdev, cmd: "grep -c left-pad-does-not-exist /var/www/package.json", expect: "0"}
  toolResult:
    - {tool: zerops_events, contains: failureClass}
  toolArg:
    - {max: 0, call: "zerops_import"}
  never: ["zerops_import{override=true}", "zerops_delete"]
userPersona: |
  The last deploy of your `appdev` service failed and you don't know
  why. You expect the agent to diagnose the cause from logs/events and
  ship a working deploy. You don't know the underlying cause is a bad
  dependency in package.json. Push back if the agent proposes recreating
  the service instead of fixing the mounted source.
notableFriction:
  - id: diagnose-before-fixing
    description: |
      The FAILED build's cause (an unresolvable dependency in
      package.json) is discoverable from build logs / zerops_events —
      surfaces whether the agent reaches for that evidence before
      editing blindly.
  - id: fix-source-not-recreate
    description: |
      The right recovery is editing the mounted source and redeploying
      appdev, never zerops_import override=true / zerops_delete —
      surfaces whether the agent reaches for the destructive escape
      hatch instead of the non-destructive redeploy path.
  - id: no-structured-failure-class
    description: |
      Known gap (batch merge-ready-1): zcp does not yet surface a
      structured failureClass for a FAILED build via zerops_events.
      This toolResult row is expected to fail until that lands —
      recorded here so a batch run doesn't misattribute the gap to
      this scenario being wrong.
---

## Starting state

`appdev` is a single Node.js dev-only service, already deployed and
mounted. Before you start, its last build failed — the container that
was previously working is still serving, but the newest deploy did not
go out.

## Task

The last deploy of `appdev` failed and I don't know why. Find out what
broke, fix it in the mounted source, and get a working deploy out.
Don't recreate the service.

## Resources

No credentials needed — everything you need is visible from the
project's own state (build logs, events, the mounted source at
`/var/www`).
