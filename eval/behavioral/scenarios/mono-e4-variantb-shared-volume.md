---
id: mono-e4-variantb-shared-volume
description: |
  F2 under variant B (mono-e0 TEST-PLAN §4 E4 rows 3 and 6, docs/spec-mate.md
  §2.8a): a Bun web + worker and a Go api in one repository, planted on
  webdev. The parts need two runtimes, so the repository lives on the
  local-storage volume `vol` that both dev services mount at /var/www/ws
  (the fixture's dev setups declare run.volume). The agent records the pair
  with its code volume (plan codeVolume: hostname vol, mountPath
  /var/www/ws, devServices apidev/dev-api), deploys each dev service with
  its own dev setup, fills the volume once, and gets each part's dev server
  running on its own service — web and worker on webdev, api on apidev.

  The containerChecks prove the outcome independently: one repository on
  the volume seen by both services, each service's own dev server
  answering, and an edit through the volume reaching both.
seed: imported
fixture: fixtures/mono-bun-go-worker-imported.yaml
preseedScript: preseed/plant-mono-bun-go-worker.sh
tags: [monorepo, variant-b, code-volume, dev-server, bun, go]
area: develop
excludeFromAll: true
retrospective:
  promptStyle: briefing-future-agent
verification:
  mode: required
  spec: spec-mate.md §2.8a
  expectedServices:
    - hostname: webdev
      status: [ACTIVE]
      type: bun@*
    - hostname: apidev
      status: [ACTIVE]
      type: go@*
    - hostname: vol
      type: local-storage*
  containerCheck:
    - {service: webdev, cmd: "git -C /var/www/ws rev-parse --verify HEAD >/dev/null && echo volume-holds-repo", match: "^volume-holds-repo"}
    - {service: apidev, cmd: "test -f /var/www/ws/apps/api/main.go && echo apidev-sees-volume", match: "^apidev-sees-volume"}
    - {service: webdev, cmd: "wget -qO- http://127.0.0.1:3000/health >/dev/null && echo web-up", match: "^web-up"}
    - {service: apidev, cmd: "curl -fsS http://127.0.0.1:8080/health >/dev/null && echo api-up", match: "^api-up"}
    - {service: webdev, cmd: "date +%s > /var/www/ws/.probe && echo written", match: "^written"}
    - {service: apidev, cmd: "test -s /var/www/ws/.probe && echo shared", match: "^shared"}
  noFailedProcesses: true
  never: ["zerops_import{override=true}", "zerops_delete"]
---

This repo has a Bun web app, a Bun worker that downloads the data, and an API in Go. The Go part and the Bun parts need their own runtimes, so keep the code once on the shared volume `vol` and run web and worker on webdev, the API on apidev. Get all three running for development and tell me where to open the web and the API.
