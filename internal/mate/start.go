package mate

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"
)

// BootWindow is how long a Mate's boot may go without a receipt of progress
// before it counts as not coming up. Every receipt starts the window again, so
// a boot that keeps advancing is never judged by elapsed time.
const BootWindow = 2 * time.Minute

// StartStageTimeout bounds a start's release-list refresh plus download and
// npm install together when current exists: how long the network may delay
// starting the Mate. A download that does not finish in it is left to the
// keep job, which has InstallTimeout, and the start runs current. Measured on
// a Mate container (2 vCPU, 0.16.179, 2026-11-12): tarball 0.2 s, npm install
// 5.9 s with npm's cache and 8.2 s without; a minute is seven times that.
const StartStageTimeout = time.Minute

// Start is one run of the start job, `zcp service start mate` (docs/spec-mate.md
// §2.1a): the version it launches and its hold on the install lock until it
// decided whether that version came up. The start job is the only installer
// and the only writer of current, last-good and the failed version.
type Start struct {
	version  string
	lastGood string
	unlock   func()
}

// BeginStart readies the start job's launch. Under the install lock, taken
// without waiting, it makes current name the version to run: one bounded
// attempt to refresh the release list and to download that version, and on
// any error it keeps current as it is. A lock held by a keep run means the
// keep job is asking or downloading: current launches as it is, the verdict
// is still this start's. It fails only when nothing is installed or what is
// installed is below MinimumMateVersion.
func BeginStart(logf func(format string, args ...any)) (*Start, error) {
	unlock, err := LockInstall(0)
	if err != nil {
		logf("%v; starting current as it is", err)
		unlock = func() {}
	} else {
		switchToVersionToRun(logf)
	}
	v := ReadVersions()
	if v.Current == "" {
		unlock()
		return nil, errors.New("no Mate is installed, and none could be")
	}
	if BelowFloor(v.Current) {
		unlock()
		return nil, &BelowFloorError{Installed: v.Current, Minimum: MinimumMateVersion}
	}
	// Every boot receipt afterwards is this launch's server's.
	_ = os.Remove(BootFilePath())
	return &Start{version: v.Current, lastGood: v.LastGood, unlock: unlock}, nil
}

// switchToVersionToRun points current at the version to run, downloading it
// when keep has not; a failure leaves current as it was. The release list's
// refresh and the download share one deadline: StartStageTimeout when current
// exists, so the network delays a start only that long and a download that
// does not finish is left to the keep job; InstallTimeout for a first install.
func switchToVersionToRun(logf func(format string, args ...any)) {
	bound := StartStageTimeout
	if ReadVersions().Current == "" {
		bound = InstallTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), bound)
	defer cancel()
	release, err := HeldRelease(ctx, false)
	if err != nil {
		logf("release list: %v", err)
	}
	v := ReadVersions()
	run := VersionToRun(v, release.Version)
	if run == "" || run == v.Current {
		return
	}
	if run != release.Version {
		release = Manifest{Version: run}
	}
	// With nothing installed there is no current to start instead, so a broken
	// download left by an interrupted first install is replaced, not kept.
	replacing := KeepFailing
	if v.Current == "" {
		replacing = ReplaceFailing
	}
	if err := StageRelease(ctx, release, replacing); err != nil {
		logf("install %s: %v; starting current %q", run, err, v.Current)
		return
	}
	if err := activate(VersionDir(run)); err != nil {
		logf("switch to %s: %v; starting current %q", run, err, v.Current)
		return
	}
	logf("switched current %q -> %s", v.Current, run)
}

// Version is the version this start launches.
func (s *Start) Version() string { return s.version }

// Judge follows the launched server, child, until it is up or its boot ends,
// then records the verdict and lets go of the install lock. Up: last-good
// names the version. Not up: the server is ended; a version newer than
// last-good is remembered as failed and current returns to last-good, unless
// last-good is below SwitchBackMinimum; last-good itself, a dev build, or a
// first install writes and switches nothing. The returned error says it did
// not come up, and the caller's process ends with it, so systemd starts the
// unit again. A stop the launcher was asked for is no verdict. exited
// delivers, once the child exited, whether a stop was asked.
func (s *Start) Judge(child *os.Process, exited <-chan bool) error {
	defer s.unlock()
	var ended, stopped bool
	observe := func(block bool) {
		if ended {
			return
		}
		if block {
			relayed, ok := <-exited
			ended, stopped = true, ok && relayed
			return
		}
		select {
		case relayed, ok := <-exited:
			ended, stopped = true, ok && relayed
		default:
		}
	}
	err := waitUp(s.version, child.Pid, BootWindow, func() bool {
		observe(false)
		return ended
	})
	if err == nil {
		return s.up()
	}
	if !ended {
		_ = child.Kill()
		observe(true)
	}
	if stopped {
		return nil
	}
	return s.down(err)
}

