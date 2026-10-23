//go:build linux || darwin

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/zeropsio/zcp/internal/devsup"
)

// ctlTimeout bounds one control call: a restart may wait for a stop's grace
// and a port.
const ctlTimeout = 60 * time.Second

func runSupervisor(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("zcp-devsup run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", "", "repo root holding .zcp/dev-servers.json (default: the working directory)")
	state := fs.String("state", "", "state directory (default: $HOME/.zcp/devsup)")
	service := fs.String("service", os.Getenv("hostname"), "this dev service's hostname (variant B)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *root == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(stderr, "zcp-devsup:", err)
			return 1
		}
		*root = wd
	}
	dir, err := stateDir(*state)
	if err != nil {
		fmt.Fprintln(stderr, "zcp-devsup:", err)
		return 1
	}
	sup, err := devsup.New(devsup.Config{
		Root: *root, StateDir: dir, Service: *service, Env: os.Environ(), Version: version, Log: stdout,
	})
	if err != nil {
		fmt.Fprintln(stderr, "zcp-devsup:", err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	if err := sup.Run(ctx); err != nil {
		fmt.Fprintln(stderr, "zcp-devsup:", err)
		return 1
	}
	return 0
}

func stateDir(flagValue string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	d, err := devsup.DefaultStateDir()
	if err != nil {
		return "", fmt.Errorf("state dir: %w", err)
	}
	return d, nil
}

// ctlArgs is a parsed `ctl` command line: flags may sit anywhere before `--`.
type ctlArgs struct {
	verb, name string
	json       bool
	state      string
	lines      int
	cwd        string
	port       int
	command    string
}

func parseCtl(args []string) (ctlArgs, error) {
	a := ctlArgs{lines: 50}
	var pos []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		value := func() (string, error) {
			if i+1 >= len(args) {
				return "", fmt.Errorf("%s needs a value", arg)
			}
			i++
			return args[i], nil
		}
		var err error
		switch arg {
		case "--":
			a.command = strings.Join(args[i+1:], " ")
			i = len(args)
		case "--json":
			a.json = true
		case "--state":
			a.state, err = value()
		case "--name":
			a.name, err = value()
		case "--cwd":
			a.cwd, err = value()
		case "--lines", "--port":
			var v string
			if v, err = value(); err == nil {
				n, cerr := strconv.Atoi(v)
				if cerr != nil || n < 0 {
					return a, fmt.Errorf("%s: %q is not a number", arg, v)
				}
				if arg == "--lines" {
					a.lines = n
				} else {
					a.port = n
				}
			}
		default:
			if strings.HasPrefix(arg, "--") {
				return a, fmt.Errorf("unknown flag %s", arg)
			}
			pos = append(pos, arg)
		}
		if err != nil {
			return a, err
		}
	}
	if len(pos) == 0 {
		return a, errors.New("a verb is required")
	}
	a.verb = pos[0]
	if len(pos) > 1 {
		if a.name != "" || len(pos) > 2 {
			return a, errors.New("too many arguments")
		}
		a.name = pos[1]
	}
	return a, nil
}

func runCtl(args []string, stdout, stderr io.Writer) int {
	a, err := parseCtl(args)
	if err != nil {
		fmt.Fprintln(stderr, "zcp-devsup ctl:", err)
		fmt.Fprintln(stderr, usage)
		return devsup.CodeFailed
	}
	dir, err := stateDir(a.state)
	if err != nil {
		return fail(a, stdout, stderr, devsup.CodeFailed, err)
	}
	switch a.verb {
	case "version":
		return emit(a, stdout, map[string]any{"version": version, "formats": devsup.Formats}, version)
	case "logs":
		return ctlLogs(a, dir, stdout, stderr)
	}
	req := devsup.Request{Verb: a.verb, Name: a.name}
	switch a.verb {
	case devsup.VerbStatus, devsup.VerbReload:
	case devsup.VerbStart, devsup.VerbStop, devsup.VerbRestart, devsup.VerbRm:
		if a.name == "" {
			return fail(a, stdout, stderr, devsup.CodeFailed, fmt.Errorf("%s needs a server name", a.verb))
		}
	case devsup.VerbRun:
		if a.name == "" || a.command == "" {
			return fail(a, stdout, stderr, devsup.CodeFailed, errors.New("run needs --name and a command after --"))
		}
		req.Name = ""
		req.Server = &devsup.Server{Name: a.name, Cwd: a.cwd, Port: a.port, Command: a.command}
	default:
		return fail(a, stdout, stderr, devsup.CodeFailed, fmt.Errorf("unknown verb %q", a.verb))
	}
	resp, err := devsup.Control(dir, req, ctlTimeout)
	if err != nil {
		return fail(a, stdout, stderr, resp.Code, err)
	}
	if resp.Code != devsup.CodeOK {
		return fail(a, stdout, stderr, resp.Code, errors.New(resp.Error))
	}
	return emit(a, stdout, resp.State, statusText(resp.State))
}

func ctlLogs(a ctlArgs, dir string, stdout, stderr io.Writer) int {
	if a.name == "" {
		return fail(a, stdout, stderr, devsup.CodeFailed, errors.New("logs needs a server name"))
	}
	lines, err := devsup.LogTail(dir, a.name, a.lines)
	if err != nil {
		return fail(a, stdout, stderr, devsup.CodeUnknownName, err)
	}
	return emit(a, stdout, map[string]any{"name": a.name, "lines": lines}, strings.Join(lines, "\n"))
}

func emit(a ctlArgs, stdout io.Writer, v any, text string) int {
	if a.json {
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return devsup.CodeFailed
		}
		fmt.Fprintln(stdout, string(b))
		return devsup.CodeOK
	}
	if text != "" {
		fmt.Fprintln(stdout, text)
	}
	return devsup.CodeOK
}

func fail(a ctlArgs, stdout, stderr io.Writer, code int, err error) int {
	if code == devsup.CodeOK {
		code = devsup.CodeFailed
	}
	if a.json {
		b, merr := json.Marshal(struct {
			Code  int    `json:"code"`
			Error string `json:"error"`
		}{code, err.Error()})
		if merr == nil {
			fmt.Fprintln(stdout, string(b))
		}
	} else {
		fmt.Fprintln(stderr, "zcp-devsup ctl:", err)
	}
	return code
}

// statusText is the human form of a state: one line per server.
func statusText(st devsup.State) string {
	names := make([]string, 0, len(st.Servers))
	for n := range st.Servers {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	fmt.Fprintf(&b, "zcp-devsup %s pid %d", st.Supervisor.Version, st.Supervisor.Pid)
	if st.Declaration.Error != "" {
		fmt.Fprintf(&b, "\ndeclaration: %s", st.Declaration.Error)
	}
	for _, n := range names {
		sv := st.Servers[n]
		fmt.Fprintf(&b, "\n%-20s %-9s pid %-7d restarts %d", n, sv.Status, sv.Pid, sv.Restarts)
		if sv.PreviewPort != 0 {
			fmt.Fprintf(&b, "  preview :%d", sv.PreviewPort)
		}
		if sv.Exit != nil {
			fmt.Fprintf(&b, "  exit %d %s at %s", sv.Exit.Code, sv.Exit.Signal, sv.Exit.At.Format(time.RFC3339))
		}
	}
	return b.String()
}
