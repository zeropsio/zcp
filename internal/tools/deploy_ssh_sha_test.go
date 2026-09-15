// Tests for: tools/deploy_ssh.go — the sha parameter end to end through the
// zerops_deploy MCP tool (docs/spec-workflows.md §4.9): sha threads into
// ops.DeploySSH and the response carries sha/appVersionId without tag operations.
package tools

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

// stubSSHSHA scripts ExecSSH by inspecting the command text — the
// deploy-from-commit path issues several distinct SSH round trips (resolve,
// mktemp, extract, push, cleanup, readiness) that each need a different
// canned response.
type stubSSHSHA struct {
	mu    sync.Mutex // guards calls: batch targets execute concurrently
	sha   string
	tmp   string
	calls []string
	// headSHA/headDirty script the HeadStatus round trip (item 4, no
	// explicit sha): headSHA == "" means "no repo" (the rev-parse fails),
	// matching a real source with no git repo at workingDir.
	headSHA   string
	headDirty bool
}

func (s *stubSSHSHA) ExecSSH(_ context.Context, _, command string) ([]byte, error) {
	s.mu.Lock()
	s.calls = append(s.calls, command)
	s.mu.Unlock()
	switch {
	case strings.Contains(command, "rev-parse --verify HEAD") && strings.Contains(command, "git status --porcelain"):
		if s.headSHA == "" {
			return nil, errStubNoRepo
		}
		out := s.headSHA + "\n"
		if s.headDirty {
			out += "M"
		}
		return []byte(out), nil
	case strings.Contains(command, "rev-parse --verify") && strings.Contains(command, "^{commit}"):
		return []byte(s.sha + "\n"), nil
	case strings.Contains(command, "mktemp -d"):
		return []byte(s.tmp + "\n"), nil
	case strings.Contains(command, "git archive --format=tar"):
		return []byte(""), nil
	case strings.Contains(command, "rm -rf"):
		return []byte(""), nil
	case command == "true": // ops.WaitSSHReady probe
		return nil, nil
	default: // login+push
		return []byte("ok"), nil
	}
}

func (s *stubSSHSHA) ExecSSHBackground(_ context.Context, _, _ string, _ time.Duration) ([]byte, error) {
	return []byte("ok"), nil
}

func TestDeployTool_SSHMode_WithSHA_ThreadsWithoutTagOperations(t *testing.T) {
	t.Parallel()

	const sha = "f0115ba0abc1234567"
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "builder"},
			{ID: "svc-2", Name: "app"},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av-1", ProjectID: "proj-1", ServiceStackID: "svc-2", Status: statusActive, Sequence: 1},
		})
	ssh := &stubSSHSHA{sha: sha, tmp: "/tmp/zcp-extract-9"}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploySSH(srv, mock, okHTTP, "proj-1", ssh, authInfo, nil, runtime.Info{}, "", testDeployEngine(t), nil)

	result := callTool(t, srv, "zerops_deploy", map[string]any{
		"sourceService": "builder",
		"targetService": "app",
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

	assertNoDeployTagOperations(t, ssh.calls)
}

func TestDeployTool_SSHMode_NoSHA_NoTagOperations(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "builder"},
			{ID: "svc-2", Name: "app"},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av-1", ProjectID: "proj-1", ServiceStackID: "svc-2", Status: statusActive, Sequence: 1},
		})
	ssh := &stubSSHSHA{}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploySSH(srv, mock, okHTTP, "proj-1", ssh, authInfo, nil, runtime.Info{}, "", testDeployEngine(t), nil)

	result := callTool(t, srv, "zerops_deploy", map[string]any{
		"sourceService": "builder",
		"targetService": "app",
	})
	if result.IsError {
		t.Fatalf("unexpected IsError: %s", getTextContent(t, result))
	}
	assertNoDeployTagOperations(t, ssh.calls)
}

