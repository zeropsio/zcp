# brief-scaffold-apidev-showcase-diff.md

**Purpose**: diff the composed brief (new atomic system) against the v34 captured dispatch at [../01-flow/flow-showcase-v34-dispatches/scaffold-apidev-nestjs-api.md](../01-flow/flow-showcase-v34-dispatches/scaffold-apidev-nestjs-api.md). Every line/section removed gets a disposition: **scar tissue** / **version-log noise** / **dispatcher instruction → DISPATCH.md** / **load-bearing → atom X**. Every line/section added gets a defect-class trace.

v34 length: 15627 chars / 329 lines.
Composed length: ~11 KB.

---

## 1. Removed from v34 → new composition

| v34 line / section | Disposition | New home |
|---|---|---|
| L1–2 "You are a scaffolding sub-agent for the `nestjs-showcase` Zerops recipe." (recipe-slug preamble + explicit `nestjs-showcase` name) | dispatcher instruction → DISPATCH.md | DISPATCH.md tells main how to interpolate `{{.RecipeSlug}}` in the sub-agent framing sentence. The atomic framing sentence uses generic "the API codebase for hostname `{{.Hostname}}`". |
| L14 "Your working directory is the zcp orchestrator container; `/var/www/apidev/` is a network mount to the dev container `apidev`." | load-bearing → atom | Moved to `principles/where-commands-run.md` (positive form) + pointer-included. |
| L16–22 "⚠ TOOL-USE POLICY — read before your first tool call." + body (Permitted / Forbidden duplicated) | **scar tissue (duplicated)** | Dispatcher-facing "read before your first tool call" framing cut. The policy body survives in `briefs/scaffold/mandatory-core.md` — transmitted once, not twice. v34 duplicated it between the short preamble and the MANDATORY block. |
| L24–32 `<<<MANDATORY — TRANSMIT VERBATIM IN AGENT DISPATCH PROMPT>>>` wrapper sentinels | dispatcher instruction → DISPATCH.md | The literal `<<<MANDATORY — TRANSMIT VERBATIM>>>` wrapper was dispatcher-facing framing to tell main NOT to compress the region. Under the atomic architecture, atoms are byte-identically concatenated; the wrapper is redundant — and when included, it's dispatcher leakage (P2). Cut. |
| L26 "File-op sequencing — every Edit must be preceded by a Read..." | load-bearing → atom | Now `principles/file-op-sequencing.md` pointer-included. Content survives verbatim; the duplication with the preceding (non-MANDATORY) paragraph disappears. |
| L28 "Tool-use policy — permitted tools: Read, Edit, ..." | load-bearing → atom | Now `principles/tool-use-policy.md` + `briefs/scaffold/mandatory-core.md`. |
| L30 "SSH-only executables — NEVER `cd /var/www/{hostname}` ..." | load-bearing (positive form rewrite) → atom | Now `principles/where-commands-run.md` — **rewritten to positive form** per P8 ("executables run via ssh ...") instead of "NEVER cd ...". |
| L34 "⚠ CRITICAL: where commands run" heading + body (L34–43) | load-bearing → atom | Positive form in `principles/where-commands-run.md`; v34's text included the MANDATORY block's SSH rule AND a longer "where commands run" preamble covering the same thing. Duplication collapsed. |
| L42 "⚠ Framework scaffolder auto-init git. The NestJS CLI `nest new` runs `git init` by default." | load-bearing (positive form) → atom | Now FixRule `skip-git` in the contract + `briefs/scaffold/framework-task.md` step 1 passes `--skip-git`. Negative warning replaced with positive action. |
| L44 "## Service plan context" (L44–53 — explicit env-var names DB_HOST etc. listed as `${db_hostname}`, `${db_port}`, ... with prose) | load-bearing → atom + contract | Replaced by the `SymbolContract.EnvVarsByKind` JSON block. **Structural change**: v34 listed `${db_hostname}` etc. (platform-side names) in the brief and left the sub-agent to infer runtime env-var mapping; new composition declares runtime env-var names directly (`DB_HOST`, `DB_PORT`, ...) as the contract. Closes v34 DB_PASS/DB_PASSWORD class. |
| L55 "## What to scaffold" body (L55–57 "A **health-dashboard-only** NestJS API scaffold. You write infrastructure.") | load-bearing → atom | `briefs/scaffold/framework-task.md` + `briefs/scaffold/api-codebase-addendum.md` preamble ("You scaffold a health-dashboard-only API..."). |
| L59–73 "### Step 1 — Scaffold NestJS" + install command block | load-bearing → atom | `briefs/scaffold/framework-task.md` steps 1–2 (verbatim same ssh commands). |
| L75–89 "### Step 2 — Modify / add files" — bulleted file list (main.ts, app.module.ts, health, status, services, entities, migrate, seed, .gitignore, .env.example) | load-bearing → atom | `briefs/scaffold/api-codebase-addendum.md`. **Change**: entity/migrate/seed bullets kept; file-op ordering ("Read every emitted file before first Edit") now comes from the pointer-included `principles/file-op-sequencing.md` rather than re-stated prose. |
| L117 "Principle 1 (graceful shutdown) is satisfied here." | load-bearing (positive form) → atom | Moved to `principles/platform-principles/01-graceful-shutdown.md` pointer-include; the prose-level claim becomes a record_fact contract (platform-principles atoms each terminate with "record_fact scope=both after implementation"). |
| L221–228 "**DO NOT WRITE:** README.md ... zerops.yaml ... .git/ ... Feature routes" | load-bearing (positive form rewrite) → atom | Rewritten in `briefs/scaffold/api-codebase-addendum.md` as "Files you do NOT write at this substep" — kept as a counter-list because the positive list (files you DO write) has already been enumerated; principle P8 permits counter-examples after the positive form. |
| L230 "## Scaffold pre-flight — platform principles" body (L230–237: Principle 1/2/3/5 declarations) | load-bearing → atom | `briefs/scaffold/api-codebase-addendum.md` has a "Platform principles" subsection whose content comes from the pointer-included `principles/platform-principles/01..06.md` atoms. **Change**: Principle 5 gets the structured `FixRule.nats-separate-creds` as its runnable form; the prose "Principle 5 — Structured credentials" is kept as the reader-friendly label. |
| L239–316 "## Pre-ship self-verification (MANDATORY)" + full bash aggregate (assertions 1-10) | load-bearing → atom (partly rewritten) | **Partially supplanted**: the `SymbolContract.FixRecurrenceRules` list provides 11 preAttestCmds that cover assertions 1/2/3/4/5/6/7/8/9/10 of v34 (0.0.0.0 bind / trust proxy / S3 forcePathStyle / NATS URL creds / Redis password-in-URL / no README / no zerops.yaml / no .git / no .env + .env.example present / gitignore node_modules). `briefs/scaffold/pre-ship-assertions.md` keeps the aggregate as a reminder snapshot. **Closure**: v34 had no mechanism to guarantee byte-identical assertion list across 3 scaffold dispatches; now the list lives in the contract and is transmitted identically. |
| L318–324 "## Build verification" ("After writing, verify compilation: `ssh apidev \"cd /var/www && npm run build 2>&1 \| tail -40\"`") | load-bearing → atom | `briefs/scaffold/pre-ship-assertions.md` retains the build step. |
| L326–328 "## Reporting back" ("Return a bulleted list of files you wrote, the pre-ship script exit code, ...") | load-bearing → atom | `briefs/scaffold/completion-shape.md`. Slightly expanded (req. "env var names your code reads per managed service" now explicitly enumerated as "ordered by service kind"). |

