---
id: develop-first-deploy-branch
description: Infra is provisioned (envelope shows bootstrapped=true) but nothing deployed yet (deployed=false) — agent must detect never-deployed state and enter develop first-deploy branch, NOT bootstrap
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
  # 1 is the realistic floor for the first-deploy branch on a single-
  # service preseed: one `zerops_workflow workflow="develop"` start is
  # enough — re-import + write + deploy + verify use other tools, and
  # auto-close fires without a manual close call when scope is covered.
  # Phase 1.5 calibration (B14) — the previous floor of 3 blocked valid
  # paths and didn't catch any deeper failure since
  # `"workflow":"develop"` + the bootstrap-forbidden pattern are the
  # load-bearing signals.
  workflowCallsMin: 1
  # Develop is the right entry point — bootstrap is already complete.
  # The agent starting bootstrap when meta.IsComplete() would be a bug.
  mustEnterWorkflow:
    - develop
  requiredPatterns:
    - '"workflow":"develop"'
    # scope is REQUIRED for develop start (cb63bf3). `appdev` is the single
    # runtime hostname in this fixture.
    - '"scope":['
    - '"appdev"'
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
  - "Co se v envelope změní po úspěšném prvním deployi + passing verify (který token v Services bloku se překlopí) a proč to má vliv na další develop session?"
---

# Úkol

Chci mít na tý php-nginx službě jednoduchý hello-world endpoint, ideálně
s krátkým PHP fragmentem co ukáže aktuální čas.
