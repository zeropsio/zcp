# Pipeline actor map

Per-phase breakdown of who reads what, who writes what, and how big the
brief is. Sizes are atom-byte totals (UTF-8 file size); engine-derived
sections are estimated from typical run shapes.

---

## Pipeline at a glance

```
┌──────────────────────────────────────────────────────────────────────┐
│                            MAIN AGENT                                │
│                                                                      │
│  Reads only:  the current phase_entry/<phase>.md (handler-delivered) │
│               + briefs from build-brief / build-subagent-prompt      │
│  Drives:      every phase via `zerops_recipe action=...`             │
│  Dispatches:  sub-agents in parallel via Agent tool                  │
└──────────────────────────────────────────────────────────────────────┘
                                  │
   ┌──────────────────────────────┴──────────────────────────────┐
   ▼                                                             ▼

╔══════════════════════════════════════╗  ╔════════════════════════╗
║  0  RESEARCH                main     ║  ║  1  PROVISION    main  ║
╠══════════════════════════════════════╣  ╠════════════════════════╣
║ READS                                ║  ║ READS                  ║
║   ◇ phase_entry/research.md    ~5 KB ║  ║   ◇ phase_entry/       ║
║   ◇ parent recipe (if set)           ║  ║       provision.md ~4K ║
║   ◇ zerops_knowledge pulls (lazy)    ║  ║                        ║
║ WRITES                               ║  ║ WRITES                 ║
║   ▶ plan.json (update-plan)          ║  ║   ▶ 14 services live   ║
║                                      ║  ║       (zerops_import)  ║
║                                      ║  ║   ▶ project envs       ║
║                                      ║  ║       (zerops_env)     ║
╚══════════════════════════════════════╝  ╚════════════════════════╝
                                  │
                                  ▼

╔══════════════════════════════════════════════════════════════════════╗
║  2  SCAFFOLD          actor: N × scaffold sub-agents (per codebase)  ║
║                       brief size: ~40–48 KB     (cap 48 KB)          ║
╠══════════════════════════════════════════════════════════════════════╣
║ READS — always                                                       ║
║   briefs/scaffold/platform_principles.md                       ~5 KB ║
║   briefs/scaffold/preship_contract.md                        ~0.6 KB ║
║   briefs/scaffold/fact_recording.md                          ~0.6 KB ║
║   briefs/scaffold/decision_recording.md                       ~14 KB ║
║   principles/dev-loop.md                                     ~4.5 KB ║
║   principles/mount-vs-container.md                             ~2 KB ║
║   principles/yaml-comment-style.md                           ~3.3 KB ║
║   principles/cross-service-urls.md         (run-20 C2)       ~7.3 KB ║
║   principles/bare-yaml-prohibition.md      (run-20 C3)       ~1.6 KB ║
║   + citation-guide list, recipe-knowledge slug list            ~2 KB ║
║                                                                      ║
║ READS — conditional                                                  ║
║   principles/init-commands-model.md      ─ anyCodebaseHasInitCommands║
║   briefs/scaffold/build_tool_host_allowlist.md  ─ frontend + nodejs  ║
║   briefs/scaffold/spa_static_runtime.md  (run-20 C2 layer 3)         ║
║                                          ─ frontend + nodejs         ║
║   tier-fact table  ─ frontend role only                              ║
║   parent excerpt   ─ only if parent.Codebases[cb.Hostname] exists    ║
║                                                                      ║
║ WRITES                                                               ║
║   ▶ code (src/**)                                                    ║
║   ▶ zerops.yaml  (must be bare — comments forbidden at scaffold)     ║
║   ▶ dev process running via zerops_dev_server                        ║
║   ▶ facts (record-fact)                                              ║
╚══════════════════════════════════════════════════════════════════════╝
                                  │
                                  ▼

╔══════════════════════════════════════════════════════════════════════╗
║  3  FEATURE                actor: 1 feature sub-agent (cross-cb)     ║
║                            brief size: ~14–22 KB     (cap 20 KB)     ║
╠══════════════════════════════════════════════════════════════════════╣
║ READS — always                                                       ║
║   briefs/feature/feature_kinds.md                            ~2.3 KB ║
║   briefs/feature/decision_recording.md                       ~5.4 KB ║
║   principles/mount-vs-container.md                             ~2 KB ║
║   principles/yaml-comment-style.md                           ~3.3 KB ║
║   + symbol table                                          ~0.5–1.5 KB║
║                                                                      ║
║ READS — conditional                                                  ║
║   principles/init-commands-model.md  ─ planDeclaresSeed              ║
║                                        (seed/scout-import/bootstrap) ║
║   briefs/feature/showcase_scenario.md  ─ plan.Tier == showcase       ║
║                                                                      ║
║ WRITES                                                               ║
║   ▶ extended code                                                    ║
║   ▶ feature facts (porter_change, field_rationale)                   ║
║   ▶ browser-walk facts (zerops_browser + record-fact)                ║
╚══════════════════════════════════════════════════════════════════════╝
                                  │
                ┌─────────────────┴─────────────────┐
                ▼                                   ▼

╔══════════════════════════════════════╗  ╔══════════════════════════╗
║  4a  CODEBASE-CONTENT                ║  ║  4b  CLAUDE.MD AUTHOR    ║
║                                      ║  ║                          ║
║  N × codebase-content sub-agents     ║  ║  N × claudemd-author     ║
║  brief: ~45–50 KB  (cap 48 KB)       ║  ║  brief: ~3.5–4 KB        ║
║                                      ║  ║          (cap 8 KB)      ║
║  (parallel siblings — same N) ───────╫──╫──→  parallel sibling     ║
╠══════════════════════════════════════╣  ╠══════════════════════════╣
║ READS — always                       ║  ║ READS — always           ║
║   phase_entry/codebase-content       ║  ║   phase_entry/           ║
║                              ~2.8 KB ║  ║     claudemd-author.md   ║
║   briefs/codebase-content/           ║  ║                  ~2.1 KB ║
║     synthesis_workflow.md     ~20 KB ║  ║   briefs/claudemd-author/║
║   briefs/scaffold/                   ║  ║     zerops_free_-        ║
║     platform_principles.md     ~5 KB ║  ║     prohibition.md       ║
║     (cross-loaded)                   ║  ║                  ~0.8 KB ║
║   principles/nats-shapes.md          ║  ║                          ║
║                  (run-20 C1) ~2.7 KB ║  ║ ENGINE-DERIVED           ║
║   principles/cross-service-urls.md   ║  ║   on-demand pointers     ║
║                  (run-20 C2) ~7.3 KB ║  ║   (package.json /        ║
║   principles/zerops-knowledge-       ║  ║    composer.json /       ║
║     attestation.md             ~3 KB ║  ║    src/** / app/**;      ║
║   principles/yaml-comment-style.md   ║  ║    zerops.yaml DELIBER-  ║
║                              ~3.3 KB ║  ║    ATELY EXCLUDED)       ║
║                                      ║  ║                          ║
║ READS — conditional                  ║  ║ Zerops-free by           ║
║   showcase_tier_supplements.md       ║  ║ construction             ║
║   ─ showcase tier + cb.IsWorker      ║  ║                          ║
║                                      ║  ║ WRITES                   ║
║ ENGINE-DERIVED                       ║  ║   ▶ codebase/<h>/        ║
║   citation-guide list        ~0.6 KB ║  ║       claude-md          ║
║   filtered facts            ~1–5 KB ║  ║     (single fragment)    ║
║     (FilterByCodebase, drop          ║  ║                          ║
║      EngineEmitted=true; mix:        ║  ║                          ║
║      porter_change +                 ║  ║                          ║
║      field_rationale +               ║  ║                          ║
║      platform-trap)                  ║  ║                          ║
║   on-demand pointer block    ~0.5 KB ║  ║                          ║
║                                      ║  ║                          ║
║ WRITES (record-fragment)             ║  ║                          ║
║   ▶ codebase/<h>/intro               ║  ║                          ║
║   ▶ codebase/<h>/integration-        ║  ║                          ║
║       guide/<n>                      ║  ║                          ║
║   ▶ codebase/<h>/knowledge-base      ║  ║                          ║
║   ▶ codebase/<h>/zerops-yaml         ║  ║                          ║
║     (v9.46.0 — WHOLE commented       ║  ║                          ║
║      zerops.yaml as ONE fragment;    ║  ║                          ║
║      stitcher writes verbatim;       ║  ║                          ║
║      replaces per-block              ║  ║                          ║
║      `zerops-yaml-comments/<block>`) ║  ║                          ║
╚══════════════════════════════════════╝  ╚══════════════════════════╝
                                  │
                                  ▼

╔══════════════════════════════════════════════════════════════════════╗
║  5  ENV-CONTENT                       actor: 1 env-content sub-agent ║
║                                       brief size: ~20–25 KB (cap 48) ║
╠══════════════════════════════════════════════════════════════════════╣
║ READS — always                                                       ║
║   phase_entry/env-content.md                                 ~1.2 KB ║
║   briefs/env-content/per_tier_authoring.md                     ~8 KB ║
║   principles/nats-shapes.md  (run-20 C1 — wired here precisely       ║
║      to close the run-19 T0+T5 JetStream fabrication)        ~2.7 KB ║
║   principles/zerops-knowledge-attestation.md                   ~3 KB ║
║   principles/yaml-comment-style.md                           ~3.3 KB ║
║                                                                      ║
║ ENGINE-DERIVED                                                       ║
║   per-tier capability matrix (Tiers())                       ~0.5 KB ║
║   cross-tier deltas (tiers.go::Diff)                         ~0.8 KB ║
║   engine-emitted tier_decision facts                          ~1–3 KB║
║   cross-codebase contract facts                              ~0.5 KB ║
║   plan snapshot + parent pointer (when set)                  ~0.8 KB ║
║                                                                      ║
║ WRITES (record-fragment)                                             ║
║   ▶ env/N/intro × 6 tiers                                            ║
║   ▶ env/N/import-comments/<svc> per tier per host                    ║
╚══════════════════════════════════════════════════════════════════════╝
                                  │
                                  ▼

╔══════════════════════════════════════════════════════════════════════╗
║  6a  FINALIZE                          actor: 1 finalize sub-agent   ║
║                                        brief: ~10–13 KB (cap 14 KB)  ║
╠══════════════════════════════════════════════════════════════════════╣
║ READS — always                                                       ║
║   briefs/finalize/intro.md                                     ~1 KB ║
║   briefs/finalize/validator_tripwires.md                       ~3 KB ║
║   briefs/finalize/anti_patterns.md                           ~0.8 KB ║
║                                                                      ║
║ ENGINE-DERIVED                                                       ║
║   tier map + tier-fact table                                 ~2.5 KB ║
║   audience paths (per-codebase SourceRoot)                   ~0.5 KB ║
║   fragment list  (formatFinalizeFragmentList)                 ~3–5 KB║
║   fragment-count math  (finalizeFragmentMath)                ~0.5 KB ║
║     ↑ ex-wrapper drift fix from run-10 S-1                           ║
║       (hand-typed 89, actual 67)                                     ║
║   symbol table                                               ~0.5 KB ║
║                                                                      ║
║ WRITES (record-fragment)                                             ║
║   ▶ root/intro                                                       ║
║   ▶ env/N/intro × 6                                                  ║
║   ▶ env/N/import-comments/project × 6                                ║
║   ▶ env/N/import-comments/<host> per cb + managed svc × 6            ║
╚══════════════════════════════════════════════════════════════════════╝
                                  │
                                  ▼   sub-agent calls stitch-content

╔══════════════════════════════════════════════════════════════════════╗
║  6b  STITCH                            actor: engine code            ║
║                                        no atom reads (0 KB context)  ║
╠══════════════════════════════════════════════════════════════════════╣
║ READS                                                                ║
║   recorded fragments + plan                                          ║
║                                                                      ║
║ WRITES                                                               ║
║   ▶ AssembleRoot/Env/Codebase READMEs                                ║
║   ▶ AssembleCodebaseClaudeMD                                         ║
║   ▶ EmitDeliverableYAML × 6                                          ║
║   ▶ WriteCodebaseYAMLWithComments  (v9.46.0 — write-through:         ║
║       reads `codebase/<h>/zerops-yaml` whole-yaml fragment + writes  ║
║       verbatim; refuse-to-wipe guard for empty body)                 ║
╚══════════════════════════════════════════════════════════════════════╝
                                  │
                                  ▼   optional

╔══════════════════════════════════════════════════════════════════════╗
║  7  REFINEMENT                actor: 1 refinement sub-agent          ║
║                               brief: ~115–130 KB  (heaviest brief)   ║
╠══════════════════════════════════════════════════════════════════════╣
║ READS — always                                                       ║
║   phase_entry/refinement.md                                  ~3.7 KB ║
║   briefs/refinement/synthesis_workflow.md                    ~6.5 KB ║
║   briefs/refinement/reference_kb_shapes.md                   ~14.7 KB║
║   briefs/refinement/reference_ig_one_mechanism.md             ~13 KB ║
║   briefs/refinement/reference_voice_patterns.md              ~8.6 KB ║
║   briefs/refinement/reference_yaml_comments.md               ~9.8 KB ║
║   briefs/refinement/reference_citations.md                   ~13.1 KB║
║   briefs/refinement/reference_trade_offs.md                  ~11.6 KB║
║   briefs/refinement/refinement_thresholds.md                  ~13 KB ║
║   briefs/refinement/embedded_rubric.md                        ~20 KB ║
║                                                                      ║
║ ENGINE-DERIVED                                                       ║
║   on-disk pointers to every stitched surface                  ~1–2 KB║
║   run-wide facts log (full snapshot, no truncation)          ~3–15 KB║
║                                                                      ║
║ WRITES                                                               ║
║   ▶ record-fragment mode=replace on quality-gap surfaces             ║
║     (no append; flips fragments only when 100%-sure threshold        ║
║      holds per the rubric)                                           ║
╚══════════════════════════════════════════════════════════════════════╝
```