// TestDeployTool_SSHMode_NoSHA_DirtyRepo_MessageSaysRecordedNotDeployed
// pins item 4's message contract (docs/spec-workflows.md §4.9): a plain
// working-tree deploy (no sha param) whose source has a DIRTY git repo
// must never claim "deployed commit X" — that implies exactly that
// commit's tree shipped, which a dirty tree contradicts.
func TestDeployTool_SSHMode_NoSHA_DirtyRepo_MessageSaysRecordedNotDeployed(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "builder"},
			{ID: "svc-2", Name: "app"},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av-1", ProjectID: "proj-1", ServiceStackID: "svc-2", Status: statusActive, Sequence: 1},
		})
	ssh := &stubSSHSHA{headSHA: "fullhead1234567", headDirty: true}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploySSH(srv, mock, okHTTP, "proj-1", ssh, authInfo, nil, runtime.Info{}, "", testDeployEngine(t), nil)

	result := callTool(t, srv, "zerops_deploy", map[string]any{
		"sourceService": "builder",
		"targetService": "app",
	})
	if result.IsError {
		t.Fatalf("unexpected IsError: %s", getTextContent(t, result))
	}
	var parsed ops.DeployResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &parsed); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	assertNoDeployTagOperations(t, ssh.calls)
	if parsed.SHA != ssh.headSHA || parsed.AppVersionID != "av-1" {
		t.Errorf("unexpected source/version evidence: %+v", parsed)
	}
	if !parsed.Dirty {
		t.Fatal("parsed.Dirty = false, want true")
	}
	if !strings.Contains(parsed.Message, "recorded: HEAD "+shortSHA("fullhead1234567")) || !strings.Contains(parsed.Message, "uncommitted changes") {
		t.Errorf("message = %q, want it to say \"recorded: HEAD <sha7> + uncommitted changes\"", parsed.Message)
	}
	if strings.Contains(parsed.Message, "deployed "+shortSHA("fullhead1234567")) {
		t.Errorf("message = %q, must not claim \"deployed <sha>\" for a dirty working-tree deploy", parsed.Message)
	}
}

// TestDeployTool_SSHMode_NoSHA_CleanRepo_MessageNamesDeployedHEAD proves
// the CLEAN working-tree case reads like an ordinary sha deploy message —
// no "recorded:" wording, since nothing here is uncommitted.
func TestDeployTool_SSHMode_NoSHA_CleanRepo_MessageNamesDeployedHEAD(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "builder"},
			{ID: "svc-2", Name: "app"},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av-1", ProjectID: "proj-1", ServiceStackID: "svc-2", Status: statusActive, Sequence: 1},
		})
	ssh := &stubSSHSHA{headSHA: "fullhead1234567"}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploySSH(srv, mock, okHTTP, "proj-1", ssh, authInfo, nil, runtime.Info{}, "", testDeployEngine(t), nil)

	result := callTool(t, srv, "zerops_deploy", map[string]any{
		"sourceService": "builder",
		"targetService": "app",
	})
	if result.IsError {
		t.Fatalf("unexpected IsError: %s", getTextContent(t, result))
	}
	var parsed ops.DeployResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &parsed); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	assertNoDeployTagOperations(t, ssh.calls)
	if parsed.Dirty {
		t.Error("parsed.Dirty = true, want false")
	}
	if !strings.Contains(parsed.Message, "deployed "+shortSHA("fullhead1234567")+" → app") {
		t.Errorf("message = %q, want it to name the recorded HEAD like a sha deploy", parsed.Message)
	}
}

// errStubNoRepo simulates HeadStatus's `git rev-parse --verify HEAD`
// failing on a source with no git repo at workingDir.
var errStubNoRepo = &stubNoRepoError{}

type stubNoRepoError struct{}

func (*stubNoRepoError) Error() string { return "fatal: not a git repository" }

// TestDeployTool_SSHMode_SelfDeploy_CleanRepo_MessageKeepsSessionsGoneFact:
// under GLC-1/2 a container self-deploy always has a repo, so the message
// takes the "deployed <sha7>" shape — the strategy-agnostic fact that the
// container was replaced (prior SSH sessions are gone) must survive that
// shape, not live only in the no-repo default branch.
func TestDeployTool_SSHMode_SelfDeploy_CleanRepo_MessageKeepsSessionsGoneFact(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app"},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av-1", ProjectID: "proj-1", ServiceStackID: "svc-1", Status: statusActive, Sequence: 1},
		})
	ssh := &stubSSHSHA{headSHA: "fullhead1234567"}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploySSH(srv, mock, okHTTP, "proj-1", ssh, authInfo, nil, runtime.Info{}, "", testDeployEngine(t), nil)

	result := callTool(t, srv, "zerops_deploy", map[string]any{
		"targetService": "app",
	})
	if result.IsError {
		t.Fatalf("unexpected IsError: %s", getTextContent(t, result))
	}
	var parsed ops.DeployResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &parsed); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if !strings.Contains(parsed.Message, "deployed "+shortSHA("fullhead1234567")) {
		t.Errorf("message = %q, want the deployed <sha7> shape", parsed.Message)
	}
	assertNoDeployTagOperations(t, ssh.calls)
	if !strings.Contains(parsed.Message, "prior SSH sessions are gone") {
		t.Errorf("message = %q, want the container-replaced fact appended", parsed.Message)
	}
}

// The deploy tool must neither consult nor mutate Git deployment tags.
func assertNoDeployTagOperations(t *testing.T, calls []string) {
	t.Helper()
	for _, command := range calls {
		if strings.Contains(command, "zcp/deploy/") || strings.Contains(command, "git tag ") || strings.Contains(command, "for-each-ref") {
			t.Errorf("unexpected deploy tag operation: %s", command)
		}
	}
}
