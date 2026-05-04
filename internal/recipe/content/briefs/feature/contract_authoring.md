# Contract authoring + curl smoke-tests (backend pass)

You are the backend feature sub-agent. Your scope is the API + worker
codebases. Routes, queue subjects, and contract shapes are authored
here; the frontend pass consumes them via curl + browser-walk.

## Record `contract`-kind facts for every route + queue subject

For every HTTP route and every queue subject you scaffold, record a
`contract` fact at the moment of authorship:

```json
{
  "topic": "items-list-endpoint",
  "kind": "contract",
  "subject": "GET /api/items",
  "purpose": "Lists items for the SPA grid panel; supports pagination via ?page=, ?limit=",
  "publishers": ["apidev"],
  "subscribers": ["appdev"],
  "payloadShape": "{ items: Item[], total: number, page: number }"
}
```

Worked example for a queue subject:

```json
{
  "topic": "jobs-process-subject",
  "kind": "contract",
  "subject": "jobs.process",
  "purpose": "Background job processing for the queue panel",
  "publishers": ["apidev"],
  "subscribers": ["workerdev"],
  "queueGroups": ["workers"],
  "payloadShape": "{ jobId: string, payload: object }"
}
```

The frontend pass reads these facts to drive curl validation. A missing
contract fact means the frontend pass has nothing to curl-test against,
and it will surface a notice asking the backend to record one.

## Curl smoke-test before closing the backend pass

Before you call `complete-phase phase=feature codebase=<host>` for an
api or worker codebase, run a curl pass against the published stage
slot. Each route + each queue publish path gets one curl invocation:

```bash
curl -s -o /tmp/items.json -w '%{http_code}\n' \
  https://apistage-${zeropsSubdomainHost}.prg1.zerops.app/api/items
```

For each curl invocation that returns the expected shape, record a
`curl_verification` fact. Required fields (validated by
`FactRecord.validateCurlVerification`): `topic`, `kind`, `subject`
(the route or queue subject the curl exercised), `service` (the
slot it hit), and `why` (prose recap — status code + observed
response shape, the load-bearing signal the frontend pass reads):

```json
{
  "topic": "items-list-endpoint-curl-verified",
  "kind": "curl_verification",
  "subject": "GET /api/items",
  "service": "apistage",
  "why": "200 OK against apistage-${zeropsSubdomainHost}.prg1.zerops.app/api/items; response shape matches the contract: { items: [], total: 0, page: 1 }. /tmp/items.json captures the body for next-session reference."
}
```

Curl-verification facts are the backend pass's close-out signal —
they're the contract receipt the frontend pass reads as the source of
truth for "this endpoint is live and returns this shape". Without a
curl_verification fact, the frontend pass can't tell whether a 404 is
a contract bug (backend never shipped the route) or a routing bug
(backend shipped it; SPA hits the wrong path).

## What this pass does NOT touch

- SPA codebase (`appdev`) source — design tokens, Tailwind, component
  rendering. The frontend pass owns those.
- Browser-walk facts. Use curl, not playwright/puppeteer; the frontend
  pass owns visual verification.
- Cross-codebase edits beyond the api+worker scope. If you discover a
  framework gotcha in the SPA codebase during research, record a
  `field_rationale` fact and let the frontend pass act on it.
