---
id: bootstrap-env-var-discovery
priority: 3
phases: [bootstrap-active]
routes: [classic, adopt]
steps: [provision]
title: "Discover env vars before generate"
---

### Discover env vars after provision, before generate

After import and before writing any zerops.yaml, discover the actual
env vars available on the project. This is the authoritative list —
do not guess alternative spellings. Unknown cross-service references
resolve to literal strings at runtime and fail silently.

```
zerops_discover includeEnvs=true
```

Record one row per service. Keys alone are enough — cross-service
references use `${hostname_varName}` syntax. Reference these in
`run.envVariables` of the app's zerops.yaml.

Common patterns by service type:

| Service type | Available env vars | Notes |
|-------------|-------------------|-------|
| PostgreSQL / MariaDB / MySQL | `connectionString`, `host`, `port`, `user`, `password`, `dbName` | `connectionString` preferred |
| Valkey / KeyDB / Redis | `host`, `port` | No password — private network, no auth |
| MongoDB | `connectionString`, `host`, `port`, `user`, `password` | `connectionString` preferred |
| RabbitMQ / NATS / Kafka | `connectionString`, `host`, `port`, `user`, `password` | AMQP/NATS URI |
| object-storage | `accessKeyId`, `secretAccessKey`, `apiUrl`, `region` | S3-compatible |

**Dev-container caveat**: env vars resolve at deploy time, not OS env
vars on a `startWithoutCode: true` container. A dev container that
never deployed has the env vars in the project catalogue but not on
`process.env`. Deploy first, then references fire.
