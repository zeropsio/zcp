package tools

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/zeropsio/zcp/internal/workflow"
)

const mountStatusMounted = "MOUNTED"

// cleanupImportYAML removes import.yaml (or import.yml) from the project root
// after the provision step. If mount paths are available, copies the file there
// first for provenance. Best-effort: errors logged to stderr, never fatal.
//
// stateDir is expected to be {projectRoot}/.zcp/state/.
func cleanupImportYAML(stateDir string, mounts []workflow.AutoMountInfo) {
	projectRoot := filepath.Dir(filepath.Dir(stateDir))

	fileNames := []string{"import.yaml", "import.yml"}
	var found string
	var content []byte
	for _, name := range fileNames {
		candidate := filepath.Join(projectRoot, name)
		data, err := os.ReadFile(candidate)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "zcp: read import file for cleanup: %v\n", err)
			// Still try to delete even if read fails.
			if removeErr := os.Remove(candidate); removeErr != nil {
				fmt.Fprintf(os.Stderr, "zcp: remove import file: %v\n", removeErr)
			}
			return
		}
		found = candidate
		content = data
		break
	}
	if found == "" {
		return
	}

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

	if err := os.Remove(found); err != nil {
		fmt.Fprintf(os.Stderr, "zcp: remove import file: %v\n", err)
	}
}
