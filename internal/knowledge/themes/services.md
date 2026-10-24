# Managed Service Reference

Reference cards for all 14 Zerops managed services. Each card provides type, ports, env vars, wiring templates, connection pattern, HA specifics, and gotchas.

## Deployment Variant + Scaling Profile

**HA vs single node is encoded in the service `type`**, not a separate field: write `<service>:single` (one node) or `<service>:ha` (HA cluster). Each card below lists its base name; in import.yaml always write the `:single`/`:ha` variant. The variant is immutable after creation.

**PostgreSQL and Valkey also take a `profile`** (autoscaling tier + tuning preset). **Recommend the conservative tier; escalate only on a clear signal.** Dev → PostgreSQL `oltp-hobby` (single only) / Valkey `hobby`. Production → PostgreSQL `oltp-staging` / Valkey `staging`. Move up to `oltp-production` / `oltp-enterprise` (PostgreSQL) or `production` (Valkey) only when the workload clearly needs it (high or critical load, large throughput). Always set `profile` explicitly — omitting it applies a default (PostgreSQL single → `oltp-staging`, HA → `oltp-production` = dedicated CPU + high minima; Valkey → `staging`), which over-provisions HA. No other managed type takes a profile; scale those with `verticalAutoscaling`.

**Legacy form** you may still see in older hand-written YAML or external examples: `type: <service>` + a separate `mode:` field. `mode: NON_HA` ≡ `:single`, `mode: HA` ≡ `:ha`. Still accepted (remapped to the variant) but `mode` is deprecated and ignored by validation. Always author new YAML as the variant. Discover surfaces a service's HA-ness through the type variant, not a `mode:` field.

## Wiring Syntax

- **Hostname substitution**: In templates below, each service uses a sample hostname (e.g., `db`, `cache`, `search`). Replace it with your actual service hostname. The syntax `${hostname_varname}` is real Zerops cross-service reference syntax — `hostname` must match the target service hostname exactly. Service hostnames are lowercase alphanumeric only (`[a-z0-9]`, no dashes or underscores), so the hostname segment maps verbatim.
- **Reference**: `${hostname_variablename}` — the hostname segment is the literal service hostname (`[a-z0-9]`)
- **Vault values** (the project's Shared vault, a service's own — `project.vault` / a service's `vault` in import.yaml, `zerops_env`, or the GUI) reach the app only through a `run.envVariables` line: `${KEY}` reads the service's own value, else the Shared one. Name the line what the app reads and the value something else — `KEY: ${KEY}` reaches the app as the literal `${KEY}` until Zerops resolves same-name references (`STRIPE_SECRET_KEY: ${STRIPE_SECRET}`). A changed value reaches a running process at its restart.
- **import.yaml values**: `vault:` at project and service level, each item `KEY: value` or `KEY: {value: …, sensitive: true}`. `envSecrets`, `dotEnvSecrets` and `envVariables` are deprecated — never write them. Put in a service's `vault` only generated secrets (`<@generateRandomString(...)>`) and real credentials.
- **Hostname = DNS**: use hostname directly for host (`db`, NOT `${db_hostname}`), but use `${db_port}` for port
- **Internal**: ALWAYS `http://` — NEVER `https://` (SSL at L7 balancer)
- **Shared vars**: referenced like any vault value — each service that needs one lists it in its `run.envVariables` under a different name
- **Password sync**: changing DB password in GUI does NOT update env vars (manual sync)

**Wire credentials in zerops.yaml `run.envVariables`** — Managed services auto-generate credentials but they are NOT automatically available to runtime services. Wire them via `run.envVariables` in zerops.yaml (the deploy-time config), like every value the app reads. Use the service's import.yaml `vault` ONLY for generated secrets like `<@generateRandomString(...)>`, and reference those too:

```yaml
# zerops.yaml — wire cross-service references here
zerops:
  - setup: myapp
    run:
      envVariables:
        DB_HOST: mydb
        DB_PORT: ${mydb_port}
        DB_NAME: ${mydb_dbName}
        DB_USER: ${mydb_user}
        DB_PASSWORD: ${mydb_password}
        APP_SECRET: ${APP_SECRET_KEY}
```
```yaml
#zeropsPreprocessor=on
# import.yaml — only generated secrets here
services:
  - hostname: mydb
    type: mariadb:single@{version}
    priority: 10

  - hostname: myapp
    type: ubuntu/nodejs@22
    vault:
      APP_SECRET_KEY: {value: <@generateRandomString(<32>)>, sensitive: true}
```

Without zerops.yaml wiring, the runtime service has no way to connect to managed services.

## Service Wiring Templates

Below, **VARS** = config values, **SECRETS** = credentials. Wire ALL cross-service references (both VARS and SECRETS) in zerops.yaml `run.envVariables`. Use a service's import.yaml `vault` ONLY for generated secrets (`<@generateRandomString(...)>`) and real credentials that must exist before first deploy. Never write the deprecated `envSecrets` or `envVariables` there (a service-level `envVariables` still creates the service's values, but every one plain — a secret there is stored readable). Replace sample hostnames (`db`, `cache`, etc.) with your actual service hostname.

## PostgreSQL
**Type**: `postgresql:single` / `postgresql:ha` (check live stacks for versions), immutable
**Profile**: dev → `oltp-hobby` (single only); production → `oltp-staging`; escalate to `oltp-production` / `oltp-enterprise` only on a clear signal. Omitting applies the default (single → `oltp-staging`, HA → `oltp-production` = dedicated CPU). Also `olap-production`, `writeheavy-production`, `custom` (+ `profileOverrides`)
**Ports**: 5432 (RW), 5433 (read replicas, HA only), 6432 (external TLS via pgBouncer)
**Env**: `hostname`, `port`, `portTls`, `user`, `password`, `connectionString`, `connectionTlsString`, `dbName`, `superUser`, `superUserPassword` | HA-only: `portReplicas`, `connectionStringReplicas`
**HA**: 1 primary + 2 read replicas, streaming replication (async), auto-failover
**Gotchas**: No internal TLS (only 6432). Don't modify `zps` user. Read replicas have async lag. Some libs need `postgres://` scheme. Min RAM 0.25 GB (platform default is 0.125 — don't set `verticalAutoscaling.minRam` below 0.25). Installing extensions needs the `superUser`/`superUserPassword` (user `postgres`): `CREATE EXTENSION IF NOT EXISTS <ext>;` (e.g. `pg_stat_statements`, `vector`, `postgis`) in the SERVICE database (not the default `postgres` db), then **restart the service** — the extension's stats stay unavailable until restart.
**Wiring** (sample hostname: `db`):
**VARS**: `DB_HOST: db` `DB_PORT: ${db_port}` `DB_NAME: ${db_dbName}`
**SECRETS**: `DATABASE_URL: postgresql://${db_user}:${db_password}@db:${db_port}/${db_dbName}`

