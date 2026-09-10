package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"time"

	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/runtime"
)

// restartTimeout bounds the unit restart. `systemctl restart` blocks until
// the unit reports started, and mate's own startup is a node process — generous
// enough not to fail a healthy start, short enough that a wedged unit does
// not hang the command indefinitely.
const restartTimeout = 60 * time.Second

// statusManifestTimeout bounds `zcp mate status`'s manifest fetch — a small
// JSON file, same budget as the manifest resolver's own internal timeout.
const statusManifestTimeout = 5 * time.Second

// mateUnitFilePath is where `zsc unit create` lands the mate unit — same check
// internal/init uses to tell "already registered" from "first boot".
// Package-level so a test can point it at a temp path instead of /usr/lib.
var mateUnitFilePath = mate.UnitFilePath

// mateRestartUnit restarts the supervised mate unit so a freshly activated
// version is actually picked up without a container restart. Package-level
// so tests can stub the shell-out.
var mateRestartUnit = defaultMateRestartUnit

func defaultMateRestartUnit(unit string) error {
	ctx, cancel := context.WithTimeout(context.Background(), restartTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sudo", "systemctl", "restart", unit)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// runMateCmd implements `zcp mate status [--json] [--refresh]` and
// `zcp mate update [--force] [--json]`. args is everything after "mate".
func runMateCmd(args []string) int {
	if len(args) == 0 {
		log.Print("usage: zcp mate <status|update> [--json] [--force] [--refresh]")
		return 1
	}
	switch args[0] {
	case mateVerbStatus:
		return runMateStatus(args[1:])
	case "update":
		return runMateUpdate(args[1:])
	default:
		log.Print("usage: zcp mate <status|update> [--json] [--force] [--refresh]")
		return 1
	}
}

// mateVerbStatus names the `zcp mate status` subcommand as a constant rather
// than a repeated literal: cmd/zcp/capture.go and cmd/zcp/telemetry_cmd.go
// each already switch on their own unrelated "status" verb in this same
// package (goconst dedups per package, not per file).
const mateVerbStatus = "status"

// mateStatusResult is `zcp mate status --json`'s answer (spec-mate.md
// §2.1a, MD-17): what zcp knows about the installed and latest mate release,
// from the same resolver DesiredRelease() and the manifest cache — never a
// fresh install.
type mateStatusResult struct {
	Installed       string `json:"installed,omitempty"`
	Latest          string `json:"latest,omitempty"`
	Contract        int    `json:"contract"`
	UpdateAvailable bool   `json:"updateAvailable"`
	CheckedAt       string `json:"checkedAt"`
	Error           string `json:"error,omitempty"`
}

// runMateStatus answers what zcp knows about the installed and latest mate
// release. It never installs anything, and it always exits 0 — even when
// the manifest is unreachable, in which case Error names why and
// UpdateAvailable is false.
func runMateStatus(args []string) int {
	asJSON := slices.Contains(args, "--json")
	refresh := slices.Contains(args, "--refresh")

	result := mateStatusResult{
		Contract:  mate.SupportedContract,
		CheckedAt: time.Now().UTC().Format(time.RFC3339),
	}

	if installed, err := mate.InstalledVersion(); err == nil {
		result.Installed = installed
	}

	ctx, cancel := context.WithTimeout(context.Background(), statusManifestTimeout)
	defer cancel()
	desired, err := mate.DesiredRelease(ctx, http.DefaultClient, mate.ManifestOptions{Refresh: refresh})
	if err != nil {
		result.Error = err.Error()
	} else {
		result.Latest = desired.Version
		if result.Installed != "" {
			result.UpdateAvailable = mate.VersionOlder(result.Installed, desired.Version)
		}
	}

	printMateStatus(result, asJSON)
	return 0
}

func printMateStatus(result mateStatusResult, asJSON bool) {
	if asJSON {
		data, err := json.Marshal(result)
		if err != nil {
			log.Printf("mate status: marshal result: %v", err)
			return
		}
		fmt.Fprintln(os.Stdout, string(data))
		return
	}

	installed := result.Installed
	if installed == "" {
		installed = "not installed"
	}
	switch {
	case result.Error != "":
		fmt.Fprintf(os.Stdout, "mate: installed %s, latest unknown: %s\n", installed, result.Error)
	case result.UpdateAvailable:
		fmt.Fprintf(os.Stdout, "mate: installed %s, %s available\n", installed, result.Latest)
	default:
		fmt.Fprintf(os.Stdout, "mate: installed %s, latest %s\n", installed, result.Latest)
	}
}

// mateUpdateResult is `zcp mate update --json`'s answer (MD-17).
type mateUpdateResult struct {
	Action    string `json:"action"`
	From      string `json:"from,omitempty"`
	To        string `json:"to,omitempty"`
	Restarted bool   `json:"restarted"`
	Error     string `json:"error,omitempty"`
}

// runMateUpdate refreshes the release manifest, runs the same EnsureInstalled
// pass the init step runs, and — only when the pass actually activated a
// different bundle AND the supervised unit is registered on this container —
// restarts it so the update takes effect without waiting for the next
// container restart. Container-only: it refuses on a local machine and when
// ZCP_MATE_ENABLED is off, since there is nothing to update either way.
func runMateUpdate(args []string) int {
	force := slices.Contains(args, "--force")
	asJSON := slices.Contains(args, "--json")

	rt := runtime.Detect()
	if !rt.InContainer {
		return failMateUpdate(asJSON, "zcp mate update only runs inside a Zerops container")
	}
	if !rt.MateEnabled {
		return failMateUpdate(asJSON, "ZCP_MATE_ENABLED is off — mate is not managed on this container")
	}

	result, err := mate.EnsureInstalled(mate.EnsureOptions{Force: force, Refresh: true})
	if err != nil {
		return failMateUpdate(asJSON, err.Error())
	}

	out := mateUpdateResult{Action: string(result.Action), From: result.From, To: result.To, Error: result.Warning}
	exitCode := 0

	if unit := "zerops@" + mate.UnitName + ".service"; bundleActivated(result.Action) {
		if _, statErr := os.Stat(mateUnitFilePath); statErr == nil {
			// A unit is registered on this container — restart it so the
			// activation just staged takes effect without waiting for the
			// next container restart. No unit registered means nothing to
			// restart: a following `zcp init` (or the next boot) creates
			// one against whatever EnsureInstalled just activated.
			if !asJSON {
				fmt.Fprintf(os.Stderr, "restarting %s...\n", unit)
			}
			if err := mateRestartUnit(unit); err != nil {
				out.Error = fmt.Sprintf("systemctl restart %s: %v", unit, err)
				exitCode = 1
			} else {
				out.Restarted = true
			}
		}
	}

	printMateUpdateResult(out, asJSON)
	return exitCode
}

// bundleActivated reports whether EnsureInstalled actually replaced the
// bytes the server runs — the one case `zcp mate update` restarts the unit
// for.
func bundleActivated(action mate.Action) bool {
	return action == mate.ActionInstalled || action == mate.ActionUpdated
}

func failMateUpdate(asJSON bool, reason string) int {
	printMateUpdateResult(mateUpdateResult{Action: string(mate.ActionNone), Error: reason}, asJSON)
	return 1
}

// printMateUpdateResult prints the final result exactly once, in either
// form: --json emits the mateUpdateResult as one JSON line to stdout,
// otherwise a human line naming what EnsureInstalled did (or the error) goes
// to stderr, mirroring the line the init step logs
// (internal/init/init_mate.go's logMateEnsureResult) — the two are the same
// seam, run from two different callers.
func printMateUpdateResult(result mateUpdateResult, asJSON bool) {
	if asJSON {
		data, err := json.Marshal(result)
		if err != nil {
			log.Printf("mate update: marshal result: %v", err)
			return
		}
		fmt.Fprintln(os.Stdout, string(data))
		return
	}
	if result.Error != "" {
		log.Printf("mate update: %s", result.Error)
		return
	}
	switch mate.Action(result.Action) {
	case mate.ActionNone:
		fmt.Fprintf(os.Stderr, "mate: already at %s, nothing to do\n", result.To)
	case mate.ActionInstalled:
		fmt.Fprintf(os.Stderr, "mate: installed %s\n", result.To)
	case mate.ActionUpdated:
		fmt.Fprintf(os.Stderr, "mate: updated %s -> %s\n", result.From, result.To)
	}
}
