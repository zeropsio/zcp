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
║                       brief size: ~24–44 KB     (cap 48 KB)          ║
╠══════════════════════════════════════════════════════════════════════╣
║ READS — always                                                       ║
║   briefs/scaffold/platform_principles.md   (run-22 R1-RC-2)  ~5.6 KB ║
║     (same-key shadow extended to project-level vars)                 ║
║   briefs/scaffold/preship_contract.md                          ~1 KB ║
║   briefs/scaffold/decision_recording_slim.md  (run-21 R2-1   ~3.6 KB ║
║                                       + run-22 R3-C-2/4/5)           ║
║     (full decision_recording.md retired from scaffold; agent reads   ║
║      schema from handlers.go RecipeInput.Fact jsonschema; R3-C-2/4/5 ║
║      added topic-vs-kind separation, citationGuide example)          ║
║   principles/dev-loop.md                                     ~4.5 KB ║
║   principles/mount-vs-container.md   (run-21 R2-4              ~4 KB ║
║                                       + run-22 R2-RC-5)              ║
║     (R2-4 stage-slot negative rule + R2-RC-5 edit-in-place during    ║
║      feature: do not zerops_deploy <host>dev; restart dev-server     ║
║      for env-var changes)                                            ║
║   principles/cross-service-urls.md   (run-20 C2 + run-22    ~10.5 KB ║
║                                       R2-RC-1 + R3-RC-3)             ║
║     (R2-RC-1 setup names → generic; R3-RC-3 update-plan              ║
║      projectEnvVars channel-sync teaching)                           ║
║   principles/bare-yaml-prohibition.md   (run-20 C3 + run-22  ~1.6 KB ║
║                                          R2-RC-1 setup name)         ║
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
║   briefs/scaffold/spa_static_runtime.md  (run-20 C2 layer 3 +        ║
║                                           run-22 R2-RC-1 setup name) ║
║                                          ─ frontend + nodejs         ║
║   tier-fact table  ─ frontend role only                              ║
║   parent excerpt   ─ filesystem path: parent.Codebases[cb.Hostname]  ║
║                      exists                                          ║
║   parent baseline  (run-22 R3-RC-0) ─ EMBEDDED fallback when         ║
║                     parent==nil AND parentSlugFor(slug)!="" AND      ║
║                     internal/knowledge/recipes/<parent>.md exists.   ║
║                     Closes the run-22 cascade root: filesystem mount ║
║                     wasn't populated in dogfood container; embedded  ║
║                     corpus IS in the binary via //go:embed.          ║
║                                                                      ║
║ WRITES                                                               ║
║   ▶ code (src/**)                                                    ║
║   ▶ zerops.yaml  (must be bare — comments forbidden at scaffold;     ║
║      run-22 R2-RC-1: setup names use generic prod/dev/worker)        ║
║   ▶ dev process running via zerops_dev_server                        ║
║   ▶ facts (record-fact; run-22 R3-C-3: warn-on-record for            ║
║      class×surface compatibility violations as Notice)               ║
╚══════════════════════════════════════════════════════════════════════╝
                                  │
                                  ▼

╔══════════════════════════════════════════════════════════════════════╗
║  3  FEATURE                actor: 1 feature sub-agent (cross-cb)     ║
║                            brief size: ~13–22 KB     (cap 22 KB)     ║
╠══════════════════════════════════════════════════════════════════════╣
║ READS — always                                                       ║
║   briefs/feature/sshfs_warning.md         (run-21 R2-7)      ~0.7 KB ║
║     (loaded FIRST so the agent encounters it before any feature      ║
║      guidance; closes the run-21 features-2nd Vite-on-SSHFS rabbit)  ║
║   briefs/feature/feature_kinds.md                            ~2.2 KB ║
║   briefs/feature/decision_recording.md                       ~5.4 KB ║
║   principles/mount-vs-container.md   (run-21 R2-4 +            ~4 KB ║
║                                       run-22 R2-RC-5)                ║
║     (R2-RC-5 edit-in-place rule reaches feature here AND scaffold    ║
║      via the same atom; closes the run-22 dev-redeploy thrash)       ║
║   + symbol table                                          ~0.5–1.5 KB║
║   + closing-footer SSHFS reminder         (run-21 R2-7)      ~0.3 KB ║
║                                                                      ║
║   (run-21 R3-2: yaml-comment-style.md DROPPED — bare-yaml contract   ║
║    applies at feature too; teaching belongs in codebase-content)     ║
║   (run-22 R2-RC-5: feature/content_extension.md marked DEPRECATED —  ║
║    has been unloaded by BuildFeatureBrief since run-16 §6.2; header  ║
║    marker added so future maintainers don't edit-without-effect)     ║
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
║                                                                      ║
║ FORBIDDEN (run-22 R2-RC-5 edit-in-place rule)                        ║
║   ✗ zerops_deploy targetService=<host>dev — code is already live     ║
║     via SSHFS; dev server picks up changes via watch                 ║
║   ✗ zerops_deploy "to apply env-var changes" — restart the dev       ║
║     server via zerops_dev_server action=restart instead              ║
║   ✓ ONE legitimate cross-deploy per feature: target stage slot       ║
║     (e.g. apistage) when in-place verification has passed            ║
╚══════════════════════════════════════════════════════════════════════╝
                                  │
                ┌─────────────────┴─────────────────┐
                ▼                                   ▼

╔══════════════════════════════════════╗  ╔══════════════════════════╗
║  4a  CODEBASE-CONTENT                ║  ║  4b  CLAUDE.MD AUTHOR    ║
║                                      ║  ║                          ║
║  N × codebase-content sub-agents     ║  ║  N × claudemd-author     ║
║  brief: ~45–55 KB  (cap 56 KB —      ║  ║  brief: ~3.5–4 KB        ║
║   run-22 R1+R2 bumped 48→52→56)      ║  ║          (cap 8 KB)      ║
║                                      ║  ║                          ║
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
║     platform_principles.md   ~5.6 KB ║  ║   on-demand pointers     ║
║     (cross-loaded; run-22 R1-RC-2    ║  ║   (package.json /        ║
║      project-level shadow rule)      ║  ║    composer.json /       ║
║   principles/zerops-knowledge-       ║  ║    src/** / app/**;      ║
║     attestation.md             ~3 KB ║  ║    zerops.yaml DELIBER-  ║
║   principles/yaml-comment-style.md   ║  ║    ATELY EXCLUDED)       ║
║                              ~3.6 KB ║  ║                          ║
║   (run-22 R1-RC-4: Unicode           ║  ║ Zerops-free by           ║
║    box-drawing forbid)               ║  ║ construction             ║
║                                      ║  ║                          ║
║ READS — conditional                  ║  ║ NOTE: engine-side        ║
║   principles/nats-shapes.md          ║  ║ Zerops-content guards    ║
║     (run-21 R2-2: per-cb gate via    ║  ║ on CLAUDE.md retired in  ║
║      shouldLoadNATSShapes — drops    ║  ║ run-21 R2-5. Brief at    ║
║      for frontends; load only when   ║  ║ zerops_free_prohibition  ║
║      cb.ConsumesServices includes a  ║  ║ is the authoring         ║
║      nats@* service)         ~2.7 KB ║  ║ contract; validators do  ║
║   principles/cross-service-urls.md   ║  ║ structural shape only    ║
║     (run-21 R2-2: per-cb gate via    ║  ║ (line cap, H2 count,     ║
║      shouldLoadCrossServiceURLs —    ║  ║ min size).               ║
║      drop when cb.ConsumesServices   ║  ║                          ║
║      is empty non-nil; run-22        ║  ║ WRITES                   ║
║      R3-RC-3 update-plan teaching    ║  ║   ▶ codebase/<h>/        ║
║      added)                 ~10.5 KB ║  ║       claude-md          ║
║   worker_kb_supplements.md           ║  ║     (single fragment)    ║
║   ─ showcase tier + cb.IsWorker      ║  ║                          ║
║     (run-22 R2-WK-1+2 + followup     ║  ║                          ║
║      F-5: KB-content shape only      ║  ║                          ║
║      after split — code shape moved  ║  ║                          ║
║      to feature; ~3 KB; names the    ║  ║                          ║
║      validator gate by file path)    ║  ║                          ║
║                                      ║  ║                          ║
║ ENGINE-DERIVED                       ║  ║                          ║
║   citation-guide list        ~0.6 KB ║  ║                          ║
║   managed-services block             ║  ║                          ║
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
║ GATES (run at complete-phase)        ║  ║                          ║
║   gateZeropsYamlSchema               ║  ║                          ║
║   gateWorkerSubscription  (run-22    ║  ║                          ║
║     R2-WK-1+2) — regex-based source  ║  ║                          ║
║     scan over showcase-tier worker   ║  ║                          ║
║     codebases; refuses naked         ║  ║                          ║
║     `nc.subscribe(SUBJECT)` (no      ║  ║                          ║
║     queue option) and warns on       ║  ║                          ║
║     `unsubscribe()` shutdown.        ║  ║                          ║
║     NATS-context heuristic to avoid  ║  ║                          ║
║     rxjs/EventEmitter false-pos.     ║  ║                          ║
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
║                                       brief size: ~19–28 KB (cap 56) ║
║                                       (run-22 R1+R2 bumped 48→52→56) ║
╠══════════════════════════════════════════════════════════════════════╣
║ READS — always                                                       ║
║   phase_entry/env-content.md                                 ~1.2 KB ║
║   briefs/env-content/per_tier_authoring.md  (run-22 R2-RC-6) ~9.8 KB ║
║     (canonical-set dedup vs per-tier flavor — keep 1-2 line per-     ║
║      service framing every tier even when no field changes; closes   ║
║      run-22's tier 1/2/3 over-strip)                                 ║
║   principles/zerops-knowledge-attestation.md                   ~3 KB ║
║   principles/yaml-comment-style.md  (run-22 R1-RC-4)         ~3.6 KB ║
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
║                                                                      ║
║ NOTE on URL constants (run-22 R3-RC-3)                               ║
║   The agent is taught at scaffold via cross-service-urls.md to call  ║
║   BOTH `zerops_env action=set` (live workspace) AND `update-plan`    ║
║   with `projectEnvVars` (Plan channel) for STAGE_API_URL etc. The    ║
║   tier emit at finalize reads ONLY Plan.ProjectEnvVars; without the  ║
║   update-plan call, tier yamls ship empty project.envVariables and  ║
║   the codebase yaml's ${DEV_API_URL}/${STAGE_API_URL} dangle. Run-22 ║
║   evidence: 6 tier yamls × 4 missing constants = 24 unmaterialized   ║
║   references. Closure: brief teaches both channels + emit per-tier   ║
║   rewrite (see Phase 6b stitch).                                     ║
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
║       (run-22 R3-RC-3 — writeProject extended with                   ║
║        rewriteURLsForSingleSlot helper. For tiers 2-5                ║
║        (!tier.RunsDevContainer): drops DEV_*-prefixed keys           ║
║        from project.envVariables (single-slot tiers have no          ║
║        separate dev runtime); collapses slot-named hostnames         ║
║        in URL values — apidev-/apistage- → api-, appdev-/            ║
║        appstage- → app-, workerdev-/workerstage- → worker-.          ║
║        Preserves ${zeropsSubdomainHost} literal for end-user         ║
║        click-deploy minting. Tiers 0-1 keep dev-pair URLs            ║
║        verbatim.)                                                    ║
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
║   briefs/refinement/embedded_rubric.md  (run-22 R1-RC-7 +    ~21 KB  ║
║                                          R3-C-1 additions)           ║
║     (R1-RC-7 added "Tier-promotion narrative (forbidden per spec     ║
║      §108)" section with case-insensitive regex set;                 ║
║      R3-C-1 added subdomain "rotate" overclaim guard)                ║
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
| `briefs/scaffold/platform_principles.md` (run-22 R1-RC-2) | ~5.6 KB |
|   ↑ run-22 R1-RC-2 extended same-key shadow trap to project-level    |
|     vars (APP_SECRET, STAGE_API_URL, etc.) — pre-fix only enumerated |
|     cross-service auto-injects, run-22 dogfood agent inferred the    |
|     rule didn't apply to APP_SECRET and shipped a self-shadowed yaml |
| `briefs/scaffold/preship_contract.md`               |  ~1 KB  |
|   ↑ run-21 R2-6 rewrite: bounds verification to runnable surface     |
|     (deploy + /health + ONE happy-path read); cross-service +        |
|     behavior matrices explicitly delegated to feature                |
| `briefs/scaffold/decision_recording_slim.md` (run-21 R2-1 | ~3.6 KB |
|                                + run-22 R3-C-2/4/5)                  |
|   ↑ run-21 R2-1 replaced legacy 14 KB `decision_recording.md`;       |
|     run-22 R3-C-2 added topic-uniqueness clarification, R3-C-5       |
|     separated topic (freeform) from kind (enum), R3-C-4 added a      |
|     citationGuide-populated worked example                           |
| `principles/dev-loop.md`                            | ~4.5 KB |
| `principles/mount-vs-container.md` (run-21 R2-4 +   |  ~4 KB  |
|                                     run-22 R2-RC-5)                  |
|   ↑ run-21 R2-4 stage-slot negative rule (`<host>stage` is a         |
|     deployed runtime, not a source mount); run-22 R2-RC-5 added      |
|     "edit-in-place during feature phase" rule (forbids               |
|     `zerops_deploy <host>dev`, mandates `zerops_dev_server restart`  |
|     for env-var changes). Atom reaches scaffold AND feature briefs.  |
| `principles/cross-service-urls.md` (run-20 C2 +     | ~10.5 KB |
|                              run-22 R2-RC-1 + R3-RC-3)               |
|   ↑ run-22 R2-RC-1 corrected example yamls to use generic            |
|     `setup: prod`/`dev` per `core.md:137`; R3-RC-3 added             |
|     `update-plan projectEnvVars` channel-sync teaching alongside     |
|     existing `zerops_env action=set` (both required; engine reads    |
|     Plan.ProjectEnvVars at tier emit, NOT zerops_env results)        |
| `principles/bare-yaml-prohibition.md` (run-20 C3 +  | ~1.6 KB |
|                                run-22 R2-RC-1 setup name)            |
| **Always-loaded subtotal**                          | **~31 KB** |

`principles/yaml-comment-style.md` was dropped at scaffold by run-21
R2-1 — it contradicts `bare-yaml-prohibition.md` (scaffold yaml is
bare; causal comments are authored at codebase-content phase). The
atom still loads at codebase-content brief.

### Atoms — conditional

| Atom                                                | Size    | Trigger                                                     |
|-----------------------------------------------------|--------:|-------------------------------------------------------------|
| `principles/init-commands-model.md`                 | ~2.8 KB | `cb.HasInitCommands` (run-21 R2-1: per-codebase, not plan-wide) |
| `briefs/scaffold/build_tool_host_allowlist.md`      |  ~1 KB  | `cb.Role == frontend && nodejs base`                        |
| `briefs/scaffold/spa_static_runtime.md` (run-20 C2 + run-22 R2-RC-1 setup name) | ~3.5 KB | `cb.Role == frontend && nodejs base` |

### Engine-derived sections

- Header (~0.2 KB)
- Role contract (~0.3 KB)
- Citation-guide map (~0.6 KB)
- Recipe-knowledge slug list (resolver-driven, ~0.5–2 KB)
- Tier-fact table (~1.5 KB; frontend role only)
- **Parent recipe excerpt** — two paths (run-22 R3-RC-0):
  - **Filesystem mount** path: when
    `parent.Codebases[cb.Hostname]` exists (resolver hit). README +
    zerops.yaml excerpts via `excerptREADME(pc.README, 1500)`.
  - **Embedded fallback** path (run-22 R3-RC-0 — closes the cascade
    root): when `parent==nil && parentSlugFor(slug)!=""` AND
    `internal/knowledge/recipes/<parent-slug>.md` exists in the
    `//go:embed all:recipes` corpus. Loads the full parent `.md`
    (truncated cap-friendly) as a "Parent recipe baseline (embedded)"
    section. Pre-fix the dogfood dev container had no
    `~/recipes/` mount → resolver returned ErrNoParent → agent fell
    into the "first-time framework" branch with no proven baseline
    for setup naming, project-secret posture, or comment style —
    cascade root for run-22's RC-1/2/3/4. Filesystem path wins when
    both paths can fire.

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

### Approximate brief size by codebase shape (post run-22 R3-RC-0)

| Codebase shape                     | ~size   |
|------------------------------------|--------:|
| api / worker (no init)             | ~26 KB  |
| api / worker (with seed)           | ~29 KB  |
| frontend nodejs (showcase, no parent)  | ~36 KB |
| frontend nodejs (showcase, embedded parent .md) | ~40–44 KB |

Run-22 R3-RC-0 added the embedded parent baseline block when the
filesystem mount is empty AND slug is `*-showcase`; pinned by
`TestScaffoldBrief_EmbedsParentMD_WhenParentAbsent_ShowcaseSlug`. The
`TestBrief_Scaffold_FrontendSPA_UnderTargetSize` soft target lifted
35 → 41 KB in run-22 R3 to absorb the new teaching (R3-RC-3
update-plan, R3-C-2/4/5 worked examples). Hard cap stays at 48 KB.

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
| `briefs/feature/feature_kinds.md`      | ~2.2 KB |
| `briefs/feature/decision_recording.md` | ~5.4 KB |
| `principles/mount-vs-container.md` (run-21 R2-4 + run-22 R2-RC-5) |  ~4 KB  |
|   ↑ run-22 R2-RC-5 added "edit-in-place during feature phase"       |
|     section. Forbids `zerops_deploy targetService=<host>dev` (code   |
|     is already live via SSHFS); forbids deploys to apply env-var     |
|     changes (use `zerops_dev_server action=restart` instead). One   |
|     legitimate cross-deploy per feature: targeting the stage slot    |
|     when in-place verification has passed. Closes the run-22         |
|     dev-redeploy thrash (5 unnecessary feature-phase dev redeploys). |
| **Always-loaded subtotal**             | **~12 KB** |

`principles/yaml-comment-style.md` was dropped at feature by run-21
R3-2 — same reasoning as scaffold (bare-yaml authoring contract;
comment-style teaching belongs in codebase-content).

`briefs/feature/content_extension.md` is **deprecated** (run-22
R2-RC-5) — has not been loaded by `BuildFeatureBrief` since run-16
§6.2. Header marker added so future maintainers don't edit-without-
effect; FIX_SPEC `R2-RC-5` deprecation rationale + active-atom map
in [`runs/22/CODEX_VERIFICATION.md` Tables A + B](runs/22/CODEX_VERIFICATION.md#L549).

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
**Cap:** 56 KB (run-22 R1+R2 bumped 48→52→56).

### Atoms — always loaded

| Atom                                                 |   Size  |
|------------------------------------------------------|--------:|
| `phase_entry/codebase-content.md`                    | ~2.8 KB |
| `briefs/codebase-content/synthesis_workflow.md`      | ~22 KB  |
|   ↑ run-21 P0-3 (golden excerpts inlined to close goldens-hunting),  |
|     R3-1 (disk-vs-fragment authority clarified — fragment is source  |
|     of truth, engine stitches it to disk before gates run),          |
|     cap-trim follow-up (excerpts trimmed to fit cap)                 |
| `briefs/scaffold/platform_principles.md` (cross-loaded; run-22 R1-RC-2) | ~5.6 KB |
| `principles/zerops-knowledge-attestation.md`         |  ~3 KB  |
| `principles/yaml-comment-style.md` (run-22 R1-RC-4)  | ~3.6 KB |
|   ↑ run-22 R1-RC-4 added Unicode box-drawing forbid alongside        |
|     ASCII variants in the anti-pattern list                          |
| **Always-loaded subtotal**                           | **~37 KB** |

### Atoms — conditional

| Atom                                                          | Size    | Trigger                                                                |
|---------------------------------------------------------------|--------:|------------------------------------------------------------------------|
| `principles/nats-shapes.md` (run-20 C1)                       | ~2.7 KB | `shouldLoadNATSShapes(plan, cb)` (run-21 R2-2): drop for frontends; load only when cb consumes a `nats@*` service |
| `principles/cross-service-urls.md` (run-20 C2 + run-22 R3-RC-3) | ~10.5 KB | `shouldLoadCrossServiceURLs(cb)` (run-21 R2-2): drop when `cb.ConsumesServices` is empty non-nil (codebase analyzed, no managed deps); R3-RC-3 added update-plan projectEnvVars channel-sync teaching |
| `briefs/codebase-content/worker_kb_supplements.md` (run-22 R2-WK-1+2 + followup F-5) | ~3 KB  | `plan.Tier == "showcase" && cb.IsWorker` — KB-content shape only after the F-5 split (code-shape MANDATORY moved to `briefs/feature/worker_subscription_shape.md`); names the validator gate by file path |

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

### Worker subscription gate (run-22 R2-WK-1+2)

`gateWorkerSubscription` ([validators_worker_subscription.go](../../internal/recipe/validators_worker_subscription.go))
runs at codebase-content complete-phase against showcase-tier worker
codebases. Regex-based source scan over `<SourceRoot>/src/**/*.ts`:

- **Refuses naked `nc.subscribe(SUBJECT)`** (no second-arg with
  `queue` option) — `worker-subscribe-missing-queue-option`. At
  tier 4-5 every NATS event is delivered to BOTH worker replicas
  → double-indexing in Meilisearch + double-LPUSH to Valkey marker
  list. KB teaches `{ queue: 'workers' }` fix; pre-fix run-22
  worker code shipped without it.
- **Warns on `unsubscribe()` shutdown** (in `OnModuleDestroy` /
  SIGTERM handler) — `worker-shutdown-uses-unsubscribe`. Drops
  in-flight events on rolling deploys. KB teaches
  `await sub.drain()` ordering; run-22 worker shipped
  `unsubscribe()`. Severity downgrade to `Notice` when `nc.drain()`
  co-occurs in same block (less-broken shape).

NATS-context heuristic (file imports `'nats'` / mentions
`NatsConnection` / `StringCodec` / etc.) avoids false-positives on
rxjs `Observable.subscribe` and Node `EventEmitter`. Pinned by
`TestWorkerSubscriptionGate_*` (9 unit tests including the run-22
verbatim shape pin in `TestGateWorkerSubscription_FlagsRun22ShapeExactly`).

### Approximate brief size

~40–55 KB per codebase. SPAs that consume nothing managed drop the
NATS + cross-service-URL atoms (~13 KB lighter); showcase worker
codebases that load every conditional sit just under the 56 KB cap
(worker variant pushed hardest because `worker_kb_supplements.md`
only loads here — its code-shape sibling lives at the feature brief
since run-22 followup F-5).

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
**Cap:** 56 KB (run-22 R1+R2 bumped 48→52→56; matches CodebaseContentBriefCap because shared atoms drive the same pressure).

### Atoms — always loaded

| Atom                                              | Size    |
|---------------------------------------------------|--------:|
| `phase_entry/env-content.md`                      | ~1.2 KB |
| `briefs/env-content/per_tier_authoring.md` (run-22 R2-RC-6) | ~9.8 KB |
|   ↑ run-22 R2-RC-6 distinguished "canonical-set dedup" (strip the    |
|     versioned service list from tiers 1-3) from "per-tier flavor"    |
|     (keep 1-2 lines per service block AT EVERY tier even when no     |
|     field changes from the previous tier). Closes the run-22 over-   |
|     strip where tiers 1/2/3 had ~6 indented `#` lines vs golden ~25  |
| `principles/zerops-knowledge-attestation.md`      |  ~3 KB  |
| `principles/yaml-comment-style.md` (run-22 R1-RC-4) | ~3.6 KB |
| **Always-loaded subtotal**                        | **~17.6 KB** |

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

~19–28 KB. Plans without a NATS broker drop ~2.7 KB; plans with NATS
keep the C1 fabrication-defense atom. Run-22 R2-RC-6 + R1-RC-4 added
~2.5 KB across the always-loaded set.

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

### Single-slot URL rewrite (run-22 R3-RC-3)

`writeProject` ([yaml_emitter.go:96-125](../../internal/recipe/yaml_emitter.go#L96))
applies `rewriteURLsForSingleSlot` to `plan.ProjectEnvVars[envKey(tier)]`
when the tier is single-slot (predicate: `!tier.RunsDevContainer`,
which is true for tiers 2-5):

- Drops keys prefixed `DEV_` (single-slot tiers have no separate
  dev runtime — DEV_API_URL etc. are dev-pair-only).
- Collapses slot-named hostnames in URL values: `apidev-`/`apistage-`
  → `api-`, `appdev-`/`appstage-` → `app-`, `workerdev-`/
  `workerstage-` → `worker-`.
- Preserves `${zeropsSubdomainHost}` literal so the end-user's
  click-deploy mints fresh subdomain values.

Pinned by `TestEmitDeliverableYAML_RewritesURLsForSingleSlotTiers`,
`TestEmitDeliverableYAML_KeepsDevPairURLsForTiers0And1`,
`TestEmitDeliverableYAML_PreservesAppSecretAlongsideURLConstants`.

Closes the second half of the run-22 RC-3 channel-sync ship-blocker:
agent records URL constants once via `update-plan projectEnvVars`
(taught at scaffold via `cross-service-urls.md`); engine reshapes
per-tier at finalize emit. End-user click-deploy gets the right
shape for their tier.

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
| `briefs/refinement/embedded_rubric.md` (run-22 R1-RC-7 + R3-C-1) | ~21 KB  |
|   ↑ run-22 R1-RC-7 added "Tier-promotion narrative (forbidden per   |
|     spec §108)" section with case-insensitive regex set —           |
|     `\bpromote\b.*\btier\b`, `\boutgrow\w*`, etc. — so refinement   |
|     has reason to flag run-22's tier-4-README "promote to tier 5"   |
|     leak. R3-C-1 added subdomain "rotate" overclaim guard.          |
| **Always-loaded subtotal**                           | **~115 KB** |

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
| 0 research                  | n/a                | main only             | ~6 KB                    |
| 1 provision                 | n/a                | main only             | ~4 KB                    |
| 2 scaffold                  | ~26–44 KB          | N codebases (1–3)     | ~26–132 KB               |
| 3 feature                   | ~13–22 KB          | 1 (cross-codebase)    | ~13–22 KB                |
| 4a codebase-content         | ~40–55 KB          | N codebases           | ~40–165 KB               |
| 4b claudemd-author          | ~3.5–4 KB          | N codebases           | ~3.5–12 KB               |
| 5 env-content               | ~19–28 KB          | 1                     | ~19–28 KB                |
| 6a finalize (sub-agent)     | ~10–13 KB          | 1                     | ~10–13 KB                |
| 6b stitch (engine)          | 0 KB               | engine only           | 0 KB                     |
| 7 refinement (optional)     | ~115–130 KB        | 1 (when triggered)    | ~115–130 KB              |

A typical 3-codebase showcase run dispatches ~10 sub-agents and burns
roughly 220–400 KB of brief context across the pipeline (excluding
refinement) — about 70% of which is the Phase 4a corpus, where
mechanics-rich teaching needs to land at every codebase.

Run-21 fix-pack net effect: scaffold dropped ~15 KB per codebase
(R2-1 slim), feature dropped ~3 KB (R3-2 slim), codebase-content
dropped ~10 KB on per-codebase-conditional atoms (R2-2 + R2-3),
env-content dropped ~3 KB on broker-less plans (R3-2). Aggregate
savings on a 3-codebase showcase: ~70 KB of dispatched-brief context.

Run-22 fix-pack net effect (cap pressure direction): scaffold +5–10 KB
per frontend codebase (R3-RC-0 embedded parent baseline +
R3-C-2/4/5 worked examples + R2-RC-1/R3-RC-3 cross-service-urls
extensions); feature +2 KB (R2-RC-5 mount-vs-container edit-in-place
section); codebase-content +1.5 KB (R1-RC-2 platform_principles
project-level shadow extension + R1-RC-4 yaml-comment-style Unicode
forbid + R2-WK-1+2 worker queue+drain mandatory, split across feature
+ codebase-content by run-22 followup F-5);
env-content +2.5 KB (R2-RC-6 per_tier_authoring canonical-set vs
flavor + R1-RC-4); refinement +1 KB (R1-RC-7 tier-promotion regex +
R3-C-1 subdomain-rotate guard). Net cap bumps:
CodebaseContentBriefCap + EnvContentBriefCap 48→52→56 KB; soft
target on frontend scaffold 35→41 KB. ScaffoldBriefCap +
FeatureBriefCap unchanged.
