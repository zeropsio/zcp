---
id: bootstrap-runtime-classes
priority: 3
phases: [bootstrap-active]
routes: [classic]
steps: [discover]
title: "Runtime classes — pick the right stack"
---

### Runtime classes

Each runtime type falls into one of four classes that shape deploy and
lifecycle behaviour:

- **Dynamic** (nodejs, go, python, bun, ruby, …) — dev setup starts with
  `zsc noop`; the real dev process starts via `zerops_dev_server`
  (container) or via your harness background task primitive (local) after
  each deploy. Stage setup uses a real `run.start` + `healthCheck` so the
  platform auto-starts it.
- **Static** (nginx, static) — auto-start after deploy, no manual step.
- **Implicit-webserver** (php-apache, php-nginx) — auto-start; set
  `documentRoot` in `zerops.yaml` and omit `run.start`.
- **Managed** (postgresql, mariadb, redis/valkey, keydb, rabbitmq, nats,
  object storage) — no deploy; scale and connect only.

Pick runtime types from the live Zerops catalog (check `zerops_knowledge`
for current versions). Managed services initialize first (`priority: 10`
in import YAML) so runtimes that depend on them can connect at start.
