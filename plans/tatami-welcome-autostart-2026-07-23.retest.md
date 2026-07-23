# Retest: Tatami welcome autostart

Target: project `localflow`, service `zcp`, bootstrap extension `0.1.14`
from current-main integration commit `ca4ae50d`.

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

- This instance is custom-GUI-configured by its existing direct runtime
  export `febridge_ZGUI_DATA_APP_URL`; the implementation does not inspect
  the browser's current parent origin.
- `ZCP_WELCOME_BRIDGE_ORIGINS` remains only the auth-bridge trust allowlist
  and no longer controls presentation.
- No live default-mode check should clear or change any env. Missing,
  invalid, app-only, build-snapshot-only, bridge-only and own-subdomain-only
  fallback cases are covered by the focused startup test.

## Rollback

Build/copy exact pre-change current-main commit `d0be6787` to the test
service and run `zcp init` again. Reload the code-server window after the
rollback so its extension host switches back to bootstrap 0.1.13.
