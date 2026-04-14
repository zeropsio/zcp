package ops

import (
	"context"
	"time"
)

// DeployResult contains the outcome of a deploy operation (shared by SSH and local modes).
type DeployResult struct {
	Status            string   `json:"status"`
	Mode              string   `json:"mode"` // "ssh" or "local"
	SourceService     string   `json:"sourceService,omitempty"`
	TargetService     string   `json:"targetService"`
	TargetServiceID   string   `json:"targetServiceId"`
	TargetServiceType string   `json:"targetServiceType,omitempty"`
	Message           string   `json:"message"`
	MonitorHint       string   `json:"monitorHint,omitempty"`
	BuildStatus       string   `json:"buildStatus,omitempty"`
	BuildDuration     string   `json:"buildDuration,omitempty"`
	Suggestion        string   `json:"suggestion,omitempty"`
	SSHReady          bool     `json:"sshReady,omitempty"`
	TimedOut          bool     `json:"timedOut,omitempty"`
	NextActions       string   `json:"nextActions,omitempty"`
	Warnings          []string `json:"warnings,omitempty"`
	BuildLogs         []string `json:"buildLogs,omitempty"`       // last N lines of build container output (BUILD_FAILED, PREPARING_RUNTIME_FAILED)
	BuildLogsSource   string   `json:"buildLogsSource,omitempty"` // "build_container" or empty
	RuntimeLogs       []string `json:"runtimeLogs,omitempty"`     // last N lines of runtime container output (DEPLOY_FAILED — initCommand stderr)
	RuntimeLogsSource string   `json:"runtimeLogsSource,omitempty"`
	FailedPhase       string   `json:"failedPhase,omitempty"` // "build" | "prepare" | "init" — which lifecycle phase failed
}

// GitPushResult contains the outcome of a git-push deploy operation.
type GitPushResult struct {
	Status    string   `json:"status"`              // "PUSHED" or "NOTHING_TO_PUSH"
	RemoteURL string   `json:"remoteUrl,omitempty"` // The remote URL used
	Branch    string   `json:"branch"`              // Branch pushed to
	Message   string   `json:"message"`             // Human-readable summary
	Warnings  []string `json:"warnings,omitempty"`  // Non-fatal warnings
}

// SSHDeployer executes commands on remote Zerops services.
//
// Two exec shapes live on this interface intentionally:
//
//   - ExecSSH is the foreground pattern: synchronous, bounded by a
//     deployer-wide ceiling (5 min), used for deploys / probes / tails
//     where the caller cares about exit status and full output.
//   - ExecSSHBackground is a fire-and-forget spawn: tighter per-call
//     timeout, extra ssh flags (-T -n BatchMode) to keep the channel
//     from lingering after the remote shell exits. Required for
//     dev-server style "spawn and detach" operations.
//
// Every implementation MUST implement both — test doubles in sibling
// packages embed noopExecSSHBackground to get a compliant stub for free.
type SSHDeployer interface {
	ExecSSH(ctx context.Context, hostname string, command string) ([]byte, error)
	ExecSSHBackground(ctx context.Context, hostname string, command string, timeout time.Duration) ([]byte, error)
}
