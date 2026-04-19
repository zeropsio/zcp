---
id: bootstrap-close
priority: 8
phases: [bootstrap-active]
steps: [close]
title: "Close bootstrap — hand off to develop / cicd"
---

### Closing bootstrap

Bootstrap ends once every service is ACTIVE, every verification passed, and
every `ServiceMeta` has been written. At that point the project is
reachable, provable, and described on-disk — but no application code has
been written yet.

Next workflow options, in order of preference:

1. **`workflow="develop"`** — implement the user's application via the
   edit → deploy → verify loop, with strategy selection gated on every
   runtime service.
2. **`workflow="cicd"`** — generate a GitHub Action pipeline that runs
   `zcli push` on every remote push.
3. **Direct tools** — `zerops_scale`, `zerops_env`, `zerops_subdomain`
   are available without any workflow wrapper.

Close the bootstrap session explicitly by completing its final step; do not
leave it open. A closed bootstrap frees the PID for the next run.
