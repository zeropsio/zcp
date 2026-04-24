---
id: bootstrap-recipe-collision-rename
description: Project already has `appdev` + `appstage` (Python) at the canonical recipe hostnames. User asks for a .NET upload service — the recipe matches but collides on BOTH runtime services. Agent must rename runtime targets (devHostname + stageHostname) in the plan so ZCP rewrites the recipe YAML with non-colliding hostnames at provision (F6 rename-in-plan contract).
seed: deployed
fixture: fixtures/runtime-hostname-collision.yaml
expect:
  mustCallTools:
    - zerops_workflow
    - zerops_discover
  workflowCallsMin: 5
  mustEnterWorkflow:
    - bootstrap
  requiredPatterns:
    # Discovery surfaced the collision annotation — prerequisite for the
    # rename-in-plan flow. Without this signal the agent has no pre-flight
    # information about what hostnames conflict.
    - '"collisions":'
    - 'appdev'
  forbiddenPatterns:
    # F6 contract: rename-in-plan prevents the platform error. The agent
    # picks non-colliding devHostname + stageHostname in the plan; ZCP
    # rewrites the import YAML accordingly. Seeing serviceStackNameUnavailable
    # means the agent submitted the recipe verbatim anyway — the atom
    # guidance didn't land.
    - 'serviceStackNameUnavailable'
    - 'hostname already in use'
    # Classic-route fallback signals the agent gave up on the recipe
    # instead of exercising the rename path. That's a regression on
    # the F6 atom guidance — classic is valid when the user needs
    # genuinely different infrastructure, but for a pure hostname
    # collision the recipe is still the right path.
    - '"route":"classic"'
  requireAssessment: true
  finalUrlStatus: 200
followUp:
  - "Kolik hostname kolizí jsi viděl v discovery `collisions:` anotaci a na jakých jménech?"
  - "Jaký `devHostname` a `stageHostname` jsi zvolil v planu a proč ty konkrétní?"
  - "Kdybys v planu nechal původní `appdev`/`appstage`, co by se stalo při submitu — který check by to zachytil?"
---

# Úkol

V projektu běží Python weather app (`appdev` + `appstage`). Potřebuju
k tomu přidat samostatnou .NET službu pro nahrávání souborů do S3 — pro
firemní upload fronty. Spolehlivost je priorita (production-shape), s
object-storage backendem.

Výsledek na veřejné subdomeně, `/health` endpoint vrací 200 včetně
kontroly připojení k S3.
