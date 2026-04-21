# diagnostic-cadence

When a signal is ambiguous — a command appears to hang, output looks wrong, a tool returns an unexpected value, an SSH session seems sluggish — probe at a bounded cadence. Each probe tests one named hypothesis; each batch is followed by progress on the feature before the next batch fires.

## Permitted probe types

Five shapes cover every ambiguity you will hit. Each slot fires one probe from one type, then you return to Read / Edit / Write work.

1. **Container log diff** — tail the relevant container's log since the last known-good moment. One call via `mcp__zerops__zerops_logs` per probe slot, for one hostname.
2. **App log tail** — tail the app-level process log (the dev-server's stdout for the failing route, the worker's pino output for the failing subject). One call via `mcp__zerops__zerops_dev_server` (status / logs) or `ssh {hostname} "tail -n 80 <logfile>"`.
3. **Round-trip probe** — one `ssh {hostname} "curl -sS ..."` (or the gRPC / NATS equivalent) against the suspected broken hop, returning status + content-type + first 400 bytes of body. One probe per hop.
4. **Manifest / contract check** — read one field from `ZCP_CONTENT_MANIFEST.json`, the SymbolContract, or the affected codebase's `package.json` / lockfile to confirm a name or a version.
5. **Targeted file grep** — one `Grep` over one codebase for the one symbol, route, or import you believe is miswired.

## Cadence rule

- Probes come in batches of at most three, each batch testing a distinct hypothesis.
- Between batches, do a Read, Edit, or Write informed by what the batch showed — you are not allowed to fire a second batch until progress against the hypothesis has been written down.
- Cap the rate at five bash shapes per minute (across probe + non-probe commands) so the concurrency queue never saturates.
- If three batches do not resolve the ambiguity, STOP probing. Return to the caller with the batches you ran, the hypotheses you tested, and the status of each. The caller has broader context — other sub-agents, workflow history, prior attempts — and will either dispatch a specific recovery or declare the state blocking.
