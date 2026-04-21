# brief-scaffold-minimal-simulation.md

**Purpose**: cold-read simulation of composed minimal scaffold composition. Unlike the showcase scaffold, this one is consumed IN-BAND by the main agent (no Agent dispatch). The cold-read simulation checks whether the same atoms are still sensible when consumed inline.

## 1. Ambiguities

| # | Locus | Ambiguity | Severity |
|---|---|---|---|
| A1 | `briefs/scaffold/framework-task.md, main-inline` label | The atom text was drafted for a sub-agent audience ("you are a scaffolding sub-agent"). Re-labeled as "main-inline" but text still reads "you" in second person, as if addressing a sub-agent. | low — the "you" pronoun still works when main is the reader (main is also a "you"); but some sub-agent-specific phrasing (e.g. "return to the main agent") bleeds in. Should be filtered at stitch-time OR the atom rewritten to be audience-neutral. |
| A2 | `pre-ship-assertions.md, main-inline` — the reminder snapshot uses mixed framework-specific asserts ("# if api" / "# if frontend") with `|| true` fallbacks | This multi-framework branch is confusing inline. Cold reader might not know which branch applies to this run. | **medium** — the Go stitcher should filter the reminder snapshot by framework-role, emitting only the branch that applies. Otherwise main reads both branches and runs spurious greps. |
| A3 | Tier-branching of `principles/platform-principles/*` | "Only principles applicable to the minimal's framework role are pointer-included." Who decides? The Go stitcher needs a framework-role classifier. | low — classifier is simple (api-style vs static-frontend); but unclear from the atom text. Stitcher must tier-branch. |
| A4 | `symbol-naming-contract.md` consumption conventions — minimal contract is "smaller" | Exactly which FixRecurrenceRules survive in a minimal contract? Example: laravel-minimal has no NATS so `nats-separate-creds` is N/A. Is the rule filtered out at contract construction time, or is it left in with an empty AppliesTo? | low — cleaner: Go stitcher filters rules based on the actually-provisioned managed services. |
| A5 | `phases/generate/scaffold/where-to-write-single.md` "you author the scaffold yourself using the atoms below as guidance" | "Atoms below" — from main's perspective reading the substep-entry guide, the rest of the stitched composition IS the atoms. A cold reader might mis-parse "below" as "below in a different file." | low — wording fix in the atom. |

## 2. Contradictions

| # | A | B | Resolution |
|---|---|---|---|
| C1 | "No sub-agent dispatch fires" (phases/scaffold/entry) vs `briefs/scaffold/framework-task.md` which was authored for sub-agent audience | The atom is consumed inline by main; the audience shift is handled at stitch-time by a note ("consume as guidance, not as dispatch"). | Not a contradiction per se, but fragile — rely on stitch-time audience-adaptation or rewrite atom. |
| C2 | tier-branching: if minimal is dual-runtime (nestjs-minimal-v3 has API + frontend split), does "single-codebase" still hold? | Per data-flow-minimal.md §1, "Codebases / SSHFS mounts: 1 OR 2 (dual-runtime minimal)." When there are 2 codebases in minimal, per §4a "multi-codebase minimal: scaffold sub-agent dispatches fire. Exact same composition as showcase §3a, with 2 dispatches." | So there's a branch condition: single-codebase minimal → main-inline; multi-codebase minimal → sub-agent dispatch. The brief covers single-codebase here; multi-codebase falls back to the scaffold-{api|frontend|worker}-showcase composition (w/o worker). |

## 3. Impossible-to-act-on

| # | Instruction | Why | Fix |
|---|---|---|---|
| I1 | `pre-ship-assertions.md` reminder with `# --- if api ---` branch markers | These are bash comments, not conditionals. Main agent runs all lines; the api-only ones will grep framework-irrelevant patterns (e.g. `main.ts` in a Laravel minimal). | stitcher tier-branches OR rewrite reminder as a switch by framework. |
| I2 | `briefs/scaffold/framework-task.md` step 1 uses `<framework-scaffolder-command>` placeholder | Main knows the framework; stitcher should interpolate the exact command per `plan.Research.Framework`. | stitcher interpolation: `{{.FrameworkScaffolderCommand}}`. |

## 4. Defect-class cold-read scan

| Class | Ships? | Evidence |
|---|---|---|
| v17 SSHFS-write-not-exec | No | where-commands-run pointer, same as showcase |
| v21 scaffold hygiene | No | FixRule `gitignore-baseline` + reminder snapshot |
| v21 framework-token-purge | No | atomization per framework-role addendum |
| v22 NATS creds | N/A to most minimals (no NATS service) — if a minimal provisions NATS, rule applies | |
| v22 queue-group | N/A — no worker in minimal | |
| v25 substep-bypass | main-agent is the actor; P4 server-state-is-plan applies normally |
| v26 git-init zcp-side chown | No | skip-git rule |
| v30 worker SIGTERM | N/A — no worker in minimal | |
| v31 apidev enableShutdownHooks | No (for api-style minimal) | |
| v32 dispatch compression | **Partially N/A** — no dispatch fires. BUT: if stitching fails to audience-adapt the atoms, the in-band guidance may contain dispatcher-only phrasing that's harmless but confusing. |
| v32 six principles | No — principles pointer-included filtered by role |
| v33 Unicode box-drawing | No | visual-style + comment-style |
| v33 phantom output tree | N/A — writer role |
| v34 cross-scaffold env-var | **Trivially N/A** — single codebase has no cross-codebase mismatch possible. For dual-runtime minimal with 2 codebases, contract still applies. |
| v34 manifest inconsistency | N/A — writer role |
| v34 convergence architecture | No — every rule has runnable preAttestCmd |

## 5. P7 verdict

| Criterion | Status |
|---|---|
| Cold-reader finishes without unresolvable contradictions | **PASS with 2 caveats** — A2/I1 (tier-branched reminder), A1 (audience neutrality of main-inline atoms) |
| Author-runnable pre-attest per applicable rule | PASS |
| Every applicable v20-v34 class has a prevention mechanism | PASS |
| No dispatcher text (P2) | PASS (consumed in-band; no Agent dispatch frames the composition) |
| No version anchors (P6) | PASS |
| No internal check vocabulary | PASS |
| No Go-source paths | PASS |

**Net**: passes conditional on audience-adaptation of atom text (or stitcher-level interpolation) and framework-branch filtering of the reminder snapshot.

## 6. Proposed edits

- Audit the `briefs/scaffold/*` atoms for sub-agent-specific phrasing ("return to the main agent", "you are a sub-agent"). Rewrite as audience-neutral OR have the Go stitcher substitute phrases when atoms are consumed in-band vs transmitted.
- Stitcher branches `pre-ship-assertions.md` reminder by framework role; only the applicable asserts land in main's guidance.
- Stitcher interpolates `{{.FrameworkScaffolderCommand}}` based on plan.Research.Framework.
- Decision point: add `AtomAudienceMode = {inband, dispatched}` enum to the stitching contract so atoms can have audience-mode-aware sections (small Go-template conditionals).
