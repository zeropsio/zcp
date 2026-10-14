---
id: mono-e1-f3-parts
description: |
  F3 shape and stages live (mono-e0 TEST-PLAN §4 E1 row 21 for F3, E2 row
  13 up to the merge): a pnpm + turbo workspace planted on appdev, its
  parts declared only as the zerops.yml setups `web` (static) and `api`
  (node) beside `dev`; no dev-server declaration. The agent proposes the
  parts from the repo, asks at most once, runs both dev servers under
  appdev's supervisor, and stands up webstage (setup web) and apistage
  (setup api) from the one dev service.
seed: imported
fixture: fixtures/mono-pnpm-workspace-imported.yaml
preseedScript: preseed/plant-mono-pnpm-workspace.sh
tags: [monorepo, parts, stages, supervisor, dev-server, pnpm, vite]
area: develop
excludeFromAll: true
retrospective:
  promptStyle: briefing-future-agent
userPersona: |
  You own this workspace and know it has a web front end and an API. You
  want to develop on appdev and preview each part on stage. Accept what
  the agent proposes; answer a question in one short sentence. If the
  agent asks a second time about the same thing, say "as you proposed".
verification:
  mode: required
  spec: spec-mate.md §2.8a
  expectedServices:
    - hostname: appdev
      status: [ACTIVE]
      type: nodejs@*
    - hostname: webstage
      status: [ACTIVE]
      type: static*
      subdomainProbe: {expectStatus: "2xx"}
    - hostname: apistage
      status: [ACTIVE]
      type: nodejs@*
  toolArg:
    - {always: "zerops_dev_server{action=start}"}
  containerCheck:
    - {service: appdev, cmd: "grep -qE 'start: *zcp-devsup run' /var/www/zerops.yml && echo supervised", match: "^supervised"}
    - {service: appdev, cmd: "zcp-devsup ctl status --json | grep -cE '\"status\": *\"running\"'", match: "^[2-9]"}
    - {service: appdev, cmd: "curl -fsS http://127.0.0.1:8080/forecast >/dev/null && echo api-up", match: "^api-up"}
    - {service: appdev, cmd: "curl -fsS -H 'Host: appdev.example' http://127.0.0.1:3000/ >/dev/null && echo web-up", match: "^web-up"}
    - {service: apistage, cmd: "curl -fsS http://127.0.0.1:8080/forecast >/dev/null && echo stage-api-up", match: "^stage-api-up"}
  noFailedProcesses: true
  never: ["zerops_import{override=true}", "zerops_delete"]
---

This repository is on appdev. Set it up so I can develop on appdev and preview it on stage, then tell me where to open things.
