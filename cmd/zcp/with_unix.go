//go:build unix

package main

import (
	"fmt"
	"syscall"
)

// replaceWithCommand makes this process the command at path: its pid, its
// streams and every signal sent to it are the command's from here on, and so
// is the exit code. It returns only when the command could not be started.
func replaceWithCommand(path string, argv, env []string) (int, error) {
	err := syscall.Exec(path, argv, env)
	return 0, fmt.Errorf("could not be started: %w", err)
}
