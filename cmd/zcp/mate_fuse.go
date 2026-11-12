package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/server"
	"github.com/zeropsio/zcp/internal/update"
)

// `zcp mate fuse` is the keep job (docs/spec-mate.md §2.1d): it decides when the
// Mate restarts, nothing else. Its systemd timer runs it every few minutes
// outside the Mate server, and `zcp mate update` runs it at once. Each run takes
// the install lock without waiting, so it does nothing while a start job
// installs or judges a boot, or while another run asks. Then, in order:
//
//  1. The server is not there: the unit is restarted; its start job reads the
//     release list and starts the version to run.
//  2. The version to run (mate.VersionToRun), from the release list read from
//     its source on every run, differs from the running one: it is
//     downloaded, and the server is asked whether it may restart now. Yes: the
//     unit restarts. A version that cannot be staged fails the run, after
//     step 3.
//  3. A newer zcp is out: the same question. Yes: the container restarts, once
//     per zcp release. An --attended run reads zcp's latest release fresh too.
//
// --automatic asks with the organization's policy, whether or not the Mate has
// an HQ link; --attended (a person's or an operator's update) asks without it.

// mateFuseRestartGrace is how long the keep job waits, after the platform
// accepted the container restart, for the restart to end this process. It
// equals the Mate server's own grace for the same restart (MATE_RESTART_GRACE,
// 5 minutes); the unit's TimeoutStartSec must exceed it plus the drain's 10
// minutes.
var mateFuseRestartGrace = 5 * time.Minute

// mateUnit is the Mate's systemd unit.
const mateUnit = "zerops@" + mate.UnitName + ".service"

// mateFuseRecord is the keep job's own memory: the zcp release it already
// restarted the container for (one attempt per release).
type mateFuseRecord struct {
	ZCPRestartedFor string `json:"zcpRestartedFor,omitempty"`
}

func mateFuseRecordPath() string { return filepath.Join(mate.Prefix(), "fuse.json") }

func runMateFuse(args []string) int {
	flags, code, done := commandFlags("usage: zcp mate fuse [--automatic|--attended]", args, "--automatic", "--attended")
	if done {
		return code
	}
	lookup := mate.LiveLookup(mate.LiveEnvStorePath)
	rt := runtime.DetectFrom(lookup)
	if !rt.InContainer || !rt.MateEnabled {
		return 0
	}
	unlock, err := mate.LockInstall(0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mate keep: %v; a start or another keep run is at work, nothing now\n", err)
		return 0
	}
	defer unlock()

	rec := readMateFuseRecord()
	automatic := !flags["--attended"]

	if !mateUnitRunning() {
		return restartAbsentMate(unlock, "the unit is not running")
	}
	running, there := mate.ServerThere(mate.BootWindow)
	if !there {
		return restartAbsentMate(unlock, fmt.Sprintf("no answer and no boot progress for %s", mate.BootWindow))
	}
	if running == "" {
		fmt.Fprintln(os.Stderr, "mate keep: the Mate is booting; nothing now")
		return 0
	}

	release, err := mate.DesiredRelease(context.Background(), http.DefaultClient)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mate keep: release list not read: %v\n", err)
	}
	// A Mate release that cannot be staged still lets the zcp step run: a newer
	// zcp is what repairs a staging defect, and where nobody can reach the
	// container this run is the only way it arrives. The run still fails.
	unstaged := ""
	if run := mate.VersionToRun(mate.ReadVersions(), release.Version); run != "" && run != running {
		if err := stageMate(run, release); err != nil {
			fmt.Fprintf(os.Stderr, "mate keep: stage Mate %s: %v; checking zcp all the same\n", run, err)
			unstaged = run
		} else {
			return restartForMate(unlock, running, run, automatic)
		}
	}

	// The same for zcp's own release, bounded as a status read.
	ctx, cancel := context.WithTimeout(context.Background(), statusManifestTimeout)
	defer cancel()
	zcp := startZCPReleaseLookup(ctx, server.Version, update.LatestOptions{Refresh: flags["--attended"]})()
	if zcp.Behind && rec.ZCPRestartedFor != zcp.Latest {
		if code := restartForZCP(unlock, rec, zcp, running, automatic, lookup); unstaged == "" {
			return code
		}
		return 1
	}
	if unstaged != "" {
		fmt.Fprintf(os.Stderr, "mate keep: Mate %s keeps running, %s is not staged; zcp %s (latest %q)\n", running, unstaged, zcp.Installed, zcp.Latest)
		return 1
	}
	fmt.Fprintf(os.Stderr, "mate keep: Mate %s runs the version to run; zcp %s (latest %q)\n", running, zcp.Installed, zcp.Latest)
	return 0
}

