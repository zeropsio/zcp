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

**Update 2026-09-24**: cause found in the KRLS broker's own log (2.7 h window): `POST /person/token`
answered 200 ×148 and **502 ×156**. Every 502 follows `ERROR "the caller's rights could not be read":
the org could not be read: the member list: zerops api: 400` or `"the throwaway could not be checked":
reading the org's members: zerops api: 400` (`GET /client/{id}/user/list`), i.e. the broker checks the
caller's freshly minted throwaway token against the org member list within milliseconds and Zerops
answers 400 — the token is evidently not usable yet; the same request 2 s later passes. Sketch: the
broker retries that member-list read on a short ladder (e.g. 250 ms, 500 ms, 1 s) before failing, and
its 502 carries the CORS headers. Also seen once: at 22:10:12Z every registered Mate's services read
answered 401 for one pass (the broker's own token), then recovered.
