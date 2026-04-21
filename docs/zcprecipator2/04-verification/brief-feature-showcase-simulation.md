# brief-feature-showcase-simulation.md

**Purpose**: cold-read simulation of composed feature-showcase brief.

## 1. Ambiguities

| # | Locus | Ambiguity | Severity |
|---|---|---|---|
| A1 | `symbol-contract-consumption.md` — scaffold state summary (`DB_PASS / NATS_PASS (not *_PASSWORD)`) | The parenthetical is a historical tell. A cold reader doesn't know why "*_PASSWORD" is called out. It suggests past confusion without naming it. If the contract is authoritative, the reader doesn't need the counter-example — but if they do need it, the reasoning should be explicit (or the annotation cut). | low — P8 permits counter-examples after positive form, but the reason-for-counter-example is missing |
| A2 | `task.md` mail-send: "Treat `preview` as `sent (preview)` text so MustObserve 'queued or sent' passes" | The brief tells the agent to make the UI match a test expectation by re-mapping state. That's bad ergonomics (test-chasing) but pragmatic. A cold reader might infer a broader pattern of "aliasing states to pass tests" which is undesirable. | medium — better: say "plan's MustObserve is 'queued or sent'; preview means the mail pipeline worked without real SMTP; render it as 'sent (preview)' so the reader understands state while the UI assertion still names one of the accepted tokens." |
| A3 | `completion-shape.md` — "do NOT attempt a 4th probe" | Strong rule, but the cadence rule allows batch-of-3 followed by a non-probe action followed by another batch. These are the same rule with different phrasing: "3 probes max per hypothesis" vs "3 probes total before returning." Which? | medium — pick one phrasing. Per v34 post-mortem the intent is 3-per-hypothesis. Clarify. |
| A4 | `task.md` "storage-upload — install `multer` + `@types/multer` if missing — verify via Read of node_modules before installing" | "Verify via Read" works for a scaffolder-created package.json — look for `multer` in dependencies. But `node_modules/multer/package.json` is usually there if nest emitted it; missing check can blindly re-install. | low — Read of `package.json` + `node_modules/multer/package.json` suffices |
| A5 | `ux-quality.md` "Svelte 5 runes only ... `$: x = ...` reactive syntax is out. `mount()` not `new App()`." | Clear. But no counter-example shown (per P8, positive form first; negative as disambiguation). The runes names + `mount()` are the positive form. OK. | — |
| A6 | Feature 5 jobs-dispatch: worker must add second subscription while keeping first. The brief says "keep `jobs.scaffold` subscription unchanged" — but the `drain()` in `onModuleDestroy` currently drains ONE subscription. When a second subscription is added, does `onModuleDestroy` drain both? | **medium** — explicitly state: "track both subscriptions as a list; drain both on onModuleDestroy." Otherwise the new subscription leaks on redeploy. |

## 2. Contradictions

| # | Statement A | Statement B | Resolution |
|---|---|---|---|
| C1 | "Library-import verification" rule: verify subpath against installed package on disk. | `task.md` routinely names import symbols (`@nestjs/platform-express` + `FileInterceptor`, `@aws-sdk/client-s3` + `PutObjectCommand`, `ListObjectsV2Command`) | The verification rule is the override for any name the brief or the training-data memory produces. The brief-given names are the starting hypotheses; the agent confirms them via a quick Read before writing the import line. No contradiction, but worth stating explicitly. |
| C2 | `ux-quality.md` "No browser-default `<input>`/`<button>`." | The api.ts helper + panel components are TypeScript / Svelte-side only. | benign — "No browser-default" rule applies inside Svelte components (style the buttons), not to the api.ts transport code. |

## 3. Impossible-to-act-on

