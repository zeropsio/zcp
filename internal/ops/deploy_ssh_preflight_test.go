// Tests for: ops/deploy_ssh.go — the self-deploy GF-12 preflight (docs/
// spec-workflows.md §8 DM, §12.6): notCarried/envFiles/repoState facts and
// the two hard refusals (.git is a file, .gitmodules present). Self-deploy
// only — a cross-deploy never runs this preflight at all.
package ops

import (
	"context"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// TestDeploySSH_SelfDeploy_PreflightNotCarried pins the positive case: a
// self-deploy source with git-ignored paths and a tracked/ignored .env
// file reports both on the result, and the message gains one sentence
// naming them.
func TestDeploySSH_SelfDeploy_PreflightNotCarried(t *testing.T) {
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app"},
		})
	ssh := &mockSSHDeployer{results: []sshResult{
		{output: []byte(
			"ZCP:GITFILE:0\n" +
				"ZCP:SUBMODULES:0\n" +
				"ZCP:HASREPO:1\n" +
				"ZCP:SHA:fullhead1234567\n" +
				"ZCP:DIRTY:1\n" +
				"ZCP:REPOSTATE:dirty\n" +
				"ZCP:IGNORED:12:cargo-ignored.txt\n" +
				"ZCP:IGNORED:5:.env\n" +
				"ZCP:ENVFILE:.env\n",
		)},
		{output: []byte("ok")}, // login+push
	}}
	authInfo := testAuthInfo()

	result, err := DeploySSH(context.Background(), mock, "proj-1", ssh, authInfo,
		"app", "app", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.NotCarried == nil {
		t.Fatal("result.NotCarried is nil, want populated (2 ignored paths)")
	}
	if result.NotCarried.Count != 2 {
		t.Errorf("NotCarried.Count = %d, want 2", result.NotCarried.Count)
	}
	if result.NotCarried.Bytes != 17 {
		t.Errorf("NotCarried.Bytes = %d, want 17", result.NotCarried.Bytes)
	}
	if len(result.EnvFiles) != 1 || result.EnvFiles[0] != ".env" {
		t.Errorf("EnvFiles = %v, want [.env]", result.EnvFiles)
	}
	if !containsSubstring(result.Message, "notCarried") && !containsSubstring(result.Message, "ignored path") {
		t.Errorf("Message should mention the not-carried paths, got: %s", result.Message)
	}
	if !containsSubstring(result.Message, "env vars") {
		t.Errorf("Message should point at service env vars, got: %s", result.Message)
	}
}

// TestDeploySSH_SelfDeploy_PreflightNone_FieldsOmitted pins the negative
// case: no ignored paths and no .env files ⇒ NotCarried stays nil and
// EnvFiles stays empty, and the message carries no extra sentence.
func TestDeploySSH_SelfDeploy_PreflightNone_FieldsOmitted(t *testing.T) {
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app"},
		})
	ssh := &mockSSHDeployer{results: []sshResult{
		{output: []byte("ZCP:GITFILE:0\nZCP:SUBMODULES:0\nZCP:HASREPO:1\nZCP:SHA:fullhead1234567\nZCP:DIRTY:0\nZCP:REPOSTATE:clean\n")},
		{output: []byte("ok")},
	}}
	authInfo := testAuthInfo()

	result, err := DeploySSH(context.Background(), mock, "proj-1", ssh, authInfo,
		"app", "app", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.NotCarried != nil {
		t.Errorf("NotCarried = %+v, want nil", result.NotCarried)
	}
	if len(result.EnvFiles) != 0 {
		t.Errorf("EnvFiles = %v, want empty", result.EnvFiles)
	}
	if containsSubstring(result.Message, "env vars") {
		t.Errorf("Message should carry no preflight note when nothing was found, got: %s", result.Message)
	}
}

// TestDeploySSH_SelfDeploy_RepoState_Merging pins that a repo mid-merge is
// classified "merging", not silently folded into "dirty".
func TestDeploySSH_SelfDeploy_RepoState_Merging(t *testing.T) {
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app"},
		})
	ssh := &mockSSHDeployer{results: []sshResult{
		{output: []byte("ZCP:GITFILE:0\nZCP:SUBMODULES:0\nZCP:HASREPO:1\nZCP:SHA:fullhead1234567\nZCP:DIRTY:1\nZCP:REPOSTATE:merging\n")},
		{output: []byte("ok")},
	}}
	authInfo := testAuthInfo()

	result, err := DeploySSH(context.Background(), mock, "proj-1", ssh, authInfo,
		"app", "app", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RepoState != "merging" {
		t.Errorf("RepoState = %q, want merging", result.RepoState)
	}
}

