```
                              MAIN AGENT
   ┌────────────────────────────────────────────────────────────────┐
   │  Reads ONLY:                                                    │
   │    • current phase entry (loadPhaseEntry per action;            │
   │      research/provision/scaffold/feature/finalize)              │
   │    • briefs returned by build-brief / build-subagent-prompt     │
   │  Drives every phase via zerops_recipe action=...                │
   │  Dispatches sub-agents in parallel via Agent tool, passing each │
   │  the brief returned by build-subagent-prompt                    │
   └────────────────────────────────────────────────────────────────┘
                                  │
                                  ▼

phase ─────── actor ─── reads (on the wire) ─────────────── writes ─────────────────────

 0 RESEARCH   main      phase_entry/research.md             plan.json (via update-plan)
              agent     + parent recipe (if any)
                        + zerops_knowledge guide pulls

 1 PROVISION  main      phase_entry/provision.md            14 services live (via
              agent                                         zerops_import); project envs
                                                            (via zerops_env)

 2 SCAFFOLD   N ×       brief composed (BuildScaffoldBrief- code (src/**)
              scaffold  WithResolver) from:                 zerops.yaml (SHOULD be bare)
              sub-       briefs/scaffold/platform_principles dev process running via MCP
              agents     briefs/scaffold/preship_contract     zerops_dev_server
                         briefs/scaffold/fact_recording     facts (via record-fact)
                         briefs/scaffold/decision_recording
                         principles/dev-loop.md
                         principles/mount-vs-container.md
                         principles/yaml-comment-style.md
                         principles/cross-service-urls.md     (run-20 C2)
                         principles/bare-yaml-prohibition.md  (run-20 C3)
                         + citation-guide list (from CitationMap)
                         + recipe-knowledge slug list (resolver-driven)
                         CONDITIONAL atoms:
                           principles/init-commands-model.md
                             (when anyCodebaseHasInitCommands)
                           briefs/scaffold/build_tool_host_allowlist.md
                             (frontend role + nodejs base)
                           briefs/scaffold/spa_static_runtime.md
                             (frontend role + nodejs base; run-20 C2 layer 3)
                         + tier-fact table (frontend role only)
                         + parent recipe excerpt (only when
                           parent.Codebases[cb.Hostname] exists —
                           parent must carry an entry for THIS codebase
                           hostname; mere parent presence isn't enough)
                         NOT INCLUDED:
                           briefs/scaffold/content_authoring.md (orphaned;
                             retired in run-16 §6.2 — never referenced
                             from the composer)
                           internal/content/workflows/recipe.md (lives in
                             a different content package — not a recipe-
                             brief atom; zcprecipator2-era, ignored)

 3 FEATURE    1 feature  brief composed (BuildFeatureBrief)  extended code
              sub-agent  from:                               feature facts
                          briefs/feature/feature_kinds       browser walks
                          briefs/feature/decision_recording
                          principles/mount-vs-container.md
                          principles/yaml-comment-style.md
                          + symbol table (codebases + services)
                         CONDITIONAL atoms:
                           principles/init-commands-model.md
                             (when planDeclaresSeed)
                           briefs/feature/showcase_scenario.md
                             (showcase tier only)
                         NOT INCLUDED:
                           briefs/feature/content_extension.md (orphaned;
                             retired in run-16 §6.2 — file present but
                             never read by the composer)

 4 CODEBASE-  N ×        brief composed (BuildCodebaseContent fragments via record-fragment:
   CONTENT    codebase-  Brief) from:                          codebase/<h>/intro
              content     phase_entry/codebase-content.md      codebase/<h>/integration-
              sub-        briefs/codebase-content/synthesis_     guide/<n>
              agents      workflow                            codebase/<h>/knowledge-base
                          briefs/scaffold/platform_principles codebase/<h>/zerops-yaml-
                          principles/nats-shapes.md             comments/<block>
                          principles/cross-service-urls.md      (zerops-yaml-comments
                          principles/zerops-knowledge-           is FACT-DRIVEN — one
                            attestation.md                       slot per field_rationale
                          principles/yaml-comment-style.md       fact, per synthesis_-
                          + citation-guide list                  workflow.md atom rules)
                          + filtered facts (FilterByCodebase
                            then drop EngineEmitted=true; the
                            kind mix is whatever the run
                            recorded — porter_change,
                            field_rationale, platform-trap)
                          + on-demand pointers (zerops.yaml,
                            src/**, parent SourceRoot if any)
                         CONDITIONAL atoms:
                           briefs/codebase-content/showcase_tier_-
                             supplements.md (showcase tier + worker)
                         NOT INCLUDED:
                           briefs/codebase-content/intro.md (orphaned)
                           briefs/codebase-content/parent_recipe_dedup.md
                             (orphaned; parent dedup teaching now lives
                             in synthesis_workflow.md + on-demand pointer)

              N × claude- brief composed (BuildClaudeMDBrief) fragment via record-fragment:
              md-author   from (Zerops-free by construction):    codebase/<h>/claude-md
              sub-agents   phase_entry/claudemd-author.md
                           briefs/claudemd-author/zerops_free_-
                             prohibition.md
                           + on-demand pointers (package.json /
                             composer.json / src/** / app/**;
                             zerops.yaml deliberately excluded)
                          NOT INCLUDED:
                            briefs/claudemd-author/intro.md (orphaned)
                            briefs/claudemd-author/init_voice.md (orphaned)

 5 ENV-       1 env-      brief composed (BuildEnvContentBrief) fragments:
   CONTENT    content     from:                                  env/N/intro × 6
              sub-agent    phase_entry/env-content.md            env/N/import-comments/
                           briefs/env-content/per_tier_authoring   <svc>
                           principles/nats-shapes.md  (run-20 C1
                             — wired here precisely to close run-19
                             T0+T5 JetStream fabrication)
                           principles/zerops-knowledge-attestation.md
                           principles/yaml-comment-style.md
                           + per-tier capability matrix (Tiers())
                           + cross-tier deltas (tiers.go::Diff)
                           + engine-emitted tier_decision facts
                           + cross-codebase contract facts
                           + plan snapshot (codebases + services)
                           + parent pointer (when parent set)
                          NOT INCLUDED:
                            briefs/env-content/intro.md (orphaned)

 6 FINALIZE   1 finalize  brief composed (BuildFinalizeBrief)  fragments via record-fragment:
              sub-agent   from:                                  root/intro
              (authors     briefs/finalize/intro.md               env/N/intro × 6
              fragments)   briefs/finalize/validator_tripwires    env/N/import-comments/
                           briefs/finalize/anti_patterns            project × 6
                           + tier map (Tiers())                   env/N/import-comments/
                           + tier-fact table (BuildTierFactTable)   <host> per codebase &
                           + audience paths (per-codebase            managed service × 6
                             SourceRoot)                          (formatFinalizeFragmentList
                           + fragment list (formatFinalize-        + finalizeFragmentMath drive
                             FragmentList)                         the exact authoring set;
                           + fragment-count math                   the math line is the
                             (finalizeFragmentMath)                ex-wrapper drift fix from
                           + symbol table (codebases + services)   run-10 S-1)

              ▼ then sub-agent calls stitch-content (engine action):
              engine     no atom reads —                       AssembleRoot/Env/Codebase
              code       reads recorded fragments + plan       READMEs
                                                               AssembleCodebaseClaudeMD
                                                               EmitDeliverableYAML × 6
                                                               WriteCodebaseYAMLWithComments
                                                               (← double-inject site)

 7 REFINEMENT 1          brief composed (BuildRefinementBrief) record-fragment mode=
   (optional) refinement  from:                                replace on quality-gap
              sub-agent   phase_entry/refinement.md            surfaces
                          briefs/refinement/synthesis_workflow
                          briefs/refinement/reference_kb_shapes
                          briefs/refinement/reference_ig_one_-
                            mechanism
                          briefs/refinement/reference_voice_-
                            patterns
                          briefs/refinement/reference_yaml_-
                            comments
                          briefs/refinement/reference_citations
                          briefs/refinement/reference_trade_offs
                          briefs/refinement/refinement_thresholds
                          briefs/refinement/embedded_rubric
                          + on-disk pointers to every stitched
                            surface (root + tier × 6 + per-
                            codebase README/yaml/CLAUDE.md)
                          + run-wide facts log
```
