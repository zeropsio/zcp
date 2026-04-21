---
id: develop-first-deploy-branch
description: Infra is provisioned (ServiceMeta complete) but nothing deployed yet — agent must detect never-deployed state and enter develop first-deploy branch, NOT bootstrap
seed: imported
fixture: fixtures/laravel-minimal.yaml
preseedScript: preseed/first-deploy-branch.sh
expect:
  mustCallTools:
    # zerops_discover intentionally NOT required: `status` + `develop`
    # start response already carry enough envelope signals (service
    # status, Bootstrapped/Deployed flags, SSHFS mount path) that an
    # efficient agent can skip the dedicated discover call. The
    # `workflow=develop` entry and the deploy+verify pair are the
    # load-bearing first-deploy branch assertions.
    - zerops_workflow
    - zerops_deploy
    - zerops_verify
  # 3 is the realistic floor for the first-deploy branch (status + start
  # develop + close-manually). Raising it blocks valid agent paths
  # (re-import with startWithoutCode) without catching a deeper failure —
  # the `"workflow":"develop"` required pattern + `"workflow":"bootstrap"`
  # forbidden pattern are the real signals we care about.
  workflowCallsMin: 3
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
