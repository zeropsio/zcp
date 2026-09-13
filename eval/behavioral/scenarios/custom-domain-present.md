---
id: custom-domain-present
description: |
  PA-3 (docs/spec-workflows.md §8 O3): domains != ∅ always wins — a
  service reachable via a project-level custom-domain routing must never
  get a subdomain auto-enable attempt, and verify must probe the domain
  (public_domain check) rather than the subdomain. This is a
  verify-only cell: no mutation, so no noFailedProcesses row. The
  preseed disables the subdomain and creates the routing directly via
  the platform API (never through zerops_subdomain, and before the agent
  is ever spawned) so the agent's job is purely to observe and report,
  never to configure access itself.
seed:
  mode: deployed
  fixture: fixtures/nodejs-simple-deployed.yaml
  expect:
    services:
      - {hostname: api, status: [ACTIVE]}
      - {hostname: db, status: [ACTIVE]}
preseedScript: preseed/custom-domain-present.sh
tags: [verify, public-access, custom-domain, no-subdomain, node]
area: develop
retrospective:
  promptStyle: briefing-future-agent
verification:
  mode: required
  spec: spec-workflows.md §8
  expectedServices:
    - {hostname: api, status: [ACTIVE]}
    - {hostname: db, status: [ACTIVE]}
  toolArg:
    - {max: 0, call: "zerops_subdomain"}
  toolResult:
    - {tool: zerops_verify, contains: "public_domain"}
    - {tool: zerops_verify, contains: "farm-"}
  mustOffer: ["farm-[A-Za-z0-9_-]+\\.example\\.com"]
  never: ["zerops_import{override=true}", "zerops_delete"]
notableFriction:
  - id: domain-not-subdomain
    description: |
      `api` already has a project-level custom-domain routing
      (farm-<runId>.example.com) pointed at it, created before the agent
      ever runs (the fixture's own subdomain, on by default, is switched
      off by the same preseed first). The agent must recognize the
      domain as the reachability surface (via zerops_verify's
      public_domain check) and never call zerops_subdomain — the domain
      wins over the default auto-enable path (PA-3), and there is
      nothing to configure here.
  - id: dns-not-delegated
    description: |
      This scenario's domain is never actually DNS-delegated to Zerops
      (a fixed fake domain, not a real one this project controls) — the
      platform accepts the routing record regardless (live-verified
      2026-09-13), but the live DNS/TLS/HTTP state of the domain itself
      may legitimately report as not-yet-pointing. The oracles here
      assert that the check ran and named the domain, not that DNS
      resolves — a correct agent reports the domain as the configured
      reachability surface and describes its current DNS state
      honestly, whatever that state is.
---

## Starting state

`api` is a single Node.js service with a managed `db`, already deployed.
It has no subdomain — instead, a custom domain is already routed to it at
the project level.

## Task

Verify that `api` is healthy, and tell me how it is reachable from the
internet.

## Resources

No credentials needed — everything you need is visible from the
project's own state (service status, the project's public HTTP routing).
