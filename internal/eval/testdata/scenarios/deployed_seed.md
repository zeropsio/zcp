---
id: test-deployed
description: Deployed seed scenario
seed: deployed
fixture: fixtures/laravel-minimal.yaml
expect:
  mustCallTools:
    - zerops_workflow
  workflowCallsMin: 3
  mustEnterWorkflow:
    - develop
followUp:
  - "Did you enter develop workflow?"
---

# Task

Add a new endpoint to the existing app.
