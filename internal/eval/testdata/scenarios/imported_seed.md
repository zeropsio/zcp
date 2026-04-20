---
id: test-imported
description: Imported seed scenario
seed: imported
fixture: fixtures/nodejs-only.yaml
expect:
  mustCallTools:
    - zerops_workflow
  workflowCallsMin: 10
  mustEnterWorkflow:
    - bootstrap
followUp:
  - "Did you detect existing services?"
---

# Task

Adopt existing app and add a database service.
