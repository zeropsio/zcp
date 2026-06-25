# Phase 4 — Build a slice (on the living dev runtime, with tests)

Goal: build one serial slice, or a reviewed safe sibling lane, to working software on the dev runtime with tests at the seam, then return a compact receipt. Repeat in DAG order; only audited siblings run concurrently.

## The dev runtime is a living server — build on it in place

The dev service runs continuously: edit the code, run the tests, and reload the running process via `zerops_dev_server` — no redeploy to see a change. Treat it like a local dev server that happens to run on Zerops with the real managed services wired in. The runtime mechanics — how the dev server starts, when it needs a restart, why it reaches the URL — come from `zerops_dev_server`'s own guidance at call time, not from here.

The guided discipline on top: **no formal deploy per slice.** Save formal deploys for the stage checkpoint (phase 5) — a per-slice deploy is wasted motion on a runtime you can just reload.

## A fresh subagent per slice

The main session is the orchestrator: it holds status summaries + compact receipts while the PRD and slice files live on disk. Build each slice in a fresh subagent whose brief **is** the slice markdown — pass the file verbatim, plus a pointer to the PRD topology chapter for service wiring and boundaries. This keeps the orchestrator's context clean across slices.

The subagent works inside the slice's **Design seam**. It may change the named boundary/module and low-volatility plumbing needed for it; it should not create a new service, shared domain utility, or cross-boundary dependency unless the PRD decision row already authorizes that change.

## Parallel sibling builds (guarded)

Parallel build is allowed only for slices whose `## Parallel` field was marked candidate by the phase 3 audit. Slice 1 is never parallel. `Blocked-by: none` alone is not enough.

When two or more siblings are marked safe, dispatch one fresh subagent per slice in the same round. Each subagent gets only its slice file, the PRD topology/boundary pointer, and a reminder that other agents may be editing disjoint slices. They must stay inside their Design seam, run only their acceptance + Preserve checks, reload only when their slice is ready, and return the compact receipt. If a subagent discovers it needs a shared migration, shared auth/router/layout/schema change, or another slice's files, it stops and reports `serial needed` instead of forcing the edit.

The orchestrator integrates receipts one at a time, runs review + verify per slice, and serializes any cross-cutting fix pass. Do not start a new dependency layer until every slice in the current parallel lane is reviewed + verified.

## TDD at the seam (red → green)

The slice's **acceptance criteria are the test contract.** The subagent:

1. **Red** — write the acceptance test(s) at the seam named in the slice file (integration at the API/use-case boundary; unit where logic is non-trivial), plus the one Preserve check if it is not already covered. Run them; see them fail for the right reason (the feature doesn't exist yet — not a setup error).
2. **Green** — implement the thinnest code that makes them pass, end-to-end through every layer the slice touches, without breaking the Preserve invariant; reload the dev runtime and see it work at the URL.
3. **Refactor** — clean up with tests green; keep the codebase-design seams (repository, `owner_id` + authorization, default-deny) intact.

Scale test depth to the tier (from the PRD): experiment = a thin integration smoke; real-but-lean = integration at the acceptance seam + unit for non-trivial logic; production-business = + the floor invariants (authorization denies cross-tenant reads; no data on runtime disk).

## Build to the established look (lean on the kit + the craft you know)

The slice inherits the kit shell + theme from its Design seam. Build with the kit's components and the motion/interaction craft you already know makes UI feel premium — restraint over flourish, `ease-out` for entrances, press feedback, respecting `prefers-reduced-motion`. Don't re-roll a bespoke CSS system per slice, and don't override the kit's good defaults with worse ad-hoc values. The loading/empty/error states are part of red→green, not deferred polish. Keep copy in the user's terms: name things by what the user controls, active voice, errors say what happened + how to fix, the same verb across a flow (Publish → Published).

## Wire to services from the live env, never literals

The slice touches provisioned services. Reference the generated connection variables (`zerops_env` / cross-service references) — never paste a host, port, password, bucket, or key into code or YAML. A hardcoded connection is a slice failure, not a detail.

## The compact receipt

Return a short receipt to the orchestrator — NOT the full implementation transcript:

```
Slice NN — <name>: built on dev.
- Acceptance: <what now works at the dev URL>
- Preserve: <existing flow/invariant checked>
- Tests: <named test(s)> — green (red→green confirmed)
- Boundary: <PRD boundary/module touched; any adapter/repository added>
- Touched: <files / services wired>
- Notes: <anything the orchestrator needs — a deferred edge, a vetoable choice made>
```

The orchestrator takes this receipt (host-reported — the slice's own tests, not a ZCP guarantee), then moves to review + verify (phase 5). Control returns to the orchestrator between slices or parallel lanes — don't start the next slice from the build subagent.
