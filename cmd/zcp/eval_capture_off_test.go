package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/zeropsio/zcp/internal/capture"
)

func TestActiveEvalCapture_OffDoesNotCreateManagerState(t *testing.T) {
	// non-parallel: t.Setenv mutates process environment.
	root := t.TempDir()
	home := filepath.Join(root, "home")
	temp := filepath.Join(root, "tmp")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("create home: %v", err)
	}
	if err := os.MkdirAll(temp, 0o700); err != nil {
		t.Fatalf("create temp: %v", err)
	}
	t.Setenv("HOME", home)
	t.Setenv("TMPDIR", temp)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "claude"))
	for _, key := range []string{capture.EnvSessionID, capture.EnvSessionDir, capture.EnvControlSocket, capture.EnvControlToken} {
		t.Setenv(key, "")
	}

	connection, err := activeEvalCapture(context.Background())
	if err != nil {
		t.Fatalf("activeEvalCapture() error = %v", err)
	}
	if connection != nil {
		t.Fatal("activeEvalCapture() returned an unexpected connection")
	}

	var created []string
	for _, base := range []string{home, temp} {
		walkErr := filepath.WalkDir(base, func(path string, _ os.DirEntry, walkErr error) error {
			if walkErr == nil && path != base {
				relative, _ := filepath.Rel(root, path)
				created = append(created, relative)
			}
			return walkErr
		})
		if walkErr != nil {
			t.Fatalf("walk %s: %v", base, walkErr)
		}
	}
	if len(created) != 0 {
		t.Fatalf("capture-off discovery created filesystem state: %v", created)
	}
}
