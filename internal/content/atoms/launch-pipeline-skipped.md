---
id: launch-pipeline-skipped
priority: 4
phases: [launch-production-active]
title: "Pipeline configuration skipped"
references-fields: []
---

### Pipeline configuration skipped

`skipPipelineSetup=true` told ZCP not to check or recommend pipeline integration. The production project is live; the first deploy ran from source HEAD via `buildFromGit`.

Without an integration, subsequent code changes do NOT auto-build. Options:

- **Manual `zcli push`** from local or CI per release.
- **Add integration later** in Zerops dashboard (`Project → Service → Source code → Connect to GitHub/GitLab`). Set the event type to `Tag`, the tag regex to `^v\d+\.\d+\.\d+$` (or your release-version convention), and the Zerops YAML setup to `prod`.

Re-run `workflow="launch-production"` with the same `launchKey` if you want ZCP to verify integration setup; that lifts the skip and runs the configuring-pipeline check.
