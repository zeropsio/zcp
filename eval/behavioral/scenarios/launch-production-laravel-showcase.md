---
id: launch-production-laravel-showcase
description: |
  Full-stack Laravel project — php-nginx dev/stage pair + postgres +
  valkey + object-storage. Tests the launch-production composer
  against a realistic showcase shape and reproduces four round-3
  findings against the legacy composer:

    - F19 (HIGH): classify-prompt leaks platform-reserved CDN URL
      keys (`storageCdnUrl` etc.) that the bundle composer then
      emits under `project.envVariables`, triggering
      `projectEnvUseOfSystemKey` rejection.
    - F20 (CRITICAL): launch composer emits `mode:` for the
      object-storage entry, triggering `storage.mode: mode not
      supported` rejection on every Laravel-style stack.
    - F21 (HIGH): neither composer maps source `quotaGBytes` →
      bundle `objectStorageSize`; mutation rejected with
      `storage.objectStorageSize: required` after F20 is fixed.
    - F25 (LOW): keepNonHA AskUserQuestion offers all 4 managed deps
      as opt-out candidates; object-storage is included despite
      having no HA/NON_HA mode at all.

  This is the load-bearing regression scenario for round-3 backlog:
  if launch survives this end-to-end the bundle composer is
  schema-correct against the full managed-dep surface.
seed: deployed
fixture: fixtures/laravel-showcase-deployed.yaml
tags: [launch-production, prod-transition, one-shot-key, separate-project, trust-boundary, object-storage, full-stack, regression]
area: launch
retrospective:
  promptStyle: briefing-future-agent
userPersona: |
  You run a Laravel app with the typical full stack on Zerops: a
  php-nginx dev/stage pair, postgres, valkey for sessions and cache,
  and object-storage for user uploads. The dev/stage pair works. You
  want production live on a separate Zerops project mirroring this
  shape.

  You will NOT hand ZCP a standing production-scoped API key. When
  ZCP asks, you generate a temporary account-wide Zerops API token,
  use it for the launch window only, and DELETE it as soon as ZCP
  reports launched. Push back if ZCP tries to persist the key
  anywhere or asks for ongoing prod access.
notableFriction:
  - id: trust-boundary-clarity
    description: |
      ZCP must explicitly tell the user a one-shot key is needed and
      explicitly tell them to delete it after launch. Surfaces
      whether the launched response embeds the delete-key step OR
      buries it in checklist content the agent doesn't surface.
  - id: object-storage-mode-rejection
    description: |
      The composer should NOT emit `mode:` for the object-storage
      entry — platform rejects any mode value. Surfaces F20: agent
      receives `projectImportInvalidParameter: storage.mode: mode
      not supported`, with no obvious unblock path because user
      input never touched `mode` on storage.
  - id: object-storage-size-required
    description: |
      After F20 is fixed (or via export-fallback), the composer must
      emit `objectStorageSize:` derived from source `quotaGBytes`.
      Surfaces F21: agent receives `projectImportMissingParameter:
      storage.objectStorageSize: required`.
  - id: cdn-env-classification
    description: |
      Source project may expose `storageCdnUrl` / `staticCdnUrl` /
      `apiCdnUrl` envs from the object-storage entry. Bundle composer
      must drop these (platform re-injects on new project) — F19
      surfaces them as user-classifiable, then propagates the
      classification, triggering `projectEnvUseOfSystemKey` rejection.
  - id: keepNonHA-object-storage
    description: |
      AskUserQuestion for keepNonHA should EXCLUDE entries with
      no HA/NON_HA mode (object-storage). F25: storage appears as a
      4th choice alongside db/cache, implying a scaling-mode lever
      that doesn't exist for storage.
  - id: source-control-mutation-phase
    description: |
      Before the import API call, the launch needs `setup: prod` in
      the source repo's zerops.yaml. Surfaces whether the agent
      drives the write+commit+push step (launch-write-prod-setup
      atom) instead of trying to publish without it.
  - id: stage-as-promotion-headline
    description: |
      Standard pair: `appstage` is the validated last-known-good
      copy, `appdev` is the iteration half. sourceContext should
      surface `appstage` as the headline runtime with `devHostname:
      appdev` disclosed. Agent should accept either half but the
      handler normalizes to the canonical dev-half key internally.
---

Dev and stage work great — I want to launch this Laravel app to production. Full stack: the php-nginx app, postgres, valkey cache, object storage. Create a separate Zerops project called `myapp-prod` in eu-central. I'll generate a one-shot Zerops API key when you ask, and I'll delete it right after launch. Manage prod through the dashboard from then on, not through this ZCP session.
