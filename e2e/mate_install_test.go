//go:build e2e

// Tests for: e2e — the zcp↔Mate seam (docs/spec-mate.md §2.1c, §2.2): a
// released Mate installs through zcp's real download → SHA-256 → install path
// and boots `mate serve` under the zerops@mate unit with the launch
// environment. The argv contract stays a unit test
// (TestServeArgv_FlagsAreContract1); only a live boot shows a release's CLI
// accepting it.
//
// Runs ON the e2e container (make e2e-deploy), which must have Mate enabled
// (a registered zerops@mate unit). The unit's ExecStart is the container's own
// `zcp service start mate`, so the argv, launch environment and start job are
// that zcp build's; this tree's download path runs in the test process. It leaves the
// release under test installed and serving, or, when red, installed and not
// serving.
//
// Prerequisites:
//   - ZCP_E2E_MATE_RELEASE: the manifest (stable.json) URL of the release under
//     test, e.g. https://github.com/zeropsio/mate/releases/download/v0.16.67/stable.json
//
// Run: ZCP_E2E_MATE_RELEASE=<manifest url> /var/www/e2e-test -test.run TestMateInstall_Release_Serves -test.v -test.timeout 600s

package e2e_test

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/mate"
)

const mateUnit = "zerops@" + mate.UnitName + ".service"

// witness: internal/mate/mate.go | if !strings.EqualFold(actualSHA256, expectedSHA256) { → if strings.EqualFold(actualSHA256, expectedSHA256) {
func TestMateInstall_Release_Serves(t *testing.T) {
	// non-parallel: t.Setenv steers the process-wide manifest URL, and the
	// case owns the container's one Mate install and unit.
	manifestURL := os.Getenv("ZCP_E2E_MATE_RELEASE")
	if manifestURL == "" {
		t.Skip("ZCP_E2E_MATE_RELEASE not set: no release to install")
	}
	t.Setenv("ZCP_MATE_MANIFEST_URL", manifestURL)

	loaded, err := exec.Command("systemctl", "show", mateUnit, "--property=LoadState", "--value").Output()
	if err != nil || strings.TrimSpace(string(loaded)) != "loaded" {
		t.Fatalf("precondition: %s is not registered on this container (LoadState %q, %v) — enable Mate on the e2e container", mateUnit, strings.TrimSpace(string(loaded)), err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	release, err := mate.DesiredRelease(ctx, http.DefaultClient, mate.ManifestOptions{Refresh: true})
	if err != nil {
		t.Fatalf("manifest step: %v", err)
	}

	// A stopped unit cannot restart into a half-removed install, and its own
	// start job cannot race this one.
	if out, err := exec.Command("sudo", "systemctl", "stop", mateUnit).CombinedOutput(); err != nil {
		t.Fatalf("stop step: %v: %s", err, out)
	}

	unlock, err := mate.LockInstall(4 * time.Minute)
	if err != nil {
		t.Fatalf("lock step: %v", err)
	}
	// Nothing installed, nothing came up, nothing failed: the unit's start job
	// starts the release the release list (refreshed above) names.
	removePath(t, mate.LastGoodLink())
	removePath(t, mate.FailedVersionPath())
	removePath(t, mate.CurrentLink())
	removePath(t, mate.VersionDir("dev"))
	removePath(t, mate.VersionDir(release.Version))
	stageCtx, cancelStage := context.WithTimeout(context.Background(), mate.InstallTimeout)
	err = mate.StageRelease(stageCtx, release, mate.ReplaceFailing)
	cancelStage()
	unlock()
	if err != nil {
		t.Fatalf("install step: %v", err)
	}

	if out, err := exec.Command("sudo", "systemctl", "start", "--no-block", mateUnit).CombinedOutput(); err != nil {
		t.Fatalf("start step: %v: %s", err, out)
	}
	// The start job names last-good once the release answers on the local
	// port with its version.
	deadline := time.Now().Add(2 * mate.BootWindow)
	for {
		if v, _ := mate.LastGoodVersion(); v == release.Version {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("health step: last-good does not name %s after %s", release.Version, 2*mate.BootWindow)
		}
		time.Sleep(time.Second)
	}
	if running, there := mate.ServerThere(mate.BootWindow); !there || running != release.Version {
		t.Fatalf("health step: the local port answers %q (there %v), want %s", running, there, release.Version)
	}
}

func removePath(t *testing.T, path string) {
	t.Helper()
	if err := os.RemoveAll(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("remove %s: %v", path, err)
	}
}
