package eval

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/capture"
	"github.com/zeropsio/zcp/internal/platform"
)

// TestExecutionBinding_UnsafeOrOverlappingPaths_Refused pins
// docs/spec-testing-architecture.md §10.4 "Preflight": work dir and results
// dir must be absolute, distinct, not nested in each other, not "/", and
// not a home directory root.
func TestExecutionBinding_UnsafeOrOverlappingPaths_Refused(t *testing.T) {
	t.Parallel()
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no resolvable home directory in this environment")
	}
	root := t.TempDir()
	work := filepath.Join(root, "work")
	results := filepath.Join(root, "results")

	cases := []struct {
		name    string
		workDir string
		results string
	}{
		{"nested: results inside work", work, filepath.Join(work, "results")},
		{"nested: work inside results", filepath.Join(results, "work"), results},
		{"equal", work, work},
		{"relative work dir", "relative/work", results},
		{"relative results dir", work, "relative/results"},
		{"work dir is root", "/", results},
		{"results dir is root", work, "/"},
		{"work dir is home", home, results},
		{"results dir is home", work, home},
		{"nested: dot-named results inside work", work, filepath.Join(work, ".results")},
		{"nested: dot-named work inside results", filepath.Join(results, ".work"), results},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := assertSafeRoots(tc.workDir, tc.results); err == nil {
				t.Fatalf("assertSafeRoots(%q, %q) = nil, want an error", tc.workDir, tc.results)
			}
		})
	}

	if err := assertSafeRoots(work, results); err != nil {
		t.Fatalf("assertSafeRoots(%q, %q) = %v, want nil for two distinct absolute non-nested dirs", work, results, err)
	}
}

// TestExecutionBinding_CandidateMismatch_ZeroMutation pins
// docs/spec-testing-architecture.md §10.4 "Preflight — zero mutation": a
// candidate whose SHA-256 doesn't match --candidate-sha256 is refused
// before ListServicesDirect/DeleteService are ever called, before the
// scenario.start marker, and meta.json is the only frozen artifact.
func TestExecutionBinding_CandidateMismatch_ZeroMutation(t *testing.T) { // non-parallel: process environment
	h := newBehavioralHarness(t)
	h.writeClaudeScript(t, defaultFakeClaudeDone)
	candidate, _ := h.writeFakeCandidate(t, "#!/bin/sh\nexit 0\n")
	scenarioPath := h.writeScenario(t, requiredExpectedAppScenario("binding-sha-mismatch"))

	client := platform.NewMock()
	sessionDir := t.TempDir()
	cfg := h.config()
	cfg.Capture = realCaptureConnection(t, sessionDir)
	cfg.CaptureOwned = true
	cfg.Binding = h.binding(candidate, "0000000000000000000000000000000000000000000000000000000000000000"[:64])
	runner := NewRunner(cfg, nil, client, "offline-project")

	result, err := runner.RunBehavioralScenario(context.Background(), scenarioPath, "suite")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(result.Error, "binding:") {
		t.Fatalf("result.Error = %q, want prefix 'binding:'", result.Error)
	}
	if result.Task == nil || result.Task.Result != CheckNotRun {
		t.Fatalf("Task = %+v, want not-run", result.Task)
	}
	if got := client.CallCounts["ListServicesDirect"]; got != 0 {
		t.Errorf("ListServicesDirect calls = %d, want 0", got)
	}
	if got := client.CallCounts["DeleteService"]; got != 0 {
		t.Errorf("DeleteService calls = %d, want 0", got)
	}
	if kinds := lifecycleKinds(t, sessionDir); kinds[capture.LifecycleScenarioStart] {
		t.Error("scenario.start marker present, want none for a zero-mutation refusal")
	}
	outDir := h.outDir("binding-sha-mismatch")
	if got := listFiles(t, outDir); len(got) != 1 || got[0] != "meta.json" {
		t.Fatalf("result dir files = %v, want only meta.json", got)
	}
}

// TestExecutionBinding_NonFreshTarget_ZeroMutation pins the fresh-target
// half of the preflight: a non-system, non-protected service already
// present refuses the run, with reads allowed (list=1) but zero deletes.
func TestExecutionBinding_NonFreshTarget_ZeroMutation(t *testing.T) { // non-parallel: process environment
	h := newBehavioralHarness(t)
	h.writeClaudeScript(t, defaultFakeClaudeDone)
	candidate, sha := h.writeFakeCandidate(t, "#!/bin/sh\nexit 0\n")
	scenarioPath := h.writeScenario(t, requiredExpectedAppScenario("binding-not-fresh"))

	client := platform.NewMock().WithServicesDirect([]platform.ServiceStack{
		{ID: "zcp-1", Name: ProtectedService, Status: "ACTIVE"},
		{ID: "app-1", Name: "app", Status: "ACTIVE"},
	})
	cfg := h.config()
	cfg.Capture = &capture.Connection{CaptureID: "owned", ProxyURL: "http://127.0.0.1:1", SessionDir: t.TempDir()}
	cfg.CaptureOwned = true
	cfg.Binding = h.binding(candidate, sha)
	runner := NewRunner(cfg, nil, client, "offline-project")

	result, err := runner.RunBehavioralScenario(context.Background(), scenarioPath, "suite")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(result.Error, "binding:") {
		t.Fatalf("result.Error = %q, want prefix 'binding:'", result.Error)
	}
	if got := client.CallCounts["ListServicesDirect"]; got != 1 {
		t.Errorf("ListServicesDirect calls = %d, want 1", got)
	}
	if got := client.CallCounts["GetProjectProcessesDirect"]; got > 1 {
		t.Errorf("GetProjectProcessesDirect calls = %d, want <= 1", got)
	}
	if got := client.CallCounts["DeleteService"]; got != 0 {
		t.Errorf("DeleteService calls = %d, want 0", got)
	}
}

