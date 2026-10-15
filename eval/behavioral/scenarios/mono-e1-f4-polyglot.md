---
id: mono-e1-f4-polyglot
description: |
  F4 shape atoms live (mono-e0 TEST-PLAN §4 E1 row 21, MODEL S4): a Node
  front end in web/ and a Python API in api/ planted on appdev, with only
  today's `dev` setup in zerops.yml and no dev-server declaration. The
  agent proposes the two parts from the sub-folder signals, asks at most
  once, installs the second runtime on the Node dev service and runs both
  parts as named dev servers under appdev's supervisor.
seed: imported
fixture: fixtures/mono-node-python-imported.yaml
preseedScript: preseed/plant-mono-node-python.sh
tags: [monorepo, parts, polyglot, supervisor, dev-server, python, nodejs]
area: develop
excludeFromAll: true
retrospective:
  promptStyle: briefing-future-agent
userPersona: |
  You own this repository: a Node web front end and a Python API. You
  want to develop both on appdev. Accept what the agent proposes; answer
  a question in one short sentence. If the agent asks a second time about
  the same thing, say "as you proposed".
verification:
  mode: required
  spec: spec-mate.md §2.8a
  expectedServices:
    - hostname: appdev
      status: [ACTIVE]
      type: nodejs@*
  toolArg:
    - {always: "zerops_dev_server{action=start}"}
  containerCheck:
    - {service: appdev, cmd: "grep -qE 'start: *zcp-devsup run' /var/www/zerops.yml && echo supervised", match: "^supervised"}
    - {service: appdev, cmd: "grep -c '\"name\"' /var/www/.zcp/dev-servers.json", match: "^[2-9]"}
    - {service: appdev, cmd: "zcp-devsup ctl status --json | grep -cE '\"status\": *\"running\"'", match: "^[2-9]"}
    - {service: appdev, cmd: "curl -fsS http://127.0.0.1:8080/forecast >/dev/null && echo python-api-up", match: "^python-api-up"}
    - {service: appdev, cmd: "curl -fsS -H 'Host: appdev.example' http://127.0.0.1:3000/ | grep -q Praha && echo web-reaches-api", match: "^web-reaches-api"}
  noFailedProcesses: true
  never: ["zerops_import{override=true}", "zerops_delete"]
---

This repository is on appdev: a web front end and an API. Get both running for development on appdev, then tell me where to open them.
