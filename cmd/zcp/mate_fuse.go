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
//  3. A newer zcp is out, read from its source on every run (for a dev zcp:
//     no active dev pin and a release newer than its base): the same
//     question. Yes: the container restarts, once per installed zcp and
//     release.
//
// --automatic asks with the organization's policy, whether or not the Mate has
// an HQ link; --attended (a person's or an operator's update) asks without it;
// --forced (an operator's forced update, `zcp mate update --force`) asks as
// --attended does, but restarts past blockers that are no live work
// (mate.KeepQuestion.Force).

// keepWay is how a keep run asks: the unit instance its systemd template names.
type keepWay string

const (
	wayAutomatic keepWay = "automatic"
	wayAttended  keepWay = "attended"
	wayForced    keepWay = "forced"
)

// mateFuseRestartGrace is how long the keep job waits, after the platform
// accepted the container restart, for the restart to end this process. It
// equals the Mate server's own grace for the same restart (MATE_RESTART_GRACE,
// 5 minutes); the unit's TimeoutStartSec must exceed it plus the drain's 10
// minutes.
var mateFuseRestartGrace = 5 * time.Minute

// mateUnit is the Mate's systemd unit.
const mateUnit = "zerops@" + mate.UnitName + ".service"

// restartedFrom says the record's zcp restart attempt restarted from the zcp
// build installed now, which zcp names; built is this binary's build time. A
// run that finds another build installed forgets the attempt, so a dev zcp
// placed after an attempt, even one pushed again under the same version, is
// still replaced once its pin ends.
func restartedFrom(r mate.KeepRecord, zcp zcpReleaseStatus, built string) bool {
	return r.ZCPRestartedFrom == zcp.Installed && r.ZCPRestartedFromBuilt == built
}

// restartTried says the keep job already restarted the container for zcp's
// latest release from the zcp build installed now.
func restartTried(r mate.KeepRecord, zcp zcpReleaseStatus, built string) bool {
	return r.ZCPRestartedFor == zcp.Latest && restartedFrom(r, zcp, built)
}

// keepQuestion wires the Mate's loopback updater and rec, written to its
// file, into the keep job's question.
func keepQuestion(rec *mate.KeepRecord) mate.KeepQuestion {
	return mate.KeepQuestion{
		Drain:     mate.DrainUpdate,
		Readiness: readMateReadiness,
		Close:     mate.CloseForUpdate,
		Save: func(d *mate.Deferral) error {
			rec.Deferred = d
			return writeMateFuseRecord(*rec)
		},
		Cancel: func() { _ = mate.PostUpdate("cancel") },
		Now:    time.Now,
	}
}

// readMateReadiness is one loopback readiness read, bounded to 5 s.
func readMateReadiness() (mate.UpdateReadiness, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var readiness mate.UpdateReadiness
	err := mate.UpdateRequest(ctx, "readiness", nil, &readiness)
	return readiness, err
}

func mateFuseRecordPath() string { return filepath.Join(mate.Prefix(), "fuse.json") }

