// Tests for the StateEnvelope-on-the-wire contract (docs/spec-mate.md,
// "Envelope on the wire"). A workflow-aware tool result's text ends with a
// fenced `json zcp-envelope` block so the mate client's reducer can rebuild
// lifecycle state from the provider's tool-result stream — the envelope
// cannot ride in MCP `structuredContent` because Claude Code replaces the
// model-facing text with it (see structured_content_lint_test.go).
package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestHandleLifecycleStatus_TextCarriesEnvelope pins the primary
// carrier: `zerops_workflow action="status"` is the lifecycle recovery
// primitive, so its result must carry the envelope the reducer keys on.
func TestHandleLifecycleStatus_TextCarriesEnvelope(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	eng := workflow.NewEngine(dir, workflow.EnvContainer, nil)
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "test"}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "apidev", Status: "ACTIVE",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}},
		})

	result, structured, err := handleLifecycleStatus(
		context.Background(), eng, mock, "proj-1", runtime.Info{InContainer: true}, nil, nil,
	)
	if err != nil {
		t.Fatalf("handleLifecycleStatus: %v", err)
	}
	if structured != nil {
		t.Fatalf("structured output must stay nil — Claude Code replaces the text with it")
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", getTextContent(t, result))
	}

	text := getTextContent(t, result)
	if !strings.Contains(text, "## Status") {
		t.Errorf("markdown guidance lost:\n%s", text)
	}

	env, ok := workflow.ExtractEnvelope(text)
	if !ok {
		t.Fatalf("status result carries no envelope block:\n%s", text)
	}
	if env.Project.ID != "proj-1" {
		t.Errorf("envelope project: got %q want %q", env.Project.ID, "proj-1")
	}
	if env.Phase == "" {
		t.Errorf("envelope phase is empty")
	}
	if len(env.Services) != 1 || env.Services[0].Hostname != "apidev" {
		t.Errorf("envelope services: got %+v want one apidev", env.Services)
	}
}

// TestRenderDevelopBriefing_TextCarriesEnvelope — the develop `start`
// briefing is the second lifecycle-status carrier: a thread that opens a
// work session must be able to seed its strip without a second call.
func TestRenderDevelopBriefing_TextCarriesEnvelope(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	eng := workflow.NewEngine(dir, workflow.EnvContainer, nil)
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-2", Name: "test"}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "apidev", Status: "ACTIVE",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}},
		})

	result, structured, err := renderDevelopBriefing(
		context.Background(), eng, mock, "proj-2", runtime.Info{InContainer: true}, nil,
	)
	if err != nil {
		t.Fatalf("renderDevelopBriefing: %v", err)
	}
	if structured != nil {
		t.Fatalf("structured output must stay nil")
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", getTextContent(t, result))
	}

	text := getTextContent(t, result)
	env, ok := workflow.ExtractEnvelope(text)
	if !ok {
		t.Fatalf("develop briefing carries no envelope block:\n%s", text)
	}
	if env.Project.ID != "proj-2" {
		t.Errorf("envelope project: got %q want %q", env.Project.ID, "proj-2")
	}
}

