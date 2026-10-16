package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// shellSSH runs a container command with sh on this machine.
type shellSSH struct{}

func (shellSSH) ExecSSH(ctx context.Context, _, command string) ([]byte, error) {
	return exec.CommandContext(ctx, "sh", "-c", command).Output()
}

func (shellSSH) ExecSSHBackground(context.Context, string, string, time.Duration) ([]byte, error) {
	return nil, nil
}

// TestFetchZeropsYamlOverSSH_NamesTheFileItRead: a git-push reads the repo's
// config file — zerops.yaml first, else zerops.yml — and names the file it
// read, so its failure classification names that file; a repo with neither
// reads as no yaml.
func TestFetchZeropsYamlOverSSH_NamesTheFileItRead(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name               string
		files              []string
		wantFile, wantBody string
	}{
		{name: "zerops.yaml", files: []string{"zerops.yaml"}, wantFile: "zerops.yaml", wantBody: "zerops: # zerops.yaml"},
		{name: "zerops.yml", files: []string{"zerops.yml"}, wantFile: "zerops.yml", wantBody: "zerops: # zerops.yml"},
		{name: "both", files: []string{"zerops.yaml", "zerops.yml"}, wantFile: "zerops.yaml", wantBody: "zerops: # zerops.yaml"},
		{name: "neither"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			for _, f := range tc.files {
				if err := os.WriteFile(filepath.Join(dir, f), []byte("zerops: # "+f+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			body, file, err := fetchZeropsYamlOverSSH(context.Background(), shellSSH{}, "app", dir)
			if err != nil {
				t.Fatalf("fetch: %v", err)
			}
			if file != tc.wantFile || body != tc.wantBody {
				t.Errorf("fetch = (%q, %q), want (%q, %q)", body, file, tc.wantBody, tc.wantFile)
			}
		})
	}
}
