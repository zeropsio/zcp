# Citation map

When a fact's topic matches one of the platform topic areas below, you MUST call `mcp__zerops__zerops_knowledge topic=<id>` BEFORE writing content about that topic. Read the guide. Align your framing with the guide's framing. Cite the guide by name in the published content.

Writing new mental models for topics the platform already documents is how folk-doctrine ships. The Citation Map is the hard wall against that class.

---

## Topic areas and guide IDs

Each row maps a topic area to the authoritative platform guide identifier passed as the `topic` argument to `mcp__zerops__zerops_knowledge`. Column three names what the guide covers so you can decide whether the topic matches.

| Topic area | Guide ID | What the guide covers |
|---|---|---|
| Cross-service env vars, self-shadow, aliasing | `env-var-model` | Auto-inject semantics across services; the self-shadow trap; legitimate renames (`DB_HOST: ${db_hostname}`); mode flags; envIsolation semantics; project-level vs service-level scope. |
| `zsc execOnce`, `appVersionId`, init commands | `init-commands` | Per-deploy execOnce key semantics; `--retryUntilSuccessful` boot retry; static vs per-deploy key rationale; migrate-vs-seed key selection. |
| Rolling deploys, SIGTERM, HA replicas | `rolling-deploys` | Two-axis `minContainers` (throughput scaling + HA failover); SIGTERM-before-teardown sequence; drain semantics; queue-group requirement for multi-replica consumers. |
| Object Storage, MinIO, `forcePathStyle` | `object-storage` | MinIO-backed Object Storage; path-style requirement vs virtual-hosted-style rejection; `storage_apiUrl` (HTTPS) vs `storage_apiHost` (HTTP redirect); credential env-var names. |
| L7 balancer, `httpSupport`, VXLAN IP routing | `http-support` | Why bind `0.0.0.0`; TLS termination at the balancer; `trust proxy` for Express / equivalent for other frameworks; `httpSupport: true` gating. |
| Deploy files, tilde suffix, static base | `deploy-files` | `./dist/~` tilde-strip rationale; `deployFiles: ./` dev-mount preservation; `base: static` limitations; content vs mount semantics on redeploy. |
| Readiness check, health check, routing gates | `readiness-health-checks` | What routes traffic (readiness) vs what restarts the container (liveness); bare-GET vs authenticated endpoint choice; gating the first request vs ongoing traffic. |
| Cross-service references, service discovery | `env-var-model` | (Same guide as row 1.) `${hostname_*}` interpolation in declared env vars; renaming platform-provided vars cleanly vs self-shadowing. |

---

## When to cite — the rule

For every gotcha bullet, every integration-guide item, and every env `import.yaml` comment you author:

1. Scan the content's topic against the table above.
2. If one row matches, call `mcp__zerops__zerops_knowledge topic=<guide-id>` and read the returned content.
3. Align your framing with the guide's framing. If your draft contradicts the guide, the guide wins — rewrite your draft to cite and quote the guide's framing, not reinvent it.
4. In the published content body, reference the guide by name (for example "see the platform's `env-var-model` guide for the full self-shadow rule"). The citation is a reader cue that the underlying mechanism is platform-documented.

## When NOT to cite

Not every piece of content needs a citation. An operational CLAUDE.md section on "drop-and-reseed without a redeploy" cites nothing because the topic is repo-local iteration, not a platform mechanism. A scaffold-decision YAML comment ("`deployFiles: ./dist/~` strips the dist wrapper") may cite `deploy-files` because the tilde syntax IS a platform mechanism, but the rationale comment itself is scaffold decision and the citation is lightweight.

Rule: cite when your content touches a platform mechanism that the platform already documents. Do not cite when your content is scaffold trade-off, operational iteration, or repo-local taste.

---

## Citation format

In published content, the citation is a prose reference that a reader can follow. Examples:

- In a gotcha body: "The platform injects NATS credentials as separate `NATS_USER` / `NATS_PASS` env vars; see the platform's `env-var-model` guide. The Node NATS client at its current major release strips URL-embedded creds silently, so the client must pass user and pass as separate `ConnectionOptions` fields."
- In an integration-guide item: "Bind to `0.0.0.0` instead of `127.0.0.1` so the L7 balancer can reach the container over VXLAN; see the platform's `http-support` guide for the routing model."
- In an env `import.yaml` comment: "`minContainers: 2` here is for HA failover during rolling deploys, not for throughput — see the platform's `rolling-deploys` guide for the two-axis model."

The citation does not need a URL; naming the guide ID by word is the contract.

---

## Missing-guide disposition

If `mcp__zerops__zerops_knowledge` returns a "no matching topic" result for a citation-map entry, the guide may not yet exist. In that case:

1. Record the gap in your completion return (the reviewer and the step above you will want to know the platform knowledge base is thin in that area).
2. Proceed to write the content without a citation reference.
3. Keep the content's framing neutral and evidence-based — do not invent mechanisms. An uncited gotcha body names the observable symptom, names the framework-side and platform-side contributions as you understand them from the facts log, and stops short of asserting mechanism details you cannot verify.

The self-review atom treats an uncited gotcha on a matching-topic row as a self-review failure UNLESS the missing-guide disposition is documented in the completion return.

---

## Every-matching-topic-gotcha-cites rule

One hard rule from this atom flows through to the self-review atom: every gotcha whose topic matches a Citation Map row must reference the cited platform topic in its body. This is the structural protection against folk-doctrine. Two gotchas the writer ships on the same matching topic must both cite; citing once in IG and leaving the gotcha uncited does not satisfy the rule because the gotcha body stands alone to its reader.
