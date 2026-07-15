package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/captureinspector"
)

const captureUIHelpFlag = "--help"

type captureUIOptions struct {
	SessionDir  string
	CaptureRoot string
	ListenAddr  string
	Active      bool
	NoOpen      bool
	Help        bool
}

func parseCaptureUIArgs(args []string) (captureUIOptions, error) {
	options := captureUIOptions{ListenAddr: "127.0.0.1:0"}
	rootExplicit := false
	value := func(index *int, flag string) (string, error) {
		if *index+1 >= len(args) {
			return "", fmt.Errorf("%s requires a value", flag)
		}
		*index++
		return args[*index], nil
	}
	for index := 0; index < len(args); index++ {
		arg := args[index]
		name, inline, hasInline := strings.Cut(arg, "=")
		if strings.HasPrefix(arg, "--") || arg == "-h" {
			switch name {
			case captureUIHelpFlag, "-h":
				if hasInline {
					return captureUIOptions{}, fmt.Errorf("%s does not accept a value", name)
				}
				options.Help = true
			case "--active":
				if hasInline {
					return captureUIOptions{}, errors.New("--active does not accept a value")
				}
				options.Active = true
			case "--no-open":
				if hasInline {
					return captureUIOptions{}, errors.New("--no-open does not accept a value")
				}
				options.NoOpen = true
			case "--root", "--listen":
				var parsed string
				var err error
				if hasInline {
					parsed = inline
				} else {
					parsed, err = value(&index, name)
					if err != nil {
						return captureUIOptions{}, err
					}
				}
				if name == "--root" {
					options.CaptureRoot = parsed
					rootExplicit = true
				} else {
					options.ListenAddr = parsed
				}
			default:
				return captureUIOptions{}, fmt.Errorf("unknown capture ui flag %s", name)
			}
			continue
		}
		if options.SessionDir != "" {
			return captureUIOptions{}, errors.New("capture ui accepts at most one session directory")
		}
		options.SessionDir = arg
	}
	if options.Active && options.SessionDir != "" {
		return captureUIOptions{}, errors.New("capture ui --active cannot be combined with a session directory")
	}
	if options.Active && rootExplicit {
		return captureUIOptions{}, errors.New("capture ui --active cannot be combined with --root")
	}
	if strings.TrimSpace(options.ListenAddr) == "" {
		return captureUIOptions{}, errors.New("capture ui listen address is empty")
	}
	return options, nil
}

func runCaptureUI(args []string) int {
	options, err := parseCaptureUIArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "capture ui: %v\n", err)
		return 2
	}
	if options.Help {
		fmt.Fprintln(os.Stdout, "Usage: zcp capture ui [<capture-directory>] [--root <capture-root>] [--active] [--listen <loopback:port>] [--no-open]")
		return 0
	}
	defaultRoot, err := defaultCaptureRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "capture ui: %v\n", err)
		return 1
	}
	if options.CaptureRoot == "" {
		options.CaptureRoot = defaultRoot
	}
	if options.Active {
		manager, err := newDefaultCaptureManager()
		if err != nil {
			fmt.Fprintf(os.Stderr, "capture ui: %v\n", err)
			return 1
		}
		connection, status, err := manager.ActiveConnection(context.Background())
		if err != nil {
			fmt.Fprintf(os.Stderr, "capture ui: active capture state %s: %v\n", status.State, err)
			return 1
		}
		if connection == nil {
			fmt.Fprintln(os.Stderr, "capture ui: no active capture window")
			return 1
		}
		options.SessionDir = connection.SessionDir
		options.CaptureRoot = filepath.Dir(connection.SessionDir)
		connection.Close()
	}
	if options.SessionDir != "" {
		absolute, err := filepath.Abs(options.SessionDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "capture ui: resolve session directory: %v\n", err)
			return 1
		}
		options.SessionDir = absolute
	}
	capability, err := newCaptureControlToken()
	if err != nil {
		fmt.Fprintf(os.Stderr, "capture ui: create capability: %v\n", err)
		return 1
	}
	reveal, err := newCaptureControlToken()
	if err != nil {
		fmt.Fprintf(os.Stderr, "capture ui: create reveal capability: %v\n", err)
		return 1
	}
	signalCtx, stop := signal.NotifyContext(context.Background(), captureShutdownSignals()...)
	defer stop()
	server, err := captureinspector.Start(signalCtx, captureinspector.Config{
		ListenAddr: options.ListenAddr, CaptureRoot: options.CaptureRoot, SessionDir: options.SessionDir,
		CapabilityToken: capability, RevealToken: reveal,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "capture ui: %v\n", err)
		return 1
	}
	fmt.Fprintln(os.Stderr, "CAPTURE UI — local read-only plaintext evidence viewer")
	fmt.Fprintf(os.Stderr, "capture root: %s\nurl: %s\n", options.CaptureRoot, server.LaunchURL())
	if options.SessionDir != "" {
		fmt.Fprintf(os.Stderr, "session: %s\n", options.SessionDir)
	}
	fmt.Fprintln(os.Stderr, "Authorization headers are absent, but bodies may contain prompts, source code, tool data, and secrets.")
	if !options.NoOpen {
		if err := openCaptureBrowser(signalCtx, server.LaunchURL()); err != nil {
			fmt.Fprintf(os.Stderr, "warning: open browser: %v\n", err)
		}
	}
	<-signalCtx.Done()
	closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Close(closeCtx); err != nil {
		fmt.Fprintf(os.Stderr, "capture ui: %v\n", err)
		return 1
	}
	return 0
}

func openCaptureBrowser(ctx context.Context, target string) error {
	var command string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		command, args = "open", []string{target}
	case "linux":
		command, args = "xdg-open", []string{target}
	case "windows":
		command, args = "rundll32", []string{"url.dll,FileProtocolHandler", target}
	default:
		return fmt.Errorf("unsupported platform %s; open %s manually", runtime.GOOS, target)
	}
	cmd := exec.CommandContext(ctx, command, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
