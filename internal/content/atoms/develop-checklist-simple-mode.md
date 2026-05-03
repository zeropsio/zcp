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

- The entry in `zerops.yaml` must have a real `start:` command **and** a
  `healthCheck` — simple services auto-start and are probed on deploy.
- There is no dev+stage pair; `{hostname}` is the single runtime container.
