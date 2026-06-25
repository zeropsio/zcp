# Phase 5 — Review + verify (promote to stage at checkpoints)

Goal: prove the just-built slice is good and live before advancing. Review is yours (host subagents); the dev URL already serves it. A formal deploy to stage is a checkpoint, not a per-slice step.

## Step 1 — review (read-only host subagents, parallel)

Dispatch read-only review passes on the slice's diff with your own subagent/Task tool — they inspect, they don't edit — covering two angles:

- **correctness** — the codebase-design seams held (repository, `owner_id` + server-side authorization, default-deny reads), no secret hardcoded, input validated, nothing written to runtime disk, the Preserve invariant still holds.
- **acceptance** — the slice's acceptance criteria are actually met (the demoable result exists, not just "tests pass"), and no PRD floor commitment was skipped.

Run them in parallel (independent perspectives). Ask for **max 3** actionable findings total, only if they block the next slice or violate a PRD floor. Fold real findings into a quick fix pass (another build-subagent turn) before moving on; drop noise.

Architecture red flags to catch, not expand into a general audit: repeated knowledge across modules, cross-boundary imports not in the PRD, new "common domain" utilities, entity-shaped dumping grounds, runtime-disk persistence, hardcoded env/secret/connection values, async/webhook/job paths without retry/idempotency/status/log handling.

If the same test, verify failure, or review finding repeats twice, stop patching symptoms. Write the pattern in the receipt as event → pattern → boundary/seam → next change, then adjust the slice or PRD decision row before continuing.

## Step 2 — verify on the dev URL

The slice is already live on the dev runtime. Confirm it: `zerops_verify` probes the dev service for reachability + health, and the acceptance tests are green. Combined with a clean review, the slice is **verified** — a composite:

| Check | Owner | Mechanism |
|---|---|---|
| Acceptance met | you | the slice's tests went red→green |
| Code quality | you | the read-only review subagents |
| Reachable / healthy | ZCP | `zerops_verify` on the dev URL |

Surface the composite to the user. State it as the composite ("acceptance tests green + reviewed + the dev URL serves it"), never as a bare "verified" that implies test quality.

**Delayed effects:** if the slice touched async work, cache, search indexing, webhook/email/notification, auth/session, or a migration, do not call it verified on the first HTTP response alone. Probe the delayed result explicitly after the expected delay: job result, notification record, searchable item, session behavior, migrated read path, or failure/status log. If there is no observable delayed path, add one before advancing.

## Step 3 — promote to stage at a checkpoint (not per slice)

A formal deploy promotes the dev work to the stage service — a production-shaped build (it runs the build commands and starts via health check, the way production will). Do it at a checkpoint, not after every slice:

- a milestone — a few slices add up to something worth a clean built-verification,
- before you show the user something durable, or before launch,
- when you need to prove the app builds and runs from scratch, not just in the living dev runtime.

Deploy through `zerops_deploy` on the develop pipeline; the work session scopes it. After a stage deploy, `zerops_verify` the stage URL. If a deploy fails, redeploy through the same tool and follow its recovery guidance — don't reach around it.

## Step 4 — narrate, then advance

Hand the user **working software**, not a status dump:

> "I built **slice NN** — you can now **[concrete thing]** at **[live URL]**. Try **[one acceptance action]** and tell me only whether **[specific outcome]** matches your real workflow. Next I'll add **[slice NN+1]** unless that is off."

A reaction ("others should log in", "make it public") maps to a seam you built cheap to change (`phases/slices.md` codebase-design) → flip it, reload, re-narrate; update the PRD's inferred-assumptions if a veto changed a one-way call. Then advance to the next slice (phase 4). Read `zerops_workflow action="status"` to confirm where things stand — done-ness is derived from status, never from a field you wrote.
