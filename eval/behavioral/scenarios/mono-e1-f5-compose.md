---
id: mono-e1-f5-compose
description: |
  F5 shape atoms live (mono-e0 TEST-PLAN §4 E1 row 21, MODEL S5, atoms
  §9/1e): a docker-compose repository (web, api, db) planted on appdev,
  with only today's `dev` setup in zerops.yml. The agent detects the
  compose file, asks once whether to convert it to native Zerops parts
  with the database as a managed service, and on yes runs web and api as
  named dev servers on appdev with a managed PostgreSQL named db. It
  never runs Docker.
seed: imported
fixture: fixtures/mono-compose-imported.yaml
preseedScript: preseed/plant-mono-compose.sh
tags: [monorepo, parts, docker-compose, supervisor, dev-server, postgresql]
area: develop
excludeFromAll: true
retrospective:
  promptStyle: briefing-future-agent
userPersona: |
  You own this repository; it came with a docker-compose.yml you do not
  need to keep running. When the agent asks whether to convert the compose
  services to native Zerops parts with the database as a managed service,
  answer "yes, convert it". Answer any other question in one short
  sentence. If the agent asks a second time about the same thing, say "as
  you proposed".
verification:
  mode: required
  spec: spec-mate.md §2.8a
  expectedServices:
    - hostname: appdev
      status: [ACTIVE]
      type: nodejs@*
    - hostname: db
      status: [ACTIVE]
      type: postgresql*
  toolArg:
    - {always: "zerops_dev_server{action=start}"}
  containerCheck:
    - {service: appdev, cmd: "cat /var/www/zerops.y*ml | grep -qE 'start: *zcp-devsup run' && echo supervised", match: "^supervised"}
    - {service: appdev, cmd: "grep -c '\"name\"' /var/www/.zcp/dev-servers.json", match: "^[2-9]"}
    - {service: appdev, cmd: "curl -fsS http://127.0.0.1:8080/health | grep -q 'db-configured' && echo api-has-db", match: "^api-has-db"}
    - {service: appdev, cmd: "curl -fsS -H 'Host: appdev.example' http://127.0.0.1:3000/ | grep -q Praha && echo web-reaches-api", match: "^web-reaches-api"}
  noFailedProcesses: true
  never: ["zerops_import{override=true}", "zerops_delete"]
---

This repository came with a docker-compose.yml: a web, an api and a database. Get it running for development on appdev, then tell me where to open it.
