package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/runtime"
)

// isHQGit recognizes the Git passthrough before CLI telemetry can add output
// to Git's streams or delay its exit.
func isHQGit(args []string) bool {
	return len(args) >= 2 && args[0] == "hq" && args[1] == "git"
}

func runHQGit(args []string) int {
	lookup := mate.LiveLookup(mate.LiveEnvStorePath)
	if runtime.EnvEnabled(lookup("ZCP_MATE_ENABLED")) {
		fmt.Fprintln(os.Stderr, "zcp hq git: this project has a Mate; use plain git with the Mate's own Git credential from its checkout")
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return hq.RunGit(ctx, hq.IdentityFrom(lookup), args, os.Stdin, os.Stdout, os.Stderr)
}
