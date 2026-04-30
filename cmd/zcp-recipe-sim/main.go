// zcp-recipe-sim runs zcprecipator3 simulations against a frozen run.
//
// Subcommands:
//
//	emit      stage a frozen scaffold output + emit dispatch prompts
//	stitch    assemble fragments into the simulated recipe shape
//	validate  run slot-shape refusals over fragments
//
// End-to-end loop:
//
//	zcp-recipe-sim emit -run docs/zcprecipator3/runs/18 \
//	    -out docs/zcprecipator3/simulations/19
//	# user dispatches 4 Agent calls (api/app/worker codebase-content + env-content)
//	# each agent reads sim/<host>dev/zerops.yaml + sim/environments/{plan.json,facts.jsonl}
//	# writes fragments under sim/fragments-new/<host>/
//	zcp-recipe-sim stitch   -dir docs/zcprecipator3/simulations/19
//	zcp-recipe-sim validate -dir docs/zcprecipator3/simulations/19
//
// The emit step is byte-identical to the production engine's
// `zerops_recipe action=build-subagent-prompt` output (plus a 20-line
// replay adapter that redirects record-fragment to file-write). Brief
// or atom edits land identically in simulation and production —
// divergence lives only in the leading adapter.
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	sub := os.Args[1]
	args := os.Args[2:]
	var err error
	switch sub {
	case "emit":
		err = runEmit(args)
	case "stitch":
		err = runStitch(args)
	case "validate":
		err = runValidate(args)
	case "help", "-h", "--help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n\n", sub)
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `zcp-recipe-sim — zcprecipator3 simulation tool

Subcommands:
  emit      Stage frozen scaffold output + emit dispatch prompts.
            Reads:  <run>/environments/{plan.json,facts.jsonl}
                    <run>/<host>dev/zerops.yaml (comments stripped on copy)
            Writes: <out>/environments/{plan.json,facts.jsonl}
                    <out>/<host>dev/zerops.yaml (bare)
                    <out>/briefs/{<host>,env}-prompt.md
                    <out>/fragments-new/{<host>,env}/  (empty)
            Flags:  -run, -out, -mount-root

  stitch    Assemble simulated recipe from authored fragments.
            Reads:  <dir>/environments/plan.json
                    <dir>/<host>dev/zerops.yaml (bare)
                    <dir>/fragments-new/{<host>,env}/*.md
            Writes: <dir>/README.md  (root)
                    <dir>/environments/<N>/{import.yaml,README.md}
                    <dir>/<host>dev/{README.md,CLAUDE.md,zerops.yaml}
            Flags:  -dir

  validate  Run slot-shape refusals over fragments.
            Reads:  <dir>/environments/plan.json
                    <dir>/fragments-new/<host>/*.md
            Flags:  -dir

`)
}
