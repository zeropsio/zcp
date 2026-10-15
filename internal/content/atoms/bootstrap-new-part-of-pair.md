---
id: bootstrap-new-part-of-pair
priority: 2
phases: [bootstrap-active]
routes: [classic]
steps: [discover]
modes: [standard]
deployStates: [deployed]
parts: [single]
title: "A new part of this repo is a stage of the pair, not a new service"
---

### A new part of the same repo

If the new piece (a worker, an api) is built from the repo of the pair
below, it is a second part of that app, never a second bootstrap:

{services-list:- `{hostname}`}

1. Add a `setup:` for the part to the repo's `zerops.yaml`.
2. Declare a named dev server for it in the same declaration.
3. Append its stage to the pair's stages.

Read `zerops_knowledge uri="zerops://playbooks/monorepo"` first. Bootstrap a
new service only for a different repo.
