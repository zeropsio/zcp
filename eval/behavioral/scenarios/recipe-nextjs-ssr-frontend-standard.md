---
id: recipe-nextjs-ssr-frontend-standard
description: |
  Greenfield Next.js SSR via recipe route — frontend-only, no separate
  API service. Standard pair (dev/stage). Tests recipe selection when
  the runtime can be SSR or static (recipe must pick SSR by user
  intent), plus standalone-mode build artefacts (deployFiles for
  .next/standalone + .next/static + public). Stresses the
  develop-first-deploy verify path on a dev runtime that boots from
  build artefact.
seed: empty
tags: [bootstrap, recipe-route, standard-pair, frontend, ssr, nextjs, postgres, build-artifact]
area: bootstrap
retrospective:
  promptStyle: briefing-future-agent
userPersona: |
  You are a developer building a Next.js 15 app with server-side
  rendering. You want a dev environment plus a staging slot. The app
  needs Postgres for a small data layer; managed-catalog substitutions
  (e.g. mariadb for mysql) are fine if needed. You prefer the SSR path
  over static export — you are explicit about that in your prompt.
  Trust the agent's choices when reasoning is given; push back if it
  proposes a fully static build or skips the dev/stage pairing.
notableFriction:
  - id: ssr-vs-static-pick
    description: |
      Recipe matcher returns both nextjs-ssr-hello-world and
      nextjs-static-hello-world. Agent must pick SSR per user intent.
      Surfaces whether the route-pick atom resolves intra-framework
      ambiguity.
  - id: standalone-deploy-files
    description: |
      Standalone mode requires three deploy-file paths
      (.next/standalone, .next/static, public). Surfaces whether the
      recipe content lists them and whether the develop-first-deploy
      atom validates the deploy path before SSR boots.
---

I want to deploy a Next.js 15 app with SSR on Zerops. Set up a dev environment to iterate plus a staging slot. The app needs a small Postgres database.
