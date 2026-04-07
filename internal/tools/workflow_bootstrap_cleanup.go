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

// cleanupImportYAML copies import.yaml to each mounted service and removes it
// from the project root. The import.yaml belongs with the service — it persists
// across deploys (via deployFiles: [.]) and is needed for repo creation / buildFromGit.
//
// In container mode (isContainer=true):
//   - Copies to each mounted service path.
//   - Deletes from project root only if at least one copy succeeded.
//
// In local mode (isContainer=false):
//   - No-op. File stays at project root for the user.
//
// stateDir is expected to be {projectRoot}/.zcp/state/.
func cleanupImportYAML(stateDir string, mounts []workflow.AutoMountInfo, isContainer bool) {
	if !isContainer {
		return
	}

	projectRoot := projectRootFromState(stateDir)

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
			return
		}
		found = candidate
		content = data
		break
	}
	if found == "" {
		return
	}

	// Copy to each mounted service. Mount write-readiness is guaranteed by
	// the write probe in SystemMounter.waitForReady.
	copied := 0
	fileName := filepath.Base(found)
	for _, m := range mounts {
		if m.MountPath == "" || m.Status != mountStatusMounted {
			continue
		}
		dest := filepath.Join(m.MountPath, fileName)
		if writeErr := os.WriteFile(dest, content, 0o600); writeErr != nil {
			fmt.Fprintf(os.Stderr, "zcp: copy import file to %s: %v\n", dest, writeErr)
			continue
		}
		copied++
	}

	// Delete from project root only if at least one service has the file.
	if copied > 0 {
		if err := os.Remove(found); err != nil {
			fmt.Fprintf(os.Stderr, "zcp: remove import file: %v\n", err)
		}
	}
}
