---
id: subdomain-user-disabled-stays-off
description: |
  PA-2 (docs/spec-workflows.md §8 O3): once zcp has auto-enabled a
  subdomain, it never re-enables on its own — an observed-off subdomain
  after the one-time stamp means the user switched it off, and intent
  reconciles from auto to none. The preseed plants the ServiceMeta state
  zcp leaves after its one auto-enable (publicAccess.appdev intent=auto,
  subdomainEnabledByZcpAt set) and then disables the subdomain via the
  platform API — simulating a user who turned it off after zcp turned it
  on. The agent edits the mounted dev source and ships a normal deploy; it
  must never re-enable the subdomain, and the deploy/dev-server hooks must
  not re-enable it either.
seed:
  mode: deployed
  fixture: fixtures/nodejs-dev-deployed-mounted.yaml
  expect:
    services:
      - {hostname: appdev, status: [ACTIVE]}
      - {hostname: db, status: [ACTIVE]}
preseedScript: preseed/subdomain-user-disabled-stays-off.sh
tags: [develop, public-access, subdomain, no-reenable, node]
area: develop
retrospective:
  promptStyle: briefing-future-agent
verification:
  mode: required
  spec: spec-workflows.md §8
  expectedServices:
    - {hostname: appdev, status: [ACTIVE], subdomainAccess: false}
    - {hostname: db, status: [ACTIVE]}
  internalLiveness: {service: appdev, port: 3000, path: /, marker: hello-v2}
  meta: [{hostname: appdev, field: publicAccess.appdev.intent, expect: none}]
  noFailedProcesses: true
  toolArg:
    - {max: 0, call: "zerops_subdomain"}
  never: ["zerops_import{override=true}", "zerops_delete"]
notableFriction:
  - id: no-manual-reenable
    description: |
      The subdomain was auto-enabled once by zcp and then switched off by
      the user (simulated by the preseed calling the platform's
      disable-subdomain-access endpoint directly, after planting the
      auto-enable stamp in ServiceMeta). The agent must not call
      zerops_subdomain at all — neither to "fix" what looks like a
      disabled route nor for any other reason the task doesn't ask for.
  - id: deploy-hook-must-not-reenable
    description: |
      This is the row that would have caught the escaped 2026-09-13
      mutation batch finding (docs/spec-scenarios.md §9.4): "every current
      cell gets its subdomain from the import flag, so no cell depends on
      the deploy handler's auto-enable." Here the import flag is
      irrelevant (the subdomain was already enabled and then explicitly
      turned off) — the only way this cell stays green is if the deploy
      and dev-server-start hooks correctly read the stamped ServiceMeta
      and skip auto-enable (PA-2), never the reverse.
---

## Starting state

`appdev` is a single Node.js dev-only service with a managed `db`, already
deployed and mounted. zcp auto-enabled its subdomain once, the way it does
for every fresh dev-mode runtime — and the user has since turned that
subdomain off. Nothing in this project should get a subdomain back
without an explicit request.

## Task

Change the root response of `appdev` so it includes the text `hello-v2`,
and get that change deployed. I don't want a public subdomain for this
service — I turned it off on purpose.

## Resources

No credentials needed — everything you need is visible from the
project's own state and the mounted source at `/var/www`.
