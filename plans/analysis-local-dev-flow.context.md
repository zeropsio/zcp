# Context: analysis-local-dev-flow
**Last updated**: 2026-03-26
**Iterations**: 1
**Task type**: implementation-planning

## Decision Log
| # | Decision | Evidence | Iteration | Rationale |
|---|----------|----------|-----------|-----------|
| D1 | Local mode should NOT create dev services on Zerops by default | User's machine IS the dev server; dev service runs zsc noop = wasteful | 1 | Eliminates unnecessary infrastructure, aligns with "local = dev" model |
| D2 | Use zcli push directly (exec.Command) for local deploy | zcli push works from any machine with --service-id flag | 1 | Simplest path, reuses existing zcli infrastructure |
| D3 | Env var bridge via zerops_discover + .env generation | zcli project env --export exists but not integrated; zerops_discover already returns values | 1 | Reuses existing MCP tool, adds .env file output |
| D4 | Route deployLocal() via existing sshDeployer nil check | sshDeployer is nil when !InContainer; existing guard can route instead of error | 1 | Minimal code change, reuses existing architecture |
| D5 | ${hostname_varName} works in zerops.yml regardless of push source | Zerops resolves at container runtime, not push-time | 1 | No change needed for deploy references |

## Rejected Alternatives
| # | Alternative | Evidence Against | Iteration | Why Rejected |
|---|------------|-----------------|-----------|-------------|
| R1 | Separate zerops_deploy_local MCP tool | Doubles tool surface, user confusion | 1 | Single tool with env-aware behavior is cleaner |
| R2 | Keep dev+stage in local mode | Dev service serves no purpose when user runs locally | 1 | Wasteful, confusing topology |
| R3 | Automate VPN management | VPN requires admin privileges, user control | 1 | Guidance-only approach is safer |
| R4 | Mount SSHFS in local mode | Contradicts CLAUDE.local.md rule; local filesystem is native | 1 | Fundamentally wrong for local mode |

## Resolved Concerns
| # | Concern | Evidence | Raised In | Resolved In | Resolution |
|---|---------|----------|-----------|-------------|-----------|
| RC1 | ${hostname_varName} breaks in local→stage push | arch_env_var_two_tier.md: resolved at container runtime | 1 | 1 | Adversarial CH4 rejected — references work regardless of push source |
| RC2 | Mount tool crashes in local mode | tools/mount.go nil-guard already prevents it | 1 | 1 | Existing architecture handles this |

## Open Questions (Unverified)
- Does `zcli project env --service X --export` resolve `${hostname_varName}` refs or output them as literals?
- Does plain hostname (without `.zerops`) resolve over VPN on macOS?
- How does object storage S3 URL work over VPN (uses `storage.app.zerops.io`, not internal hostname)?
- Should ZCP eventually manage local dev server lifecycle (Phase 2+)?

## Confidence Map
| Section/Area | Confidence | Evidence Basis |
|-------------|------------|---------------|
| Deploy redesign (deployLocal) | HIGH | Code structure verified, routing path clear |
| Env var bridge | HIGH | zcli project env exists, zerops_discover returns values |
| Guidance branching | HIGH | Pattern exists at deploy_guidance.go:56-63 |
| Local mode topology (no dev service) | MEDIUM | Logical but design decision, needs user validation |
| VPN hostname format | MEDIUM | Docs contradict, .zerops suffix is safe bet |
| Verification localhost | MEDIUM | Logical but port parsing needs design |
| zcli exec.Command invocation | MEDIUM | Standard Go pattern but error handling needs careful design |
