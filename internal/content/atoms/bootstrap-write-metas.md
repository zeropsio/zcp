---
id: bootstrap-write-metas
priority: 6
phases: [bootstrap-active]
routes: [classic, adopt]
steps: [close]
title: "Write ServiceMeta evidence files"
---

### Persist the ServiceMeta files

The bootstrap step list reserves the `write-metas` step for persisting a
`ServiceMeta` file per runtime service. The meta is the on-disk evidence
that bootstrap completed and the downstream workflows (`develop`, `cicd`)
rely on to target services. Without it, a service is invisible to ZCP.

Each meta captures:

- `Hostname` — the deployed-to hostname
- `Mode` — `dev` / `standard` / `simple` (user-confirmed)
- `StageHostname` — only for `standard`; the dev/stage pair
- `BootstrapSession` — session id, links the meta to its bootstrap run
- `BootstrappedAt` — RFC3339 timestamp of completion

Strategy is NOT chosen at bootstrap — it is left unset and gets written by
the develop workflow on first use. Bootstrap's job is infrastructure
verification; strategy selection belongs to the code-iteration phase.
