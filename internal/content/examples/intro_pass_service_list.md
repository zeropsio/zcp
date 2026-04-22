---
surface: intro
verdict: pass
reason: concrete-action-ok
title: "Root README intro — service list + tier shortcuts"
---

> # NestJS Showcase Recipe
>
> A [NestJS](https://nestjs.com) application connected to
> [PostgreSQL](https://www.postgresql.org/), [Valkey](https://valkey.io/)
> (Redis-compatible), [NATS](https://nats.io/), S3-compatible object
> storage, and [Meilisearch](https://www.meilisearch.com/) running on
> [Zerops](https://zerops.io) with six ready-made environment
> configurations — from AI agent and remote development to stage and
> highly-available production.
>
> ⬇️ **Full recipe page and deploy with one-click**
>
> [![Deploy on Zerops](https://.../deploy-button.svg)](https://app.zerops.io/recipes/nestjs-showcase?environment=small-production)
>
> - **AI Agent** [[info]](/0%20—%20AI%20Agent) — [[deploy]](https://app.zerops.io/recipes/nestjs-showcase?environment=ai-agent)
> - **Remote (CDE)** [[info]](/1%20—%20Remote%20(CDE)) — [[deploy]](...)
> - **Local** [[info]](/2%20—%20Local) — [[deploy]](...)
> - **Stage** [[info]](/3%20—%20Stage) — [[deploy]](...)
> - **Small Production** [[info]](/4%20—%20Small%20Production) — [[deploy]](...)
> - **Highly-available Production** [[info]](/5%20—%20Highly-available%20Production) — [[deploy]](...)

**Why this passes the root-README test.**
- One-sentence intro names every managed service the recipe provisions.
- Six tiers listed with `[[info]]` and `[[deploy]]` shortcuts so a
  reader can pick one in 30 seconds.
- Deploy button above the fold — the primary CTA.
- No debugging narratives, no code details, no gotchas (each routes to
  its own surface).

Spec §2 test: *"Can a reader decide in 30 seconds whether this recipe
deploys what they need, and pick the right tier?"* — yes; service list
answers "what it deploys," the six tier links answer "pick the right
tier."
