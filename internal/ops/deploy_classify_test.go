// Tests for: ops/deploy_classify.go — SSH error classification.
package ops

import (
	"fmt"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestClassifySSHError_OSLevel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		sshErr      *platform.SSHExecError
		wantMessage string
	}{
		{
			name: "signal killed → OOM",
			sshErr: &platform.SSHExecError{
				Hostname: "builder",
				Output:   "building...\nKilled",
				Err:      fmt.Errorf("signal: killed"),
			},
			wantMessage: "killed (likely OOM)",
		},
		{
			name: "connection refused",
			sshErr: &platform.SSHExecError{
				Hostname: "builder",
				Output:   "",
				Err:      fmt.Errorf("dial tcp 10.0.0.5:22: connection refused"),
			},
			wantMessage: "cannot reach source service",
		},
		{
			name: "no route to host",
			sshErr: &platform.SSHExecError{
				Hostname: "builder",
				Output:   "",
				Err:      fmt.Errorf("connect: no route to host"),
			},
			wantMessage: "cannot reach source service",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pe := classifySSHError(tt.sshErr, "builder", "app")
			if pe.Code != platform.ErrSSHDeployFailed {
				t.Errorf("code = %s, want %s", pe.Code, platform.ErrSSHDeployFailed)
			}
			if !contains(pe.Message, tt.wantMessage) {
				t.Errorf("message %q should contain %q", pe.Message, tt.wantMessage)
			}
		})
	}
}

func TestClassifySSHError_Default_ShowsOutput(t *testing.T) {
	t.Parallel()

	sshErr := &platform.SSHExecError{
		Hostname: "builder",
		Output:   "step 1: ok\nstep 2: ok\nERR: authentication failed\n",
		Err:      fmt.Errorf("exit status 1"),
	}

	pe := classifySSHError(sshErr, "builder", "app")
	if pe.Code != platform.ErrSSHDeployFailed {
		t.Errorf("code = %s, want %s", pe.Code, platform.ErrSSHDeployFailed)
	}
	// Default case should show last lines of output in message.
	if !contains(pe.Message, "authentication failed") {
		t.Errorf("message should contain actual error from output, got: %s", pe.Message)
	}
	// Diagnostic should contain full output.
	if pe.Diagnostic != sshErr.Output {
		t.Errorf("diagnostic should be full output, got: %s", pe.Diagnostic)
	}
}

func TestClassifySSHError_NoFalsePositive_ZeropsYml(t *testing.T) {
	t.Parallel()

	// This is the exact scenario from forensic analysis: zcli output mentions
	// "zerops.yml" in a progress line, but the actual error is different.
	// Old code would match "zerops.yml" and return "zerops.yml not found".
	// New code should show the actual error.
	sshErr := &platform.SSHExecError{
		Hostname: "zcp",
		Output:   "Using config file: /home/zerops/.config/zerops/.zcli.yml\n✓ Parsing zerops.yml\n✗ ERR allowed only in interactive terminal\n",
		Err:      fmt.Errorf("exit status 1"),
	}

	pe := classifySSHError(sshErr, "zcp", "app")

	// Must NOT say "zerops.yml not found".
	if contains(pe.Message, "zerops.yml not found") {
		t.Errorf("FALSE POSITIVE: message says 'zerops.yml not found' but actual error is 'interactive terminal'.\nMessage: %s", pe.Message)
	}

	// Must contain the actual error.
	if !contains(pe.Message, "interactive terminal") {
		t.Errorf("message should contain actual error 'interactive terminal', got: %s", pe.Message)
	}

	// Diagnostic must contain full output.
	if !contains(pe.Diagnostic, "Parsing zerops.yml") {
		t.Errorf("diagnostic should contain full zcli output, got: %s", pe.Diagnostic)
	}
}

func TestClassifySSHError_DiagnosticAlwaysSet(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		output string
	}{
		{"with output", "some output\nmore lines\n"},
		{"empty output", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sshErr := &platform.SSHExecError{
				Hostname: "builder",
				Output:   tt.output,
				Err:      fmt.Errorf("exit status 1"),
			}
			pe := classifySSHError(sshErr, "builder", "app")
			if pe.Diagnostic != tt.output {
				t.Errorf("diagnostic = %q, want %q", pe.Diagnostic, tt.output)
			}
		})
	}
}

func TestClassifySSHError_NonSSHError(t *testing.T) {
	t.Parallel()

	// Non-SSHExecError (e.g., from service resolution) should pass through.
	err := fmt.Errorf("service 'missing' not found")
	pe := classifySSHError(err, "builder", "app")

	if pe.Code != platform.ErrSSHDeployFailed {
		t.Errorf("code = %s, want %s", pe.Code, platform.ErrSSHDeployFailed)
	}
	if !contains(pe.Message, "not found") {
		t.Errorf("message should contain original error, got: %s", pe.Message)
	}
}

func TestLastLines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		n     int
		want  string
	}{
		{"fewer than n", "line1\nline2", 5, "line1\nline2"},
		{"exactly n", "a\nb\nc", 3, "a\nb\nc"},
		{"more than n", "a\nb\nc\nd\ne", 2, "d\ne"},
		{"trailing newlines", "a\nb\nc\n\n", 2, "b\nc"},
		{"empty", "", 3, ""},
		{"single line", "hello", 3, "hello"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := lastLines(tt.input, tt.n)
			if got != tt.want {
				t.Errorf("lastLines(%q, %d) = %q, want %q", tt.input, tt.n, got, tt.want)
			}
		})
	}
}

// isSSHBuildTriggered tests in deploy_ssh_test.go.
// contains helper in deploy_git_test.go.
