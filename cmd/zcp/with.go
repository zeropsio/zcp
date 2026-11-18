package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"slices"
	"strings"

	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/ops"
)

// `zcp with <KEY>[,<KEY>…] [--] <command> [<args>…]` runs one command with
// the named values in its environment (docs/spec-mate.md §5.8a). The values
// are read from the container's live env store as it is now — a value the
// person saved a moment ago is there about two seconds after the write,
// while the caller's own environment is the one it started with — and set on
// top of the caller's. zcp then replaces itself with the command
// (replaceWithCommand): stdin, stdout, stderr, signals and the exit code are
// the command's. A call is refused before anything runs, with gitExitRefused
// and one line on stderr saying why, for a key the store does not hold, a
// name Mate and zcp keep for themselves (ops.IsVaultHidden), an argument that
// is not key names, no command, and a store it cannot read. No line of
// zcp's carries a value: an argument that is not a key's name is never
// echoed, and a store that does not parse is not quoted. Like `zcp run` it
// writes nothing to stdout itself and runs before CLI telemetry.

const withUsage = "usage: zcp with <KEY>[,<KEY>...] [--] <command> [<args>...]"

// withVerbName is the subcommand.
const withVerbName = "with"

// A command that is not found or cannot be started ends the call as a shell
// ends it.
const (
	withExitNotFound   = 127
	withExitNotStarted = 126
)

// withDeps is what `zcp with` reads and starts.
type withDeps struct {
	// storePath is the container's live env store (mate.LiveEnvStorePath).
	storePath string
	// environ is the caller's environment, which the command keeps.
	environ  []string
	lookPath func(command string) (string, error)
	// replace hands the process to the command (replaceWithCommand): the
	// exit code it returns is the command's, an error that the command could
	// not be started.
	replace func(path string, argv, env []string) (int, error)
	stderr  io.Writer
}

// isWith recognizes `zcp with` before CLI telemetry can add output to the
// command's streams or delay its exit.
func isWith(args []string) bool {
	return len(args) >= 1 && args[0] == withVerbName
}

// runWith is `zcp with` in this container: the live env store, the caller's
// environment and PATH, and the process handed to the command.
func runWith(args []string) int {
	return runWithCmd(args, withDeps{
		storePath: mate.LiveEnvStorePath,
		environ:   os.Environ(),
		lookPath:  exec.LookPath,
		replace:   replaceWithCommand,
		stderr:    os.Stderr,
	})
}

// runWithCmd runs `zcp with` with args — everything after "with" — on d.
func runWithCmd(args []string, d withDeps) int {
	refuse := func(msg string) int {
		fmt.Fprintln(d.stderr, "zcp with: "+msg)
		return gitExitRefused
	}
	if len(args) == 0 {
		return refuse("no key given; " + withUsage)
	}
	keys := strings.Split(args[0], ",")
	for _, key := range keys {
		if !ops.IsEnvKeyName(key) {
			// Not echoed: what stands where the key names belong may be a
			// KEY=value.
			return refuse("the first argument is key names only (letters, digits and `_`, joined by commas), never KEY=value; " + withUsage)
		}
		if ops.IsVaultHidden(key) {
			return refuse(key + " is a name Mate and zcp keep for themselves; zcp with hands it to no command")
		}
	}
	argv := args[1:]
	if len(argv) > 0 && argv[0] == "--" {
		argv = argv[1:]
	}
	if len(argv) == 0 {
		return refuse("no command given; " + withUsage)
	}
	lines, err := mate.LoadLiveEnv(d.storePath)
	if err != nil {
		return refuse(storeUnreadWords(err))
	}
	held := make(map[string]string, len(lines))
	for _, line := range lines {
		if key, value, ok := strings.Cut(line, "="); ok {
			held[key] = value
		}
	}
	env := slices.Clone(d.environ)
	for _, key := range keys {
		value, ok := held[key]
		if !ok {
			return refuse(fmt.Sprintf("%s is not in this container's env, and nothing was run: it is not saved yet (an open ask waits for the person), "+
				"was saved moments ago (it arrives about two seconds after the write: run this again), "+
				"or was never asked for (zerops_env action=\"request\" mate=true key=%q)", key, key))
		}
		env = withValue(env, key, value)
	}
	path, err := d.lookPath(argv[0])
	if err != nil {
		fmt.Fprintf(d.stderr, "zcp with: %s: command not found\n", argv[0])
		return withExitNotFound
	}
	code, err := d.replace(path, argv, env)
	if err != nil {
		fmt.Fprintf(d.stderr, "zcp with: %s: %v\n", argv[0], err)
		return withExitNotStarted
	}
	return code
}

// withValue is env with key set to value, whatever env held under key.
func withValue(env []string, key, value string) []string {
	env = slices.DeleteFunc(env, func(entry string) bool {
		return strings.HasPrefix(entry, key+"=")
	})
	return append(env, key+"="+value)
}

// storeUnreadWords says why the live env store gave no values. Only the
// system's own error on the file is quoted: one from reading its content may
// quote the content.
func storeUnreadWords(err error) string {
	var onFile *fs.PathError
	if errors.As(err, &onFile) {
		return fmt.Sprintf("this container's env store could not be read (%v); zcp with runs in a Zerops container, where the platform keeps it", onFile)
	}
	return "this container's env store does not read as the platform writes it; nothing was run"
}
