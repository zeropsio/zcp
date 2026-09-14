// Tests for: tools/deploy_ssh.go — the sha parameter end to end through the
// zerops_deploy MCP tool (docs/spec-workflows.md §4.9): sha threads into
// ops.DeploySSH, the response carries sha/appVersionId, and a successful
// build writes the zcp/deploy/* tag ledger in the source container.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
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
	// prevSHA, when set, is what the ledger already has on record for
	// this target — the LastDeployOnRecord call (a `git for-each-ref`
	// against the zcp/deploy/* tag namespace) returns it. Empty means
	// "nothing on record yet": for-each-ref succeeds with empty output,
	// exactly like a real repo with no matching tags.
	prevSHA          string
	prevAppVersionID string
	// headSHA/headDirty script the HeadStatus round trip (item 4, no
	// explicit sha): headSHA == "" means "no repo" (the rev-parse fails),
	// matching a real source with no git repo at workingDir.
	headSHA   string
	headDirty bool
}

func (s *stubSSHSHA) ExecSSH(_ context.Context, _, command string) ([]byte, error) {
	s.calls = append(s.calls, command)
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
	case strings.Contains(command, "for-each-ref") && strings.Contains(command, "refs/tags/zcp/deploy/"):
		if s.prevSHA == "" {
			return []byte(""), nil
		}
		appVersionID := s.prevAppVersionID
		if appVersionID == "" {
			appVersionID = "av-0"
		}
		msg := fmt.Sprintf(`{"sha":%q,"appVersionId":%q,"target":"app","project":"proj-1","at":"2026-09-14T11:00:00Z"}`, s.prevSHA, appVersionID)
		return []byte(s.prevSHA + "\t" + msg + "\n"), nil
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
	default: // login+push, and the ledger tag write
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

	// Ledger write: WriteLedger creates one annotated tag —
	// zcp/deploy/<project>/<target>/<appVersionId> — pointing at the
	// deployed sha.
	var sawTag bool
	for _, c := range ssh.calls {
		if strings.Contains(c, "tag -a -f -m") && strings.Contains(c, "zcp/deploy/proj-1/app/av-1") {
			sawTag = true
		}
	}
	if !sawTag {
		t.Errorf("expected a `git tag -a -f -m` call for zcp/deploy/proj-1/app/av-1; calls: %v", ssh.calls)
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
		if strings.Contains(c, "zcp/deploy/") || strings.Contains(c, "tag -a") || strings.Contains(c, "for-each-ref") {
			t.Errorf("no ledger call expected without sha, got: %s", c)
		}
	}
}

func TestDeployTool_SSHMode_WithSHA_NoPriorRecord_MessageOmitsPreviousClause(t *testing.T) {
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
	ssh := &stubSSHSHA{sha: sha, tmp: "/tmp/zcp-extract-9"} // prevSHA empty: nothing on record
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
	if strings.Contains(parsed.Message, "previous zcp deploy on record") {
		t.Errorf("message = %q, want it to say nothing about a previous record when none exists", parsed.Message)
	}
	if !strings.Contains(parsed.Message, "deployed "+shortSHA(sha)+" → app") {
		t.Errorf("message = %q, want it to still name the deployed sha and target", parsed.Message)
	}
}

func TestDeployTool_SSHMode_WithSHA_Redeploy_MessageNamesPreviousOnRecord(t *testing.T) {
	t.Parallel()

	const sha = "fullshaabc1234567"
	const prevSHA = "oldshadeadbeef0001"
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "builder"},
			{ID: "svc-2", Name: "app"},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av-1", ProjectID: "proj-1", ServiceStackID: "svc-2", Status: statusActive, Sequence: 1},
		})
	ssh := &stubSSHSHA{sha: sha, tmp: "/tmp/zcp-extract-9", prevSHA: prevSHA}
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
	if !strings.Contains(parsed.Message, "previous zcp deploy on record: "+prevSHA[:7]) {
		t.Errorf("message = %q, want it to say \"previous zcp deploy on record: %s\"", parsed.Message, prevSHA[:7])
	}
	if strings.Contains(parsed.Message, "replaces") {
		t.Errorf("message = %q, must never say \"replaces\" — the platform, not the ledger, is the authority for what's active", parsed.Message)
	}
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
