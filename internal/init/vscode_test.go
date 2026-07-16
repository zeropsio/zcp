package init

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/runtime"
)

// TestInstallVSCodeStudio_ContainerInstallsCodeServer pins the container
// dispatch: inside a Zerops container the editor is code-server, so
// `zcp init --vscode` installs into ~/.local/share/code-server/extensions/
// (code-server entry shape) and never touches the desktop ~/.vscode dir.
// Non-parallel: mutates the HOME env (resolveHome reads it).
func TestInstallVSCodeStudio_ContainerInstallsCodeServer(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := InstallVSCodeStudio(runtime.Info{InContainer: true}); err != nil {
		t.Fatalf("container install: %v", err)
	}

	// Materialized under code-server, not the desktop dir.
	matches, _ := filepath.Glob(filepath.Join(home, ".local", "share", "code-server", "extensions", "zerops.zcp-studio-*", "extension.js"))
	if len(matches) != 1 {
		t.Errorf("want the Studio extension.js under code-server extensions, got %v", matches)
	}
	if _, err := os.Stat(filepath.Join(home, ".vscode")); !os.IsNotExist(err) {
		t.Errorf("container mode must not create ~/.vscode (got err=%v)", err)
	}

	// Registered with the code-server entry shape (desktop shape lacks fsPath).
	raw, err := os.ReadFile(filepath.Join(home, ".local", "share", "code-server", "extensions", "extensions.json"))
	if err != nil {
		t.Fatalf("read code-server index: %v", err)
	}
	for _, want := range []string{"zerops.zcp-studio", "fsPath"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("code-server index missing %q; got: %s", want, raw)
		}
	}
}

// TestInstallVSCodeStudio_LocalInstalls proves the local path materializes the
// extension under the resolved home. Non-parallel: mutates the HOME env.
func TestInstallVSCodeStudio_LocalInstalls(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := InstallVSCodeStudio(runtime.Info{}); err != nil {
		t.Fatalf("local install: %v", err)
	}
	matches, _ := filepath.Glob(filepath.Join(home, ".vscode", "extensions", "zerops.zcp-studio-*", "extension.js"))
	if len(matches) != 1 {
		t.Errorf("want the Studio extension.js installed under ~/.vscode/extensions, got %v", matches)
	}
}
