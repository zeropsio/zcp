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
	"github.com/zeropsio/zcp/internal/server"
	"github.com/zeropsio/zcp/internal/update"
)

// statusManifestTimeout bounds `zcp mate status`'s manifest fetch — a small
// JSON file, same budget as the manifest resolver's own internal timeout.
const statusManifestTimeout = 5 * time.Second

// runMateCmd implements `zcp mate status [--json] [--local]`,
// `zcp mate update [--json] [--automatic|--force]`, `zcp mate pin`, the dev pin's one
// writer, and `zcp mate fuse`, the keep job its systemd units run
// (mate_fuse.go). args is everything after "mate".
func runMateCmd(args []string) int {
	const usage = "usage: zcp mate <status|update> [--json] [--local] [--automatic|--force] | zcp mate pin [--for <duration> --reason <text> | --end]"
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
	case "pin":
		return runMatePin(args[1:])
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
	Installed       string              `json:"installed"`
	Minimum         string              `json:"minimum"`
	BelowFloor      bool                `json:"belowFloor"`
	Latest          string              `json:"latest"`
	Contract        int                 `json:"contract"`
	UpdateAvailable bool                `json:"updateAvailable"`
	CheckedAt       string              `json:"checkedAt"`
	Error           string              `json:"error,omitempty"`
	Updater         *mate.UpdaterStatus `json:"updater,omitempty"`
	ZCP             zcpReleaseStatus    `json:"zcp"`
	RestartNeeded   bool                `json:"restartNeeded"`
	Why             string              `json:"why,omitempty"`
	DevPin          *mate.DevPin        `json:"devPin,omitempty"`
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
	result.DevPin = versions.DevPin
	if result.Installed != "" {
		result.BelowFloor = mate.BelowFloor(result.Installed)
	}
	result.Updater = mate.UpdaterStatusOf(versions, readMateFuseRecord().Deferred)

	ctx, cancel := context.WithTimeout(context.Background(), statusManifestTimeout)
	defer cancel()
	zcpRelease := startZCPReleaseLookup(ctx, zcpVersion, local, versions.DevPin != nil)
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
	if u := result.Updater; u != nil && u.Phase == mate.PhasePostponed {
		why := ""
		if u.PostponedWhy != "" {
			why = ": " + u.PostponedWhy
		}
		fmt.Fprintf(os.Stdout, "mate: update to %s postponed since %s%s\n", u.PostponedFor, u.PostponedSince, why)
	}
	devZCP := !update.IsRelease(result.ZCP.Installed)
	switch {
	case result.ZCP.Behind:
		fmt.Fprintf(os.Stdout, "zcp: installed %s, %s available; a restart installs it\n", result.ZCP.Installed, result.ZCP.Latest)
	case devZCP:
		latest := result.ZCP.Latest
		if latest == "" {
			latest = "unknown"
		}
		fmt.Fprintf(os.Stdout, "zcp: installed %s (dev build), latest %s\n", result.ZCP.Installed, latest)
	}
	if result.RestartNeeded && !result.ZCP.Behind {
		fmt.Fprintf(os.Stdout, "restart needed: %s\n", result.Why)
	}
	if devZCP || mate.IsDevVersion(result.Installed) {
		fmt.Fprintln(os.Stdout, devPinLine(result.DevPin))
	}
}

// devPinLine says what keeps the installed dev builds (docs/spec-mate.md
// §2.1a): the active pin, or the rule that replaces them without one.
func devPinLine(pin *mate.DevPin) string {
	if pin == nil {
		return "no dev pin: a release newer than a dev build's base replaces it"
	}
	return fmt.Sprintf("dev builds pinned until %s (since %s): %s", pin.Until.Format(time.RFC3339), pin.Since.Format(time.RFC3339), pin.Reason)
}

// runMatePin implements `zcp mate pin`, the dev pin's one writer
// (docs/spec-mate.md §2.1a): with --reason (and --for, DevPinDefault without
// it) it places the pin in place of any earlier one, with --end it removes it,
// and with neither it shows the active one. A refused pin exits 1 and writes
// nothing. Container-only, with ZCP_MATE_ENABLED on.
func runMatePin(args []string) int {
	const usage = "usage: zcp mate pin [--for <duration> --reason <text> | --end]"
	if slices.ContainsFunc(args, isHelpArg) {
		fmt.Fprintln(os.Stdout, usage)
		return 0
	}
	values := map[string]string{}
	end := false
	for rest := args; len(rest) > 0; rest = rest[1:] {
		switch arg := rest[0]; arg {
		case "--end":
			end = true
		case "--for", "--reason":
			if len(rest) == 1 {
				fmt.Fprintf(os.Stderr, "%s needs a value\n%s\n", arg, usage)
				return 2
			}
			rest = rest[1:]
			values[arg] = rest[0]
		default:
			fmt.Fprintf(os.Stderr, "unknown argument %q\n%s\n", arg, usage)
			return 2
		}
	}
	if end && len(values) > 0 {
		fmt.Fprintf(os.Stderr, "--end takes no --for or --reason\n%s\n", usage)
		return 2
	}

	rt := runtime.Detect()
	if !rt.InContainer {
		fmt.Fprintln(os.Stderr, "zcp mate pin only runs inside a Zerops container")
		return 1
	}
	if !rt.MateEnabled {
		fmt.Fprintln(os.Stderr, "ZCP_MATE_ENABLED is off — mate is not managed on this container")
		return 1
	}

	switch {
	case end:
		if err := mate.RemoveDevPin(); err != nil {
			fmt.Fprintf(os.Stderr, "mate pin: %v\n", err)
			return 1
		}
		fmt.Fprintln(os.Stdout, devPinLine(nil))
		return 0
	case len(values) == 0:
		if pin, ok := mate.ActiveDevPin(time.Now()); ok {
			fmt.Fprintln(os.Stdout, devPinLine(&pin))
		} else {
			fmt.Fprintln(os.Stdout, "no dev pin")
		}
		return 0
	}
	d := mate.DevPinDefault
	if raw, ok := values["--for"]; ok {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			fmt.Fprintf(os.Stderr, "mate pin: --for %q is not a duration (e.g. 24h, 90m)\n", raw)
			return 1
		}
		d = parsed
	}
	pin, err := mate.NewDevPin(time.Now(), d, values["--reason"])
	if err == nil {
		err = mate.WriteDevPin(pin)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "mate pin: %v\n", err)
		return 1
	}
	fmt.Fprintln(os.Stdout, devPinLine(&pin))
	return 0
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
// person's or an operator's update); with it, with the policy. `zcp mate
// update --force` is an operator's forced update: it runs `zcp mate fuse
// --forced`, which restarts past blockers that are no live work
// (docs/spec-mate.md §2.1d); with --automatic it is a usage error. A start or
// another keep run holding the install lock is reported, not waited for.
// Container-only, with ZCP_MATE_ENABLED on.
func runMateUpdate(args []string) int {
	const usage = "usage: zcp mate update [--json] [--automatic|--force]"
	flags, code, done := commandFlags(usage, args, "--json", "--automatic", "--force")
	if done {
		return code
	}
	if flags["--force"] && flags["--automatic"] {
		fmt.Fprintf(os.Stderr, "--force takes no --automatic: a forced update is an operator's, never the policy's\n%s\n", usage)
		return 1
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

	way := mate.WayAttended
	switch {
	case flags["--automatic"]:
		way = mate.WayAutomatic
	case flags["--force"]:
		way = mate.WayForced
	}
	unit := mate.KeepUnitName + "@" + string(way) + ".service"
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
