---
id: recipe-nestjs-minimal-standard
description: |
  Greenfield NestJS via recipe route — Node-stack contrast to the
  laravel-minimal recipe scenario. Standard pair (dev/stage). Tests
  whether route-pick atom telegraphs recipe-confidence threshold for
  Node frameworks and whether nestjs-minimal recipe content covers
  the typical first-deploy traps (TypeORM `synchronize`, listen on
  0.0.0.0, ts-node devDependencies).
seed: empty
tags: [bootstrap, recipe-route, standard-pair, node, nestjs, postgres, recipe-match]
area: bootstrap
retrospective:
  promptStyle: briefing-future-agent
userPersona: |
  You are a developer building a small NestJS API and you want a dev
  environment plus a staging slot. Compatible substitutions in the
  managed catalog (e.g. valkey for redis) are fine — accept them and
  ask the agent to mention what was substituted in the final summary.
  Trust the agent's reasoning when it has a clear basis. Push back if
  it proposes HA tier you didn't ask for or skips the staging slot.
notableFriction:
  - id: recipe-confidence-node
    description: |
      Discover step returns nestjs-minimal recipe and a classic Node
      fallback. Agent should pick the recipe when "NestJS" is named
      explicitly. Surfaces whether the recipe-confidence atom matches
      the Node ecosystem the same way it does for PHP frameworks.
  - id: nestjs-recipe-content
    description: |
      Recipe gotchas (listen 0.0.0.0, TypeORM synchronize:false,
      ts-node in devDeps) should reach the agent before the trap
      surfaces in a deploy. Surfaces recipe-content quality.
---

Set up a NestJS API on Zerops. I want a dev environment to iterate against and a staging slot for build validation. Postgres is fine as the database.
