---
id: launch-pipeline-configuring
priority: 3
phases: [launch-production-active]
title: "Checking pipeline integration for ongoing CD"
references-fields: []
---

### Checking pipeline integration for ongoing CD

ZCP is verifying whether each runtime service in the new production project has a CD pipeline integration configured. This reads-only — ZCP never mutates pipeline config (Path B trust model, see `docs/spec-launch-production-platform-spike.md §B.3`).

Possible outcomes:

- **Configured** → ongoing builds will fire on the integration's trigger (tag-push for prod-recommended setup).
- **Not configured** → response will carry a `pipeline-not-configured-<hostname>` blocker with a Zerops dashboard deep-link and the recommended config payload (`repositoryFullName`, `eventType=TAG`, `tagRegex`, `zeropsYamlSetup=prod`). User configures via dashboard, then re-calls `workflow="launch-production"` to recheck — no need to re-send the launch token, ZCP reads the staged secret server-side.
