package update

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// OnceOpts configures the Once update function.
// BinaryPath and CacheDir override defaults for testing.
type OnceOpts struct {
	CurrentVersion string
	Waiter         *IdleWaiter
	Shutdown       func()
	LogOutput      io.Writer
	BinaryPath     string // empty = use os.Executable()
	CacheDir       string // empty = use default cache dir
}

// Once checks for an update, applies it, waits for idle, then triggers shutdown.
// Completely transparent — no MCP tool, no notification to LLM.
// Skips "dev" builds. Errors logged to logOutput, never blocks the caller
// beyond the idle wait.
func Once(ctx context.Context, currentVersion string, waiter *IdleWaiter, shutdown func(), logOutput io.Writer) {
	OnceWithOpts(ctx, OnceOpts{
		CurrentVersion: currentVersion,
		Waiter:         waiter,
		Shutdown:       shutdown,
		LogOutput:      logOutput,
	})
}

// OnceWithOpts is the configurable version of Once (used by tests).
func OnceWithOpts(ctx context.Context, opts OnceOpts) {
	if opts.CurrentVersion == "dev" {
		return
	}

	checker := NewChecker(opts.CurrentVersion)
	if opts.CacheDir != "" {
		checker.CacheDir = opts.CacheDir
	}
	info := checker.Check(ctx)
	if !info.Available {
		return
	}

	binary := opts.BinaryPath
	if binary == "" {
		var err error
		binary, err = os.Executable()
		if err != nil {
			fmt.Fprintf(opts.LogOutput, "zcp: auto-update: resolve executable: %v\n", err)
			return
		}
	}

	if !CanWrite(filepath.Dir(binary)) {
		return
	}

	if err := Apply(ctx, info, binary, nil); err != nil {
		fmt.Fprintf(opts.LogOutput, "zcp: auto-update: %v\n", err)
		return
	}

	fmt.Fprintf(opts.LogOutput, "zcp: updated %s → %s, waiting for idle to restart...\n",
		info.CurrentVersion, info.LatestVersion)

	if err := opts.Waiter.WaitForIdle(ctx); err != nil {
		return
	}
	opts.Shutdown()
}
