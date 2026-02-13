//go:build !windows

package update

import (
	"fmt"
	"os"
	"syscall"
)

// Exec replaces the current process with a fresh execution of the binary.
// On success this function never returns (the process is replaced).
func Exec() error {
	binary, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}

	return syscall.Exec(binary, os.Args, os.Environ())
}
