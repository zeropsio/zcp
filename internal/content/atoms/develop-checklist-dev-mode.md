---
id: develop-checklist-dev-mode
priority: 3
phases: [develop-active]
modes: [dev]
environments: [container]
title: "Dev-mode checklist extras (container)"
---

### Checklist (dev-mode services)

- Dev entry in `zerops.yaml`: `start: zsc noop --silent`, **no** `healthCheck`
  (agent owns the dev process and starts it via `zerops_dev_server`
  after each deploy).
- Stage entry (if a dev+stage pair exists): real `start:` command **plus**
  a `healthCheck` — stage auto-starts and is probed by the platform.
