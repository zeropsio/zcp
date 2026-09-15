// Tests for: ops/deploy_ssh.go — the sha parameter (docs/spec-workflows.md
// §4.9): DeploySSH resolves an explicit sha, extracts its tree into a fresh
// temp dir inside the source container (over SSH, never the SSHFS mount),
// and pushes from there with --version-name, --no-git, and no
// GitEnsureRepoHeadCommand step.
package ops

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

func TestDeploySSH_WithSHA_ResolvesExtractsAndPushesFromExtractedDir(t *testing.T) {
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "builder"},
			{ID: "svc-2", Name: "app"},
		})
	ssh := &mockSSHDeployer{results: []sshResult{
		{output: []byte("f0115ba1234567\n")},     // resolve
		{output: []byte(validCommitZeropsYaml)},  // git show <sha>:zerops.yaml
		{output: []byte("/tmp/zcp-extract-1\n")}, // mktemp -d
		{output: []byte("")},                     // extract (archive | tar -x)
		{output: []byte("ok")},                   // login+push
		{output: []byte("")},                     // cleanup rm -rf
	}}
	authInfo := testAuthInfo()

	result, err := DeploySSH(context.Background(), mock, "proj-1", ssh, authInfo,
		"builder", "app", "", "", "abc123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.SHA != "f0115ba1234567" {
		t.Errorf("result.SHA = %q, want f0115ba1234567", result.SHA)
	}
	if len(ssh.calls) != 6 {
		t.Fatalf("ssh calls = %d, want 6 (resolve, git show zerops.yaml, mktemp, extract, push, cleanup): %+v", len(ssh.calls), ssh.calls)
	}
	for _, c := range ssh.calls {
		if c.hostname != "builder" {
			t.Errorf("call hostname = %q, want builder (the SOURCE container): %+v", c.hostname, c)
		}
	}
	if !strings.Contains(ssh.calls[0].command, "rev-parse --verify") || !strings.Contains(ssh.calls[0].command, "^{commit}") {
		t.Errorf("call[0] = %q, want a rev-parse --verify ...^{commit}", ssh.calls[0].command)
	}
	// The commit's zerops.yaml is validated, NEVER the SSHFS mount — the
	// mount may be missing or stale relative to the deployed commit.
	if !strings.Contains(ssh.calls[1].command, "git show") || !strings.Contains(ssh.calls[1].command, "'f0115ba1234567:zerops.yaml'") {
		t.Errorf("call[1] = %q, want git show 'f0115ba1234567:zerops.yaml'", ssh.calls[1].command)
	}
	if !strings.Contains(ssh.calls[2].command, "mktemp -d") {
		t.Errorf("call[2] = %q, want mktemp -d", ssh.calls[2].command)
	}
	if !strings.Contains(ssh.calls[3].command, "git archive --format=tar") || !strings.Contains(ssh.calls[3].command, "| tar -x -C") {
		t.Errorf("call[3] = %q, want the archive|tar pipe", ssh.calls[3].command)
	}
	pushCmd := ssh.calls[4].command
	if !strings.Contains(pushCmd, "--no-git") {
		t.Errorf("push command = %q, want --no-git", pushCmd)
	}
	if !strings.Contains(pushCmd, "--version-name 'f0115ba1234567'") {
		t.Errorf("push command = %q, want --version-name 'f0115ba1234567'", pushCmd)
	}
	if !strings.Contains(pushCmd, "cd '/tmp/zcp-extract-1'") {
		t.Errorf("push command = %q, want to cd into the extracted dir", pushCmd)
	}
	if strings.Contains(pushCmd, " -g") {
		t.Errorf("push command = %q, must not include -g (no .git in an extracted tree)", pushCmd)
	}
	if !strings.Contains(ssh.calls[5].command, "rm -rf '/tmp/zcp-extract-1'") {
		t.Errorf("call[5] = %q, want cleanup of the extracted dir", ssh.calls[5].command)
	}
}

