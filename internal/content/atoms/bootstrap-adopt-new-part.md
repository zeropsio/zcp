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
repo of the pair you adopt, it is a second part of that app, never a second
runtime of the plan:

1. Import the part's stage service (`zerops_import`, `startWithoutCode: true`)
   under its own hostname, such as `workerstage`.
2. Plan the pair as the one runtime it is: its dev hostname with
   `isExisting: true` and `stages` listing every stage with its setup, the
   new one included (`{"hostname": "workerstage", "setupName": "worker"}`).
   Never a second runtime for the part.
   An existing setup and its stage keep their names; only the new part gets
   a new setup. A stage that deploys `setup: prod` stays
   `{"hostname": "appstage", "setupName": "prod"}` — never rename a `setup:`
   the repo already has.
3. In develop, add the part's `setup:` to the repo's `zerops.yaml`, then in
   the same step give the part its own entry in `.zcp/dev-servers.json`
   (autostart, unless the dev service's RAM is short) and run
   `zerops_dev_server action=start hostname="<dev>" name=<part>`. A worker without a port is
   still a named server. Every part on the dev service runs as one.

Read `zerops_knowledge uri="zerops://playbooks/monorepo"` first. Plan a new
runtime only for a different repo.