func runMateFuse(args []string) int {
	flags, code, done := commandFlags("usage: zcp mate fuse [--automatic|--attended|--forced]", args, "--automatic", "--attended", "--forced")
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
	way := wayAutomatic
	switch {
	case flags["--forced"]:
		way = wayForced
	case flags["--attended"]:
		way = wayAttended
	}

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
	releaseRead := err == nil
	if err != nil {
		fmt.Fprintf(os.Stderr, "mate keep: release list not read: %v\n", err)
	}
	// A Mate release that cannot be staged still lets the zcp step run: a newer
	// zcp is what repairs a staging defect, and where nobody can reach the
	// container this run is the only way it arrives. The run still fails.
	unstaged := ""
	versions := mate.ReadVersions()
	if run := mate.VersionToRun(versions, release.Version); run != "" && run != running {
		if err := stageMate(run, release); err != nil {
			fmt.Fprintf(os.Stderr, "mate keep: stage Mate %s: %v; checking zcp all the same\n", run, err)
			unstaged = run
		} else {
			return restartForMate(unlock, &rec, running, run, way)
		}
	}

	// The same for zcp's own release, bounded as a status read.
	ctx, cancel := context.WithTimeout(context.Background(), statusManifestTimeout)
	defer cancel()
	zcp := startZCPReleaseLookup(ctx, server.Version, false, versions.DevPin != nil)()
	// Another build than the one the attempt restarted from: the attempt took
	// effect, and a build pushed again later, even under the same version, is
	// a new placement.
	if rec.Attempted() && !restartedFrom(rec, zcp, server.Built) {
		rec = rec.WithoutAttempt()
		_ = writeMateFuseRecord(rec)
	}
	if zcp.Behind && !restartTried(rec, zcp, server.Built) {
		if code := restartForZCP(unlock, rec, zcp, running, way, lookup); unstaged == "" {
			return code
		}
		return 1
	}
	// Nothing asked: a deferral this run knows is over is cleared. A record
	// that cannot be written is said and never fails the run.
	_ = keepQuestion(&rec).Settle(rec.Deferred, mate.KeepRun{Running: running, ReleaseRead: releaseRead, Unstaged: unstaged, ZCPLatest: zcp.Latest})
	dev := devBuildsLine(versions, release.Version, unstaged, zcp)
	if unstaged != "" {
		fmt.Fprintf(os.Stderr, "mate keep: Mate %s keeps running, %s is not staged; zcp %s (latest %q)%s\n", running, unstaged, zcp.Installed, zcp.Latest, dev)
		return 1
	}
	fmt.Fprintf(os.Stderr, "mate keep: Mate %s runs the version to run; zcp %s (latest %q)%s\n", running, zcp.Installed, zcp.Latest, dev)
	return 0
}

