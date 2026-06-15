# Managed services in `env/<N>/import-comments/<host>`

Managed services (`db`, `cache`, `broker`, `search`, `storage`) are
NOT slotted — they have ONE hostname per project. Use the bare
service hostname as the `<host>` target:

- `env/<N>/import-comments/db`       — Postgres comment block
- `env/<N>/import-comments/cache`    — Valkey/Redis comment block
- `env/<N>/import-comments/broker`   — NATS comment block
- `env/<N>/import-comments/search`   — Meilisearch comment block
- `env/<N>/import-comments/storage`  — object-storage comment block
- `env/<N>/import-comments/project`  — project-scope comment block

A common run-time trap: agents try `dbdev`/`dbstage` thinking
managed services follow the same dev/stage shape as runtime
codebases. They don't. If you see "unknown fragmentId
`env/0/import-comments/dbdev`", drop the `dev`/`stage` suffix.
Managed services have a single deployment per project, not a pair.
