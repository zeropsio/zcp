# Dispatch 11 — apply-editorial-crit-wrong-fixes

**Dispatched at**: `2026-04-21T21:23:30`
**Description**: Apply editorial CRIT+WRONG fixes
**Subagent type**: `general-purpose`
**Prompt length**: 5784 chars
**Tool-use id**: `toolu_01FyxkvFzmHb5W1zwfDXn2D1`

---

## Transmitted prompt (verbatim)

```
Apply fixes to the Zerops nestjs-showcase recipe to clear the editorial-review CRIT+WRONG findings. Only edit the files named. Do NOT use zerops_* workflow/deploy/import tools.

Permitted: Read, Edit, Write, Grep, Glob, Bash (local utilities only — no SSH).

## CRIT findings to fix

### CRIT-1: tier-3 Stage README falsely claims cross-tier DB sharing
File: `/var/www/environments/stage/README.md`
Verify the file for any line claiming "Stage hits the same DB as dev on tiers 0-2" or similar cross-tier DB sharing. If present, delete or rewrite — each tier is a separate Zerops project per tier-0 README's own explanation. If NOT present, note that and move on.

### CRIT-2: tier-5 NATS exactly-once fabrication
File: `/var/www/zcprecipator/nestjs-showcase/5 — Highly-available Production/import.yaml` — AUTO-GENERATED, you cannot edit.
Source for fix: `/var/www/environments/prod-ha/README.md` MAY contain the same claim; also the envComments that the main agent submits regenerate the YAML comments. But since this subagent cannot call zerops_workflow, your job is to find and rewrite any similar claim in the mount env READMEs. Search all `/var/www/environments/*/README.md` for phrases like "exactly once" / "exactly-once" / "no message loss" in a NATS context and soften to "at-most-once with competing-consumer load balancing; client handlers must be idempotent for retry safety". Same search across `/var/www/{apidev,workerdev,appdev}/README.md` and `GOTCHAS.md` and `INTEGRATION-GUIDE.md`.

### CRIT-3: meilisearch HA contradiction
The tier-1 env comment says "Single-node is the only mode available today" while tier-5 sets mode:HA. The platform's availableStacks list did NOT explicitly restrict meilisearch to non-HA. Since the tier-5 config is what the platform actually accepts, update any tier-1-or-lower env README that asserts meilisearch has no HA, to instead say "defaults to NON_HA at this tier; promote to mode: HA at the production-HA tier". Check `/var/www/environments/*/README.md` for the contradicting assertion and reword.

### CRIT-4: NATS servers scheme contradiction between apidev and workerdev
Search `/var/www/apidev/README.md`, `/var/www/apidev/GOTCHAS.md`, `/var/www/apidev/INTEGRATION-GUIDE.md` for the prescription about NATS `servers` being bare host:port (no scheme). Search `/var/www/workerdev/README.md`, `/var/www/workerdev/GOTCHAS.md`, `/var/www/workerdev/INTEGRATION-GUIDE.md` for the scheme-prefixed prescription.

Ground truth (verified by reading source):
- apidev/src/clients/queue/nats.client.ts uses `${host}:${port}` (no scheme) — raw `nats` v2 Node client
- workerdev/src/main.ts uses `nats://${host}:${port}` — NestJS microservice transport (which internally uses same nats v2 but accepts scheme-prefixed)

Reword each prescription so neither presents the form as a UNIVERSAL rule. Acceptable phrasing: "this codebase uses the bare `host:port` form that the raw nats v2 client accepts" or "this codebase uses the scheme-prefixed `nats://host:port` form per the Nest microservice transport". The key: each codebase's doc describes ITS OWN form and does NOT generalize.

## WRONG findings to fix

### WRONG-1: tier-0 README initCommands assertion
File: `/var/www/environments/ai-agent/README.md`
Search for "initCommands do NOT fire automatically" or "initCommands do not fire". Rewrite to reflect that apidev's dev setup DOES declare initCommands (migrate + seed) which run at each deploy via `zsc execOnce` keys. Possible phrasing: "initCommands run on every deploy regardless of the `zsc noop` idle start — migrate is gated by `${appVersionId}` so it fires once per deploy, seed by a static key so it fires once for the stack's lifetime."

### WRONG-2: tier-2 Local README calls cache service `cache` instead of `redis`
File: `/var/www/environments/local-dev/README.md`
Search for the hostname list or any mention of `cache` as a service hostname. Replace with `redis`. The managed service is named `redis` in every tier's import.yaml.

### WRONG-3: tier-4 Small Production claim about upload byte streaming
File: `/var/www/environments/prod/README.md`
Search for "Presigned URLs hit the CDN edge directly" or "runtime containers never stream upload bytes". This is wrong — uploads go through Multer in apidev/src/files/files.controller.ts (`@UseInterceptors(FileInterceptor('file'))`). Soften to: "Presigned-URL download hits the storage edge directly; uploads flow through the runtime Multer buffer before putObject, so upload sizing still counts toward the runtime container's RAM budget."

### WRONG-4: workerdev README exactly-once claim
File: `/var/www/workerdev/README.md`
Find "each message runs exactly once" or similar. Reword to "each message is delivered to exactly one live replica at a time (competing-consumer load balancing); handler crashes mid-processing can re-deliver, so the handler must be idempotent for retry safety."

## Reclassification delta (add to manifest)

File: `/var/www/ZCP_CONTENT_MANIFEST.json`
Add a new entry to the facts array:
```json
{
  "fact_title": "Worker container stays ACTIVE in the dashboard even when the Nest microservice has crashed",
  "classification": "framework-invariant",
  "routed_to": "content_gotcha",
  "override_reason": ""
}
```

Parse the current JSON, add the entry to the facts array, write it back with `jq .` or JSON.stringify to preserve format. Do not break the JSON.

## Reporting

Return a brief list of:
- Which files you actually edited (absolute paths + one-line summary of the change).
- Which CRIT/WRONG findings you verified as NON-EXISTENT (reviewer misread) vs FIXED.
- The new ZCP_CONTENT_MANIFEST.json entry count and whether jq empty confirms it's still valid JSON.

Budget: 6 tool-round batches max. Be surgical — search, confirm, edit, move on.
```
