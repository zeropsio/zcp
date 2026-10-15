---
id: mono-e1-f6-spa-api-onefolder
description: |
  F6 shape atoms live (mono-e0 TEST-PLAN §4 E1 row 21, MODEL S7, decision
  7): one package.json with a Vite SPA and a Node server that serves dist/
  and /api, planted on the appdev/appstage pair with only today's `dev`
  setup. The agent may ask once whether this is one app or two; the app
  stays ONE part: no `web`/`api` setups, one stage (appstage, setup prod)
  and the path of F1 — dev setup on appdev, prod setup on appstage, both
  subdomains answering.
seed: imported
fixture: fixtures/mono-spa-api-onefolder-imported.yaml
preseedScript: preseed/plant-mono-spa-api-onefolder.sh
tags: [monorepo, single-part, spa, vite, supervisor, nodejs]
area: develop
excludeFromAll: true
retrospective:
  promptStyle: briefing-future-agent
userPersona: |
  You own a small weather app: a Vite page and a Node server that serves
  it and the /api. It is one application. If the agent asks whether this
  is one app or two, answer "one app, the server serves the page". Answer
  any other question in one short sentence.
verification:
  mode: required
  spec: spec-mate.md §2.8a
  expectedServices:
    - hostname: appdev
      status: [ACTIVE]
      type: nodejs@*
    - hostname: appstage
      status: [ACTIVE]
      type: nodejs@*
      subdomainProbe: {expectStatus: "2xx"}
  meta:
    - {hostname: appdev, field: stages.0.hostname, expect: "appstage"}
    - {hostname: appdev, field: stages.0.setupName, expect: "prod"}
  containerCheck:
    - {service: appdev, cmd: "grep -qE '^ *- setup: *(web|api)\\b' /var/www/zerops.yml || echo one-part", match: "^one-part"}
    - {service: appdev, cmd: "grep -qE 'start: *zcp-devsup run' /var/www/zerops.yml && echo supervised", match: "^supervised"}
    - {service: appstage, cmd: "curl -fsS http://127.0.0.1:3000/api/forecast >/dev/null && echo stage-serves-api", match: "^stage-serves-api"}
    - {service: appstage, cmd: "curl -fsS http://127.0.0.1:3000/ | grep -q 'src=' && echo stage-serves-spa", match: "^stage-serves-spa"}
  noFailedProcesses: true
  never: ["zerops_import{override=true}", "zerops_delete"]
---

My weather app is in the repository on appdev. Get it running for development on appdev and put it on appstage too, then tell me both addresses.
