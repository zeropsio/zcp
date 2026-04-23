---
id: develop-ambiguous-state
description: Terse "deploy my app" prompt against a multi-signal project — bootstrapped standard pair, never deployed, scaffolded zerops.yaml and code already on mount. Agent must read envelope + mount + zerops.yaml and pick the right next step.
seed: imported
fixture: fixtures/laravel-minimal.yaml
preseedScript: preseed/ambiguous-state.sh
expect:
  mustCallTools:
    - zerops_workflow
    - zerops_discover
    - zerops_deploy
  workflowCallsMin: 3
  mustEnterWorkflow:
    - develop
  requiredPatterns:
    # Develop is the only correct entry — services are bootstrapped so
    # no bootstrap call should appear.
    - '"workflow":"develop"'
    # scope is REQUIRED for develop start (cb63bf3). The preseed plants a
    # standard pair (appdev + appstage) — auto-close needs BOTH halves
    # deployed+verified, so both must be in scope.
    - '"scope":['
    - '"appdev"'
    - '"appstage"'
  forbiddenPatterns:
    # Anti-pattern: agent throws away pre-seeded zerops.yaml + app code
    # and rewrites from scratch. The existing code is intentional and
    # the agent must read it first.
    - "Write a new zerops.yaml"
    # Anti-pattern: treats bootstrapped standard pair as unadopted.
    - '"route":"adopt"'
  requireAssessment: true
  finalUrlStatus: 200
  finalUrlHostname: appdev
followUp:
  - "Kolik signálů o stavu projektu jsi zkombinoval, než ses rozhodl co dělat? (status phase, discover, mount obsah, zerops.yaml na mountu, FirstDeployedAt na metas …)"
  - "Proč jsi šel do develop flow a ne do bootstrap? Kdyby ServiceMeta neměla `BootstrappedAt`, volba by byla jiná?"
  - "V jakém pořadí jsi deployoval dev a stage? Proč tohle pořadí (a ne opačně)?"
---

# Úkol

Deploy mojí weather app.
