# brief-scaffold-workerdev-showcase-diff.md

**Purpose**: diff against [../01-flow/flow-showcase-v34-dispatches/scaffold-workerdev-nestjs-worker.md](../01-flow/flow-showcase-v34-dispatches/scaffold-workerdev-nestjs-worker.md). v34 length: 8668 chars / 228 lines.

## 1. Removed from v34 → disposition

| v34 segment | Disposition | New home |
|---|---|---|
| L1–2 recipe-slug preamble (`nestjs-showcase`) | dispatcher → DISPATCH.md | Stitcher interpolates; sub-agent framing is generic. |
| L14 "zcp orchestrator + SSHFS mount" | load-bearing → atom | `principles/where-commands-run.md`. |
| L16–22 TOOL-USE POLICY (duplicated with MANDATORY body) | **scar tissue** | `briefs/scaffold/mandatory-core.md` transmits once. |
| L24–32 `<<<MANDATORY>>>` wrapper sentinels | dispatcher → DISPATCH.md | Atom concatenation makes wrapper redundant. |
| L34–36 "⚠ CRITICAL: where commands run" short version | load-bearing → atom | `principles/where-commands-run.md` positive form. |
| L38–46 "## Service plan context" prose listing `${queue_hostname}`, `${queue_port}`, `${queue_user}`, `${queue_password}`, `${db_*}` | load-bearing → atom + contract | `SymbolContract.EnvVarsByKind.queue` + `.db` provide the runtime names (`NATS_*`, `DB_*`) byte-identical across dispatches. v34 listed platform-side names and left mapping implicit. |
| L48–50 "## What to scaffold" prose | load-bearing → atom | `briefs/scaffold/worker-codebase-addendum.md`. |
| L52–58 "### Step 1 — Scaffold Nest" `ssh workerdev "cd /var/www && npx -y @nestjs/cli new ..."` | load-bearing → atom | `framework-task.md`. |
| L60–62 "### Step 2 — Install deps" | load-bearing → atom | `framework-task.md` with install command literal retained. |
| L64–143 "### Step 3 — Write files" body (main.ts / app.module.ts / job.entity.ts / worker.service.ts / .gitignore / .env.example) | load-bearing → atom | `worker-codebase-addendum.md` — file bullets preserved verbatim; code block for worker.service.ts kept in full (it's the load-bearing reference pattern for queue group + drain sequence). |
| L145–147 "Principle 4 — use NATS queue group. Record a fact. / Principle 1 — `onModuleDestroy` drains..." | load-bearing → atom | Pointer-included `principles/platform-principles/01.md` + `04.md` + `05.md`; addendum repeats the "record a fact per principle" contract. |
| L149 "**`.gitignore`**, **`.env.example`** — as in standard Node projects." | load-bearing → atom | FixRule `gitignore-baseline` + `env-example-preserved` + addendum bullet. |
| L151–155 "**DO NOT WRITE:** README.md ... zerops.yaml ... .git/ ... Real job handlers" | load-bearing (positive form) → atom | Files-you-do-not-write counter-list in `worker-codebase-addendum.md`. |
| L157–213 Pre-ship self-verification bash aggregate | load-bearing → atom (contract-driven) | FixRecurrenceRules with worker-applicable preAttestCmds + reminder snapshot. |
| L215–223 Build verification + "Reporting back" | load-bearing → atom | `pre-ship-assertions.md` + `completion-shape.md`. |

## 2. Added to new composition

| Content | Closes defect class |
|---|---|
| `SymbolContract` JSON (byte-identical across scaffolds) | **v34-cross-scaffold-env-var** (DB_PASS/DB_PASSWORD). In v34 the workerdev scaffold independently decided `DB_PASSWORD` while apidev used `DB_PASS`. Contract eliminates the divergence. |
| `FixRule queue-group` with preAttestCmd and AppliesTo `["worker"]` | Generalizes the v22 "queue group missing" class — the v34 worker brief had a grep assertion but no cross-codebase contract to tie `queue: 'workers'` to a shared symbol. Now `contract.NATSQueues.workers = "workers"` lives in the contract and the FixRule enforces it locally. |
| `FixRule graceful-shutdown` applies to `["api","worker"]` | **v30 worker SIGTERM missing** + **v31 apidev enableShutdownHooks missing** merged into one rule. v34 handled them as separate ad-hoc pre-ship asserts. |
| `FixRule nats-separate-creds` applies to `["api","worker"]` | **v22-nats-url-creds**. v34 had the grep assertion; the FixRule formalizes the rule and shares it across dispatches. |
| `Relevant contract sections` prose — reminds worker role which parts of the contract are load-bearing for its scope | Closes the ambiguity class where v34 listed "NATSSubjects: jobs.scaffold, jobs.process, jobs.dispatch" and expected the worker to guess which it owns. Now explicit: "this scaffold subscribes jobs.scaffold only; feature adds jobs.process." |
| Explicit note "worker's TypeOrm has `synchronize: false` — api owns migration; at deploy the order is apidev migrate → workerdev start" (proposed by simulation I2 discussion, kept as an addendum note) | Future-proofing against cross-codebase deploy-ordering class (v31-era concern) |

## 3. Boundary changes

| Axis | v34 | New |
|---|---|---|
| Audience | Mixed dispatcher + sub-agent | Pure sub-agent |
| Env-var names | `${queue_hostname}` etc. (platform-side, lowercase) | `NATS_HOST` etc. (runtime, UPPER_SNAKE via contract) |
| Queue group | prose + grep ("must reference queue:") | contract JSON literal `"workers"` + FixRule preAttestCmd `grep -qE "queue:\s*['\"]workers['\"]"` |
| Principles | inline prose | pointer-included atoms + contract FixRules |
| Job entity | prose "copy the shape from the API codebase" | explicit JobDTO interface name in contract.DTOs |

## 4. Byte-budget reconciliation

| Segment | v34 | new | delta |
|---|---:|---:|---:|
| Preamble + framing | ~700 | ~150 | -550 |
| Duplicated MANDATORY | ~700 | 0 | -700 |
| where-commands-run prose | ~350 | ~250 | -100 |
| Service plan context | ~450 | ~200 (contract subset + prose summary) | -250 |
| Scaffold task + install | ~550 | ~450 | -100 |
| File addendum (main.ts, app.module.ts, job.entity, worker.service.ts) | ~3500 | ~3400 (minor trim; code blocks kept verbatim) | -100 |
| Platform principles | ~250 | ~200 | -50 |
| DO NOT WRITE | ~200 | ~180 | -20 |
| Pre-ship aggregate | ~1500 | ~1200 | -300 |
| Build / reporting | ~350 | ~350 | 0 |
| **Total** | **~8.7 KB** | **~8 KB** | **-700 B (~8% reduction)** |

Worker-dispatch reduction is smaller than api/frontend because the worker brief was tighter to begin with — less dispatcher bloat. The structural wins (contract, FixRules, positive-form atoms) are present even though raw byte reduction is modest.

## 5. Silent-drops audit

Every v34 line/section covered above. No silent drops.
