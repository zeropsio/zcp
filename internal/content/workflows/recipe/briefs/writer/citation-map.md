# Citation map

When a fact's topic matches one of the platform topic areas below, you MUST call `mcp__zerops__zerops_knowledge topic=<id>` BEFORE writing content about that topic. Read the guide. Align your framing with the guide's framing. Cite the guide by name in the published content.

Every `content_gotcha` and `content_ig` manifest entry is gated: no entry ships without at least one citation carrying a non-empty `guide_fetched_at` timestamp. The gate is the structural protection against folk-doctrine — the Citation Map tells you WHICH guide to fetch for the topic you're writing about.

---

## Topic areas and guide IDs

| Topic area | Guide ID | What the guide covers |
|---|---|---|
| Cross-service env vars, self-shadow, aliasing | `env-var-model` | Auto-inject semantics across services; self-shadow trap; legitimate renames (`DB_HOST: ${db_hostname}`); envIsolation semantics; project-level vs service-level scope. |
| `zsc execOnce`, `appVersionId`, init commands | `init-commands` | Per-deploy execOnce key semantics; `--retryUntilSuccessful` boot retry; static vs per-deploy key rationale. |
| Rolling deploys, SIGTERM, HA replicas | `rolling-deploys` | Two-axis `minContainers` (throughput vs HA); SIGTERM-before-teardown sequence; drain semantics; queue-group requirement for multi-replica consumers. |
| Object Storage, MinIO, `forcePathStyle` | `object-storage` | MinIO-backed; path-style requirement; `storage_apiUrl` vs `storage_apiHost`; credential env-var names. |
| L7 balancer, `httpSupport`, VXLAN IP routing | `http-support` | Why bind `0.0.0.0`; TLS termination at the balancer; `trust proxy`; `httpSupport: true` gating. |
| Deploy files, tilde suffix, static base | `deploy-files` | `./dist/~` tilde-strip rationale; `deployFiles: ./` dev-mount preservation; `base: static` limitations. |
| Readiness check, health check, routing gates | `readiness-health-checks` | What routes traffic (readiness) vs what restarts (liveness); bare-GET vs authenticated endpoint choice. |

---

## Citation format

Reference the guide by name in prose — no URL required.

- In a gotcha body: *"The platform injects NATS credentials as separate `NATS_USER` / `NATS_PASS` env vars; see the platform's `env-var-model` guide. The Node NATS client at its current major release strips URL-embedded creds silently, so the client must pass user and pass as separate `ConnectionOptions` fields."*
- In an integration-guide item: *"Bind to `0.0.0.0` instead of `127.0.0.1` so the L7 balancer can reach the container over VXLAN; see the platform's `http-support` guide for the routing model."*
- In an env `import.yaml` comment: *"`minContainers: 2` here is for HA failover during rolling deploys, not for throughput — see the platform's `rolling-deploys` guide for the two-axis model."*

Record the fetch in the manifest entry's `citations` array: `{topic: "<guide-id>", guide_fetched_at: "<RFC3339 timestamp>"}`.

---

## Missing-guide disposition

If `mcp__zerops__zerops_knowledge` returns "no matching topic" for a Citation Map entry, the guide may not yet exist. In that case:

1. Record the gap in your completion return (the reviewer + the step above you will want to know the platform knowledge base is thin in that area).
2. Proceed to write the content without a citation reference, keeping the framing neutral and evidence-based. Do not invent mechanism details you cannot verify; an uncited gotcha body names the observable symptom and stops short of asserting mechanism you lack a source for.
3. Still emit the citations entry on the manifest with the closest-adjacent topic you DID fetch (e.g. `env-var-model` for a cross-service env-var fact whose exact intersection isn't in the guide) — the `guide_fetched_at` timestamp is the evidence that the lookup happened, and that is what the gate reads.
