// Tests for: server.go — the workspace CLAUDE.md's repository imports follow
// zcp's pair records: at serve start, and after every tool call, which is
// where a pair is bootstrapped, adopted, stood up, moved onto its pool or
// deleted, so the next session reads the list current
// (docs/spec-workflows.md §5.1).
package server

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

const (
	reposBegin = "<!-- ZCP:BEGIN -->\n"
	reposEnd   = "<!-- ZCP:END -->\n"
	reposAbove = "# Franta\n\nKeep this.\n\n"
	reposBelow = "\n## Mine\n\n- and this, @verbatim\n"
)

// reposWorkspace is a workspace zcp init has set up — AGENTS.md and a CLAUDE.md
// whose managed block sits between the user's own lines — with its state
// directory.
func reposWorkspace(t *testing.T) (root, stateDir string) {
	t.Helper()
	root = t.TempDir()
	stateDir = filepath.Join(root, ".zcp", "state")
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(reposBegin+"# Zerops\n"+reposEnd), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte(reposAbove+reposBegin+"@AGENTS.md\n"+reposEnd+reposBelow), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, stateDir
}

func writePair(t *testing.T, stateDir string, m *workflow.ServiceMeta) {
	t.Helper()
	if err := workflow.WriteServiceMeta(stateDir, m); err != nil {
		t.Fatalf("write pair %s: %v", m.Hostname, err)
	}
}

func claudeMDWith(block string) string {
	return reposAbove + reposBegin + block + reposEnd + reposBelow
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// TestFollowRepos_ToolCall_CLAUDEMDImportsFollowThePairs: once a tool call
// answered — whatever it answered, since the records may have moved anyway —
// CLAUDE.md imports the repository of every pair the records hold: a pair
// added gains its line, a pair deleted or moved onto its pool loses it. The
// handler's answer goes back as it was; the user's lines stay byte for byte.
func TestFollowRepos_ToolCall_CLAUDEMDImportsFollowThePairs(t *testing.T) {
	t.Parallel()
	root, stateDir := reposWorkspace(t)
	s := &Server{
		rtInfo:   runtime.Info{InContainer: true, ServiceName: "zcp", MateEnabled: true},
		stateDir: stateDir,
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	steps := []struct {
		name   string
		change func()
		result *mcp.CallToolResult
		want   string
	}{
		{name: "a pair bootstrapped", change: func() {
			writePair(t, stateDir, &workflow.ServiceMeta{Hostname: "appdev", Mode: topology.PlanModeStandard, Stages: []topology.StageRef{{Hostname: "appstage"}}, BootstrappedAt: "2026-10-01"})
		}, result: successResult(), want: "@AGENTS.md\n@appdev/CLAUDE.md\n"},
		{name: "a pair adopted by a call that answered an error", change: func() {
			writePair(t, stateDir, &workflow.ServiceMeta{Hostname: "botdev", Mode: topology.PlanModeDev, BootstrappedAt: "2026-10-01"})
		}, result: errorResult(t, "INVALID_PARAMETER"), want: "@AGENTS.md\n@appdev/CLAUDE.md\n@botdev/CLAUDE.md\n"},
		{name: "a call that moved nothing", change: func() {}, result: successResult(), want: "@AGENTS.md\n@appdev/CLAUDE.md\n@botdev/CLAUDE.md\n"},
		{name: "a pair deleted", change: func() {
			if err := workflow.DeleteServiceMeta(stateDir, "botdev"); err != nil {
				t.Fatal(err)
			}
		}, result: successResult(), want: "@AGENTS.md\n@appdev/CLAUDE.md\n"},
		{name: "a pair moved onto its pool", change: func() {
			if _, err := workflow.MigratePairToPool(stateDir, workflow.PoolPairMove{Dev: "appdev", Pool: "apppool", PoolServiceID: "pool-1"}); err != nil {
				t.Fatal(err)
			}
		}, result: successResult(), want: "@AGENTS.md\n"},
	}
	for _, step := range steps {
		handler := s.followRepos()(func(_ context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
			step.change()
			return step.result, nil
		})
		got, err := handler(t.Context(), methodCallTool, callToolReq(t, "zerops_workflow", map[string]string{"action": "status"}))
		if err != nil || got != step.result {
			t.Errorf("%s: answered %v, %v; want the handler's own answer", step.name, got, err)
		}
		if got, want := readFileString(t, filepath.Join(root, "CLAUDE.md")), claudeMDWith(step.want); got != want {
			t.Errorf("%s: CLAUDE.md =\n%s\nwant\n%s", step.name, got, want)
		}
	}
}

// TestFollowRepos_NotAUserContainer_CLAUDEMDUntouched: a server outside a
// container, or in an authoring context, writes no import, whatever pairs the
// records hold — and a request that is no tool call reads nothing.
func TestFollowRepos_NotAUserContainer_CLAUDEMDUntouched(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		rt     runtime.Info
		method string
	}{
		{name: "local", rt: runtime.Info{}, method: methodCallTool},
		{name: "container authoring", rt: runtime.Info{InContainer: true, ServiceName: "zcp", Authoring: true}, method: methodCallTool},
		{name: "no tool call", rt: runtime.Info{InContainer: true, ServiceName: "zcp"}, method: "tools/list"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root, stateDir := reposWorkspace(t)
			writePair(t, stateDir, &workflow.ServiceMeta{Hostname: "appdev", Mode: topology.PlanModeDev, BootstrappedAt: "2026-10-01"})
			s := &Server{rtInfo: tt.rt, stateDir: stateDir, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
			handler := s.followRepos()(func(_ context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
				return successResult(), nil
			})
			if _, err := handler(t.Context(), tt.method, callToolReq(t, "zerops_discover", nil)); err != nil {
				t.Fatal(err)
			}
			if got, want := readFileString(t, filepath.Join(root, "CLAUDE.md")), claudeMDWith("@AGENTS.md\n"); got != want {
				t.Errorf("CLAUDE.md =\n%s\nwant it untouched:\n%s", got, want)
			}
		})
	}
}

// TestRefreshAgentContext_AtStart_ImportsEachPairsRepository: serve start —
// the refresh every new session's zcp runs — writes the imports of the pairs
// the records hold, around the user's own lines.
func TestRefreshAgentContext_AtStart_ImportsEachPairsRepository(t *testing.T) {
	t.Parallel()
	root, stateDir := reposWorkspace(t)
	writePair(t, stateDir, &workflow.ServiceMeta{Hostname: "botdev", Mode: topology.PlanModeDev, BootstrappedAt: "2026-10-01"})
	writePair(t, stateDir, &workflow.ServiceMeta{Hostname: "apidev", Mode: topology.PlanModeStandard, Stages: []topology.StageRef{{Hostname: "apistage"}}, BootstrappedAt: "2026-10-01"})

	refreshAgentContext(stateDir, runtime.Info{InContainer: true, ServiceName: "zcp"}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if got, want := readFileString(t, filepath.Join(root, "CLAUDE.md")), claudeMDWith("@AGENTS.md\n@apidev/CLAUDE.md\n@botdev/CLAUDE.md\n"); got != want {
		t.Errorf("CLAUDE.md =\n%s\nwant\n%s", got, want)
	}
}