// TestDeploySSH_SelfDeploy_RepoState_Detached pins the detached-HEAD case.
func TestDeploySSH_SelfDeploy_RepoState_Detached(t *testing.T) {
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app"},
		})
	ssh := &mockSSHDeployer{results: []sshResult{
		{output: []byte("ZCP:GITFILE:0\nZCP:SUBMODULES:0\nZCP:HASREPO:1\nZCP:SHA:fullhead1234567\nZCP:DIRTY:0\nZCP:REPOSTATE:detached\n")},
		{output: []byte("ok")},
	}}
	authInfo := testAuthInfo()

	result, err := DeploySSH(context.Background(), mock, "proj-1", ssh, authInfo,
		"app", "app", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RepoState != "detached" {
		t.Errorf("RepoState = %q, want detached", result.RepoState)
	}
}

// TestDeploySSH_GitFile_Refused pins the GLC-hard-refusal: a self-deploy
// source whose .git is a regular file (linked-worktree/submodule pointer)
// is refused BEFORE any push — the replacement container would get only
// the pointer file, a broken repository.
func TestDeploySSH_GitFile_Refused(t *testing.T) {
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app"},
		})
	ssh := &mockSSHDeployer{results: []sshResult{
		{output: []byte("ZCP:GITFILE:1\nZCP:SUBMODULES:0\nZCP:HASREPO:0\n")},
	}}
	authInfo := testAuthInfo()

	_, err := DeploySSH(context.Background(), mock, "proj-1", ssh, authInfo,
		"app", "app", "", "", "")
	if err == nil {
		t.Fatal("expected error for .git being a regular file")
	}
	var pe *platform.PlatformError
	if !errorAs(err, &pe) {
		t.Fatalf("expected PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrGitWorktreeUnsupported {
		t.Errorf("code = %s, want %s", pe.Code, platform.ErrGitWorktreeUnsupported)
	}
	if len(ssh.calls) != 1 {
		t.Errorf("ssh calls = %d, want 1 (preflight only, no push attempted)", len(ssh.calls))
	}
}

// TestDeploySSH_Submodules_Refused pins the GLC-hard-refusal: a self-
// deploy source carrying .gitmodules is refused before any push — `git
// archive` ships submodule directories empty.
func TestDeploySSH_Submodules_Refused(t *testing.T) {
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app"},
		})
	ssh := &mockSSHDeployer{results: []sshResult{
		{output: []byte("ZCP:GITFILE:0\nZCP:SUBMODULES:1\nZCP:HASREPO:1\nZCP:SHA:fullhead1234567\nZCP:DIRTY:0\nZCP:REPOSTATE:clean\n")},
	}}
	authInfo := testAuthInfo()

	_, err := DeploySSH(context.Background(), mock, "proj-1", ssh, authInfo,
		"app", "app", "", "", "")
	if err == nil {
		t.Fatal("expected error for .gitmodules present")
	}
	var pe *platform.PlatformError
	if !errorAs(err, &pe) {
		t.Fatalf("expected PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrGitSubmodulesUnsupported {
		t.Errorf("code = %s, want %s", pe.Code, platform.ErrGitSubmodulesUnsupported)
	}
	if len(ssh.calls) != 1 {
		t.Errorf("ssh calls = %d, want 1 (preflight only, no push attempted)", len(ssh.calls))
	}
}

// TestDeploySSH_CrossDeploy_NoPreflight pins that the preflight (and its
// two hard refusals) NEVER fires on a cross-deploy — a distinct source's
// own repo shape isn't what a cross-deploy ships, so a source with a
// .gitmodules file or a file-shaped .git must not be refused.
func TestDeploySSH_CrossDeploy_NoPreflight(t *testing.T) {
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "builder"},
			{ID: "svc-2", Name: "app"},
		})
	// Plain HeadStatus-shaped output (no ZCP: markers) — if the preflight
	// script ran, its markers would be absent and every field would read
	// zero/false, which happens to also "pass"; the real proof is the
	// call count/shape staying identical to the pre-existing HeadStatus
	// path (TestDeploySSH_WorkingTree_DoesNotReadDeploymentTags already
	// pins the command shape) and that no refusal error fires here despite
	// this fixture's name implying a submodule/worktree source.
	ssh := &mockSSHDeployer{output: []byte("abcdef1234567\n")}
	authInfo := testAuthInfo()

	result, err := DeploySSH(context.Background(), mock, "proj-1", ssh, authInfo,
		"builder", "app", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.NotCarried != nil || len(result.EnvFiles) != 0 || result.RepoState != "" {
		t.Errorf("cross-deploy must never populate preflight fields: %+v", result)
	}
}
