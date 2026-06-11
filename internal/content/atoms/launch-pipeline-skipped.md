---
id: launch-pipeline-skipped
priority: 4
phases: [launch-production-active]
title: "Pipeline configuration skipped"
references-fields: []
---

### Pipeline configuration skipped

`skipPipelineSetup=true` told ZCP not to check or recommend pipeline integration. The production project is live — but its runtimes are EMPTY (startWithoutCode) and stay empty until something delivers a build: with the pipeline skipped, NOTHING deploys the application, including the first time. Options:

- **Manual `zcli push`** from local or CI per release (`zcli login <prod-scoped token>` + `zcli push --service-id <prod service ID> --setup <setup>`).
- **Add integration later** in Zerops dashboard (`Project → Service → Source code → Connect to GitHub/GitLab`). Set the event type to `Tag`, the tag regex to `^v\d+\.\d+\.\d+$` (or your release-version convention), and the Zerops YAML setup to `prod`.

Re-run `workflow="launch-production"` with the same `launchKey` if you want ZCP to verify integration setup; that lifts the skip and runs the configuring-pipeline check.
