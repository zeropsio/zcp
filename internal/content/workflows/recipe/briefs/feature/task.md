# task

You own the declared feature list end-to-end across every mount named for this dispatch. A single author is responsible for the api routes, the worker payloads, and the frontend components as one coherent whole. The feature list is authoritative — implement exactly what is declared, no more, no less.

## What "feature as vertical slice" means

For every feature `F` in the declared feature list:

- If `F.surface` includes `api`: implement an endpoint at the contract path named by the feature. It returns 200 with `Content-Type: application/json` on the happy path. Read features return a typed JSON object / array on GET; write features accept a typed JSON body on POST and return the created / updated record.
- If `F.surface` includes `ui`: implement the dashboard section wrapped in `<element data-feature="{F.uiTestId}">` with four explicit render states — loading, error (visible banner in the `[data-error]` slot), empty ("no results yet" text), populated. The section renders whatever selectors the feature's `MustObserve` references (for example `[data-row]`, `[data-hit]`, `[data-result]`, `[data-file]`, `[data-processed-at]`). Every fetch goes through the scaffolded api helper at `src/lib/api.ts` (or the language-equivalent); bare `fetch()` in a component is out.
- If `F.surface` includes `worker`: implement the consumer, the publishing endpoint on the api, and the result write-back (DB row, cache key, whatever `MustObserve` polls). The worker handler never swallows silently — on failure, mark the row failed with the error message and log loudly.
- If `F.surface` includes `search`: the search-sync step is part of the seed / migrate path, awaits task completion (search-client `waitForTask` or equivalent), and exits non-zero if durability is not observable.

## Single author, one session per feature

Implement each feature's api route, worker consumer (when applicable), and frontend component in **one edit session** per feature. Do not split a feature across codebase-scoped passes. Contract mismatches — frontend reading `.hits` while api returns `{ hits: [] }`, worker reading `payload.jobType` while api publishes `type` — are the single biggest class caught at close-review, and the only structural prevention is writing both sides through the same interface declaration in the same session.

## Cross-codebase contract discipline

For each feature, in this order:

1. Declare the response DTO (and for worker features: the NATS payload interface + worker result interface) as TypeScript interfaces at the top of the owning api controller, named from the contract's `DTOs` list.
2. Implement the api controller using the interface as the return type.
3. Implement the consuming frontend component in the same session, copy-pasting the DTO byte-identically into the component file.
4. Implement the worker handler in the same session (when applicable), copy-pasting the payload + result interfaces byte-identically.
5. Smoke-test over SSH immediately: `ssh {api-hostname} "curl -sS -o /dev/null -w '%{http_code} %{content_type}\n' http://localhost:{port}{route}"`. 200 + `application/json` advances you; 200 + `text/html` means the frontend helper is not in use or the route never registered; fix before moving to the next feature.

## UX acceptance standards

- Every feature reachable from the dashboard at its own `data-feature` wrapper.
- Every dashboard section styled with the scaffolded CSS tokens / component classes — styled form controls with padding + border-radius + focus ring + button hover; headings delineated; consistent vertical rhythm; tables with headers + cell padding + alternating row shading; monospace for ids; timestamps humanized.
- Four render states per async section (loading / error / empty / populated); error state visible, not swallowed into an empty render.
- Dynamic content auto-escaped by the framework; never raw-HTML output on user-influenced data.
- Observable evidence for every `MustObserve` assertion: the selector exists, the count increments (or the text matches) after the feature's `Interaction` runs.

## Installed-package verification

Before writing any import, decorator registration, adapter wiring, or module-wiring call, verify the symbol / subpath against the installed package on disk — Read `node_modules/<pkg>/package.json` (Node), `vendor/<pkg>/composer.json` (PHP), `go.mod` (Go), the `*.gemspec` (Ruby), or the equivalent manifest. Training-data memory for library APIs is version-frozen and will surface stale paths that compiled under prior majors but do not exist in the version installed here. One Read per package is always cheaper than a close-step round-trip. When uncertain, run the installed CLI's own scaffolder against a scratch directory and copy the import shape byte-identically.

## After implementing every feature

1. `ssh {api-hostname} "cd /var/www && npm run build 2>&1 | tail -20"` (or language-equivalent build) — must succeed in every codebase you modified.
2. Start the dev servers via `mcp__zerops__zerops_dev_server` for each hostname named in this dispatch.
3. Curl each feature's `healthCheck` endpoint and confirm 200 + `application/json` + the DTO shape the interface declares.
4. For queue+worker features: POST the dispatch endpoint, wait for the worker to consume, GET the result endpoint, confirm the processed-at field is populated. This proves the producer → broker → worker → persistence round-trip.
5. For search features: the search endpoint returns hits for a term present in the seeded records.
6. Port hygiene: before restarting any dev server, kill any stale port holder with `ssh {hostname} "fuser -k {port}/tcp 2>/dev/null || true"`.
7. Iterate fixes under the cadence rule stitched below. Do not ship a feature you could not verify.
