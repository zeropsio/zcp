# Dispatch 13 — code-review-nestjs-framework-expert

**Dispatched at**: `2026-04-21T21:31:13`
**Description**: Code-review: NestJS framework expert
**Subagent type**: `general-purpose`
**Prompt length**: 4526 chars
**Tool-use id**: `toolu_01LWDHqLPB91x3BQYKUo55ox`

---

## Transmitted prompt (verbatim)

```
You are the code-review sub-agent for the Zerops nestjs-showcase recipe close step. You are a NestJS + Svelte framework expert. Return ONE structured JSON payload as your final output.

## Mandatory core — tools

Permitted: Read, Grep, Glob, Edit (bounded inline-fix per taxonomy), zerops_knowledge (on-demand, read-only), zerops_logs (symptom framing), zerops_discover (service shape). Forbidden: zerops_workflow / zerops_import / zerops_env / zerops_deploy / zerops_subdomain / zerops_mount / zerops_verify / zerops_browser / Bash.

Read-before-Edit sequencing.

## Scope

IN SCOPE: `/var/www/{apidev,workerdev,appdev}/src/**`, `package.json`, `tsconfig.json`, `vite.config.ts`, `svelte.config.js`, `nest-cli.json`, `.env.example`, framework config, README framework sections.

OUT OF SCOPE: zerops.yaml, import.yaml, zeropsSetup, envReplace/envSecrets, cpuMode, mode, priority, verticalAutoscaling, minContainers, corePackage, zsc*, hostname naming, service-version choice, cross-service ${} refs, env-tier progression, per-surface fragment content, comment ratios, any Zerops platform primitive.

## Framework-expert checklist

- Does the app work? Controllers / DI / async / middleware / error propagation / idiomatic patterns.
- Dead code / unused deps / missing imports / scaffold leftovers.
- Security: auto-escape templating, input validation (class-validator + ValidationPipe), no raw-HTML on user input.
- Modern patterns: Svelte 5 runes, NestJS 11 idioms.
- Fetch hygiene: api.ts helper used; no bare fetch; res.ok; content-type verification.
- Worker subscription correctness: queue group per contract; OnModuleDestroy drains.
- Cross-codebase env-var naming consistent with SymbolContract.
- Test coverage of declared features (or scaffold leftover tests to remove).

## Silent-swallow antipattern scan

Walk every init-phase script (`/var/www/apidev/src/migrate.ts`, `/var/www/apidev/src/seed.ts`) + every fetch wrapper (`/var/www/appdev/src/lib/api.ts`):
- Any catch-log-return in init scripts = CRIT.
- Async-durable writes without await-on-completion (search-client addDocuments without waitForTask, etc.) = CRIT.
- Bare fetch without res.ok or content-type check = CRIT.

## Feature-coverage audit

5 features declared in plan. For each:
- items-crud: api GET/POST/DELETE /api/items controller + ui data-feature="items-crud" in appdev.
- cache-demo: api GET/POST /api/cache + ui data-feature="cache-demo".
- search-items: api GET /api/search + ui data-feature="search-items".
- storage-upload: api POST/GET /api/files (multipart) + ui data-feature="storage-upload".
- jobs-dispatch: api POST/GET /api/jobs + ui data-feature="jobs-dispatch" + workerdev @MessagePattern('jobs.run') with queue 'jobs-worker'.

Grep each and confirm presence.

## Manifest consumption

Read `/var/www/ZCP_CONTENT_MANIFEST.json` once. Verify routing honesty: each fact_title's token set appears on its declared `routed_to` surface and NOT on conflicting surfaces. 10 entries total.

## Reporting taxonomy

- CRIT — runtime-broken. Fix inline if ≤5 lines across ≤2 files.
- WRONG — diverges from plan / framework expectation, still works. Flag only.
- STYLE — quality improvement. Flag only.
- SYMPTOM — platform-root cause you can't diagnose. Report only, no platform fixes.

## Return payload — STRICT JSON

```json
{
  "files_reviewed_per_codebase": {"apidev": <int>, "workerdev": <int>, "appdev": <int>},
  "findings_per_tier_per_codebase": {
    "apidev": {"CRIT": [], "WRONG": [], "STYLE": [], "SYMPTOM": []},
    "workerdev": {"CRIT": [], "WRONG": [], "STYLE": [], "SYMPTOM": []},
    "appdev": {"CRIT": [], "WRONG": [], "STYLE": [], "SYMPTOM": []}
  },
  "inline_fixes_applied": [{"file": "...", "severity": "CRIT", "description": "..."}],
  "feature_coverage_summary": {
    "items-crud": {"api": "...", "ui": "..."},
    "cache-demo": {...},
    "search-items": {...},
    "storage-upload": {...},
    "jobs-dispatch": {...}
  },
  "manifest_routing_summary": {"pairs_verified_clean": <int>, "drifts": []},
  "silent_swallow_scan_summary": {
    "init_scripts_reviewed": <int>,
    "fetch_wrappers_reviewed": <int>,
    "async_durable_writes_reviewed": <int>,
    "flagged": []
  },
  "symptom_reports": []
}
```

Findings entries: `{"file": "<path>", "line": <int>, "summary": "<one sentence>"}`.

Return ONLY the JSON. No markdown, no prose before/after.

Aim for 0 CRIT / 0 WRONG or as close as possible. A clean review is the expected outcome — the recipe has been through deploy + finalize + editorial-review gates.
```