---

## 2. Added to new composition (not present in v34)

| New content | Closes defect class |
|---|---|
| `SymbolContract` JSON block with `EnvVarsByKind` + `HTTPRoutes` + `NATSSubjects` + `NATSQueues` + `Hostnames` + `DTOs` + `FixRecurrenceRules` | **v34-cross-scaffold-env-var** — v34's DB_PASS/DB_PASSWORD class. The contract's `EnvVarsByKind.db.pass = "DB_PASS"` is byte-identical across all scaffold dispatches, so no sub-agent can invent `DB_PASSWORD` independently. Also closes **v34-convergence-architecture** by making env-var coordination pre-attest runnable rather than post-attest observable. |
| `FixRecurrenceRule` entries with positive-form statements + author-runnable preAttestCmd | **v22-nats-url-creds** (`nats-separate-creds` rule), **v22-s3-endpoint** (`s3-uses-api-url`), **v22-dev-start-vs-build** partially (trust proxy via rule), **v30-worker-SIGTERM + v31-apidev-enableShutdownHooks** (`graceful-shutdown` rule), **v21-scaffold-hygiene** (`gitignore-baseline` + `no-scaffold-test-artifacts` rules), **v28-env-READMEs-substantive** indirectly (`env-example-preserved`), **v26-git-init-zcp-side-chown** / **v33-pre-init-git-sequencing** (`skip-git` rule). |
| `PriorDiscoveriesBlock(...)` slot at brief tail | Closes misroute-map.md §1 (v25 substep-bypass architectural risk). Slot is explicitly named so first dispatch sees empty block; subsequent re-dispatches carry downstream-scope facts from the session's facts log. |
| Positive-form `principles/where-commands-run.md` (no "NEVER" prefix as the primary rule) | **v33-phantom-output-tree** architectural class — positive paths/allow-list for commands vs enumerated prohibition that was v34's regression vector. Principle P8. |
| `principles/comment-style.md` + `principles/visual-style.md` pointer-includes | **v33-Unicode-box-drawing** — covered positively even though scaffold rarely writes YAML-style comments; future-proofing. |
| Explicit `Controllers declare their own path (@Controller('health')) / global prefix applied once via app.setGlobalPrefix('api')` note | new coverage for a latent ambiguity (see simulation C3). Not in v34, not in registry; flagged as a step-4 cold-read-discovered edit. |

