# verify's HTTP probes should hit the declared healthCheck path, not `/`

**Surfaced**: 2026-09-13 — farm batch `pa-1`, run `internal-only-worker` (observer,
medium). The worker exposes an HTTP port only for `/healthz`; `zerops_verify`
classified it as an HTTP runtime, `http_internal` probed `GET /`, got 404 and
reported `degraded` with the hedge "root path not serving a 2xx/3xx — verify a real
endpoint or accept as cosmetic". The agent had to judge the failure cosmetic by hand
and force-close the session.

**Why deferred**: the probe path is unchanged behaviour (the old `http_root` probed `/`
too); the public-access split only made it visible on a worker that now gets an
internal probe. Fixing it means reading the deployed setup's
`run.healthCheck.httpGet.path` (the recorded setup verify already classifies from)
and using it for `http_internal`/`http_public`, with `/` as the fallback — a small,
separate change with its own table test.

**Trigger to promote**: a second farm run where a service with a declared healthCheck
path is graded `degraded` on `/`; or any owner request to make verify's verdicts
final (no "accept as cosmetic" hedge).

## Sketch
- `ops.verify`: resolve `probePath` = deployed setup `run.healthCheck.httpGet.path`
  if present, else `/`; use it in both probes and name it in the check `Detail`.
- Drop the "accept as cosmetic" hedge once the declared path is probed: a 404 on the
  declared health path is a real failure.
- Test: `TestVerify_HTTPProbes_UseDeclaredHealthCheckPath_Table` (declared path ⇒
  probed; none ⇒ `/`).
