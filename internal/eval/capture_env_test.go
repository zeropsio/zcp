package eval

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/capture"
)

func TestRunnerCaptureMCPConfig_UsesCurrentExecutableAndStrictSingleZeropsServer(t *testing.T) {
	t.Parallel()

	runner := NewRunner(RunnerConfig{Capture: &capture.Connection{CaptureID: "capture-eval", ProxyURL: "http://127.0.0.1:1", SessionDir: t.TempDir()}}, nil, nil, "project")
	outDir := t.TempDir()
	if err := runner.prepareCaptureMCPConfig(outDir); err != nil {
		t.Fatalf("prepareCaptureMCPConfig() error = %v", err)
	}
	data, err := os.ReadFile(runner.config.MCPConfig)
	if err != nil {
		t.Fatalf("read capture MCP config: %v", err)
	}
	var document struct {
		MCPServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("decode capture MCP config: %v", err)
	}
	server, ok := document.MCPServers["zerops"]
	if !ok || len(document.MCPServers) != 1 {
		t.Fatalf("MCP servers = %+v, want exactly zerops", document.MCPServers)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}
	if server.Command != executable || len(server.Args) != 1 || server.Args[0] != "serve" || !runner.strictMCPConfig {
		t.Fatalf("capture MCP server = %+v strict=%v, want current executable", server, runner.strictMCPConfig)
	}
}

func TestRunnerUserSimulator_CaptureDisabledPreservesInheritedHome(t *testing.T) {
	// non-parallel: t.Setenv mutates process environment.
	binaryDir := t.TempDir()
	observedPath := filepath.Join(t.TempDir(), "home.txt")
	script := "#!/bin/sh\nprintf '%s' \"$HOME\" > \"$OBSERVED_HOME_PATH\"\nprintf '%s\\n' '{\"type\":\"assistant\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"ok\"}]}}'\n"
	if err := os.WriteFile(filepath.Join(binaryDir, "claude"), []byte(script), 0o700); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}
	parentHome := filepath.Join(t.TempDir(), "parent-home")
	sandboxHome := filepath.Join(t.TempDir(), "sandbox-home")
	t.Setenv("HOME", parentHome)
	t.Setenv("PATH", binaryDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("OBSERVED_HOME_PATH", observedPath)

	runner := NewRunner(RunnerConfig{ClaudeHome: sandboxHome, Capture: nil}, nil, nil, "project")
	if _, err := runner.userSimRunner(&Scenario{}).Reply(context.Background(), "probe"); err != nil {
		t.Fatalf("user simulator reply: %v", err)
	}
	observed, err := os.ReadFile(observedPath)
	if err != nil {
		t.Fatalf("read observed HOME: %v", err)
	}
	if string(observed) != parentHome {
		t.Fatalf("capture-disabled user simulator HOME = %q, want inherited %q", observed, parentHome)
	}
}

func TestRunnerClaudeEnv_ActiveCaptureOverridesProviderForEveryClaudePath(t *testing.T) {
	// non-parallel: t.Setenv mutates process environment.
	t.Setenv("ANTHROPIC_BASE_URL", "https://old.example")
	runner := NewRunner(RunnerConfig{
		WorkDir: "/tmp/work",
		Capture: &capture.Connection{
			CaptureID:  "capture-eval",
			ProxyURL:   "http://127.0.0.1:43210",
			SessionDir: "/private/capture-eval",
		},
	}, nil, nil, "project")

	environment := runner.claudeEnvForScope(captureProcessScope{evalRunID: "suite-1", scenarioRunID: "weather", invocationID: "weather/agent.initial", phase: "agent.initial"})
	joined := strings.Join(environment, "\n")
	for _, want := range []string{
		"ANTHROPIC_BASE_URL=http://127.0.0.1:43210",
		capture.EnvSessionID + "=capture-eval",
		capture.EnvSessionDir + "=/private/capture-eval",
		capture.EnvEvalRunID + "=suite-1",
		capture.EnvScenarioRunID + "=weather",
		capture.EnvInvocationID + "=weather/agent.initial",
		capture.EnvInvocationPhase + "=agent.initial",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("claudeEnv() missing %q in %v", want, environment)
		}
	}
	if strings.Contains(joined, "ANTHROPIC_BASE_URL=https://old.example") {
		t.Fatalf("claudeEnv() retained old provider URL: %v", environment)
	}
}
