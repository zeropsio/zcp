package init

import (
	"fmt"
	"os"

	"github.com/zeropsio/zcp/internal/init/adapters"
	"github.com/zeropsio/zcp/internal/runtime"
)

// InstallVSCodeStudio materializes the Zerops Studio desktop VS Code extension
// (the `zcp init --vscode` action). It is the LOCAL-mode product: inside a
// Zerops container the editor is code-server and the bootstrap extension owns
// that surface, so this is a no-op there with a notice. The gate mirrors the
// local/container split in Run (MCP config is local-only) — the desktop
// extensions dir (~/.vscode/extensions) is a local concept.
func InstallVSCodeStudio(rt runtime.Info) error {
	if rt.InContainer {
		fmt.Fprintln(os.Stderr, "  → zcp init --vscode is a local-desktop flag; skipping inside a Zerops container")
		return nil
	}
	fmt.Fprintln(os.Stderr, "  → Zerops Studio (VS Code extension)")
	if err := adapters.InstallStudioExtension(resolveHome()); err != nil {
		return fmt.Errorf("install Zerops Studio extension: %w", err)
	}
	return nil
}
