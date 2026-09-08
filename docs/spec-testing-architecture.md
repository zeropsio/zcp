# ZCP Testing Architecture Specification

> **Scope**: The governing MAP for EVERY verification surface — which tier proves
> which behavior, and the rule that decides where a new test lands. The
> test-surface analog of CLAUDE.md's "one home per knowledge": one behavior, one
> verification home, chosen by a TIER RULE, not by convenience.
>
> **Status**: DESIGN — defines the target the consolidation
> (`plans/test-eval-consolidation-impl-2026-06-16.md`, executing the audit
> `plans/e2e-eval-surface-consolidation-2026-06-16.md`) lands against.
>
> This is a map: pointers + disciplines. It caches no test values and no
> per-file dispositions — those live in the tests + the audit. To answer "is
> behavior X covered," go to the tier this spec names + read the test.

---

## 1. The Placement Principle — one behavior, one home, chosen by a rule

Every behavior has exactly ONE verification home. The home is chosen by the
**tier rule** (§2) — "what does proving this actually require" — never by which
file the author was editing.

**The symptom this fixes:** tests land in the wrong tier by copy-paste. A
platform-free guard inherits the `e2e` build tag of the file it was pasted near
and then **never runs** — not in `go test ./...`, not in CI, only behind a
real-platform gate it doesn't need. The 2026-06-16 audit found exactly this:
`update_test.go` (subprocess binary-swap) and `orphan_cleanup_safety_test.go`
(pure `hasTestPrefix` table) were `e2e`-tagged safety guards that executed in
neither the default suite nor CI. The tier rule is the antidote: placement is a
property of the behavior (what it needs to be proved), not the editing context.

---

## 2. The tiers — and the rule that assigns them

There are now **two real-platform build tags** (down from four — `probe` deleted,
`live` folded into `api`; `plans/e2e-eval-surface-consolidation-2026-06-16.md`).
Tiers in increasing cost/coupling:

| Tier | Build tag | What it proves | Cost |
|---|---|---|---|
| **default** | *(untagged)* | everything provable OFFLINE | `go test ./...` |
| **`api`** | `//go:build api` | read-mostly REAL-platform CONTRACT | `ZCP_API_KEY`, read |
| **`e2e`** | `//go:build e2e` | MUTATING real-platform lifecycle | eval-zcp, mutates |
| **UI-drive** | *(not a build tag — Node)* | the ASSEMBLED embedded UI against a live container | puppeteer-core, mutates |
| **behavioral eval** | *(NOT a build tag)* | non-deterministic AGENT-decision quality | full agent run |

**The tier rule** (apply top-down; the first tier that can prove the behavior is its home):

- **default (untagged)** — anything provable without the live platform: units,
  schema/content lint, parser/render, safety guards (e.g. `hasTestPrefix`), the
  subprocess `update` binary-swap test. If it needs no `ZCP_API_KEY` and no
  eval-zcp, it belongs here — and ONLY here.
- **`api`** — read-mostly REAL platform: the SDK-decode / status / error-code
  CONTRACT (what the wire actually decodes to, which no mock can pin honestly),
  the live catalog + recipe audits, the export-composer audit against live
  source. The retired `live` tag's three real audits folded in here; `probe`
  (spent investigation scaffolding) was deleted. `api` asserts the SDK PROTOCOL,
  not tool behavior — which is why it is NOT foldable into `e2e`.
- **`e2e`** — MUTATING real-platform lifecycle: full ZCP handler / MCP-tool runs
  vs eval-zcp (deploy ssh+local, failure classification, bootstrap, export,
  launch single-token lifecycle, verify, subdomain, git-delivery, env-generate).
  The everything-net for deterministic platform truth. The tag also covers a
  second, independent suite on a different PLANE: the Data Console's live
  data-plane conformance harness
  (`internal/dataconsole/console/provider/conformance/`) dials the managed
  engines directly — pg/redis/s3/http/kafka/nats over the project VPN — never
  the Zerops REST API, so it needs no `ZCP_API_KEY`. Same tier semantics as
  the control-plane suite above (mutating, real backends, no retries on
  semantic assertions), just aimed at engine data instead of platform
  lifecycle. `vet-tags` already compiles it (`go vet -tags e2e ./...` walks
  the whole tree), so the one compile rot-guard covers both suites without a
  second Makefile target. The console's own tier map, proof-coverage rule,
  typed manifest/matrix gate, and live-lane operations live in
  `docs/spec-dataconsole-testing.md`.
