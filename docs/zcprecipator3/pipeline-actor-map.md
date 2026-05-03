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
║                       brief size: ~22–32 KB     (cap 48 KB)          ║
╠══════════════════════════════════════════════════════════════════════╣
║ READS — always                                                       ║
║   briefs/scaffold/platform_principles.md                       ~5 KB ║
║   briefs/scaffold/preship_contract.md                        ~0.6 KB ║
║   briefs/scaffold/fact_recording.md                          ~0.6 KB ║
║   briefs/scaffold/decision_recording_slim.md  (run-21 R2-1)  ~1.2 KB ║
║     (full decision_recording.md retired from scaffold; agent reads   ║
║      schema from handlers.go RecipeInput.Fact jsonschema)            ║
║   principles/dev-loop.md                                     ~4.5 KB ║
║   principles/mount-vs-container.md         (run-21 R2-4)       ~2 KB ║
║     (carries stage-slot negative rule)                               ║
║   principles/cross-service-urls.md         (run-20 C2)       ~7.3 KB ║
║   principles/bare-yaml-prohibition.md      (run-20 C3)       ~1.6 KB ║
║   + citation-guide list, recipe-knowledge slug list            ~2 KB ║
║                                                                      ║
║   (run-21 R2-1: yaml-comment-style.md DROPPED — contradicts          ║
║    bare-yaml-prohibition; lives in codebase-content brief instead)   ║
║                                                                      ║
║ READS — conditional                                                  ║
║   principles/init-commands-model.md      ─ cb.HasInitCommands        ║
║                                            (run-21 R2-1: per-cb,     ║
║                                            no longer plan-wide leak) ║
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
║                            brief size: ~12–20 KB     (cap 22 KB)     ║
╠══════════════════════════════════════════════════════════════════════╣
║ READS — always                                                       ║
║   briefs/feature/sshfs_warning.md         (run-21 R2-7)      ~0.7 KB ║
║     (loaded FIRST so the agent encounters it before any feature      ║
║      guidance; closes the run-21 features-2nd Vite-on-SSHFS rabbit)  ║
║   briefs/feature/feature_kinds.md                            ~2.3 KB ║
║   briefs/feature/decision_recording.md                       ~5.4 KB ║
║   principles/mount-vs-container.md                             ~2 KB ║
║   + symbol table                                          ~0.5–1.5 KB║
║   + closing-footer SSHFS reminder         (run-21 R2-7)      ~0.3 KB ║
║                                                                      ║
║   (run-21 R3-2: yaml-comment-style.md DROPPED — bare-yaml contract   ║
║    applies at feature too; teaching belongs in codebase-content)     ║
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
║     synthesis_workflow.md     ~22 KB ║  ║   briefs/claudemd-author/║
║     (run-21 P0-3 / R3-1 /            ║  ║     zerops_free_-        ║
║      cap-trim: inline goldens,       ║  ║     prohibition.md       ║
║      disk-vs-fragment clarified,     ║  ║                  ~0.8 KB ║
║      trimmed to fit cap)             ║  ║                          ║
║   briefs/scaffold/                   ║  ║ ENGINE-DERIVED           ║
║     platform_principles.md     ~5 KB ║  ║   on-demand pointers     ║
║     (cross-loaded)                   ║  ║   (package.json /        ║
║   principles/zerops-knowledge-       ║  ║    composer.json /       ║
║     attestation.md             ~3 KB ║  ║    src/** / app/**;      ║
║   principles/yaml-comment-style.md   ║  ║    zerops.yaml DELIBER-  ║
║                              ~3.3 KB ║  ║    ATELY EXCLUDED)       ║
║                                      ║  ║                          ║
║ READS — conditional                  ║  ║ Zerops-free by           ║
║   principles/nats-shapes.md          ║  ║ construction             ║
║     (run-21 R2-2: per-cb gate via    ║  ║                          ║
║      shouldLoadNATSShapes — drops    ║  ║ NOTE: engine-side        ║
║      for frontends; load only when   ║  ║ Zerops-content guards    ║
║      cb.ConsumesServices includes a  ║  ║ on CLAUDE.md retired in  ║
║      nats@* service)         ~2.7 KB ║  ║ run-21 R2-5. Brief at    ║
║   principles/cross-service-urls.md   ║  ║ zerops_free_prohibition  ║
║     (run-21 R2-2: per-cb gate via    ║  ║ is the authoring         ║
║      shouldLoadCrossServiceURLs —    ║  ║ contract; validators do  ║
║      drop when cb.ConsumesServices   ║  ║ structural shape only    ║
║      is empty non-nil) (C2)  ~7.3 KB ║  ║ (line cap, H2 count,     ║
║   showcase_tier_supplements.md       ║  ║ min size).               ║
║   ─ showcase tier + cb.IsWorker      ║  ║                          ║
║                                      ║  ║ WRITES                   ║
║ ENGINE-DERIVED                       ║  ║   ▶ codebase/<h>/        ║
║   citation-guide list        ~0.6 KB ║  ║       claude-md          ║
║   managed-services block             ║  ║     (single fragment)    ║
║   (run-21 R2-3: filtered to          ║  ║                          ║
║    cb.ConsumesServices; SPAs get     ║  ║                          ║
║    no block at all if they consume   ║  ║                          ║
║    no managed service)      ~0–1 KB  ║  ║                          ║
║   filtered facts            ~1–5 KB  ║  ║                          ║
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
║                                       brief size: ~17–25 KB (cap 48) ║
╠══════════════════════════════════════════════════════════════════════╣
║ READS — always                                                       ║
║   phase_entry/env-content.md                                 ~1.2 KB ║
║   briefs/env-content/per_tier_authoring.md                     ~8 KB ║
║   principles/zerops-knowledge-attestation.md                   ~3 KB ║
║   principles/yaml-comment-style.md                           ~3.3 KB ║
║                                                                      ║
║ READS — conditional                                                  ║
║   principles/nats-shapes.md  (run-20 C1 — wired here precisely       ║
║      to close the run-19 T0+T5 JetStream fabrication)        ~2.7 KB ║
║      (run-21 R3-2: gated on planUsesNATS(plan); plans without a      ║
║       nats@* service drop the atom — dead weight otherwise)          ║
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
║       (run-21 P0-1 Layer B: atomic write — tmp + sync + chmod +      ║
║        rename in same dir; closes the truncate-then-write race       ║
║        against any concurrent disk reader)                           ║
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

| Atom                                                | Size    |
|-----------------------------------------------------|--------:|
| `briefs/scaffold/platform_principles.md`            |  ~5 KB  |
| `briefs/scaffold/preship_contract.md`               | ~0.6 KB |
|   ↑ run-21 R2-6 rewrite: bounds verification to runnable surface     |
|     (deploy + /health + ONE happy-path read); cross-service +        |
|     behavior matrices explicitly delegated to feature                |
| `briefs/scaffold/fact_recording.md`                 | ~0.6 KB |
| `briefs/scaffold/decision_recording_slim.md` (run-21 R2-1) | ~1.2 KB |
|   ↑ replaces the legacy 14 KB `decision_recording.md` (worked        |
|     examples retired from scaffold; full Fact schema lives on the    |
|     `RecipeInput.Fact` jsonschema description in handlers.go)        |
| `principles/dev-loop.md`                            | ~4.5 KB |
| `principles/mount-vs-container.md` (run-21 R2-4)    |  ~2 KB  |
|   ↑ now carries the stage-slot negative rule: `<host>stage` is a     |
|     deployed runtime target, not a source mount                      |
| `principles/cross-service-urls.md` (run-20 C2)      | ~7.3 KB |
| `principles/bare-yaml-prohibition.md` (run-20 C3)   | ~1.6 KB |
| **Always-loaded subtotal**                          | **~23 KB** |

`principles/yaml-comment-style.md` was dropped at scaffold by run-21
R2-1 — it contradicts `bare-yaml-prohibition.md` (scaffold yaml is
bare; causal comments are authored at codebase-content phase). The
atom still loads at codebase-content brief.

### Atoms — conditional

| Atom                                                | Size    | Trigger                                                     |
|-----------------------------------------------------|--------:|-------------------------------------------------------------|
| `principles/init-commands-model.md`                 | ~2.8 KB | `cb.HasInitCommands` (run-21 R2-1: per-codebase, not plan-wide) |
| `briefs/scaffold/build_tool_host_allowlist.md`      |  ~1 KB  | `cb.Role == frontend && nodejs base`                        |
| `briefs/scaffold/spa_static_runtime.md` (run-20 C2) | ~3.2 KB | `cb.Role == frontend && nodejs base`                        |

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

### Approximate brief size by codebase shape (post run-21 R2-1)

| Codebase shape              | ~size   |
|-----------------------------|--------:|
| api / worker (no init)      | ~24 KB  |
| api / worker (with seed)    | ~27 KB  |
| frontend nodejs (showcase)  | ~32 KB  |

Pinned by `TestBrief_Scaffold_FrontendSPA_UnderTargetSize` at the 35 KB
ceiling.

---

## Phase 3 — Feature

**Actor:** 1 feature sub-agent (cross-codebase).
**Brief composer:** `BuildFeatureBrief` ([briefs.go:336](../../internal/recipe/briefs.go#L336)).
**Cap:** 22 KB (run-21 R2-4 raised from 20 KB; R3-2 will tighten back
down once env-content is fully slimmed).

### Atoms — always loaded

| Atom                                   | Size    |
|----------------------------------------|--------:|
| `briefs/feature/sshfs_warning.md` (run-21 R2-7) | ~0.7 KB |
|   ↑ loaded FIRST so the agent encounters it before any feature-      |
|     content guidance. Closes the run-21 features-2nd Vite-on-SSHFS   |
|     ESM-import rabbit hole (warning was previously buried mid-doc    |
|     in a brief that wasn't even composed at feature)                 |
| `briefs/feature/feature_kinds.md`      | ~2.3 KB |
| `briefs/feature/decision_recording.md` | ~5.4 KB |
| `principles/mount-vs-container.md`     |  ~2 KB  |
| **Always-loaded subtotal**             | **~10 KB** |

`principles/yaml-comment-style.md` was dropped at feature by run-21
R3-2 — same reasoning as scaffold (bare-yaml authoring contract;
comment-style teaching belongs in codebase-content).

### Atoms — conditional

| Atom                                  | Size    | Trigger                                         |
|---------------------------------------|--------:|-------------------------------------------------|
| `principles/init-commands-model.md`   | ~2.8 KB | `planDeclaresSeed(plan)` — seed/scout-import/bootstrap |
| `briefs/feature/showcase_scenario.md` | ~6.5 KB | `plan.Tier == "showcase"`                       |

### Engine-derived sections

- Header (~0.2 KB)
- Symbol table — codebases + services (~0.5–1.5 KB)
- Closing-footer SSHFS reminder (run-21 R2-7) — re-states the SSHFS
  rule at brief close so the agent re-encounters it before terminating.
  Lives in `briefs_subagent_prompt.go::writePromptCloseFooter`.
  (~0.3 KB)

### Sub-agent output

- Extended code
- Feature facts (`porter_change`, `field_rationale`)
- Browser-walk facts (`zerops_browser` + `record-fact`)

### Approximate brief size by tier

| Tier                       | ~size   |
|----------------------------|--------:|
| hello-world / minimal      | ~12 KB  |
| showcase (with seed)       | ~20 KB  |

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
| `briefs/codebase-content/synthesis_workflow.md`      | ~22 KB  |
|   ↑ run-21 P0-3 (golden excerpts inlined to close goldens-hunting),  |
|     R3-1 (disk-vs-fragment authority clarified — fragment is source  |
|     of truth, engine stitches it to disk before gates run),          |
|     cap-trim follow-up (excerpts trimmed to fit cap)                 |
| `briefs/scaffold/platform_principles.md` (cross-loaded) | ~5 KB |
| `principles/zerops-knowledge-attestation.md`         |  ~3 KB  |
| `principles/yaml-comment-style.md`                   | ~3.3 KB |
| **Always-loaded subtotal**                           | **~36 KB** |

### Atoms — conditional

| Atom                                                          | Size    | Trigger                                                                |
|---------------------------------------------------------------|--------:|------------------------------------------------------------------------|
| `principles/nats-shapes.md` (run-20 C1)                       | ~2.7 KB | `shouldLoadNATSShapes(plan, cb)` (run-21 R2-2): drop for frontends; load only when cb consumes a `nats@*` service |
| `principles/cross-service-urls.md` (run-20 C2)                | ~7.3 KB | `shouldLoadCrossServiceURLs(cb)` (run-21 R2-2): drop when `cb.ConsumesServices` is empty non-nil (codebase analyzed, no managed deps) |
| `briefs/codebase-content/showcase_tier_supplements.md`        | ~2.6 KB | `plan.Tier == "showcase" && cb.IsWorker`                               |

Both NATS-shapes and cross-service-urls fall back to load-all when
`cb.ConsumesServices == nil` (sim-path back-compat for codebases the
engine couldn't analyze — `populateConsumesServicesFromYaml` runs at
scaffold complete-phase only).

### Engine-derived sections

- Citation-guide list (~0.6 KB)
- Codebase metadata (~0.3 KB)
- Recipe-context **Managed services block** (run-21 R2-3): filtered to
  `cb.ConsumesServices` so a SPA codebase that consumes only
  `${api_zeropsSubdomain}` no longer sees db/cache/broker/search/
  storage in its dispatched brief. Three-state semantics: nil →
  full list (back-compat), `[]string{}` → block omitted entirely,
  populated → named services only. (~0–1 KB)
- Filtered facts — `FilterByCodebase` then drop `EngineEmitted=true`;
  the kind mix is whatever the run recorded (porter_change,
  field_rationale, platform-trap). Variable: ~1–5 KB per codebase.
- On-demand pointer block (`zerops.yaml`, `src/**`, parent SourceRoot)
  (~0.5 KB)
- `zerops_knowledge` consultation reminder (run-21 P0-2 — replaces the
  retired "fill engine-pre-seeded codebase facts" prompt).
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
  Pre-stitch at PhaseCodebaseContent so the on-disk yaml is the
  agent's fragment-derived body, not stale bare scaffold output.

  Run-21 P0-1 Layer A: the gate now prefers the in-memory fragment
  body (`Plan.Fragments[codebase/<h>/zerops-yaml]`) over the disk read
  when present. Eliminates the truncate-then-write race that blocked
  all 3 codebase-content sub-agents in run-21. Disk fallback retained
  for SSH-edited yaml that bypasses fragment recording.

### Approximate brief size

~37–50 KB per codebase. SPAs that consume nothing managed drop the
NATS + cross-service-URL atoms (~10 KB lighter); showcase worker
codebases that load every conditional still hit the cap.

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
| `principles/zerops-knowledge-attestation.md`      |  ~3 KB  |
| `principles/yaml-comment-style.md`                | ~3.3 KB |
| **Always-loaded subtotal**                        | **~15 KB** |

### Atoms — conditional

| Atom                                  | Size    | Trigger                                                               |
|---------------------------------------|--------:|-----------------------------------------------------------------------|
| `principles/nats-shapes.md` (run-20 C1 — wired here to close run-19 T0+T5 JetStream fabrication) | ~2.7 KB | `planUsesNATS(plan)` (run-21 R3-2): plans without a `nats@*` service drop the atom — dead weight otherwise |

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

~17–25 KB. Plans without a NATS broker drop ~2.7 KB; plans with NATS
keep the C1 fabrication-defense atom.

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
|                                             | (← run-21 P0-1 Layer B: atomic         |
|                                             |  write — tmp + sync + chmod + rename   |
|                                             |  in same dir; closes the truncate-     |
|                                             |  then-write race against concurrent    |
|                                             |  disk readers)                         |

`gateZeropsYamlSchema` (in the gate set, not stitch itself) prefers
the in-memory `Plan.Fragments[codebase/<h>/zerops-yaml]` body over a
disk read when the fragment is recorded — Layer A of the run-21 P0-1
race fix. Disk fallback retained for SSH-edit-only paths.

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
| 2 scaffold                  | ~24–32 KB          | N codebases (1–3)     | ~24–96 KB                |
| 3 feature                   | ~12–20 KB          | 1 (cross-codebase)    | ~12–20 KB                |
| 4a codebase-content         | ~37–50 KB          | N codebases           | ~37–150 KB               |
| 4b claudemd-author          | ~3.5–4 KB          | N codebases           | ~3.5–12 KB               |
| 5 env-content               | ~17–25 KB          | 1                     | ~17–25 KB                |
| 6a finalize (sub-agent)     | ~10–13 KB          | 1                     | ~10–13 KB                |
| 6b stitch (engine)          | 0 KB               | engine only           | 0 KB                     |
| 7 refinement (optional)     | ~115–130 KB        | 1 (when triggered)    | ~115–130 KB              |

A typical 3-codebase showcase run dispatches ~10 sub-agents and burns
roughly 200–350 KB of brief context across the pipeline (excluding
refinement) — about 70% of which is the Phase 4a corpus, where
mechanics-rich teaching needs to land at every codebase.

Run-21 fix-pack net effect: scaffold dropped ~15 KB per codebase
(R2-1 slim), feature dropped ~3 KB (R3-2 slim), codebase-content
dropped ~10 KB on per-codebase-conditional atoms (R2-2 + R2-3),
env-content dropped ~3 KB on broker-less plans (R3-2). Aggregate
savings on a 3-codebase showcase: ~70 KB of dispatched-brief context.
