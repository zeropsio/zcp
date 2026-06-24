# Phase 6 — Review + deploy + verify

Goal: before advancing the DAG, prove the just-built slice is good AND live. Review is yours (host subagents); deploy + reachability are ZCP's. "Verified" is the composite of both — labeled as such.

## Step 1 — review (read-only host subagents, parallel)

Dispatch read-only review passes on the slice's diff with your own subagent/Task tool — they inspect, they don't edit — covering two angles:

- **correctness** — the codebase-design seams held (repository, `owner_id` + server-side authorization, default-deny reads), no secret hardcoded, input validated, no data written to runtime disk.
- **acceptance** — the slice's acceptance criteria are actually met (not just "tests pass" — the demoable result exists), and nothing in the PRD's floor commitments was skipped.

Run them in parallel (independent perspectives). Fold real findings back into a quick fix pass (another build-subagent turn) before deploy; drop noise. Review quality is a **host** concern — ZCP guarantees nothing about it, so never narrate "automatically reviewed" as a platform feature.

## Step 2 — deploy the slice (scoped to its services)

Deploy through `zerops_deploy` on the **unchanged** develop pipeline. The deploy scope is the **work session's service set** — you do not invent per-slice scoping in code; the existing work session already scopes the deploy to the services this slice touches. A corrective redeploy is non-destructive and never gated; don't reach for `zerops_import override=true` to recover a failed deploy.

## Step 3 — verify (ZCP's half of the composite)

`zerops_verify` (HTTP/health probe) proves the service is reachable and healthy. This is the ZCP-owned half. Combined with the host-owned half (acceptance tests green + review clean), the slice is **verified** — a composite:

| Check | Owner | Mechanism |
|---|---|---|
| Service deployed | ZCP | `zerops_deploy` success |
| Reachable / healthy | ZCP | `zerops_verify` |
| Acceptance met | you | the slice's tests went red→green |
| Code quality | you | the read-only review subagents |

Surface the composite result through `zerops_record_fact`. State it as the composite ("deployed + reachable + acceptance tests green + reviewed"), never as a bare ZCP "verified" that implies test quality.

## Step 4 — narrate, then advance

Hand the user **working software**, not a status dump:

> "I built **slice NN** — you can now **[concrete thing]** at **[live URL]**. Next I'll add **[slice NN+1]** — tell me if that's off."

The user reacts to the live URL. A reaction ("others should log in", "make it public") maps to a seam you built cheap to change (`phases/slices.md` codebase-design) → flip it, redeploy, re-narrate. Update the PRD's inferred-assumptions if a veto changed a one-way call.

**Only now advance the DAG.** Slice NN+1 starts (back to phase 5) once NN is deployed + verified. Read `zerops_workflow action="status"` to confirm where things stand — done-ness is derived from status, never from a field you wrote. Hold the one-slice-at-a-time line: there is no code gate enforcing it, only your discipline.
