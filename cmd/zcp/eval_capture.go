package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zeropsio/zcp/internal/capture"
)

const evalCaptureModeRaw = "raw"

// evalCaptureOwnerEnv carries the capture session ID of a scoped window this
// invocation's own `--capture raw` created, from the wrapper to its child
// `zcp eval` process. initEvalRunner compares it against the attached
// connection's CaptureID to decide RunnerConfig.CaptureOwned — the only
// signal that satisfies verification.mode: required
// (docs/spec-capture-inspector.md §6 "Owned window for required results").
// A global or inherited window never sets this variable in the child's
// environment, so it never satisfies required mode.
const evalCaptureOwnerEnv = "ZCP_EVAL_CAPTURE_OWNER"

func parseEvalCaptureArgs(args []string) (clean []string, requested bool, err error) {
	clean = make([]string, 0, len(args))
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--capture" {
			if index+1 >= len(args) {
				return nil, false, errors.New("--capture requires mode raw")
			}
			mode := args[index+1]
			if mode != evalCaptureModeRaw {
				return nil, false, fmt.Errorf("unsupported eval capture mode %q; only raw is available", mode)
			}
			requested = true
			index++
			continue
		}
		if mode, found := strings.CutPrefix(arg, "--capture="); found {
			if mode != evalCaptureModeRaw {
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
		return true, runEval(clean)
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
		return true, runEval(clean)
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
	return true, runEvalScopedCaptureRaw(wrapperArgs)
}

// runEvalScopedCaptureRaw creates the private scoped capture window this
// eval invocation owns, hands its identity to the child via
// evalCaptureOwnerEnv, and decides the final exit from the typed result
// (docs/spec-capture-inspector.md §6): the child's own exit is provisional
// until the window closed complete and its manifest validates.
func runEvalScopedCaptureRaw(wrapperArgs []string) int {
	flags, exitCode, done := parseCaptureRawFlags(wrapperArgs)
	if done {
		return exitCode
	}
	result, err := runCaptureRawWork(flags, func(sessionID string) []string {
		return []string{evalCaptureOwnerEnv + "=" + sessionID}
	})
	if err != nil {
		return 1
	}
	windowID := filepath.Base(result.SessionDir)
	if result.ChildErr != nil {
		fmt.Fprintf(os.Stderr, "capture: child process: %v\n", result.ChildErr)
		if result.CloseErr != nil || result.Status != capture.CaptureComplete {
			fmt.Fprintf(os.Stderr, "capture: window %s closed %s: %v\n", windowID, result.Status, result.CloseErr)
		}
		return 1
	}
	if result.CloseErr != nil || result.Status != capture.CaptureComplete {
		fmt.Fprintf(os.Stderr, "capture: window %s closed %s: %v\n", windowID, result.Status, result.CloseErr)
		return 1
	}
	report, err := capture.InspectSession(result.SessionDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "capture: window %s inspection failed: %v\n", windowID, err)
		return 1
	}
	if !report.Integrity.Valid || !report.Integrity.Complete {
		fmt.Fprintf(os.Stderr, "capture: window %s manifest invalid or incomplete\n", windowID)
		return 1
	}
	fmt.Fprintln(os.Stderr, "capture: complete")
	return result.ChildExit
}
