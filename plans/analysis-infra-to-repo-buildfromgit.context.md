# Context: analysis-infra-to-repo-buildfromgit
**Last updated**: 2026-04-03
**Iterations**: 1
**Task type**: codebase-analysis

## Decision Log
| # | Decision | Evidence | Iteration | Rationale |
|---|----------|----------|-----------|-----------|
| D1 | No reverse workflow exists — confirmed as gap | Full codebase search, all 4 workflows forward-only | 1 | KB + primary-analyst both verified independently |
| D2 | Mode (HA/NON_HA) is available in API, just not surfaced in discover | verifier: types.go:30, zerops_mappers.go:98 | 1 | Trivial fix, not a platform gap |
| D3 | Recipe knowledge (30+ templates) is the best inference source | KB: knowledge/recipes/ covers all major frameworks | 1 | No new pattern library needed — reuse existing recipes |
| D4 | Recommended "export" workflow over extending bootstrap or recipe | Primary-analyst architecture analysis | 1 | Bootstrap = infra creation, recipe = forward-gen. Export is conceptually distinct. |
| D5 | buildFromGit is one-time, not persistent CI/CD | Verifier: docs import.mdx:399 | 1 | User needs separate CI/CD setup after initial deploy |
| D6 | **Zerops API HAS export endpoints** — ZCP just hasn't implemented them | Verifier: live API test, zerops-go SDK has `GetProjectExport`/`GetServiceStackExport` | 1 | Critical discovery — changes F1 from platform limitation to ZCP implementation gap |
| D7 | Export + Discover together cover ~90% of import.yaml | Export gives project config + skeleton; Discover fills scaling/ports/containers/mode | 1 | Combined approach is stronger than either alone |
| D8 | Inline `zeropsYaml` enables single-file IaC export | Adversarial: import.yaml supports `zeropsYaml` inline field | 1 | Simplifies flow — no separate zerops.yml file required for re-import |

## Rejected Alternatives
| # | Alternative | Evidence Against | Iteration | Why Rejected |
|---|------------|-----------------|-----------|-------------|
| RA1 | SSH-based source extraction as Phase 1 | Framework-dependent (~60% coverage), SSH complexity in stateless MCP, user can provide source | 1 | High effort, low reliability. Deferred to Phase 3. |
| RA2 | Extend bootstrap "adopt" for export | Conceptual mismatch — bootstrap creates infra, export extracts config | 1 | Muddled abstraction. Dedicated workflow is cleaner. |
| RA3 | New pattern library (`knowledge/patterns/`) | Existing recipe knowledge already encodes framework patterns | 1 | Redundant — reuse recipes instead |
| RA4 | Require GUI export as first step | API export endpoints exist — GUI export unnecessary | 1 | Superseded by D6: programmatic export via API is available |

## Resolved Concerns
| # | Concern | Evidence | Raised In | Resolved In | Resolution |
|---|---------|----------|-----------|-------------|-----------|
| RC1 | Mode field missing from discover | types.go:30 has it, discover.go omits it | 1 | 1 | Trivial fix — add field to ServiceInfo |
| RC2 | SubdomainEnabled missing | discover.go:37 already has it | 1 | 1 | Not actually missing |

## Open Questions (Unverified)
| # | Question | Context |
|---|---------|---------|
| OQ1 | Does GUI export include `zeropsYaml` inline config if original import used it? | Untestable without GUI access |
| OQ2 | Does Zerops API store the zerops.yml used for last successful build? (`appVersion` build config) | Not in current Client interface; may exist as undocumented API |
| OQ3 | Can build artifacts be downloaded via API? (GUI has "download build artifact") | Not in Client interface; may be undocumented |
| OQ4 | For compiled-language services, is any source retained in the container? | SSH timeout prevented verification |

## Confidence Map
| Section/Area | Confidence | Evidence Basis |
|-------------|-----------|---------------|
| No reverse workflow exists | HIGH (VERIFIED) | Full codebase search by 3 agents |
| Discover coverage ~75% | HIGH (VERIFIED) | Live platform test + code review |
| Recipe knowledge as inference source | HIGH (VERIFIED) | 30+ recipe files confirmed |
| Architecture recommendation | MEDIUM (LOGICAL) | Combines verified capabilities, not tested end-to-end |
| Source code availability | LOW (PARTIAL) | Framework-dependent reasoning, SSH not verified |
| zerops.yml generation coverage 80-90% | MEDIUM (LOGICAL) | Recipe template quality assumed, not tested |
