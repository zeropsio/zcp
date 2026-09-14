// Tests for: tools/deploy_ssh.go — the sha parameter end to end through the
// zerops_deploy MCP tool (docs/spec-workflows.md §4.5): sha threads into
// ops.DeploySSH, the response carries sha/appVersionId, and a successful
// build writes the refs/zcp/* ledger in the source container.
package tools

import (
	"context"
	"encoding/json"
	"strings"
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
	sha   string
	tmp   string
	calls []string
}

func (s *stubSSHSHA) ExecSSH(_ context.Context, _, command string) ([]byte, error) {
	s.calls = append(s.calls, command)
	switch {
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
	default: // login+push, and every ledger git command (tree/commit-tree/update-ref)
		return []byte("ok"), nil
	}
}

func (s *stubSSHSHA) ExecSSHBackground(_ context.Context, _, _ string, _ time.Duration) ([]byte, error) {
	return []byte("ok"), nil
}

func TestDeployTool_SSHMode_WithSHA_ThreadsAndWritesLedger(t *testing.T) {
	t.Parallel()

	const sha = "fullshaabc1234567"
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

	// Ledger write: WriteLedger moves refs/zcp/env/<target> and appends a
	// refs/zcp/deploy/<n> ref — both update-ref calls must have run against
	// the source container after the build resolved.
	var sawEnvRef, sawDeployRef bool
	for _, c := range ssh.calls {
		if strings.Contains(c, "update-ref") && strings.Contains(c, "refs/zcp/env/app") {
			sawEnvRef = true
		}
		if strings.Contains(c, "update-ref") && strings.Contains(c, "refs/zcp/deploy/") {
			sawDeployRef = true
		}
	}
	if !sawEnvRef {
		t.Errorf("expected an update-ref call moving refs/zcp/env/app; calls: %v", ssh.calls)
	}
	if !sawDeployRef {
		t.Errorf("expected an update-ref call appending refs/zcp/deploy/<n>; calls: %v", ssh.calls)
	}
}

func TestDeployTool_SSHMode_NoSHA_NoLedgerCalls(t *testing.T) {
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
	for _, c := range ssh.calls {
		if strings.Contains(c, "refs/zcp/") {
			t.Errorf("no ledger call expected without sha, got: %s", c)
		}
	}
}
