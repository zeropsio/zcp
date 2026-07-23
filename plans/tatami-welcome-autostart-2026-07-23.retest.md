# Retest: Tatami welcome autostart

Target: project `localflow`, service `zcp`, bootstrap extension `0.1.15`
from S4 commit `80c0e969`.

## Happy path

1. Close the existing code-server tab, then open the `zcp` editor again from
   Tatami/febridge (or perform a full Reload Window).
2. Confirm the advanced current-main welcome (the same content previously
   installed as 0.1.13) is the first editor surface.
3. Confirm `ZCP Launcher` did not open and the primary sidebar/Explorer is
   hidden.
4. Complete an action that writes the watched zembed env (for example an
   agent authorization update).
5. Confirm the legacy launcher does not reappear over the welcome.

## Regression boundary

- This instance was configured during `zcp init` from its existing system env
  `zeropsSubdomain=https://zcp-24cb-8080.prg1.zerops.app`; its installed
  `startup.json` contains `{"autoOpenWelcome":true}`.
- `ZCP_WELCOME_BRIDGE_ORIGINS` remains only the auth-bridge trust allowlist
  and no longer controls presentation.
- `ZGUI_DATA_APP_URL` no longer controls presentation.
- No live default-mode check should clear or change any env. Missing, invalid,
  non-HTTP(S), app-host, malformed-policy and missing-policy fallback cases
  are covered by the Go/Node startup tests.

## Rollback

Install released `v9.133.2` on the test service and run `zcp init` again.
Reload the code-server window after the rollback so its extension host
switches back to bootstrap 0.1.14.
