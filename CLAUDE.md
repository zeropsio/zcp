# ZCP — Zerops Control Plane

Single Go binary: MCP server + CLI for managing Zerops PaaS.

---

## Where knowledge lives

**CLAUDE.md is a MAP, not a knowledge store.** It records where each kind of
knowledge authoritatively lives + the disciplines that have no other home. It
NEVER caches product knowledge that already lives in a spec / test / code — a
cached copy rots invisibly and can't be traced to its source. To answer a
question, go to the home.

Source of truth, by authority:

```
1. Tests (table-driven, executable)   ← behavior
2. Code (Go types, interfaces)        ← implementation
3. Specs (docs/spec-*.md)             ← workflow design
4. Plans (plans/*.md)                 ← TRANSIENT roadmap (expires; never cite as a source)
5. CLAUDE.md                          ← this map + the disciplines below
```

Every kind of knowledge has ONE home — record it there, never here:

| Knowledge | Home |
|---|---|
| Behavior invariant (what the system does) | a **test** + the **spec** section |
| Implementation mechanism (how) | the **code** + its doc-comment |
| Workflow / design decision | **docs/spec-*.md** (promoted from a plan) |
| Zerops platform fact | **../zerops-docs/** or live verification (never training data) |
| Recipe / framework gotcha | **internal/knowledge/recipes/\<slug\>.md** |
| Universal platform mechanic | an **atom** (`internal/content/atoms/`) |
| How to work (not a system fact) | **CLAUDE.md** (here) |
| Dev-loop ops (SSH, eval, flow-eval) | **CLAUDE.local.md** |
| Transient roadmap / journal | **plans/** |

Key specs:
- `docs/spec-workflows.md` — workflow steps, envelope/plan/atom pipeline; deploy + close-mode (§4.3), deploy modes (§8 DM), pipeline (§8 P4), launch-production (§10), export (§9)
- `docs/spec-work-session.md` — per-PID Work Session, compaction survival, auto-close (§7.5)
- `docs/spec-knowledge-distribution.md` — atom corpus authoring contract (§11)
- `docs/spec-knowledge-architecture.md` — knowledge retrieval (tools-only, single fetch)
- `docs/spec-content-surfaces.md` — recipe content-quality contract; fact classification
- `docs/spec-authoring-boundary.md` — maintainer-only authoring domain, ZCP_AUTHORING gate
- `docs/spec-guided-mode.md` — user-only `zcp init --guided`: local marker, AGENTS.md block + skill, authoring mutual-exclusion
- `docs/spec-architecture.md` — per-package map; `docs/spec-local-dev.md` — local vs container
- `docs/spec-capture-inspector.md` — capture evidence contract, local UI security, and cold CLI-only inspector boundary
- `docs/spec-welcome-mode.md` — agent-first mode: FE-driven onboarding (bidirectional bridge: announce/set-mode/launch-agent/agent-ready §4), container agent panel, terminal-only launch, versioned bootstrap install, skill packs + guided, FE wizard contract (§8)
- `docs/spec-scenarios.md` — per-phase walkthroughs (pinned by `scenarios_test.go`)
- `docs/spec-testing-architecture.md` — test+eval surface map: tier rule (offline/api/e2e/eval), api/e2e vs behavioral division, drift guards, scenario manifest
- `docs/schema-integration.md` — schema validation ownership
- `docs/spec-oss-port-flow.md` — gated `zerops_port` tool (foreign OSS → curated recipe)
- `docs/spec-dataconsole.md` — Managed Data Console: code-isolated managed-service data viewer/editor; caller-bound write-token posture, embed/standalone reach, family taxonomy, value-fidelity wire contract (§7), install
- `docs/spec-dataconsole-testing.md` — Data Console testing architecture: 5-tier map, ServiceProfile proof-coverage rule (declared ⇒ proven), typed manifest + engine×proof matrix gate, dc-live/dc-live-remote lanes, version policy

### Subsystem invariants live in their specs (read the home when you touch the subsystem)

- Authoring boundary (`internal/authoring/`, ZCP_AUTHORING gate, L1/L2 laws + C1-C5 contracts, self-register-inside-gate) → `spec-authoring-boundary.md`; depguard `core-not-authoring`/`authoring-allowlist` + `TestAuthoringBoundary_*`.
- Guided mode is a LOCAL per-project marker (`.zcp/state/guided`), never on `runtime.Info`; the AGENTS.md guided block + skill render iff `guided && !Authoring` (user-only, authoring mutual-exclusion) → `spec-guided-mode.md`; `TestBuildAgentsMD_AuthoringExcludesGuided`.
- Deploy delivery is DERIVED from `GitPushState`, not chosen by close-mode: `configured` ⇒ commit+push is the terminal act for every close-mode except `manual`; legacy `git-push` close-mode folds to `auto` → `spec-workflows.md §4.3`/S5; `TestResolve_ConfiguredDrivesGitPushDelivery`.
- Deploy mode asymmetry (self vs cross; DM-2 self-deploy deployFiles=`[.]` gate; no client-side source stat-check) → `spec-workflows.md §8` DM-1…DM-5.
- Runtime meta is pair-keyed (one meta per dev/stage pair; index via `ManagedRuntimeIndex`/`FindServiceMeta`, never key on `m.Hostname` alone) → `spec-workflows.md §8 E8`.
- Check-before-mutate for non-idempotent platform APIs (read REST-authoritative state, short-circuit) → `spec-workflows.md §8 O3`; canonical `ops.Subdomain`.
- Schema validation ownership (schema=client-side source / topology=classification / platform=authority) → `docs/schema-integration.md`.
- launch-production is pipeline-first: prod import carries NO `buildFromGit`; runtimes start `startWithoutCode:true`, the first release IS the first build → `spec-workflows.md §10`.
- Single-token launch lifecycle: token = `ZCP_LAUNCH_TOKEN` staged service secret (NOT `ZEROPS_`-prefixed; `ZEROPS_TOKEN_PROD` is the GitHub repo secret) → `spec-workflows.md §10.2b`/P-LP-14.
- Export = single-repo `buildFromGit` snapshot (one runtime + N managed deps; HA in type-variant not `mode:`; live `git remote` is source of truth; schema-validate before publish) → `spec-workflows.md §9`/E1-E6.
- Lifecycle recovery is `action="status"` (envelope = recovery primitive; mutations may be terse; errors stay leaf payloads) → `spec-workflows.md` P4.
- Data Console write authority is CALLER-BOUND: a new mutating route gates on the per-request `X-Write-Token` via `safety.Policy.AuthorizeWrite`, never on the read bearer or `X-Confirm` alone → `spec-dataconsole.md §5`; `TestWriteToken_DualClient_CallerBound`.

Live schemas (YAML field validation) are fetched host-derived from `ZCP_API_HOST`
(`schema.URLs`, pinned to `schema.CanonicalAPIHost` for dev tooling). Error codes
catalog: `internal/platform/errors.go`.

**The activity/list SEARCHES are Elasticsearch-backed and LAG writes** — `SearchProcesses`,
`SearchAppVersions`, AND `ListServices` (all `Post*Search`) read an ES index that trails
the DB and is slower when ES is under load. A just-imported service / just-started process
can be ABSENT from these searches for sub-second to several seconds; `ListServices` usually
indexes a new service BEFORE its build process/appVersion show up (live-verified eval 2026-06-30:
List@3.9s, Proc/AppVer@4.3s after a buildFromGit import — variable with load). So **absence in
a search is not ground truth right after a mutation/import**, and a freshly-imported service
can briefly read "idle"/"adoptable" before its in-flight process is queryable. Never conclude
"no such service" / "service is idle" from a single search taken right after an import — re-query.
By-id GETs (`GetService`/`GetProcess`) are direct reads, NOT ES — use them to freshen a search
verdict (the adopt gate's `processStillLive` does exactly this). Project-level direct reads exist
too — `ListServicesDirect` (GET `/project/{id}/service-stack`) + `GetProjectProcessesDirect` (GET
`/project/{id}/process`) are lag-free; `ops.Discover` + `ops.ProjectActivity` use THESE (not the
searches) so a just-imported service + its live process are visible at-creation. The ES searches
stay for history/timeline (`ops.Events`) + resolve/poll callers. Spec: `spec-workflows.md §3.5`.

---

## Architecture — 4 layers + dependency rules

Strict layering: **L1 `platform/`** (raw Zerops API, no ZCP concepts) → **L2
`topology/`** (ZCP vocabulary: `Mode`, `RuntimeClass`, `CloseDeployMode`,
`GitPushState`, `BuildIntegration` + predicates; zero non-stdlib imports) → **L3
`workflow/` ←peers→ `ops/`** (orchestration / discrete operations) → **L4
`cmd/` + `server/` + `tools/`** (MCP + CLI entry points; convert input strings →
typed at the boundary). Per-package map + diagram: `docs/spec-architecture.md`.

**Dependency rule** (pinned by `.golangci.yaml::depguard` + `internal/topology/architecture_test.go`):

| Rule | Reason |
|------|--------|
| `topology/` imports stdlib only | Foundational vocabulary |
| `platform/` imports no internal/ packages | Bottom of stack |
| `ops/` does NOT import `workflow/`, `tools/`, `authoring/` | Peer/upper |
| `workflow/` does NOT import `ops/`, `tools/`, `authoring/` | Peer/upper |
| core does NOT import `authoring/` (composition root: `server/`) | Authoring boundary L1 |
| `authoring/` imports core only via allowlist (topology, schema, knowledge, platform, sync) | Authoring boundary L2 |
| New shared type → `topology/` first, never `workflow/` | Promotion rule |

Cross-cutting packages live under `internal/` (peer, not strict-layered):
`auth/` runs pre-engine, `authoring/` is the ZCP_AUTHORING-gated maintainer
domain, `service/` is exec wrappers. Full list via `ls internal/`.

---

## TDD — Mandatory

RED → GREEN → REFACTOR. Pure refactors skip RED — verify all affected layers stay green.

Change impact — tests at every affected layer must pass:
- Interface/type change in `platform`/`ops` → unit + tool + integration + e2e
- Tool handler change → tool + integration + e2e
- New MCP tool → tool + `annotations_test.go` + integration + e2e

Layers: unit (`./internal/...`), tool (`./internal/tools/...`), integration
(`./integration/` mock), e2e (`./e2e/ -tags e2e` real Zerops). Rules:
table-driven; naming `Test{Op}_{Scenario}_{Result}`; `t.Parallel()` only where
global state allows (document why not); long tests check `testing.Short()`.

---

## Commands

```
make setup             Bootstrap dev env (lint + git hooks)
make lint-fast         ~3s native fast linters
make lint-local        ~15s full golangci lint
go test ./... -short   All tests fast
go test ./... -race    All tests with race detector
```

Knowledge sync (recipe/guide markdown is gitignored — pull before build):
```
zcp sync pull recipes [<slug>] | pull guides
zcp sync push recipes <slug> [--dry-run] | push guides     # → GitHub PR
zcp sync cache-clear [<slug>]
zcp sync recipe {create-repo,push-app,publish,export}
```
Workflow: pull → edit `.md` → push → merge → cache-clear → pull. Config:
`.sync.yaml` + `.env STRAPI_API_TOKEN`.

---

## How to work

### Dev flow — `/flow`
Non-trivial changes run through the `/flow` skill (`.claude/skills/flow/`):
FRAME → PROVE (load-bearing assumptions proven live on `zcp-eval-clean`) →
SHAPE (Codex plan gate; owner approves register + spec promotion) → BUILD
(AFK worktree slices, RED replay) → ASSEMBLE (battery + owner retest pack) →
LAND (spec reconciliation, archive). Small safe fixes take the LITE path
(`problem-solving` + one slice). Details live in the skill, not here.

Code conventions:
- **Service by hostname** — agents/tools speak hostnames; resolve to ID internally.
- **Shell/SQL composition** — POSIX single-quote via `shellQuote()`; never `fmt.Sprintf` to compose shell/SQL.
- **Error wrapping** — `fmt.Errorf("op: %w", err)`; never bare `return err`.
- **File splits driven by cohesion, not line count.**
- **English everywhere** — code, comments, docs, commits.
- **Phased refactors** — verify each phase before continuing; no half-finished states.
- **Rename safety** — no AST-aware tooling; grep calls, types, strings, tests separately.

Knowledge authoring:
- **Atoms are single-owner** — describe observable state / orchestration / concepts / pitfalls; never spec IDs, handler-behavior verbs, invisible-state fields, plan paths, or env-only title qualifiers. Extend `internal/content/atoms_lint{,_axes}.go`, never add per-topic atom tests. `TestAtomAuthoringLint`. Spec: `spec-knowledge-distribution.md §11`.
- **Recipe-specific findings** (framework / library / dev-workflow gotchas) → recipe `.md` (`internal/knowledge/recipes/<slug>.md`, then `zcp sync push recipes <slug>`), never atoms (platform-mechanics-only) or audits (transient). Classification: `docs/spec-content-surfaces.md`.

Do NOT:
- Use global mutable state (except `sync.Once` for init).
- Use `replace` directives in `go.mod`.
- Use `interface{}`/`any` when the concrete type is known.
- Use `panic()` — return errors. Skip error checks (`errcheck` enforces).
- Write tests + implementation in the same commit without RED first.
- Add `t.Parallel()` to packages with global state without thread-safety first.
- Hold mutexes during I/O — copy under lock, release, then I/O.

---

## Traps — an agent can violate these at a NEW call site before a test catches it

- **A corrective `zerops_deploy` is non-destructive and is NEVER gated** — only `zerops_import override=true` (+ DM-2 self-deploy) is a hard gate. A redeploy of a FAILED / READY_TO_DEPLOY-with-failed-history target keeps the prior appVersion serving; gating it deadlocks recovery and forces the agent into the destructive override escape. `TestDeployLocal_*Proceeds`, `TestImport_OverrideOnFailedRequiresAck`.
- **Service "busy" = a LIVE process (`PENDING`/`RUNNING`/`ROLLBACKING`/`CANCELING`) referencing it — the SOLE busy-truth, always carrying a cancelable `processId`** (`ops.ProjectActivity`/`ops.IsProcessLive` own it; the adopt gate + discover `activity` steer both read it). The appVersion `BUILDING`/`DEPLOYING` is ONLY a phase LABEL, never an independent busy signal — an appVersion-only gate deadlocks (a stuck `BUILDING` has no process to cancel). A FAILED/terminal build is never busy: never key on mere stack.build presence / `WAITING_TO_BUILD` (the <1s fast-fail leaves a `FAILED` process + frozen `WAITING_TO_BUILD`); gating those deadlocks recovery. The adopt gate freshens via `GetProcess` so a stale search row can't deadlock it. `TestProjectActivity`, `TestAdoptGate_*`.
- **Credentials are user-owned** — `GIT_TOKEN` / launch token are secrets the agent NEVER fabricates: on `GIT_TOKEN_INVALID`/`MISSING` it surfaces the contract and asks the user (`appendCredentialContract`); the launch token enters the conversation once, or — on a user-granted one-time platform delegation — zero times (the platform mints it, not the agent), and never crosses response / state / audit surfaces. `TestConvertError_CredentialContract`.
- **Subdomain auto-enable is the deploy handler's job** — predicate is mode-allowlist + `IsSystem()` ONLY; NEVER read DTO `SubdomainAccess`/`Ports[].HTTPSupport` as import intent. launch-prod strips it (P-PROD-2). `TestServiceEligible_*`, `TestBuildLaunchBundle_StripsSubdomainAccess`.
- **Telemetry ingest `clientIP()` keys on balancer-authoritative `X-Real-IP`, NEVER client `X-Forwarded-For`** — the Zerops L7 balancer appends client XFF left-most (spoofable) and overwrites X-Real-IP; every IP-rate-limited / blocklisted ingest route routes through `clientIP` (netip-canonical, `RemoteAddr` fallback for in-project), and `INGEST_BLOCK_IPS` entries canonicalize identically. On the shared `*.zerops.app` subdomain X-Real-IP is the constant proxy addr (one global bucket) — a v1 test-endpoint tradeoff, custom domain (origin IP) for v2. `TestClientIP_SpoofedXFF_Ignored`. → `docs/spec-telemetry.md §6`.
- **Lifecycle readers MUST use `workflow.IsOpen`/`DeriveCloseState`** — never a raw `ws.ClosedAt == ""`. Auto-close is derived, not stamped, so a done session keeps `ClosedAt==""` on disk; a raw read reads it as open and stuck-loops the agent. `TestNoRawClosedAtReads`.
- **Repo URL `.git`/slash suffix is not identity** — every `buildFromGit` EMIT + every repo-URL identity COMPARE routes through `topology.CanonicalRepoURL` (a raw `.git` → clone-preflight FAILED in ~0.3s, no logs); store `meta.RemoteURL` verbatim. `TestCanonicalRepoURL*`.
- **SSH-error sites must wrap via `tools.withSSHStderr`** — `SSHExecError.Error()` drops the distinguishing git stderr (it lives in `.Output`). `TestGitPushSetupContainer_ProbeFailure_SurfacesGitStderr`.
- **Agent-facing markdown: tool-call form only, never a bare backticked `zerops://`** — ZCP is tools-only (no MCP resources protocol / no `capabilities.resources`); emit `zerops_knowledge uri="zerops://..."`. `TestNoBareZeropsURIInAgentContent`, `TestServer_DoesNotAdvertiseResourcesCapability`.
- **Managed-section/REFLOG markers match line-anchored only** — every marker lookup routes through `content.IndexMarkerLine`, never raw `strings.Index`/`Contains` (guarded by `TestNoRawMarkerMatching`): a mid-line marker MENTION in prose (agent documenting ZCP in CLAUDE.md) would be cut as structure, truncating user lines at `-->`. Corollary: a file with NO anchored block (markerless, or a block damaged by the old bug) is PREPENDED, never has content above a REFLOG dropped — the damaged population is exactly who the fix protects. `TestInit_MidLine*`, `TestInit_Damaged*MD_NoDataLoss`.
- **A new `zcp init` step is FATAL until you say otherwise** — `zcp init` is a `run.init` command, so its exit code gates the container start; every step that is not the deliverable registers `step.degraded` (naming what breaks), and no step repairs/replaces an environment-owned path it finds broken — it names the entry and continues. `TestRun_SkillRoots_UnusableRoot_DegradesNotFatal`, `TestRun_RequiredStepFailure_Aborts`.
- **JSON-only stdout** — debug to stderr; MCP STDIO depends on it (CLI under `cmd/` exempt). `TestNoStdoutOutsideJSONPath`.
- **`tools/eval/cmd` never call `client.{ListServices,GetServiceEnv,GetProjectEnv}` directly** — go through `ops.*` so caching/retries/instrumentation land at one site. `TestNoDirectClientCallsInToolsEvalCmd`.
- **Editing any `vscode-bootstrap-*` template ships ONLY with a `BootstrapExtVersion` bump** — code-server reloads off the extensions.json index version (versioned immutable install dirs), never file mtimes; an unbumped edit builds fine but NEVER reaches a running fleet. `TestBootstrapExtVersion_ParityWithManifest`.
- **Service userData writes REQUIRE `sensitive` and are HAND-ROLLED** (`platform.CreateServiceEnvVar`) — never route a new env write through the SDK's `UserDataPost/Put` handlers (their bodies lack the field; the API rejects the POST with `field is required`); every service-scope writer passes the flag explicitly. `TestSDKUserDataBody_StillLacksSensitive`, `TestCreateServiceEnvVar_SendsSensitiveInBody`.

---

## Maintenance — keep this file a map

Default home for durable knowledge is its proper home (table above), NOT here.

**Adding knowledge** — route, stop at the first match:
- behavior → a test + the spec section · mechanism → code doc-comment · design
  decision → `docs/spec-*.md` (promote the plan first — never cite `plans/*.md`
  as a source) · platform fact → `../zerops-docs/` or live · recipe gotcha →
  `recipes/` · universal mechanic → atom · how-to-work → here · dev-loop →
  CLAUDE.local.md.
- CLAUDE.md earns a line ONLY when: (a) a cross-cutting **trap** an agent can
  violate at a new call site before the test catches it, or (b) it is not
  test-/spec-shaped (a discipline). Both stay one line and point at the test.
- Never put here: a copy of a sourced value · `Spec: plans/*.md` · a commit
  hash · a date · a "Pinned by Test…" dump · a journal entry.

**Pruning** (on every touch + every plan→spec promotion):
- A spec section now states the same invariant → delete the line (the spec wins).
- Archive residue (hash, date, "DELETED 20xx", "fixed in <hash>") → delete on
  sight; `git log` is the changelog.
- A line pointing at an archived plan → promote-or-delete.

**Budget**: Traps + the subsystem-invariant index stay under ~30 lines. Over
budget means a plan→spec promotion is overdue, not that the budget is wrong.

Other: plan completed → `git mv plans/X.md plans/archive/X.md`. New error code →
declare in `internal/platform/errors.go`. New global state → document in the
package's seed test as `// non-parallel: <reason>`.
