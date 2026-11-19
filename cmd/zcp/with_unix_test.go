//go:build unix

// Tests for: with_unix.go — `zcp with` runs an executable script that has no
// interpreter line as a shell does (docs/spec-mate.md §5.8a).

package main

import (
	"fmt"
	"slices"
	"syscall"
	"testing"
)

// An executable script with no `#!` line runs with /bin/sh, the script
// first and its arguments after, as sh, bash, zsh and env run it — never
// "cannot be run" with 126, which a script written without that line would
// get from nothing else. Any other refusal of the system stays a refusal.
// witness: cmd/zcp/with_unix.go | append([]string{"sh", path}, argv[1:]...) → argv
func TestWith_AScriptWithNoInterpreterLine_RunsWithSh(t *testing.T) {
	t.Parallel()

	refused := fmt.Errorf("fork/exec /work/bin/sign-in: %w", syscall.ENOEXEC)
	shell, argv, ok := asShellScript(refused, "/work/bin/sign-in", []string{"sign-in", "--as", "first"})
	if !ok || shell != "/bin/sh" || !slices.Equal(argv, []string{"sh", "/work/bin/sign-in", "--as", "first"}) {
		t.Errorf("a script with no interpreter line: run %q %q (ok %v), want the shell with the script and its arguments", shell, argv, ok)
	}
	if _, _, ok := asShellScript(syscall.EACCES, "/work/bin/sign-in", []string{"sign-in"}); ok {
		t.Error("a file the system may not run was handed to /bin/sh")
	}
}
