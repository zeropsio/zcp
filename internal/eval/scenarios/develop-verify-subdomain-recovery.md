---
id: develop-verify-subdomain-recovery
description: Adopted web-facing service where subdomain access is OFF (replicates session 2 grofovo broken state). Agent must call zerops_verify, see the Recovery field on http_root failure, follow it to zerops_subdomain action=enable, then re-verify — without going to zerops_browser first or wasting a redeploy.
seed: deployed
fixture: fixtures/standard-pair-no-subdomain.yaml
expect:
  mustCallTools:
    - zerops_workflow
    - zerops_discover
    - zerops_verify
    - zerops_subdomain
  workflowCallsMin: 4
  mustEnterWorkflow:
    - bootstrap
  requiredPatterns:
    - '"workflow":"bootstrap"'
    - '"route":"adopt"'
    # Verify response must carry the new structured Recovery field
    # pointing at zerops_subdomain action=enable for the failing
    # http_root check on appdev. These three patterns together prove the
    # agent saw the recovery hint AND executed it on the right service.
    - '"recovery":'
    - '"action":"enable"'
    - '"serviceHostname":"appdev"'
  forbiddenPatterns:
    # Anti-pattern from session 2: agent went straight to zerops_browser
    # against an unenabled subdomain → 502 from proxy → wrong diagnosis →
    # wasted deploy. The deploy specifically (self-deploy on appdev) is
    # the strongest detectable wrong path.
    - '"sourceService":"appdev","targetService":"appdev"'
  requireAssessment: true
followUp:
  - "Co konkrétně `zerops_verify` vrátil pro `appdev` a jaký byl shape `recovery` pole na `http_root` checku?"
  - "Proč jsi nešel rovnou na `zerops_browser`? Co by se stalo kdyby jsi ho zavolal před `zerops_subdomain action=enable`?"
  - "Co znamená `degraded` aggregate status v `zerops_verify` odpovědi a jak souvisí s auto-close gating?"
---

# Úkol

V projektu už existuje statická služba `appdev` (type: static) — služba
běží, ale nemá ZCP metadata.

Tvůj úkol:

1. Adoptuj existující službu (bootstrap → route=adopt).
2. Ověř že `appdev` je dostupná v prohlížeči přes její subdoménu — chceme
   tu URL otevřít a vidět defaultní static page.
3. Pokud něco brání tomu aby URL fungovala, oprav to a znovu ověř.

Pravidla:

- Začni `zerops_workflow action="status"` — zjisti stav.
- **Nepouštěj `zerops_browser` jako první.** Verify musí proběhnout
  předtím — pokud nějaký check vrátí pole `recovery`, exekutuj přesně to
  (zavolej `tool` s těmi `args`), pak teprve znovu verify a teprve potom
  případně browser.
- Nedělej self-deploy na appdev "abys to opravil" — kontroluj nejdřív co
  verify reportuje. Tady není deploy odpověď.
