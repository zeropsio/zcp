---
id: bootstrap-classic-plan-static
priority: 2
phases: [bootstrap-active]
routes: [classic]
runtimes: [static]
steps: [discover]
title: "Classic bootstrap — static runtime plan"
---

### Static runtime plan

Static containers (nginx) come up serving an empty document root — no
verification server needed.

Before deploy, confirm with the user:

- the chosen runtime hostname (`appdev` is the standard convention)
- whether a stage pair is wanted (dev/stage pattern) or single-container
  dev mode
- deploy strategy for each runtime service

Static runtimes accept `deployFiles` that point at an empty directory; the
initial deploy will succeed without any real build artifacts.
