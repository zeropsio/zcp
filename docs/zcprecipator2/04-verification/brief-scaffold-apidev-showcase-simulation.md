# brief-scaffold-apidev-showcase-simulation.md

**Purpose**: cold-read simulation of the composed scaffold-apidev brief. I read the composed brief (`brief-scaffold-apidev-showcase-composed.md`) as if with no prior context — no recipe-version-log knowledge, no recipe.md exposure, no v34 memory. I record ambiguities, contradictions, and impossible-to-act-on instructions. §5 records the P7 verdict.

Cold-read session rules:
- I pretend I have read only the transmitted brief.
- I have access to the tools the brief permits. I do not have access to `docs/zcprecipator2/DISPATCH.md`, `recipe-version-log.md`, `principles.md`, or the atomic tree outside what has been pointer-included in the composition.
- Any reference I cannot resolve from the transmitted text is an ambiguity, not "something I already know."

---

## 1. Ambiguities

| # | Locus | Ambiguity | Severity |
|---|---|---|---|
| A1 | `api-codebase-addendum.md` "CACHE_STORE provider wrapping cache-manager-redis-yet" | The brief says "a CACHE_STORE provider wrapping cache-manager-redis-yet" without naming the binding token or showing import syntax. v34 dispatch had the same issue; scaffolder left `check the installed package for the exact import` in the prompt. Same ambiguity reproduced here. | medium |
| A2 | `symbol-contract-consumption.md` FixRule `env-self-shadow` | `preAttestCmd` reads "(later, once zerops.yaml exists; main-agent substep)". As a sub-agent, what do I do with this rule at my substep? Skip? Record it for the main agent? Unclear from the atom itself. | medium |
| A3 | `api-codebase-addendum.md` "Keep the framework-provided `.env.example` if the scaffolder emitted it" | NestJS scaffolder does not emit `.env.example` by default. Rule requires the file at pre-ship. Creating one is implied but not explicit. | low |
| A4 | `symbol-contract-consumption.md` `HTTPRoutes` | Only `health` + `status` are in the contract. api-codebase-addendum says routes /api/items etc. are feature-sub-agent work. The contract-consumption atom doesn't clarify that the feature-route names are not yet contract members — a reader could interpret "if it's not in the contract I own it." Since feature routes are out-of-scope for this role, the effect is benign, but the boundary is implicit. | low |
| A5 | `framework-task.md` step 3 "Read every file the scaffolder emitted" | Nest scaffold emits ~40 files. "Every file" is infeasible without a Glob-then-batch-Read strategy; the atom hints at batching via `principles/file-op-sequencing.md` but doesn't give a concrete glob pattern. | low |
| A6 | `pre-ship-assertions.md` first block (Grep aggregate) vs `symbol-contract-consumption.md` FixRecurrenceRules | Both describe the same checks (0.0.0.0 bind, trust proxy, NATS URL creds, S3 forcePathStyle, .gitignore baseline) but the FixRecurrenceRules are the "canonical contract-driven" version while the addendum block is a free-form copy. If they diverge, which wins? The composition doesn't name a precedence. | **high** — risk that a rewrite of one list leaves the other stale |
| A7 | `completion-shape.md` "with byte counts" | The sub-agent has no direct `wc -c` wrapper on every Write — it would need to add an explicit `ls -l` step post-Write. Minor tooling friction, not blocking. | low |

---

## 2. Contradictions

| # | First statement | Second statement | Resolution needed? |
|---|---|---|---|
| C1 | `mandatory-core.md`: Bash ONLY via `ssh apidev "..."`. | `pre-ship-assertions.md`: the aggregate uses local shell (`grep -q 'foo' $MOUNT/src/main.ts`) — which implicitly runs on zcp against the SSHFS mount. | **yes, clarify**. This is the same pattern v34 used (and it works — a grep against an SSHFS mount path is a file Read, not an execution), but the brief's positive form "Bash ONLY via ssh apidev" could be read as forbidding local grep. The principle is "executables that mutate state / depend on container uid must run via ssh; pure read-only grep against the mount is a file read." The atom should state the carve-out explicitly. |
| C2 | `framework-task.md` step 1 passes `--skip-git` to `nest new`. | `symbol-contract-consumption.md` FixRule `skip-git` says `--skip-git OR rm -rf .git/`. | benign duplication, but: the positive form lists two alternatives where the addendum locks in one. The rule allows either branch; the task shows only one. |
| C3 | `api-codebase-addendum.md`: "Nest global prefix is `/api`" AND `HTTPRoutes` contract names `/api/health`, `/api/status`. | Individual controller decorators like `@Controller('health')` + `app.setGlobalPrefix('api')` combined yield `/api/health`. But the addendum doesn't say "declare controllers without the /api prefix" — a literal reading of "HTTPRoutes /api/health" could cause the author to hardcode `@Controller('api/health')`, producing `/api/api/health`. | **yes, clarify**. The composition should name the convention (setGlobalPrefix + controller names without the prefix). |

---

## 3. Impossible-to-act-on instructions

| # | Instruction | Why it's impossible | Proposed fix |
|---|---|---|---|
| I1 | `symbol-contract-consumption.md` FixRule `env-self-shadow` with placeholder preAttestCmd `(later, once zerops.yaml exists; main-agent substep)` | Sub-agent cannot run a pre-attest command that doesn't exist. Also, the rule's `appliesTo: ["any"]` implies it's relevant at this substep when in fact it is not. | Either (a) filter the rule out of the sub-agent's transmitted list at stitching time when its substep-scope is main-only, or (b) add an explicit `AppliesAtSubstep` field so the sub-agent knows to skip. |
| I2 | Mention of `mcp__zerops__zerops_record_fact` in platform-principles pointer but also permit list — however `record_fact` is not in the v34 scaffold dispatch's explicit permit list; the composed brief adds it. This means it IS in the brief. OK — no impossibility. | — | none (flagged because I double-checked) |
| I3 | `framework-task.md` step 2 install command names `cache-manager-redis-yet` as a package. If the package name changed upstream, the scaffold fails with npm ERR. The brief doesn't say "verify the package name is live." | This is a real installation — and `cache-manager-redis-yet` IS a real npm package (same as v34). Not impossible to act on; the instruction is executable as-is. | none |