---

## Main agent — what it sees

The main agent is intentionally context-poor. It reads only:

- The current phase's `phase_entry/<phase>.md` atom (delivered by the
  handler at `action=start | enter-phase | complete-phase | status`).
- The brief returned by `build-brief` / `build-subagent-prompt` for any
  phase that dispatches sub-agents.

It never reads ZCP-internal state directly; it drives every phase via
`zerops_recipe action=...` and dispatches sub-agents in parallel via
the Agent tool, passing each the composed brief verbatim.

### Handler-delivered phase entries (main-agent context, per phase)

| Phase                | Atom                            | Size    |
|----------------------|---------------------------------|--------:|
| 0 research           | `phase_entry/research.md`       |  ~5 KB  |
| 1 provision          | `phase_entry/provision.md`      |  ~4 KB  |
| 2 scaffold           | `phase_entry/scaffold.md`       | ~12 KB  |
| 3 feature            | `phase_entry/feature.md`        |  ~6 KB  |
| 6 finalize           | `phase_entry/finalize.md`       |  ~6 KB  |

(Phases 4/5/7 dispatch sub-agents only; their phase_entry atoms ride
inside the sub-agent brief, not the main-agent context.)

---

## Phase 0 — Research

**Actor:** main agent.
**Brief:** none (handler-delivered atom only).

