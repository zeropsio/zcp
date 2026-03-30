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
	LogOutput      io.Writer
	BinaryPath     string // empty = use os.Executable()
	CacheDir       string // empty = use default cache dir
}

// Once checks for an update and applies it (replaces the binary on disk).
// The new version takes effect on next natural server restart — the running
// process is NOT shut down. Completely transparent — no MCP tool, no
// notification to LLM. Skips "dev" builds. Errors logged to logOutput.
func Once(ctx context.Context, currentVersion string, logOutput io.Writer) {
	OnceWithOpts(ctx, OnceOpts{
		CurrentVersion: currentVersion,
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

	// Follow symlinks — update the real binary, not the symlink itself.
	// This ensures ~/.local/bin/zcp (symlink) → /usr/local/bin/zcp (real)
	// gets the real binary updated, avoiding stale-copy divergence.
	if resolved, err := filepath.EvalSymlinks(binary); err == nil {
		binary = resolved
	}

	if !CanWrite(filepath.Dir(binary)) {
		return
	}

	if err := Apply(ctx, info, binary, nil); err != nil {
		fmt.Fprintf(opts.LogOutput, "zcp: auto-update: %v\n", err)
		return
	}

	fmt.Fprintf(opts.LogOutput, "zcp: updated %s → %s (active on next restart)\n",
		info.CurrentVersion, info.LatestVersion)
}
