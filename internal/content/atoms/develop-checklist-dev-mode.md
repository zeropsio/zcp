---
id: develop-checklist-dev-mode
priority: 3
phases: [develop-active]
modes: [dev]
runtimes: [dynamic]
environments: [container]
title: "Dev-mode checklist extras (dynamic runtimes)"
references-fields: [workflow.ServiceSnapshot.Mode, workflow.ServiceSnapshot.RuntimeClass]
---

### Checklist (dev-mode dynamic-runtime services)

Applies to **dynamic runtimes only** (Node, Bun, Deno, Go, Rust, Python,
Ruby, Java, .NET — anything with a long-running app process under
manual control). For implicit-webserver runtimes (`php-apache`,
`php-nginx`) see `develop-implicit-webserver`; for static runtimes the
web server auto-starts and this checklist does not apply.

- Dev setup block in `zerops.yaml`: `start: zsc noop --silent`, **no**
  `healthCheck`. Zerops keeps the runtime container idle; you start
  the dev process yourself via `zerops_dev_server action=start` after
  each deploy.
- Stage setup block (if a dev+stage pair exists): real `start:`
  command **plus** a `healthCheck`. Stage auto-starts on deploy and
  Zerops probes it on its configured interval.
