package ops

import (
	"context"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/zeropsio/zcp/internal/topology"
)

// A service's code is sshfs-mounted into the zcp container, and a command
// started there runs on zcp: its runtime and dependencies read file by file
// over the mount, on the container the agent itself runs in. `zcp run` runs
// the command in the service instead, over ssh, as `zcp git` runs git there.

// BuildServiceRunCommand is the remote command of a `zcp run` call: a script
// that changes into dir and runs argv, each argument through ShellQuote, so
// every byte the caller gave reaches the command as data. Like a `zcp git`
// call's (BuildBrokeredGitCommand) the script runs under /bin/sh whatever
// the login shell, ShellQuoted whole as its one -c argument. The command is
// not exec'd: a shell builtin (`command -v`, `test`) is a command too.
func BuildServiceRunCommand(dir string, argv []string) string {
	var b strings.Builder
	b.WriteString("cd ")
	b.WriteString(ShellQuote(dir))
	b.WriteString(" &&")
	for _, a := range argv {
		b.WriteByte(' ')
		b.WriteString(ShellQuote(a))
	}
	return "/bin/sh -c " + ShellQuote(b.String())
}

// ServiceRunDir is the directory of host a `zcp run` call made from cwd runs
// in. zcp mounts host's /var/www at <CodeMountRoot>/<host> (platform.Mount),
// so a cwd in a folder of that mount names the same folder of the service,
// and the command runs there — typed in a package's folder, it runs in that
// package. From anywhere else it runs in checkout, where the service's code
// is.
func ServiceRunDir(host, checkout, cwd string) string {
	if !path.IsAbs(cwd) {
		return checkout
	}
	mount := path.Join(topology.CodeMountRoot, host)
	if rel, inMount := strings.CutPrefix(path.Clean(cwd), mount+"/"); inMount {
		return path.Join(topology.CodeMountRoot, rel)
	}
	return checkout
}

// RunInService runs argv in host at dir over ssh, the caller's stdin, stdout
// and stderr passed through, and returns the command's exit code. When no
// answer of the command came back — ssh ended with its own 255, could not
// run, or ctx stopped it — it returns an error saying so, with exit code 255
// (or -1 when ssh did not end by itself). A stopped call's command may still
// be running in host: nothing but its own end, or a limit it was started
// under, stops it.
func RunInService(ctx context.Context, ssh SSHStdioRunner, host, dir string, argv []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	exit, err := ssh.ExecSSHStdio(ctx, host, BuildServiceRunCommand(dir, argv), stdin, stdout, stderr)
	if err != nil {
		return exit, fmt.Errorf("run the command over ssh: %w", err)
	}
	if exit == sshTransportExit {
		return exit, fmt.Errorf("ssh to %s failed (exit %d): the message above is ssh's own, not the command's answer", host, exit)
	}
	return exit, nil
}
