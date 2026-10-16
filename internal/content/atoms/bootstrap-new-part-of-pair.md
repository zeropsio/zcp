---
id: bootstrap-new-part-of-pair
priority: 2
phases: [bootstrap-active]
routes: [classic]
steps: [discover]
modes: [standard]
parts: [single]
title: "A new part of this repo is a stage of the pair, not a new service"
---

### A new part of the same repo

If the new piece (a worker, an api) is built from the repo of the pair
below, it is a second part of that app, never a second bootstrap:

{services-list:- `{hostname}`}

1. Import the part's stage service (`zerops_import`, `startWithoutCode: true`)
   under its own hostname, such as `workerstage`.
2. Plan the pair as the one runtime it is: its dev hostname with
   `isExisting: true` and `stages` listing every stage with its setup, the
   new one included (`{"hostname": "workerstage", "setupName": "worker"}`).
   Never a second runtime for the part.
3. In develop, add the part's `setup:` to the repo's `zerops.yaml`, then in
   the same step give the part its own entry in `.zcp/dev-servers.json`
   (autostart, unless the dev service's RAM is short) and run
   `zerops_dev_server action=start hostname="<dev>" name=<part>`. A worker without a port is
   still a named server. Every part on the dev service runs as one.

Read `zerops_knowledge uri="zerops://playbooks/monorepo"` first. Bootstrap a
new runtime only for a different repo.
