package ops

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
)

// `zcp git <host> <git args…>` runs one git command inside the dev service
// host, over ssh, in that service's checkout — never in the zcp container
// and never on its sshfs mount of the service. Two git clients on one .git,
// one of them on the mount, misread and break the repository, and git run
// on the mount executes the dev service's hooks and config commands in the
// zcp container. The boundary is that git runs over ssh in that service;
// the argument rules and the environment scrub below are guardrails.

// SSHStdioRunner is the side of an SSH deployer that runs a remote command
// with the caller's own stdin, stdout and stderr wired to the session and no
// deadline of its own, returning the exit code ssh ended with; a nil stdin
// is the null device. It is an interface of its own, so every SSHDeployer
// fake stays as it is. platform.SystemSSHDeployer implements it.
type SSHStdioRunner interface {
	ExecSSHStdio(ctx context.Context, hostname, command string, stdin io.Reader, stdout, stderr io.Writer) (int, error)
}

var _ SSHStdioRunner = (*platform.SystemSSHDeployer)(nil)

// BrokeredGitArgs is the git argv of one `zcp git` call, checked by
// ParseBrokeredGitArgs: the leading options it accepts, the subcommand, and
// everything after the subcommand as given.
type BrokeredGitArgs struct {
	// Config is each leading `-c <key=value>`, in order.
	Config []string
	// Dir is the leading `-C <dir>`, cleaned: an absolute path strictly below
	// /tmp/ (a dev-side worktree); "" for none.
	Dir string
	// Subcommand is the first argument that is no leading option.
	Subcommand string
	// Rest is everything after the subcommand — the subcommand's own.
	Rest []string
}

// brokeredGitDirRoot is the only tree a `-C` may name: dev-side worktrees
// live below it, and the checkout itself is reached without -C.
const brokeredGitDirRoot = "/tmp/"

// ParseBrokeredGitArgs checks the git argv of a `zcp git` call. Before the
// subcommand only `-c <key=value>` and a single `-C <dir>` are accepted, the
// dir cleaned and strictly below /tmp/; every other leading option
// (--git-dir, --work-tree, --exec-path, …) is refused, and so is a call with
// no subcommand. What follows the subcommand is the subcommand's and is not
// read.
func ParseBrokeredGitArgs(args []string) (BrokeredGitArgs, error) {
	var parsed BrokeredGitArgs
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-c":
			if i+1 >= len(args) || args[i+1] == "" {
				return BrokeredGitArgs{}, errors.New("-c needs a <key>=<value> after it")
			}
			i++
			parsed.Config = append(parsed.Config, args[i])
		case arg == "-C":
			if i+1 >= len(args) {
				return BrokeredGitArgs{}, errors.New("-C needs a directory under /tmp/ after it")
			}
			if parsed.Dir != "" {
				return BrokeredGitArgs{}, errors.New("-C may be given once")
			}
			i++
			dir, err := brokeredGitDir(args[i])
			if err != nil {
				return BrokeredGitArgs{}, err
			}
			parsed.Dir = dir
		case strings.HasPrefix(arg, "-"):
			return BrokeredGitArgs{}, fmt.Errorf("%q is refused before the subcommand: only -c <key>=<value> and -C <dir under /tmp/> are accepted there", arg)
		case arg == "":
			return BrokeredGitArgs{}, errors.New("no git subcommand given")
		default:
			parsed.Subcommand = arg
			if rest := args[i+1:]; len(rest) > 0 {
				parsed.Rest = rest
			}
			return parsed, nil
		}
	}
	return BrokeredGitArgs{}, errors.New("no git subcommand given")
}

// brokeredGitDir is dir cleaned, when it is an absolute path strictly below
// /tmp/ — a dev-side worktree — and refuses anything else: the checkout, /,
// /tmp itself, a relative path, or one that climbs out of /tmp/ with "..".
func brokeredGitDir(dir string) (string, error) {
	clean := path.Clean(dir)
	if !path.IsAbs(dir) || !strings.HasPrefix(clean, brokeredGitDirRoot) {
		return "", fmt.Errorf("-C %q is refused: only an absolute path below %s (a worktree of the checkout) is accepted; the checkout itself is reached without -C", dir, brokeredGitDirRoot)
	}
	return clean, nil
}

