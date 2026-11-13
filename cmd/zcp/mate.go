package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/server"
)

// statusManifestTimeout bounds `zcp mate status`'s manifest fetch — a small
// JSON file, same budget as the manifest resolver's own internal timeout.
const statusManifestTimeout = 5 * time.Second

// runMateCmd implements `zcp mate status [--json] [--local]`,
// `zcp mate update [--json] [--automatic]`, and `zcp mate fuse`, the keep job
// its systemd units run (mate_fuse.go). args is everything after "mate".
func runMateCmd(args []string) int {
	const usage = "usage: zcp mate <status|update> [--json] [--local] [--automatic]"
	if len(args) == 0 {
		log.Print(usage)
		return 1
	}
	if isHelpArg(args[0]) {
		fmt.Fprintln(os.Stdout, usage)
		return 0
	}
	switch args[0] {
	case mateVerbStatus:
		return runMateStatus(args[1:])
	case "update":
		return runMateUpdate(args[1:])
	case "fuse":
		return runMateFuse(args[1:])
	default:
		log.Print(usage)
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
// from the same resolver DesiredRelease() (LastReadRelease() under --local) —
// never a fresh install — and about this zcp against its latest release, with
// whether a container restart would change either (§2.9).
type mateStatusResult struct {
	Installed       string             `json:"installed"`
	Minimum         string             `json:"minimum"`
	BelowFloor      bool               `json:"belowFloor"`
	Latest          string             `json:"latest"`
	Contract        int                `json:"contract"`
	UpdateAvailable bool               `json:"updateAvailable"`
	CheckedAt       string             `json:"checkedAt"`
	Error           string             `json:"error,omitempty"`
	Updater         *mateUpdaterStatus `json:"updater,omitempty"`
	ZCP             zcpReleaseStatus   `json:"zcp"`
	RestartNeeded   bool               `json:"restartNeeded"`
	Why             string             `json:"why,omitempty"`
}

// mateUpdaterStatus is the status's "updater" key (§2.1d), in the shape the
// Mate server reads: protocol 1, phase "idle" (zcp keeps no update in
// progress), the version current names, and a failed version newer than it.
type mateUpdaterStatus struct {
	Protocol       int    `json:"protocol"`
	Phase          string `json:"phase"`
	RunningVersion string `json:"runningVersion"`
	FailedVersion  string `json:"failedVersion,omitempty"`
}

// runMateStatus answers what zcp knows about the installed and latest mate
// release. It never installs anything, and it always exits 0 — even when
// the manifest is unreachable, in which case Error names why and
// UpdateAvailable is false.
func runMateStatus(args []string) int { return runMateStatusFor(args, server.Version) }

// runMateStatusFor is runMateStatus for a zcp of version zcpVersion. Every
// status reads both release sources; --local answers from the releases last
// read instead, without a network call, for the Mate server's read at its
// boot.
func runMateStatusFor(args []string, zcpVersion string) int {
	flags, code, done := commandFlags("usage: zcp mate status [--json] [--local]", args, "--json", "--local")
	if done {
		return code
	}
	printMateStatus(readMateStatus(zcpVersion, flags["--local"]), flags["--json"])
	return 0
}

// readMateStatus is the one reader behind `zcp mate status`: the installed
// and latest Mate, the version to run, and this zcp of version zcpVersion
// against its latest release.
func readMateStatus(zcpVersion string, local bool) mateStatusResult {
	result := mateStatusResult{
		Contract:  mate.SupportedContract,
		Minimum:   mate.MinimumMateVersion,
		CheckedAt: time.Now().UTC().Format(time.RFC3339),
	}
	versions := mate.ReadVersions()
	result.Installed = versions.Current
	if result.Installed != "" {
		result.BelowFloor = mate.BelowFloor(result.Installed)
		result.Updater = &mateUpdaterStatus{Protocol: 1, Phase: "idle", RunningVersion: result.Installed}
		if mate.VersionOlder(result.Installed, versions.Failed) {
			result.Updater.FailedVersion = versions.Failed
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), statusManifestTimeout)
	defer cancel()
	zcpRelease := startZCPReleaseLookup(ctx, zcpVersion, local)
	var desired mate.Manifest
	var err error
	if local {
		desired, _ = mate.LastReadRelease()
	} else {
		desired, err = mate.DesiredRelease(ctx, http.DefaultClient)
	}
	if err != nil {
		result.Error = err.Error()
	} else {
		result.Latest = desired.Version
		if result.Installed != "" {
			result.UpdateAvailable = mate.VersionOlder(result.Installed, mate.VersionToRun(versions, desired.Version))
		}
	}

	result.ZCP = zcpRelease()
	result.RestartNeeded, result.Why = restartVerdict(result.ZCP, result.Installed, result.Latest, !local && mateWithoutUpdater(result.UpdateAvailable))
	return result
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
	switch {
	case result.ZCP.Behind:
		fmt.Fprintf(os.Stdout, "zcp: installed %s, %s available; a restart installs it\n", result.ZCP.Installed, result.ZCP.Latest)
	case result.RestartNeeded:
		fmt.Fprintf(os.Stdout, "restart needed: %s\n", result.Why)
	}
}

// mateUpdateResult is `zcp mate update --json`'s answer (MD-17).
type mateUpdateResult struct {
	Action    string `json:"action"`
	From      string `json:"from,omitempty"`
	Restarted bool   `json:"restarted"`
	Error     string `json:"error,omitempty"`
	Started   bool   `json:"started,omitempty"`
}

// runMateUpdate runs the keep job now (mate_fuse.go), outside the Mate server
// that may be calling it: the server's call returns at once while the keep job
// asks it whether it may restart, which can take the drain's ten minutes.
// Without --automatic the keep job asks without the organization's policy (a
// person's or an operator's update); with it, with the policy. A start or
// another keep run holding the install lock is reported, not waited for.
// Container-only, with ZCP_MATE_ENABLED on.
func runMateUpdate(args []string) int {
	flags, code, done := commandFlags("usage: zcp mate update [--json] [--automatic]", args, "--json", "--automatic")
	if done {
		return code
	}
	asJSON := flags["--json"]

	rt := runtime.Detect()
	if !rt.InContainer {
		return failMateUpdate(asJSON, "zcp mate update only runs inside a Zerops container")
	}
	if !rt.MateEnabled {
		return failMateUpdate(asJSON, "ZCP_MATE_ENABLED is off — mate is not managed on this container")
	}
	unlock, err := mate.LockInstall(0)
	if err != nil {
		return failMateUpdate(asJSON, "the Mate is starting or another update is asking it; try again shortly")
	}
	unlock()

	way := "attended"
	if flags["--automatic"] {
		way = "automatic"
	}
	unit := mate.KeepUnitName + "@" + way + ".service"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sudo", "systemctl", "start", "--no-block", unit)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return failMateUpdate(asJSON, fmt.Sprintf("start %s: %v", unit, err))
	}
	installed, _ := mate.InstalledVersion()
	printMateUpdateResult(mateUpdateResult{Action: "none", From: installed, Started: true}, asJSON)
	return 0
}

func failMateUpdate(asJSON bool, reason string) int {
	printMateUpdateResult(mateUpdateResult{Action: "none", Error: reason}, asJSON)
	return 1
}

// printMateUpdateResult prints the result exactly once: --json emits it as one
// JSON line to stdout, otherwise one human line goes to stderr.
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
	fmt.Fprintln(os.Stderr, "mate: keep job started; it restarts the Mate once the Mate agrees (zcp mate status)")
}
