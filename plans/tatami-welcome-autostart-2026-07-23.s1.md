# Slice brief: S1 — Custom-GUI welcome autostart

Self-contained: no other plan file is required to execute this.

**Outcome**: A container with at least one usable non-app origin in its
existing live `ZCP_WELCOME_BRIDGE_ORIGINS` opens the new singleton welcome
on bootstrap activation, closes Explorer idempotently, and never lets a
later env update reopen the legacy launcher. Default containers retain the
current dark/lazy launcher and restored-editor behavior.

**Allowed scope**
- Files: `internal/content/templates/vscode-bootstrap-extension.js`;
  `internal/content/templates/vscode-bootstrap-package.json`;
  `internal/content/welcomejs/welcome_dark.test.js`;
  `internal/init/adapters/claude.go`;
  `internal/init/adapters/launcher_test.go`
- Explicitly excluded: welcome HTML/host feature behavior; bridge trust/ACK
  semantics; frontend code; service env mutation; production release.

**Spec citations**: `docs/spec-welcome-mode.md §1` (W-ENTRY), §2
(W-INSTALL), invariant W3.

**RED test list**
- `custom GUI mode auto-opens welcome and closes the primary sidebar` —
  layer: unit (`welcome_dark.test.js`)
- `custom GUI mode ignores later env changes instead of reopening launcher`
  — layer: unit (`welcome_dark.test.js`)
- `default mode stays lazy and preserves launcher/restored-editor behavior`
  — layer: unit (`welcome_dark.test.js`)
- `TestBootstrapExtension_WelcomeLazyPins` / version parity pins — layer:
  unit (`launcher_test.go` and existing adapter tests)

**Protocol**: RED → GREEN → REFACTOR.
1. Write one named behavior test first and confirm failure:
   `node --test internal/content/welcomejs/welcome_dark.test.js`
2. Implement the smallest activation seam; repeat for the env-change and
   fallback cases, never batch-writing all behavior before a RED.
3. Bump both `BootstrapExtVersion` and the manifest version.
4. Run:
   `node --test internal/content/welcomejs/welcome_dark.test.js`
   `go test ./internal/init/adapters ./internal/init -short -count=1`
   `make lint-fast`

**Independent oracle**: expected startup panels, command IDs, and fallback
behavior come from `docs/spec-welcome-mode.md §1` and fixed literals
(`zeropsWelcome`, `zcpLauncher`, `workbench.action.closeSidebar`), never
from implementation-derived values.

**BUILD addendum**
- Never batch-write tests: RED → GREEN → REFACTOR one named test at a time.
- Independent oracle: expected values come from the spec §/a known-good
  literal, never recomputed the implementation's own way.
- Assert on the activation/command public seam, not a private helper.
- Named tests must report an observed RED and GREEN exit code.
- `make lint-fast` must be clean before completion.

**Stop conditions**: halt on scope drift, a material unknown, an
acceptance-criteria change, or a repeated unexplained check failure.

**Definition of Done**
- [ ] RED replay fails at the slice base and passes at slice head
- [ ] Named Node and Go tests pass with clean one-shot output
- [ ] `make lint-fast` clean
- [ ] No file outside Allowed scope touched
- [ ] Bootstrap manifest and Go version constant remain parity-pinned
