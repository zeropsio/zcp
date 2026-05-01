package recipe

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// gate_worker_dev_server.go — Run-20 C4 closure.
//
// Run-19 scaffold-worker invoked the MCP `zerops_dev_server` tool zero
// times (api+app: 1 each). The worker's NestJS standalone-context
// process never ran on workerdev; behavior was attested only on
// workerstage's compiled entry. The dev-loop principle's
// `port`+`healthPath` args read as HTTP-only, so the agent skipped
// the tool entirely for the no-HTTP worker.
//
// `principles/dev-loop.md` now teaches the `port=0 healthPath=""`
// carve-out + a mandatory `worker_dev_server_started` fact. This gate
// enforces the attestation: any dev codebase with
// `start: zsc noop --silent` and no recorded
// `worker_dev_server_started` (or bypass `worker_no_dev_server`) fact
// fails scaffold complete-phase.
//
// Gate-context-only: reads Plan, Plan.Codebases[i].SourceRoot's
// zerops.yaml on disk, and FactsLog. No tool history access (gates
// can't see sub-agent transcripts) — the fact is the contract.

// gateWorkerDevServerStarted refuses scaffold complete-phase when a
// dev codebase ran without the agent owning its long-running process
// via `zerops_dev_server`. The attestation contract is described in
// principles/dev-loop.md; the gate enforces it.
//
// Detection: a codebase is dev-server-required when its dev setup's
// `start:` is the canonical `zsc noop --silent` idle command. That
// shape is the marker that "the agent owns the long-running process,
// not the platform."
//
// Bypass: a recorded fact with topic `worker_no_dev_server` (any
// kind) on the codebase scope suppresses the requirement, intended
// for one-shot batch codebases that don't need a watcher loop.
func gateWorkerDevServerStarted(ctx GateContext) []Violation {
	if ctx.Plan == nil {
		return nil
	}
	var out []Violation
	for _, cb := range ctx.Plan.Codebases {
		if cb.Hostname == "" || cb.SourceRoot == "" {
			continue
		}
		if !codebaseHasNoopDevStart(cb.SourceRoot) {
			continue
		}
		if hasFactTopic(ctx.FactsLog, cb.Hostname, "worker_no_dev_server") {
			// Explicit opt-out — one-shot batch codebases.
			continue
		}
		if !hasFactTopic(ctx.FactsLog, cb.Hostname, "worker_dev_server_started") {
			out = append(out, Violation{
				Code:     "worker-dev-server-not-started",
				Path:     cb.Hostname,
				Severity: SeverityBlocking,
				Message: fmt.Sprintf(
					"codebase/%s has `start: zsc noop --silent` but no `worker_dev_server_started` fact recorded — agent must own the long-running process via `zerops_dev_server` (see principles/dev-loop.md no-HTTP worker carve-out). Bypass with a `worker_no_dev_server` fact + reason for one-shot batch codebases.",
					cb.Hostname,
				),
			})
		}
	}
	return out
}

// codebaseHasNoopDevStart returns true iff the dev setup of the
// codebase's on-disk zerops.yaml uses `start: zsc noop --silent` as
// the run command. Any other start (compiled entry, framework
// process) means the platform manages the process and no dev-server
// attestation is required.
//
// The check is text-shape rather than yaml-AST because the start
// directive is a single line; matching it as text avoids pulling
// gopkg.in/yaml.v3 into a hot gate path. Other gates that need full
// directive-tree parsing already pay the parse cost; this one
// shouldn't.
func codebaseHasNoopDevStart(sourceRoot string) bool {
	yamlPath := filepath.Join(sourceRoot, "zerops.yaml")
	raw, err := os.ReadFile(yamlPath)
	if err != nil {
		// Pre-scaffold or unreadable — gate doesn't fire. The
		// existing facts-recorded gate already notices missing
		// scaffold artifacts.
		return false
	}
	for line := range strings.SplitSeq(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "start:") {
			continue
		}
		// Match the canonical idle shape; tolerate quoting variants.
		val := strings.TrimSpace(strings.TrimPrefix(trimmed, "start:"))
		val = strings.Trim(val, "\"'")
		if val == "zsc noop --silent" {
			return true
		}
	}
	return false
}

// hasFactTopic returns true when FactsLog contains at least one
// record with the given topic on the named codebase scope.
//
// Scope matching: the fact's Scope field is host-prefixed
// (`<host>/...`) per the run-17 fact attribution convention.
// Bare-host equality also counts.
func hasFactTopic(log *FactsLog, host, topic string) bool {
	if log == nil || host == "" || topic == "" {
		return false
	}
	records, err := log.Read()
	if err != nil {
		return false
	}
	for _, f := range records {
		if f.Topic != topic {
			continue
		}
		factHost := f.Scope
		if i := strings.IndexByte(factHost, '/'); i > 0 {
			factHost = factHost[:i]
		}
		if factHost == host {
			return true
		}
	}
	return false
}