// TestEnvelope_AttemptHistory_OnlyStatusCarriesItWhole: a reply other than
// action="status" carries only each host's last deploy and verify attempt,
// keeping every host that has one, while action="status" carries the whole
// history. Mate reads only the last attempt and which hosts have any
// (docs/spec-mate.md §1.4).
func TestEnvelope_AttemptHistory_OnlyStatusCarriesItWhole(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	eng := workflow.NewEngine(dir, workflow.EnvContainer, nil)
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-3", Name: "test"}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "apidev", Status: "ACTIVE",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}},
			{ID: "svc-2", Name: "webdev", Status: "ACTIVE",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}},
		})
	rt := runtime.Info{InContainer: true}

	ws := workflow.NewWorkSession("proj-3", string(workflow.EnvContainer), "ship it", []string{"apidev", "webdev"})
	ws.Deploys = map[string][]workflow.DeployAttempt{
		"apidev": {
			{AttemptedAt: "2026-10-16T10:00:00Z", Error: "build failed"},
			{AttemptedAt: "2026-10-16T10:05:00Z", Error: "build failed again"},
			{AttemptedAt: "2026-10-16T10:10:00Z", SucceededAt: "2026-10-16T10:11:00Z"},
		},
		"webdev": {
			{AttemptedAt: "2026-10-16T10:20:00Z", Error: "start failed"},
		},
	}
	ws.Verifies = map[string][]workflow.VerifyAttempt{
		"apidev": {
			{AttemptedAt: "2026-10-16T10:12:00Z", Summary: "http 502"},
			{AttemptedAt: "2026-10-16T10:15:00Z", PassedAt: "2026-10-16T10:15:00Z", Passed: true, Summary: "healthy"},
		},
	}
	if err := workflow.SaveWorkSession(dir, ws); err != nil {
		t.Fatalf("SaveWorkSession: %v", err)
	}

	wireOf := func(t *testing.T, result *mcp.CallToolResult) *workflow.WorkSessionSummary {
		t.Helper()
		env, ok := workflow.ExtractEnvelope(getTextContent(t, result))
		if !ok {
			t.Fatalf("result carries no envelope block:\n%s", getTextContent(t, result))
		}
		if env.WorkSession == nil {
			t.Fatalf("envelope carries no work session")
		}
		return env.WorkSession
	}
	type last struct {
		count     int
		success   bool
		iteration int
	}
	check := func(t *testing.T, kind string, got map[string][]workflow.AttemptInfo, want map[string]last) {
		t.Helper()
		if len(got) != len(want) {
			t.Errorf("%s hosts: got %d %v, want %d", kind, len(got), got, len(want))
		}
		for host, w := range want {
			attempts := got[host]
			if len(attempts) != w.count {
				t.Errorf("%s[%s]: got %d attempts, want %d", kind, host, len(attempts), w.count)
				continue
			}
			final := attempts[len(attempts)-1]
			if final.Success != w.success || final.Iteration != w.iteration {
				t.Errorf("%s[%s] last attempt: got success=%v iteration=%d, want success=%v iteration=%d",
					kind, host, final.Success, final.Iteration, w.success, w.iteration)
			}
		}
	}

	replies := map[string]func(t *testing.T) *mcp.CallToolResult{
		"the develop briefing": func(t *testing.T) *mcp.CallToolResult {
			t.Helper()
			result, _, err := renderDevelopBriefing(context.Background(), eng, mock, "proj-3", rt, nil)
			if err != nil {
				t.Fatalf("renderDevelopBriefing: %v", err)
			}
			return result
		},
		"a prose mutation reply": func(t *testing.T) *mcp.CallToolResult {
			t.Helper()
			return withFreshEnvelope(context.Background(), textResult("closed"), dir, mock, "proj-3", rt)
		},
		"a JSON mutation reply": func(t *testing.T) *mcp.CallToolResult {
			t.Helper()
			env := freshEnvelope(context.Background(), dir, mock, "proj-3", rt)
			if env == nil {
				t.Fatalf("freshEnvelope: nil")
			}
			return textResult(workflow.AppendEnvelope("", *env))
		},
	}
	for name, reply := range replies {
		t.Run(name+" carries only the last attempt per host", func(t *testing.T) {
			t.Parallel()
			got := wireOf(t, reply(t))
			check(t, "deploys", got.Deploys, map[string]last{
				"apidev": {count: 1, success: true, iteration: 3},
				"webdev": {count: 1, success: false, iteration: 1},
			})
			check(t, "verifies", got.Verifies, map[string]last{
				"apidev": {count: 1, success: true, iteration: 2},
			})
		})
	}

	t.Run("action=status carries the whole history", func(t *testing.T) {
		t.Parallel()
		result, _, err := handleLifecycleStatus(context.Background(), eng, mock, "proj-3", rt, nil, nil)
		if err != nil {
			t.Fatalf("handleLifecycleStatus: %v", err)
		}
		got := wireOf(t, result)
		check(t, "deploys", got.Deploys, map[string]last{
			"apidev": {count: 3, success: true, iteration: 3},
			"webdev": {count: 1, success: false, iteration: 1},
		})
		check(t, "verifies", got.Verifies, map[string]last{
			"apidev": {count: 2, success: true, iteration: 2},
		})
	})
}

// TestConvertError_CarriesNoEnvelope — an error result is a leaf payload
// (spec-workflows P4). Appending an envelope there would let the reducer
// treat a failed call as fresh state.
func TestConvertError_CarriesNoEnvelope(t *testing.T) {
	t.Parallel()

	result := convertError(platform.NewPlatformError(
		platform.ErrNotImplemented, "boom", ""), WithRecoveryStatus())
	if _, ok := workflow.ExtractEnvelope(getTextContent(t, result)); ok {
		t.Errorf("error results must not carry an envelope block")
	}
}

// TestFreshEnvelope_CarriesLeases pins that the envelope a JSON tool carries
// reads the project's leases like action="status" does: a project that lists a
// pool reads them (none held is `"leases":[]`), a project that cannot hold a
// lease leaves the key absent.
func TestFreshEnvelope_CarriesLeases(t *testing.T) {
	t.Parallel()

	pool := platform.ServiceStack{ID: "svc-pool", Name: "devpool", Status: "ACTIVE",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}}
	tests := []struct {
		name     string
		services []platform.ServiceStack
		stateDir func(t *testing.T) string
		wantRead bool
		want     []string
	}{
		{"a pool and a lease record", []platform.ServiceStack{pool}, func(t *testing.T) string {
			t.Helper()
			dir := t.TempDir()
			if err := os.MkdirAll(filepath.Join(dir, "leases"), 0o700); err != nil {
				t.Fatal(err)
			}
			rec := `{"poolHostname":"devpool","containerNumber":3,"containerHostname":"node-id-3.runtime.devpool.zerops","branch":"feat-x","lease":"SECRET"}`
			if err := os.WriteFile(filepath.Join(dir, "leases", "devpool-3.json"), []byte(rec), 0o600); err != nil {
				t.Fatal(err)
			}
			return dir
		}, true, []string{"devpool-3"}},
		{"no pool and no record", nil, func(t *testing.T) string { t.Helper(); return t.TempDir() }, false, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := platform.NewMock().WithServices(tt.services).WithProject(&platform.Project{ID: "proj-l", Name: "p"})
			env := freshEnvelope(context.Background(), tt.stateDir(t), mock, "proj-l", runtime.Info{})
			if env == nil {
				t.Fatal("freshEnvelope: nil")
			}
			if (env.Leases != nil) != tt.wantRead {
				t.Fatalf("leases read = %v, want %v", env.Leases != nil, tt.wantRead)
			}
			got := make([]string, 0, len(env.LeaseList()))
			for _, l := range env.LeaseList() {
				got = append(got, l.Alias)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("leases = %v, want %v", got, tt.want)
			}
			raw, err := json.Marshal(env)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), "SECRET") {
				t.Errorf("the lease string reached the envelope: %s", raw)
			}
		})
	}
}
