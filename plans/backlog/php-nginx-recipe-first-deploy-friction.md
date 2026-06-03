# php-nginx recipe first-deploy friction (flow-eval findings)

**Surfaced**: 2026-06-03, `flow-eval recipe-laravel-minimal-standard` (run
20260603-075230) during P0c diverse-scenario verification. The run COMPLETED the
full bootstrap→develop→deploy→promote flow (no P0c regression — these are
pre-existing guidance/platform gaps, captured here so they're not lost). Cohesive
cluster: all are php-nginx + recipe + first-deploy friction; split if promoted.

**Why deferred**: out of P0c scope (P0c is develop-guidance de-bloat). Each needs
platform verification or recipe-side work, not an atom trim. The one quick win
(sudo applies to `build.prepareCommands`) was fixed in P0c round 1b chunk 3 — the
rest remain.

**Trigger to promote**: php/Laravel recipe usage grows, or a platform-verification
pass is being run anyway (some of these need live confirmation, not a guess).

## Findings

1. **OS string vs actual distro mismatch (biggest trap).** Service type came back
   `ubuntu/php-nginx@8.4` but the container was Alpine 3.23 (`/sbin/apk`,
   `/etc/os-release`). Agent wrote Debian-style `apt-get` `prepareCommands`, failed,
   only found out by SSH. **Needs platform verification**: is php-nginx (and which
   other bases) always Alpine regardless of the `ubuntu/` composite prefix? If a
   stable rule exists, encode it; if not, the generic "check `/etc/os-release`
   first" line added to `develop-platform-rules-common` (round 1b chunk 3) is the
   safe floor. DO NOT guess the rule — verify against real services / `../zerops-docs`.

2. **Asset-pipeline guidance assumes npm on the php-nginx runtime.**
   `develop-first-deploy-asset-pipeline-container` says `ssh appdev 'cd /var/www &&
   npm run build'`, but `npm` isn't on a php-nginx runtime container — agent had to
   `sudo apk add --no-cache nodejs npm` first. Recipe/runtime-specific (Laravel+Vite):
   per CLAUDE.md "recipe-specific findings go in recipes", the Laravel recipe md
   should carry the `apk add nodejs npm` precondition; the atom could add a generic
   "ensure the build toolchain exists on the runtime before running it" caveat.

3. **DIAGNOSIS_REQUIRED double-reimport.** After a failed prod build (the sudo
   issue), redeploy was refused with `DIAGNOSIS_REQUIRED` → reimport with
   `override:true` + `startWithoutCode:true`. Agent missed `startWithoutCode` on the
   first reimport (did override only), landing appstage in READY_TO_DEPLOY, and the
   next deploy was refused again — a SECOND reimport with `startWithoutCode:true` was
   needed. The `recovery.args` named both; the UX cost is that skipping one field
   silently re-loops. Possible fix: the recovery response / gate could reject a
   reimport that carries `override` without `startWithoutCode` when the target is
   mid-failed, naming the missing field. Relates to the diagnose-before-destruct gate.

4. **deployFiles `[.]` for cross-deploy reads as ambiguous.** Guidance says
   cross-deploys "cherry-pick build output" but never says `[.]` is also valid for a
   cross-deploy; the self-deploy-destruction warning (narrower-than-`[.]` destroys
   target) sits in the same section, so an agent may over-rotate on cherry-picking
   for the prod setup when `[.]` is fine. `develop-deploy-modes` / the self-deploy-
   destruction section could separate "self-deploy MUST be `[.]`" from "cross-deploy
   MAY narrow" more cleanly. (P0c round 1a/1b already trimmed deploy-modes; a future
   restructure could clarify this.)

## Refs
- Retrospective: `eval/behavioral/runs/20260603-075230/recipe-laravel-minimal-standard/self-review.md`
- Fixed in P0c (round 1b chunk 3): sudo applies to `build.prepareCommands` +
  check-os-release floor, in `develop-platform-rules-common`.
- Sibling: `laravel-recipe-app-key-syslog.md` (other Laravel recipe gotchas).
