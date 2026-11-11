package mate

import (
	"fmt"
	"os"
	"os/user"
	"strconv"

	"github.com/zeropsio/zcp/internal/runtime"
)

// UpdateWorkerArgv is the command, run through sudo, that starts the update
// worker, `zcp mate update --worker`, as an independent systemd transient
// unit, outside the cgroup of the Mate it switches and of whoever asked: the
// one owner of every switch of a running Mate (`zcp mate update`, `zcp
// init`). Its ExecStopPost recovers a switch the worker left unfinished, so a
// switch outlives an interrupted caller. flags are the worker's own:
// --automatic, --force, --restart.
func UpdateWorkerArgv(flags ...string) ([]string, error) {
	binary, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("find updater executable: %w", err)
	}
	currentUser, err := user.Current()
	if err != nil {
		return nil, fmt.Errorf("find updater user: %w", err)
	}
	// No stop timeout: recovery follows a boot while it progresses, and every
	// step of it is bounded by itself (the progress window, the install lock
	// wait, the commit resend, each systemctl call).
	argv := []string{"systemd-run", "--collect", "--unit=zcp-mate-update", "--property=Type=exec", "--property=TimeoutStopSec=infinity", "--property=ExecStopPost=" + strconv.Quote(binary) + " mate update --recover --worker", "--uid=" + currentUser.Uid, "--setenv=HOME=" + runtime.HomeDir()}
	if override := os.Getenv("ZCP_MATE_MANIFEST_URL"); override != "" {
		argv = append(argv, "--setenv=ZCP_MATE_MANIFEST_URL="+override)
	}
	argv = append(argv, binary, "mate", "update", "--worker")
	return append(argv, flags...), nil
}
