# Z3 UI backlog — deferred from the demo surface sweep

Only items too large or too semantic for the current visual pass live here.

| Priority | Item | Minimal current treatment |
|---|---|---|
| P0 | A stale hosted-demo environment can report `environment credential is invalid` until `/zerops` refreshes the project feed | Keep the error honest and compact; make credential recovery durable instead of depending on a picker refresh. |
| P1 | Real Zerops tag-group and production-lane identity in the sidebar | The current client contract has only project id/name/status. Ship the truthful project → environment → thread tree now; later carry explicit tag/role metadata through connection onboarding instead of inferring it from names. |
| P1 | Complete multi-repository Git lifecycle | Saved checkpoint diffs now read every mounted repository. Branch/commit/push/PR actions still need an explicit per-repository contract and partial-failure UX; do not run one implicit Git action against `/var/www`. |
| P1 | Per-ZCP agent credential management | Authorization now uses the ZCP's real server snapshot and attention states. Add server-backed revoke/re-authorize/expiry controls only after the credential lifecycle contract exists; never recreate this as global Settings provider instances. |
| P1 | Multi-repository restore completion receipt | The confirmation now truthfully names tracked/staged changes, but the command still needs a final per-repository success/failure result before UI can claim restore completion. |
| P2 | Landing and `/zerops` route consolidation | Polish the authenticated picker first; reconcile the entry route after the demo surfaces stabilize. |
| P2 | Native mobile Zerops surfaces | Narrow web is covered by this sweep; native mobile remains manifest-planned. |
| P2 | Web production bundle remains large (`index` about 3.86 MB / 1.18 MB gzip) | Keep this pass visual; profile route-level/code-editor splitting before treating first-load speed as demo-ready. |
| P2 | Data Console, production line, integrations and new capability IA | Do not tease unusable mutations; revisit from validated journeys. |
| P3 | Internal `@t3tools`, `T3CODE_` and legacy storage naming | No demo payoff; leave wire and migration identities intact. |

Resolved/scheduled in `z3-sidebar-demo-2026-09-01`: useful sidebar hierarchy, default-open remembered
Zerops panel, coherent blocking question/approval UI, mobile overlay ordering, a scrollable provider
wizard, and recoverable file loading. The old milestone-fold row was removed:
all current fold/group branches already preserve Zerops milestone cards and are covered by focused
tests, so it was stale rather than deferred work.

Resolved in `z3-ui-polish-2026-09-01`: untouched workspace threads are compact shortcuts, active
thread cards use their height for up to two title lines, long service identity wraps inside the
Zerops panel, and terminal drawer resize is a keyboard-accessible ARIA separator.

Resolved in `z3-ui-final-polish-2026-09-01`: every workspace now owns a compact new-thread action,
available Zerops project identity is shared across the active web chrome, connected composer copy
is task-first, agent sign-in wraps with a persistent device code, provider marks are visually quiet,
and the Zerops panel uses a readable centered content width. The existing P0 credential-recovery
item remains open and was reproduced during the hosted verification.
