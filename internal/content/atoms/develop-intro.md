---
id: develop-intro
priority: 0
phases: [develop-active]
deployStates: [deployed]
title: "Develop & deploy intro"
---

### Development & Deploy

Infrastructure is provisioned and at least one runtime already has a
successful first deploy on record. You're in the edit loop: discover
the current state, implement the user's request, redeploy, verify.

This atom only fires for the edit-loop branch. If a runtime in this
project is bootstrapped but `deployed: false`, the first-deploy branch
atoms (priority 1–5 under `deployStates: [never-deployed]`) take over
instead — running the full scaffold → write → first deploy → stamp
sequence before normal iteration begins.