// devBuildsLine ends a run's last line with what keeps the installed dev
// builds (docs/spec-mate.md §2.1a): the active pin, or for each unpinned one
// the reason this run left it in place. latest is the Mate release list's
// version ("" unread), unstaged the Mate version that could not be staged.
// "" with no dev build installed.
func devBuildsLine(versions mate.Versions, latest, unstaged string, zcp zcpReleaseStatus) string {
	devZCP, devMate := !update.IsRelease(zcp.Installed), mate.IsDevVersion(versions.Current)
	if !devZCP && !devMate {
		return ""
	}
	if versions.DevPin != nil {
		return fmt.Sprintf("; dev builds pinned until %s: %s", versions.DevPin.Until.Format(time.RFC3339), versions.DevPin.Reason)
	}
	var why []string
	if devZCP {
		switch {
		case zcp.Latest == "":
			why = append(why, "dev zcp stays: its latest release is unknown")
		case zcp.Behind:
			why = append(why, fmt.Sprintf("dev zcp stays: the container restart for zcp %s was already asked from this build", zcp.Latest))
		default:
			why = append(why, "dev zcp stays: no release is newer than its base")
		}
	}
	if devMate {
		switch {
		case unstaged != "":
			why = append(why, fmt.Sprintf("dev Mate stays: Mate %s is not staged", unstaged))
		case latest == "":
			why = append(why, "dev Mate stays: the release list was not read")
		default:
			why = append(why, "dev Mate stays: no release is newer than its base")
		}
	}
	return "; " + strings.Join(why, "; ")
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
func restartForMate(unlock func(), rec *mate.KeepRecord, running, run string, way keepWay) int {
	yes, ended, code := askRestart(rec, run, running, run, way)
	if !yes {
		return code
	}
	fmt.Fprintf(os.Stderr, "mate keep: restarting the Mate for %s (runs %s)\n", run, running)
	unlock()
	if err := restartMateUnit(); err != nil {
		_ = mate.PostUpdate("cancel")
		fmt.Fprintf(os.Stderr, "mate keep: %v\n", err)
		return 1
	}
	if ended != nil {
		// A forced yes holds intake closed by a drain request still open, and
		// the Mate reopens intake when that request is interrupted: the run
		// ends only once the stopping Mate drops it, within the request's own
		// bound.
		<-ended
	}
	return 0
}

// askRestart asks the running server "may I restart you now?" through its
// drain route (mate.KeepQuestion.Ask), or forced (mate.KeepQuestion.Force).
// Not now prints why and is 1 only when the deferral could not be recorded; a
// yes goes on whatever the record. ended, non-nil only after a forced yes that
// closed intake itself, is closed when that drain request ends.
func askRestart(rec *mate.KeepRecord, version, running, asked string, way keepWay) (yes bool, ended <-chan struct{}, code int) {
	q := keepQuestion(rec)
	var notNow, saveErr error
	if way == wayForced {
		f := q.Force(rec.Deferred, version, running, asked)
		yes, ended, notNow, saveErr = f.Yes, f.Ended, f.NotNow, f.SaveErr
		if yes && len(f.Overridden) > 0 {
			fmt.Fprintf(os.Stderr, "mate keep: forced past blockers that are no live work: %s\n", strings.Join(f.Overridden, "; "))
		}
	} else {
		yes, notNow, saveErr = q.Ask(rec.Deferred, version, running, asked, way == wayAutomatic)
	}
	if yes {
		return true, ended, 0
	}
	fmt.Fprintf(os.Stderr, "mate keep: not now: %v\n", notNow)
	if saveErr != nil {
		return false, nil, 1
	}
	return false, nil, 0
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
func restartForZCP(unlock func(), rec mate.KeepRecord, zcp zcpReleaseStatus, running string, way keepWay, lookup func(string) string) int {
	installed := zcp.Installed
	if !update.IsRelease(installed) {
		installed += ", a dev build with no pin"
	}
	fmt.Fprintf(os.Stderr, "mate keep: zcp %s is out (installed %s); asking to restart the container\n", zcp.Latest, installed)
	// A forced yes's drain request stays open with this process through the
	// grace below, holding intake closed for the container restart.
	if yes, _, code := askRestart(&rec, running, running, mate.ZCPAsked(zcp.Latest), way); !yes {
		return code
	}
	unlock()
	before := rec
	rec.ZCPRestartedFor, rec.ZCPRestartedFrom, rec.ZCPRestartedFromBuilt = zcp.Latest, zcp.Installed, server.Built
	if writeMateFuseRecord(rec) != nil {
		_ = mate.PostUpdate("cancel")
		return 1
	}
	answer, serviceID := selfRestart(lookup, newSelfRestartClient)
	printSelfRestart(os.Stderr, answer, serviceID, false)
	if !keepsAttempt(answer) {
		_ = writeMateFuseRecord(before)
	}
	code := 1
	if answer.Error == "" {
		code = 0
		// An accepted request is not a restart that happened: the container's
		// restart kills this process within the grace; still alive after it, the
		// restart never came and the Mate admits work again. The record keeps
		// the attempt: one per installed zcp and release.
		time.Sleep(mateFuseRestartGrace)
		fmt.Fprintf(os.Stderr, "mate keep: the container did not restart within %s\n", mateFuseRestartGrace)
	}
	_ = mate.PostUpdate("cancel")
	return code
}

// keepsAttempt reports whether the keep job keeps the record of a zcp
// restart it asked for: an accepted restart, or a refusal for a FAILED
// service, which only a stop and start from outside the container ends, so
// asking again would only drain the Mate for the same release. Any other
// refusal or an unconfirmed answer forgets it, and the next run asks again.
func keepsAttempt(answer selfRestartAnswer) bool {
	return answer.Error == "" || answer.ServiceFailed
}

func readMateFuseRecord() mate.KeepRecord {
	var rec mate.KeepRecord
	if raw, err := os.ReadFile(mateFuseRecordPath()); err == nil {
		_ = json.Unmarshal(raw, &rec)
	}
	return rec
}

// writeMateFuseRecord writes rec through a temp-file rename and says a
// failure on stderr.
func writeMateFuseRecord(rec mate.KeepRecord) error {
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
		return fmt.Errorf("write keep record: %w", err)
	}
	return nil
}
