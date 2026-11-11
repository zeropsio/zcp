package init

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"

	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/runtime"
)

// The keep job's units (docs/spec-mate.md §2.1d): systemd runs `zcp mate fuse`
// every few minutes with jitter, independent of the Mate's own unit, so it runs
// while that unit is down or crash-looping. The service is a template whose
// instance is the way it asks the Mate: `automatic` (the timer's, and the
// server's own automatic update) or `attended` (a person's or an operator's
// `zcp mate update`). The timer is enabled, so systemd starts it at boot even
// when `zcp init` fails before its Mate step. The names stay those of the
// cut-off fuse the keep job replaced: a renamed timer would leave the old one
// enabled on every running container.
const (
	mateFuseName = mate.KeepUnitName
	mateFuseDir  = "/etc/systemd/system"
)

// ensureMateFuse writes the keep job's service and timer, reloads systemd when
// either changed, and enables and starts the timer. A failure is a named line,
// never a stop: the Mate runs without its keep job.
func ensureMateFuse() {
	if err := installMateFuse(); err != nil {
		fmt.Fprintf(os.Stderr, "    ! Mate keep job not installed: %v\n", err)
	}
}

func installMateFuse() error {
	bin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find zcp: %w", err)
	}
	me, err := user.Current()
	if err != nil {
		return fmt.Errorf("find user: %w", err)
	}
	units := map[string]string{
		mateFuseName + "@.service": "[Unit]\nDescription=zcp: keep the Mate on the newest version that works (%i)\n\n" +
			"[Service]\nType=oneshot\nUser=" + me.Uid + "\nEnvironment=HOME=" + runtime.HomeDir() + "\n" +
			"ExecStart=" + bin + " mate fuse --%i\nTimeoutStartSec=20min\n",
		mateFuseName + ".timer": "[Unit]\nDescription=zcp: Mate keep job\n\n" +
			"[Timer]\nOnBootSec=10min\nOnUnitInactiveSec=5min\nRandomizedDelaySec=2min\nUnit=" + mateFuseName + "@automatic.service\n\n" +
			"[Install]\nWantedBy=timers.target\n",
	}
	changed := false
	for name, body := range units {
		path := filepath.Join(mateFuseDir, name)
		if have, err := os.ReadFile(path); err == nil && string(have) == body {
			continue
		}
		tmp, err := os.CreateTemp("", name+"-*")
		if err != nil {
			return fmt.Errorf("stage %s: %w", name, err)
		}
		_, werr := tmp.WriteString(body)
		cerr := tmp.Close()
		if err := errors.Join(werr, cerr); err != nil {
			_ = os.Remove(tmp.Name())
			return fmt.Errorf("stage %s: %w", name, err)
		}
		err = commandRunner("sudo", "install", "-m", "0644", tmp.Name(), path)
		_ = os.Remove(tmp.Name())
		if err != nil {
			return fmt.Errorf("install %s: %w", path, err)
		}
		changed = true
	}
	if changed {
		if err := commandRunner("sudo", "systemctl", "daemon-reload"); err != nil {
			return fmt.Errorf("systemctl daemon-reload: %w", err)
		}
	}
	if err := commandRunner("sudo", "systemctl", "enable", "--now", mateFuseName+".timer"); err != nil {
		return fmt.Errorf("enable %s.timer: %w", mateFuseName, err)
	}
	return nil
}

// removeMateFuse undoes ensureMateFuse when Mate is turned off (§2.1b).
func removeMateFuse() error {
	timer := filepath.Join(mateFuseDir, mateFuseName+".timer")
	if _, err := os.Stat(timer); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err := commandRunner("sudo", "systemctl", "disable", "--now", mateFuseName+".timer"); err != nil {
		fmt.Fprintf(os.Stderr, "    ! systemctl disable %s.timer failed (continuing): %v\n", mateFuseName, err)
	}
	if err := commandRunner("sudo", "rm", "-f", timer, filepath.Join(mateFuseDir, mateFuseName+"@.service")); err != nil {
		return fmt.Errorf("remove %s units: %w", mateFuseName, err)
	}
	if err := commandRunner("sudo", "systemctl", "daemon-reload"); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w", err)
	}
	return nil
}
