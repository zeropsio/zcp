# Phase 5 — Review + verify

Goal: check the just-built slice before advancing. Review is yours (host subagents); the dev URL already serves it. Follow the handover for your environment below.

## Step 1 — review (read-only host subagents, parallel)

Dispatch read-only review passes on the slice's diff with your own subagent/Task tool — they inspect, they don't edit — covering three angles:

- **correctness** — the codebase-design seams held (repository, `owner_id` + server-side authorization, default-deny reads), no secret hardcoded, input validated, nothing written to runtime disk, the Preserve invariant still holds.
- **acceptance** — the slice's acceptance criteria are actually met (the demoable result exists, not just "tests pass"), and no PRD floor commitment was skipped.
- **looks-right** — render the dev URL and critique it against `SKILL.md`'s craft floor (kit used, not stock-default-gray; loading/empty/error states built; responsive + visible keyboard focus + reduced-motion; restrained motion; copy in the user's terms; a `brand` slice is distinctive, not interchangeable with the stock kit). Screenshot the primary flow + its empty/error states if you can; otherwise inspect the markup and say which you did.

Run them in parallel (independent perspectives). Ask for **max 3** actionable findings total, only if they block the next slice or violate a PRD floor. Fold real findings into a quick fix pass (another build-subagent turn) before moving on; drop noise.

Architecture red flags (catch, don't expand into a general audit) beyond the correctness pass above: cross-boundary imports not in the PRD, new "common domain" utilities, entity-shaped dumping grounds, async/webhook/job paths without retry/idempotency/status/log handling.

If the same test, verify failure, or review finding repeats twice, stop patching symptoms. Write the pattern in the receipt as event → pattern → boundary/seam → next change, then adjust the slice or PRD decision row before continuing.

## Step 2 — verify on the dev URL

The slice is already live on the dev runtime. Confirm it: `zerops_verify` probes the dev service for reachability + health, and the acceptance tests are green. Combined with a clean review, the slice is **verified** — a composite:

| Check | Owner | Mechanism |
|---|---|---|
| Acceptance met | you | the slice's tests went red→green |
| Code quality | you | the read-only review subagents |
| Reachable / healthy | ZCP | `zerops_verify` on the dev URL |
| Looks & feels right | you | rendering the dev URL against the design chapter + the floor (screenshot if you can) |

Surface the composite to the user. State it as the composite ("acceptance tests green + reviewed + the dev URL serves it + I checked it looks right"), never as a bare "verified" that implies test quality. `zerops_verify` proves reachability only — it is not a design or test guarantee.

**Delayed effects:** if the slice touched async work, cache, search indexing, webhook/email/notification, auth/session, or a migration, do not call it verified on the first HTTP response alone. Probe the delayed result explicitly after the expected delay: job result, notification record, searchable item, session behavior, migrated read path, or failure/status log. If there is no observable delayed path, add one before advancing.

## Step 3 — hand over the change or promote at a checkpoint

### In a Mate

Work in the selected dev copy and try the work on its Preview. Commit and push, then open the change with a description or describe the existing change. The Review build puts the described head on the stage as a check. Do not wait for it; you are told if it fails. Offer its address as a Review build only when it passed at the change's head, naming the head and time. Build the stage in your own turn only for an app's first pass, to work on a build or infrastructure change, or when the person asks; say why.

### Outside a Mate

A formal deploy promotes the dev work to the stage service — a production-shaped build (build commands + health-check start, the way production will). Do it at a checkpoint, not per slice: a milestone worth a clean built-verification, before showing the user something durable (outside a dev pool, call its address their preview), before launch, or to prove the app builds from scratch (not just in the living dev runtime).

Deploy through `zerops_deploy` on the develop pipeline; the work session scopes it. After a stage deploy, `zerops_verify` the stage URL. If a deploy fails, redeploy through the same tool and follow its recovery guidance — don't reach around it.

## Step 4 — narrate

Hand the user **working software**, not a status dump. In a Mate, use the dev copy's **Preview** and its address; for a dev pool, also give the part's stage address as **Your app**, and name the lease Preview **Live while I work**, as "Words for the person" says. Elsewhere, use the preview address chosen above and, for a dev pool, the same two labels:

> "I built **[what it does, in their words]** — you can now **[concrete thing]**. Try **[one acceptance action]** and tell me only whether **[specific outcome]** matches your real workflow."

Outside a Mate, add: "Next I'll add **[the next slice, in their words]** unless that is off."

A reaction ("others should log in", "make it public") maps to a seam you built cheap to change (`phases/slices.md` codebase-design) → flip it, reload, re-narrate; update the PRD's inferred-assumptions if a veto changed a one-way call. In a Mate, end the turn after handing over the described change. Elsewhere, advance to the next slice (phase 4); read `zerops_workflow action="status"` to confirm where things stand.
