# Context: analysis-delete-self-guard
**Last updated**: 2026-04-03
**Iterations**: 1
**Task type**: implementation-planning

## Decision Log
| # | Decision | Evidence | Iteration | Rationale |
|---|----------|----------|-----------|-----------|
| 1 | Guard at tools layer, not ops | deploy_ssh.go:29 pattern, ops/delete.go is thin wrapper | 1 | Consistency with rtInfo usage pattern; guard is tool-level safety, not business logic |
| 2 | Guard runs before pre-delete unmount | delete.go:34-40, adversarial MF1 | 1 | Avoid unnecessary unmount when delete will be rejected |
| 3 | Use strings.EqualFold for hostname comparison | KB recommendation, DNS hostname format | 1 | Defensive against case mismatch in user input |
| 4 | New error code ErrSelfDeleteBlocked | errors.go:10-50 scan | 1 | No existing code covers this case |

## Rejected Alternatives
| # | Alternative | Evidence Against | Iteration | Why Rejected |
|---|------------|-----------------|-----------|--------------|
| 1 | Guard at ops layer (ops.Delete) | ops/delete.go is 6 lines of logic, thin API wrapper; deploy_ssh uses tools-layer rtInfo | 1 | Would couple pure API wrapper to container detection; inconsistent with established pattern |
| 2 | Platform-level protection | Verifier confirmed: no API protection exists, no "protected" flag | 1 | Not available — Zerops API has no self-delete concept |

## Resolved Concerns
| # | Concern | Evidence | Raised In | Resolved In | Resolution |
|---|---------|----------|-----------|-------------|-----------|
| 1 | Can user bypass via ServiceID? | DeleteInput only has ServiceHostname field | 1 | 1 | Input struct prevents ID bypass |
| 2 | mount.go doesn't guard hostnames at tools layer | mount has no self-destruction risk | 1 | 1 | Different risk profile — not comparable |

## Open Questions (Unverified)
- Exact container shutdown timing when service is deleted (requires sacrificing a test service)
- Whether Zerops hostnames can contain non-ASCII characters (assumed DNS-format only)

## Confidence Map
| Section/Area | Confidence | Evidence Basis |
|-------------|-----------|---------------|
| Guard placement (tools layer) | VERIFIED | deploy_ssh.go pattern, codebase grep |
| rtInfo wiring | VERIFIED | server.go, existing signatures |
| Error code addition | VERIFIED | errors.go scan |
| Hostname comparison | LOGICAL | DNS format assumption + EqualFold safety |
| Test plan coverage | VERIFIED | Existing test patterns in delete_test.go |
| Pre-delete unmount ordering | VERIFIED | delete.go:34-40 code read |