// mateUnitRunning says systemd runs the Mate's unit: active, starting,
// reloading or stopping. An answer it cannot read counts as running, so only
// systemd's own word restarts it.
func mateUnitRunning() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, _ := exec.CommandContext(ctx, "systemctl", "is-active", mateUnit).Output()
	const inactive, failed = "inactive", "failed"
	state := strings.TrimSpace(string(out))
	return state != inactive && state != failed
}

// restartAbsentMate restarts a unit whose server is not there; its start job
// reads the release list and starts the newest version that works. A server
// that is not there holds no work to ask about.
func restartAbsentMate(unlock func(), why string) int {
	fmt.Fprintf(os.Stderr, "mate keep: the Mate is not there (%s); restarting it\n", why)
	unlock()
	if err := restartMateUnit(); err != nil {
		fmt.Fprintf(os.Stderr, "mate keep: %v\n", err)
		return 1
	}
	return 0
}

// stageMate downloads run into its version directory, within InstallTimeout.
func stageMate(run string, release mate.Manifest) error {
	if run != release.Version {
		release = mate.Manifest{Version: run}
	}
	ctx, cancel := context.WithTimeout(context.Background(), mate.InstallTimeout)
	defer cancel()
	return mate.StageRelease(ctx, release, mate.ReplaceFailing)
}

// restartForMate restarts the unit for the staged run once the running server
// agrees; its start job then starts run.
func restartForMate(unlock func(), running, run string, automatic bool) int {
	if !askRestart(run, automatic) {
		return 0
	}
	fmt.Fprintf(os.Stderr, "mate keep: restarting the Mate for %s (runs %s)\n", run, running)
	unlock()
	if err := restartMateUnit(); err != nil {
		_ = mate.PostUpdate("cancel")
		fmt.Fprintf(os.Stderr, "mate keep: %v\n", err)
		return 1
	}
	return 0
}

// askRestart asks the running server "may I restart you now?" through its
// drain route. Not now (a refusal, the deadline, no answer) reopens what the
// question closed.
func askRestart(version string, automatic bool) bool {
	if _, err := mate.DrainUpdate(version, automatic); err != nil {
		_ = mate.PostUpdate("cancel")
		fmt.Fprintf(os.Stderr, "mate keep: not now: %v\n", err)
		return false
	}
	return true
}

func restartMateUnit() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sudo", "systemctl", "restart", "--no-block", mateUnit)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("restart %s: %w", mateUnit, err)
	}
	return nil
}

// restartForZCP brings a newer zcp in the way a container boot does: the
// restart's initCommands install it, then the Mate's start job starts the
// newest Mate. The running Mate first agrees, as for its own restart.
func restartForZCP(unlock func(), rec mateFuseRecord, zcp zcpReleaseStatus, running string, automatic bool, lookup func(string) string) int {
	fmt.Fprintf(os.Stderr, "mate keep: zcp %s is out (installed %s); asking to restart the container\n", zcp.Latest, zcp.Installed)
	if !askRestart(running, automatic) {
		return 0
	}
	unlock()
	tried := rec.ZCPRestartedFor
	rec.ZCPRestartedFor = zcp.Latest
	code := writeMateFuseRecord(rec)
	if code == 0 {
		code = runSelfCmd([]string{"restart"}, os.Stderr, lookup, newSelfRestartClient)
		if code != 0 {
			rec.ZCPRestartedFor = tried
			_ = writeMateFuseRecord(rec)
		}
	}
	if code == 0 {
		// An accepted request is not a restart that happened: the container's
		// restart kills this process within the grace; still alive after it, the
		// restart never came and the Mate admits work again. The record keeps
		// the release: one attempt per release.
		time.Sleep(mateFuseRestartGrace)
		fmt.Fprintf(os.Stderr, "mate keep: the container did not restart within %s\n", mateFuseRestartGrace)
	}
	_ = mate.PostUpdate("cancel")
	return code
}

func readMateFuseRecord() mateFuseRecord {
	var rec mateFuseRecord
	if raw, err := os.ReadFile(mateFuseRecordPath()); err == nil {
		_ = json.Unmarshal(raw, &rec)
	}
	return rec
}

func writeMateFuseRecord(rec mateFuseRecord) int {
	body, err := json.Marshal(rec)
	if err == nil {
		err = os.MkdirAll(mate.Prefix(), 0o755)
	}
	if err == nil {
		tmp := mateFuseRecordPath() + ".next"
		if err = os.WriteFile(tmp, body, 0o600); err == nil {
			err = os.Rename(tmp, mateFuseRecordPath())
		}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "mate keep: write record: %v\n", err)
		return 1
	}
	return 0
}
