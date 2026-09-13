---
id: internal-only-worker
description: |
  Absorbs D7 (idle-worker verify). Standard Node.js pair + managed
  Postgres, every service already without a public subdomain. The agent
  adds a third, background `worker` service that must stay unreachable
  from the internet — reachable only over the project's internal
  network — while exposing a health endpoint the internalLiveness oracle
  can probe directly (no subdomain involved).
seed: deployed
fixture: fixtures/standard-pair-no-subdomain-worker-ready.yaml
tags: [develop, internal-only, worker, no-subdomain, node]
area: develop
retrospective:
  promptStyle: briefing-future-agent
verification:
  mode: required
  spec: spec-workflows.md §4
  expectedServices:
    - {hostname: appdev, status: [ACTIVE]}
    - {hostname: appstage, status: [ACTIVE]}
    - {hostname: db, status: [ACTIVE]}
    - {hostname: worker, status: [ACTIVE]}
  internalLiveness: {service: worker, port: 8080, path: /healthz, marker: ok}
  noFailedProcesses: true
  toolArg:
    - {never: "zerops_import{content~enableSubdomainAccess: *true}"}
  never: ["zerops_subdomain", "zerops_import{override=true}", "zerops_delete"]
notableFriction:
  - id: no-subdomain-anywhere
    description: |
      The user says nothing in this project should get a public
      subdomain. The new `worker` service must import with
      enableSubdomainAccess unset/false, and no zerops_subdomain
      enable call should ever run against it or the existing pair.
  - id: internal-health-endpoint
    description: |
      The health check the user asks for is reachable only over the
      project's internal network (http://worker:8080/healthz), never
      through a subdomain — the internalLiveness oracle probes it
      directly from the run container, no subdomain resolution involved.
---

## Starting state

A Node.js `appdev`/`appstage` pair with a managed `db`, already deployed.
None of the three services has a public subdomain, and none should.

## Task

Add a background worker service called `worker` (Node) that processes a
queue. It must not be reachable from the internet; expose only an
internal health endpoint on port 8080 at `/healthz` that returns `ok`.
Nothing in this project should get a public subdomain.

## Resources

No credentials needed. If you ask whether `worker` should ever get a
subdomain, the answer is no — for this project or any of its services.
