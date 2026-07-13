package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/zeropsio/zcp/internal/capture"
	"github.com/zeropsio/zcp/internal/server"
)

const captureUpstreamEnv = "ZCP_CAPTURE_UPSTREAM_BASE_URL"

func runCapture(args []string) int {
	if len(args) == 0 || isCaptureHelp(args[0]) {
		printCaptureUsage()
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	switch args[0] {
	case "on":
		return runCaptureOn(args[1:])
	case "off":
		return runCaptureOff(args[1:])
	case "status":
		return runCaptureStatus(args[1:])
	case "raw":
		return runCaptureRaw(args[1:])
	case "inspect":
		return runCaptureInspectTo(args[1:], os.Stdout, os.Stderr)
	case "daemon":
		return runCaptureDaemon(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown capture subcommand: %s\n\n", args[0])
		printCaptureUsage()
		return 2
	}
}

func isCaptureHelp(arg string) bool {
	return arg == "help" || arg == "--help" || arg == "-h"
}

func printCaptureUsage() {
	fmt.Fprintln(os.Stderr, `Usage:
  zcp capture on [--label <name>] [--upstream <url>] [--listen <host:port>]
  zcp capture off
  zcp capture status
  zcp capture raw [flags] -- <command> [args...]
  zcp capture inspect <session-dir> [--view summary|timeline|context|all]
                      [--eval <id>] [--scenario <id>] [--invocation <id>]
                      [--format text|json]

Lifecycle:
  on      Start a private capture daemon, verify it, then configure Claude
  off     Restore Claude configuration, finalize capture, and stop the daemon
  status  Reconcile configuration, daemon process, and listener state

Raw flags:
  --label <name>         Human label for the developer capture
  --output-dir <path>    Capture root (default: ~/.local/state/zcp/captures)
  --listen <host:port>   Loopback listener (default: 127.0.0.1:0)
  --upstream <url>       Fixed provider origin (default: current Anthropic base or api.anthropic.com)

The capture is plaintext and may contain prompts, source code, tool inputs, and
tool results. Provider authorization/cookie header values are not recorded.`)
}

func runCaptureOn(args []string) int {
	flags := flag.NewFlagSet("capture on", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	label := flags.String("label", "capture-window", "capture window label")
	upstream := flags.String("upstream", "", "fixed provider upstream")
	listen := flags.String("listen", "", "loopback provider listener")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if len(flags.Args()) != 0 {
		fmt.Fprintln(os.Stderr, "capture on: unexpected positional arguments")
		return 2
	}
	manager, err := newDefaultCaptureManager()
	if err != nil {
		fmt.Fprintf(os.Stderr, "capture on: %v\n", err)
		return 1
	}
	status, err := manager.On(context.Background(), capture.ManagerOnOptions{Label: *label, UpstreamURL: *upstream, ListenAddr: *listen})
	if err != nil {
		fmt.Fprintf(os.Stderr, "capture on: %v\n", err)
		renderCaptureManagerStatus(os.Stderr, status)
		return 1
	}
	fmt.Fprintln(os.Stderr, "CAPTURE ON — plaintext prompts, source code, and tool data")
	renderCaptureManagerStatus(os.Stderr, status)
	fmt.Fprintln(os.Stderr, "New Claude sessions now use the local capture proxy.")
	return 0
}

func runCaptureOff(args []string) int {
	if len(args) != 0 {
		fmt.Fprintln(os.Stderr, "capture off: no arguments accepted")
		return 2
	}
	manager, err := newDefaultCaptureManager()
	if err != nil {
		fmt.Fprintf(os.Stderr, "capture off: %v\n", err)
		return 1
	}
	status, err := manager.Off(context.Background())
	for _, warning := range status.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", warning)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "capture off: %v\n", err)
		renderCaptureManagerStatus(os.Stderr, status)
		return 1
	}
	fmt.Fprintln(os.Stderr, "CAPTURE OFF — Claude configuration restored; capture proxy stopped")
	return 0
}

func runCaptureStatus(args []string) int {
	if len(args) != 0 {
		fmt.Fprintln(os.Stderr, "capture status: no arguments accepted")
		return 2
	}
	manager, err := newDefaultCaptureManager()
	if err != nil {
		fmt.Fprintf(os.Stderr, "capture status: %v\n", err)
		return 1
	}
	status, statusErr := manager.Status(context.Background())
	renderCaptureManagerStatus(os.Stdout, status)
	if statusErr != nil {
		return 1
	}
	return 0
}

func renderCaptureManagerStatus(writer io.Writer, status capture.ManagerStatus) {
	fmt.Fprintf(writer, "State: %s\n", status.State)
	if status.CaptureID != "" {
		fmt.Fprintf(writer, "Capture: %s\nProxy: %s\nRecords: %s\nPID: %d\n", status.CaptureID, status.ProxyURL, status.SessionDir, status.ProcessID)
	}
	for _, problem := range status.Problems {
		fmt.Fprintf(writer, "Problem: %s\n", problem)
	}
}

type captureInspectOptions struct {
	SessionDir string
	View       string
	Format     string
	Filter     capture.InspectionFilter
}

func parseCaptureInspectArgs(args []string) (captureInspectOptions, error) {
	options := captureInspectOptions{View: "all", Format: "text"}
	readValue := func(index *int, name string) (string, error) {
		if *index+1 >= len(args) {
			return "", fmt.Errorf("%s requires a value", name)
		}
		*index++
		return args[*index], nil
	}
	for index := 0; index < len(args); index++ {
		arg := args[index]
		name, inline, hasInline := strings.Cut(arg, "=")
		if strings.HasPrefix(arg, "--") {
			var value string
			var err error
			if hasInline {
				value = inline
			} else {
				value, err = readValue(&index, name)
				if err != nil {
					return captureInspectOptions{}, err
				}
			}
			switch name {
			case "--view":
				options.View = value
			case "--format":
				options.Format = value
			case "--eval":
				options.Filter.EvalRunID = value
			case "--scenario":
				options.Filter.ScenarioRunID = value
			case "--invocation":
				options.Filter.InvocationID = value
			default:
				return captureInspectOptions{}, fmt.Errorf("unknown capture inspect flag %s", name)
			}
			continue
		}
		if options.SessionDir != "" {
			return captureInspectOptions{}, errors.New("exactly one session directory is required")
		}
		options.SessionDir = arg
	}
	if options.SessionDir == "" {
		return captureInspectOptions{}, errors.New("exactly one session directory is required")
	}
	switch options.View {
	case "all", "summary", "timeline", "context":
	default:
		return captureInspectOptions{}, fmt.Errorf("unknown inspection view %q", options.View)
	}
	if options.Format != "text" && options.Format != "json" {
		return captureInspectOptions{}, fmt.Errorf("unknown inspection format %q", options.Format)
	}
	return options, nil
}

func runCaptureInspectTo(args []string, stdout, stderr io.Writer) int {
	options, err := parseCaptureInspectArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "capture inspect: %v\n", err)
		return 2
	}
	report, err := capture.InspectSession(options.SessionDir)
	if err != nil {
		fmt.Fprintf(stderr, "capture inspect: %v\n", err)
		return 1
	}
	report, err = capture.FilterInspection(report, options.Filter)
	if err != nil {
		fmt.Fprintf(stderr, "capture inspect: %v\n", err)
		return 1
	}
	if options.Format == "json" {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			fmt.Fprintf(stderr, "capture inspect: encode JSON: %v\n", err)
			return 1
		}
		return 0
	}
	var renderErr error
	switch options.View {
	case "summary":
		renderErr = capture.RenderInspectionSummary(stdout, report)
	case "timeline":
		renderErr = capture.RenderTimelineInspection(stdout, report)
	case "context":
		renderErr = capture.RenderContextInspection(stdout, report)
	default:
		renderErr = capture.RenderInspection(stdout, report)
	}
	if renderErr != nil {
		fmt.Fprintf(stderr, "capture inspect: %v\n", renderErr)
		return 1
	}
	return 0
}

