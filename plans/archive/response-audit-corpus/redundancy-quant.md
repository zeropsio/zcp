# Deterministic delivery quant (independent ground-truth for synthesis)

Source: 428 transcripts. Recent = date>=20260604.

## 1. Prose vs structured (all 5298 responses)

- structured (JSON) responses: 4847, bytes 12,200,803 (61%)
- prose (_text markdown) responses: 451, bytes 7,790,511 (38%)
- NOTE: prose responses are 451 of 5298 but carry 38% of all delivered bytes — the heavy responses are the unstructured ones.

## 2. Within-session re-delivery (surface-once cost)

Bytes the agent receives that are EXACT-DUPLICATE content blocks (>=120-char leaf strings / paragraphs) re-sent in a later response within the SAME run.

| scope | avg total ZCP bytes/run | avg re-delivered bytes/run | re-delivered % |
|---|---|---|---|
| recent (>=20260604) | 42,920 | 1,862 | 4% |
| all | 47,149 | 2,934 | 6% |

### Worst re-delivery runs (recent)

| scenario | responses | total bytes | re-delivered bytes | % |
|---|---|---|---|---|
| recipe-nextjs-ssr-frontend-standard | 26 | 106,755 | 19,155 | 17% |
| launch-with-existing-cicd | 16 | 61,407 | 10,571 | 17% |
| recipe-laravel-showcase-fullstack | 12 | 75,945 | 9,574 | 12% |
| cadence-multiservice-build-run2-replay | 28 | 94,057 | 6,069 | 6% |
| discover-adoption-state-resumable-uses-sessionid | 13 | 33,592 | 5,668 | 16% |
| discover-adoption-state-resumable-uses-sessionid | 13 | 34,160 | 5,668 | 16% |
| launch-production-existing-project-token | 12 | 31,567 | 5,537 | 17% |
| kanban-laravel-minimal-dev-only | 20 | 77,427 | 5,380 | 6% |
| launch-production-new-project-push-mode | 7 | 24,944 | 4,811 | 19% |
| launch-production-from-standard-pair | 9 | 30,220 | 4,811 | 15% |
| launch-production-dev-only | 7 | 27,867 | 4,811 | 17% |
| launch-production-laravel-showcase | 8 | 31,437 | 4,811 | 15% |

## 3. Most re-sent content blocks (across sessions, counted once/session)

Leaf blocks that appear in the most distinct runs — candidates for surface-once / single-owner.

| seen in N runs | block bytes | prefix |
|---|---|---|
| 360 | 5620 | Bootstrap is **infrastructure-only**: create services, mount filesystems, discover env var… |
| 355 | 167 | SUCCESS WHEN: plan submitted via zerops_workflow action=complete step=discover with valid … |
| 352 | 208 | SUCCESS WHEN: all plan services exist in API with ACTIVE/RUNNING status AND service types … |
| 265 | 433 | This is the route-menu phase (kind="route-menu") — NO session is open yet. Pick one option… |
| 264 | 141 | Values showing ${...} are cross-service references — resolved inside the running container… |
| 231 | 334 |   Client-side pre-flight rejects this with `INVALID_ZEROPS_YML` before any build triggers,… |
| 229 | 451 |   1. The build container assembles the artifact from the upload + any `buildCommands` outp… |
| 229 | 277 |   In a self-deploy, `sourceService == targetService` — the runtime is both   the build sou… |
| 215 | 210 |   - Dev-mode dynamic runtime: edit code in place; reload via     `zerops_dev_server` (no f… |
| 191 | 1555 |   - **Runtime user is `zerops`, not root.** Package installs need `sudo`     (`sudo apk ad… |
| 189 | 221 |   ZCP pre-flight does NOT check cross-deploy path existence; Zerops   builder emits `WARN:… |
| 189 | 376 |   - **VERDICT: PASS** → service verified, proceed.   - **VERDICT: FAIL** → visual/function… |
| 185 | 420 |   \| Setup block purpose \| deployFiles \| Why \|   \|---\|---\|---\|   \| Self-deploy (de… |
| 185 | 247 |   **Suppress restart**: pass `skipRestart=true`; response reports   `restartSkipped: true`… |
| 185 | 343 |   Auto-close is gated on `closeDeployMode` being set for every in-scope   service — `unset… |
| 185 | 195 |   Scaffold `zerops.yaml` if absent or refine it in place if already   present. The file li… |
| 185 | 595 |   ```yaml   zerops:     - setup: <hostname>       build:         base: <runtime-only key, … |
| 185 | 447 |   \| Channel \| Set with \| When live \|   \|---\|---\|---\|   \| Service-level env \| `ze… |
| 185 | 467 |   \| Class \| Trigger \| `deployFiles` constraint \| Typical use \|   \|---\|---\|---\|---… |
| 185 | 201 |   **Env var references** use `${hostname_KEY}` syntax — Zerops rewrites   the placeholder … |