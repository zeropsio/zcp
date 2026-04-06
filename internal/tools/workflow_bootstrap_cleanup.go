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

// cleanupImportYAML stores import.yaml provenance and cleans up after provision.
//
// In all environments:
//   - Stores a durable provenance copy in stateDir (import-provenance.yaml).
//
// In container mode (isContainer=true):
//   - Best-effort copies to each mounted service path for convenience.
//   - Deletes the original from project root (provenance is in state dir).
//
// In local mode (isContainer=false):
//   - Leaves the original at project root (user may need it).
//
// stateDir is expected to be {projectRoot}/.zcp/state/.
func cleanupImportYAML(stateDir string, mounts []workflow.AutoMountInfo, isContainer bool) {
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
			if isContainer {
				if removeErr := os.Remove(candidate); removeErr != nil {
					fmt.Fprintf(os.Stderr, "zcp: remove import file: %v\n", removeErr)
				}
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

	// Step 1: Store provenance in state dir (all environments).
	provenancePath := filepath.Join(stateDir, "import-provenance.yaml")
	if writeErr := os.WriteFile(provenancePath, content, 0o600); writeErr != nil {
		fmt.Fprintf(os.Stderr, "zcp: store import provenance at %s: %v\n", provenancePath, writeErr)
		// Provenance storage failed — do NOT delete the source file.
		return
	}

	if !isContainer {
		return
	}

	// Step 2 (container only): Best-effort copy to mount paths.
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

	// Step 3 (container only): Delete from project root.
	if err := os.Remove(found); err != nil {
		fmt.Fprintf(os.Stderr, "zcp: remove import file: %v\n", err)
	}
}
