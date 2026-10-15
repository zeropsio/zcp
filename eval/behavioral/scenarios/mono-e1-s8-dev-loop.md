---
id: mono-e1-s8-dev-loop
description: |
  S8 dev loop live (mono-e0 TEST-PLAN §4 E1 row 22, TARGETS §1): a
  z3-shaped Node 24 workspace — a server on a fixed port from env (never
  3773, the installed Mate unit's port) and a Vite web that proxies /api to
  it — planted on appdev with only today's dev and prod setups. The agent
  runs server and web as two named dev servers in the one dev service
  under the supervisor, with the ports pinned through the environment, and
  the web reaches the server through its own origin. The real z3 is a
  private repository the farm cannot clone, so this is its shape, not its
  tree.
seed: imported
fixture: fixtures/mono-s8-devloop-imported.yaml
preseedScript: preseed/plant-mono-z3-shape.sh
tags: [monorepo, dev-loop, supervisor, dev-server, vite, nodejs, s8]
area: develop
excludeFromAll: true
retrospective:
  promptStyle: briefing-future-agent
userPersona: |
  You own this workspace: a server and a web in apps/. You want both
  running on appdev while you develop. Accept what the agent proposes;
  answer a question in one short sentence. If the agent asks a second time
  about the same thing, say "as you proposed".
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
    - {service: appdev, cmd: "grep -q '\"name\": *\"server\"' /var/www/.zcp/dev-servers.json && grep -q '\"name\": *\"web\"' /var/www/.zcp/dev-servers.json && echo named-server-and-web", match: "^named-server-and-web"}
    - {service: appdev, cmd: "zcp-devsup ctl status --json | grep -cE '\"status\": *\"running\"'", match: "^[2-9]"}
    - {service: appdev, cmd: "curl -fsS -H 'Host: appdev.example' http://127.0.0.1:5733/api/hello | grep -q '\"hello\":\"server\"' && echo web-reaches-server", match: "^web-reaches-server"}
    - {service: appdev, cmd: "(ss -ltn 2>/dev/null || netstat -ltn) | grep -q ':3773 ' && echo took-mate-port || echo mate-port-free", match: "^mate-port-free"}
  noFailedProcesses: true
  never: ["zerops_import{override=true}", "zerops_delete"]
---

This workspace is on appdev: a server and a web. Set up the development loop so both run on appdev while I work, then tell me where to open the web.
