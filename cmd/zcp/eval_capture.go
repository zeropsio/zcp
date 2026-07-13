package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/zeropsio/zcp/internal/capture"
)

func parseEvalCaptureArgs(args []string) (clean []string, requested bool, err error) {
	clean = make([]string, 0, len(args))
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--capture" {
			if index+1 >= len(args) {
				return nil, false, errors.New("--capture requires mode raw")
			}
			mode := args[index+1]
			if mode != "raw" {
				return nil, false, fmt.Errorf("unsupported eval capture mode %q; only raw is available", mode)
			}
			requested = true
			index++
			continue
		}
		if mode, found := strings.CutPrefix(arg, "--capture="); found {
			if mode != "raw" {
				return nil, false, fmt.Errorf("unsupported eval capture mode %q; only raw is available", mode)
			}
			requested = true
			continue
		}
		clean = append(clean, arg)
	}
	return clean, requested, nil
}

// runEvalWithOptionalScopedCapture intercepts only explicit --capture raw.
// Global capture remains automatic inside initEvalRunner even without the flag.
func runEvalWithOptionalScopedCapture(args []string) (handled bool, exitCode int) {
	clean, requested, err := parseEvalCaptureArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "eval capture: %v\n", err)
		return true, 2
	}
	if !requested {
		return false, 0
	}
	ctx := context.Background()
	connection, configured, err := capture.ConnectionFromEnvironment(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "eval capture: %v\n", err)
		return true, 1
	}
	if configured {
		connection.Close()
		runEval(clean)
		return true, 0
	}
	manager, err := newDefaultCaptureManager()
	if err != nil {
		fmt.Fprintf(os.Stderr, "eval capture: %v\n", err)
		return true, 1
	}
	connection, status, err := manager.ActiveConnection(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "eval capture: global state %s: %v\n", status.State, err)
		return true, 1
	}
	if connection != nil {
		connection.Close()
		runEval(clean)
		return true, 0
	}

	executable, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "eval capture: resolve executable: %v\n", err)
		return true, 1
	}
	label := "eval"
	if id := flagValue(clean, "--id"); id != "" {
		label = "eval-" + id
	}
	command := make([]string, 0, len(clean)+2)
	command = append(command, executable, "eval")
	command = append(command, clean...)
	wrapperArgs := make([]string, 0, 3+len(command))
	wrapperArgs = append(wrapperArgs, "--label", label, "--")
	wrapperArgs = append(wrapperArgs, command...)
	return true, runCaptureRaw(wrapperArgs)
}
