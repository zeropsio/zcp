# The group broker's first `POST /person/token` of a page load fails without CORS headers

**Surfaced**: 2026-09-23 — the Mate client state-model programme's live runs against the KRLS broker
(gates B, E, G and the S2 measurements): the first person-token POST of a cold load failed in 26 of 30
recorded runs; the retry 2 s later succeeded with the same request shape and a fresh throwaway.

**Why deferred**: the client already treats it as transient (`unavailable`, retried on the 2 s rung),
so the cost is ~2 s on the Gitea surfaces of every cold load (PR rows, banners). The fix is in the
broker (`zeropsio/gitea-mate`), outside zcp and the Mate client.

**Trigger to promote**: a Gitea surface's first paint budget under ~3 s; any report that PR rows
"appear late" on load.

## What happens
- Chrome reports `No 'Access-Control-Allow-Origin' header is present on the requested resource` — a
  real HTTP answer that skipped the broker's CORS handling, not a failed preflight (that reads
  "Response to preflight request doesn't pass access control check").
- Not a cold or unreachable broker: the POST fails after 200–300 ms, and the broker's own liveness
  `GET /` answers 404 about 1.7 s later.
- Not the request: failing and succeeding attempts both mint their throwaway 43–94 ms before the POST.
- The browser hides the status, so the cause (an error path without CORS headers, or the L7's 502) is
  in the broker's logs or a curl replay with a throwaway.

## Sketch
- Broker: every response, error paths included, carries the CORS headers D22 promises; log the
  status and cause of a failed person-token request.
- Then find why the first request of a load fails (for example a throwaway not yet valid at the
  Zerops API when the broker checks it, which would call for a server-side short retry).

## Refs
- Related: `mate-registry-outlives-deleted-projects.md` (its Sketch names the 502 without CORS).
- Evidence (local, not committed): the mate repo's `.plans/mate-state-model/evidence/gate-G/s2-r*/`
  console and network logs.
