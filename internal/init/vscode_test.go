package init

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/zeropsio/zcp/internal/runtime"
)

// TestInstallVSCodeStudio_ContainerIsNoOp pins the local/container gate: inside
// a Zerops container the editor is code-server (the bootstrap extension owns it)
// so `zcp init --vscode` does nothing and creates no desktop ~/.vscode dir.
// Non-parallel: mutates the HOME env (resolveHome reads it).
func TestInstallVSCodeStudio_ContainerIsNoOp(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := InstallVSCodeStudio(runtime.Info{InContainer: true}); err != nil {
		t.Fatalf("container no-op should not error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".vscode")); !os.IsNotExist(err) {
		t.Errorf("container mode must not create ~/.vscode (got err=%v)", err)
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
