---
id: mono-e2-f1-add-worker-keeps-app
description: |
  E2 row 14 live (mono-e0 TEST-PLAN §4): adding `worker` to the F1 app
  keeps the existing stage service named appstage (setup prod). The F1
  Bun page is planted on appdev; the agent first puts it on appdev and
  appstage as in mono-e1-f1-single, then adds a background worker as a
  second part: a `worker` setup, a preview service for it, and a dev
  server on appdev. appstage is never renamed, recreated or re-pointed:
  appdev's stages[0] stays appstage/prod and the worker preview joins as
  stages[1] with setup worker.
seed: imported
fixture: fixtures/mono-single-bun-imported.yaml
preseedScript: preseed/plant-mono-single-bun.sh
tags: [monorepo, parts, stages, worker, bun]
area: develop
excludeFromAll: true
retrospective:
  promptStyle: briefing-future-agent
userPersona: |
  You own a small Bun weather page. You want it on appdev and appstage,
  and then a background worker next to it that logs the Praha forecast
  every minute. Accept the agent's defaults; answer a question in one
  short sentence. If the agent asks a second time about the same thing,
  say "as you proposed".
verification:
  mode: required
  spec: spec-mate.md §2.8a
  expectedServices:
    - hostname: appdev
      status: [ACTIVE]
      type: bun@*
    - hostname: appstage
      status: [ACTIVE]
      type: bun@*
      subdomainProbe: {expectStatus: "2xx"}
  meta:
    - {hostname: appdev, field: stages.0.hostname, expect: "appstage"}
    - {hostname: appdev, field: stages.0.setupName, expect: "prod"}
    - {hostname: appdev, field: stages.1.setupName, expect: "worker"}
  containerCheck:
    - {service: appdev, cmd: "grep -qE '^ *- setup: *worker\\b' /var/www/zerops.y*ml && echo worker-setup", match: "^worker-setup"}
    - {service: appdev, cmd: "zcp-devsup ctl status --json | grep -cE '\"status\": *\"running\"'", match: "^[2-9]"}
  noFailedProcesses: true
  never: ["zerops_import{override=true}", "zerops_delete"]
---

My weather page is in the repository on appdev. Get it running for development on appdev and put it on appstage too. Then add a background worker as its own part, one that logs the Praha forecast every minute, and tell me where everything runs.
