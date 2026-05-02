---
id: bootstrap-runtime-classes
priority: 3
phases: [bootstrap-active]
routes: [classic]
steps: [discover]
title: "Runtime classes — pick the right stack"
---

### Runtime classes

Each runtime type falls into one of four classes — pick the right class for each runtime in the plan:

- **Dynamic** (nodejs, go, python, bun, ruby, …) — needs an explicit dev-server lifecycle in develop (container: `zerops_dev_server`; local: harness background task).
- **Static** (nginx, static) — serves files from `deployFiles`; platform auto-starts after deploy.
- **Implicit-webserver** (php-apache, php-nginx) — webserver is part of the runtime; platform auto-starts after deploy.
- **Managed** (postgresql, mariadb, redis/valkey, keydb, rabbitmq, nats, object storage) — no deploy; scale and connect only.

Pick runtime types from the live Zerops catalog (check `zerops_knowledge` for current versions). Managed services initialize first (`priority: 10` in import YAML) so runtimes that depend on them can connect at start.

Lifecycle and `zerops.yaml` mechanics for each class (start commands, healthCheck, deployFiles, dev-server primitives) are delivered by the develop response at first-deploy time.
