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

The service plan includes one or more static runtimes (nginx, static file
servers). Static containers do NOT need a verification server — they come
up serving an empty document root and the HTTP layer's readiness is proof
enough.

Before deploy, confirm with the user:

- the chosen runtime hostname (`appdev` is the standard convention)
- whether a stage pair is wanted (dev/stage pattern) or single-container
  dev mode
- deploy strategy for each runtime service

Static runtimes accept `deployFiles` that point at an empty directory; the
initial deploy will succeed without any real build artifacts.