| # | Instruction | Why | Fix |
|---|---|---|---|
| I1 | "Set FactRecord.RouteTo at record time if you know the surface" | The record_fact tool schema may not have a `RouteTo` field yet (architecturally proposed, not implemented). Until the schema change ships, this field is inaccessible. | Add to `fact-recording-discipline.md`: "`RouteTo` is set on the new record_fact schema introduced alongside this architecture. Until that ships, omit the field — the writer still reads `scope`." |
| I2 | `task.md` "ssh apidev \"curl -sS -o /dev/null -w '%{http_code} %{content_type}\n' http://localhost:3000{path}\"" | Requires the apidev dev-server to be running. At feature-implementation time the dev-server is NOT running (starts at step 3 of "After implementing all features"). Hence the inline smoke test per-feature (step 5 of "Contract discipline") will fail if run before the dev-server starts. | Re-order: "Contract discipline" step 5 runs AFTER "After implementing all features" step 3 has started the dev-servers. Alternatively: the smoke test at contract-discipline step 5 is aspirational — actual verification happens after dev-server start. |

## 4. Defect-class cold-read scan

| Class | Ships? | Evidence |
|---|---|---|
| v17 SSHFS-write-not-exec | No | where-commands-run atom + mandatory-core SSH clause |
| v22 NATS URL creds | No | `principles/platform-principles/05-structured-creds.md` + contract names separate user/pass + feature task never names URL-embedded form |
| v22 queue group / competing consumer | No | Principle 4 + explicit `{ queue: 'workers' }` in jobs-dispatch worker code |
| v25 substep-bypass | No | feature is dispatched at the `deploy.subagent` substep — forbidden from calling `zerops_workflow` |
| v25 subagent at spawn | No | forbidden list |
| v28 debug writes content | N/A | writer role |
| v28 33% genuine gotchas | N/A | writer-side framing |
| v29 env-self-shadow | No to the extent feature touches zerops.yaml (it doesn't — main-agent territory) |
| v30 worker SIGTERM | No | ux-quality + principle 1 + feature task explicitly keeps drain intact (per simulation A6 clarification) |
| v31 apidev enableShutdownHooks | N/A | scaffold-phase concern; feature preserves |
| v32 dispatch compression | No | pointer-includes are byte-identical; no compression pressure |
| v32 six principles missing | No | all 6 pointer-included |
| v33 diagnostic probe burst | **No** — `diagnostic-cadence.md` has positive form (max 3 probes per hypothesis, batch separated by non-probes) | closes v33 Fix D class |
| v33 Unicode box-drawing | No | visual-style atom |
| v34 cross-scaffold env-var | No (this role CONSUMES the contract, not re-declares) | the feature sub-agent reads DB_PASS from env like any other consumer |
| v34 convergence architecture | No | feature's pre-attest verification is the curl smoke tests + build + dev-server probes — all author-runnable |
| v34 manifest content inconsistency | N/A | writer role |
| v8.94 library-import version drift | **No** — first paragraph of mandatory-core says "verify the symbol/subpath against the installed package on disk" | first paragraph is the v8.97-era library-verify preamble kept |

## 5. P7 verdict

| Criterion | Status |
|---|---|
| Cold-reader finishes without unresolvable contradictions | **PASS with 3 caveats** — A3 (cadence "3-per-hypothesis" vs "3-total"), A6 (drain both subs), I1 (RouteTo availability) |
| Author-runnable pre-attest for each feature | PASS — curl smoke tests + build + dev-server health checks |
| Every applicable v20-v34 defect class has a prevention mechanism | PASS |
| No dispatcher text (P2) | PASS |
| No version anchors (P6) | PASS |
| No internal check vocabulary | PASS |
| No Go-source paths | PASS |

**Net**: passes conditional on the 3 caveat clarifications.

## 6. Proposed edits

- Rewrite A2: replace "treat preview as sent (preview) text so MustObserve passes" → "the plan's MustObserve accepts 'queued' OR 'sent'; when SMTP_HOST is empty the mail pipeline succeeds in jsonTransport mode; render this as 'sent (preview)' so the reader sees both the pipeline's success and the fallback state. The assertion tokenizes 'sent' from that text."
- Rewrite A3: explicit "max 3 probes per hypothesis; if the 3rd probe does not resolve, escalate to the main agent before forming a new hypothesis."
- Add A6 explicit instruction: "worker tracks both subscriptions in an array; `onModuleDestroy` drains each before draining the connection."
- Add I1 note: "RouteTo is only set on the new record_fact schema; until then, omit the field."
- Re-order I2: explicit ordering "curl smoke tests run after dev-servers start; per-feature contract-discipline step 5 is a sanity check once server is up."
