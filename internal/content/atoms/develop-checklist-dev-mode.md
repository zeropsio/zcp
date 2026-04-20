---
id: develop-checklist-dev-mode
priority: 3
phases: [develop-active]
modes: [dev]
title: "Dev-mode checklist extras"
---

### Checklist (dev-mode services)

- Dev entry in `zerops.yaml`: `start: zsc noop --silent`, **no** `healthCheck`
  (agent starts the server over SSH).
- Stage entry (if a dev+stage pair exists): real `start:` command **plus**
  a `healthCheck` — stage auto-starts and is probed by the platform.
