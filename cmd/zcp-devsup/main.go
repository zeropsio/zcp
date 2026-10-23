// Command zcp-devsup is the dev-server supervisor a dev service runs as its
// run.start (docs/spec-mate.md §2.8a.2):
//
//	zcp-devsup run [--root <dir>] [--state <dir>] [--service <hostname>]
//	zcp-devsup ctl status [name] | start|stop|restart <name> | reload
//	               | logs <name> [--lines N] | run --name <n> --cwd <d> [--port <p>] -- <command>
//	               | rm <name> | env (--set NAME=VALUE)... | env --clear
//	               | version          [--json] [--state <dir>]
//	zcp-devsup version
//
// zcp and Mate's engine drive it with `ctl` over SSH. Exit codes: 0 done,
// 2 no supervisor, 3 unknown name, 4 invalid declaration, 1 anything else.
package main

import (
	"fmt"
	"io"
	"os"
)

// version is set at build time (-X main.version=vX.Y.Z).
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, usage)
		return 1
	}
	switch args[0] {
	case "run":
		return runSupervisor(args[1:], stdout, stderr)
	case "ctl":
		return runCtl(args[1:], stdout, stderr)
	case "version", "--version", "-v":
		fmt.Fprintln(stdout, version)
		return 0
	}
	fmt.Fprintln(stderr, usage)
	return 1
}

const usage = `usage:
  zcp-devsup run [--root <dir>] [--state <dir>] [--service <hostname>]
  zcp-devsup ctl <verb> [args] [--json] [--state <dir>]
    status [name] | start <name> | stop <name> | restart <name> | reload
    logs <name> [--lines N] | run --name <n> --cwd <d> [--port <p>] -- <command> | rm <name>
    env (--set NAME=VALUE)... | env --clear | version
  zcp-devsup version`
