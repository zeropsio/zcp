package init

import (
	"fmt"
	"os"

	"github.com/zeropsio/zcp/internal/init/adapters"
	"github.com/zeropsio/zcp/internal/runtime"
)

// InstallVSCodeStudio materializes the Zerops Studio (Managed Data) VS Code
// extension (the `zcp init --vscode` action), dispatching on the runtime host:
//
//   - Inside a Zerops container the editor is code-server, so it installs into
//     code-server's extensions dir (~/.local/share/code-server/extensions/) with
//     the code-server entry shape — the same target the container-boot init path
//     lands via configureVSCode. Both are idempotent, so the explicit flag and
//     the automatic boot install coexist safely.
//   - Locally the target is stock desktop VS Code's ~/.vscode/extensions/.
//
// The desktop and container installers write the identical extension tree; they
// differ only in the target dir and the extensions.json entry shape each editor
// host expects.
func InstallVSCodeStudio(rt runtime.Info) error {
	if rt.InContainer {
		fmt.Fprintln(os.Stderr, "  → Zerops Studio (code-server extension, in-container)")
		if err := adapters.InstallStudioExtensionContainer(runtime.HomeDir()); err != nil {
			return fmt.Errorf("install Zerops Studio extension (container): %w", err)
		}
		return nil
	}
	fmt.Fprintln(os.Stderr, "  → Zerops Studio (VS Code extension)")
	if err := adapters.InstallStudioExtension(runtime.HomeDir()); err != nil {
		return fmt.Errorf("install Zerops Studio extension: %w", err)
	}
	return nil
}
