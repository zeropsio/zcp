# Phase 5 — Build a slice (fresh subagent + TDD)

Goal: build ONE slice to working software, in a fresh subagent, with tests at the seam. Then return to the main session with a compact receipt. Repeat per slice — one at a time, in DAG order.

## Why a fresh subagent per slice

The main session is the **orchestrator**: it holds only status summaries + compact receipts, while the full PRD and slice files live on disk. Each slice runs in a **fresh subagent** (a clean context) whose brief **is** the slice markdown — already on disk, so you pass it verbatim, no prompt-composer needed. This is the context-saving substitute for "clear between tasks": the orchestrator never accumulates the noise of every slice's implementation.

Dispatch with your own subagent/Task tool. Hand the subagent: the slice file (`.zcp/guided/slices/NN-*.md`), a pointer to the PRD topology chapter for service wiring, and the instruction to follow the TDD discipline below and return a compact receipt.

## TDD discipline (red → green at the seam)

The slice's **acceptance criteria are the test contract.** The subagent:

1. **Red** — write the acceptance test(s) at the seam named in the slice file (integration at the API/use-case boundary; unit where logic is non-trivial). Run them; see them fail for the right reason (the feature doesn't exist yet — not a setup error).
2. **Green** — implement the thinnest code that makes them pass, end-to-end through every layer the slice touches.
3. **Refactor** — clean up with the tests green; keep the codebase-design seams (repository, `owner_id` + authorization, default-deny) intact.

Scale test depth to the tier (from the PRD): experiment = a thin integration smoke; real-but-lean = integration at the acceptance seam + unit for non-trivial logic; production-business = + the floor invariants (authorization denies cross-tenant reads; no data on runtime disk).

## Wire to services from the live env, never literals

The slice touches provisioned services. Reference the generated connection variables (`zerops_env` / cross-service references) — never paste a host, port, password, bucket, or key into code or YAML. A leaked or hardcoded connection is a slice failure, not a detail.

## The compact receipt

The subagent returns to the main session with a short receipt — NOT the full implementation transcript:

```
Slice NN — <name>: built.
- Acceptance: <what now works>
- Tests: <named test(s)> — green (red→green confirmed)
- Touched: <files / services wired>
- Notes: <anything the orchestrator needs — a deferred edge, a vetoable choice made>
```

The orchestrator records this (and surfaces host-reported results through `zerops_record_fact`), then proceeds to review + deploy + verify (phase 6). **Do not start the next slice from the build subagent** — control returns to the orchestrator, which advances the DAG only after the current slice is deployed and verified.

## Honesty boundary

The subagent's tests are the **acceptance** check — they are host-reported, not a ZCP guarantee. Never claim "tested" or "reviewed" as something ZCP enforces. The enforceable contract is the OUTPUT of phase 6: a scoped deploy + `zerops_verify` + the named acceptance test that went red→green.