- **UI-drive** (`internal/dataconsole/uitest/`) — live-only and Node-run, outside
  the Go build-tag system entirely: a `puppeteer-core` harness drives the Data
  Console's ASSEMBLED embedded UI (code-server -> VS Code workbench -> nested
  webview iframes -> the console SPA) against a real deployed container — the one
  layer the `e2e` conformance harness above and the jsdom/HTTP test layers below
  it cannot reach, since a divergence *between* layers (SPA state green while the
  broker is actually read-only), native-control styling, or layout reflow is
  invisible to all of them. `run.js` (scenario registry), `gallery.js` (a
  state-gallery screenshot sweep), and `button-audit.js` (exhaustive control
  enumeration) are its three entry points. Every scenario asserts THREE oracles:
  O-UI (a DOM assertion in the real webview), O-ENGINE (an independent CLI over
  SSH — `psql`/`redis-cli`/`curl`/`mc` — never the console's own API), and
  O-HONESTY (the UI's claim equals engine truth: success implies applied, error
  implies unchanged, refusal implies refused and looks refused). It is never part
  of `go test ./...`, and `make vet-tags` does not compile it — `node run.js`
  against a live container is the only way to run it.
- **behavioral eval** (`eval/behavioral/`, run by `flow-eval.sh`) — the only home
  for **non-deterministic agent-decision quality**: route choice, plan shape,
  env-wiring, blocker comprehension. A markdown scenario corpus + an agent
  runner, NOT a build tag. See §6.

---

## 3. Division of labor — deterministic correctness vs agent navigation

`api`/`e2e` and behavioral eval are **complementary by design, not redundant**:

- **`api`/`e2e`** prove deterministic **handler + platform correctness** — given
  these inputs, the handler does exactly this and the platform reaches exactly
  that state. Assertable, repeatable, gating.
- **behavioral eval** proves non-deterministic **agent navigation** — does the
  agent, free-running, choose the right route / shape / wiring. Observed, not
  asserted (§4).

**The load-bearing rule:** **no e2e is deletable on an "an eval covers it" basis.**
Behavioral verification observes by default and, when `required` (§4, §10),
proves one declared outcome on one fixture — either way an agent-flow eval is
NOT a substitute for a deterministic platform pin. A behavioral scenario exercising
the same surface narrows nothing about the e2e's keep/delete decision. e2e
shrinks only by removing *deterministic* redundancy (one e2e subsuming another's
asserts), never by deferring coverage to an eval.

---

## 4. The eval-as-gate decision — observe by default, required by declaration

Behavioral verification has two modes, selected per scenario by
`verification.mode` (§7, §10.1):

- **`observe`** (the default when the field is omitted — every legacy scenario)
  is **OBSERVATION-ONLY**. The runner writes the same result rows to
  `verification.json` but nothing gates on them; the suite verdict is the
  execution result, and **the local Claude session is the grader**
  (`eval/behavioral/README.md`).
- **`required`** makes the scenario's *deterministic platform assertions*
  (`expectedServices`, `noFailedProcesses`) a hard result: a proven-false
  assertion is `failed`, unavailable evidence is `blocked`, and only a fully
  proven set is `passed`. The CLI exit code carries that result (§10.1).

**What stays advisory, and why:** grading the non-deterministic NAVIGATION
quality of an open-ended agent run is itself a hard, non-deterministic problem;
a flaky LLM-judge gate would block CI on grader noise, not on real regressions.
So `required` mode enforces only what an independent platform read can prove.
Retrospective phrase checks (`retrospectiveMustNotMention`) and the self-review
remain advisory in both modes, and the human stays in the loop where judgment is
irreducible.

**The named inversion (now partially corrected):** the rigor gradient used to run
*backwards* relative to the product's purpose — maximal rigor on
handler-correctness (`api`/`e2e`), minimal on the agent-success the product
exists to deliver (behavioral, warn-only). `required` mode lets one declared
scenario carry a deterministic outcome gate. It does NOT relax §3: a required
scenario proves an outcome on one fixture, not a handler's contract, so no e2e
is deletable on an "a required eval covers it" basis either.

---

## 5. Anti-drift — the compile-guard catches rot it can't see; these catch the rest

`make vet-tags` compiles the `api` + `e2e` tagged files so they can't ROT at the
COMPILE level (a renamed symbol breaks the build). It is blind to **semantic**
rot: a stale string literal compiles fine. The defenses against semantic rot:

- **Scenario drift guards** (`internal/content/eval_scenario_drift_test.go`) —
  `TestNoNeverShippedLaunchVocabInEvalScenarios` (a whole never-shipped DESIGN's
  vocabulary) + `TestNoRetiredMechanicsVocabInEvalScenarios` (individual retired
  MECHANIC literals: `ZEROPS_TOKEN_STAGE`, re-supply-same-launchKey,
  `closeMode=git-push`, `.netrc`). A scenario that names dead vocab trains the
  agent against behavior the shipped handler cannot produce — and burns a
  14-17 min eval cycle with no code fix. Scoped to `eval/behavioral/scenarios{,-local}/`
  deliberately: plans/ and archived design docs legitimately name this vocab as
  history, so the whole-repo gate must not claim it.
- **Single-owner derivation** (`tell == check`) — a test's expected set DERIVES
  from the runtime owner instead of re-listing it. `knowledge_quality`'s service
  key-table derives from `knowledge.serviceNormalizer` (its `Keys()` accessor),
  so a key dropped from the normalizer can't leave a stale expectation behind.
  This is the §3.3 derive-from-code rule of `spec-knowledge-architecture.md`
  applied to the test surface.
- **Scenario-manifest convention** (§7) — the front-matter every behavioral
  scenario carries, enforced by `eval_scenario_manifest_test.go`, so the implicit
  convention can't silently lapse.

`vet-tags` catches COMPILE rot; the drift guards + single-owner derivation catch
SEMANTIC rot. Both are required — neither subsumes the other.

---

## 6. The behavioral corpus — runner, scenarios, retrospective

`eval/behavioral/scenarios/` (container-mode, run over SSH to a zcp container) +
`eval/behavioral/scenarios-local/` (local-mode, run directly on the dev Mac) are
the two scenario directories. `flow-eval.sh` (container) and `make flow-eval-local`
build+ship the binary, run the agent, and pull the retrospective; the local
Claude session reads `self-review.md` and drives analysis. Full architecture +
round-trip protocol: `eval/behavioral/README.md`.

A scenario is a markdown file: YAML front-matter (the manifest, §7) + a body used
verbatim as the agent prompt. `cmd/zcp eval behavioral {list,run,all}` parses it
via `internal/eval/scenario.go`; only scenarios carrying `retrospective:`
(`IsBehavioral()`) are behavioral. A scenario missing it is SKIPPED by `list`/`all`
but ERRORS under `run` (`RunBehavioralScenario` rejects a non-behavioral scenario,
`behavioral_run.go`) — addressing one by id is an explicit choice the runner
refuses rather than silently no-ops. A scenario may additionally declare
`verification.mode: required` (§10.1); the parser rejects it before any
platform call when it has nothing executable to require.

---

## 7. Scenario manifest convention — the enforced front-matter

Every behavioral scenario carries front-matter fields that `flow-eval.sh` + the
`cmd/zcp eval behavioral` runner actually rely on. `eval_scenario_manifest_test.go`
pins the universal set so the convention is enforced, not implicit — with NO new
per-file data (every field below is already universal in the corpus).

**The enforced set** (each is RELIED ON by flow-eval's own parsing — the lint
enforces only what the harness reads, never an invented field):

| Field | Relied on by |
|---|---|
| `id` | `validate()` requires it; `flow-eval.sh <id>` addresses the scenario as `<id>.md` (filename MUST equal `id`); `list` prints it |
| `description` | `list` prints it; the flow-eval descriptor-match (run-by-qualifier) matches against it |
| `seed` | `validate()` requires a valid enum (`empty\|imported\|deployed\|settled`) — drives fixture seeding |
| `tags` | `list` prints them; descriptor-match |
| `area` | `list` prints it; descriptor-match |
| `retrospective` | `IsBehavioral()` gate — a scenario WITHOUT it is invisible to `list`/`run`/`all` |

**Optional, parsed, not universal:** `verification.mode` (`observe` | `required`,
§10.1) is the per-scenario enforce-vs-observe lever. It is validated by
`validate()` but NOT in the enforced set above — omitted means `observe`, and
the corpus is not migrated: a scenario opts into `required` only when it is
touched and its assertions are known to be independently provable.

**Deferred — FUTURE fields, none parsed today:** the audit recommends a richer
curation manifest (`canonical` / `overlaps` / `last-reviewed`) to make de-dup
toward the founding 12-15 matrix mechanical and to give the drift lint an
ownership anchor. These are **NOT mass-added** here — the corpus carries ~55
scenarios and a bulk retrofit is its own curation pass. Add them as scenarios are
touched; the lint enforces only the universal set above until the curation pass
makes the richer set universal too.

---

## 8. Invariants (pin with tests)

- **I1** Every behavior has exactly one verification home, chosen by the tier
  rule (§2) — offline behavior is untagged-only, never `e2e`-tagged.
- **I2** Two real-platform build tags only (`api`, `e2e`); `vet-tags` compiles
  both. (`Makefile::vet-tags`)
- **I3** No e2e is deletable on an "an eval covers it" basis — not for an
  observe scenario, and not for a required one either. (§3, §4)
- **I4** Behavioral scenarios carry the universal manifest set (§7).
  (`eval_scenario_manifest_test.go`)
- **I5** Eval scenarios name no never-shipped / retired vocab.
  (`eval_scenario_drift_test.go`)
- **I6** Test expectations over a runtime-owned set DERIVE from the owner, never
  re-list it (single-owner; e.g. `knowledge_quality` ← `serviceNormalizer`).
- **I7** A `required` scenario's result is one of `passed | failed | blocked |
  not-run`, owned by the result rows — never by the execution error alone, never
  by an empty findings list, never by the retrospective. (§10.1;
  `TestBehavioralOutcome_*`, `TestBehavioralCLI_*`)
- **I8** Task-end evidence is frozen and persisted BEFORE the retrospective and
  before any cleanup; `required` mode performs no destructive post-task cleanup.
  (§10.2; `TestBehavioralOutcome_TaskEndPrecedesRetrospective_Frozen`,
  `TestBehavioralOutcome_PersistenceFailure_RetainsEvidenceAndNoCleanup`)

---

## 9. Relationship to other specs

- `spec-knowledge-architecture.md` — the knowledge-surface analog: one fact, one
  owner, computed delivery. §5's single-owner derivation + drift lints are its
  §3.3 / §5 rules applied to the TEST surface.
- `eval/behavioral/README.md` — the behavioral runner's operational home
  (round-trip protocol, failure modes); this spec governs where eval sits among
  the tiers, the README governs how it runs.
- `docs/spec-authoring-boundary.md` — the `ZCP_AUTHORING`-gated authoring domain
  has its own test surface under the same tier rule (depguard + `TestAuthoringBoundary_*`).

---

## 10. Required results and task-end evidence

The behavioral runner (`internal/eval`, CLI `zcp eval behavioral run`) can be
asked for a **deterministic task result** instead of a warn-only observation.
This section owns that contract. It is deliberately narrow: the result is
derived from independent platform reads over the scenario's declared
assertions, frozen at the end of task work, and carried to the CLI exit code.
It is not an application oracle, an isolation driver, a report, or a comparison
— those are separate contracts with their own gates.

### 10.1 Required results

**Mode.** `verification.mode` is `observe` or `required`. Omitted means
`observe`. Any other value is a scenario parse error. `required` with no
executable check — no `expectedServices` entry and `noFailedProcesses` unset —
is a parse error: `retrospectiveMustNotMention` is advisory and never counts as
executable. Parse errors surface from `ParseScenario`, which runs before seed,
init, and before the runner registers any cleanup, so a rejected scenario makes
**zero** platform calls.

**Rows.** Every executable check yields exactly one **result row**, in both
modes:

| Field | Meaning |
|---|---|
| `id` | stable identity: `expected_service/<hostname>/exists`, `…/status`, `…/type`, `…/subdomain_probe`, `no_failed_processes/<projectId>` |
| `check` | the check family (`expected_service`, `service_status`, `service_type`, `subdomain_probe`, `no_failed_processes`) |
| `scope` | hostname or project id the row is about |
| `result` | `passed` \| `failed` \| `blocked` \| `not-run` |
| `expected` / `observed` | the literal compared values (status list vs status, type glob vs type, `no FAILED process after <start>` vs the process ids found) |
| `observedAt` | UTC time of the direct platform read the row was decided from |
| `source` | the read that produced `observed` (`ListServicesDirect`, `GetProjectProcessesDirect`, `HTTP GET <url>`) |
| `message` | human sentence; never the sole carrier of the verdict |

Row semantics: a proven-false assertion is `failed`; an assertion whose
evidence could not be obtained (platform read error, unresolvable probe URL,
unsettled live process — §10.2) is `blocked`; a proven assertion is `passed`;
a row the runner never reached because task work did not happen is `not-run`.
Rows are never dropped: a `blocked` row sits next to a `failed` one.

**Result.** The scenario's task result aggregates the rows: any `failed` →
`failed`; else any `blocked` → `blocked`; else every row `passed` → `passed`;
no task work (the initial agent invocation did not complete) → `not-run`. A
known failure stays `failed` when another row is blocked. Evidence completeness
(capture status, §10.2 persistence) is a separate dimension and never turns a
`failed` into anything else.

**Artifacts.** `verification.json` is one object, format
`zcp-eval-verification-2`, in both modes:

```json
{"formatVersion":"zcp-eval-verification-2","mode":"required","result":"failed",
 "frozenAt":"…","checks":[…rows…],"advisory":[…legacy findings…]}
```

`advisory` carries the legacy `{severity, check, message}` findings projected
from the rows (`failed` → `fail`, `blocked` → `warn`) plus the retrospective
phrase findings, so operators keep the familiar shape; it never carries a
verdict the rows lack. The legacy bare-array format is retired.
`platform-snapshot.json` keeps owning the observed services/processes. Whether
a scenario ran, how it ended, and what the task result is are three separate
dimensions of `meta.json`:

- `error` — execution meaning only, unchanged (seed/init/spawn/extract failure);
- `task` — `{mode, result, frozenAt}`;
- `taskEnd` — `{observedAt, settled, liveProcesses, simulator:{terminatedBy,
  error}, persisted, persistError}` (§10.2).

**CLI acceptance.** For `zcp eval behavioral run`:

- `observe`: exit 0 iff `error` is empty — the legacy execution-only exit.
- `required`: exit 0 iff `error` is empty AND `task.result == passed` AND
  `taskEnd.persisted` AND the invocation's own scoped capture window closed
  `complete` with a manifest that validates (capture spec §6). The CLI prints
  the three dimensions (`Execution`, `Task`, `Task-end evidence`) explicitly;
  a nonzero exit names which one failed.
- `required` refuses to start — before init, seed, or cleanup registration —
  unless its capture is the private scoped window created by this invocation's
  own `--capture raw`. A global or inherited window is refused (capture spec
  §6); observe mode keeps attaching to whatever is on.
- A capture close or manifest failure is nonzero even when the task passed; the
  inner runner result is provisional until the window is finalized.

`behavioral all` applies the same per-scenario acceptance and counts a
non-passed required scenario as a failure.

### 10.2 Task-end freeze and retention

**Task end** is the instant after the initial agent invocation and every
user-simulation resume have returned, and before the retrospective. In
`required` mode it is recorded once and is the only point the task result is
decided. `observe` mode keeps its legacy ordering: its rows are decided by one
platform read taken AFTER the retrospective (so the advisory phrase check can
see the self-review), and nothing gates on them.

At task end the runner, in this order:

1. records how the simulator stopped (`terminatedBy`, loop error) — recorded,
   never graded: a `DONE` classification is not a pass;
2. confirms every owned Claude child has exited (the runner spawns them
   synchronously, so this holds by construction and is recorded, not polled);
3. takes one direct platform observation (`ListServicesDirect` +
   `GetProjectProcessesDirect`). If any process created after scenario start is
   live (`PENDING`/`RUNNING`/`ROLLBACKING`/`CANCELING`) it re-observes at an
   interval until settled or the settle budget (`RunnerConfig.TaskEndSettle`,
   default 60 s) elapses. Unsettled → `taskEnd.settled=false`,
   `liveProcesses` lists the ids, and every platform-state row is `blocked`
   (observed = those ids) — the result is never `passed` over an in-flight
   mutation;
4. decides the rows and result (§10.1);
5. persists `platform-snapshot.json`, then `verification.json`, then
   `meta.json` (each temp file + fsync + rename). A persistence failure sets
   `taskEnd.persisted=false` with `persistError`, turns a would-be `passed`
   into `blocked`, leaves a `failed` as `failed`, and never removes anything
   already written. The order is what keeps the frozen artifacts in
   agreement: a snapshot failure is known before `verification.json` is
   written and lands in its `result`; a later `meta.json` failure leaves no
   `meta.json` to disagree with.

Only after step 5 does the optional retrospective run. A retrospective failure
lands in `error` (execution) and never in `task.result`; the bytes of
`verification.json` are identical before and after the retrospective. The
self-review is diagnostic text beside the frozen result, not an input to it.

**Retention.** In `required` mode the runner performs **no** post-task cleanup:
no service deletion, no work-dir wipe, no state reset — the project and the
result directory stay for the operator to copy and then clean explicitly. The
CLI claims no copy and no cleanup. `observe` mode cleans up after the
retrospective exactly as before. Bundling the result directory into the capture
window happens after the freeze; a bundle failure marks the window `partial`,
which the required CLI acceptance reads as a failed evidence dimension.

**Probe honesty.** An `expectedServices[].subdomainProbe` whose URL cannot be
resolved from the platform read is a `blocked` row, never a silent pass; when a
URL resolves, the row is decided by an actual HTTP request. Resolving the URL
and proving application behavior over HTTP/SQL belong to the application-oracle
contract, not this section.

### 10.3 One application oracle: Node runtime + managed PostgreSQL

The platform rows of §10.1 prove that services exist and are ACTIVE; they
cannot tell a working application from one that answers HTTP 201 and stores
nothing. This section owns the one independent application check the
behavioral runner carries: fixture-specific, verifier-held values, an
independent database read. It is not a verifier registry, a DSL, or a
generic HTTP/SQL assertion language — a second application shape gets its
own section and its own gate.

**Declaration.** A scenario may declare

```yaml
verification:
  mode: required
  nodePostgresRecord:
    stage: appstage      # Node runtime serving POST/GET /records
    database: db         # managed PostgreSQL the record must land in
    unrelated: other     # runtime whose deployed artifact must not change
```

All three are hostnames, resolved by the runner through `ListServicesDirect`
at the task-end freeze. The block counts as an executable check for
`required` mode (§10.1). It has exactly this shape; there is no
`application:` umbrella field.

**Rows** (snake_case; this list extends the §10.1 id table):

| id | passed when | failed when | blocked when |
|---|---|---|---|
| `node_postgres_record/<stage>/record_roundtrip` | `POST /records {nonce,value}` → 201 with an `id`, and `GET /records/<id>` returns that `id`, the verifier's `nonce` and `value` | any of those differ, or a non-2xx status | URL unresolvable, HTTP unreachable, or the freeze is unsettled |
| `node_postgres_record/<stage>/environment` | the GET body's `environment` equals the literal `stage` | it differs | as above |
| `node_postgres_record/<database>/db_row` | `SELECT id, value FROM records WHERE nonce = $1` returns exactly one row whose `value` equals the verifier's and whose `id` equals the GET-returned id | zero rows, more than one, or a mismatch (the plausible in-memory fake fails HERE) | DB unreachable, credentials unresolvable, or the freeze is unsettled |
| `unrelated_artifact/<unrelated>/unchanged` | the service's active app-version id at the freeze equals the one recorded at scenario start | it differs | either observation missing |

**Independence.** The verifier generates a fresh unpredictable `nonce` and
`value` per run. The URL comes from `ops.ResolveSubdomainURL` over the
resolved `stage` service — never from anything the candidate printed. The
database connection is built only from the `database` service's
platform-generated env (`ops.FetchServiceEnv`: `hostname`, `port`, `user`,
`password`, `dbName`), and the connect host must equal the `database`
hostname; a user-set override on that service is refused. The SELECT is
parameterised, runs in a read-only transaction, and keys on the nonce —
never on the app-returned id. Redirects, a host other than the resolved
subdomain, or a project other than the run's are refused before any request.
Credentials stay in memory and never enter rows, messages, meta, or the
capture bundle; driver errors are sanitised before they become a `message`.

**Baseline.** Right after seed and init, before the initial agent invocation,
the runner records the `unrelated` service's active app-version id in
`meta.json` as `baseline: {unrelatedAppVersion, observedAt}`. The
`unchanged` row compares against it at the freeze.

**Ordering and settle.** The verifier runs inside the task-end freeze
(§10.2) after the platform rows and only when the observation is settled;
an unsettled freeze emits its four rows `blocked` with zero HTTP or SQL
calls. The existing `subdomainProbe` row resolves its URL the same way
(`ops.ResolveSubdomainURL`); the earlier "unresolvable" stub is retired.

**Calibration before any agent.** The verifier is exported and is proven
standalone, on a manually prepared disposable fixture, before a scenario
relies on it: a known-good app must pass all four rows; a plausible fake that
answers 201/200 with matching JSON but stores only in memory must fail
`db_row` and nothing else. That calibration is an `e2e`-tagged test inside
`internal/eval` that invokes the exported verifier directly — it never calls
the runner, `Seed*`, `CleanupProject`, or the `./e2e` package (whose
`TestMain` deletes prefixed services). Its inputs are `ZCP_EVAL_ORACLE_API_HOST`,
`_TOKEN`, `_PROJECT_ID`, `_STAGE`, `_DATABASE`, `_UNRELATED`,
`_STAGE_ID`, `_DB_ID`, `_OTHER_ID` (cross-checks that must equal the
resolved services), `_OTHER_APPVERSION` (the baseline the operator read before
deploying the fake) and `_ACK_DISPOSABLE_PROJECT`. With no oracle input the
test skips; with any input it either has all of them and matching
cross-checks or fails before any network call. A skipped calibration is not
acceptance. It runs where the managed database is reachable — the disposable
project's own container or a laptop with the project VPN up. A
multi-project VPN resolves services only as
`<host>.<routingDomain>.zerops-project`, never as the bare `<host>`; the
calibration takes that routing domain as `_DB_RESOLVE_DOMAIN` and applies
it to dialling only, after the verifier has already required the managed
env `hostname` to equal the declared database hostname (single-project VPN
and in-project runs leave it empty). The acknowledgement is an operator
assertion the code cannot verify; the code's own protection is the
cross-check refusal.
