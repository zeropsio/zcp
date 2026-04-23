---
id: deploy-warnings-fresh-only
description: Two consecutive deploys — first intentionally emits a build warning, second is clean. Pins that the second deploy's buildLogs field contains NO content from the first deploy (stale-warning leakage would be an I-LOG-2 regression).
seed: imported
fixture: fixtures/laravel-minimal.yaml
preseedScript: preseed/deploy-warnings-fresh-only.sh
expect:
  # Two deploys = two zerops_deploy calls, minimum.
  mustCallTools:
    - zerops_workflow
    - zerops_deploy
    - zerops_verify
  workflowCallsMin: 1
  mustEnterWorkflow:
    - develop
  requiredPatterns:
    - '"workflow":"develop"'
  # The CRITICAL assertion: the stale warning text from the FIRST deploy must
  # NOT appear in the SECOND deploy's response. If tag-based scoping is
  # dropped (I-LOG-2 regression), the first deploy's zbuilder@<appVersionId>
  # entries leak into the second deploy's buildLogs because the build
  # service-stack persists across builds.
  forbiddenPatterns:
    - 'deployFiles.*not.*found.*dist'
    - 'stale.*warning'
  requireAssessment: true
  finalUrlStatus: 200
  finalUrlHostname: appdev
followUp:
  - "Proč první deploy vyhodil warning o 'dist' a druhý ne?"
  - "Kdyby se log backend dotazoval jen přes serviceStackId (bez tag filtru), co by agent viděl v result.buildLogs druhého deployu? Proč?"
  - "Jaký tag má každý build entry na build service-stacku a jak ho poznáš z eventu?"
---

# Úkol

Mám PHP-nginx hello-world aplikaci ve fixture. V `zerops.yaml` mám chybu:
`deployFiles` ukazuje na adresář `dist`, který build reálně nevytváří.
Zkus první deploy — vyhodí to warning o chybějící cestě. Pak to oprav
(`deployFiles: ./`) a deploynij znovu. Druhý deploy musí proběhnout čistě —
stará hláška z předchozího buildu se tam nesmí objevit.
