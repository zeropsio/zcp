package eval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/zeropsio/zcp/internal/capture"
)

type captureMCPConfigDocument struct {
	MCPServers map[string]captureMCPServer `json:"mcpServers"`
}

type captureMCPServer struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

func (r *Runner) prepareCaptureMCPConfig(outputDir string) error {
	if r.config.Capture == nil {
		return nil
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve current zcp executable for capture MCP: %w", err)
	}
	document := captureMCPConfigDocument{MCPServers: map[string]captureMCPServer{
		"zerops": {Command: executable, Args: []string{"serve"}},
	}}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return fmt.Errorf("encode capture MCP config: %w", err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		return fmt.Errorf("create capture MCP config directory: %w", err)
	}
	path := filepath.Join(outputDir, "capture-mcp.json")
	temp, err := os.CreateTemp(outputDir, ".capture-mcp-*.tmp")
	if err != nil {
		return fmt.Errorf("create capture MCP config temp: %w", err)
	}
	tempPath := temp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return fmt.Errorf("set capture MCP config permissions: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write capture MCP config: %w", err)
	}
	if err := errors.Join(temp.Sync(), temp.Close()); err != nil {
		return fmt.Errorf("close capture MCP config: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace capture MCP config: %w", err)
	}
	removeTemp = false
	r.config.MCPConfig = path
	r.strictMCPConfig = true
	return nil
}

func (r *Runner) appendMCPConfigArgs(args []string) []string {
	if r.config.MCPConfig == "" {
		return args
	}
	args = append(args, "--mcp-config", r.config.MCPConfig)
	if r.strictMCPConfig {
		args = append(args, "--strict-mcp-config")
	}
	return args
}

func (r *Runner) captureMark(ctx context.Context, marker capture.LifecycleMarker) {
	if r.config.Capture == nil {
		return
	}
	markerCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 500*time.Millisecond)
	defer cancel()
	if _, err := r.config.Capture.Mark(markerCtx, marker); err != nil {
		// Capture annotation is a side channel. Its failure must be visible but
		// must not change the evaluated agent's control flow or verdict.
		fmt.Fprintf(os.Stderr, "warning: capture lifecycle %s: %v\n", marker.Kind, err)
	}
}

func (r *Runner) beginCaptureEvalRun(ctx context.Context, evalRunID string) {
	r.captureMark(ctx, capture.LifecycleMarker{Kind: capture.LifecycleEvalRunStart, EvalRunID: evalRunID})
}

func (r *Runner) endCaptureEvalRun(ctx context.Context, evalRunID, status string, runErr error) {
	marker := capture.LifecycleMarker{Kind: capture.LifecycleEvalRunEnd, EvalRunID: evalRunID, Status: status}
	if runErr != nil {
		marker.Error = runErr.Error()
	}
	r.captureMark(ctx, marker)
}

func (r *Runner) captureScenarioStart(ctx context.Context, evalRunID, scenarioRunID string) {
	r.captureMark(ctx, capture.LifecycleMarker{Kind: capture.LifecycleScenarioStart, EvalRunID: evalRunID, ScenarioRunID: scenarioRunID})
}

func (r *Runner) captureScenarioEnd(ctx context.Context, evalRunID, scenarioRunID, status string, scenarioErr error) {
	marker := capture.LifecycleMarker{Kind: capture.LifecycleScenarioEnd, EvalRunID: evalRunID, ScenarioRunID: scenarioRunID, Status: status}
	if scenarioErr != nil {
		marker.Error = scenarioErr.Error()
	}
	r.captureMark(ctx, marker)
}

func (r *Runner) finishBehavioralCapture(ctx context.Context, evalRunID, scenarioRunID, scenarioPath, outputDir string, result *BehavioralResult, returnErr error) {
	status := capture.CaptureComplete
	scenarioErr := returnErr
	if scenarioErr != nil || result != nil && result.Error != "" {
		status = capture.CapturePartial
		if scenarioErr == nil {
			scenarioErr = fmt.Errorf("%s", result.Error)
		}
	}
	if r.config.Capture != nil {
		paths, bundleErr := capture.BundleEvalScenario(r.config.Capture.SessionDir, evalRunID, scenarioRunID, scenarioPath, outputDir)
		if bundleErr != nil {
			fmt.Fprintf(os.Stderr, "warning: capture eval artifacts: %v\n", bundleErr)
			status = capture.CapturePartial
			if scenarioErr == nil {
				scenarioErr = bundleErr
			}
		}
		for _, path := range paths {
			r.captureMark(ctx, capture.LifecycleMarker{
				Kind: capture.LifecycleArtifact, EvalRunID: evalRunID, ScenarioRunID: scenarioRunID, ArtifactPath: path,
			})
		}
	}
	r.captureScenarioEnd(ctx, evalRunID, scenarioRunID, status, scenarioErr)
}

