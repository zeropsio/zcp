# brief-scaffold-workerdev-showcase-simulation.md

**Purpose**: cold-read simulation of composed scaffold-workerdev brief.

## 1. Ambiguities

| # | Locus | Ambiguity | Severity |
|---|---|---|---|
| A1 | `worker-codebase-addendum.md` `TypeOrmModule.forFeature([Job])` in app.module | The worker module imports Job entity and registers the repository for injection in WorkerService. But cross-codebase schema alignment ("api owns migration") is stated; what if the api scaffold hasn't run yet and the DB lacks the `job` table? Worker bootstrap will crash. | **medium** — worker depends on api at deploy time (init-commands substep). At scaffold substep the worker doesn't connect; it only builds. So the ambiguity is about runtime, not scaffold-time. But clarify that `synchronize: false` means the worker must not attempt to create the table. |
| A2 | `symbol-contract-consumption.md` "NATSSubjects — this scaffold subscribes jobs.scaffold only" | Contract's `NATSSubjects` includes `jobs.dispatch`, `jobs.process`, `jobs.scaffold`. Worker scaffold uses `jobs.scaffold`. Why that particular subject? A cold reader has no cue about which subject is appropriate at which phase. | low — addendum explicitly says "a ping echo at generate-time; feature sub-agent adds jobs.process" |
| A3 | `pre-ship-assertions.md` reminder `grep -q "queue:" $MOUNT/src/worker.service.ts` | The grep matches any line containing `queue:` — including comments. A cold reader could write `// queue: set below` (never set it) and pass. Same false-positive risk exists in v34. | **medium** — stricter regex: `grep -qE "queue:\s*['\"]workers['\"]"` (and runnable via FixRule `queue-group`). |
| A4 | `worker-codebase-addendum.md` — the `JSONCodec` import from `nats` v2 | Depending on `nats` client version, `JSONCodec` may or may not be the canonical codec name. v2 still has it. Not ambiguous if the reader verifies the installed package. | low — cold reader should verify (and the feature-brief's "verify installed-package imports" preamble applies, though not pointer-included here) |
| A5 | `main.ts` bootstrap: `app.enableShutdownHooks()` is called before `worker.start()` — so if SIGTERM arrives during `worker.start()`, the hook is already registered | fine behavior, but unusual pattern. Cold reader might expect shutdown hooks registered AFTER worker.start() | low |

## 2. Contradictions

| # | Statement A | Statement B | Resolution |
|---|---|---|---|
| C1 | "Worker is standalone; no HTTP listener." | FixRule `routable-bind` applies to `api, frontend` — worker not listed. | consistent; noted positively. |
| C2 | "Record a fact per principle after implementation." | `mandatory-core.md` says mcp__zerops__zerops_record_fact is permitted. | consistent. |

No actual contradictions.

## 3. Impossible-to-act-on

| # | Instruction | Why | Fix |
|---|---|---|---|
| I1 | `grep -q "drain"` in pre-ship — matches any drain occurrence even in comments | false-positive risk | Tighten to `grep -qE "\.drain\("` |
| I2 | FixRule `queue-group` preAttestCmd not shown in the brief (applies to worker only — it would be in the filtered list) | Implicit — the worker-role sub-agent is supposed to run every rule with appliesTo including `worker`. The brief doesn't re-emit the preAttestCmd in its reminder aggregate. | Either fold preAttestCmd for queue-group into the reminder aggregate OR strengthen the filter note ("source of truth is FixRecurrenceRules; reminder is a subset"). |

## 4. Defect-class cold-read scan

| Class | Ships? | Evidence |
|---|---|---|
| v17 SSHFS-write-not-exec | No | principles/where-commands-run |
| v21 scaffold hygiene | No | FixRule gitignore-baseline |
| v22 NATS URL creds | No | FixRule nats-separate-creds + pre-ship grep + addendum code literal shows separate user/pass |
| v30 worker SIGTERM missing | **No** — explicit in addendum (`onModuleDestroy` drain sequence), pre-ship grep `drain`, FixRule `graceful-shutdown` | v30 class closed here |
| v22 queue-group missing (generalized) | **No** — `this.nats.subscribe('jobs.scaffold', { queue: 'workers' })` literal + FixRule `queue-group` + pre-ship grep | |
| v33 Unicode box-drawing | No | visual-style atom |
| v34 cross-scaffold DB_PASS/DB_PASSWORD | **No** — contract is byte-identical; worker reads DB_PASS (not DB_PASSWORD) | v34 closure |
| v34 manifest content inconsistency | N/A | writer role |
| v34 convergence architecture | No | author-runnable preAttestCmds |

## 5. P7 verdict

| Criterion | Status |
|---|---|
| Cold-reader finishes without unresolvable contradictions | PASS |
| Each applicable FixRule has a runnable form | PASS (nats-separate-creds, queue-group, graceful-shutdown, gitignore-baseline, env-example-preserved, no-scaffold-test-artifacts, skip-git all runnable) |
| Every applicable v20-v34 defect class has a prevention mechanism | PASS |
| No dispatcher text (P2) | PASS |
| No version anchors (P6) | PASS |
| No internal check vocabulary | PASS |
| No Go-source paths | PASS |

**Net**: passes P7. Minor tightening needed on the grep regexes in the reminder aggregate.

## 6. Proposed edits

- Tighten `grep -q "queue:"` → `grep -qE "queue:\s*['\"]workers['\"]"`.
- Tighten `grep -q "drain"` → `grep -qE "\.drain\("`.
- Add to `worker-codebase-addendum.md`: "the worker bootstraps the TypeOrm data source with `synchronize: false`; if the `job` table is absent at runtime the worker logs an error and crashes on first message — this is intended behavior since the api owns the migration. At deploy-time the ordering is: apidev migrate (execOnce ${appVersionId}) → workerdev start."
- Add pointer-include of the `principles/symbol-naming-contract.md` consumption conventions to surface the "queue group name = `contract.NATSQueues.workers`" link explicitly.