// validCommitZeropsYaml is the zerops.yaml content a `git show
// <sha>:zerops.yaml` call returns in these tests — a minimal setup entry
// matching the "app" target hostname used throughout this file.
const validCommitZeropsYaml = "zerops:\n  - setup: app\n"

// TestDeploySSH_WithSHA_CommitMissingZeropsYaml_ReturnsErrorBeforeExtraction
// pins the hard-error path (docs/spec-workflows.md §4.9): a commit with no
// zerops.yaml at all (git show fails) aborts BEFORE mktemp/extraction ever
// runs — no wasted temp-dir or archive work for an input that cannot
// deploy.
func TestDeploySSH_WithSHA_CommitMissingZeropsYaml_ReturnsErrorBeforeExtraction(t *testing.T) {
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "builder"},
			{ID: "svc-2", Name: "app"},
		})
	ssh := &mockSSHDeployer{results: []sshResult{
		{output: []byte("f0115ba1234567\n")}, // resolve
		{output: []byte("fatal: path 'zerops.yaml' does not exist in 'f0115ba1234567'"), err: errTestNoZeropsYaml}, // git show zerops.yaml fails
		{output: []byte("fatal: path 'zerops.yml' does not exist in 'f0115ba1234567'"), err: errTestNoZeropsYaml},  // …and the zerops.yml fallback fails too
	}}
	authInfo := testAuthInfo()

	_, err := DeploySSH(context.Background(), mock, "proj-1", ssh, authInfo,
		"builder", "app", "", "", "abc123")
	if err == nil {
		t.Fatal("expected error for a commit with no zerops.yaml")
	}
	var pe *platform.PlatformError
	if !errorAs(err, &pe) {
		t.Fatalf("expected PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrInvalidParameter {
		t.Errorf("code = %s, want %s", pe.Code, platform.ErrInvalidParameter)
	}
	if !strings.Contains(pe.Message, "f0115ba") && !strings.Contains(pe.Message, "no zerops.yaml") {
		t.Errorf("message = %q, want it to name the commit and the missing file", pe.Message)
	}
	if len(ssh.calls) != 3 {
		t.Fatalf("ssh calls = %d, want 3 (resolve, git show zerops.yaml, git show zerops.yml) — mktemp/extract must NOT run: %+v", len(ssh.calls), ssh.calls)
	}
	if !strings.Contains(ssh.calls[2].command, "zerops.yml") {
		t.Errorf("call[2] = %q, want the zerops.yml fallback read", ssh.calls[2].command)
	}
	for _, c := range ssh.calls {
		if strings.Contains(c.command, "mktemp") || strings.Contains(c.command, "git archive") {
			t.Errorf("extraction step ran despite the missing zerops.yaml: %q", c.command)
		}
	}
}

var errTestNoZeropsYaml = &noZeropsYamlError{}

type noZeropsYamlError struct{}

func (*noZeropsYamlError) Error() string { return "exit 128" }

func TestDeploySSH_WithUnresolvableSHA_ReturnsInvalidParameterError(t *testing.T) {
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "builder"},
			{ID: "svc-2", Name: "app"},
		})
	ssh := &mockSSHDeployer{err: errTestSHA}
	authInfo := testAuthInfo()

	_, err := DeploySSH(context.Background(), mock, "proj-1", ssh, authInfo,
		"builder", "app", "", "", "nope")
	if err == nil {
		t.Fatal("expected error for an unresolvable sha")
	}
	var pe *platform.PlatformError
	if !errorAs(err, &pe) {
		t.Fatalf("expected PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrInvalidParameter {
		t.Errorf("code = %s, want %s", pe.Code, platform.ErrInvalidParameter)
	}
	if len(ssh.calls) != 1 {
		t.Errorf("only the resolve call should run when sha does not resolve, got %d: %+v", len(ssh.calls), ssh.calls)
	}
}