type capturedInvocation struct {
	runner        *Runner
	evalRunID     string
	scenarioRunID string
	invocationID  string
	phase         string
	endOnce       sync.Once
}

func (r *Runner) captureInvocationStart(ctx context.Context, evalRunID, scenarioRunID, invocationID, phase, knownSessionID string) *capturedInvocation {
	invocation := &capturedInvocation{
		runner:        r,
		evalRunID:     evalRunID,
		scenarioRunID: scenarioRunID,
		invocationID:  invocationID,
		phase:         phase,
	}
	r.captureMark(ctx, capture.LifecycleMarker{
		Kind:          capture.LifecycleInvocationStart,
		EvalRunID:     evalRunID,
		ScenarioRunID: scenarioRunID,
		InvocationID:  invocationID,
		Phase:         phase,
	})
	if knownSessionID != "" {
		invocation.Bind(ctx, knownSessionID)
	}
	return invocation
}

func (i *capturedInvocation) Bind(ctx context.Context, claudeSessionID string) {
	if i == nil || claudeSessionID == "" {
		return
	}
	i.runner.captureMark(ctx, capture.LifecycleMarker{
		Kind:            capture.LifecycleInvocationBind,
		EvalRunID:       i.evalRunID,
		ScenarioRunID:   i.scenarioRunID,
		InvocationID:    i.invocationID,
		Phase:           i.phase,
		ClaudeSessionID: claudeSessionID,
	})
}

func (i *capturedInvocation) End(ctx context.Context, status string, invocationErr error) {
	if i == nil {
		return
	}
	i.endOnce.Do(func() {
		marker := capture.LifecycleMarker{
			Kind:          capture.LifecycleInvocationEnd,
			EvalRunID:     i.evalRunID,
			ScenarioRunID: i.scenarioRunID,
			InvocationID:  i.invocationID,
			Phase:         i.phase,
			Status:        status,
		}
		if invocationErr != nil {
			marker.Error = invocationErr.Error()
		}
		i.runner.captureMark(ctx, marker)
	})
}

// BeginCaptureEvalRun and EndCaptureEvalRun are command-boundary hooks. The
// scenario runner owns nested scenario/invocation markers.
func (r *Runner) BeginCaptureEvalRun(ctx context.Context, evalRunID string) {
	r.beginCaptureEvalRun(ctx, evalRunID)
}

func (r *Runner) EndCaptureEvalRun(ctx context.Context, evalRunID, status string, runErr error) {
	r.endCaptureEvalRun(ctx, evalRunID, status, runErr)
}

type capturedUserSimRunner struct {
	runner        *Runner
	delegate      UserSimRunner
	evalRunID     string
	scenarioRunID string
	iteration     int
}

func (r *Runner) captureUserSimRunner(_ *Scenario, evalRunID, scenarioRunID string, delegate UserSimRunner) UserSimRunner {
	if r.config.Capture == nil {
		return delegate
	}
	return &capturedUserSimRunner{runner: r, delegate: delegate, evalRunID: evalRunID, scenarioRunID: scenarioRunID}
}

func (c *capturedUserSimRunner) Reply(ctx context.Context, prompt string) (string, error) {
	c.iteration++
	phase := fmt.Sprintf("user-sim.%d", c.iteration)
	invocation := c.runner.captureInvocationStart(ctx, c.evalRunID, c.scenarioRunID, c.scenarioRunID+"/"+phase, phase, "")
	var (
		reply     string
		sessionID string
		err       error
	)
	if claudeRunner, ok := c.delegate.(*claudeUserSimRunner); ok {
		scopedRunner := *claudeRunner
		scopedRunner.environment = c.runner.claudeEnvForScope(captureProcessScope{
			evalRunID: c.evalRunID, scenarioRunID: c.scenarioRunID, invocationID: c.scenarioRunID + "/" + phase, phase: phase,
		})
		reply, sessionID, err = scopedRunner.ReplyWithSession(ctx, prompt)
	} else if sessionRunner, ok := c.delegate.(userSimSessionRunner); ok {
		reply, sessionID, err = sessionRunner.ReplyWithSession(ctx, prompt)
	} else {
		reply, err = c.delegate.Reply(ctx, prompt)
	}
	if sessionID != "" {
		invocation.Bind(ctx, sessionID)
	}
	status := capture.CaptureComplete
	if err != nil {
		status = capture.CapturePartial
	}
	invocation.End(ctx, status, err)
	return reply, err
}
