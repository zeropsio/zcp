//go:build !unix

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
)

// replaceWithCommand runs the command at path where a process cannot become
// another: as a child on this process's streams, awaited, its exit code
// returned. Ctrl-C reaches the child from the console; zcp catches it into a
// channel nobody reads, so it stays to report the command's own exit.
func replaceWithCommand(path string, argv, env []string) (int, error) {
	signal.Notify(make(chan os.Signal, 1), os.Interrupt)
	cmd := exec.CommandContext(context.Background(), path) //nolint:gosec // the command the caller named, as `zcp with` exists to run it
	cmd.Args, cmd.Env = argv, env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0, nil
	case errors.As(err, &exit):
		return capturedExitCode(exit), nil
	}
	return 0, fmt.Errorf("could not be started: %w", err)
}
