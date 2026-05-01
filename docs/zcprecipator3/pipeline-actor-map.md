```
                              MAIN AGENT
   ┌────────────────────────────────────────────────────────────────┐
   │  Reads ONLY:                                                    │
   │    • current phase entry (loadPhaseEntry per action)            │
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

 2 SCAFFOLD   3 ×       brief composed from:                code (src/**)
              scaffold    phase_entry/scaffold.md           zerops.yaml (SHOULD be bare)
              sub-       briefs/scaffold/decision_recording dev process running via MCP
              agents     briefs/scaffold/platform_principles  zerops_dev_server
                         briefs/scaffold/preship_contract   facts (via record-fact)
                         briefs/scaffold/fact_recording
                         briefs/scaffold/build_tool_host_allowlist
                         principles/dev-loop.md
                         principles/yaml-comment-style.md
                         principles/init-commands-model.md
                         principles/mount-vs-container.md
                         atom corpus pulls (per type)
                         (laravel goldens cited as references)
                         NOT INCLUDED:
                           briefs/scaffold/content_authoring.md (orphaned)
                           workflows/recipe.md (zcprecipator2-era, ignored)

 3 FEATURE    1 feature  brief composed from:               extended code
              sub-agent    phase_entry/feature.md           feature facts
                           briefs/feature/feature_kinds     browser walks
                           briefs/feature/showcase_scenario
                           briefs/feature/decision_recording
                           briefs/feature/content_extension

 4 CODEBASE-  3 ×        brief composed from:               fragments via record-fragment:
   CONTENT    codebase-    phase_entry/codebase-content.md     codebase/<h>/intro
              content      briefs/codebase-content/intro       codebase/<h>/integration-
              sub-         briefs/codebase-content/synthesis_   guide/<n>
              agents       workflow                          codebase/<h>/knowledge-base
                          briefs/codebase-content/showcase_  codebase/<h>/zerops-yaml-
                          tier_supplements                     comments/<block>
                          briefs/codebase-content/parent_      (FACT-DRIVEN — only blocks
                          recipe_dedup                         with field_rationale facts
                          principles/yaml-comment-style.md     get a slot)
                          + filtered facts (codebase scope,
                            field_rationale only)

              3 × claude- brief composed from:               fragment via record-fragment:
              md-author    phase_entry/claudemd-author.md     codebase/<h>/claude-md
              sub-agents   briefs/claudemd-author/intro
                          briefs/claudemd-author/init_voice
                          briefs/claudemd-author/zerops_free_
                          prohibition

 5 ENV-       1 env-      brief composed from:               fragments:
   CONTENT    content      phase_entry/env-content.md          env/N/intro × 6
              sub-agent    briefs/env-content/intro            env/N/import-comments/
                          briefs/env-content/per_tier_         <svc>
                          authoring                          (NATS atom NOT wired here —
                                                              source of T0+T5 JetStream
                                                              fabrication)

 6 FINALIZE   main +     phase_entry/finalize.md             via stitch-content action:
   (stitch)   engine     briefs/finalize/intro                 AssembleRoot/Env/Codebase
              code       briefs/finalize/anti_patterns        READMEs
                         briefs/finalize/validator_tripwires AssembleCodebaseClaudeMD
                                                            EmitDeliverableYAML × 6
                                                            WriteCodebaseYAMLWithComments
                                                            (← double-inject site)

 7 REFINEMENT 1          briefs/refinement/synthesis_         record-fragment mode=
   (optional) refinement  workflow                            replace on quality-gap
              sub-agent  briefs/refinement/embedded_rubric    surfaces
                         briefs/refinement/refinement_
                         thresholds
                         briefs/refinement/reference_*.md
                         (kb_shapes, ig_one_mechanism,
                         citations, voice_patterns,
                         yaml_comments, trade_offs)
                         + every published surface
```
