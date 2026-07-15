//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package capture

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

func lockManagerFile(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_EX)
}

func unlockManagerFile(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_UN)
}

func managerConnectionRefused(err error) bool {
	return errors.Is(err, unix.ECONNREFUSED)
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	if err := unix.Kill(pid, 0); err != nil {
		return errors.Is(err, unix.EPERM)
	}
	// kill(2) reports a zombie as present even though it can no longer own a
	// listener or execute capture code. Treat that terminal state as exited;
	// the daemon starter's Wait goroutine remains responsible for reaping its
	// own child.
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	output, err := exec.CommandContext(ctx, "ps", "-p", strconv.Itoa(pid), "-o", "stat=").Output() //nolint:gosec // fixed ps arguments; pid is decimal
	if err == nil && strings.HasPrefix(strings.TrimSpace(string(output)), "Z") {
		return false
	}
	return true
}
