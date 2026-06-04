---
id: develop-checklist-simple-mode
priority: 3
phases: [develop-active]
modes: [simple]
runtimes: [dynamic, implicit-webserver]
environments: [container]
title: "Simple-mode checklist extras"
---

### Checklist (simple-mode services)

- A dynamic runtime needs a real `start:` command in its `zerops.yaml`
  entry — simple-mode services auto-start on deploy (no manual dev-server
  step like dev mode). `healthCheck` is **recommended** (a deterministic
  readiness probe) but **not required** — `run.start` keeps the service up
  and verify probes `GET /` regardless. (Implicit-webserver runtimes auto-run
  their server; no explicit `start:` needed.)
- There is no dev+stage pair; `{hostname}` is the single runtime container.
