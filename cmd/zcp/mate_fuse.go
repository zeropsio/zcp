package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
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
//  1. The server is not there: the release list is read fresh and the unit is
//     restarted; its start job starts the version to run.
//  2. The version to run (mate.VersionToRun) differs from the running one: it is
//     downloaded, and the server is asked whether it may restart now. Yes: the
//     unit restarts. An --attended run reads the release list fresh for this;
//     an automatic one uses the copy zcp holds for the cache time.
//  3. A newer zcp is out: the same question. Yes: the container restarts, once
//     per zcp release.
//
// --automatic asks with the organization's policy, unless the Mate has had no
// HQ link for an hour; --attended (a person's or an operator's update) asks
// without it.

// mateFuseCutoff is how long the Mate goes without an HQ link before the keep
// job asks it without the organization's policy.
const mateFuseCutoff = time.Hour

// mateFuseRestartGrace is how long the keep job waits, after the platform
// accepted the container restart, for the restart to end this process. It
// equals the Mate server's own grace for the same restart (MATE_RESTART_GRACE,
// 5 minutes); the unit's TimeoutStartSec must exceed it plus the drain's 10
// minutes.
var mateFuseRestartGrace = 5 * time.Minute

// mateUnit is the Mate's systemd unit.
const mateUnit = "zerops@" + mate.UnitName + ".service"

// mateFuseRecord is the keep job's own memory: when it last saw the link up, and
// the zcp release it already restarted the container for (one attempt per
// release).
type mateFuseRecord struct {
	LinkSeenAt      time.Time `json:"linkSeenAt,omitzero"`
	ZCPRestartedFor string    `json:"zcpRestartedFor,omitempty"`
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
	automatic := !flags["--attended"] && !cutOff(&rec, rt.ProjectID)

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

	// An attended run decides on the release list as it is now; an automatic
	// one on the copy zcp holds for the cache time.
	attended := flags["--attended"]
	release, err := mate.HeldRelease(context.Background(), attended)
	switch {
	case err != nil:
		fmt.Fprintf(os.Stderr, "mate keep: release list: %v\n", err)
	case attended:
		fmt.Fprintf(os.Stderr, "mate keep: release list read fresh: Mate %s\n", release.Version)
	}
	if run := mate.VersionToRun(mate.ReadVersions(), release.Version); run != "" && run != running {
		return restartForMate(unlock, running, run, release, automatic)
	}

	ctx, cancel := context.WithTimeout(context.Background(), statusManifestTimeout)
	defer cancel()
	zcp := startZCPReleaseLookup(ctx, server.Version, update.LatestOptions{})()
	if zcp.Behind && rec.ZCPRestartedFor != zcp.Latest {
		return restartForZCP(unlock, rec, zcp, running, automatic, lookup)
	}
	fmt.Fprintf(os.Stderr, "mate keep: Mate %s runs the version to run; zcp %s (latest %q)\n", running, zcp.Installed, zcp.Latest)
	return 0
}

// cutOff notes the HQ link when it is up and says whether the Mate has gone an
// hour without it. The hour counts from the later of the link last seen up and
// the activation of current: a Mate that never linked counts from its start.
func cutOff(rec *mateFuseRecord, projectID string) bool {
	now := time.Now().UTC()
	doc, err := hq.NewFileMateStateSource(hq.MateStatePath(), projectID).Read()
	if err == nil && doc.Link == hq.MateLinkLinked {
		rec.LinkSeenAt = now
		_ = writeMateFuseRecord(*rec)
		return false
	}
	since := rec.LinkSeenAt
	if info, err := os.Lstat(mate.CurrentLink()); err == nil && info.ModTime().After(since) {
		since = info.ModTime().UTC()
	}
	if since.IsZero() || now.Sub(since) < mateFuseCutoff {
		return false
	}
	fmt.Fprintf(os.Stderr, "mate keep: no HQ link since %s; asking without the organization's policy\n", since.Format(time.RFC3339))
	return true
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

// restartAbsentMate restarts a unit whose server is not there, after reading
// the release list fresh, so its start job starts the newest version that
// works. A server that is not there holds no work to ask about.
func restartAbsentMate(unlock func(), why string) int {
	fmt.Fprintf(os.Stderr, "mate keep: the Mate is not there (%s); restarting it\n", why)
	if _, err := mate.HeldRelease(context.Background(), true); err != nil {
		fmt.Fprintf(os.Stderr, "mate keep: release list: %v\n", err)
	}
	unlock()
	if err := restartMateUnit(); err != nil {
		fmt.Fprintf(os.Stderr, "mate keep: %v\n", err)
		return 1
	}
	return 0
}

// restartForMate downloads run and restarts the unit once the running server
// agrees; its start job then starts run.
func restartForMate(unlock func(), running, run string, release mate.Manifest, automatic bool) int {
	if run != release.Version {
		release = mate.Manifest{Version: run}
	}
	stageCtx, cancel := context.WithTimeout(context.Background(), mate.InstallTimeout)
	defer cancel()
	if err := mate.StageRelease(stageCtx, release, mate.ReplaceFailing); err != nil {
		fmt.Fprintf(os.Stderr, "mate keep: stage Mate %s: %v\n", run, err)
		return 1
	}
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
