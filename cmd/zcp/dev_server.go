package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/tools"
)

// devServerKeepInterval is how often the keeper looks: a restarted dev
// container is serving again within this much of coming back up.
const devServerKeepInterval = 20 * time.Second

// runDevServerCmd implements `zcp dev-server keep --state-dir <dir>
// [--interval <duration>]` — the dev-server keeper zerops_dev_server registers
// as a unit on the zcp container (docs/spec-workflows.md §8 O4). It brings
// back every dev server zcp keeps whose container restarted or was redeployed,
// until it is stopped.
func runDevServerCmd(args []string) int {
	if len(args) == 0 || args[0] != "keep" {
		log.Print("usage: zcp dev-server keep --state-dir <dir> [--interval 20s]")
		return 1
	}
	fs := flag.NewFlagSet("zcp dev-server keep", flag.ContinueOnError)
	stateDir := fs.String("state-dir", "", "the .zcp/state directory whose kept dev servers to keep")
	interval := fs.Duration("interval", devServerKeepInterval, "how often to look")
	if err := fs.Parse(args[1:]); err != nil {
		return 1
	}
	if *stateDir == "" || *interval <= 0 {
		log.Print("usage: zcp dev-server keep --state-dir <dir> [--interval 20s]")
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	tools.RunDevServerKeeper(ctx, platform.NewSystemSSHDeployer(), *stateDir, *interval, os.Stderr)
	return 0
}
