package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/server"
)

// `zcp mate fuse` is the cut-off fuse (docs/spec-mate.md §2.1e). A Mate updates
// on its organization's word, which only its HQ link delivers, so a release that
// breaks the link would also cut the Mate off from its repair. A timer of the
// container's systemd runs this every few minutes, outside the Mate server.
// While the link is up it only notes so. After an hour without it, it installs
// a newer Mate through the update worker, or, with only a newer zcp out, asks the
// platform for the container restart that installs both. With nothing newer it
// does nothing at all.

// mateFuseCutoff is how long the Mate goes without an HQ link before the fuse acts.
const mateFuseCutoff = time.Hour

// mateFuseRestartGrace is how long the fuse waits, after the platform accepted
// the container restart, for the restart to end this process. It equals the
// Mate server's own grace for the same restart (MATE_RESTART_GRACE, 5 minutes);
// the unit's TimeoutStartSec must exceed it.
var mateFuseRestartGrace = 5 * time.Minute

// mateFuseRecord is the fuse's own memory: when it last saw the link up, and the
// zcp release it already restarted the container for (one attempt per release).
type mateFuseRecord struct {
	LinkSeenAt      time.Time `json:"linkSeenAt,omitzero"`
	ZCPRestartedFor string    `json:"zcpRestartedFor,omitempty"`
}

func mateFuseRecordPath() string { return filepath.Join(mate.Prefix(), "fuse.json") }

func runMateFuse() int {
	lookup := mate.LiveLookup(mate.LiveEnvStorePath)
	rt := runtime.DetectFrom(lookup)
	if !rt.InContainer || !rt.MateEnabled {
		return 0
	}
	var rec mateFuseRecord
	if raw, err := os.ReadFile(mateFuseRecordPath()); err == nil {
		_ = json.Unmarshal(raw, &rec)
	}
	now := time.Now().UTC()
	doc, err := hq.NewFileMateStateSource(hq.MateStatePath(), rt.ProjectID).Read()
	if err == nil && doc.Link == hq.MateLinkLinked {
		rec.LinkSeenAt = now
		return writeMateFuseRecord(rec)
	}
	// The hour counts from the later of the link last seen up and the installed
	// Mate's activation: a Mate that never linked counts from its install.
	since := rec.LinkSeenAt
	if info, err := os.Lstat(mate.CurrentLink()); err == nil && info.ModTime().After(since) {
		since = info.ModTime().UTC()
	}
	if since.IsZero() || now.Sub(since) < mateFuseCutoff {
		return 0
	}
	why := doc.Reason
	if err != nil {
		why = err.Error()
	}
	fmt.Fprintf(os.Stderr, "mate fuse: no HQ link since %s (%s)\n", since.Format(time.RFC3339), why)

	status := readMateStatus(server.Version, true, false)
	if phase := status.Updater.Phase; phase != updaterIdle && !mate.UpdateSettledPhase(phase) {
		fmt.Fprintf(os.Stderr, "mate fuse: the updater is %s; nothing to do now\n", phase)
		return 0
	}
	if status.Installed != "" && status.Latest != "" && mate.AutomaticUpdateAllowed(status.Installed, status.Updater.FailedVersion, mate.Manifest{Version: status.Latest}) {
		fmt.Fprintf(os.Stderr, "mate fuse: installing Mate %s over %s\n", status.Latest, status.Installed)
		if err := startMateUpdateWorker(); err != nil {
			fmt.Fprintf(os.Stderr, "mate fuse: %v\n", err)
			return 1
		}
		return 0
	}
	if status.ZCP.Behind && rec.ZCPRestartedFor != status.ZCP.Latest {
		return restartForZCP(rec, status, lookup)
	}
	fmt.Fprintf(os.Stderr, "mate fuse: nothing newer (Mate %s, latest %q, failed %q; zcp %s, latest %q)\n",
		status.Installed, status.Latest, status.Updater.FailedVersion, status.ZCP.Installed, status.ZCP.Latest)
	return 0
}

// restartForZCP brings a newer zcp in the way a container boot does: the
// restart's initCommands install it, then `zcp init` installs the latest Mate.
// The running Mate first proves idle behind its admission barrier, as for an
// attended update; a Mate that does not answer admitted no work.
func restartForZCP(rec mateFuseRecord, status mateStatusResult, lookup func(string) string) int {
	fmt.Fprintf(os.Stderr, "mate fuse: zcp %s is out (installed %s); restarting the container once the Mate is idle\n", status.ZCP.Latest, status.ZCP.Installed)
	_, err := mate.DrainUpdate(status.Installed, false)
	if err != nil && !errors.Is(err, mate.ErrNotAnswering) {
		_ = mate.PostUpdate("cancel")
		fmt.Fprintf(os.Stderr, "mate fuse: restart postponed: %v\n", err)
		return 0
	}
	drained := err == nil
	tried := rec.ZCPRestartedFor
	rec.ZCPRestartedFor = status.ZCP.Latest
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
		// restart never came and the drain is cancelled so the Mate admits work
		// again. The record keeps the release: one attempt per release.
		time.Sleep(mateFuseRestartGrace)
		fmt.Fprintf(os.Stderr, "mate fuse: the container did not restart within %s\n", mateFuseRestartGrace)
	}
	if drained {
		_ = mate.PostUpdate("cancel")
	}
	return code
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
		fmt.Fprintf(os.Stderr, "mate fuse: write record: %v\n", err)
		return 1
	}
	return 0
}
