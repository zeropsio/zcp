package recipe

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zeropsio/zcp/internal/topology"
)

// gate_dev_runtime_no_run_start.go — Run-52 Fix 4 (Issue 3 structural
// catch).
//
// The dynamic dev convention (run-49 issue 3) is: SSHFS-mounted source +
// omitted `run.start` + the long-running process owned out-of-band via
// `zerops_dev_server`. A stale `start:` directive on a dynamic-class dev
// runtime silently breaks that contract — the container runs the start
// command (or the placeholder `zsc noop --silent` that run-51 shipped)
// instead of staying alive for the watcher loop. Nothing caught it at
// authoring time: the live schema accepts `run.start` as a valid field,
// and gate_worker_dev_server.go was already refactored off the literal
// `zsc noop` marker (it now keys on dynamic-runtime class + the
// worker_dev_server_started fact), so this gate doesn't conflict with it.
//
// Scope: dynamic-class dev blocks only. Implicit-webserver (php-nginx,
// php-apache), static, and managed runtimes auto-serve and legitimately
// run a start command (or none); they are out of scope. Prod blocks
// legitimately carry the real app start and are never inspected —
// extractDevRunStart terminates at the next `- setup:` exactly like
// extractDevRunBase.

// gateDevRuntimeNoRunStart refuses scaffold complete-phase (and, running
// transitively at feature + finalize, those too) when a dev codebase
// whose dev setup is a dynamic runtime carries a non-empty `run.start`.
// The dynamic dev runtime must omit run.start entirely.
func gateDevRuntimeNoRunStart(ctx GateContext) []Violation {
	if ctx.Plan == nil {
		return nil
	}
	var out []Violation
	for _, cb := range ctx.Plan.Codebases {
		if cb.Hostname == "" || cb.SourceRoot == "" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(cb.SourceRoot, "zerops.yaml"))
		if err != nil {
			// Pre-scaffold or unreadable — the facts-recorded gate already
			// notices missing scaffold artifacts.
			continue
		}
		body := string(raw)
		base := extractDevRunBase(body)
		if base == "" || topology.RuntimeClassFor(base) != topology.RuntimeDynamic {
			// No dev block, or non-dynamic class (implicit-webserver /
			// static / managed) — out of scope.
			continue
		}
		if extractDevRunStart(body) == "" {
			continue
		}
		out = append(out, Violation{
			Code:     "dev-runtime-run-start-present",
			Path:     cb.Hostname,
			Severity: SeverityBlocking,
			Message: fmt.Sprintf(
				"codebase/%s has a dynamic-runtime `setup: dev` block that declares `run.start` — the dynamic dev convention omits run.start entirely (SSHFS-mounted source + the long-running process owned via `zerops_dev_server`). Remove the `start:` directive from the dev block; a stale start (e.g. `zsc noop --silent`) runs the placeholder instead of staying alive for the watcher loop.",
				cb.Hostname,
			),
		})
	}
	return out
}

// extractDevRunStart walks a zerops.yaml body and returns the `run.start`
// value of the `setup: dev` block (empty string if no dev block, no run
// section, or no start directive). Modeled line-for-line on
// extractDevRunBase: find a `- setup: dev` (or `- setup: <name>dev`)
// line; from there, scan forward for a `run:` mapping at greater
// indentation; from there, scan forward for a `start:` directive at
// greater indentation still. A new `- setup:` at any indentation level
// terminates the scan (so prod blocks are never inspected).
func extractDevRunStart(yaml string) string {
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
		if rest, ok := strings.CutPrefix(trimmed, "- setup:"); ok {
			val := strings.TrimSpace(rest)
			val = strings.Trim(val, "\"'")
			inDev = val == devSetupName || strings.HasSuffix(val, devSetupName)
			inDevRun = false
			devSetupIndent = indent
			runIndent = -1
			continue
		}
		if !inDev {
			continue
		}
		// We left the dev block if a non-setup directive at or below the
		// setup-list indent appears.
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
		// Inside dev.run — look for `start:` at greater indent than the
		// `run:` directive itself.
		if indent <= runIndent {
			// Back out to setup level (or beyond) — stop scanning run.
			inDevRun = false
			continue
		}
		if rest, ok := strings.CutPrefix(trimmed, "start:"); ok {
			val := strings.TrimSpace(rest)
			val = strings.Trim(val, "\"'")
			return val
		}
	}
	return ""
}