// brokeredGitScrubbedEnv are the variables the remote command unsets before
// git runs: tracing that would print git's traffic (credentials included),
// TLS checks turned off or pointed elsewhere, askpass prompters, overrides of
// which repository, executable, ssh or proxy command git uses, config read
// from elsewhere or injected through the environment, and curl's proxies.
// A guardrail, not the boundary: the dev service's shell startup runs before
// the command.
const brokeredGitScrubbedEnv = "" +
	"GIT_TRACE GIT_TRACE_PACKET GIT_TRACE_PACK_ACCESS GIT_TRACE_PACKFILE " +
	"GIT_TRACE_PERFORMANCE GIT_TRACE_SETUP GIT_TRACE_SHALLOW GIT_TRACE_REFS " +
	"GIT_TRACE_FSMONITOR GIT_TRACE_CURL GIT_TRACE_CURL_NO_DATA " +
	"GIT_TRACE2 GIT_TRACE2_EVENT GIT_TRACE2_PERF GIT_TRACE2_BRIEF " +
	"GIT_TRACE2_EVENT_NESTING GIT_TRACE2_CONFIG_PARAMS GIT_TRACE2_ENV_VARS " +
	"GIT_CURL_VERBOSE GIT_TRACE_REDACT " +
	"GIT_SSL_NO_VERIFY GIT_SSL_CAINFO GIT_SSL_CAPATH GIT_SSL_CERT " +
	"GIT_SSL_KEY GIT_SSL_CERT_PASSWORD_PROTECTED GIT_SSL_VERSION GIT_SSL_CIPHER_LIST " +
	"GIT_ASKPASS SSH_ASKPASS SSH_ASKPASS_REQUIRE " +
	"GIT_EXEC_PATH GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_COMMON_DIR " +
	"GIT_OBJECT_DIRECTORY GIT_ALTERNATE_OBJECT_DIRECTORIES " +
	"GIT_SSH GIT_SSH_COMMAND GIT_PROXY_COMMAND " +
	"GIT_CONFIG_GLOBAL GIT_CONFIG_SYSTEM GIT_CONFIG_PARAMETERS GIT_CONFIG_COUNT " +
	"http_proxy HTTP_PROXY https_proxy HTTPS_PROXY ALL_PROXY all_proxy NO_PROXY no_proxy"

// BuildBrokeredGitCommand is the remote command of a `zcp git` call: a
// script that unsets the scrubbed variables, changes into dir — the checkout
// — and execs git with terminal prompts off, each argument through
// ShellQuote, so every byte the caller gave reaches git as data. sshd hands
// the command to the service user's login shell (bash on one image, busybox
// ash on alpine), so the script runs under /bin/sh, ShellQuoted whole as its
// one -c argument: the login shell only splits those three words.
func BuildBrokeredGitCommand(dir string, args BrokeredGitArgs) string {
	argv := make([]string, 0, 2+2*len(args.Config)+1+len(args.Rest))
	if args.Dir != "" {
		argv = append(argv, "-C", args.Dir)
	}
	for _, c := range args.Config {
		argv = append(argv, "-c", c)
	}
	argv = append(argv, args.Subcommand)
	argv = append(argv, args.Rest...)
	var b strings.Builder
	b.WriteString("unset ")
	b.WriteString(brokeredGitScrubbedEnv)
	b.WriteString("; cd ")
	b.WriteString(ShellQuote(dir))
	b.WriteString(" && GIT_TERMINAL_PROMPT=0 exec git")
	for _, a := range argv {
		b.WriteByte(' ')
		b.WriteString(ShellQuote(a))
	}
	return "/bin/sh -c " + ShellQuote(b.String())
}

// sshTransportExit is the exit code ssh itself ends with when it fails —
// cannot connect, cannot authenticate, loses the connection.
const sshTransportExit = 255

// RunBrokeredGit runs git with args in host's checkout at dir over ssh, the
// caller's stdout and stderr passed through, and returns git's exit code.
// The caller's stdin reaches ssh only when the argv makes git read it
// (BrokeredGitReadsStdin, the rule a relayed call forwards by); any other
// call hands ssh none, so its remote reads nothing and a caller reading its
// stdin on — a loop over hosts — keeps it. When no git verdict came back —
// ssh ended with its own 255, could not run, or ctx stopped it — it returns
// an error saying so, with exit code 255 (or -1 when ssh did not end by
// itself). A stopped call's remote git may still be running; whatever lock
// it holds clears when it finishes.
func RunBrokeredGit(ctx context.Context, ssh SSHStdioRunner, host, dir string, args BrokeredGitArgs, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	if !BrokeredGitReadsStdin(args) {
		stdin = nil
	}
	exit, err := ssh.ExecSSHStdio(ctx, host, BuildBrokeredGitCommand(dir, args), stdin, stdout, stderr)
	if err != nil {
		return exit, fmt.Errorf("run git over ssh: %w", err)
	}
	if exit == sshTransportExit {
		return exit, fmt.Errorf("ssh to %s failed (exit %d): the message above is ssh's own, not git's answer", host, exit)
	}
	return exit, nil
}
