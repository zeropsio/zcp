package tools

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/zeropsio/zcp/internal/workflow"
)

const mountStatusMounted = "MOUNTED"

// importFileNames are the file names to look for at the project root.
var importFileNames = []string{"import.yaml", "import.yml"}

// cleanupImportYAML removes import.yaml (or import.yml) from the project root
// after the provision step. If mount paths are available, copies the file there
// first for provenance. Best-effort: errors logged to stderr, never fatal.
//
// stateDir is expected to be {projectRoot}/.zcp/state/.
func cleanupImportYAML(stateDir string, mounts []workflow.AutoMountInfo) {
	projectRoot := filepath.Dir(filepath.Dir(stateDir))

	var found string
	for _, name := range importFileNames {
		candidate := filepath.Join(projectRoot, name)
		if _, err := os.Stat(candidate); err == nil {
			found = candidate
			break
		}
	}
	if found == "" {
		return
	}

	content, err := os.ReadFile(found)
	if err != nil {
		fmt.Fprintf(os.Stderr, "zcp: read import file for cleanup: %v\n", err)
		// Still try to delete even if read fails.
		if removeErr := os.Remove(found); removeErr != nil {
			fmt.Fprintf(os.Stderr, "zcp: remove import file: %v\n", removeErr)
		}
		return
	}

	// Copy to each successfully mounted runtime path.
	fileName := filepath.Base(found)
	for _, m := range mounts {
		if m.MountPath == "" || m.Status != mountStatusMounted {
			continue
		}
		dest := filepath.Join(m.MountPath, fileName)
		if writeErr := os.WriteFile(dest, content, 0o600); writeErr != nil {
			fmt.Fprintf(os.Stderr, "zcp: copy import file to %s: %v\n", dest, writeErr)
		}
	}

	// Delete from project root.
	if err := os.Remove(found); err != nil {
		fmt.Fprintf(os.Stderr, "zcp: remove import file: %v\n", err)
	}
}
