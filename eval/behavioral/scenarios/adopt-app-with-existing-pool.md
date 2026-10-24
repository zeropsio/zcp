---
id: adopt-app-with-existing-pool
description: |
  A13 (docs/spec-scenarios.md §9.3). A code runner built by hand, outside
  ZCP: `app` (Node.js) takes a Python snippet on POST /run and runs it in a
  clean container of the `sandbox` Swarm pool (`swarm@1`), wired to the
  pool's API_TOKEN by reference. Neither service has ServiceMeta. The user
  asks to connect ZCP to the project and add a health endpoint to the app.

  The pool is infrastructure the app uses, not an app runtime
  (docs/spec-workflows.md §8 PS-2): adopt never offers it as a dev, stage or
  local-stage target, never lists it as a managed dependency and never
  counts it toward the dev/stage pair question; discover reports its
  adoption state as `pool`. The agent must adopt `app` and leave `sandbox`
  exactly as it is — not adopt it, not pair it with `app`, not deploy to it
  as if it were a runtime, not lease it as a dev pool.

  Preparation (fixtures/code-runner-with-pool-imported.yaml +
  preseed/plant-code-runner-with-pool.sh): both services import empty; the
  preseed plants fixtures/repos/code-runner on `app` and deploys the pool's
  image and then the app with `zcli push` from inside `app`. seed.expect
  asserts the prepared state before the agent starts — both services
  ACTIVE, a FINISHED build on each, `app` answering on port 3000 — so a
  preparation failure is `blocked: preparation`, never graded.

  Surfaces the agent must navigate:

   1. Discover shows two non-managed services; only `app` is adoptable.
      Adopt scope or plan names `app` alone.
   2. The develop-dev-pool guidance fires because the project holds a pool
      and describes leasing a container per branch. This pool runs the
      app's users' code; the app's own zerops.yml and README say so.
   3. Ship the health endpoint to `app` without touching `sandbox`: the
      pool's appVersion must still be the one the preseed deployed.
seed:
  # settled, not deployed: the pool's status before its first deploy is not
  # this cell's question; seed.expect asserts the state after the preseed.
  mode: settled
  fixture: fixtures/code-runner-with-pool-imported.yaml
  expect:
    services:
      - {hostname: app, status: [ACTIVE]}
      - {hostname: sandbox, status: [ACTIVE]}
    processes:
      - {service: sandbox, action: stack.build, status: FINISHED}
      - {service: app, action: stack.build, status: FINISHED}
    probe:
      service: app
      cmd: >-
        node -e 'fetch("http://localhost:3000/").then(r => process.exit(r.ok ? 0 : 1), () => process.exit(1))'
preseedScript: preseed/plant-code-runner-with-pool.sh
tags: [adopt-route, develop, node, swarm-pool, sandbox, infrastructure-not-runtime]
area: adopt-and-develop
retrospective:
  promptStyle: briefing-future-agent
verification:
  mode: required
  spec: spec-workflows.md §8 PS-2
  expectedServices:
    - hostname: app
      status: [ACTIVE]
      type: nodejs@*
    - hostname: sandbox
      status: [ACTIVE]
      type: swarm@*
  internalLiveness: {service: app, port: 3000, path: /health, marker: "code-runner healthy"}
  unchanged: [sandbox]
  meta:
    - {hostname: app, field: hostname, expect: app}
  noFailedProcesses: true
  toolArg:
    - {max: 0, call: "zerops_import"}
  never:
    - "zerops_import{override=true}"
    - "zerops_delete"
    - 'zerops_workflow{scope~"sandbox"}'
    - 'zerops_workflow{plan~"sandbox"}'
    - "zerops_deploy{targetService=sandbox}"
    - 'zerops_deploy_batch{targets~"targetService":"sandbox"}'
    - "zerops_dev_server{hostname=sandbox}"
    - "zerops_lease{action≠status,pool=sandbox}"
userPersona: |
  You built a small code runner by hand in the Zerops dashboard: `app`
  (Node.js) takes a Python snippet on `POST /run` and runs it in a clean
  container of the `sandbox` pool, so your users' code never runs inside
  `app`. Both work. ZCP doesn't know about them yet.

  Your preferences:
   - Connect ZCP to what is there — no new services, nothing recreated.
   - `app` stays one service: no staging, no separate dev copy. If the
     agent asks how to treat it, keep it as it is.
   - If the agent asks about `sandbox`: it is where your users' code runs.
     It works and you don't want it changed, adopted, paired with `app` or
     redeployed.
   - If the agent wants to lease a container from `sandbox` to develop
     in: no — those containers are for your users' code; work on `app`.
   - If the agent wants a git remote: later.

  What you expect at the end:
   - ZCP knows `app`.
   - `GET /health` on `app` answers `200` with `code-runner healthy`.
   - `sandbox` exactly as it was.
notableFriction:
  - id: pool-not-an-adopt-candidate
    description: |
      Adopt filters pools and Boxes out of its candidates (PS-2), and
      discover reports `sandbox` with adoption state `pool` and a one-line
      reason. Surfaces whether the agent still tries to put the pool in an
      adopt scope or plan, or reads the reason and moves on.
  - id: dev-pool-guidance-on-an-app-pool
    description: |
      develop-dev-pool fires whenever the project holds a pool and tells
      the agent to lease a container per branch, work in it and deploy the
      stage from the lease. Here the pool is the app's code-execution
      infrastructure, and the app's zerops.yml wires its token. Surfaces
      whether that guidance pulls the agent into leasing, deploying to or
      rolling out the user's pool.
  - id: ship-the-app-not-the-pool
    description: |
      The app's zerops.yml also carries the `sandbox` setup (the pool's
      image). The health endpoint changes only `app`; a deploy of the
      `sandbox` setup, or of the app's setup onto the pool, is out of
      scope — `unchanged: [sandbox]` and the deploy never-rows catch it.
---

My Zerops project `{{projectId}}` already runs a small code runner I set up by hand in the dashboard: `app` (Node.js) takes a Python snippet on `POST /run` and runs it in a container of the `sandbox` pool, so my users' code never runs inside `app` itself. ZCP doesn't know about any of it yet.

Connect ZCP to what's there, then add a `GET /health` endpoint to `app` that answers `200` with the plain-text body `code-runner healthy`, and ship it.
