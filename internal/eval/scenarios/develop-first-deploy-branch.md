---
id: develop-first-deploy-branch
description: Infra is provisioned (ServiceMeta complete) but nothing deployed yet — agent must detect never-deployed state and enter develop first-deploy branch, NOT bootstrap
seed: imported
fixture: fixtures/laravel-minimal.yaml
preseedScript: preseed/first-deploy-branch.sh
expect:
  mustCallTools:
    - zerops_workflow
    - zerops_discover
    - zerops_deploy
    - zerops_verify
  workflowCallsMin: 4
  # Develop is the right entry point — bootstrap is already complete.
  # The agent starting bootstrap when meta.IsComplete() would be a bug.
  mustEnterWorkflow:
    - develop
  requiredPatterns:
    - '"workflow":"develop"'
  # Starting a bootstrap session on a bootstrapped project is the primary
  # anti-pattern this scenario guards against.
  forbiddenPatterns:
    - '"workflow":"bootstrap","intent"'
  requireAssessment: true
  finalUrlStatus: 200
  finalUrlHostname: appdev
followUp:
  - "Z čeho jsi poznal, že služba `appdev` je bootstrapped ale nikdy nedeployed? Kde přesně v odpovědích tool volání se ta informace objevila?"
  - "Proč jsi nešel do bootstrap flow? Jak by se to chovalo kdybys ho zavolal?"
  - "Co konkrétně první deploy stampne na ServiceMeta a proč to má vliv na další develop session?"
---

# Úkol

Chci mít na tý php-nginx službě jednoduchý hello-world endpoint, ideálně
s krátkým PHP fragmentem co ukáže aktuální čas.
