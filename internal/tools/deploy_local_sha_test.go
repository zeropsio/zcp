// Tests for: tools/deploy_local.go — the sha parameter end to end through
// the zerops_deploy MCP tool in local mode (docs/spec-workflows.md §4.9):
// sha threads into ops.DeployLocal, the response carries sha/appVersionId
// and the "deployed <sha7> → ..." message, and a successful build writes
// the zcp/deploy/* tag ledger in workingDir.
package tools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/ops"
	git "github.com/zeropsio/zcp/internal/ops/git"
	"github.com/zeropsio/zcp/internal/platform"
)

// initGitRepoWithZerops creates a real git repo at a temp dir with one
// commit containing zerops.yaml, returning (dir, full commit sha).
func initGitRepoWithZerops(t *testing.T) (dir, sha string) {
	t.Helper()
	dir = t.TempDir()
	run := func(args ...string) {
		cmd := exec.CommandContext(context.Background(), "git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=zcp-test", "GIT_AUTHOR_EMAIL=zcp-test@example.com",
			"GIT_COMMITTER_NAME=zcp-test", "GIT_COMMITTER_EMAIL=zcp-test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "zerops.yaml"), []byte("zerops:\n  - setup: app\n"), 0o644); err != nil {
		t.Fatalf("write zerops.yaml: %v", err)
	}
	run("init", "-q", "-b", "main")
	run("add", "zerops.yaml")
	run("commit", "-q", "-m", "initial")
	out, err := exec.CommandContext(context.Background(), "git", "-C", dir, "rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v\n%s", err, out)
	}
	return dir, strings.TrimSpace(string(out))
}

// localZcliMock scripts ops's commandRunner for a successful local zcli
// push (see ops.OverrideRunnerForTest) — LookPath succeeds, every Run
// (login, push) succeeds with no output. The interface is unexported in
// ops, but Go's structural typing lets this satisfy it from another
// package by matching the method set exactly.
type localZcliMock struct{}

func (localZcliMock) LookPath(_ string) (string, error) { return "/usr/local/bin/zcli", nil }
func (localZcliMock) Run(_ context.Context, _ string, _ ...string) (string, string, error) {
	return "", "", nil
}

func TestDeployLocalTool_WithSHA_ThreadsAndWritesLedger(t *testing.T) {
	dir, sha := initGitRepoWithZerops(t)

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av-1", ProjectID: "proj-1", ServiceStackID: "svc-1", Status: statusActive, Sequence: 1},
		})
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	restore := ops.OverrideRunnerForTest(localZcliMock{})
	defer restore()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeployLocal(srv, mock, okHTTP, "proj-1", authInfo, nil, "", testDeployEngine(t), nil)

	result := callTool(t, srv, "zerops_deploy", map[string]any{
		"targetService": "app",
		"workingDir":    dir,
		"sha":           sha[:7],
	})
	if result.IsError {
		t.Fatalf("unexpected IsError: %s", getTextContent(t, result))
	}

	var parsed ops.DeployResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &parsed); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if parsed.SHA != sha {
		t.Errorf("result.sha = %q, want %q", parsed.SHA, sha)
	}
	if parsed.AppVersionID != "av-1" {
		t.Errorf("result.appVersionId = %q, want av-1", parsed.AppVersionID)
	}
	if !strings.Contains(parsed.Message, sha[:7]) || !strings.Contains(parsed.Message, "av-1") {
		t.Errorf("message = %q, want it to name the short sha and appVersion", parsed.Message)
	}

	gotEntry, ok, err := git.LastDeployOnRecord(context.Background(), git.LocalRunner{}, dir, "proj-1", "app")
	if err != nil {
		t.Fatalf("LastDeployOnRecord: %v", err)
	}
	if !ok {
		t.Fatal("LastDeployOnRecord: ok = false, want true")
	}
	if gotEntry.SHA != sha {
		t.Errorf("ledger tag sha = %q, want %q", gotEntry.SHA, sha)
	}
}
