# Local-mode develop-guidance iterate gating (verify + maybe gate aggressively)

**Surfaced**: 2026-06-03, during P0c develop-guidance de-bloat (round 1a). Container
flow-eval validated gating learned-once platform-reference atoms to
`envelopeDeployStates:[never-deployed]` (drops them on iterate, pointer-recoverable) with
zero regression across two neutral scenarios (greenfield-node-postgres-dev-stage,
develop-loop-after-bootstrap). The **local-mode** equivalent was left conservative because
it can't be auto-verified in this session.

**Why deferred**: verifying local mode needs `flow-eval-local`, which runs `make install`
(swaps `/usr/local/bin/zcp` — prohibited without Karel's explicit ask, per CLAUDE.local.md)
and requires VPN + `ZCP_API_KEY`. Local mode is also the fiddliest develop surface, so
removing reference from its iterate response without a behavioral check is exactly the
"don't break what works unverified" trap. Round 1a therefore **kept** the local platform
atom on iterate: `develop-platform-rules-local` was degated (reverted the
`envelopeDeployStates:[never-deployed]` I'd added) but **kept** `multiService:aggregate`
(the real dup-bug fix — it rendered 2× on standard pairs and was the dominant contributor
to the one MCP-cap breach). Net: the dup fix shipped; the local iterate-gating did not.

**Trigger to promote**: (a) a `flow-eval-local` pass is being run anyway (operator OKs
`make install` + VPN), OR (b) local-mode iterate bloat becomes a measured problem, OR
(c) the round-2 pointer-render mechanism lands (then local reference can pointer-render
uniformly with container, sidestepping the gate-off-iterate question entirely).

## Sketch — candidate local atoms to gate to never-deployed (verify each)
Same principle as the container atoms shipped in round 1a: learned-once platform mechanics
→ first-deploy call only, pointer-recoverable on iterate. Candidates + current state:

| Atom | bytes | iterate-relevant? | gate candidate? |
|---|---|---|---|
| `develop-platform-rules-local` | 2412 | mixed (VPN/.env-bridge/no-mount = learned-once; bg-dev-server warning = iterate) | maybe, IF the bg-dev-server warning is fully covered elsewhere |
| `develop-local-env-channels` | 2323 | the 3-channel `.env` model — mostly setup/first-deploy | strong candidate |
| `develop-local-env-troubleshoot` | 1356 | error-recovery reference | strong candidate (pointer) |
| `develop-dynamic-runtime-start-local` | 1035 | **NO — keep**: carries the dominant local failure warning ("foreground `npm run dev` blocks the turn → use the agent's background-task primitive") + the per-iterate dev-server restart loop | keep on iterate |

**Key dependency to re-confirm before gating `platform-rules-local`**: the dominant
local-mode failure warning (foreground dev command blocks the turn) currently lives in
BOTH `develop-platform-rules-local` AND `develop-dynamic-runtime-start-local`. The latter
is ungated (fires on local iterate), so the warning survives even if platform-rules-local
is gated off iterate — but verify that's still true at promote time (dedup in P0c round 1b
may move it).

## Risks
- Removing local platform reference from iterate without a behavioral check could strand a
  local-iterating agent (VPN down, `.env` stale, mount confusion) with no inline recovery.
  Local mode has the thinnest real-world coverage of all develop surfaces.
- The `flow-eval-local` harness itself has known macOS-keychain HOME-isolation friction (see
  `local-eval-home-isolation-macos-keychain.md`) — factor that into the verification cost.

## Refs
- Round-1a commit on `feat/p0c-develop-guidance-trim` (container gating + the
  `platform-rules-local` dup fix + local-gating revert).
- `plans/bootstrap-restore-four-goals-2026-06-02.md` § "P0c — Develop-guidance de-bloat"
  (ground-truth capture, LLM-judge verdicts, dedup ownership, execution rounds).
- Sibling backlog: `steady-state-env-atom-gating.md`, `develop-response-atom-proliferation.md`
  (overlapping atom-gating concerns — consolidate if all three promote together).
- Local-mode harness: `eval/behavioral/scenarios-local/`, `make flow-eval-local ID=<id>`.
