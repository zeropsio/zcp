# Classification taxonomy

Every observation in the facts log — a surprise, an incident, a scaffold decision, an operational hack — classifies into exactly one of six classes below BEFORE it is placed on any surface. Classification is an input to routing; routing (atom: routing-matrix.md) is an input to the surface contracts (atom: content-surface-contracts.md).

The taxonomy is a positive allow-list: each class defines what its members ARE, what tests qualify them, and where they default-route. Facts whose default route is "discarded" may be re-routed elsewhere only with a non-empty `override_reason` in the manifest.

---

## Class 1 — framework-invariant

What it IS: a fact true of the Zerops platform regardless of the recipe's framework, scaffold choices, or library selection. A different framework entirely, or a different application built on the same framework, would hit the same trap.

Test: would a porter running a completely different framework on Zerops hit the same behavior? Yes → framework-invariant.

Example: cross-service environment variables auto-inject project-wide; a service that redeclares `db_hostname: ${db_hostname}` in its own `run.envVariables` breaks the container's env. Independent of what runs in the service.

Default route: `content_gotcha` (published as a knowledge-base gotcha) with a citation to the matching platform topic on the Citation Map.

---

## Class 2 — framework × platform intersection

What it IS: a fact specific to one framework AND caused by a Zerops platform behavior. Neither side alone produces the failure mode — you need the framework's specific implementation choice AND the platform's specific mechanism.

Test: does the Zerops side contribute materially to the failure mode (not just "it happens on Zerops because I ran it there")? Yes → intersection.

Example: a Node NATS client at its current major release strips URL-embedded credentials silently; Zerops injects NATS credentials as separate `NATS_USER` / `NATS_PASS` env vars. Either side alone would not produce `AUTHORIZATION_VIOLATION` on first subscribe. Together they do.

Default route: `content_gotcha`. The bullet must name both sides clearly — the platform mechanism and the framework-side implementation choice.

---

## Class 3 — framework-quirk

What it IS: a fact about the framework's own behavior with no Zerops involvement. A porter running the same framework on any hosting platform would hit the same trap.

Test: does the Zerops side contribute materially? No → framework-quirk.

Example: `app.setGlobalPrefix('api')` colliding with `@Controller('api/...')` decorators in a NestJS app. The collision is a NestJS behavior; Zerops is nowhere in the failure chain.

Default route: `discarded`. Framework docs or code comments are the right home. Routing this anywhere else requires a non-empty `override_reason` in the manifest (and, in practice, a reframing of the fact so the Zerops side becomes material).

---

## Class 4 — scaffold-decision

What it IS: a design choice the recipe made in its own code — "we picked X over Y, reader should understand why". Non-obvious design trade-off in the recipe's own scaffold, zerops.yaml, or application code.

Test: would the porter have a different equivalent choice in their own codebase? Yes → scaffold-decision (and their choice might differ).

Example: using `deployFiles: ./dist/~` in the prod setup to strip the `dist` directory wrapper, keyed to a Zerops-specific tilde-syntax decision; using a single setup for both dev and prod to avoid env-var-map divergence.

Default route: split three ways by sub-kind:

- Config choice in YAML → `zerops_yaml_comment`.
- Code-level principle a porter should know → `content_ig` (H3 item in the integration guide).
- Operational choice for this specific repo → `claude_md`.

---

## Class 5 — operational

What it IS: how to iterate on, test, or reset THIS specific repo locally. Not a deploy instruction, not a porting instruction.

Test: is the fact useful for running this repo locally, independent of deploying it? Yes → operational.

Example: "drop-and-reseed without a full redeploy via `sudo -u zerops psql -c 'TRUNCATE items CASCADE'` then re-run the seed command"; "recover from a burned `execOnce` key by touching any source file to rotate `appVersionId`".

Default route: `claude_md`.

---

## Class 6 — self-inflicted

What it IS: the recipe's own code had a bug; the code was fixed; a reasonable porter bringing their own code would not hit that specific bug because their code does not have that specific bug.

Test: could the observation be summarized as "our code did X, we fixed it to do Y"? Yes → self-inflicted.

Example: a seed script that silently exited 0 with no output, causing `execOnce` to record a successful seed that inserted zero rows. The seed script was wrong; fixing it loudly is the correction. There is no platform teaching here.

Default route: `discarded`. Routing this anywhere except `discarded` requires a non-empty `override_reason` that reframes the fact as a porter-facing symptom with a concrete failure mode.

---

## Classification workflow

1. Separate mechanism (what Zerops does) from symptom (what our code did). Classify on mechanism.
2. Ask "would a porter with different scaffold code hit this?" — yes → framework-invariant or intersection; no → scaffold-decision or self-inflicted.
3. Check the Citation Map. If a matching platform topic exists, the fact is almost certainly framework-invariant or intersection with the platform topic cited.
4. Self-inflicted litmus: "could this be summarized as 'our code did X, we fixed it to do Y'?" Yes → self-inflicted, and the fix belongs in code, not in content.

## Override-reason rule

Classes with default route `discarded` (framework-quirk, self-inflicted) may be routed elsewhere, but only when the manifest entry carries a non-empty `override_reason` explaining why the default does not apply for this specific fact. A canonical reason reads: "reframed from scaffold-internal bug to porter-facing symptom with concrete failure mode and platform-mechanism citation". An empty `override_reason` on a default-discarded class fails the manifest consistency contract.
