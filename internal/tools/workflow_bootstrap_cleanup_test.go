// Tests for: workflow_bootstrap_cleanup.go — import.yaml cleanup after provision.

package tools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/zeropsio/zcp/internal/workflow"
)

func TestCleanupImportYAML(t *testing.T) {
	t.Parallel()

	const importContent = "services:\n  - hostname: weather\n    type: nodejs@22\n"

	tests := []struct {
		name          string
		createFile    bool   // whether to create import.yaml at root
		fileName      string // file name (import.yaml or import.yml)
		isContainer   bool   // container vs local mode
		mounts        []workflow.AutoMountInfo
		wantDeleted   bool     // file should be removed from root
		wantCopiedTo  []string // mount subdirs that should contain the file
		wantNotCopied []string // mount subdirs that should NOT contain the file
	}{
		{
			name:        "container: copies to mount and deletes from root",
			createFile:  true,
			fileName:    "import.yaml",
			isContainer: true,
			mounts: []workflow.AutoMountInfo{
				{Hostname: "weather", MountPath: "", Status: mountStatusMounted},
			},
			wantDeleted:  true,
			wantCopiedTo: []string{"weather"},
		},
		{
			name:        "container: handles import.yml legacy extension",
			createFile:  true,
			fileName:    "import.yml",
			isContainer: true,
			mounts: []workflow.AutoMountInfo{
				{Hostname: "app", MountPath: "", Status: mountStatusMounted},
			},
			wantDeleted:  true,
			wantCopiedTo: []string{"app"},
		},
		{
			name:        "no file at root — noop",
			createFile:  false,
			fileName:    "import.yaml",
			isContainer: true,
			mounts: []workflow.AutoMountInfo{
				{Hostname: "weather", MountPath: "", Status: mountStatusMounted},
			},
			wantDeleted: false,
		},
		{
			name:        "container: no mounts — keeps file at root",
			createFile:  true,
			fileName:    "import.yaml",
			isContainer: true,
			mounts:      nil,
			wantDeleted: false,
		},
		{
			name:        "container: skips failed mounts, copies to good ones",
			createFile:  true,
			fileName:    "import.yaml",
			isContainer: true,
			mounts: []workflow.AutoMountInfo{
				{Hostname: "ok", MountPath: "", Status: mountStatusMounted},
				{Hostname: "bad", Status: "FAILED", Error: "mount failed"},
			},
			wantDeleted:   true,
			wantCopiedTo:  []string{"ok"},
			wantNotCopied: []string{"bad"},
		},
		{
			name:        "container: multiple successful mounts get copies",
			createFile:  true,
			fileName:    "import.yaml",
			isContainer: true,
			mounts: []workflow.AutoMountInfo{
				{Hostname: "api", MountPath: "", Status: mountStatusMounted},
				{Hostname: "web", MountPath: "", Status: mountStatusMounted},
			},
			wantDeleted:  true,
			wantCopiedTo: []string{"api", "web"},
		},
		{
			name:        "container: all mount copies fail — keeps file at root",
			createFile:  true,
			fileName:    "import.yaml",
			isContainer: true,
			mounts: []workflow.AutoMountInfo{
				{Hostname: "broken", MountPath: "/nonexistent/path/broken", Status: mountStatusMounted},
			},
			wantDeleted: false,
		},
		{
			name:        "local: noop — keeps file at root",
			createFile:  true,
			fileName:    "import.yaml",
			isContainer: false,
			mounts:      nil,
			wantDeleted: false,
		},
		{
			name:        "local: noop even with mounts available",
			createFile:  true,
			fileName:    "import.yaml",
			isContainer: false,
			mounts: []workflow.AutoMountInfo{
				{Hostname: "app", MountPath: "", Status: mountStatusMounted},
			},
			wantDeleted: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Set up project root with .zcp/state/ structure.
			root := t.TempDir()
			stateDir := filepath.Join(root, ".zcp", "state")
			if err := os.MkdirAll(stateDir, 0o755); err != nil {
				t.Fatal(err)
			}

			// Create import file if requested.
			importPath := filepath.Join(root, tt.fileName)
			if tt.createFile {
				if err := os.WriteFile(importPath, []byte(importContent), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			// Set up mount paths: /var/www/{hostname}/ simulated in temp dir.
			mountBase := filepath.Join(root, "var", "www")
			for i := range tt.mounts {
				if tt.mounts[i].Status == mountStatusMounted && tt.mounts[i].MountPath == "" {
					mountDir := filepath.Join(mountBase, tt.mounts[i].Hostname)
					if err := os.MkdirAll(mountDir, 0o755); err != nil {
						t.Fatal(err)
					}
					tt.mounts[i].MountPath = mountDir
				}
			}

			// Execute.
			cleanupImportYAML(stateDir, tt.mounts, tt.isContainer)

			// Verify deletion from root.
			_, err := os.Stat(importPath)
			if tt.wantDeleted {
				if err == nil {
					t.Errorf("import file should have been deleted from root, but still exists")
				}
			} else if tt.createFile && err != nil {
				t.Errorf("import file should still exist at root, but got: %v", err)
			}

			// Verify no provenance in state dir (import.yaml belongs on service, not in ZCP state).
			provenancePath := filepath.Join(stateDir, "import-provenance.yaml")
			if _, provenanceErr := os.Stat(provenancePath); provenanceErr == nil {
				t.Errorf("provenance file should NOT exist in state dir, but found at %s", provenancePath)
			}

			// Verify copies to mount paths.
			for _, hostname := range tt.wantCopiedTo {
				dest := filepath.Join(mountBase, hostname, tt.fileName)
				data, err := os.ReadFile(dest)
				if err != nil {
					t.Errorf("expected copy at %s, got: %v", dest, err)
					continue
				}
				if string(data) != importContent {
					t.Errorf("copy at %s has wrong content: %q", dest, string(data))
				}
			}
			for _, hostname := range tt.wantNotCopied {
				dest := filepath.Join(mountBase, hostname, tt.fileName)
				if _, err := os.Stat(dest); err == nil {
					t.Errorf("should NOT have copied to %s, but file exists", dest)
				}
			}
		})
	}
}
