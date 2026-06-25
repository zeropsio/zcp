# Phase 4 — Build a slice (on the living dev runtime, with tests)

Goal: build one serial slice, or a reviewed safe sibling lane, to working software on the dev runtime with tests at the seam, then return a compact receipt. Repeat in DAG order; only audited siblings run concurrently.

## The dev runtime is a live server — develop on it in place, deploy to make it durable

The dev service serves your **working tree** at the dev URL — the same tree you edit on the mount. So you iterate on it in place; **`zerops_deploy` is not how you preview a change:**

- **Served / interpreted runtimes (php-nginx, static)** — edit a file and the running server reflects it at the URL within a second or two; nothing to start.
- **Process runtimes (Node, Go, Python, a worker, or a `php artisan serve` loop)** — run and reload the long-running process with `zerops_dev_server`; its own guidance owns start/restart at call time.

But the working tree is on **ephemeral container disk** — un-deployed edits **revert if the container cycles** (restart / scale / redeploy). So deploy makes work *durable*; it is not how you see it:

1. **The first `zerops_deploy` stands the app up** — it applies `run.envVariables` (DB wiring + secrets), runs the `initCommands` (migrate/seed), and points the readiness check. After it, the URL is live and your edits are in-place.
2. **Build the slices on that live runtime** — no redeploy to preview.
3. **Deploy at a checkpoint** — after a batch of slices, before anything that could cycle the container, or to promote — to capture the accumulated work durably. **Not a deploy per slice.** Promotion to stage is a phase-5 checkpoint (`phases/review-deploy.md`).

## A fresh subagent per slice

The main session is the orchestrator: it holds status summaries + compact receipts while the PRD and slice files live on disk. Build each slice in a fresh subagent whose brief **is** the slice markdown — pass the file verbatim, plus a pointer to the PRD topology chapter for service wiring and boundaries. This keeps the orchestrator's context clean across slices.

The subagent works inside the slice's **Design seam**. It may change the named boundary/module and low-volatility plumbing needed for it; it should not create a new service, shared domain utility, or cross-boundary dependency unless the PRD decision row already authorizes that change.

## Parallel sibling builds (guarded)

Run siblings in parallel only where `phases/slices.md` marked `## Parallel: candidate`. Slice 1 is never parallel.

When two or more siblings are marked safe, dispatch one fresh subagent per slice in the same round. Each subagent gets only its slice file, the PRD topology/boundary pointer, and a reminder that other agents may be editing disjoint slices. They must stay inside their Design seam, run only their acceptance + Preserve checks, reload only when their slice is ready, and return the compact receipt. If a subagent discovers it needs a shared migration, shared auth/router/layout/schema change, or another slice's files, it stops and reports `serial needed` instead of forcing the edit.

The orchestrator integrates receipts one at a time, runs review + verify per slice, and serializes any cross-cutting fix pass. Do not start a new dependency layer until every slice in the current parallel lane is reviewed + verified.

## TDD at the seam (red → green)

The slice's **acceptance criteria are the test contract**, and the slice's seam is **the interface a real caller crosses — that same interface is the test surface.** Test *through* it: exercise the behavior the user relies on, not the functions behind it. If a test has to reach past the interface to assert anything, the seam is the wrong shape — fix the shape, don't weaken the test. The subagent:

1. **Red** — write the acceptance test(s) through the slice's seam (behavior at the use-case/API interface, not internals), plus the one Preserve check if it is not already covered. Run them; see them fail for the right reason (the feature doesn't exist yet — not a setup error).
2. **Green** — implement the thinnest code that makes them pass, end-to-end through every layer the slice touches, without breaking the Preserve invariant; see it work live at the dev URL (in place — no redeploy).
3. **Refactor** — clean up with tests green; keep the codebase-design seams (repository, `owner_id` + authorization, default-deny) intact.

Floor invariants live in the interface, not in an assertion's strength: an owner-scoped, default-deny repository can't be called wrong, so the test *demonstrates* the invariant while the model *owns* it. Scale test depth to the tier (from the PRD): experiment = a thin integration smoke; real-but-lean = integration at the acceptance seam + unit for non-trivial logic; production-business = + the floor invariants (authorization denies cross-tenant reads; no data on runtime disk).

## Build to the established look (lean on the kit + the craft you know)

The slice inherits the kit shell + theme from its Design seam; build with the kit's components and the craft floor (`SKILL.md`) — don't re-roll a bespoke CSS system per slice or override the kit's good defaults with worse ad-hoc values. The loading/empty/error states are part of red→green, not deferred polish. Keep copy in the user's terms (active voice; errors say what happened + how to fix; the same verb across a flow: Publish → Published).

## Wire to services from the live env, never literals

Reference the generated connection variables (`zerops_env` / cross-service references) — never paste a host/port/password/bucket/key into code or YAML. A hardcoded connection is a slice failure.

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

The orchestrator takes this receipt, then moves to review + verify (phase 5). Control returns to the orchestrator between slices or parallel lanes — don't start the next slice from the build subagent.
