# G5 — L5 live smoke test results (cycle 3 binding)

Date: 2026-04-27 (cycle-3 Phase 5 binding re-run per plan §5 Phase 5 step 4)
Binary: `zcp dev (babdc79f, 2026-04-27T13:50:43Z)` cross-compiled via `make linux-amd VERSION=dev` from post-Phase-4 HEAD; SHA-256 `dc5ff90f07748c36b101edf4139ee1bb5436fbfa479ac9e38ff2398eb14ee9f3`. Dev-tagged to bypass auto-update during the smoke.
Target: eval-zcp project, container `zcp` per CLAUDE.local.md authorization, patched binary at `/home/zerops/.local/bin/zcp-final`.

**Status: BINDING ✅ GREEN**.

## Procedure

Per plan §5 Phase 5 step 4 — re-run G5 live smoke on the post-Phase-4 corpus. Cycle-2 Phase 7 G5 was the prior BINDING; cycle-3 Phase 5 is the new BINDING.

1. **Build**: `make linux-amd VERSION=dev` produced `builds/zcp-linux-amd64-final` at HEAD `babdc79f` (post-Phase-4 EXIT hash backfill).
2. **Push**: `scp` to eval-zcp `zcp` container at `/home/zerops/.local/bin/zcp-final`. `zcp-final version` → `zcp dev (babdc79f, 2026-04-27T13:50:43Z)`.
3. **MCP STDIO smoke** — idle envelope (eval-zcp project empty post-cycle-2 G6 cleanup):
   3-line message stream (`initialize` → `notifications/initialized` → `tools/call zerops_workflow action="status"`).
   Required `sleep 0.5` between messages so the server processes initialize before notification + tools/call.
4. **Decode + measure** wire-frame + decoded text length.

## Result — Idle envelope

| line | id | response | wire-size (B) | text-content (B) |
|---:|---:|---|---:|---:|
| 1 | 1 | initialize | 202 | (n/a) |
| 2 | 2 | status (idle, no services) | 2,400 | 2,220 |

Decoded text head:

```
## Status
Phase: idle
Services: none
Guidance:
  ### Bootstrap route discovery

  Start with discovery:

  ```
  zerops_workflow action="start" workflow="bootstrap" intent="<one-sentence>"
  ```

  Do this **without** `route`. `BootstrapDiscoveryResponse` returns
  priority-ordered `routeOptions[]`; no session is committed.
  ...
```

## Comparison vs cycle-2 Phase 7 G5 binding

| Metric | Cycle-2 G5 binding | Cycle-3 G5 binding | Δ |
|---|---:|---:|---:|
| L1 initialize wire | 202 B | 202 B | 0 |
| L2 status wire | 2,406 B | 2,400 B | −6 B |
| L2 text content | 2,220 B | 2,220 B | 0 |
| L2 envelope overhead | 186 B | 180 B | −6 B |

**Cycle-3 idle envelope rendering is essentially identical to cycle-2** (within ±6 B noise; cycle-3 atom edits targeted develop-active envelopes — F1 first-deploy, F3 zcli push refs, F4 push-dev cycle, F5 + Axis N universal-atom env-leaks. Idle path doesn't render those atoms, so byte impact is zero/minimal).

## Assertion 1 — wire-frame variance vs probe

The probe baseline is for develop-active envelopes (no idle fixture); direct ±5% / ±50 B comparison N/A as in cycle-2.

For cycle-3 develop-active envelope coverage: covered TRANSITIVELY via G6 binding scenario (see `g6-eval-regression-v3.md`). The G6 scenario seeds a Laravel adopt + develop flow which exercises the develop-active envelope end-to-end via real agent calls.

## Assertion 2 — markdown structure

✅ PASS. Status header, services list, guidance, plan/next sections all present. Text parses as valid markdown with canonical structure (matches cycle-2 idle output verbatim except for the −6 B envelope-overhead difference).

## Disposition (cycle-3 Phase 5 binding)

| Aspect | State |
|---|---|
| End-to-end MCP STDIO | ✅ PASS |
| Decoded markdown structure | ✅ PASS |
| Cumulative size variance vs cycle-2 idle | ✅ within ±6 B noise (idle path unchanged in cycle 3) |
| Wire-frame ± probe | ✅ structural-mismatch-not-regression (same as cycle-2 disposition; envelope-shape mismatch is testing-infra gap, not corpus regression) |
| G5 binding gate | ✅ GREEN |

**G5 BINDING GATE: GREEN.**

## Archived response

- `plans/audit-composition-v3/g5-smoke-2026-04-27/idle-response.ndjson`