## MariaDB
**Type**: `mariadb:single` / `mariadb:ha` (check live stacks for versions), immutable. No profile — scale with `verticalAutoscaling`
**Ports**: 3306 (fixed, no separate replica port)
**Env**: `hostname`, `port`, `projectId`, `serviceId`, `user`, `password`, `connectionString`, `dbName`
**HA**: MaxScale routing, read/write splitting, async replication, auto-failover
**Gotchas**: No separate replica port (MaxScale routes on single port). No internal TLS. Don't modify `zps` user. Min RAM 0.25 GB (platform default is 0.125 — don't set `verticalAutoscaling.minRam` below 0.25).
**Wiring** (sample hostname: `db`):
**VARS**: `DB_HOST: db` `DB_PORT: ${db_port}` `DB_NAME: ${db_dbName}`
**SECRETS**: `DATABASE_URL: mysql://${db_user}:${db_password}@db:${db_port}/${db_dbName}`

## Valkey
**Type**: `valkey:single` / `valkey:ha` (v7.2 only — v8 passes validation but fails import; check live stacks for the version), immutable
**Profile**: dev → `hobby`; production → `staging`; escalate to `production` only on a clear signal. Omitting applies `staging`
**Use for**: **cache + sessions ONLY**. Do NOT use Valkey as a queue broker for Zerops showcases — the canonical queue broker is NATS (see `nats` below and `choose-queue` decision). Using Valkey for queues is a legacy polymorphism pattern (one service wearing three hats); the showcase tier separates concerns explicitly. Exception: Laravel Horizon, Rails Sidekiq, Django+Celery-with-Redis — frameworks with a first-class Redis-bound queue library can keep their queue on Valkey, BUT the showcase still provisions a NATS broker as a separate `queue` service for the messaging feature section on the dashboard.
**Replaces**: the removed **KeyDB** — Valkey is the drop-in redis-family cache. When a user mentions "KeyDB", "Redis", or "cache", use Valkey; migrating an existing KeyDB service changes only the hostname.
**Ports**: 6379 (RW), 6380 (RW TLS), 7000 (RO, HA only), 7001 (RO TLS, HA only)
**Env**: `hostname`, `port`, `password`, `connectionString`, `connectionTlsString`, `portTls` — AUTH REQUIRED (default user + `password`; there is no separate `user` key)
**HA**: 1 master + 2 replicas. Zerops-specific: ports 6379/6380 on replicas forward to master (NOT native Valkey). Async replication.
**Gotchas**: MUST NOT use v8 (passes validation but fails import — check live stacks for valid versions). **Auth REQUIRED** — `${cache_password}` exists and is mandatory; connecting without it throws `NOAUTH Authentication required`. There is no separate `${cache_user}` (Valkey uses the default user + password). Prefer `${cache_connectionString}` (full auth'd URL) or supply `${cache_password}` explicitly. Port forwarding is Zerops-specific. Use 7000/7001 for direct read scaling. TLS ports for external/VPN only.
**Wiring** (sample hostname: `cache`):
**VARS**: `REDIS_URL: ${cache_connectionString}` (full auth'd URL) — or split: `REDIS_HOST: cache` + `REDIS_PORT: ${cache_port}` + `REDIS_PASSWORD: ${cache_password}`

## Elasticsearch
**Type**: `elasticsearch:single` / `elasticsearch:ha` (check live stacks for versions), immutable. No profile — scale with `verticalAutoscaling`
**Ports**: 9200 (HTTP only, no native transport)
**Env**: `hostname`, `port`, `password` (user always `elastic`)
**HA**: Multi-node cluster, automatic repair
**Config**: `PLUGINS` (comma-separated, restart required), `HEAP_PERCENT` (default 50%, range 1-100)
**Gotchas**: HTTP only internally. Min RAM 0.25 GB. Default user `elastic`. Tune `HEAP_PERCENT=75` for search-heavy.
**Wiring** (sample hostname: `search`):
**VARS**: `ES_HOST: search` `ES_PORT: ${search_port}`
**SECRETS**: `ES_PASSWORD: ${search_password}`
**CONN**: `http://search:${search_port}` with `Authorization: Basic elastic:${search_password}`

## Object Storage
**Type**: `object-storage` or `objectstorage` (no version, no `:ha`/`:single` variant — runs on independent infra)
**Env**: `apiUrl`, `apiHost`, `accessKeyId`, `secretAccessKey`, `bucketName`, `quotaGBytes`, `projectId`, `serviceId`, `hostname`
**Config**: `objectStorageSize: 1-100` GB, `objectStoragePolicy` or `objectStorageRawPolicy`, `priority: 10`
**Infrastructure**: runs on **independent infra** separate from other project services -- accessible from any Zerops service or remotely over internet
**Bucket**: one auto-created per service (name = hostname + random prefix, **immutable**). Need multiple buckets? Create multiple object-storage services
**Policies**: `private` | `public-read` (list+get) | `public-objects-read` (get only, no listing) | `public-write` (put only) | `public-read-write` (full). Or use `objectStorageRawPolicy` with IAM Policy JSON (`{{ .BucketName }}` template variable available)
**Endpoint — use `apiUrl` (carries the full `https://` scheme), not `apiHost`**: `${storage_apiUrl}` resolves to a complete `https://...` URL ready to hand to any S3 SDK's `endpoint` option. `${storage_apiHost}` is host-only (no scheme) — if you manually prepend `http://` to it you get an endpoint the object-storage gateway rejects and 301-redirects to the `https://` equivalent, which most S3 SDKs don't follow automatically (symptom: `UnknownError` / connection-refused on the first bucket call). Either pass `${storage_apiUrl}` directly, or if the S3 client library you're using needs host-only as a separate field, combine `https://${storage_apiHost}` — never `http://`.
**Gotchas**: MinIO backend. **No Zerops backup**. `forcePathStyle: true` / `AWS_USE_PATH_STYLE_ENDPOINT: true` REQUIRED. Region `us-east-1` REQUIRED but NOT auto-generated — every S3 SDK needs it, MinIO ignores the value. No autoscaling, no verticalAutoscaling. Quota changeable in GUI after creation
**Wiring** (sample hostname: `storage`):
**VARS**: `S3_ENDPOINT: ${storage_apiUrl}` `S3_BUCKET: ${storage_bucketName}` + a region env var set to `us-east-1` (use whichever name the framework's S3 client reads — NOT auto-generated by platform)
**SECRETS**: `S3_KEY: ${storage_accessKeyId}` `S3_SECRET: ${storage_secretAccessKey}`
**REQUIRED**: `forcePathStyle: true` / `AWS_USE_PATH_STYLE_ENDPOINT: true` (MinIO backend)

## Local Storage
**Type**: `local-storage:single@1` — single-only, there is NO `:ha` variant and no `mode:`. No profile — capacity is the max disk in `verticalAutoscaling`, billed as the service's disk usage
**Mount**: zerops.yaml `run.volume` on each runtime that needs it: `volume: {hostname: <storage>, mountPath: /path, readOnly: false}` (`mountPath` defaults to `/mnt/{hostname}`). One volume per runtime service; several services may mount the same volume. Mounted in RUNTIME containers only — not during build, not in `run.prepareCommands`. Survives deploys: new containers get the same volume reattached
**The POSIX guarantee**: single-kernel locking and `mmap` are correct here — this is the ONLY Zerops storage where SQLite, Prometheus and other embedded/file-based databases are safe. Single-writer still applies: keep writes in one service and mount `readOnly: true` everywhere else
**Gotchas**: the volume lives on ONE physical machine and **every container of every connected service is co-located on it** — horizontal scaling is capped by that machine's capacity. Not HA today: on hardware failure or maintenance Zerops moves the volume and every connected service takes a short outage (stop, move, start), and recovery is not guaranteed — keep backups. Daily `.tar.gz` backups of the live volume are on by default, but they can capture a database mid-write: for SQLite add a `run.crontab` job (`allContainers: false`) writing a consistent copy onto the volume (`sqlite3 <db> ".backup <path-on-volume>"`) and let the archive pick that copy up
**Wiring**: No env vars. The storage service's own container carries the volume at `/data` with SSH / web-shell access for direct inspection

## SeaweedFS
**Type**: `seaweedfs:single@3.85` / `seaweedfs:ha@3.85`, variant immutable (`shared-storage:single` / `shared-storage:ha` are accepted aliases of the same service). No profile — scale with `verticalAutoscaling`
**Mount**: mounting is the RUNTIME's job, not the platform's — add a `run.startCommands` entry `sudo zsc shared-storage mount {storageHostname}` (mounts the filer at `/mnt/{storageHostname}`, stays in the foreground, restarted by Zerops if it exits). **There is no config-file mount field at all**: import-level `mount:` was retired with Shared Storage and is now REJECTED (`yamlMountDeprecated`), and a `zerops.yaml` `run.mount` is silently STRIPPED (it even passes yaml validation — validation-passing ≠ honored — but produces no mount). Full mount recipe incl. init-command and raw `weed mount` forms: `zerops_knowledge uri="zerops://guides/seaweedfs-integration"`
**HA**: 1:1 replication, auto-failover
**Gotchas**: SeaweedFS backend. Max 60 GB data regardless of disk. POSIX only (not S3), and per-mount-only locking — databases (SQLite, Prometheus) are UNSAFE here, they belong on `local-storage`. `:single` = data loss on hardware failure. The mount exists only in running containers — not during build, not in `run.prepareCommands`
**Wiring**: Env vars `hostname` + `port` (8888), no credentials — the filer is unauthenticated and reachable only on the project network / over VPN, never via `enableSubdomainAccess`. Filer HTTP `http://{storageHostname}.zerops:8888` works without any mount
**Disambiguation**: `zerops_mount` (SSHFS dev tool) is a completely different feature -- it mounts the service `/var/www` locally for development, not shared storage.

## Kafka
**Type**: `kafka:single` / `kafka:ha` (check live stacks for versions), immutable. No profile — scale with `verticalAutoscaling`
**Ports**: 9092 (SASL PLAIN)
**Env**: `hostname`, `port`, `user`, `password`
**HA**: 3 brokers, 6 partitions, replication factor 3, auto-repair
**Gotchas**: SASL PLAIN only (no anonymous). `:single` = 1 broker, **no replication**. Indefinite topic retention (implement cleanup). 250 GB cap.
**Wiring** (sample hostname: `kafka`):
**VARS**: `KAFKA_BROKERS: kafka:9092`
**SECRETS**: `KAFKA_USER: ${kafka_user}` `KAFKA_PASSWORD: ${kafka_password}`
**CONN**: `security.protocol=SASL_PLAINTEXT`, `sasl.mechanism=PLAIN` (no anonymous)

## NATS
**Type**: `nats:single` / `nats:ha` (check live stacks for versions), immutable. No profile — scale with `verticalAutoscaling`
**Use for**: **messaging / queue broker for every showcase recipe**. NATS is the canonical queue service for Zerops showcases — the `queue` target in the showcase service list. It is a dedicated broker, NOT a generic KV store or cache substitute. Workers subscribe to subjects; dashboards publish test messages. The NATS connection is framework-agnostic, which is why it's a better default than a language-bound queue library.
**Replaces**: the removed **RabbitMQ** — when a user mentions "RabbitMQ", "AMQP", or "message queue", use NATS (switch the AMQP client to a NATS client; JetStream for durable messaging). A Redis-backed queue on **Valkey** is the alternative ONLY for frameworks with a first-class Redis queue library (Laravel Horizon, Rails Sidekiq, Django+Celery).

**Two distinct messaging shapes — pick ONE per recipe and document only that one in yaml comments / KB:**
- **Core pub/sub + queue groups** — `nc.subscribe('subject', { queue: 'workers' })`. Fire-and-forget delivery; queue groups load-balance across replicas; nothing is persisted server-side. HA story: cluster nodes preserve pub/sub liveness on node loss; **there is no stream state to replicate** because no streams exist. Use this when "fan-out + load balance + at-most-once redelivery" is enough — most showcase recipes.
- **JetStream streams + durable consumers** — `js = jetstream(nc); await jsm.streams.add({ name, subjects }); js.subscribe('subject', { config: { durable_name: '...' } })`. Persistent message store on top of NATS; durable consumers replay on reconnect; ack/redeliver semantics. HA story: cluster replicates stream state across nodes, surviving node loss without losing acked-but-unprocessed messages. Use this when at-least-once + replay + persistence are required.

**Authoring rule**: when a recipe uses ONLY core pub/sub (no `jetstream(nc)` call, no `JetStreamManager`, no streams created), the import-yaml + KB content must NOT invoke JetStream framing — there's no stream to discuss, and "JetStream replication" claims become misleading at HA tiers. When a recipe uses JetStream, the JetStream framing is the relevant HA story and core pub/sub is irrelevant. Match yaml/KB language to actual code, not to the union of NATS features.

**Canonical hostname**: `queue` (literal) — keeps env var references readable: `${queue_hostname}`, `${queue_port}`, `${queue_user}`, `${queue_password}`. Do not name it `nats` in the showcase target list even though the type is `nats:single@2.12`.
**Ports**: 4222 (client), 8222 (HTTP monitoring — `/healthz` health endpoint lives here)
**Env**: `hostname`, `user` (always `zerops`), `password`, `connectionString`
**Config**: `JET_STREAM_ENABLED` (default 1; the platform enables JetStream by capability — recipes opt in by writing JetStream client code), `MAX_PAYLOAD` (default 8 MB, max 64 MB)
**Storage** (JetStream): Memory store up to 40 GB, File store up to 250 GB.
**Gotchas**: Config changes require restart. JetStream HA sync lag 1 min applies only to recipes that actually open streams. Set `JET_STREAM_ENABLED=0` only if you want to hard-disable the capability across the project.
**Wiring** (sample hostname: `queue`) — **two supported patterns, pick ONE**:

**Pattern A — separate env vars** (recommended; works with every NATS client library):
```yaml
envVariables:
  NATS_HOST: ${queue_hostname}
  NATS_PORT: ${queue_port}
  NATS_USER: ${queue_user}
  NATS_PASS: ${queue_password}
```
Client code passes host+port as the servers list and user+pass as connect options:
```js
import { connect } from "nats";
const nc = await connect({
  servers: `${process.env.NATS_HOST}:${process.env.NATS_PORT}`,
  user: process.env.NATS_USER,
  pass: process.env.NATS_PASS,
});
```

**Pattern B — opaque connection string** (compact; the platform builds a correctly-formatted URL):
```yaml
envVariables:
  NATS_URL: ${queue_connectionString}
```
Client code passes it directly:
```js
const nc = await connect({ servers: process.env.NATS_URL });
```

**Gotcha — authorization violation from manual URL composition**: do NOT hand-compose a URL like `nats://${queue_user}:${queue_password}@${queue_hostname}:${queue_port}`. Most NATS client libraries will parse the embedded credentials AND separately attempt SASL with the same values, producing a double-auth attempt the NATS server rejects with `Authorization Violation` on the first CONNECT frame. The symptom is a startup crash with `NatsError: 'Authorization Violation'` and no successful subscription. Use Pattern A or Pattern B above — they both avoid the double-auth path. Pattern A by passing credentials as connect options with a credential-free URL, Pattern B by letting the platform-generated `connectionString` carry exactly the auth shape the NATS server expects.

## Meilisearch
**Type**: `meilisearch:single` (single node only — no HA variant; check live stacks for versions), immutable. No profile — scale with `verticalAutoscaling`
**Ports**: 7700
**Env**: `hostname`, `masterKey`, `defaultSearchKey`, `defaultAdminKey`
**Gotchas**: **No HA** (single-node only). Never expose `masterKey` to frontend -- use `defaultSearchKey`.
**Wiring** (sample hostname: `search`):
**VARS**: `MEILI_HOST: http://search:7700`
**SECRETS**: `MEILI_MASTER_KEY: ${search_masterKey}`

## ClickHouse
**Type**: `clickhouse:single` / `clickhouse:ha` (check live stacks for versions), immutable. No profile — scale with `verticalAutoscaling`
**Ports**: 9000 (native), 8123 (HTTP), 9004 (MySQL compat), 9005 (PostgreSQL compat)
**Env**: `hostname`, `port`, `portHttp`, `portMysql`, `portPostgresql`, `portNative`, `password`, `superUserPassword`, `dbName`
**HA**: 3 nodes, replication factor 3, cluster `zerops`
**Gotchas**: HA requires `ReplicatedMergeTree` (standard `MergeTree` won't replicate). Super user for backups.
**Wiring** (sample hostname: `ch`):
**VARS**: `CH_HOST: ch` `CH_DB: ${ch_dbName}`
**SECRETS**: `CH_PASSWORD: ${ch_password}` `CH_SUPER_PASSWORD: ${ch_superUserPassword}`
**CONN**: Native `clickhouse://ch:${ch_port}`, HTTP `http://ch:${ch_portHttp}`, MySQL `ch:9004`, PostgreSQL `ch:9005`

## Qdrant
**Type**: `qdrant:single` / `qdrant:ha` (check live stacks for versions), immutable. No profile — scale with `verticalAutoscaling`
**Ports**: 6333 (HTTP), 6334 (gRPC)
**Env**: `hostname`, `port`, `grpcPort`, `apiKey`, `readOnlyApiKey`, `connectionString`, `grpcConnectionString`
**HA**: 3 nodes, `automaticClusterReplication=true` by default
**Gotchas**: No public access (internal only). Use 6334 for gRPC (higher perf for large vectors).
**Wiring** (sample hostname: `qdrant`):
**VARS**: `QDRANT_HOST: qdrant` `QDRANT_PORT: ${qdrant_port}` `QDRANT_GRPC: ${qdrant_grpcPort}`
**SECRETS**: `QDRANT_API_KEY: ${qdrant_apiKey}`
**CONN**: HTTP `http://qdrant:${qdrant_port}` with `api-key` header, gRPC `qdrant:${qdrant_grpcPort}`

## Typesense
**Type**: `typesense:single` / `typesense:ha` (check live stacks for versions), immutable. No profile — scale with `verticalAutoscaling`
**Ports**: 8108
**Env**: `hostname`, `port`, `apiKey` (immutable master key)
**HA**: 3-node Raft consensus, auto leader election, recovery up to 1 min
**Gotchas**: API key immutable. 1-min recovery -- expect brief 503s. CORS always on.
**Wiring** (sample hostname: `typesense`):
**VARS**: `TYPESENSE_HOST: typesense` `TYPESENSE_PORT: ${typesense_port}`
**SECRETS**: `TYPESENSE_API_KEY: ${typesense_apiKey}`
**CONN**: `http://typesense:${typesense_port}` with `x-typesense-api-key` header

## Pools and Box

Read this when an app needs a Swarm pool or a Zerops Box. Either is infrastructure the app uses — never the app's own runtime, and neither is a managed database. Code the app's users send runs in a Box sandbox, never in the app's container: *Running code the app's users send* below.

### Swarm pool

**Type**: `swarm@1` (also `swarm-vm@1`). A pool is a service of up to 10 containers that its orchestrator hands out.
**Import**: `minContainers` / `maxContainers` between 0 and 10 (a higher maximum is capped at 10), plus `verticalAutoscaling`. Import a pool with `minContainers: 0`: containers the platform creates for the minimum have no primary snapshot and cannot be reset. Pre-warm containers through the orchestrator instead — `POST /container` with `{"count": N}`.
**Image**: comes from the app's zerops.yml setup, deployed with `zcli push`; `run.base` is the language base (e.g. `python@3.12`). A deploy reaches only the containers created after it.
**Orchestrator**: answers at `http://orch.<pool>.zerops`. An app service authenticates with the env reference `${<pool>_API_TOKEN}`; a call without it gets `401`.
**Tokens**: the pool's `API_TOKEN` (the bearer of every orchestrator call) and `ADMIN_TOKEN` (sent in the `X-Swarm-Admin-Token` header, only to take a container from its holder) are generated with the pool. Both are secrets: an app wires `API_TOKEN` by reference in its `run.envVariables` — `POOL_TOKEN: ${<pool>_API_TOKEN}` — and never copies the value into code, a file or another variable.
**Acquire**: `POST /container/acquire` with `start`, `clean`, `leaseTtlSeconds` (`0` = the lease never expires). The lease it returns must accompany every later call for that container — as the `X-Swarm-Lease` header or the `lease` query parameter.
**Release**: `release` frees the container; `reset=true` also restores the container's primary snapshot. A lease that expires frees its container silently, unreset. A container the platform created for `minContainers` has no primary snapshot: `reset=true` is refused (`400`, `containerAction`, reason `no-primary-snapshot`, not retryable) and the reservation stays; `DELETE /container/{id}` with its lease removes it (`202`, gone in about 7 s), and the pool creates fresh containers, with a snapshot, on later acquires.
**Exec**: `POST /container/{id}/exec` — the output streams as NDJSON, one command at a time.
**Clean**: a `clean` acquire resets only a container marked dirty, and writes over SSH never mark it: a container released without `reset` (or freed by an expiring lease) is handed out with the previous holder's files in `/var/www`, `/home/zerops`, `/tmp` and `/opt`. A reset restores the image: everything `run.prepareCommands` wrote survives, `/var/www` included; what was written or started since is gone, and `run.start` does not run again — `initCommands` do.
**Removals**: container removals run one at a time.
**Full pool**: when every container that can serve an acquire is leased and the pool holds its maximum, an acquire (or `POST /container/run`) is refused at once with reason `all-reserved` — nothing is queued and nothing created; it succeeds only after a release. A container freed with `reset=true` is handed out again at once; an empty pool creates one in about 10 s, and simultaneous acquires create one container at a time.
**Lowering the maximum**: setting `maxContainers` below the containers the pool holds deletes the highest-numbered ones within about 30 s, leased ones included, with their `/var/www`; their leases then answer `container-not-found`.
**Stop**: a stopped pool's orchestrator does not answer. A restart, or a stop and start, keeps every container's id, number, hostname and lease, its `/var/www` and `/home/zerops`; processes and `/tmp` are lost.
**Env**: env variables set on the pool service reach its containers, including leased ones for processes started after the change; the pool's own `API_TOKEN` does not.
**Isolation**: never set `envIsolation: none` on a pool — every service of the project would get its `ADMIN_TOKEN` and `API_TOKEN`. A service that needs the pool's token references it under a different name (`POOL_TOKEN: ${<pool>_API_TOKEN}`). A variable named `<pool>_API_TOKEN` holding `${<pool>_API_TOKEN}` stays the literal text, and while it exists every other reference to that name on the service does too.
**Addressing**: each container answers on its own hostname `node-id-<n>.runtime.<pool>.zerops`, from anywhere in the project, on every port it listens on; `run.ports` governs only the public subdomain. SSH into pool containers follows the pool's `sshIsolation`, set as a service env variable. A per-port subdomain routes to the containers listening on that port.

### A part's dev pool

A part's dev environment may be a pool: each branch gets one leased container (`zerops_lease`), and `zerops_dev_server`, `zerops_deploy` and `zerops_verify` take the lease alias `<pool>-<number>` as the hostname.

**Import**:

```yaml
services:
  - hostname: devpool
    type: swarm@1
    minContainers: 0
    maxContainers: <1-10>
    verticalAutoscaling:
      minRam: <GB>
    envVariables:
      sshIsolation: "vpn service@<zcp hostname>"
    envSecrets:
      GIT_TOKEN: <the user sets the value>
```

- `minContainers: 0`: a container the platform creates for the minimum has no primary snapshot and cannot be reset, so releasing its lease deletes it, and it never counts as the pool's warm container. The pool keeps a warm container through the orchestrator instead (**Warm container** below).
- `minRam`: a lease's dev servers and every check you run in it (a typecheck, tests, a build) share the container's RAM, so size `minRam` for the dev server plus the heaviest check run beside it, and run heavy checks one at a time and single-threaded where the tool offers it (TypeScript 7's `tsc --singleThreaded`, a test runner's single-worker mode). A check killed for memory ends its SSH call with exit 255 and no output, while the dev server can run on.
- `sshIsolation` goes in `envVariables`: a pool container answers SSH only when it is set as a service env variable admitting the zcp service. The top-level import field did not reach pool containers.
- `GIT_TOKEN` is a secret the user owns: name it in `envSecrets`, never write a value. Acquire clones with the pool's own `GIT_TOKEN`.
- The service env reaches the pool's containers.
- zcp calls the orchestrator with the pool's `API_TOKEN`. When zcp's own API token reads the pool's secrets masked, the zcp service takes it by reference under a different name: `<pool>_LEASE_TOKEN: ${<pool>_API_TOKEN}` in its env.

**zerops.yml setup**:

```yaml
zerops:
  - setup: devpool
    build:
      base: <language base>
      addToRunPrepare:
        - <every file the dependency install reads: the manifest and its lockfile — in a workspace monorepo, every member's manifest and the workspace definition too>
    run:
      base: <language base>
      ports:
        - port: <previewPortBase + 1>
          httpSupport: true
        # ... one entry per preview port, up to previewPortBase + maxContainers
      prepareCommands:
        - <install zcp-devsup>
        - <copy them into /var/www, keeping their paths, and install the dependencies there>
      start: zcp-devsup run
```

- Preview ports: with `previewPortBase` in `.zcp/dev-servers.json`, a lease answers on `previewPortBase + ZEROPS_StableNumber`. A per-port subdomain routes only to containers listening on that port, and a container's `ZEROPS_StableNumber` starts at 1, so every port from `previewPortBase + 1` to `previewPortBase + maxContainers` is declared in `run.ports` with `httpSupport: true`.
- Dependencies: bake them into `/var/www` in `run.prepareCommands`. `run.prepareCommands` see only what `build.addToRunPrepare` copies — never the repository — so list there every file the install reads; the image rebuilds when one of them changes. They survive a reset, and the checkout keeps them (ignored files stay). A branch whose lockfile differs declares its incremental install as `prepare` in `.zcp/dev-servers.json` (e.g. `"prepare": "npm install --prefer-offline"`); acquire runs it in `/var/www` after the checkout, before the supervisor starts the declared servers, and reports it — a failed one leaves the lease held.
- Workspace monorepo (a package manager's workspace — pnpm, yarn, npm and the like): the install resolves every member, so `build.addToRunPrepare` lists every member's manifest (each `package.json` under the workspace's folders), the workspace definition (`pnpm-workspace.yaml`, or the root `package.json` whose `workspaces` lists the members), the lockfile (`pnpm-lock.yaml`, `yarn.lock`, `package-lock.json`) and any patch directory the lockfile references (`patches/`). A build command can gather them from the full tree into one folder that `addToRunPrepare` lists; `run.prepareCommands` copy them into `/var/www` keeping their paths and install the whole workspace from the lockfile (`pnpm install --frozen-lockfile`), so the installed tree matches the lockfile: a package manager that checks the tree before it runs a script (pnpm 11) reads a filtered install as stale — a `--filter "<pkg>..."` that keeps the part's whole closure included — and a later `pnpm run` or `pnpm exec` in a lease starts a full install first, heavy enough to run the lease out of memory. An image kept filtered calls the part's tools by path (`./node_modules/.bin/<tool>`), never through the package manager. And a part's checks can reach beyond its own package closure: a test or a typecheck that imports another member's sources needs that member's dependencies baked too, or the errors of the missing packages bury the change's own.
- Supervisor: the image carries `zcp-devsup` (installed in `run.prepareCommands`) and `start: zcp-devsup run`. `run.start` runs only in a container the pool just created — never again after a reset — so acquire and `zerops_dev_server` start the supervisor in a lease themselves. A pool whose image has no `zcp-devsup` is no dev pool: acquire says so and `zerops_dev_server` refuses its leases. A dev service's setup carries the same two lines (see `zerops_knowledge uri="zerops://themes/dev-servers"`).
- A deploy of the pool changes only containers created after it; existing containers, leased or free, keep their version until `zerops_lease action=rollout pool="<pool>"` replaces the free outdated ones. A lease is skipped and keeps its version until it is released and the pool is rolled out again.

**Several parts, one branch**: when a change edits and runs code in several parts, each its own dev pool, the branch holds one lease per such part. A part it only calls stays unleased; its callers reach that part's stage, which tracks `main`.

- Acquire: `zerops_lease action=acquire pools=["apipool","webpool"] branch="<branch>"` checks every pool, the branch and one repository before it takes any container, then leases one per pool at once, all or none — a failure hands back every container the call took. A branch new to origin is cut from one `main` commit for every part. A pool the branch already holds answers with that lease and `reused: true` (one lease per repository, branch and pool: another conversation on the branch shares it). A part added later joins with `pool="<pool>"` and the same branch. Each lease the acquire took carries `devServers` — each server's `status`, `running` or `starting` while it still compiles, and the preview server's `url` — and the `siblingEnv` values its servers got; check a `starting` one with `zerops_dev_server action=status hostname="<alias>" name="<server>"`.
- Wiring: `.zcp/dev-servers.json` names in `siblingEnv` each variable a part's servers read and the pool it reaches, and each entry names its part's pool as `service`:

```json
{
  "version": 1,
  "siblingEnv": { "API_HOST": "apipool" },
  "servers": [
    { "name": "api", "port": 8080, "command": "<the api's dev command, listening on 0.0.0.0:8080>", "cwd": "apps/api", "service": "apipool" },
    { "name": "web", "port": 3000, "command": "<the web's dev command>", "cwd": "apps/web", "service": "webpool" }
  ]
}
```

- Every server started in a lease gets the variable set to the container hostname of the branch's lease on that pool (`node-id-<n>.runtime.<pool>.zerops`, its own for its own pool); the app adds `http://` and the port. It serves server-side calls (a dev proxy, server rendering); a browser cannot resolve it. For code that runs in the browser, name the variable in `siblingPreviewEnv` instead (`"siblingPreviewEnv": {"VITE_HQ_URL": "hqpool"}`): it gets the preview URL (`https://`) of the branch's lease on that pool, resolved again whenever a lease of the branch joins or leaves — the port follows the container, so never copy it by hand. A name is in one of the two maps, never both. A pool the branch holds no lease on leaves the variable to the container's own value: set it in the calling part's pool service env to the called part's stage hostname (a `siblingPreviewEnv` variable to the stage's public URL) — `API_HOST: <the api's stage hostname>` beside `sshIsolation` in its import, or `zerops_env action=set serviceHostname="webpool" variables=["API_HOST=<the api's stage hostname>"]` — and run the called part's dev server on its stage's port. A supervisor already running keeps the env it started with. The start result's `siblingEnv` names each variable `set` and each `leftToContainerEnv`. A variable's name is never `ZEROPS_*`, `hostname`, `serviceId`, `projectId`, `PATH` or `HOME`; a broken rule invalidates the file. The pool's `zcp-devsup` must be new enough to take the values: on an older one a start that would set one is refused, naming the way to a newer one.
- A server a sibling calls listens on `0.0.0.0`: the preview reaches a loopback-only server, a sibling does not.
- `service`: a lease runs the entries naming its pool and those naming none, so an entry without `service` runs in every part's lease. An entry a lease's `zerops_dev_server` start adds names its pool. `port` and `preview: true` stay unique across the whole file.
- Sync: commit in one lease over SSH, then `zerops_lease action=sync lease="<alias>"` pushes the branch and fast-forwards every other lease of it. It never forces, and refuses a lease of `main`, uncommitted or untracked files in the source, and a push origin rejects — when origin moved on, integrate `origin/<branch>` there and sync again. Each other lease answers `up-to-date` or `fast-forwarded`; `has-own-commits`, `local-changes-in-the-way`, `on-another-branch`, `unreachable` and `failed` leave it as it was, with a note — so sync before you commit in another lease. A fast-forwarded lease lists in `unwatchedServers` only its runtime entries (`ctl run`), which serve the old code until restarted; a declared server reloads by itself — through its `watch`, or its command's own watcher — so a sync never names one. A lease whose `.zcp/dev-servers.json` changed has its supervisor reload it (`reloaded`).
- Release: `zerops_lease action=release branch="<branch>"` (with `repo` when the branch is leased for several repositories) reads every lease's unsaved work first; any found refuses them all, naming each lease's files and commits, and `force=true` releases every lease, discarding it. A lease the orchestrator would not release stays held with an `error`; the same call again finishes the branch.
- Rewire: an acquire or release that adds or removes a lease of the branch hands every other lease the recomputed values (a release, before its container goes), and their supervisors restart the servers running under the old ones (`rewired`); a lease it cannot reach is a note naming `zerops_dev_server action=restart hostname="<alias>" name="<server>"`. A lease that ends outside zcp is not chased: status marks a lease still wired to its container `wiring: "stale"`, with the restart that fixes it.

**Warm container**: each pool keeps one warm container — free, running, reset, on the pool's current version — that an acquire takes at once, where a create takes about 16 s on a container pool and minutes on a VM pool.

- Release: once its check for unsaved work passes, a release keeps its container as the pool's warm one, reset, when the pool has none besides it and the container runs the pool's current version; otherwise it removes the container with its own lease. The result says which — "kept as the pool's warm container", or removed and why (the pool already had its warm container, or the container ran an outdated version). A removal the platform refuses falls back to the reset and is named.
- Top-up: after every acquire and release, zcp creates what the pool lacks through the orchestrator's `POST /container`, in the background and within `maxContainers` — never through `minContainers`, whose containers cannot be reset and never count as warm.
- Status: `zerops_lease action=status` reports each pool's `warm` — `target`, `ready` (absent with a note when it could not be counted), `preparing` and `last`, what the last top-up did, a failed or full one included.
- Clone: the warm container already holds the pool's repository — the one its last lease checked out — at `main`, cloned by zcp after it creates or keeps the warm container, so an acquire on it only fetches and checks out what its branch changes (z3-sized repository: 3–5 s instead of 9–10 s). An acquire that comes while that clone still runs waits for it; a clone of another repository, or one that did not finish, is removed and the checkout starts clean. The pool image needs `git`, `flock` and `setsid` (Ubuntu bases have them). `zerops_lease action=status` reports the last clone as `warm.clone`.
- An acquire that comes while a container is being created can be refused `containers-preparing`; zcp retries it for up to a minute (on a VM pool, to the acquire's own deadline) and the lease's note names the wait.

**Idleness**: `zerops_lease action=status` reads each held lease in one SSH call that changes nothing.

- `activity`: `lastChangeAt` is the checkout's last change — the newest of HEAD's last move (a commit, a checkout) and any modified or untracked file git does not ignore; a deleted tracked file or a rename does not move it. `previewLastConnAt` and `previewOpenConns` are the preview's last connection and the connections open now, from a `zcp-devsup` new enough to report them. `acquiredAt` is when the lease was taken.
- `unsaved`: the release's own check — uncommitted and untracked files (the first 20, all counted) and commits on no remote branch.
- A lease with no activity for 24 h — no change, no preview connection, no acquire — is marked `idle` with `since` (its last activity) and `next`: with nothing unsaved, `zerops_lease action=release lease="<alias>"`; with unsaved work, what it holds and how to keep it (commit and push, or park it), never `force`.
- A preview connection open now is in use, however long ago it opened, so that lease is never idle. A fact that could not be read — an older `zcp-devsup`, an unreachable container — leaves the lease unmarked, never idle.
- The leases of one branch across several pools are idle together or not at all, and `next` names the branch's release, or what each lease holds.
- A full pool's refusal lists the leases zcp holds the same way, oldest activity first, each with what a release would lose.

**Parking**: a lease you will come back to need not run meanwhile. `zerops_lease action=park lease="<alias>"` unmounts it, then stops its container and keeps it reserved with its `/var/www` and `$HOME` (an Ubuntu base empties `/tmp` at every start); every process ends, its dev servers and supervisor included. A stopped container still counts toward the pool's maximum.

- While parked, nothing answers on it: status lists it `parked` with `parkedAt`; `zerops_dev_server`, `zerops_deploy` and `zerops_verify` refuse it, naming the unpark; a sync from it is refused, and a sync from another lease of the branch skips it (`parked`). It keeps its hostname, so the branch's other leases stay wired to it and reach nothing there until it runs again.
- `zerops_lease action=unpark lease="<alias>"` starts the container, waits for SSH, mounts `/var/www/<alias>` and starts the supervisor when none runs, with the branch's sibling values as they now stand; the declared servers start again.
- A release of a parked lease starts it first so its unsaved work can be read, and stops it again when the release is refused. `force=true` releases it as it stands, stopped.
- Nothing parks a lease by itself: an idle lease with unsaved work is only offered the park.

**Releasing**: Nothing releases, stops or deletes a lease because it is idle — idleness is a label, and a person decides. zcp records no conversation per lease: status and a full pool's refusal list every lease this zcp service holds, other conversations' included. So ask the person before releasing a lease this conversation did not take, and never force one to free a container. Files git ignores (a local `.env`, a SQLite file, build output) and stashes are counted by no release check: they go with any release.

### Zerops Box

**Type**: `ubuntu/zbox@1` (an import may write `zbox@1`, stored as `ubuntu/zbox@1`), in beta. A Box is a single Ubuntu VM, not a container, that runs sandboxes as microVMs. It is not a zerops.yml `run.base`: no build, no deploy, no container counts.
**Region**: a Box can be created only in a project in region prg1. In any other region its create fails with the platform's "not enough hardware capacity to run your container" message and the platform removes the service, so `zerops_import` refuses a Box there before it creates anything, and so does a bootstrap plan that would create one.
**After import**: a Box created by import or in the GUI starts empty — no base and no domain, so a sandbox has nothing to boot from and its ports get no URL. `zerops_import` finishes the setup of each Box it creates before it returns — the `default` base pulled, and the 8081 subdomain added as a sandbox domain when subdomain access is on — and answers one `boxSetup` block per Box; `zerops_box action=setup serviceHostname=<box>` finishes it later (a pull still running, subdomain access turned on afterwards). Outside zcp, `zbox setup` does both (by hand: `zbox base add <image> --name default`, then `zbox workspace domain add <its 8081 subdomain> --form dash`).
**Lifecycle**: a reload restarts only the orchestrator and keeps running sandboxes. A restart, a stop and every resize (CPU and RAM as well as disk) stop the VM: running sandboxes come back `PARKED` without their memory — park the ones to keep first. Disk can only grow. zcp never restarts a Box for an env change.
**Ports**: 8080 serves the API and the web console, 8081 the sandbox ingress. Sandbox port URLs need subdomain access (`enableSubdomainAccess: true`; the GUI form leaves it off) and the 8081 subdomain among the workspace's domains. A project service reaches a sandbox's `project` port at `http://<box>.zerops:8081` with the port's host in the `Host` header.
**API**: other services reach the workspace API at `http://<box>:8080` with the workspace API key as the bearer token. Every route is under `/api/rest/`; only `GET /api/rest/ping` answers without the key, and a key in the query string is refused. Requests that consume capacity — create, fork, park, wake, snapshot, restore or terminate a sandbox, pull a base, create a template — need an `Idempotency-Key` header and answer with an operation: long-poll it with `GET /api/rest/operation/{id}?waitSeconds=30`. A sandbox create requires `networkMode`. Errors carry the HTTP status and `{"error":{"code","message","meta"}}`, e.g. `401 unauthorized`, `409 workspaceFull`. The OpenAPI document is at `/api/rest/swagger/openapi.yml`.
**Limits**: through the public API URL the Zerops edge ends a request after 60 s and refuses uploads over 50 MiB. An exec runs 60 s by default and 10 min at most, with 4 MiB each of stdout and stderr in its answer — start a background process for longer work.
**Keys**: the API key is the Box's `ZBOX_WORKSPACE_API_KEY` — the whole authority over the workspace, with no scopes. An app wires it by reference in its `run.envVariables` — `BOX_API_KEY: ${<box>_ZBOX_WORKSPACE_API_KEY}` — and never copies the value. Editing it rotates it: the new key applies on the Box's next reload (`zerops_manage action=reload`), and until then only the old key works. `ZBOX_WORKSPACE_VOLUME_KEY` unlocks the Box's encrypted data disk and cannot be changed; no app needs it. The Box's other variables (sandbox secrets, integration credentials) apply at once. Box never delivers a `ZBOX_` variable into a sandbox.
**Sandboxes**: no Docker yet; images must be `linux/amd64`; inside a sandbox a project service resolves only as `<host>.zerops`, never by its bare hostname.

### Running code the app's users send

Code the app does not control — a playground's snippets, a grader's submissions, a notebook's cells, an agent's tool calls, plugins — runs in a Box sandbox, never inside the app's own container: namespaces, chroot or a subprocess there still share the container's kernel, its files and its project network. Each sandbox is its own microVM, reads none of the project's values unless they are named for it (*Untrusted code* below), and its network mode decides what it reaches. Import the Box (`ubuntu/zbox@1`) beside the runtime — in a bootstrap plan as a dependency of the runtime (`"resolution": "CREATE"`), or in its own `zerops_import`. It is never a runtime target, and it exists only in a project in region prg1. Every shape below is the workspace API as an app in the project met it (OpenAPI document and live, 2026-10-24).

**Wiring**: the app's zerops.yaml names the API and the key by reference in `run.envVariables`, so the key reaches only the app's process — never copy its value:

```yaml
run:
  envVariables:
    BOX_API_URL: http://<box>:8080
    BOX_API_KEY: ${<box>_ZBOX_WORKSPACE_API_KEY}
```

Every call below goes to `$BOX_API_URL` with `Authorization: Bearer $BOX_API_KEY`; a wrong key answers `401 unauthorized`.

**Base**: a create names its base by digest. `GET /api/rest/base-list` answers `{"items": [...]}`: take the `digest` of the item named `default` whose `status` is `READY`. The name in place of the digest answers `404 baseNotFound`. Read it when the app starts rather than writing it into code: a base pulled again gets a new digest.

**Create**: `POST /api/rest/sandbox` with a new `Idempotency-Key` header and

```json
{"baseDigest": "<digest>", "networkMode": "AIRGAPPED", "integrations": [], "waitSeconds": 30}
```

A missing `Idempotency-Key` or `networkMode` answers `400`. `AIRGAPPED` gives the sandbox no network interface at all — the mode for strangers' code, and exec still works; `INTERNET` adds the internet and no project service. With `waitSeconds` the answer holds the finished operation and the sandbox: `operation.status` is `FINISHED`, `sandboxes[0].id` is the sandbox and `sandboxes[0].status` is `RUNNING`. A sandbox gets 1 vCPU and 512 MiB unless the body sets `cpus` and `memoryBytes` (a multiple of 16 MiB, at least 64 MiB). The same key with the same body replays the first answer; with another body it answers `409 idempotencyKeyConflict`.

**Exec**: `POST /api/rest/sandbox/{id}/exec` with

```json
{"command": ["python3", "-c", "<source>"], "timeoutSeconds": 10, "workingDir": "/workspace"}
```

`command` is argv, run with no shell (wrap pipes and redirects in `sh -c`), and there is no stdin — larger input goes in a file first (`PUT /api/rest/sandbox/{id}/file?path=<path>`, the bytes as the body). It runs as root, in `/` unless `workingDir` names another directory. The answer is `{"sessionId": "…", "exitCode": 0, "stdout": "42\n", "stderr": "", "timedOut": false, "stdoutTruncated": false, "stderrTruncated": false}`. A command past its `timeoutSeconds` is killed and keeps what it printed: `timedOut` is true and `exitCode` is null. A program that does not exist exits `127`. Each of `stdout` and `stderr` is cut at 4 MiB, with `stdoutTruncated` / `stderrTruncated` set. The timeout is 60 s by default and 10 min at most. The `default` base is Ubuntu with Python 3, Node.js, gcc and git; in any sandbox `uname -r` reads 6.18.53 and the hostname is the sandbox's id.

**Terminate**: `DELETE /api/rest/sandbox/{id}` with an `Idempotency-Key` answers its `TERMINATE` operation, which finishes within milliseconds: `GET /api/rest/operation/{id}?waitSeconds=30` returns it `FINISHED`, and the sandbox then reads `TERMINATED`. Terminate in a `finally`: idle and absolute TTLs are not enforced yet, so a sandbox the app forgets keeps running and holds its share of the Box.

**Per run**: for strangers' code, create one sandbox per run and terminate it afterwards — no file or process of one run reaches the next. From an app in the project a create took about 250 ms, an exec tens of milliseconds and a terminate about 10 ms: about 0.3 s a run. A Box as imported (2 CPUs, about 3.25 GiB for sandboxes) holds six default sandboxes at once; the next create answers `409 workspaceFull`, its `metadata` naming the `resource`, `requested` and `free` — tell the user to try again, or give each sandbox less `memoryBytes`.

**Duration**: the 60 s and 50 MiB caps of the Zerops edge apply only through the Box's public API URL. Inside the project, at `http://<box>:8080`, a request lasts as long as its exec: a 70 s exec answered after 70 s.

### Untrusted code

Every container of a project, a pool's included, gets the project's Shared vault values and its own service's (Zerops still injects unreferenced ones today). In a project with `envIsolation: none` it also gets every other service's values as `<host>_KEY`, secrets included — a pool's `<pool>_API_TOKEN`, a database's password, the zcp service's `ZCP_API_KEY`. Code running in a pool container can read all of them. Where that code is not yours (an agent's experiments, users' submissions):

- Keep the project, and every service whose values the code must not read, on `envIsolation: service` (the default): it stops the `<host>_KEY` copies. A service set to `envIsolation: none` hands its values to every other container. Keep secrets out of the Shared vault, which reaches every container either way.
- Run code that must read none of the project's values in a Box sandbox: each sandbox is its own microVM and gets only the variables named for it with `--secret` (`--secrets all` hands it every variable the Box sees, project values included), never a `ZBOX_` one. Its default network mode, `PROJECT_INTERNET`, still reaches every project service, the database included: give untrusted code `INTERNET` or `AIRGAPPED` (a workspace policy's `allowedNetworkModes` enforces it), and hand it API credentials through the credentials proxy (an integration), which keeps the value out of the sandbox. A base built from a Dockerfile runs its `RUN` steps on the Box VM with the project network — build only Dockerfiles you trust.

## Common Patterns

- **Deployment variant**: HA vs single is the type variant (`<svc>:ha` / `<svc>:single`), immutable. Legacy `mode: NON_HA`/`HA` still accepted (≡ `:single`/`:ha`) but deprecated.
- **Profile** (PostgreSQL + Valkey only): autoscaling tier — dev → `oltp-hobby` / `hobby`, production → `oltp-staging` / `staging`; escalate higher only on a clear signal. Set it explicitly — omitting it on HA applies `oltp-production` (dedicated CPU).
- **Hostname**: immutable, becomes internal DNS name
- **Internal**: HTTP/plain TCP only (no TLS) -- TLS for external/VPN ports
- **Credentials**: auto-generated in env vars
- **System Users**: don't modify `zps`/`zerops`/`super`
- **VPN**: append `.zerops` to hostname
- **Backup**: PostgreSQL, MariaDB, Elasticsearch, Meilisearch, Qdrant, NATS (NOT Valkey/KeyDB, NOT ClickHouse -- use SQL BACKUP)
- **Priority**: `priority: 10` for databases/storage to start before apps

## Service Operations

### Database Export/Import
- **PostgreSQL**: `pg_dump` for export, `psql` for import
- **MariaDB**: `mysqldump` for export, `mysql` for import
- Requires Zerops VPN or Adminer (built-in web DB tool)
- Desktop tools (DBeaver, pgAdmin) connect via VPN using standard env vars

### Backup System
- **Supported**: PostgreSQL, MariaDB, Elasticsearch, Meilisearch, Qdrant, NATS, SeaweedFS, Local Storage (daily `.tar.gz` of the volume)
- **NOT supported**: Valkey, KeyDB, ClickHouse (use native dump), Object Storage
- Default schedule: daily 00:00-01:00 UTC (configurable)
- Retention: 7 daily, 4 weekly, 3 monthly; max 50 per service
- End-to-end encryption (X25519 per project)
- Storage limits: 5 GB (Lightweight), 25 GB (Serious), 1 TiB max

### Debug Mode
- Available for build and runtime prepare phases
- Pause points: Disable, Before First Command, On Command Failure, After Last Command
- Commands: `zsc debug continue`, `zsc debug success`, `zsc debug fail`
- Max duration: 60 minutes