func (s *Start) up() error {
	if !canBeLastGood(s.version) {
		return nil
	}
	if s.version != s.lastGood {
		if err := replaceLink(LastGoodLink(), VersionDir(s.version)); err != nil {
			return fmt.Errorf("%s is up, last-good unchanged: %w", s.version, err)
		}
	}
	if err := forgetFailedVersion(s.version); err != nil {
		return err
	}
	pruneOldVersions(s.version)
	return nil
}

func (s *Start) down(cause error) error {
	if !s.switchesBack() {
		return fmt.Errorf("%s did not come up: %w; nothing switched", s.version, cause)
	}
	// The switch back does not wait on the record: a failed version that
	// could not be written still returns current to last-good, so the next
	// start runs last-good rather than the version that did not come up.
	recordErr := writeFailedVersion(s.version)
	if err := activate(VersionDir(s.lastGood)); err != nil {
		return errors.Join(fmt.Errorf("%s did not come up: %w", s.version, cause), recordErr, err)
	}
	if recordErr != nil {
		return errors.Join(fmt.Errorf("%s did not come up: %w; %s starts next", s.version, cause, s.lastGood), recordErr)
	}
	return fmt.Errorf("%s did not come up: %w; %s starts next", s.version, cause, s.lastGood)
}

// switchesBack says a version that did not come up is remembered as failed and
// left for last-good: a release newer than a last-good that refuses state a
// newer version wrote.
func (s *Start) switchesBack() bool {
	return s.lastGood != "" && !IsDevVersion(s.version) && VersionOlder(s.lastGood, s.version) &&
		!VersionOlder(s.lastGood, SwitchBackMinimum)
}

// waitUp waits until the server process server answers on the local port with
// version. It fails once ended reports the process gone, once another version
// answers, once the server's boot file reports a failed boot, or once window
// passes without a receipt of progress (a boot-file progress that grew).
func waitUp(version string, server int, window time.Duration, ended func() bool) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	horizon := time.Now().Add(window)
	var progress int64
	for {
		if ended() {
			return errors.New("the server's process exited")
		}
		probeDeadline := time.Now().Add(time.Second)
		if horizon.Before(probeDeadline) {
			probeDeadline = horizon
		}
		ctx, cancel := context.WithDeadline(context.Background(), probeDeadline)
		var r UpdateReadiness
		answer := UpdateRequest(ctx, "readiness", nil, &r)
		cancel()
		if answer == nil && r.Protocol == 1 {
			if r.Version == version {
				return nil
			}
			return fmt.Errorf("the server on the port runs %q", r.Version)
		}
		if f, ok := readBootFile(); ok && f.PID == server {
			if f.Error != "" {
				return fmt.Errorf("its boot reported: %s", f.Error)
			}
			if f.Progress > progress {
				progress = f.Progress
				horizon = time.Now().Add(window)
			}
		}
		if !time.Now().Before(horizon) {
			if answer != nil {
				return fmt.Errorf("no boot progress for %s: %w", window, answer)
			}
			return fmt.Errorf("no boot progress for %s", window)
		}
		<-ticker.C
	}
}

// ServerThere is the keep job's observation of the running server: there when
// it answers on the local port, with the version it runs, or when its boot
// file advances within window, with version "" (a booting server). Not there
// otherwise.
func ServerThere(window time.Duration) (version string, there bool) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	horizon := time.Now().Add(window)
	first, seen := readBootFile()
	for {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		var r UpdateReadiness
		err := UpdateRequest(ctx, "readiness", nil, &r)
		cancel()
		if err == nil && r.Protocol == 1 {
			return r.Version, true
		}
		if f, ok := readBootFile(); ok && (!seen || f.PID != first.PID || f.Progress > first.Progress) {
			return "", true
		}
		if !time.Now().Before(horizon) {
			return "", false
		}
		<-ticker.C
	}
}

// HeldRelease is the release list zcp holds: refreshed once, bounded by
// manifestFetchTimeout and ctx, when refresh asks for it or the copy is older
// than the cache time. On any error it is the copy zcp holds whatever its age
// (a zero Manifest when there is none), with the error.
func HeldRelease(ctx context.Context, refresh bool) (Manifest, error) {
	release, err := DesiredRelease(ctx, http.DefaultClient, ManifestOptions{Refresh: refresh})
	if err != nil {
		held, _ := ReleaseList()
		return held, err
	}
	return release, nil
}
