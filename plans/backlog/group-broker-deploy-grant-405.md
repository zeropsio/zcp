# The deployed group broker answers `POST /deploy/grant` with 405, so every deploy job fails

**Surfaced**: 2026-09-23 — the Mate client state-model programme's flow fixture in the test org KRLS
(group `sm-fixture`, repo `sm-fixture/app`, stage `EDJS9HVFTFiVdwViTgpxeg`). Every push to `main`
started the repository's deploy@v4 workflow on the group's runner, and the job went red on
`POST /deploy/grant` → 405. The stage was still deployed, by the broker's own deploy pass (source CLI,
within one 5-minute pass), so nothing looked broken from the Mate client.

**Why deferred**: the fixture needed a deployed stage, and the broker's own pass supplies one. The fix
is a broker release, outside the Mate client programme.

**Trigger to promote**: shipping D27 as the deploy path (a job deploys with `zcli push`); any user
looking at a group repository's Actions tab, which is red on every push to `main`.

## What happens
- spec-mate D27 and §10.8 make the job the deployer: it asks `POST /deploy/grant` and the broker hands
  it the environment's deploy token. The broker deployed in KRLS answers that route with 405 Method
  Not Allowed, so it predates D27 (or mounts the route under another method or path).
- The job fails before `zcli push`. The broker's catch-up deploy pass then deploys the same commit,
  so the environment ends up right, but up to one pass later and with a red job for every push.
- Two deployers exist side by side: the job D27 describes and the broker pass D27 supersedes.

## Sketch
- Release the broker with the D27 grant route and redeploy it in existing orgs.
- Once the job deploys, the broker's own upload path goes, per D27.
- A readiness check on the broker service could assert that the grant route exists (a 405 there is a
  version skew, not a refusal).

## Refs
- spec-mate D27, §10.8 (`POST /deploy/grant`, `POST /deploy/{id}/result`).
- Fixture record (local, not committed): the mate repo's `.plans/mate-state-model/e2e/fixtures.json`
  (`flow` note).
- Related: `mate-registry-outlives-deleted-projects.md` (same broker, same org).
