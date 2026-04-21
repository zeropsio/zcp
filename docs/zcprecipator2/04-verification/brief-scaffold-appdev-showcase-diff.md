# brief-scaffold-appdev-showcase-diff.md

**Purpose**: diff against [../01-flow/flow-showcase-v34-dispatches/scaffold-appdev-svelte-spa.md](../01-flow/flow-showcase-v34-dispatches/scaffold-appdev-svelte-spa.md). v34 length: 10459 chars / 192 lines.

## 1. Removed from v34 → disposition

| v34 segment | Disposition | New home |
|---|---|---|
| L1–2 recipe-slug preamble (`nestjs-showcase`) | dispatcher → DISPATCH.md | Recipe-slug interpolated by stitcher; sub-agent framing is generic. |
| L14 "zcp orchestrator" + SSHFS-mount explainer | load-bearing → atom | `principles/where-commands-run.md`. |
| L16–22 TOOL-USE POLICY preamble (duplicated with MANDATORY body) | **scar tissue (duplicate)** | Cut. `briefs/scaffold/mandatory-core.md` transmits once. |
| L24–32 `<<<MANDATORY>>>` wrapper | dispatcher → DISPATCH.md | Atomic concatenation makes the wrapper redundant. |
| L34–43 "⚠ CRITICAL: where commands run" long prose | load-bearing → atom | Positive form in `principles/where-commands-run.md`. v34's "File writes via Write/Edit against `/var/www/appdev/` work correctly" + "Executable commands MUST run via SSH" carve-out becomes a single positive statement in the atom. |
| L46 "⚠ Framework scaffolders that auto-init git" | load-bearing (positive form) → atom | FixRule `skip-git` with positive form "pass `--skip-git` if supported OR `rm -rf /var/www/.git` after scaffolder return." |
| L48 "## What to scaffold" body (L48–99) — the bulleted file list | load-bearing → atom | `briefs/scaffold/frontend-codebase-addendum.md`. |
| L91 "**DO NOT WRITE:** README.md ... zerops.yaml ... .git/ ... Any feature code ... CORS config ... A service client or adapter module ... `src/lib/types.ts` shared cross-codebase" | load-bearing (rewritten positively) → atom | First three items (README/zerops.yaml/.git) become "Files you do NOT write at this substep" counter-list per P8 (counter-examples permitted after positive form). CORS + cross-codebase-types: folded into the "Files you do NOT write" list with short explanation ("API owns CORS, main agent reconciles cross-codebase types"). |
| L102 "## Scaffold pre-flight — platform principles" body | load-bearing → atom | `principles/platform-principles/01..06.md` pointer-includes. Frontend's applicable rule is Principle 2 (routable bind); Principle 6 explicitly noted as main-agent concern. |
| L106 "Run it via `bash -c` from a scratch file in `/tmp` (NEVER inside `/var/www/appdev/`)" | **scar tissue** | Cut. The instruction was reactive to a v33-era bug where the aggregate was invoked inside the SSHFS mount path. The rule is already covered by "executables via ssh, not on zcp against the mount"; the explicit /tmp directive is redundant once the carve-out in `where-commands-run.md` covers read-only grep-on-mount. |
| L108–180 Pre-ship self-verification bash aggregate | load-bearing → atom (contract-driven + reminder snapshot) | Same pattern as apidev: FixRecurrenceRules provide the canonical runnable checks; `briefs/scaffold/pre-ship-assertions.md` keeps the aggregate as a reminder. Closes v34 cross-scaffold-assertion drift class (v34 showed apidev and appdev using slightly different pre-ship scripts). |
| L183 "After writing files, run `ssh appdev "cd /var/www && npm install"`" + build verification | load-bearing → atom | Same as apidev treatment. |
| L189–191 "## Reporting back" | load-bearing → atom | `briefs/scaffold/completion-shape.md`. |

## 2. Added to new composition

| Content | Closes defect class |
|---|---|
| `SymbolContract` JSON (shared verbatim) | **v34-cross-scaffold-env-var** — byte-identical transmission means frontend cannot use different env-var names than api or worker. |
| `FixRecurrenceRules` with `routable-bind` preAttestCmd (`grep -q "'0.0.0.0'" /var/www/appdev/vite.config.ts`) | **v22-routable-bind class** — generalized. |
| Role-filter note "rules whose `appliesTo` is api or worker do NOT apply" | prevents frontend sub-agent wasting cycles running api-only checks (author-side efficiency) |
| Positive-form `principles/where-commands-run.md` pointer | **v33-phantom-output-tree** (class: enumerated prohibitions produce invention) — positive paths. |
| StatusPanel explicit Svelte 5 runes guidance (`$state`, `$effect`) | closes frontend-specific version-drift class (v7 Svelte 3 → v34 Svelte 5 patterns — training-data memory fights runtime) |
| `src/lib/api.ts` content-type assertion requirement | retained (v18 gotcha about SPA fallback serving HTML masquerading as JSON response) |

## 3. Boundary changes (structural)

| Axis | v34 | New |
|---|---|---|
| Audience | Mixed dispatcher + sub-agent | Pure sub-agent (atoms); dispatcher text outside |
| Recipe-slug | Literal `nestjs-showcase` appears in brief body | Absent; generic framing |
| Env-var names | Prose "STAGE_API_URL / DEV_API_URL" | Contract names same vars; sub-agent reads contract, not brief prose |
| Probes | None expected (frontend scaffold is synchronous) | Not applicable — feature sub-agent has cadence rule |

## 4. Byte-budget reconciliation

| Segment | v34 | new | delta |
|---|---:|---:|---:|
| Preamble + framing | ~850 | ~180 | -670 |
| Duplicated MANDATORY | ~700 | 0 | -700 |
| where-commands-run prose | ~500 | ~250 | -250 |
| Service plan context | N/A for frontend | ~200 (contract subset) | +200 |
| File list | ~3500 | ~3000 | -500 |
| DO NOT WRITE | ~300 | ~250 | -50 |
| Pre-flight aggregate | ~2000 | ~1600 | -400 |
| Build / reporting | ~500 | ~450 | -50 |
| **Total** | **~10.5 KB** | **~8.5 KB** | **-2 KB (~19% reduction)** |

## 5. Silent-drops audit

Every v34 line/section covered in §1. Zero silent drops.
