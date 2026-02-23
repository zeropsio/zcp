package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/zeropsio/zcp/internal/auth"
	zcpinit "github.com/zeropsio/zcp/internal/init"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/server"
	"github.com/zeropsio/zcp/internal/update"
)

func main() {
	// Subcommand dispatch.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "init":
			if err := zcpinit.Run("."); err != nil {
				log.Fatalf("init: %v", err)
			}
			return
		case "version":
			printVersion()
			return
		case "update":
			runUpdate()
			return
		}
	}

	// MCP server mode — starts immediately, no blocking update check.
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func printVersion() {
	fmt.Fprintf(os.Stdout, "zcp %s (%s, %s)\n", server.Version, server.Commit, server.Built)
}

func runUpdate() {
	ctx := context.Background()

	fmt.Fprintln(os.Stderr, "Checking for updates...")
	checker := update.NewChecker(server.Version)
	checker.CacheTTL = 0 // force fresh check
	info := checker.Check(ctx)

	if !info.Available {
		fmt.Fprintf(os.Stderr, "Already up to date (%s).\n", server.Version)
		return
	}

	fmt.Fprintf(os.Stderr, "Update available: %s → %s\n", info.CurrentVersion, info.LatestVersion)
	fmt.Fprintln(os.Stderr, "Downloading...")

	binary, err := os.Executable()
	if err != nil {
		log.Fatalf("resolve executable: %v", err)
	}

	if err := update.Apply(ctx, info, binary, nil); err != nil {
		log.Fatalf("update: %v", err)
	}

	fmt.Fprintln(os.Stderr, "Updated successfully. Restart ZCP to use the new version.")
}

func run() error {
	// Bootstrap: resolve credentials (env var or zcli) to create platform client.
	creds, err := auth.ResolveCredentials()
	if err != nil {
		return fmt.Errorf("auth: %w", err)
	}

	client, err := platform.NewZeropsClient(creds.Token, creds.APIHost)
	if err != nil {
		return fmt.Errorf("create platform client: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Full auth: validate token via API and discover project.
	authInfo, err := auth.Resolve(ctx, client)
	if err != nil {
		return fmt.Errorf("auth: %w", err)
	}

	// Log fetcher for zerops_logs tool.
	logFetcher := platform.NewLogFetcher()

	// Knowledge store for zerops_knowledge tool.
	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		return fmt.Errorf("knowledge store: %w", err)
	}

	// Detect runtime environment (Zerops container vs local dev).
	rtInfo := runtime.Detect()

	// Mounter requires SSHFS — only available inside Zerops containers.
	var mounter ops.Mounter
	if rtInfo.InContainer {
		mounter = platform.NewSystemMounter()
	}

	// Local deployer for zcli push (zerops_deploy tool) — works from anywhere.
	localDeployer := platform.NewSystemLocalDeployer()

	// SSH deployer for cross-service deploys — only available inside Zerops containers.
	var sshDeployer ops.SSHDeployer
	if rtInfo.InContainer {
		sshDeployer = platform.NewSystemSSHDeployer()
	}

	// Idle tracker for graceful update restart.
	idleWaiter := update.NewIdleWaiter()

	// Create and run MCP server on STDIO.
	srv := server.New(ctx, client, authInfo, store, logFetcher, sshDeployer, localDeployer, mounter, idleWaiter, rtInfo)

	// Silent background update — completely invisible to LLM.
	// Checks GitHub (24h cache), downloads if newer, waits for idle, then exits.
	// Claude Code auto-restarts the MCP server with the new binary.
	if os.Getenv("ZCP_AUTO_UPDATE") != "0" {
		go update.Once(ctx, server.Version, idleWaiter, stop, os.Stderr)
	}

	if err := srv.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("server: %w", err)
	}

	return nil
}
