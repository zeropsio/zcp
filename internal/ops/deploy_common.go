package ops

import "context"

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
	BuildLogs         []string `json:"buildLogs,omitempty"`       // last N lines of build output
	BuildLogsSource   string   `json:"buildLogsSource,omitempty"` // "build_container" or empty
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
type SSHDeployer interface {
	ExecSSH(ctx context.Context, hostname string, command string) ([]byte, error)
}
