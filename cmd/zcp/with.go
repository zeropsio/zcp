package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/runtime"
)

// `zcp with <KEY>[,<KEY>…] [--] <command> [<args>…]` runs one command with
// the named values in its environment (docs/spec-mate.md §5.8a). The values
// are read from the container's live env store as it is now
// (ops.LiveEnvValues) — a value the person saved a moment ago is there a few
// seconds after the write, while the caller's own environment is the one it
// started with — and set on top of the caller's. zcp then replaces itself
// with the command (replaceWithCommand): stdin, stdout, stderr, signals and
// the exit code are the command's. A call is refused before anything runs,
// with gitExitRefused and one line on stderr saying why, for no command and
// for each refusal of the store's reader: a key it does not hold, a name
// Mate and zcp keep for themselves, an argument that is not key names, and a
// store it cannot read. No line of zcp's carries a value. Like `zcp run` it
// writes nothing to stdout itself and runs before CLI telemetry.

const withUsage = "usage: zcp with <KEY>[,<KEY>...] [--] <command> [<args>...]"

// withVerbName is the subcommand.
const withVerbName = "with"

// A command that is not found, or is there and cannot be run, ends the call
// as a shell ends it.
const (
	withExitNotFound   = 127
	withExitNotStarted = 126
)

// withDeps is what `zcp with` reads and starts.
type withDeps struct {
	// storePath is the container's live env store (mate.LiveEnvStorePath).
	storePath string
	// environ is the caller's environment, which the command keeps.
	environ []string
	// inMate says this container is a Mate's: a key the store does not hold
	// is worded by it (ops.LiveEnvNotHeldWords).
	inMate   bool
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
	rt := runtime.DetectFrom(mate.LiveLookup(mate.LiveEnvStorePath))
	return runWithCmd(args, withDeps{
		storePath: mate.LiveEnvStorePath,
		environ:   os.Environ(),
		inMate:    rt.InContainer && rt.MateEnabled,
		lookPath:  lookCommand,
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
	argv := args[1:]
	if len(argv) > 0 && argv[0] == "--" {
		argv = argv[1:]
	}
	if len(argv) == 0 {
		return refuse("no command given; " + withUsage)
	}
	values, refused := ops.LiveEnvValues(d.storePath, keys)
	if refused != nil {
		return refuse(withRefusalWords(refused, d.inMate))
	}
	env := slices.Clone(d.environ)
	for _, key := range keys {
		env = withValue(env, key, values[key])
	}
	path, err := d.lookPath(argv[0])
	switch {
	case err == nil:
	case errors.Is(err, exec.ErrNotFound), errors.Is(err, fs.ErrNotExist):
		fmt.Fprintf(d.stderr, "zcp with: %s: command not found\n", argv[0])
		return withExitNotFound
	default:
		// It is there and cannot be run: not executable, a directory.
		cause := err
		var lookErr *exec.Error
		if errors.As(err, &lookErr) {
			cause = lookErr.Err
		}
		fmt.Fprintf(d.stderr, "zcp with: %s: cannot be run: %v\n", argv[0], cause)
		return withExitNotStarted
	}
	code, err := d.replace(path, argv, env)
	if err != nil {
		fmt.Fprintf(d.stderr, "zcp with: %s: %v\n", argv[0], err)
		return withExitNotStarted
	}
	return code
}

// withRefusalWords say why the store handed out no value, and what to do. No
// line carries a value: what stood where the key names belong is not echoed,
// and of a store that could not be read only the system's own error on the
// file is quoted — one from reading its content may quote the content.
func withRefusalWords(refused *ops.LiveEnvRefusal, inMate bool) string {
	switch refused.Kind {
	case ops.LiveEnvNotAName:
		return "the first argument is key names only (letters, digits and `_`, joined by commas), never KEY=value; " + withUsage
	case ops.LiveEnvHidden:
		return refused.Key + " is a name Mate and zcp keep for themselves; zcp with hands it to no command"
	case ops.LiveEnvNotHeld:
		return fmt.Sprintf("%s is not in this container's env, and nothing was run. %s",
			refused.Key, ops.LiveEnvNotHeldWords(refused.Key, inMate, "run this again"))
	case ops.LiveEnvStoreUnread:
		var onFile *fs.PathError
		if errors.As(refused.Err, &onFile) {
			return fmt.Sprintf("this container's env store could not be read (%v); zcp with runs in a Zerops container, where the platform keeps it", onFile)
		}
	}
	return "this container's env store does not read as the platform writes it; nothing was run"
}

// lookCommand finds the command as a shell does. A name with no path in it is
// looked for on PATH, and where no directory holds an executable of that
// name but one holds a file of it, the command is there and cannot be run —
// a shell's 126, not its 127 for a command that is nowhere.
func lookCommand(name string) (string, error) {
	path, err := exec.LookPath(name)
	if err == nil {
		return path, nil
	}
	if errors.Is(err, exec.ErrNotFound) && !strings.ContainsRune(name, filepath.Separator) {
		for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
			if info, statErr := os.Stat(filepath.Join(dir, name)); statErr == nil && !info.IsDir() {
				return "", &exec.Error{Name: name, Err: fs.ErrPermission}
			}
		}
	}
	return "", fmt.Errorf("look for the command: %w", err)
}

// withValue is env with key set to value, whatever env held under key.
func withValue(env []string, key, value string) []string {
	env = slices.DeleteFunc(env, func(entry string) bool {
		return strings.HasPrefix(entry, key+"=")
	})
	return append(env, key+"="+value)
}
