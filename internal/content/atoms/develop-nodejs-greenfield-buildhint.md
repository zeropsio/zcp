---
id: develop-nodejs-greenfield-buildhint
priority: 2
phases: [develop-active]
runtimeBases: [nodejs]
deployStates: [never-deployed]
multiService: aggregate
title: "Node.js greenfield — use npm install, not npm ci"
references-fields: [workflow.ServiceSnapshot.TypeVersion]
---

### Node.js — `npm install`, not `npm ci`

Fresh Node scaffold with no committed `package-lock.json`: `npm install` in `build.buildCommands`. `npm ci` fails with `EUSAGE` until a lockfile is committed.
