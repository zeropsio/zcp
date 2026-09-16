// Tests for: ops/gitea_workflow_file.go — writing a file into a pair's
// working tree, only when it differs from what is there.
package ops

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBuildWriteRepoFileCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("runs a real shell")
	}
	const body = "name: Zerops deploy\non:\n  push:\n    branches: [main]\n"

	t.Run("writes the file, creating the directories it needs", func(t *testing.T) {
		dir := t.TempDir()
		runShell(t, BuildWriteRepoFileCommand(dir, ".gitea/workflows/zerops.yml", body))

		got, err := os.ReadFile(filepath.Join(dir, ".gitea/workflows/zerops.yml"))
		if err != nil {
			t.Fatalf("read the written file: %v", err)
		}
		if string(got) != body {
			t.Errorf("content = %q, want %q", got, body)
		}
	})

	t.Run("identical content is not rewritten", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, ".gitea/workflows/zerops.yml")
		cmd := BuildWriteRepoFileCommand(dir, ".gitea/workflows/zerops.yml", body)
		runShell(t, cmd)

		before, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		// Make any rewrite unmistakable: a fresh write cannot reproduce this.
		stale := before.ModTime().Add(-time.Hour)
		if err := os.Chtimes(path, stale, stale); err != nil {
			t.Fatalf("chtimes: %v", err)
		}
		runShell(t, cmd)

		after, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat after: %v", err)
		}
		if !after.ModTime().Equal(stale) {
			t.Errorf("an identical body was rewritten (mtime moved to %v)", after.ModTime())
		}
		// And no scratch file is left beside it.
		entries, err := os.ReadDir(filepath.Dir(path))
		if err != nil {
			t.Fatalf("readdir: %v", err)
		}
		if len(entries) != 1 {
			t.Errorf("the write left something behind: %v", entries)
		}
	})

	t.Run("changed content replaces what is there", func(t *testing.T) {
		dir := t.TempDir()
		runShell(t, BuildWriteRepoFileCommand(dir, ".gitea/workflows/zerops.yml", body))
		runShell(t, BuildWriteRepoFileCommand(dir, ".gitea/workflows/zerops.yml", body+"# newer\n"))

		got, err := os.ReadFile(filepath.Join(dir, ".gitea/workflows/zerops.yml"))
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if string(got) != body+"# newer\n" {
			t.Errorf("content = %q, want the newer body", got)
		}
	})

	t.Run("a body full of shell metacharacters survives verbatim", func(t *testing.T) {
		dir := t.TempDir()
		hostile := "it's $HOME `whoami` \"quoted\" \\ && rm -rf /\n"
		runShell(t, BuildWriteRepoFileCommand(dir, "a/b.txt", hostile))

		got, err := os.ReadFile(filepath.Join(dir, "a/b.txt"))
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if string(got) != hostile {
			t.Errorf("content = %q, want %q", got, hostile)
		}
	})
}
