---
id: bootstrap-write-metas
priority: 6
phases: [bootstrap-active]
routes: [classic, adopt, recipe]
steps: [close]
title: "Write ServiceMeta evidence files"
---

### Persist the ServiceMeta files

`ServiceMeta` is the on-disk evidence downstream workflows (`develop`,
`cicd`) use to target services. Each meta captures:

- `Hostname` — the deployed-to hostname
- `Mode` — `dev` / `standard` / `simple` (user-confirmed)
- `StageHostname` — only for `standard`; the dev/stage pair
- `BootstrapSession` — session id, links the meta to its bootstrap run
- `BootstrappedAt` — RFC3339 timestamp of completion

Strategy is NOT chosen at bootstrap — it is left unset and gets written by
the develop workflow on first use.
