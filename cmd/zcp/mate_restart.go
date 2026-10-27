package main

import (
	"context"
	"fmt"
	"time"

	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/update"
)

// A container restart is the whole update of a Mate (docs/spec-mate.md §2.9):
// the zcp service's run.initCommands install the latest zcp release
// (install.sh), then `zcp init` installs the stable Mate. `zcp mate status`
// says when one would change something — restartNeeded and why — and
// `zcp self restart` is how the Mate server asks for one.

// zcpReleaseCacheTTL is how long `zcp mate status` trusts a cached zcp
// release: the Mate manifest's hour (§2.1c), so a status reaches either
// release source at most hourly. The file is the self-update's cache
// (internal/update), which trusts an entry for a day.
const zcpReleaseCacheTTL = time.Hour

// zcpReleaseStatus is `zcp mate status --json`'s "zcp" key: this binary
// against the release install.sh installs, the tag GitHub's releases/latest
// names.
type zcpReleaseStatus struct {
	Installed string `json:"installed"`
	Latest    string `json:"latest"`
	Behind    bool   `json:"behind"`
}

// startZCPReleaseLookup starts reading the latest zcp release beside the
// manifest read and returns the wait for it. The answer is nil when the
// release is unknown (offline, rate-limited, nothing cached under --local),
// and for a build no release names (update.IsRelease) — a dev build, compared
// with nothing and never sent to the network, since a restart would replace
// it.
func startZCPReleaseLookup(ctx context.Context, installed string, opts update.LatestOptions) func() *zcpReleaseStatus {
	if !update.IsRelease(installed) {
		return func() *zcpReleaseStatus { return nil }
	}
	done := make(chan *zcpReleaseStatus, 1)
	go func() {
		checker := update.NewChecker(installed)
		checker.CacheTTL = zcpReleaseCacheTTL
		latest, err := checker.Latest(ctx, opts)
		if err != nil {
			done <- nil
			return
		}
		done <- &zcpReleaseStatus{Installed: installed, Latest: latest, Behind: update.Behind(installed, latest)}
	}()
	return func() *zcpReleaseStatus { return <-done }
}

// mateWithoutUpdater reports whether the running Mate proved it predates
// updater protocol 1: its loopback readiness answered exactly 404, the answer
// mateUpdateProbe reads as "endpoint absent". Such a Mate is moved onto the
// updater by one restart (§2.1d). Every other answer — protocol 1, an
// unknown protocol, no answer — is false, never a guess. The Mate is asked
// only where a newer Mate is out, which the restart would install, and only
// in an enabled Mate container: elsewhere port 3773 is an ordinary user port.
func mateWithoutUpdater(updateAvailable bool) bool {
	if !updateAvailable {
		return false
	}
	rt := runtime.DetectFrom(mate.LiveLookup(mate.LiveEnvStorePath))
	if !rt.InContainer || !rt.MateEnabled {
		return false
	}
	supported, err := mateUpdateProbe()
	return err == nil && !supported
}

// restartVerdict is whether a container restart would change this container,
// and why, in one line for a person: zcp is behind the release install.sh
// installs, or the running Mate has no updater while a newer Mate is out.
func restartVerdict(zcp *zcpReleaseStatus, mateInstalled, mateLatest string, mateNoUpdater bool) (bool, string) {
	zcpBehind := zcp != nil && zcp.Behind
	switch {
	case zcpBehind && mateNoUpdater:
		return true, fmt.Sprintf("zcp %s is out and mate %s has no updater; a restart installs both", zcp.Latest, mateInstalled)
	case zcpBehind:
		return true, fmt.Sprintf("zcp %s is out; a restart installs it", zcp.Latest)
	case mateNoUpdater:
		return true, fmt.Sprintf("mate %s has no updater; a restart installs mate %s", mateInstalled, mateLatest)
	default:
		return false, ""
	}
}
