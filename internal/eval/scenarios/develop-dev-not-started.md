---
id: develop-dev-not-started
description: Dev service sits at READY_TO_DEPLOY after import — LLM must recognize and recover (deploy OR re-import with startWithoutCode)
seed: imported
fixture: fixtures/laravel-minimal.yaml
expect:
  mustCallTools:
    - zerops_workflow
    - zerops_discover
  workflowCallsMin: 5
  mustEnterWorkflow:
    - develop
  forbiddenPatterns:
    - '"workflow":"bootstrap"'
followUp:
  - "V jakém stavu byla služba `appdev`, když jsi začal? Jak jsi to zjistil?"
  - "Jakou cestu jsi zvolil (deploy existujícího kódu / re-import s startWithoutCode)? Proč?"
  - "Proč SSHFS mount a SSH nefunguje, dokud služba není ACTIVE?"
---

# Úkol

V projektu je naimportovaná Laravel služba `appdev` + `db`. Služba `appdev`
ale **nikdy nebyla deployed** — sedí ve stavu READY_TO_DEPLOY (import
YAML ji vytvořil bez `startWithoutCode: true` a první deploy ještě
neproběhl).

Tvým úkolem je dostat `appdev` do stavu ACTIVE a potvrdit, že běží.
Můžeš to udělat jakýmkoliv validním způsobem:

- **Nasadit reálný kód** (první deploy z READY_TO_DEPLOY → BUILDING → ACTIVE).
- **Re-importovat službu** s `startWithoutCode: true` (platforma sama
  vystartuje prázdný kontejner a služba přejde do ACTIVE).

Pravidla:

- Jdi přes develop workflow (ne bootstrap — služby už existují).
- Nejdřív zjisti stav služeb (`zerops_discover`) — nezačínej naslepo.
- Po provedení verify, že `appdev` je skutečně ACTIVE.

Verify: `zerops_discover service="appdev"` vrací status ACTIVE a HTTP
subdomain endpoint odpovídá (200 nebo 404 stačí — hlavní je, že server
běží).
