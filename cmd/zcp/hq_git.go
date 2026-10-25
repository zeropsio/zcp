package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/mate"
)

// isHQGit recognizes the Git passthrough before CLI telemetry can add output
// to Git's streams or delay its exit.
func isHQGit(args []string) bool {
	return len(args) >= 2 && args[0] == "hq" && args[1] == "git"
}

func runHQGit(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return hq.RunGit(ctx, hq.IdentityFrom(mate.LiveLookup(mate.LiveEnvStorePath)), args, os.Stdin, os.Stdout, os.Stderr)
}