// TestExecutionBinding_RequiredWithoutBinding_Refused pins §10.4 "A required
// scenario refuses to start without a complete binding": an owned capture
// window is not enough on its own — refused before any platform call.
func TestExecutionBinding_RequiredWithoutBinding_Refused(t *testing.T) { // non-parallel: process environment
	h := newBehavioralHarness(t)
	h.writeClaudeScript(t, defaultFakeClaudeDone)
	scenarioPath := h.writeScenario(t, requiredExpectedAppScenario("binding-missing"))

	client := platform.NewMock()
	cfg := h.config()
	cfg.Capture = &capture.Connection{CaptureID: "owned", ProxyURL: "http://127.0.0.1:1", SessionDir: t.TempDir()}
	cfg.CaptureOwned = true
	runner := NewRunner(cfg, nil, client, "offline-project")

	result, err := runner.RunBehavioralScenario(context.Background(), scenarioPath, "suite")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Error, "explicit binding") {
		t.Errorf("result.Error = %q, want mention of an explicit binding", result.Error)
	}
	for method, count := range client.CallCounts {
		if count != 0 {
			t.Errorf("platform call %s made %d times, want 0", method, count)
		}
	}
}

// TestExecutionBinding_CandidateOwnsAgentSurface pins §10.4 "Candidate owns
// the agent surface": `<candidate> init` (not the evaluator's own
// initcmd.Run) writes the agent-facing files, every claude child's PATH
// starts with PrivateBin and HOME is ClaudeHome with ZCP_AUTO_UPDATE=0, and
// the capture MCP config names the candidate by absolute path.
func TestExecutionBinding_CandidateOwnsAgentSurface(t *testing.T) { // non-parallel: process environment
	h := newBehavioralHarness(t)
	candidate, sha := h.writeFakeCandidate(t, "#!/bin/sh\nif [ \"$1\" = \"init\" ]; then\n  printf 'CANDIDATE-INIT\\n' > AGENTS.md\n  exit 0\nfi\nexit 1\n")

	envDump := filepath.Join(h.root, "claude-env-dump.txt")
	t.Setenv("CLAUDE_ENV_DUMP", envDump)
	t.Setenv("RETRO_MARKER", filepath.Join(h.root, "retro-marker"))
	h.writeClaudeScript(t, `#!/bin/sh
if [ -n "$CLAUDE_ENV_DUMP" ]; then
  { echo "PATH=$PATH"; echo "HOME=$HOME"; echo "ZCP_AUTO_UPDATE=$ZCP_AUTO_UPDATE"; } > "$CLAUDE_ENV_DUMP"
fi
`+defaultFakeClaudeDone[len("#!/bin/sh\n"):])

	scenarioPath := h.writeScenario(t, requiredExpectedAppScenario("binding-owns-surface"))
	client := platform.NewMock()
	cfg := h.config()
	cfg.Capture = &capture.Connection{CaptureID: "owned", ProxyURL: "http://127.0.0.1:1", SessionDir: t.TempDir()}
	cfg.CaptureOwned = true
	cfg.Binding = h.binding(candidate, sha)
	cfg.ClaudeHome = cfg.Binding.ClaudeHome // production wiring: cmd/zcp's initEvalRunnerFor sets ClaudeHome from the binding when bound
	runner := NewRunner(cfg, nil, client, "offline-project")

	result, err := runner.RunBehavioralScenario(context.Background(), scenarioPath, "suite")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("result.Error = %q, want empty", result.Error)
	}

	agentsMD, err := os.ReadFile(filepath.Join(h.work, "AGENTS.md"))
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	if !strings.Contains(string(agentsMD), "CANDIDATE-INIT") {
		t.Errorf("AGENTS.md = %q, want it written by the candidate's own init", agentsMD)
	}
	claudeMDMarker := filepath.Join(h.work, "CLAUDE.md")
	if data, err := os.ReadFile(claudeMDMarker); err == nil && strings.Contains(string(data), "REFLOG") {
		t.Errorf("CLAUDE.md contains a REFLOG managed-section marker, want none — evaluator initcmd.Run must not have run: %q", data)
	}

	envDumpData, err := os.ReadFile(envDump)
	if err != nil {
		t.Fatalf("read claude env dump: %v", err)
	}
	dump := string(envDumpData)
	if !strings.Contains(dump, "PATH="+cfg.Binding.PrivateBin) {
		t.Errorf("claude env dump PATH does not start with PrivateBin %q:\n%s", cfg.Binding.PrivateBin, dump)
	}
	if !strings.Contains(dump, "HOME="+cfg.Binding.ClaudeHome) {
		t.Errorf("claude env dump HOME != ClaudeHome %q:\n%s", cfg.Binding.ClaudeHome, dump)
	}
	if !strings.Contains(dump, "ZCP_AUTO_UPDATE=0") {
		t.Errorf("claude env dump missing ZCP_AUTO_UPDATE=0:\n%s", dump)
	}

	mcpConfigData, err := os.ReadFile(filepath.Join(h.outDir("binding-owns-surface"), "capture-mcp.json"))
	if err != nil {
		t.Fatalf("read capture-mcp.json: %v", err)
	}
	if !strings.Contains(string(mcpConfigData), candidate) {
		t.Errorf("capture-mcp.json does not name the candidate %q:\n%s", candidate, mcpConfigData)
	}
}
