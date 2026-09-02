# Shared Storage residue — `connect-storage` disposition + internal naming

**Surfaced**: 2026-09-02, the shared-storage sweep (commit `b2987a37`). Matěj's ask: "minimize
mentions and recommendations of shared storage, in favor of local storage or raw managed seaweedfs
(released today) … leave no traces of shared storage for agents and search indexes, so that people
start using local storage or raw managed seaweedfs with custom mounts". Karel: "dej si nekam
poznamku ze to po case docistime."

The sweep took every surface an agent actually reads — knowledge corpus, atoms, guided skill
template, catalog line, tool descriptions, README. What is listed here is deliberately what it did
NOT take, because each item needs a decision or a live fact the sweep could not supply.

**Why deferred**: item 1 is a breaking MCP-surface change resting on a doc paraphrase, not on a
live probe. Items 2-3 are a topology-layer rename — foundational L2 vocabulary, `/flow` shaped, and
invisible to both agents and search indexes, so it buys nothing toward Matěj's stated goal.

**Trigger to promote**: any of —
1. Matěj (or a live probe) confirms `PutServiceStackConnectSharedStorage` is gone or permanently
   best-effort → item 1 becomes a deletion, not a question.
2. A user or eval hits `connect-storage` and gets a platform error → item 1 is now a bug.
3. Any other topology rename ships (the `CloseDeployMode` → `DeliveryMode` backlog entry is the
   obvious partner) → items 2-3 ride along cheaply.

## 1. `zerops_manage action="connect-storage" / "disconnect-storage"` — disposition undecided

`internal/tools/manage.go` → `internal/ops/manage.go::ConnectStorage` →
`platform.ZeropsClient.ConnectSharedStorage` → SDK `PutServiceStackConnectSharedStorage`.

This action drives the **platform-managed mount** — the mechanism the migration guide says was
retired with Shared Storage: "Previously, Zerops managed the mount through the GUI's Shared Storage
connections page. Now, services must mount the storage themselves", and the surviving
`zeropsSharedStorageMounts` path "is best-effort from now on and will be removed in a later
release" (https://docs.zerops.io/seaweedfs/how-to/migrate-from-shared-storage).

So the action very likely calls a dead or dying endpoint, and it steers agents at the deprecated
mechanism by existing at all.

What the sweep did instead of deleting: relabelled the description
(`connect-storage/disconnect-storage drive the DEPRECATED managed seaweedfs mount at
/mnt/{storageHostname}; prefer local-storage run.volume`), rewrote both `ops` doc-comments, and
removed the "call connect-storage after first stage deploy" prescription from
`docs/spec-workflows.md §4.8` and from the services.md card.

**Open question, needs a live answer**: does `PUT .../connect-shared-storage` still succeed on a
seaweedfs service? Probe on a rig project (VPN needs a human for sudo), or ask Matěj. If it is
dead — delete the two actions, `ops.ConnectStorage`/`DisconnectStorage`, the `platform.Client`
interface methods, the mock arms, and `StorageHostname` from `ManageInput`; the annotations test's
`/mnt/` + `connect-storage` keywords for `zerops_manage` go with them. Deleting outright is the
CLAUDE.md-correct move ("delete, don't disable") — the only thing missing is the fact.

## 2. Internal identifiers still carry the retired name

Not agent-facing (no MCP text, no corpus, no rendered output), so Matěj's goal is already met
without them. They are a legibility debt, not a trace:

- `internal/topology/type_equivalence.go` — `kindSharedStorage`; `predicates.go` —
  `IsSharedStorageType`. Note the CONSTRAINT: `CanonicalBaseName` folds every `seaweedfs:*` and
  `shared-storage:*` spelling to the key `"shared-storage"`, and `serviceNormalizer`,
  `bundle.RulesForType`, `workflow.serviceTypeKind`, `workflow.contractKindForType` and
  `tools.isManagedNonStorage` all key off it. Flipping the canonical direction (seaweedfs
  canonical, shared-storage the alias) is the honest rename and touches all of them at once.
- `internal/authoring/port/harden.go` — `SurfaceSharedStorage`, whose VALUE `"shared-storage"` is
  deliberately the topology key. Emitted guidance text already says SeaweedFS.
- `internal/platform/` — `ConnectSharedStorage` on the client interface + mock (moot if item 1
  deletes it).
- `internal/eval/prompt.go:164` — the `"sharedstorage"` eval-scenario category label.
- `internal/ops/bundle/rules.go` — doc comments on which types accept `mode:`.

## 3. Archives left untouched on purpose

`plans/archive/**` and `docs/archive/**` hold most of the remaining grep hits. They are a
historical record of what was true when written; rewriting them would be falsification, and
`TestNoPlansCitations`-style discipline already keeps them from being cited as sources. Leave them.

## Refs

- Sweep commit `b2987a37`; the schema drift that exposed it, `705d7242` (live import schema dropped
  the `mount` property, seaweedfs enum narrowed to `@3.85`).
- Authoritative corpus (already correct, written before the sweep):
  `internal/knowledge/decisions/choose-storage.md`, `internal/knowledge/guides/seaweedfs-integration.md`.
- `https://docs.zerops.io/storage/overview`, `https://docs.zerops.io/seaweedfs/how-to/migrate-from-shared-storage`.
- Sibling `../zerops-docs` checkout was stale at sweep time (2026-08-26, no `seaweedfs/` section) —
  pull before trusting it.
- Partner rename entry: `plans/backlog/rename-closedeploymode-to-deliverymode.md`.
