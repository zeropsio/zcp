package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
	git "github.com/zeropsio/zcp/internal/ops/git"
	"github.com/zeropsio/zcp/internal/topology"
)

// writeDeployLedgerLocal records a successful zcp deploy as an annotated
// git tag in the SOURCE repo (docs/spec-workflows.md §4.9). No-op when the
// deploy never reached a resolved appVersion, or has no SHA at all (no
// explicit sha AND the source has no git repo with a HEAD) — a ledger
// write failure is a warning, never a deploy failure: the build already
// succeeded.
func writeDeployLedgerLocal(ctx context.Context, workingDir, projectID string, result *ops.DeployResult) {
	if result == nil || result.SHA == "" || result.AppVersionID == "" {
		return
	}
	dir := workingDir
	if dir == "" {
		dir = "."
	}
	entry := topology.LedgerEntry{
		SHA:          result.SHA,
		AppVersionID: result.AppVersionID,
		Target:       result.TargetService,
		Project:      projectID,
		At:           time.Now().UTC().Format(time.RFC3339),
		Dirty:        result.Dirty,
	}
	if err := git.WriteLedger(ctx, git.LocalRunner{}, dir, entry); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("ledger write failed: %v", err))
	}
}

// writeDeployLedgerSSH is the SSH-mode twin of writeDeployLedgerLocal: the
// ledger lives in the SOURCE container (result.SourceService), reached over
// SSH — never the SSHFS mount (docs/spec-mate.md §"git").
func writeDeployLedgerSSH(ctx context.Context, sshDeployer ops.SSHDeployer, workingDir, projectID string, result *ops.DeployResult) {
	if result == nil || result.SHA == "" || result.AppVersionID == "" || sshDeployer == nil {
		return
	}
	dir := workingDir
	if dir == "" {
		dir = ops.DefaultWorkingDir
	}
	hostname := result.SourceService
	if hostname == "" {
		hostname = result.TargetService
	}
	entry := topology.LedgerEntry{
		SHA:          result.SHA,
		AppVersionID: result.AppVersionID,
		Target:       result.TargetService,
		Project:      projectID,
		At:           time.Now().UTC().Format(time.RFC3339),
		Dirty:        result.Dirty,
	}
	runner := git.SSHRunner{Executor: sshDeployer, Hostname: hostname}
	if err := git.WriteLedger(ctx, runner, dir, entry); err != nil {
		result.Warnings = append(result.Warnings, withSSHStderr("ledger write failed", err))
	}
}
