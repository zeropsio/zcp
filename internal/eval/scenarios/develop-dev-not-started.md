---
id: develop-dev-not-started
description: Seeded runtime sits at READY_TO_DEPLOY without ZCP meta — LLM must adopt first, then either deploy code or re-import with startWithoutCode
seed: imported
fixture: fixtures/laravel-minimal.yaml
expect:
  mustCallTools:
    - zerops_workflow
    - zerops_discover
  workflowCallsMin: 5
  mustEnterWorkflow:
    - bootstrap
    - develop
followUp:
  - "V jakém stavu byla služba `appdev` při startu? Jak jsi to zjistil?"
  - "Jakou recovery cestu jsi zvolil (deploy existujícího kódu / re-import s startWithoutCode)? Proč?"
  - "Proč SSHFS mount a SSH nefunguje, dokud služba není ACTIVE?"
---

# Úkol

V projektu je naimportovaná jediná Laravel služba `appdev` — vytvořená
mimo ZCP (žádná metadata). Služba **nikdy nebyla deployed** a sedí ve
stavu READY_TO_DEPLOY (import ji vytvořil bez `startWithoutCode: true`).

Tvým úkolem je:

1. **Adoptovat** existující služby do ZCP (bootstrap/adopt route) —
   jinak develop flow nemá kontext.
2. Dostat `appdev` do stavu ACTIVE. Můžeš to udělat jakkoli validně:
   - **Nasadit reálný kód** (první deploy z READY_TO_DEPLOY → BUILDING → ACTIVE).
   - **Re-importovat službu** s `startWithoutCode: true` (platforma sama
     vystartuje prázdný kontejner, služba přejde do ACTIVE).

Pravidla:

- Začni `zerops_workflow action="status"` — nezačínej naslepo.
- Po adopci pokračuj v develop flow.
- Na konci ověř, že `appdev` je ACTIVE.

Verify: `zerops_discover service="appdev"` vrací status ACTIVE a HTTP
subdomain endpoint odpovídá (200 nebo 404 stačí — hlavní je, že server
běží).
