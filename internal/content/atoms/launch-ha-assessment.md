---
id: launch-ha-assessment
priority: 3
phases: [launch-production-active]
title: "Launch — assess whether the app is ready to run on multiple containers"
references-fields: []
references-atoms: [launch-scope-prompt]
---

### Assess whether the app is ready to run on multiple containers

Production defaults every promoted runtime to a 2-container floor (`minContainers: 2`) — requests round-robin across containers, each with its OWN filesystem and memory. An app that worked on one dev container can break at scale 2 in ways the build never shows. BEFORE settling `runtimeScaling`, walk this checklist against the source code — each row is a grep-able question, not a guess:

| Check | Question to answer from the source | Failure at scale ≥ 2 |
|---|---|---|
| In-memory state | Are sessions, rate-limit counters, caches, or job queues held in process memory (e.g. default express-session MemoryStore, Laravel `SESSION_DRIVER=file`)? | A request lands on container B and the session from container A doesn't exist — random logouts, lost carts. Move state to the managed db/redis dep. |
| Local-disk writes | Does code write uploads, SQLite files, or generated assets to its own disk paths? | Each container has its own disk — files exist on one container and 404 on the other. Use object storage or a shared-storage mount. |
| Migrations on boot | Do schema migrations run on every container start (init commands, ORM sync-on-boot)? | Two containers boot in parallel and race the migration — duplicate-column / lock errors. Migrations must be idempotent or run once per deploy, not per container. |
| Scheduled / queue work | Do in-process cron jobs or queue consumers run inside the web app? | Every container runs its own copy — emails sent twice, jobs double-processed. Move to a single-container worker service or use a locking scheme. |
| Realtime connections | Do WebSocket / SSE clients broadcast through in-process state? | Clients connected to container A never see events published on container B. Route pub/sub through the redis/valkey dep. |

**The decision belongs to the user, framed by load:** ask what traffic production expects. Two outcomes:

- App passes the checklist (stateless, externalized state) → keep the 2-container default; raise `maxContainers` for headroom under load.
- App fails a row and fixing it now is out of scope → a consented single container is a legitimate launch: pass `runtimeScaling={"<hostname>":{"minContainers":1,"maxContainers":1}}` — it surfaces as a warning, not a block — and record the failed row as the follow-up work before scaling up.

Don't silently pick either path. The checklist result + the user's load answer ARE the consent conversation; per-runtime counts land in the `ready-to-launch` preview (`containers` field) for the final confirm.
