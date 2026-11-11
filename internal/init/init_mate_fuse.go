package init

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"

	"github.com/zeropsio/zcp/internal/runtime"
)

// The cut-off fuse's timer (docs/spec-mate.md §2.1e): systemd runs `zcp mate
// fuse` every few minutes with jitter, independent of the Mate's own unit, so it
// runs while that unit is down, crash-looping or fenced. Enabled, so systemd
// starts it at boot even when `zcp init` fails before its Mate step.
const (
	mateFuseName = "zcp-mate-fuse"
	mateFuseDir  = "/etc/systemd/system"
)

// ensureMateFuse writes the fuse's service and timer, reloads systemd when either
// changed, and enables and starts the timer. A failure is a named line, never a
// stop: the Mate runs without its fuse.
func ensureMateFuse() {
	if err := installMateFuse(); err != nil {
		fmt.Fprintf(os.Stderr, "    ! Mate cut-off fuse not installed: %v\n", err)
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
		mateFuseName + ".service": "[Unit]\nDescription=zcp: update a Mate cut off from HQ for an hour\n\n" +
			"[Service]\nType=oneshot\nUser=" + me.Uid + "\nEnvironment=HOME=" + runtime.HomeDir() + "\n" +
			"ExecStart=" + bin + " mate fuse\nTimeoutStartSec=15min\n",
		mateFuseName + ".timer": "[Unit]\nDescription=zcp: Mate cut-off fuse\n\n" +
			"[Timer]\nOnBootSec=10min\nOnUnitInactiveSec=5min\nRandomizedDelaySec=2min\n\n" +
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
	if err := commandRunner("sudo", "rm", "-f", timer, filepath.Join(mateFuseDir, mateFuseName+".service")); err != nil {
		return fmt.Errorf("remove %s units: %w", mateFuseName, err)
	}
	if err := commandRunner("sudo", "systemctl", "daemon-reload"); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w", err)
	}
	return nil
}
