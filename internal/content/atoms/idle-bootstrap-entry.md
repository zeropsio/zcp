---
id: idle-bootstrap-entry
priority: 1
phases: [idle]
idleScenarios: [empty]
title: "Bootstrap entry"
---

This is an empty project. Bootstrap provisions the initial infrastructure. After the first bootstrap call returns the ranked routes, pick one and call `start` again with `route=...` to commit the session; a service plan is then proposed for you to approve before any services are created.

Bootstrap is the canonical entry point even when the user wants to USE an existing Zerops recipe (`nodejs-hello-world`, `laravel-minimal`, `nextjs-ssr-hello-world`, etc.) — describe their stack in `intent` and the recipe match surfaces as one of the ranked route options.

Keep the `intent` to one sentence — it scopes route ranking but doesn't constrain the plan.