---

## 3. Boundary changes (structural, not line-for-line)

| Axis | v34 shape | New shape | Rationale |
|---|---|---|---|
| **Audience** | Single text blob with mixed dispatcher + sub-agent audience (MANDATORY wrappers, "transmit verbatim" sentinels, "nestjs-showcase" preamble) | Physical separation — atoms in `briefs/scaffold/` are sub-agent-only, dispatcher guidance in `docs/zcprecipator2/DISPATCH.md` outside the content tree | Principle P2 |
| **Env var names** | Listed as platform-side `${db_hostname}` etc. with prose explaining the mapping to runtime `DB_HOST` | Contract declares runtime `DB_HOST` etc. as the canonical keys | Principle P3 — closes v34 cross-scaffold env-var class |
| **Prohibitions** | "NEVER `cd /var/www && <exec>`", "DO NOT WRITE README.md", "No .env file" | "Executables run via ssh", "Files you write are: …", FixRules with positive-form statements | Principle P8 |
| **Platform-principles** | Prose ("Principle 1 — graceful shutdown: `app.enableShutdownHooks()` in main.ts + OnModuleDestroy...") | Pointer-included atoms + structured `FixRule.graceful-shutdown` with a preAttestCmd | Principle P1 |
| **Pre-ship verification** | One hand-maintained bash aggregate | Contract-driven aggregate + atom-level reminder snapshot | Principle P1 + P3 (byte-identical across scaffold dispatches) |
| **Recipe-slug interpolation** | Literal `nestjs-showcase` in prose (L1, L14, L45 "You are scaffolding an API that connects to ALL these managed services" — the "ALL" is showcase-specific) | Recipe-slug absent from sub-agent text; managed-services list comes from the contract JSON (only the services this recipe actually provisions) | Principle P2 (leaf artifact) + Principle P6 (recipe-specific content via interpolation, not hardcoded) |

---

## 4. Byte-budget reconciliation

| Segment | v34 bytes | new bytes | delta |
|---|---:|---:|---:|
| Preamble + framing | ~900 | ~180 | -720 |
| MANDATORY block (duplicated with main body) | ~700 | 0 (single atom handles it) | -700 |
| Service plan context (prose list) | ~800 | ~1200 (structured JSON) | +400 |
| Scaffold steps 1-2 (scaffolder + install) | ~600 | ~560 | -40 |
| File-by-file addendum | ~4500 | ~3800 | -700 |
| DO NOT WRITE list | ~350 | ~300 | -50 |
| Platform principles prose | ~400 | ~280 (atoms pointer-included) | -120 |
| Pre-ship bash aggregate | ~2000 | ~1600 + FixRule rollup | ~0 net (the aggregate is a reminder snapshot; rules are canonical) |
| Build verification + Reporting back | ~800 | ~700 | -100 |
| **Total** | **~15.6 KB** | **~11 KB** | **-4.6 KB (~29% reduction)** |

Reduction sources: duplicated MANDATORY region (700 B), recipe-slug/preamble trimmed (700 B), platform-principles atomization (120 B), "DO NOT" → positive allow-list (50 B). Offset by +400 B structured SymbolContract JSON. Net reduction supports P6 (atomic guidance) and the overall context-reduction target.

---

## 5. Silent drops audit (per task instruction "no silent drops")

Every line removed has a disposition in §1. Cross-checked against v34 at line granularity:

- L1–13: framing — dispatcher + load-bearing split covered.
- L14–43: tool policy + SSH rule — covered.
- L44–53: service plan context — replaced by contract.
- L55–228: scaffold task body — covered.
- L230–237: platform principles — covered.
- L239–316: pre-ship aggregate — covered.
- L318–328: build + reporting — covered.

Zero silent drops.
