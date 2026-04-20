---
id: test-empty
description: Basic empty seed scenario for parser tests
seed: empty
expect:
  mustCallTools:
    - zerops_workflow
    - zerops_import
  workflowCallsMin: 5
  mustEnterWorkflow:
    - bootstrap
  finalUrlStatus: 200
  forbiddenPatterns:
    - "<projectId>"
followUp:
  - "Why did you choose that approach?"
  - "What would you do differently?"
---

# Task

Create a simple web app with a database.
