package ops

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/zeropsio/zcp/internal/devsup"
	"github.com/zeropsio/zcp/internal/mate"
)

type copyStateSSH interface {
	ExecSSHStdio(context.Context, string, string, io.Reader, io.Writer, io.Writer) (int, error)
}

// copyServerState is one read-only snapshot or one held supervisor wait.
// The wait uses the SSH transport without ExecSSH's five-minute ceiling.
func copyServerState(ctx context.Context, ssh SSHDeployer, host string, after devsup.Status) (devsup.Status, error) {
	cmd := "zcp-devsup ctl copy-state"
	if after != "" {
		cmd += " --after " + shellQuote(string(after))
	}
	cmd += " --json"
	var output []byte
	if after == "" {
		readCtx, cancel := context.WithTimeout(ctx, devsupCtlTimeout)
		defer cancel()
		out, err := ssh.ExecSSH(readCtx, host, cmd)
		if err != nil {
			return "", fmt.Errorf("copy state snapshot: %w", err)
		}
		output = out
	} else {
		stream, ok := ssh.(copyStateSSH)
		if !ok {
			return "", fmt.Errorf("copy state wait: ssh transport cannot hold a wait")
		}
		var stdout, stderr bytes.Buffer
		code, err := stream.ExecSSHStdio(ctx, host, cmd, nil, &stdout, &stderr)
		if err != nil {
			return "", fmt.Errorf("copy state wait: %w", err)
		}
		if code != 0 {
			return "", fmt.Errorf("copy state wait: supervisor exited %d", code)
		}
		output = stdout.Bytes()
	}
	var answer struct {
		Status devsup.Status `json:"status"`
	}
	if err := json.Unmarshal(output, &answer); err != nil {
		return "", fmt.Errorf("copy state answer: %w", err)
	}
	switch answer.Status {
	case devsup.StatusStarting, devsup.StatusRunning, devsup.StatusStopped, devsup.StatusCrashed:
		return answer.Status, nil
	default:
		return "", fmt.Errorf("copy state answer: invalid status %q", answer.Status)
	}
}

func copyWaitAfter(state mate.CopyState) devsup.Status {
	switch state {
	case mate.CopyStarting:
		return devsup.StatusStarting
	case mate.CopyRunning:
		return devsup.StatusRunning
	case mate.CopyStopped:
		return devsup.StatusStopped
	case mate.CopyFailed:
		return devsup.StatusCrashed
	case mate.CopyParked, mate.CopyGone, mate.CopyUnknown:
		return ""
	default:
		return ""
	}
}

// ReportMateCopy records one supervisor snapshot. Its start time prevents an
// older read from replacing a later event; missing capability means link lost.
func ReportMateCopy(ctx context.Context, d LeaseDeps, statusPath string, target MateCopyTarget, ds *DevServers) error {
	at := d.now().UTC()
	event := mate.CopyEvent{Kind: mate.CopyLinkLost}
	var readErr error
	if ds != nil && ds.Supervisor.Supports(devsup.CapabilityCopyState) {
		var status devsup.Status
		status, readErr = copyServerState(ctx, d.SSH, target.Hostname, "")
		if readErr == nil {
			event = mate.CopyEvent{Kind: mate.CopyReport, Report: string(status)}
		}
	}
	if err := StoreMateCopyEvent(d.StateDir, statusPath, target, event, at, ""); err != nil {
		return fmt.Errorf("publish copy report: %w", err)
	}
	if readErr != nil {
		return fmt.Errorf("read copy report: %w", readErr)
	}
	return nil
}

// AwaitMateServer holds one wait for a copy and writes its answer only over
// the state on which it was armed. A lost connection writes unknown once.
func AwaitMateServer(ctx context.Context, d LeaseDeps, statusPath, root string, target MateCopyTarget) error {
	if target.Pool != "" {
		lease, ok, err := LeaseTargetChecked(ctx, d, target.Hostname, "observed")
		if err != nil {
			return recordLostCopy(ctx, d, statusPath, target, err)
		}
		if !ok || lease.AcquiredAt != target.AcquiredAt || lease.Branch != target.Branch {
			return nil
		}
		if lease.Parked {
			if err := StoreMateCopyEvent(d.StateDir, statusPath, target, mate.CopyEvent{Kind: mate.CopyPark}, d.now().UTC(), target.StateAt); err != nil {
				return fmt.Errorf("store parked copy: %w", err)
			}
			return nil
		}
	}
	ds, err := readDevServers(ctx, d.SSH, target.Hostname, root, d.now)
	if err != nil {
		return recordLostCopy(ctx, d, statusPath, target, err)
	}
	if !ds.Supervisor.Supports(devsup.CapabilityCopyState) {
		return recordLostCopy(ctx, d, statusPath, target, fmt.Errorf("supervisor lacks copy-state capability"))
	}
	after := copyWaitAfter(target.State)
	if after == "" {
		return nil
	}
	status, err := copyServerState(ctx, d.SSH, target.Hostname, after)
	if err != nil {
		return recordLostCopy(ctx, d, statusPath, target, err)
	}
	if err := StoreMateCopyEvent(d.StateDir, statusPath, target, mate.CopyEvent{Kind: mate.CopyReport, Report: string(status)}, d.now().UTC(), target.StateAt); err != nil {
		return fmt.Errorf("store waited copy: %w", err)
	}
	return nil
}

func recordLostCopy(ctx context.Context, d LeaseDeps, statusPath string, target MateCopyTarget, cause error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("copy wait stopped: %w", err)
	}
	if err := StoreMateCopyEvent(d.StateDir, statusPath, target, mate.CopyEvent{Kind: mate.CopyLinkLost}, d.now().UTC(), target.StateAt); err != nil {
		return fmt.Errorf("store lost copy link: %w", err)
	}
	return fmt.Errorf("copy link: %w", cause)
}