| Reads                                  | Writes                          |
|----------------------------------------|---------------------------------|
| `phase_entry/research.md` (~5 KB)      | `plan.json` (via `update-plan`) |
| Parent recipe inline (variable)        |                                 |
| `zerops_knowledge` guide pulls (lazy)  |                                 |

**Approximate context delivered:** ~5 KB + parent recipe (when set).

---

## Phase 1 — Provision

**Actor:** main agent.
**Brief:** none.

| Reads                                | Writes                                 |
|--------------------------------------|----------------------------------------|
| `phase_entry/provision.md` (~4 KB)   | 14 services live (`zerops_import`)     |
|                                      | project envs (`zerops_env`)            |

**Approximate context delivered:** ~4 KB.

---

## Phase 2 — Scaffold

**Actor:** N × scaffold sub-agents (one per codebase).
**Brief composer:** `BuildScaffoldBriefWithResolver` ([briefs.go:167](../../internal/recipe/briefs.go#L167)).
**Cap:** 48 KB.

### Atoms — always loaded

| Atom                                           |   Size  |
|------------------------------------------------|--------:|
| `briefs/scaffold/platform_principles.md`       |  ~5 KB  |
| `briefs/scaffold/preship_contract.md`          | ~0.6 KB |
| `briefs/scaffold/fact_recording.md`            | ~0.6 KB |
| `briefs/scaffold/decision_recording.md`        | ~14 KB  |
| `principles/dev-loop.md`                       | ~4.5 KB |
| `principles/mount-vs-container.md`             |  ~2 KB  |
| `principles/yaml-comment-style.md`             | ~3.3 KB |
| `principles/cross-service-urls.md` (run-20 C2) | ~7.3 KB |
| `principles/bare-yaml-prohibition.md` (run-20 C3) | ~1.6 KB |
| **Always-loaded subtotal**                     | **~39 KB** |

### Atoms — conditional

| Atom                                                | Size    | Trigger                                              |
|-----------------------------------------------------|--------:|------------------------------------------------------|
| `principles/init-commands-model.md`                 | ~2.8 KB | `anyCodebaseHasInitCommands(plan)`                   |
| `briefs/scaffold/build_tool_host_allowlist.md`      |  ~1 KB  | `cb.Role == frontend && nodejs base`                 |
| `briefs/scaffold/spa_static_runtime.md` (run-20 C2) | ~3.2 KB | `cb.Role == frontend && nodejs base`                 |

### Engine-derived sections

- Header (~0.2 KB)
- Role contract (~0.3 KB)
- Citation-guide map (~0.6 KB)
- Recipe-knowledge slug list (resolver-driven, ~0.5–2 KB)
- Tier-fact table (~1.5 KB; frontend role only)
- Parent recipe excerpt (~1.5 KB; **only** when
  `parent.Codebases[cb.Hostname]` exists — mere parent presence isn't
  enough)

### Sub-agent output

- Code (`src/**`)
- `zerops.yaml` (must be bare — comments are forbidden at scaffold
  time)
- Dev process running via `zerops_dev_server`
- Facts via `record-fact`

### Complete-phase gates (relevant subset)

- `scaffold-bare-yaml` — refuses `^\s+#` causal comment lines
  (carve-outs for shebang + trailing data-line comments)
- `fact-rationale-completeness` — every directive group in committed
  yaml needs an attesting `field_rationale` fact
- `worker-dev-server-started` — dev codebase with `start: zsc noop
  --silent` needs a `worker_dev_server_started` fact (or
  `worker_no_dev_server` bypass)
- `zerops-yaml-schema` (v9.46.0 RC2) — strict-mode schema validation
  against the live zerops-yml schema. Catches schema-invalid fields
  (e.g. `verticalAutoscaling` placed under `run:` — it's an
  import.yaml service-level field, not a zerops.yaml run-level field)
  in the producer's same-context window, not deferred to codebase-
  content / finalize where the authoring agent has moved on.

### Approximate brief size by codebase shape

| Codebase shape              | ~size   |
|-----------------------------|--------:|
| api / worker (no init)      | ~40 KB  |
| api / worker (with seed)    | ~43 KB  |
| frontend nodejs (showcase)  | ~48 KB  |

---

## Phase 3 — Feature

**Actor:** 1 feature sub-agent (cross-codebase).
**Brief composer:** `BuildFeatureBrief` ([briefs.go:321](../../internal/recipe/briefs.go#L321)).
**Cap:** 20 KB.

### Atoms — always loaded

| Atom                                | Size    |
|-------------------------------------|--------:|
| `briefs/feature/feature_kinds.md`   | ~2.3 KB |
| `briefs/feature/decision_recording.md` | ~5.4 KB |
| `principles/mount-vs-container.md`  |  ~2 KB  |
| `principles/yaml-comment-style.md`  | ~3.3 KB |
| **Always-loaded subtotal**          | **~13 KB** |

### Atoms — conditional

| Atom                                  | Size    | Trigger                                         |
|---------------------------------------|--------:|-------------------------------------------------|
| `principles/init-commands-model.md`   | ~2.8 KB | `planDeclaresSeed(plan)` — seed/scout-import/bootstrap |
| `briefs/feature/showcase_scenario.md` | ~6.5 KB | `plan.Tier == "showcase"`                       |

### Engine-derived sections

- Header (~0.2 KB)
- Symbol table — codebases + services (~0.5–1.5 KB)

### Sub-agent output

- Extended code
- Feature facts (`porter_change`, `field_rationale`)
- Browser-walk facts (`zerops_browser` + `record-fact`)

### Approximate brief size by tier

| Tier                       | ~size   |
|----------------------------|--------:|
| hello-world / minimal      | ~14 KB  |
| showcase (with seed)       | ~22 KB  |

---

## Phase 4a — Codebase content

**Actor:** N × codebase-content sub-agents (one per codebase, dispatched
in parallel with claudemd-author).
**Brief composer:** `BuildCodebaseContentBrief` ([briefs_content_phase.go:28](../../internal/recipe/briefs_content_phase.go#L28)).
**Cap:** 48 KB.

### Atoms — always loaded

| Atom                                                 |   Size  |
|------------------------------------------------------|--------:|
| `phase_entry/codebase-content.md`                    | ~2.8 KB |
| `briefs/codebase-content/synthesis_workflow.md`      | ~20 KB  |
| `briefs/scaffold/platform_principles.md` (cross-loaded) | ~5 KB |
| `principles/nats-shapes.md` (run-20 C1)              | ~2.7 KB |
| `principles/cross-service-urls.md` (run-20 C2)       | ~7.3 KB |
| `principles/zerops-knowledge-attestation.md`         |  ~3 KB  |
| `principles/yaml-comment-style.md`                   | ~3.3 KB |
| **Always-loaded subtotal**                           | **~44 KB** |

### Atoms — conditional

| Atom                                                          | Size    | Trigger                                  |
|---------------------------------------------------------------|--------:|------------------------------------------|
| `briefs/codebase-content/showcase_tier_supplements.md`        | ~2.6 KB | `plan.Tier == "showcase" && cb.IsWorker` |

### Engine-derived sections

- Citation-guide list (~0.6 KB)
- Codebase metadata (~0.3 KB)
- Filtered facts — `FilterByCodebase` then drop `EngineEmitted=true`;
  the kind mix is whatever the run recorded (porter_change,
  field_rationale, platform-trap). Variable: ~1–5 KB per codebase.
- On-demand pointer block (`zerops.yaml`, `src/**`, parent SourceRoot)
  (~0.5 KB)
- Sibling-sub-agent note (~0.2 KB)

### Sub-agent output (via `record-fragment`)

- `codebase/<h>/intro`
- `codebase/<h>/integration-guide/<n>`
- `codebase/<h>/knowledge-base`
- `codebase/<h>/zerops-yaml` — **v9.46.0 whole-yaml**: ONE fragment
  per codebase whose body is the entire commented zerops.yaml. Agent
  walks every field, comments where porter value calls for one,
  preserves yaml structure from the bare scaffold output. Stitcher
  writes the body verbatim to `<SourceRoot>/zerops.yaml`. Replaces the
  per-block `zerops-yaml-comments/<block>` shape because the per-
  block contract produced uneven coverage (the agent lost sight of the
  document when authoring N small slots).

  Slot-shape refusals at record time: empty body, doc-link punts
  (`Read more about it here:`, `More information at:`, `See docs:`,
  `For more details, see`, `See also:`), slug citations, structural
  changes vs the bare yaml (CRLF/BOM/trailing-whitespace normalized
  before structural compare).

  Schema gate (`gateZeropsYamlSchema`) fires at this phase's complete-
  phase AND at scaffold complete-phase (RC2 — producer-side catch).
  Pre-stitch at PhaseCodebaseContent so the gate reads the agent's
  fragment-derived on-disk yaml, not stale bare scaffold output.

### Approximate brief size

~45–50 KB per codebase (showcase worker codebases hit the cap).

---

## Phase 4b — CLAUDE.md author

**Actor:** N × claudemd-author sub-agents (parallel with
codebase-content).
**Brief composer:** `BuildClaudeMDBrief` ([briefs_content_phase.go:352](../../internal/recipe/briefs_content_phase.go#L352)).
**Cap:** 8 KB. Strictly Zerops-free by construction.

### Atoms — always loaded

| Atom                                                  | Size    |
|-------------------------------------------------------|--------:|
| `phase_entry/claudemd-author.md`                      | ~2.1 KB |
| `briefs/claudemd-author/zerops_free_prohibition.md`   | ~0.8 KB |
| **Always-loaded subtotal**                            | **~3 KB**  |

### Engine-derived sections

- Codebase metadata (~0.2 KB)
- On-demand pointer block — `package.json` / `composer.json` (when
  PHP-flavored) / `src/**` / `app/**` (Laravel). `zerops.yaml`
  deliberately excluded. (~0.4 KB)
- Output instruction (~0.1 KB)

### Sub-agent output

- `codebase/<h>/claude-md` — single fragment, single slot.

### Approximate brief size

~3.5–4 KB per codebase.

---

## Phase 5 — Env content

**Actor:** 1 env-content sub-agent.
**Brief composer:** `BuildEnvContentBrief` ([briefs_content_phase.go:208](../../internal/recipe/briefs_content_phase.go#L208)).
**Cap:** 48 KB.

### Atoms — always loaded

| Atom                                              | Size    |
|---------------------------------------------------|--------:|
| `phase_entry/env-content.md`                      | ~1.2 KB |
| `briefs/env-content/per_tier_authoring.md`        |  ~8 KB  |
| `principles/nats-shapes.md` (run-20 C1 — wired here precisely to close run-19 T0+T5 JetStream fabrication) | ~2.7 KB |
| `principles/zerops-knowledge-attestation.md`      |  ~3 KB  |
| `principles/yaml-comment-style.md`                | ~3.3 KB |
| **Always-loaded subtotal**                        | **~18 KB** |

### Engine-derived sections

- Per-tier capability matrix from `Tiers()` (~0.5 KB)
- Cross-tier deltas from `tiers.go::Diff` (~0.8 KB)
- Engine-emitted `tier_decision` facts (~1–3 KB)
- Cross-codebase contract facts (0–5 records, ~0.5 KB)
- Plan snapshot — codebases + services (~0.5 KB)
- Parent pointer (~0.3 KB; when set)

### Sub-agent output (via `record-fragment`)

- `env/N/intro` × 6 tiers
- `env/N/import-comments/<svc>` per tier per host

### Approximate brief size

~20–25 KB.

---

## Phase 6a — Finalize (sub-agent authoring)

**Actor:** 1 finalize sub-agent.
**Brief composer:** `BuildFinalizeBrief` ([briefs.go:398](../../internal/recipe/briefs.go#L398)).
**Cap:** 14 KB.

### Atoms — always loaded

| Atom                                       | Size    |
|--------------------------------------------|--------:|
| `briefs/finalize/intro.md`                 |  ~1 KB  |
| `briefs/finalize/validator_tripwires.md`   |  ~3 KB  |
| `briefs/finalize/anti_patterns.md`         | ~0.8 KB |
| **Always-loaded subtotal**                 | **~5 KB**  |

### Engine-derived sections

- Header (~0.2 KB)
- Tier map from `Tiers()` (~1 KB)
- Tier-fact table from `BuildTierFactTable(plan)` (~1.5 KB)
- Audience paths — per-codebase SourceRoot (~0.5 KB)
- Fragment list from `formatFinalizeFragmentList(plan)` (~3–5 KB
  depending on N codebases × 6 tiers)
- Fragment-count math from `finalizeFragmentMath(plan)` (~0.5 KB)
- Symbol table — codebases + services (~0.5 KB)

### Sub-agent output (via `record-fragment`)

- `root/intro`
- `env/N/intro` × 6
- `env/N/import-comments/project` × 6
- `env/N/import-comments/<host>` per codebase + managed service × 6

The fragment list + math drive the exact authoring set; the math line
is the ex-wrapper drift fix from run-10 S-1 (hand-typed 89, actual 67).

### Approximate brief size

~10–13 KB.

---

## Phase 6b — Stitch (engine)

**Actor:** engine code; no sub-agent dispatch.
**Trigger:** sub-agent calls `stitch-content` after fragments are
recorded.

| Reads                                       | Writes                                 |
|---------------------------------------------|----------------------------------------|
| Recorded fragments + plan (no atom reads)   | `AssembleRoot/Env/Codebase` READMEs    |
|                                             | `AssembleCodebaseClaudeMD`             |
|                                             | `EmitDeliverableYAML` × 6              |
|                                             | `WriteCodebaseYAMLWithComments`        |
|                                             | (← double-inject site)                 |

**Approximate context delivered:** 0 KB (engine-only).

---

## Phase 7 — Refinement (optional)

**Actor:** 1 refinement sub-agent.
**Brief composer:** `BuildRefinementBrief` ([briefs_refinement.go:43](../../internal/recipe/briefs_refinement.go#L43)).

### Atoms — always loaded

| Atom                                                 | Size    |
|------------------------------------------------------|--------:|
| `phase_entry/refinement.md`                          | ~3.7 KB |
| `briefs/refinement/synthesis_workflow.md`            | ~6.5 KB |
| `briefs/refinement/reference_kb_shapes.md`           | ~14.7 KB |
| `briefs/refinement/reference_ig_one_mechanism.md`    | ~13 KB  |
| `briefs/refinement/reference_voice_patterns.md`      | ~8.6 KB |
| `briefs/refinement/reference_yaml_comments.md`       | ~9.8 KB |
| `briefs/refinement/reference_citations.md`           | ~13.1 KB |
| `briefs/refinement/reference_trade_offs.md`          | ~11.6 KB |
| `briefs/refinement/refinement_thresholds.md`         | ~13 KB  |
| `briefs/refinement/embedded_rubric.md`               | ~20 KB  |
| **Always-loaded subtotal**                           | **~114 KB** |

### Engine-derived sections

- Stitched-output pointer block — root, tier × 6, per-codebase
  README/yaml/CLAUDE.md (~1–2 KB)
- Run-wide facts log — full snapshot, no truncation. Variable: 20–100
  facts × ~150 B = ~3–15 KB.

### Sub-agent output

- `record-fragment mode=replace` on quality-gap surfaces (no append;
  the rubric flips fragments only when the 100%-sure threshold holds).

### Approximate brief size

~115–130 KB. By far the heaviest brief — refinement is an entire-corpus
re-read, deliberately context-dense.

---

## Brief size summary

| Phase                       | Per-dispatch brief | Dispatches per run    | Aggregate per run        |
|-----------------------------|-------------------:|-----------------------|-------------------------:|
| 0 research                  | n/a                | main only             | ~5 KB                    |
| 1 provision                 | n/a                | main only             | ~4 KB                    |
| 2 scaffold                  | ~40–48 KB          | N codebases (1–3)     | ~40–144 KB               |
| 3 feature                   | ~14–22 KB          | 1 (cross-codebase)    | ~14–22 KB                |
| 4a codebase-content         | ~45–50 KB          | N codebases           | ~45–150 KB               |
| 4b claudemd-author          | ~3.5–4 KB          | N codebases           | ~3.5–12 KB               |
| 5 env-content               | ~20–25 KB          | 1                     | ~20–25 KB                |
| 6a finalize (sub-agent)     | ~10–13 KB          | 1                     | ~10–13 KB                |
| 6b stitch (engine)          | 0 KB               | engine only           | 0 KB                     |
| 7 refinement (optional)     | ~115–130 KB        | 1 (when triggered)    | ~115–130 KB              |

A typical 3-codebase showcase run dispatches ~10 sub-agents and burns
roughly 350–500 KB of brief context across the pipeline (excluding
refinement) — about 80% of which is the Phase 2 + 4a corpora, where
mechanics-rich teaching needs to land at every codebase.
