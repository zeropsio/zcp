# Integration validator (frontend pass)

You are the frontend feature sub-agent. The backend pass has shipped
the api + worker codebases and recorded `contract` facts naming every
endpoint and queue subject, plus `curl_verification` facts proving each
endpoint returns the expected shape. Your job: wire the SPA against
those contracts, validate the integration end-to-end, and ship the
visible recipe.

## Curl + browser-walk before claiming UI works

Every SPA panel exercises one or more api endpoints. Before you wire a
panel into the UI:

1. Read the matching `contract` fact from the facts log.
2. Curl the endpoint to the recorded shape against the stage slot:
   ```bash
   curl -s -o /tmp/items.json -w '%{http_code}\n' \
     https://apistage-${zeropsSubdomainHost}.prg1.zerops.app/api/items
   ```
3. Confirm the curl response matches the contract's `payloadShape`. If
   it does, wire the panel. If it doesn't, see "cross-codebase edit
   authority" below.

After wiring, browser-walk the panel. Open the appstage subdomain via
`agent-browser`, click the panel into its load state, confirm the data
renders. Record a `browser_verification` fact. Required fields
(validated by `FactRecord.validateBrowserVerification`): `topic`,
`kind`, `subject` (the panel or page name), `service` (the deployed
slot the browser walked), and `why` (visual evidence — what
rendered + how many items + which state):

```json
{
  "topic": "items-grid-panel-rendered",
  "kind": "browser_verification",
  "subject": "items panel (ItemsGrid)",
  "service": "appstage",
  "why": "Walked appstage-${zeropsSubdomainHost}.prg1.zerops.app/items in agent-browser; 20 items rendered in a grid; pagination shows page 1 of 2; no console errors."
}
```

## Cross-codebase edit authority — bounded

When curl proves the contract is broken (404, 500, missing field,
broken pagination, schema mismatch vs the recorded contract fact), you
MAY edit the backend codebase to fix it. The bar:

> Would the curl FAIL without this edit, or is it just nicer if the
> response matches my preferred shape?

If the answer is "the curl FAILS without this edit", ACT. Edit
`apidev/src/`, `workerdev/src/`, etc. through the SSH path:

```bash
ssh apidev "cd /var/www && <your edit via heredoc or sed>"
```

If the answer is "I'd rather have field X named Y so my SPA is
cleaner", HOLD. Wire the SPA to the contract as-shipped. Backend
ergonomic preferences are not your authority.

Concrete decision examples:

| Scenario | Decision |
|---|---|
| `GET /api/items` returns 404 | ACT — backend never shipped the route. Add the controller + register the route. |
| `GET /api/items` returns 500 | ACT — backend shipped a buggy implementation. Fix it. |
| `GET /api/items` returns `{result: [...]}` instead of contracted `{output: [...]}` | ACT — broken contract: 200 OK, wrong field name. The HTTP status is success but the SPA can't read `{result}` against a contract that names `{output}`. Decide: rename in backend OR update the contract fact + SPA both. HOLD only if the contract fact itself was wrong (rare). |
| `GET /api/items` returns the items but pagination is `{page, perPage}` and your SPA prefers `{page, limit}` | HOLD — that's an ergonomic preference. Wire the SPA to `perPage`. |
| Endpoint missing CORS headers; SPA can't fetch | ACT — wire-mode CORS bug. Fix in backend. |

## Record contract revisions when you edit backend

When you edit a backend, record a `contract` fact with `changeKind:
replace` naming the prior shape, the new shape, and the reason. Then
re-run curl against the new shape and record a fresh
`curl_verification` fact. The post-edit curl pass becomes the
validation evidence.

## Redeploy what was changed

If you edit `apidev`, redeploy `apistage` (and `workerstage` if
applicable) so the next agent run sees the canonical contract:

```
zerops_deploy slot=apistage
```

Without redeploy, your edit is dev-only and the next session walks the
old contract.

## What NOT to do

- Don't edit the backend for "nicer schema" preferences. The bar is
  curl-proves-broken, not subjective taste.
- Don't skip the curl pass before wiring. SPA-bug-blamed-on-backend
  is the leading wasted-cycle cause when the SPA can't fetch.
- Don't browser-walk before curling. Curl is the cheaper signal; if
  curl fails, the browser-walk would fail the same way and burns more
  time.
- Don't skip the redeploy after a backend edit. The `apidev` ssh edit
  is iterative; the `apistage` slot is the integration-witness that
  the next session reads.
