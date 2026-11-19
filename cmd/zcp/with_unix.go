//go:build unix

package main

import (
	"errors"
	"fmt"
	"syscall"
)

// replaceWithCommand makes this process the command at path: its pid, its
// streams and every signal sent to it are the command's from here on, and so
// is the exit code. A file the system does not take for a program is run as
// a shell runs it (asShellScript). It returns only when the command could
// not be started.
func replaceWithCommand(path string, argv, env []string) (int, error) {
	err := syscall.Exec(path, argv, env)
	if shell, shellArgv, ok := asShellScript(err, path, argv); ok {
		err = syscall.Exec(shell, shellArgv, env)
	}
	return 0, fmt.Errorf("could not be started: %w", err)
}

// withShell is the shell a script with no interpreter line is run with.
const withShell = "/bin/sh"

// asShellScript answers how to run a file the system refused as a program
// with "exec format error" — an executable script with no `#!` line: with
// withShell, the file first and the command's arguments after it, as sh, bash,
// zsh and env all run one. ok is false for any other refusal.
func asShellScript(refusal error, path string, argv []string) (shell string, shellArgv []string, ok bool) {
	if !errors.Is(refusal, syscall.ENOEXEC) {
		return "", nil, false
	}
	return withShell, append([]string{"sh", path}, argv[1:]...), true
}
