# Citation audit

Every published gotcha whose topic matches a platform topic area MUST cite the matching platform topic guide. Missing citations on matching-topic gotchas are the surface defect that produces folk-doctrine — the writer invents a mechanism because they did not consult the guide the platform already publishes.

## The topic map

Eight topic areas. When a gotcha's mechanism falls inside any of these areas, the gotcha cites the guide by its full topic name. Abbreviated references ("the env guide") do not count; the full topic name or the guide's URL is required.

| Topic area | Platform topic identifier | What the guide covers |
|---|---|---|
| Cross-service environment variables, self-shadow, aliasing | `env-var-model` | Auto-inject semantics; never declare `key: ${key}`; legitimate renames such as `DB_HOST: ${db_hostname}`; mode flags |
| `zsc execOnce` gate, `appVersionId`, init commands | `init-commands` | Per-deploy gate semantics; `--retryUntilSuccessful` usage |
| Rolling deploys, SIGTERM, HA replicas | `rolling-deploys` and `minContainers-semantics` | Two-axis `minContainers` (throughput and HA separately); SIGTERM-before-teardown; drain semantics |
| Object Storage, MinIO, `forcePathStyle` | `object-storage` | MinIO-backed storage; path-style addressing required; `storage_*` environment variable shape |
| L7 balancer, `httpSupport`, VXLAN routing | `http-support` and `l7-balancer` | Why bind `0.0.0.0`; TLS termination; `trust proxy` rationale |
| Cross-service references, isolation modes | `env-var-model` (same guide as the self-shadow entry) | `envIsolation` semantics; project-level versus service-level |
| Deploy files, tilde suffix, static base | `deploy-files` and `static-runtime` | `./dist/~` rationale; `base: static` limitations |
| Readiness check, health check, routing gates | `readiness-health-checks` | What routes traffic; what restarts the container |

## How to audit

For each gotcha on each codebase's `GOTCHAS.md`:

1. Read the gotcha's mechanism statement.
2. Scan the topic-area column above. If the mechanism falls under any row, the gotcha is a **matching-topic gotcha** and a citation is required.
3. Check the gotcha's body for the guide's full topic identifier. A match is an inline occurrence — somewhere in the gotcha's prose, not only in an appendix at the bottom of the file.
4. If the gotcha is a matching-topic gotcha and the citation is missing, record a citation-coverage finding at WRONG severity (see the reporting-taxonomy atom).
5. If the gotcha cites the guide but the cited phrasing contradicts what the guide actually says, record a fabricated-mechanism finding at CRIT severity — the citation is cover for an invented mechanism.

## Coverage metric

At completion, compute the citation-coverage percentage:

- Denominator: the count of matching-topic gotchas across all codebases.
- Numerator: the count of matching-topic gotchas that carry an inline citation to the guide's full topic identifier.

Report the percentage in the completion payload. A coverage below 100% means the deliverable has gotchas whose mechanism the platform already documents but which the writer did not cite — the structural enabler of folk-doctrine.

## What is NOT a citation-audit finding

A gotcha whose topic falls OUTSIDE the eight topic areas does not require a citation. Some gotchas are genuinely new content — a platform-×-framework intersection the platform guides are silent on. In those cases, the absence of a citation is correct. The audit flags only matching-topic gotchas with missing citations, not uncited content whose topic is genuinely outside the map.
