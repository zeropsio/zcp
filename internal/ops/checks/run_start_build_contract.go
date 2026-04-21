package checks

import (
	"context"
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/workflow"
)

// buildCommandPrefixes enumerates leading tokens that identify a build
// command masquerading as a run.start value. The list is narrow on
// purpose: every entry is a package manager or compiler driver whose
// output lands in deployFiles, not a process that the runtime should
// launch. Shell wrappers (bash -c, sh -c) and language launchers (node,
// python, ruby, php-fpm) are correctly absent — they may invoke a build
// sub-step (e.g. `bash -c "npm run build && node dist/main.js"`) but the
// top-level start is still a launcher, which is legitimate.
var buildCommandPrefixes = []string{
	"npm install",
	"pip install",
	"go build",
	"cargo build",
	"mvn package",
	"gradle build",
}

// CheckRunStartBuildContract flags `run.start` values that start with a
// build-command prefix — the v8.81 §4.5 dev-start vs buildCommands
// contract. A build command at run.start means the container re-runs
// package installation on every boot, which both breaks rolling deploys
// (install fails because the package registry is unreachable mid-deploy)
// and masks missing buildCommands.
//
// Returns nil on pass (caller accumulates checks and only fails surface)
// and a single-element slice on fail. Behavior preserved exactly from
// the pre-C-7a inline form so check counts across the suite stay stable.
// Returns nil when entry or entry.Run.Start is empty — other checks
// (`_run_start`) already report the missing-start case so this predicate
// stays focused on the "populated but wrong shape" class.
func CheckRunStartBuildContract(_ context.Context, hostname string, entry *ops.ZeropsYmlEntry) []workflow.StepCheck {
	if entry == nil || entry.Run.Start == "" {
		return nil
	}
	startLower := strings.ToLower(entry.Run.Start)
	for _, prefix := range buildCommandPrefixes {
		if strings.HasPrefix(startLower, prefix) {
			return []workflow.StepCheck{{
				Name:   hostname + "_run_start_build_cmd",
				Status: StatusFail,
				Detail: fmt.Sprintf(
					"run.start %q looks like a build command — move it to build.buildCommands",
					entry.Run.Start,
				),
			}}
		}
	}
	return nil
}
