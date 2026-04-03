package ops

import (
	"context"
	"fmt"
	"strings"
)

// GitStatus describes the git state of a service's /var/www directory.
type GitStatus struct {
	HasGit       bool   `json:"hasGit"`
	HasRemote    bool   `json:"hasRemote"`
	RemoteURL    string `json:"remoteUrl,omitempty"`
	Branch       string `json:"branch,omitempty"`
	HasZeropsYml bool   `json:"hasZeropsYml"`
	IsDirty      bool   `json:"isDirty"`
}

// CheckGitStatus inspects the git state of /var/www on a running container via SSH.
func CheckGitStatus(ctx context.Context, ssh SSHDeployer, hostname string) (*GitStatus, error) {
	cmd := `cd /var/www && ` +
		`echo "GIT_DIR=$(test -d .git && echo yes || echo no)" && ` +
		`echo "REMOTE=$(git remote get-url origin 2>/dev/null || echo '')" && ` +
		`echo "BRANCH=$(git branch --show-current 2>/dev/null || echo '')" && ` +
		`echo "DIRTY=$(git status --porcelain 2>/dev/null | head -1)" && ` +
		`echo "ZEROPS_YML=$(test -f zerops.yml && echo yes || test -f zerops.yaml && echo yes || echo no)"`

	output, err := ssh.ExecSSH(ctx, hostname, cmd)
	if err != nil {
		return nil, fmt.Errorf("check git status on %s: %w", hostname, err)
	}

	return parseGitStatus(string(output)), nil
}

// parseGitStatus extracts GitStatus fields from the SSH command output.
func parseGitStatus(output string) *GitStatus {
	gs := &GitStatus{}
	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "GIT_DIR="):
			gs.HasGit = strings.TrimPrefix(line, "GIT_DIR=") == "yes"
		case strings.HasPrefix(line, "REMOTE="):
			remote := strings.TrimPrefix(line, "REMOTE=")
			if remote != "" {
				gs.HasRemote = true
				gs.RemoteURL = remote
			}
		case strings.HasPrefix(line, "BRANCH="):
			gs.Branch = strings.TrimPrefix(line, "BRANCH=")
		case strings.HasPrefix(line, "DIRTY="):
			gs.IsDirty = strings.TrimPrefix(line, "DIRTY=") != ""
		case strings.HasPrefix(line, "ZEROPS_YML="):
			gs.HasZeropsYml = strings.TrimPrefix(line, "ZEROPS_YML=") == "yes"
		}
	}
	return gs
}
