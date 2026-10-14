package tools

import (
	"context"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// putChangeReceipt writes the change's receipt after a push of its branch
// (spec-mate §2.8a.6), only for a pair of more than one stage. Best-effort,
// like a retitle: the change is there either way, and the next push writes
// it again; an HQ that does not know the key refuses it and loses nothing.
func putChangeReceipt(ctx context.Context, hqc hq.Client, stateDir string, m *workflow.ServiceMeta, number int) {
	if len(m.Stages) < 2 {
		return
	}
	ws, _ := workflow.CurrentWorkSession(stateDir)
	receipt := changeReceipt(m.Stages, ws, time.Now())
	callCtx, cancel := context.WithTimeout(ctx, hqCallTimeout)
	defer cancel()
	_, _ = hqc.EditChange(callCtx, m.HQ.Repo, number, hq.ChangeEdit{Receipt: receipt})
}

// changeReceipt is each stage's preview as its last deploy in the work
// session left it; nil for a pair of at most one stage, whose change never
// carries one.
func changeReceipt(stages []topology.StageRef, ws *workflow.WorkSession, at time.Time) *hq.ChangeReceipt {
	if len(stages) < 2 {
		return nil
	}
	receipt := &hq.ChangeReceipt{At: at.UTC().Format(time.RFC3339), Parts: make([]hq.ChangeReceiptPart, 0, len(stages))}
	for _, stage := range stages {
		receipt.Parts = append(receipt.Parts, receiptPart(stage, ws))
	}
	return receipt
}

// receiptPart is one preview: none until a deploy of it is recorded; then
// the last attempt, failed or ok. A commit is named only for an ok deploy
// from a clean workspace; verified only by a verify that passed after it.
func receiptPart(stage topology.StageRef, ws *workflow.WorkSession) hq.ChangeReceiptPart {
	part := hq.ChangeReceiptPart{Setup: stage.SetupName, PreviewHostname: stage.Hostname, Deploy: hq.ReceiptDeployNone}
	if ws == nil || len(ws.Deploys[stage.Hostname]) == 0 {
		return part
	}
	attempts := ws.Deploys[stage.Hostname]
	last := attempts[len(attempts)-1]
	part.WorkspaceClean = !last.Dirty
	if last.SucceededAt == "" {
		part.Deploy, part.DeployedAt = hq.ReceiptDeployFailed, last.AttemptedAt
		return part
	}
	part.Deploy, part.DeployedAt = hq.ReceiptDeployOK, last.SucceededAt
	if part.WorkspaceClean {
		part.DeployedCommit = last.SHA
	}
	for _, verify := range ws.Verifies[stage.Hostname] {
		// Both are RFC 3339 in UTC, which order as text.
		if verify.Passed && verify.AttemptedAt >= last.SucceededAt {
			part.Verified = true
		}
	}
	return part
}
