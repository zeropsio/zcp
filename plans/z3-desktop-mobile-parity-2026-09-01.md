# z3 desktop + mobile parity — implementation plan

Status: COMPLETE 2026-09-01. Base: `../z3` `1dae9edba` (`v0.1.7`).

Owner approval: the owner explicitly asked to research, implement the desktop app, continue directly to the connected iPhone, and use parallel agents. This plan narrows that request to the already-shaped `docs/spec-z3.md §9` / S5-3 path and records the implementation and verification boundary.

## Outcome

Zerops Code has one recognizable product identity across web, desktop, installer, and mobile. The Electron client remains the existing hosted web shell. The mobile client gains the same Zerops account entry path and project/environment picker, while the current pairing flow remains available as a fallback.

## Constraints

- Preserve the hard-fork boundary. Never merge or rebase upstream.
- Preserve internal compatibility identifiers (`t3code://`, package names, app-support paths, bundle IDs) unless a concrete runtime defect requires a separate migration.
- Do not touch the local live database.
- Use the existing shared Zerops client-runtime modules; do not fork protocol or candidate logic into mobile.
- Keep Clerk-backed notification/relay code operational until the relay replacement is implemented in its own slice.
- Existing uncommitted zcp spec/plan files are the owner's baseline and remain untouched.

## Slices

### S1 — installation and native branding

- Replace visible T3 wordmarks/marks in mobile headers, widget assets, desktop DMG art, and generated app-icon sources with the canonical Zerops mark.
- Keep the current production/development/nightly background treatments so build channels remain distinguishable.
- Rename user-visible desktop artifacts from `T3-Code-*` to `Zerops-Code-*`; retain internal IDs.
- Update source-of-truth asset documentation and focused tests.

Acceptance: no user-visible T3 mark or product name remains on the desktop/mobile launch and install surfaces; focused brand/export tests pass.

### S2 — native mobile Zerops entry

- Add a SecureStore-backed adapter and native Zerops session provider outside the existing cloud/relay provider.
- Add a native Zerops sign-in flow including TOTP and session restore.
- Add a native project/container picker using `packages/client-runtime/src/zerops` candidate semantics.
- Add the mobile `registerZeropsIdentity` atom command and connect ready containers directly.
- Route the primary add/connect actions and Zerops Account setting to this flow.
- Keep the one-time-link / QR pairing screen as an explicit fallback.

Acceptance: on the connected iPhone, a user can sign in with a Zerops account, see eligible environments, connect one without typing a pairing code, and reach the thread UI. Existing pairing remains reachable.

### S3 — build and visual verification

- Run focused tests and package typechecks for desktop, mobile, shared runtime, and changed scripts.
- Build the desktop shell and inspect its first-run/sign-in surfaces.
- Build, install, and launch `cz.krls.z3` on the connected iPhone; inspect the real native UI where tooling permits.
- Record larger defects (OAuth callback migration, relay/Clerk removal, push, provisioning/new-project depth) in the existing z3 backlog rather than expanding this pass silently.

## Known research findings

- Desktop is already the intended Electron shell over the staged hosted web bundle; it does not need a native UI rewrite.
- Desktop custom-protocol registration exists, but OAuth return handling is not implemented. Password/TOTP sign-in is in scope now; a robust native OAuth callback is backlog unless it blocks acceptance.
- Mobile already has the shared client-runtime foundation but still exposes T3 artwork and only the legacy pairing entry.
- The connected iPhone has `cz.krls.z3` 1.0.4 installed and is available through `devicectl`.

## Explicit follow-up backlog

- Implement a real Electron deep-link / callback contract for Zerops browser handover. The desktop showcase now honestly leads with password/TOTP and does not expose the cross-origin flow that cannot return its nonce.
- Add mobile project creation / pool claim, `Enable Zerops Code`, restart and provisioning-wait actions. This pass probes health and refuses to call an unverified container `Ready`, but deliberately leaves operator-required candidates non-actionable.
- Replace the remaining Clerk-backed relay/push plumbing and retarget the upstream EAS owner/project before a store release. The Zerops account session itself is already independent and Keychain-backed.
- Regenerate Windows, Linux and packaged-web icon outputs with Icon Composer 2.x. iOS consumes the canonical `.icon` source directly and macOS now has a reproducible `actool` export; the other generated outputs remain pinned to the unavailable exporter.
- Install/pin the Rust toolchain required by `native/resource-monitor` before producing a full DMG. The desktop hosted-static web and Electron shell build and run locally, but the final artifact builder correctly refuses to omit that native binary.

## RED / GREEN register

| Slice | RED proof | GREEN proof |
|---|---|---|
| S1 | focused tests/source audit reject the remaining visible T3 assets/names | asset exports regenerate and focused branding tests pass |
| S2 | mobile tests prove the missing storage/session/onboarding/picker states | focused tests + mobile typecheck pass |
| S3 | installed baseline demonstrates old branding/pairing-only entry | desktop build + iPhone install/launch and visual acceptance |

## Verification result

- Desktop hosted-static staging, Electron build and real smoke capture passed. The app renders the
  current Zerops web shell and uses the supported password/TOTP entry on Electron.
- The signed Release mobile build installed and launched on the connected iPhone. The stable native
  title slot renders the live connection state on iOS 26; the brand occupies that same slot once the
  environment is connected. The iPad sidebar preserves its separate editor-style alignment.
- Final focused battery: 18 files / 177 tests passed. Mobile, desktop, web, client-runtime and script
  package typechecks passed during the slice. Focused lint/format checks and `git diff --check` passed.
- macOS icon source verification passed for all three variants.
