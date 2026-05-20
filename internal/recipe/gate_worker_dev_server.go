package recipe

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zeropsio/zcp/internal/topology"
)

// gate_worker_dev_server.go — Run-20 C4 closure, updated for the
// omit-`run.start` dev convention (run-49 issue 3).
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
// enforces the attestation: any dev codebase whose dev setup is a
// dynamic runtime (the kind that needs `zerops_dev_server` to own
// the long-running process) and lacks the recorded
// `worker_dev_server_started` (or bypass `worker_no_dev_server`) fact
// fails scaffold complete-phase.
//
// Marker change (run-49 issue 3): the previous marker was the literal
// `start: zsc noop --silent` line on the dev setup. Under the new
// convention dev setups omit `run.start` entirely, so the marker is
// now "dev setup with a dynamic-runtime base" — classified via
// topology.RuntimeClassFor. Implicit-webserver (php-nginx, php-apache,
// nginx) and static runtimes auto-serve and never need
// `zerops_dev_server`, so they remain exempt.
//
// Gate-context-only: reads Plan, Plan.Codebases[i].SourceRoot's
// zerops.yaml on disk, and FactsLog. No tool history access (gates
// can't see sub-agent transcripts) — the fact is the contract.

// gateWorkerDevServerStarted refuses scaffold complete-phase when a
// dev codebase ran without the agent owning its long-running process
// via `zerops_dev_server`. The attestation contract is described in
// principles/dev-loop.md; the gate enforces it.
//
// Detection: a codebase is dev-server-required when its dev setup
// (`setup: dev`) uses a dynamic-runtime base (nodejs, go, python,
// bun, rust, deno, …). Implicit-webserver and static bases auto-serve
// on container boot and need no dev-server attestation.
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
		if !codebaseDevSetupIsDynamic(cb.SourceRoot) {
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
					"codebase/%s has a dynamic-runtime `setup: dev` block but no `worker_dev_server_started` fact recorded — agent must own the long-running process via `zerops_dev_server` (see principles/dev-loop.md no-HTTP worker carve-out). Bypass with a `worker_no_dev_server` fact + reason for one-shot batch codebases.",
					cb.Hostname,
				),
			})
		}
	}
	return out
}

// codebaseDevSetupIsDynamic returns true iff the codebase's on-disk
// zerops.yaml has a `setup: dev` block whose `run.base` is a dynamic
// runtime (nodejs, go, python, bun, rust, deno, etc.). Implicit-
// webserver (php-nginx, php-apache, nginx) and static bases return
// false — those classes auto-serve on boot and need no dev-server
// attestation.
//
// Text-shape scan (no yaml parser) — looks for the `setup: dev` block
// header, then the next `base:` directive at greater indentation that
// is inside a `run:` mapping. This stays in the hot gate path without
// pulling gopkg.in/yaml.v3 in. Other gates that need full directive-
// tree parsing already pay the parse cost; this one shouldn't.
func codebaseDevSetupIsDynamic(sourceRoot string) bool {
	yamlPath := filepath.Join(sourceRoot, "zerops.yaml")
	raw, err := os.ReadFile(yamlPath)
	if err != nil {
		// Pre-scaffold or unreadable — gate doesn't fire. The
		// existing facts-recorded gate already notices missing
		// scaffold artifacts.
		return false
	}
	base := extractDevRunBase(string(raw))
	if base == "" {
		return false
	}
	return topology.RuntimeClassFor(base) == topology.RuntimeDynamic
}

// extractDevRunBase walks a zerops.yaml body and returns the `run.base`
// value of the `setup: dev` block (empty string if no dev block, no run
// section, or no base directive). Tolerates the legacy `setup: appdev`
// / `setup: dev` shapes and quoting variants on the base value.
//
// Algorithm: find a `- setup: dev` (or `- setup: <name>dev`) line; from
// there, scan forward for a `run:` mapping at greater indentation; from
// there, scan forward for a `base:` directive at greater indentation
// still. A new `- setup:` at any indentation level terminates the
// scan (we left the dev block).
func extractDevRunBase(yaml string) string {
	// devSetupName is the canonical dev-setup identifier. Distinct from
	// devEngineVersion (which is the binary-build placeholder); the two
	// share the literal "dev" by coincidence.
	const devSetupName = "dev"
	lines := strings.Split(yaml, "\n")
	inDev := false
	inDevRun := false
	devSetupIndent := -1
	runIndent := -1
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		// A new setup entry at the setup level resets state. The
		// `- setup:` shape can be `- setup: dev` or `- setup: prod`,
		// always with a `- ` prefix when at the entry-list level.
		if rest, ok := strings.CutPrefix(trimmed, "- setup:"); ok {
			val := strings.TrimSpace(rest)
			val = strings.Trim(val, "\"'")
			// Match canonical "dev" and legacy "<host>dev" shapes used
			// by older recipes. "prod" / "stage" / "appstage" don't
			// match. Be permissive on the suffix to catch transitional
			// recipes mid-rename.
			inDev = val == devSetupName || strings.HasSuffix(val, devSetupName)
			inDevRun = false
			devSetupIndent = indent
			runIndent = -1
			continue
		}
		if !inDev {
			continue
		}
		// We left the dev block if a non-setup directive at or below
		// the setup-list indent appears.
		if indent <= devSetupIndent && !strings.HasPrefix(trimmed, "- ") {
			inDev = false
			continue
		}
		if !inDevRun {
			if strings.HasPrefix(trimmed, "run:") {
				inDevRun = true
				runIndent = indent
			}
			continue
		}
		// Inside dev.run — look for `base:` at greater indent than the
		// `run:` directive itself.
		if indent <= runIndent {
			// Back out to setup level (or beyond) — stop scanning run.
			inDevRun = false
			continue
		}
		if rest, ok := strings.CutPrefix(trimmed, "base:"); ok {
			val := strings.TrimSpace(rest)
			val = strings.Trim(val, "\"'")
			return val
		}
	}
	return ""
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
