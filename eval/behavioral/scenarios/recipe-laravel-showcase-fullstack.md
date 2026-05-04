---
id: recipe-laravel-showcase-fullstack
description: |
  Greenfield Laravel showcase via recipe route — bigger sibling of
  recipe-laravel-minimal-standard. Showcase recipe pulls Postgres +
  Valkey + S3 object storage + Meilisearch + queue worker (multi-
  service recipe). Tests the recipe-route plan submission for a
  multi-service template and stresses showcase recipe content
  (APP_KEY base64 trap, config:cache vs initCommands, predis over
  phpredis, AWS path-style for MinIO). Counterpart to laravel-minimal:
  same route, much larger content surface.
seed: empty
tags: [bootstrap, recipe-route, standard-pair, laravel, postgres, valkey, s3, meilisearch, queue, recipe-fullstack]
area: bootstrap
retrospective:
  promptStyle: briefing-future-agent
userPersona: |
  You are setting up a Laravel app that needs the full toolkit:
  database, cache, object storage, full-text search, and a queue
  worker. You want a dev environment plus a staging slot. Compatible
  managed-catalog substitutions are fine. You expect the agent to
  warn you about Laravel-specific gotchas (APP_KEY format, .env
  shadowing) before they bite. Push back if it drops services from
  your asked toolkit silently or proposes HA tier.
notableFriction:
  - id: showcase-recipe-content-quality
    description: |
      Showcase recipe Gotchas section lists 6+ traps (APP_KEY
      base64 prefix, config:cache in initCommands, .env shadowing,
      predis vs phpredis, MinIO path-style, vite manifest on dev).
      Surfaces whether the recipe content telegraphs these to the
      agent BEFORE deploys fail.
  - id: multi-service-recipe-import
    description: |
      Recipe import yaml carries 5+ service entries (runtime + db +
      cache + s3 + search + worker). Surfaces whether the agent
      handles a many-service import yaml (strip project block,
      validate before submit).
---

Set up a Laravel app on Zerops with the full toolkit: Postgres, a Redis-compatible cache, S3-style object storage, full-text search, and a queue worker. I want a dev environment to iterate on plus a staging slot.
