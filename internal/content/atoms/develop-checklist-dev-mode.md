---
id: develop-checklist-dev-mode
priority: 3
phases: [develop-active]
modes: [dev]
title: "Dev-mode checklist extras"
---

### Checklist (dev-mode services)

- Dev entry in `zerops.yaml`: `start: zsc noop --silent`, **no** `healthCheck`.
  The agent is expected to start the real server over SSH; Zerops will not
  auto-start it and will not probe a health endpoint.
- Stage entry (if a dev+stage pair exists): real `start:` command **plus**
  a `healthCheck` — stage containers are expected to auto-start and be
  probed by the platform.
