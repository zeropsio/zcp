# Context: plan-local-dev-flow
**Last updated**: 2026-03-26
**Iterations**: 3
**Task type**: document-review

## Decision Log
| # | Decision | Evidence | Iteration | Rationale |
|---|----------|----------|-----------|-----------|
| D1 | Conditional registration (Varianta C) — same tool name, different schema per env | server.go:108, tools/deploy.go:18-23 | 2 | sourceService meaningless in local; workingDir has different semantics; no phantom params in schema |
| D2 | ServiceMeta.hostname = appstage in local mode | router.go:76 filterStaleMetas, bootstrap_outputs.go:23 | 2 | hostname must exist on Zerops or meta is filtered out; appstage exists, appdev doesn't |
| D3 | Add Environment field to ServiceMeta | instructions_orientation.go:112, workflow_checks_deploy.go:173 | 2 | Consumers need to branch on env; meta self-describes context, no fragile inference |
| D4 | Bootstrap guidance: new sections in bootstrap.md (generate-local, deploy-local) | bootstrap_guidance.go:37-63, existing mode pattern | 2 | Follows existing mode-specific section pattern; deploy-local replaces deploy entirely |
| D5 | buildDeployGuide needs env parameter | deploy_guidance.go:76, deploy.go:317 | 2 | Currently generates SSH/mount content unconditionally |
| D6 | zcli push uses positional arg (hostname) not --serviceId flag | KB + verifier: actual flag is --service-id kebab-case | 1 | Simpler, avoids camelCase vs kebab-case issue |
| D7 | S3 without VPN: UNVERIFIED | TLS failed at generic endpoint | 1 | Per-service apiUrl may differ; needs E2E before claiming works |
| D8 | Separate strategy lookup key from meta hostname in writeBootstrapOutputs | bootstrap_outputs.go:23-27, Strategies map keyed by DevHostname | 3 | Strategy stored under DevHostname by agent; meta hostname changes to StageHostname in local mode → lookup must use DevHostname |
| D9 | BuildTransitionMessage needs env parameter for display names | bootstrap_guide_assembly.go:79-103 | 3 | Uses DevHostname which doesn't exist in local mode; display should show StageHostname |

## Rejected Alternatives
| # | Alternative | Evidence Against | Iteration | Why Rejected |
|---|------------|-----------------|-----------|--------------|
| A1 | Single tool with internal routing (Varianta A) | tools/deploy.go:18-23 DeployInput schema | 2 | Shows sourceService in local mode where it's meaningless; workingDir has different defaults |
| A2 | Two separate tool names (Varianta B) | guidance system says "zerops_deploy" everywhere | 2 | Agent must know which env → more complexity; guidance must branch on tool name |
| A3 | Remove dev targets from plan.Targets in local mode | Would break plan format invariant (D11) | 1 | Simpler to branch in writeBootstrapOutputs |
| A4 | Infer environment from absence of SSHFS mount | fragile, no mount ≠ local | 2 | Explicit Environment field in ServiceMeta is cleaner |
| A5 | Reuse single hostname variable for both strategy key and meta hostname | bootstrap_outputs.go:23-27 ordering | 3 | Strategy lookup happens after hostname override → loses strategy |

## Resolved Concerns
| # | Concern | Evidence | Raised In | Resolved In | Resolution |
|---|---------|----------|-----------|-------------|-----------|
| C1 | Deploy tool never registers in local mode | server.go:108 | 1 | 2 | Conditional registration: RegisterDeploySSH vs RegisterDeployLocal |
| C2 | ValidateZeropsYml reads from SSHFS mount path | deploy.go:140 | 1 | 2 | deployLocal() calls ValidateZeropsYml(workingDir, target) with local path |
| C3 | ServiceMeta writes wrong hostname | bootstrap_outputs.go:23 | 1 | 2 | Environment branch in writeBootstrapOutputs + Environment field in ServiceMeta |
| C4 | Strategy lookup key mismatch after hostname override | bootstrap_outputs.go:23-27 | 3 | 3 | Separate devHostname (strategy key) from metaHostname (ServiceMeta) |
| C5 | BuildTransitionMessage shows non-existent service in local mode | bootstrap_guide_assembly.go:79-103 | 3 | 3 | Add env parameter, display StageHostname in local mode |
| C6 | buildGuide `_ Environment` dead parameter | bootstrap_guide_assembly.go:13 | 3 | 3 | Plan corrected: activate existing parameter, not add new one |
| M1 | zcli --serviceId camelCase wrong | KB + verifier | 1 | 2 | Use positional arg: zcli push <hostname> |
| M5 | Guidance has no local branch | deploy_guidance.go:56 | 1 | 2 | Full guidance redesign: both bootstrap and deploy systems get env parameter |

## Open Questions (Unverified)
| # | Question | Status |
|---|----------|--------|
| Q1 | Is S3 apiUrl accessible without VPN from local machine? | Needs E2E with real object-storage |
| Q2 | VPN sudo: every invocation or first-time only? | Docs say "may be required" for daemon install |
| Q3 | Does `wg-quick --help` return exit 0? (CanAutoVPN detection) | Needs testing; may need `sudo -n true` instead |

## Confidence Map
| Section/Area | Confidence | Evidence Basis |
|-------------|------------|---------------|
| Conditional registration design | HIGH | Analyzed all 3 variants, traced schema implications |
| ServiceMeta hostname=appstage | HIGH | Traced through filterStaleMetas, orientation, BuildDeployTargets |
| Environment field in ServiceMeta | HIGH | Traced all 8 consumers that need to branch |
| Strategy key separation (devHostname vs metaHostname) | HIGH | Traced Strategies map population + lookup chain end-to-end |
| BuildTransitionMessage env-awareness | HIGH | Traced DevHostname usage through display + strategy lookup |
| Bootstrap guidance sections | HIGH | Follows existing mode-specific pattern in bootstrap.md |
| buildGuide env parameter (dead code activation) | HIGH | Verified _ Environment at bootstrap_guide_assembly.go:13 |
| Deploy guidance env parameter | HIGH | Traced full call chain: DeployState.buildGuide → buildDeployGuide |
| zcli positional arg | HIGH | KB + verifier confirmed |
| S3 without VPN | LOW | Docs say yes, TLS test says maybe not |
| CanAutoVPN wg-quick detection | LOW | wg-quick --help validity unverified |
