// Tests for: internal/tools/workflow_checks_worker_correctness.go —
// production-correctness rules for worker codebases.
package tools

import (
	"context"
	"fmt"
	"strings"

	opschecks "github.com/zeropsio/zcp/internal/ops/checks"
	"github.com/zeropsio/zcp/internal/workflow"
)

// checkWorkerProductionCorrectness composes the two queue-group and
// shutdown predicates that C-7c moved into internal/ops/checks. The
// composition behavior is preserved exactly from the pre-C-7c inline
// form: when BOTH sub-checks pass, a single aggregate
// `{hostname}_worker_production_correctness` pass row is emitted (no
// individual pass rows). When either fails, only the fail rows are
// emitted (no aggregate pass).
//
// Non-worker targets and shared-codebase workers return nil — the
// ops/checks predicates already short-circuit both of those.
func checkWorkerProductionCorrectness(ctx context.Context, hostname string, readmeContent string, target workflow.RecipeTarget) []workflow.StepCheck {
	qg := opschecks.CheckWorkerQueueGroupGotcha(ctx, hostname, readmeContent, target)
	sh := opschecks.CheckWorkerShutdownGotcha(ctx, hostname, readmeContent, target)
	if len(qg) == 0 && len(sh) == 0 {
		return nil // non-worker or shared-codebase worker: upstream skips
	}

	qgPass := len(qg) == 1 && qg[0].Status == statusPass
	shPass := len(sh) == 1 && sh[0].Status == statusPass
	if qgPass && shPass {
		return []workflow.StepCheck{{
			Name:   hostname + "_worker_production_correctness",
			Status: statusPass,
		}}
	}

	var out []workflow.StepCheck
	for _, c := range qg {
		if c.Status == statusFail {
			out = append(out, c)
		}
	}
	for _, c := range sh {
		if c.Status == statusFail {
			out = append(out, c)
		}
	}
	return out
}

// checkWorkerDrainCodeBlock enforces that a separate-codebase worker
// README carries a fenced code block showing the SIGTERM → drain →
// exit call sequence. v7's worker README had this as IG #3 with a full
// typescript diff; v18's worker README shipped the drain topic only as
// prose inside a gotcha — correct content but no copy-paste reference.
//
// The existing `worker_shutdown_gotcha` check verifies the topic is
// mentioned anywhere in the knowledge-base fragment. This complementary
// check verifies the topic has concrete reference code: a fenced block
// somewhere in the worker README (either the integration-guide or the
// knowledge-base fragment) must contain a drain call AND a process/app
// exit call, proving the reader has a copy-pasteable implementation.
//
// "Keep" disposition per check-rewrite.md §17 — the predicate reads the
// whole README (not a structured manifest field), and the tools layer
// already has the mount-walking context it needs; no shim benefit.
func checkWorkerDrainCodeBlock(hostname string, readmeContent string, target workflow.RecipeTarget) []workflow.StepCheck {
	if !target.IsWorker {
		return nil
	}
	if target.SharesCodebaseWith != "" {
		return nil
	}
	blocks := extractFencedBlockBodies(readmeContent)
	for _, b := range blocks {
		lower := strings.ToLower(b)
		if !containsDrainCall(lower) {
			continue
		}
		if !containsExitCall(lower) {
			continue
		}
		return []workflow.StepCheck{{
			Name:   hostname + "_drain_code_block",
			Status: statusPass,
		}}
	}
	return []workflow.StepCheck{{
		Name:   hostname + "_drain_code_block",
		Status: statusFail,
		Detail: fmt.Sprintf(
			"worker %q README has a drain-topic gotcha but no fenced code block showing the actual SIGTERM → drain → exit call sequence. Add an integration-guide step (e.g. \"### N. Drain on SIGTERM\") with a copy-pasteable code block: process.on('SIGTERM', stop); stop() { await nc.drain(); await dataSource.destroy(); process.exit(0); } — the exact call sequence the reader needs. The existing shutdown gotcha tells them to drain; this check ensures they have reference code to lift.",
			hostname,
		),
	}}
}

// drainCallTokens identify a drain or close call inside a fenced
// block — "drain(" covers nc.drain()/q.drain()/consumer.drain(),
// "app.close" covers NestJS microservice shutdown, "graceful" catches
// Go-style graceful shutdown helpers.
var drainCallTokens = []string{
	"drain(",
	".drain ",
	"app.close(",
	"server.close(",
	"graceful shutdown",
	"gracefulshutdown",
}

// exitCallTokens identify a process/app exit call inside the same
// fenced block — proves the block shows the call sequence, not just
// the drain.
var exitCallTokens = []string{
	"process.exit",
	"os.exit",
	"exit(0)",
	"exit(1)",
	"return nil", // go-style: `if err := drain(); err != nil { ... } return nil`
}

func containsDrainCall(lowerBlock string) bool {
	for _, token := range drainCallTokens {
		if strings.Contains(lowerBlock, token) {
			return true
		}
	}
	return false
}

func containsExitCall(lowerBlock string) bool {
	for _, token := range exitCallTokens {
		if strings.Contains(lowerBlock, token) {
			return true
		}
	}
	return false
}

// extractFencedBlockBodies returns the body text of every fenced code
// block in the content. Used by checkWorkerDrainCodeBlock to walk every
// block looking for a call sequence.
func extractFencedBlockBodies(content string) []string {
	var out []string
	rest := content
	for {
		start := strings.Index(rest, "```")
		if start < 0 {
			return out
		}
		// Skip past the opening fence line (lang tag included).
		lineEnd := strings.Index(rest[start:], "\n")
		if lineEnd < 0 {
			return out
		}
		bodyStart := start + lineEnd + 1
		end := strings.Index(rest[bodyStart:], "```")
		if end < 0 {
			return out
		}
		out = append(out, rest[bodyStart:bodyStart+end])
		rest = rest[bodyStart+end+3:]
	}
}
