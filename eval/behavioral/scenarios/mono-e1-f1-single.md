---
id: mono-e1-f1-single
description: |
  F1, the single-part invariant live (mono-e0 TEST-PLAN §2.3 L4, E1 rows 13
  and 20): one Bun part planted on appdev with an uncommitted edit and an
  untracked file, appdev and appstage running and empty. The agent deploys
  the dev setup on appdev, starts its dev server and deploys the prod setup
  to appstage. The app looks and behaves as today: the pair is appdev +
  appstage, the dev server is named after the dev hostname, both
  subdomains answer, and the dev service runs under the supervisor with
  the uncommitted work kept.
seed: imported
fixture: fixtures/mono-single-bun-imported.yaml
preseedScript: preseed/plant-mono-single-bun.sh
tags: [monorepo, single-part, supervisor, dev-server, bun]
area: develop
excludeFromAll: true
retrospective:
  promptStyle: briefing-future-agent
userPersona: |
  You own a small Bun weather page. You want to iterate on it live on
  appdev and see it on appstage. Accept the agent's defaults; answer any
  question in one short sentence. You never ask for anything beyond that.
verification:
  mode: required
  spec: spec-mate.md §2.8a
  expectedServices:
    - hostname: appdev
      status: [ACTIVE]
      type: bun@*
      subdomainProbe: {expectStatus: "2xx"}
    - hostname: appstage
      status: [ACTIVE]
      type: bun@*
      subdomainProbe: {expectStatus: "2xx"}
  meta:
    - {hostname: appdev, field: stages.0.hostname, expect: "appstage"}
    - {hostname: appdev, field: stages.0.setupName, expect: "prod"}
  containerCheck:
    - {service: appdev, cmd: "grep -qE 'start: *zcp-devsup run' /var/www/zerops.y*ml && echo supervised", match: "^supervised"}
    - {service: appdev, cmd: "grep -c '\"name\"' /var/www/.zcp/dev-servers.json", match: "^1\\s*$"}
    - {service: appdev, cmd: "grep -q '\"name\": *\"appdev\"' /var/www/.zcp/dev-servers.json && echo named-after-host", match: "^named-after-host"}
    - {service: appdev, cmd: "zcp-devsup ctl status --json | grep -qE '\"status\": *\"running\"' && echo running", match: "^running"}
    - {service: appdev, cmd: "wget -qO- http://127.0.0.1:3000/api/forecast >/dev/null && echo serves", match: "^serves"}
    - {service: appdev, cmd: "grep -q 'wip: preseed uncommitted' /var/www/index.ts && test -f /var/www/wip-untracked.txt && echo wip-kept", match: "^wip-kept"}
  noFailedProcesses: true
  never: ["zerops_import{override=true}", "zerops_delete"]
---

My weather page is in the repository on appdev. Get it running for development on appdev and put it on appstage too, then tell me both addresses.