// TestDeploySSH_SelfDeployWithSHA_RefusesBeforeAnySSHCall pins the DM-2-
// style self-deploy guard on the sha path (docs/spec-workflows.md §4.9):
// a self-deploy from a commit would extract sha's tree and push
// --no-git, shipping no .git and losing the container's repository that
// the normal path keeps via -g (GLC-2, P10). The guard must fire BEFORE
// any SSH round trip — not even the resolve call runs.
func TestDeploySSH_SelfDeployWithSHA_RefusesBeforeAnySSHCall(t *testing.T) {
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app"},
		})
	ssh := &mockSSHDeployer{output: []byte("ok")}
	authInfo := testAuthInfo()

	_, err := DeploySSH(context.Background(), mock, "proj-1", ssh, authInfo,
		"", "app", "", "", "abc123") // sourceService omitted → auto-infers self-deploy
	if err == nil {
		t.Fatal("expected error for a self-deploy sha")
	}
	var pe *platform.PlatformError
	if !errorAs(err, &pe) {
		t.Fatalf("expected PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrInvalidParameter {
		t.Errorf("code = %s, want %s", pe.Code, platform.ErrInvalidParameter)
	}
	if len(ssh.calls) != 0 {
		t.Errorf("zero SSH calls expected — the guard must fire before any SSH round trip, got %d: %+v", len(ssh.calls), ssh.calls)
	}
}

// TestDeploySSH_SelfDeployWithSHA_ExplicitSameSource_AlsoRefused proves
// the guard fires on an EXPLICIT source==target pair too, not only the
// auto-inferred (empty sourceService) case.
func TestDeploySSH_SelfDeployWithSHA_ExplicitSameSource_AlsoRefused(t *testing.T) {
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app"},
		})
	ssh := &mockSSHDeployer{output: []byte("ok")}
	authInfo := testAuthInfo()

	_, err := DeploySSH(context.Background(), mock, "proj-1", ssh, authInfo,
		"app", "app", "", "", "abc123")
	if err == nil {
		t.Fatal("expected error for an explicit self-deploy sha")
	}
	if len(ssh.calls) != 0 {
		t.Errorf("zero SSH calls expected, got %d: %+v", len(ssh.calls), ssh.calls)
	}
}

// TestDeploySSH_NoSHA_SourceHasNoRepo_UnaffectedByRecording pins the "no
// behaviour change" half of item 4 (docs/spec-workflows.md §4.9): when the
// source has no git repo at all (HeadStatus's rev-parse fails), the
// working-tree deploy is untouched — no source revision set, no extra
// machinery beyond the one read-only HeadStatus round trip.
func TestDeploySSH_NoSHA_SourceHasNoRepo_UnaffectedByRecording(t *testing.T) {
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "builder"},
			{ID: "svc-2", Name: "app"},
		})
	ssh := &mockSSHDeployer{results: []sshResult{
		{err: errTestSHA},      // HeadStatus: source has no git repo
		{output: []byte("ok")}, // login+push
	}}
	authInfo := testAuthInfo()

	result, err := DeploySSH(context.Background(), mock, "proj-1", ssh, authInfo,
		"builder", "app", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.SHA != "" {
		t.Errorf("result.SHA = %q, want empty (source has no git repo)", result.SHA)
	}
	if result.Dirty {
		t.Error("result.Dirty = true, want false")
	}
	if len(ssh.calls) != 2 {
		t.Fatalf("ssh calls = %d, want 2 (HeadStatus, push): %+v", len(ssh.calls), ssh.calls)
	}
	if !strings.Contains(ssh.calls[0].command, "rev-parse --verify HEAD") {
		t.Errorf("call[0] = %q, want the HeadStatus check", ssh.calls[0].command)
	}
}