---

## 4. Defect-class cold-read scan (would this brief re-ship any v20–v34 class?)

| Registry class | Would the cold-read reader re-ship it? | Evidence |
|---|---|---|
| v17 SSHFS-write-not-exec | No. `principles/where-commands-run.md` is pointer-included with a positive form. | `principles/where-commands-run.md` body resolves cleanly: executables via ssh; mount is write-only. |
| v21 scaffold hygiene (node_modules leak) | No. FixRule `gitignore-baseline` has a runnable preAttestCmd + pre-ship assertion. | `.gitignore` grep in pre-ship script. |
| v22 NATS URL-embedded creds | No. FixRule `nats-separate-creds` + pre-ship grep + contract EnvVarsByKind declares NATS_USER and NATS_PASS as separate fields. | Cold reader has no ambiguity about how to pass creds. |
| v22 S3 endpoint (storage_apiHost vs apiUrl) | **Cold reader concern**: contract names `STORAGE_ENDPOINT` (capitalized) but platform actually emits lowercase `storage_apiUrl`. The Go stitcher upper-cases contract keys — but from the sub-agent's perspective, the only surface it sees is `process.env.STORAGE_ENDPOINT`. That maps to what? No cross-reference. | The contract needs a note: "these are the env-var keys your runtime reads; the platform injects them via env-var map `storage_apiUrl → STORAGE_ENDPOINT`." Without the note, a sub-agent could bypass the contract and read `process.env.storage_apiUrl` directly. |
| v29 env-self-shadow | No (at this role). The rule defers to the main-agent substep. Sub-agent-side writes are `.env.example` only — no `${VAR}` references. |
| v30 worker SIGTERM | N/A to apidev. |
| v31 apidev `enableShutdownHooks` | No. FixRule `graceful-shutdown` + pre-ship assertion. |
| v32 dispatch compression dropping Read-before-Edit | No. `mandatory-core.md` is atomic and stitched verbatim; no dispatcher prose to compress. |
| v32 platform principles missing | No. All 6 principles pointer-included. |
| v33 phantom output tree | N/A to scaffold. |
| v33 Unicode box-drawing | No. `principles/visual-style.md` pointer-included with a positive form. |
| v33 diagnostic-probe burst | N/A to scaffold (feature-role concern). |
| v33 seed ${appVersionId} burn | N/A to scaffold (main-agent `phases/generate/zerops-yaml/seed-execonce-keys.md` territory). |
| v34 DB_PASS / DB_PASSWORD mismatch | No. Contract names `DB_PASS` and is byte-identical across all 3 scaffold dispatches. | This is the v34 structural fix. |
| v34 manifest↔content inconsistency | N/A to scaffold (writer-role concern). |

**Overall**: no defect class re-ships from this brief as-composed, provided ambiguity A6 (canonical FixRules vs addendum pre-ship list divergence) is eliminated and C3 (global prefix) is clarified.

---

## 5. P7 verdict (cold-read + defect-coverage test per principles.md §P7)

| Criterion | Status |
|---|---|
| Cold-reader finishes without unresolvable contradictions | **PASS with 2 caveats** — A6 (precedence) + C3 (global-prefix convention) |
| Cold-reader can reach each FixRule's pre-attest verdict locally | PASS (every FixRule has a runnable preAttestCmd except the placeholder-flagged one, I1) |
| Every v20-v34 defect class applicable to scaffold-apidev has a prevention mechanism | PASS (see §4 table) |
| Brief has no dispatcher-facing text (P2) | PASS. No "compress", "verbatim", "main agent", "dispatcher" tokens. |
| Brief has no version anchors (P6) | PASS. Grep for `v[0-9]+\.[0-9]+` on the composition returns zero matches. |
| Brief has no internal check vocabulary (`_exists`, `*_honesty`) transmitted | PASS (FixRule IDs appear but they are positive-form rule identifiers, not server check names). |
| Brief has no Go-source paths (`internal/`) | PASS. |

**Net**: the brief passes P7 conditional on closing A6 (add a one-liner "FixRecurrenceRules are the source of truth; addendum pre-ship list is a reminder only") and C3 (add "declare @Controller('health')/(status'), not ('api/health')").

---

## 6. Proposed one-line edits

- Insert into `briefs/scaffold/pre-ship-assertions.md` header: `Source of truth for mandatory assertions is the SymbolContract.FixRecurrenceRules list. The assertions below are a reminder snapshot for the api-role subset.`
- Insert into `briefs/scaffold/api-codebase-addendum.md` after the `src/main.ts` bullet: `Controllers declare their own path (e.g. @Controller('health')); the /api prefix is applied once via app.setGlobalPrefix('api').`
- Split `FixRule.AppliesAtSubstep` into the schema or filter out main-only rules at stitch-time.
- Add to `symbol-contract-consumption.md`: `Contract env-var keys (DB_HOST, STORAGE_ENDPOINT, ...) are the names your runtime reads. Platform-side injection mapping (storage_apiUrl → STORAGE_ENDPOINT) is handled by zerops.yaml.run.envVariables authored by the main agent — you consume UPPER_SNAKE keys only.`
