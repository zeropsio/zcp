# Subdomain auto-enable mis-claims success on dev-mode `zsc noop` runtimes

- **Surfaced**: 2026-05-17, flow-eval `cadence-multiservice-build-run2-replay` retrospective (suite `20260517-045943`). Agent set `enableSubdomainAccess: true` at import time, first deploy succeeded, but `zerops_verify` came back `degraded` with `subdomain access not enabled — service is not reachable via HTTP`. Recovery field correctly pointed at `zerops_subdomain action=enable`, which worked, but the develop guidance lied first.
- **Why deferred**: env-var audit closed; verify/subdomain interaction is a distinct subsystem. The current behavior is "annoying but recoverable" — verify gives a clean recovery path. Not blocking; visible in one retro out of three.
- **Trigger to promote**: another agent run where this round-trip costs >1 deploy cycle, OR a recipe-author run where the false-positive "subdomain on" message leads to user-facing 502 in their first verify. Adding a third retro that names this as confusing tips the scale.

## What the agent saw

The develop response guidance contains the claim:

> "On first-deploy success the response carries `subdomainAccessEnabled: true` and a `subdomainUrl` — no manual `zerops_subdomain` call is needed in the happy path."

This is true for runtime services that actually serve HTTP at start (the dev/standard/static cases the line was written for). It is FALSE for dev-mode runtimes whose `start:` is `zsc noop --silent` — the runtime container is idle, the platform doesn't see any HTTP listener, and subdomain L7 activation is deferred. The deploy reports success, the verify reports degraded, the agent has to call `zerops_subdomain action=enable` manually.

Agent's verbatim observation:

> "That was misleading for dev mode with `zsc noop`. The deploy succeeded but the platform didn't flip the subdomain on because nothing was actually serving HTTP yet. Just plan on running `zerops_subdomain action=enable` after starting the dev server, or expect the verify-then-recover round-trip."

## Sketch — candidate fixes (none committed)

1. **Guidance — qualify the auto-enable claim by runtime-class.** Edit the develop-active atom that carries the "happy path" line so dev-mode + `zsc noop` runtimes get a different sentence: *"subdomain L7 will flip after the dev server starts; expect `zerops_verify` to report `degraded` until then, or call `zerops_subdomain action=enable` once the dev server is listening."* Smallest fix; defensive only.
2. **Platform / deploy handler — defer subdomain auto-enable until first HTTP listener appears.** Currently `maybeAutoEnableSubdomain` in `internal/tools/deploy_subdomain.go` runs immediately after build. For `zsc noop` runtimes it should wait until the dev server is up. Bigger scope; touches deploy handler + needs a "HTTP listener detected" signal from the platform.
3. **Verify hint — short-circuit to "expected" when status=degraded AND service is dev-mode AND start is `zsc noop`.** Verify response would mark `subdomain not enabled` as `informational`, not `degraded`, so it doesn't fire the recovery flow at all. Cleanest UX but couples verify to runtime-state.

Option 1 is the cheapest first move; if the friction reappears, escalate to 2 or 3.

**Update 2026-05-20:** 2nd retro names this drift explicitly —
`develop-loop-after-bootstrap` (suite `20260520-161651`, after Phase 1-4
ship from plans/env-discover-three-changes-2026-05-20.md). Agent verbatim:
*"The subdomain situation was the one place the guidance actively misled
me. The develop atom says 'On first-deploy success the response carries
subdomainAccessEnabled: true and a subdomainUrl — no manual zerops_subdomain
call is needed in the happy path.' My deploy response had neither field.
I only discovered subdomain was off when zerops_verify returned http_root:
fail with 'subdomain access not enabled'. ... don't trust the 'happy path'
claim — plan on checking verify and following recovery actions."* This
is the 2nd-of-3 retros referenced in the original promote trigger; one
more eval retro flagging the same misclaim flips this to Option 1 + 2
combined.

## Risks

- Option 2 (deferred auto-enable) interacts with the F8 deferred-start path for worker services (`serviceStackIsNotHttp`) which is already handled at the deploy layer. Need to make sure the new branch doesn't double-up.
- Option 3 changes verify's semantics — anyone relying on `degraded` as "something I should care about" will see fewer alarms. Probably fine since the recovery field stays present.

## Refs

- `internal/tools/deploy_subdomain.go::maybeAutoEnableSubdomain` — current auto-enable site.
- `internal/ops/verify*.go` — verify check authoring; the `http_root` check is the one that fires `degraded` here.
- Retrospective: `eval/behavioral/runs/20260517-045943/cadence-multiservice-build-run2-replay/self-review.md` (item 4).
- Adjacent invariant from `CLAUDE.md`: *"Subdomain L7 activation is the deploy handler's concern, platform classifies"* — current claim already says ONLY platform-classified `serviceStackIsNotHttp` is the silent-skip exception. Dev-mode `zsc noop` would be a second exception with a similar shape.