func runCaptureRaw(args []string) int {
	defaultRoot, err := defaultCaptureRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "capture: %v\n", err)
		return 1
	}
	flags := flag.NewFlagSet("capture raw", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	label := flags.String("label", "capture", "capture label")
	outputDir := flags.String("output-dir", defaultRoot, "capture root directory")
	listen := flags.String("listen", "127.0.0.1:0", "loopback listen address")
	upstream := flags.String("upstream", captureUpstreamFromEnv(), "fixed provider upstream")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	command := flags.Args()
	if len(command) == 0 {
		fmt.Fprintln(os.Stderr, "capture: command required after --")
		return 2
	}

	sessionID, err := newCaptureSessionID()
	if err != nil {
		fmt.Fprintf(os.Stderr, "capture: create session ID: %v\n", err)
		return 1
	}
	controlDir, err := os.MkdirTemp(os.TempDir(), "zcp-capture-control-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "capture: create control directory: %v\n", err)
		return 1
	}
	defer func() { _ = os.RemoveAll(controlDir) }()
	if err := os.Chmod(controlDir, 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "capture: secure control directory: %v\n", err)
		return 1
	}
	controlToken, err := newCaptureControlToken()
	if err != nil {
		fmt.Fprintf(os.Stderr, "capture: create control token: %v\n", err)
		return 1
	}
	runtime, err := capture.StartRuntime(context.Background(), capture.RuntimeConfig{
		RootDir:       *outputDir,
		CaptureID:     sessionID,
		Label:         *label,
		ListenAddr:    *listen,
		UpstreamURL:   *upstream,
		ControlSocket: filepath.Join(controlDir, "control.sock"),
		ControlToken:  controlToken,
		Command:       command,
		Build: capture.CaptureBuildInfo{
			Version: server.Version,
			Commit:  server.Commit,
			Built:   server.Built,
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "capture: start runtime: %v\n", err)
		return 1
	}

	fmt.Fprintln(os.Stderr, "CAPTURE ON — plaintext prompts, source code, and tool data")
	fmt.Fprintf(os.Stderr, "session: %s\n", sessionID)
	fmt.Fprintf(os.Stderr, "provider proxy: %s\n", runtime.ProxyURL())
	fmt.Fprintf(os.Stderr, "records: %s\n", filepath.Join(runtime.SessionDir(), "provider.jsonl"))
	fmt.Fprintf(os.Stderr, "manifest: %s\n", filepath.Join(runtime.SessionDir(), "manifest.json"))

	childEnv := captureChildEnv(os.Environ(), runtime.ProxyURL(), sessionID, runtime.SessionDir(), runtime.ControlSocket(), controlToken)
	exitCode, childErr := runCaptureChild(command, childEnv)
	requestedStatus := capture.CaptureComplete
	if childErr != nil {
		requestedStatus = capture.CapturePartial
	}
	status, closeErr := runtime.CloseChild(requestedStatus, exitCode)
	if childErr != nil {
		fmt.Fprintf(os.Stderr, "capture: child process: %v\n", childErr)
	}
	if closeErr != nil {
		fmt.Fprintf(os.Stderr, "capture: close runtime: %v\n", closeErr)
	}
	fmt.Fprintf(os.Stderr, "child: exit %d\ncapture: %s\n", exitCode, status)

	if childErr != nil {
		return 1
	}
	return exitCode
}

func defaultCaptureRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".local", "state", "zcp", "captures"), nil
}

func captureUpstreamFromEnv() string {
	if value := strings.TrimSpace(os.Getenv(captureUpstreamEnv)); value != "" {
		return value
	}
	if value := strings.TrimSpace(os.Getenv("ANTHROPIC_BASE_URL")); value != "" && !isLoopbackURL(value) {
		return value
	}
	return capture.DefaultUpstreamBaseURL
}

func captureManifestProviderOrigin(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse provider origin for manifest: %w", err)
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	return parsed.String(), nil
}

func isLoopbackURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := parsed.Hostname()
	return strings.EqualFold(host, "localhost") || strings.HasPrefix(host, "127.") || host == "::1"
}

func newCaptureSessionID() (string, error) {
	var random [6]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("read crypto randomness: %w", err)
	}
	return "capture-" + time.Now().UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(random[:]), nil
}

func captureChildEnv(environment []string, proxyURL, sessionID, sessionDir, controlSocket, controlToken string) []string {
	replaced := map[string]bool{
		"ANTHROPIC_BASE_URL":     true,
		capture.EnvSessionID:     true,
		capture.EnvSessionDir:    true,
		capture.EnvControlSocket: true,
		capture.EnvControlToken:  true,
	}
	out := make([]string, 0, len(environment)+5)
	for _, entry := range environment {
		key, _, found := strings.Cut(entry, "=")
		if found && replaced[key] {
			continue
		}
		out = append(out, entry)
	}
	out = append(out,
		"ANTHROPIC_BASE_URL="+proxyURL,
		capture.EnvSessionID+"="+sessionID,
		capture.EnvSessionDir+"="+sessionDir,
		capture.EnvControlSocket+"="+controlSocket,
		capture.EnvControlToken+"="+controlToken,
	)
	return out
}

func newCaptureControlToken() (string, error) {
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("read crypto randomness: %w", err)
	}
	return hex.EncodeToString(random[:]), nil
}

func runCaptureChild(command, environment []string) (int, error) {
	cmd := exec.Command(command[0], command[1:]...) //nolint:gosec // explicit developer-selected wrapped command
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = environment
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return 1, fmt.Errorf("start %q: %w", command[0], err)
	}

	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()

	for {
		select {
		case received := <-signals:
			sig, ok := received.(syscall.Signal)
			if !ok {
				continue
			}
			if err := syscall.Kill(-cmd.Process.Pid, sig); err != nil && !errors.Is(err, syscall.ESRCH) {
				fmt.Fprintf(os.Stderr, "capture: forward signal %s: %v\n", received, err)
			}
		case waitErr := <-waited:
			if waitErr == nil {
				return 0, nil
			}
			var exitErr *exec.ExitError
			if errors.As(waitErr, &exitErr) {
				if waitStatus, ok := exitErr.ProcessState.Sys().(syscall.WaitStatus); ok && waitStatus.Signaled() {
					return 128 + int(waitStatus.Signal()), nil
				}
				return exitErr.ExitCode(), nil
			}
			return 1, fmt.Errorf("wait for %q: %w", command[0], waitErr)
		}
	}
}