// TestDeploySSH_NoSHA_SourceHasCleanRepo_RecordsHEADAndKeepsPushByteIdentical
// pins item 4's positive case: a working-tree deploy (no explicit sha)
// from a source with a clean git repo records HEAD as SHA, Dirty=false —
// and the push command itself (args, -g, workspace-state) is BYTE
// IDENTICAL to what a no-git-repo source would have produced (the only
// difference is the extra read-only HeadStatus round trip beforehand).
func TestDeploySSH_NoSHA_SourceHasCleanRepo_RecordsHEADAndKeepsPushByteIdentical(t *testing.T) {
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app"},
		})
	ssh := &mockSSHDeployer{results: []sshResult{
		{output: []byte("fullhead1234567\n")}, // HeadStatus: clean
		{output: []byte("ok")},                // login+push
	}}
	authInfo := testAuthInfo()

	result, err := DeploySSH(context.Background(), mock, "proj-1", ssh, authInfo,
		"app", "app", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.SHA != "fullhead1234567" {
		t.Errorf("result.SHA = %q, want fullhead1234567", result.SHA)
	}
	if result.Dirty {
		t.Error("result.Dirty = true, want false (clean status)")
	}
	if len(ssh.calls) != 2 {
		t.Fatalf("ssh calls = %d, want 2 (HeadStatus, push): %+v", len(ssh.calls), ssh.calls)
	}

	// The push command must be EXACTLY what buildSSHCommand produces for
	// a plain self-deploy — recording HEAD must never perturb it.
	want := buildSSHCommand(authInfo, "svc-1", defaultWorkingDir, "", true, topology.RuntimeUnknown)
	if ssh.calls[1].command != want {
		t.Errorf("push command = %q, want byte-identical to buildSSHCommand's output %q", ssh.calls[1].command, want)
	}
}

// TestDeploySSH_NoSHA_SourceHasDirtyRepo_RecordsDirty pins the Dirty=true
// case: an uncommitted change on top of HEAD is recorded, never claimed
// as a clean deploy of that commit.
func TestDeploySSH_NoSHA_SourceHasDirtyRepo_RecordsDirty(t *testing.T) {
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app"},
		})
	ssh := &mockSSHDeployer{results: []sshResult{
		{output: []byte("fullhead1234567\nM")}, // HeadStatus: dirty
		{output: []byte("ok")},                 // login+push
	}}
	authInfo := testAuthInfo()

	result, err := DeploySSH(context.Background(), mock, "proj-1", ssh, authInfo,
		"app", "app", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.SHA != "fullhead1234567" {
		t.Errorf("result.SHA = %q, want fullhead1234567", result.SHA)
	}
	if !result.Dirty {
		t.Error("result.Dirty = false, want true")
	}
}

// TestDeploySSH_WithSHA_AlwaysDirtyFalse pins that the deploy-from-commit
// path never sets Dirty — it always ships exactly sha's tree.
func TestDeploySSH_WithSHA_AlwaysDirtyFalse(t *testing.T) {
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "builder"},
			{ID: "svc-2", Name: "app"},
		})
	ssh := &mockSSHDeployer{results: []sshResult{
		{output: []byte("f0115ba1234567\n")},     // resolve
		{output: []byte(validCommitZeropsYaml)},  // git show
		{output: []byte("/tmp/zcp-extract-1\n")}, // mktemp -d
		{output: []byte("")},                     // extract
		{output: []byte("ok")},                   // login+push
		{output: []byte("")},                     // cleanup
	}}
	authInfo := testAuthInfo()

	result, err := DeploySSH(context.Background(), mock, "proj-1", ssh, authInfo,
		"builder", "app", "", "", "abc123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Dirty {
		t.Error("result.Dirty = true, want false — a deploy-from-commit is never dirty")
	}
}

type shaTestError struct{ msg string }

func (e *shaTestError) Error() string { return e.msg }

var errTestSHA = &shaTestError{"exit 128: bad revision"}

func TestDeploySSH_WorkingTree_DoesNotReadDeploymentTags(t *testing.T) {
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-1", Name: "builder"},
		{ID: "svc-2", Name: "app"},
	})
	ssh := &mockSSHDeployer{output: []byte("abcdef1234567\n")}
	if _, err := DeploySSH(context.Background(), mock, "proj-1", ssh, testAuthInfo(),
		"builder", "app", "", "", ""); err != nil {
		t.Fatalf("DeploySSH: %v", err)
	}
	for _, call := range ssh.calls {
		if strings.Contains(call.command, "for-each-ref") || strings.Contains(call.command, "git tag") {
			t.Errorf("deployment must not read or mutate tags: %s", call.command)
		}
	}
}
