# Retest: Tatami welcome autostart

Target: project `localflow`, service `zcp`, bootstrap extension `0.5.1`
from commit `3e8c54b8`.

## Happy path

1. Close the existing code-server tab, then open the `zcp` editor again from
   Tatami/febridge (or perform a full Reload Window).
2. Confirm the UX v5 Zerops welcome is the first editor surface.
3. Confirm `ZCP Launcher` did not open and the primary sidebar/Explorer is
   hidden.
4. Complete an action that writes the watched zembed env (for example an
   agent authorization update).
5. Confirm the legacy launcher does not reappear over the welcome.

## Regression boundary

- This instance is custom-GUI-configured by its existing
  `ZCP_WELCOME_BRIDGE_ORIGINS`; the implementation does not inspect the
  browser's current parent origin.
- No live default-mode check should clear or change that env. The absent,
  empty, invalid and `https://app.zerops.io` fallback cases are covered by
  the focused startup test.

## Rollback

Revert `3e8c54b8` on `feat/welcome-ux-v2`, rebuild/copy that binary to the
test service and run `zcp init` again. Reload the code-server window after
the rollback so its extension host uses the restored bootstrap version.
