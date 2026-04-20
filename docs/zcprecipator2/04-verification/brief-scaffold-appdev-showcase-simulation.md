# brief-scaffold-appdev-showcase-simulation.md

**Purpose**: cold-read simulation of the composed scaffold-appdev brief.

## 1. Ambiguities

| # | Locus | Ambiguity | Severity |
|---|---|---|---|
| A1 | `symbol-contract-consumption.md` "rules whose `appliesTo` is api or worker do NOT apply" | The contract JSON does not have an explicit field that says "and you can skip these rules." The sub-agent must filter the list themselves. Simple to do (one-line filter), but the instruction and the data structure are in tension. | low |
| A2 | `framework-task.md` Step 1: `npm create vite@latest . -- --template svelte-ts --skip-git` | Vite's `create-vite` script does not accept `--skip-git`. The brief acknowledges this ("If the flag is unknown, drop it...") but the acknowledgement comes after the command — a cold reader hits npm-ERR before reading the fallback. | **medium** — reorder so the fallback-first shape leads. |
| A3 | `frontend-codebase-addendum.md` `src/app.css` — "CSS custom properties for theme, `.panel` / `.dashboard` / form-input styles" | Too abstract for a scaffold sub-agent who will be asked to produce a specific layout. v34 had the same problem and the appdev sub-agent filled it in with its own taste. Not a defect per se — but a consistency risk across runs. | low |
| A4 | `frontend-codebase-addendum.md` StatusPanel — "reads `/api/status` every 5s" | No explicit stop condition. The component polls forever. Browser tab left open for hours = 720 calls/hour/tab. Minor concern but non-trivial for a code review. | low |
| A5 | `framework-task.md` Step 3 "Write the files in the addendum below using Write/Edit on the mount" | "Write/Edit" — but some files don't exist yet (StatusPanel is fresh). Convention is "Write new file, Edit existing." Implicit. | low |
| A6 | `symbol-contract-consumption.md` "The rules that apply to `frontend`: `routable-bind`..." vs the FixRecurrenceRules list where `appliesTo: ["api","frontend"]` for routable-bind | The contract correctly names both roles; the addendum says "applies to frontend" — consistent with the data. Not a contradiction. OK. | — |

## 2. Contradictions

| # | First statement | Second statement | Resolution |
|---|---|---|---|
| C1 | `mandatory-core.md`: Bash ONLY via `ssh appdev "..."`. | Pre-ship aggregate grep runs on zcp (SSHFS mount paths). | Same carve-out as scaffold-apidev (grep-against-mount is a file-read, not an execution). Needs explicit note in `principles/where-commands-run.md`. |
| C2 | `frontend-codebase-addendum.md` `src/main.ts` says `mount(App, { target: document.getElementById('app')! })` (Svelte 5 API). | Vite + Svelte 5 CLI scaffold emits `new App({ target: ... })` (legacy API). | Brief explicitly says "Svelte 5 API (mount())", so authoritative — but a cold reader who copies the scaffolder default blindly will regress. Low risk if sub-agent Reads the emitted file first. |

## 3. Impossible-to-act-on instructions

| # | Instruction | Why impossible | Fix |
|---|---|---|---|
| I1 | `framework-task.md` step 1 `npm create vite@latest . -- --template svelte-ts --skip-git` | `create-vite` doesn't know `--skip-git`; npm runs the flag-check-failing version. | Lead with `ssh appdev "cd /var/www && npm create vite@latest . -- --template svelte-ts" && ssh appdev "rm -rf /var/www/.git"`. Mention `--skip-git` as "if your version supports it, include it; otherwise rely on the post-rm step." |
| I2 | `symbol-contract-consumption.md` instructs skipping rules that don't apply — but no concrete filter pattern (`.filter(r => r.appliesTo.includes('frontend') || r.appliesTo.includes('any'))`) is given. | Readable as a human; non-trivial as a runnable pipeline. | Add a note: "you can pick applicable rules manually; the list is short." |
| I3 | StatusPanel "every 5s" reads — no documented cleanup in `$effect`. | If the component unmounts, the interval should be cleared. Svelte 5's `$effect` returns a cleanup function; this convention is framework-standard. | The addendum should state "the `$effect` returns its cleanup (`clearInterval`)" explicitly so a cold reader doesn't forget. |

## 4. Defect-class cold-read scan

| Registry class | Ships? | Evidence |
|---|---|---|
| v17 SSHFS-write-not-exec | No | where-commands-run pointer + mandatory-core SSH clause |
| v21 scaffold hygiene | No | `.gitignore` rule + pre-ship assertion |
| v22 NATS creds | N/A | frontend holds no credentials |
| v22 S3 endpoint | N/A | frontend has no S3 client |
| v29 env-self-shadow | N/A | frontend doesn't write zerops.yaml |
| v26 git-init zcp-side | No | FixRule `skip-git` positive form |
| v32 Read-before-Edit | No | `principles/file-op-sequencing.md` pointer-included verbatim |
| v32 platform principles missing | No | principles 1-6 pointer-included (principle 2 is the relevant one for frontend) |
| v33 Unicode box-drawing | No | `principles/visual-style.md` pointer-included |
| v33 pre-init git sequencing | No | FixRule `skip-git` |
| v34 cross-scaffold env-var | No | contract is byte-identical across scaffold dispatches — frontend sees same DB_* / NATS_* / STORAGE_* names |
| v34 convergence architecture | No | every applicable rule has an author-runnable preAttestCmd |

## 5. P7 verdict

| Criterion | Status |
|---|---|
| Cold-reader finishes without unresolvable contradictions | **PASS with 1 caveat** — I1 needs fallback-first ordering in framework-task.md step 1 |
| Each applicable FixRule has a runnable verdict locally | PASS (routable-bind, gitignore-baseline, env-example-preserved, no-scaffold-test-artifacts, skip-git all runnable) |
| Every applicable v20-v34 defect class has a prevention mechanism | PASS |
| No dispatcher text (P2) | PASS |
| No version anchors (P6) | PASS |
| No internal check vocabulary | PASS |
| No Go-source paths | PASS |

**Net**: passes P7 conditional on I1 ordering fix and I3 $effect cleanup note.

## 6. Proposed edits

- Reorder `framework-task.md` step 1: default to the no-flag scaffolder invocation + explicit `.git` rm; treat `--skip-git` as an optional flag only when verified supported.
- Add to StatusPanel spec: "`$effect` returns `() => clearInterval(h)` as cleanup; interval handle stored in local `$state`."
- Add `.filter(r => ['frontend','any'].includes(...))` concrete one-liner or a pre-filtered sub-list in the stitched output for each scaffold role.
