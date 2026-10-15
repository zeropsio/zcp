---
id: bootstrap-adopt-new-part
priority: 3
phases: [bootstrap-active]
routes: [adopt]
steps: [discover]
title: "Adopting a pair and adding a part: the part is a stage of the pair"
---

### A new part next to the adopted pair

If the task also asks for another piece (a worker, an api) built from the
repo of the pair you adopt, it is a second part of that app. Adopt the pair
alone — the plan lists no new service for the part:

1. Adopt the pair as it is.
2. In develop, add a `setup:` for the part to the repo's `zerops.yaml`.
3. Declare a named dev server for it in the same declaration.
4. Append its stage to the pair's stages.

Read `zerops_knowledge uri="zerops://playbooks/monorepo"` first. Create a new
service only for a different repo.
