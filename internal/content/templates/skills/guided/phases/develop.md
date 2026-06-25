# Phase 4 — Build a slice (on the living dev runtime, with tests)

Goal: build ONE slice to working software on the dev runtime, with tests at the seam, then return a compact receipt. Repeat per slice, in DAG order.

## The dev runtime is a living server — build on it in place

The dev service runs continuously: edit the code, run the tests, and reload the running process via `zerops_dev_server` — no redeploy to see a change. Treat it like a local dev server that happens to run on Zerops with the real managed services wired in. The runtime mechanics — how the dev server starts, when it needs a restart, why it reaches the URL — come from `zerops_dev_server`'s own guidance at call time, not from here.

The guided discipline on top: **no formal deploy per slice.** Save formal deploys for the stage checkpoint (phase 5) — a per-slice deploy is wasted motion on a runtime you can just reload.

## A fresh subagent per slice

The main session is the orchestrator: it holds status summaries + compact receipts while the PRD and slice files live on disk. Build each slice in a fresh subagent whose brief **is** the slice markdown — pass the file verbatim, plus a pointer to the PRD topology chapter for service wiring. This keeps the orchestrator's context clean across slices.

## TDD at the seam (red → green)

The slice's **acceptance criteria are the test contract.** The subagent:

1. **Red** — write the acceptance test(s) at the seam named in the slice file (integration at the API/use-case boundary; unit where logic is non-trivial). Run them; see them fail for the right reason (the feature doesn't exist yet — not a setup error).
2. **Green** — implement the thinnest code that makes them pass, end-to-end through every layer the slice touches; reload the dev runtime and see it work at the URL.
3. **Refactor** — clean up with tests green; keep the codebase-design seams (repository, `owner_id` + authorization, default-deny) intact.

Scale test depth to the tier (from the PRD): experiment = a thin integration smoke; real-but-lean = integration at the acceptance seam + unit for non-trivial logic; production-business = + the floor invariants (authorization denies cross-tenant reads; no data on runtime disk).

## Wire to services from the live env, never literals

The slice touches provisioned services. Reference the generated connection variables (`zerops_env` / cross-service references) — never paste a host, port, password, bucket, or key into code or YAML. A hardcoded connection is a slice failure, not a detail.

## The compact receipt

Return a short receipt to the orchestrator — NOT the full implementation transcript:

```
Slice NN — <name>: built on dev.
- Acceptance: <what now works at the dev URL>
- Tests: <named test(s)> — green (red→green confirmed)
- Touched: <files / services wired>
- Notes: <anything the orchestrator needs — a deferred edge, a vetoable choice made>
```

The orchestrator takes this receipt (host-reported — the slice's own tests, not a ZCP guarantee), then moves to review + verify (phase 5). Control returns to the orchestrator between slices — don't start the next slice from the build subagent.
