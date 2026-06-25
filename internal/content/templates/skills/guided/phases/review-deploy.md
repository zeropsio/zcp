# Phase 5 — Review + verify (promote to stage at checkpoints)

Goal: prove the just-built slice is good and live before advancing. Review is yours (host subagents); the dev URL already serves it. A formal deploy to stage is a checkpoint, not a per-slice step.

## Step 1 — review (read-only host subagents, parallel)

Dispatch read-only review passes on the slice's diff with your own subagent/Task tool — they inspect, they don't edit — covering two angles:

- **correctness** — the codebase-design seams held (repository, `owner_id` + server-side authorization, default-deny reads), no secret hardcoded, input validated, nothing written to runtime disk.
- **acceptance** — the slice's acceptance criteria are actually met (the demoable result exists, not just "tests pass"), and no PRD floor commitment was skipped.

Run them in parallel (independent perspectives). Fold real findings into a quick fix pass (another build-subagent turn) before moving on; drop noise.

## Step 2 — verify on the dev URL

The slice is already live on the dev runtime. Confirm it: `zerops_verify` probes the dev service for reachability + health, and the acceptance tests are green. Combined with a clean review, the slice is **verified** — a composite:

| Check | Owner | Mechanism |
|---|---|---|
| Acceptance met | you | the slice's tests went red→green |
| Code quality | you | the read-only review subagents |
| Reachable / healthy | ZCP | `zerops_verify` on the dev URL |

Surface the composite to the user. State it as the composite ("acceptance tests green + reviewed + the dev URL serves it"), never as a bare "verified" that implies test quality.

## Step 3 — promote to stage at a checkpoint (not per slice)

A formal deploy promotes the dev work to the stage service — a production-shaped build (it runs the build commands and starts via health check, the way production will). Do it at a checkpoint, not after every slice:

- a milestone — a few slices add up to something worth a clean built-verification,
- before you show the user something durable, or before launch,
- when you need to prove the app builds and runs from scratch, not just in the living dev runtime.

Deploy through `zerops_deploy` on the develop pipeline; the work session scopes it. A corrective redeploy is non-destructive and never gated — don't reach for `zerops_import override=true` to recover a failed deploy. After a stage deploy, `zerops_verify` the stage URL.

## Step 4 — narrate, then advance

Hand the user **working software**, not a status dump:

> "I built **slice NN** — you can now **[concrete thing]** at **[live URL]**. Next I'll add **[slice NN+1]** — tell me if that's off."

A reaction ("others should log in", "make it public") maps to a seam you built cheap to change (`phases/slices.md` codebase-design) → flip it, reload, re-narrate; update the PRD's inferred-assumptions if a veto changed a one-way call. Then advance to the next slice (phase 4). Read `zerops_workflow action="status"` to confirm where things stand — done-ness is derived from status, never from a field you wrote.
