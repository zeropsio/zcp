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

	// Auto-update check (before MCP server starts — stdout is still free).
	updateInfo := checkAndApplyUpdate()

	// MCP server mode.
	if err := run(updateInfo); err != nil {
		log.Fatal(err)
	}
}

func printVersion() {
	fmt.Fprintf(os.Stdout, "zcp %s (%s, %s)\n", server.Version, server.Commit, server.Built)
}

func runUpdate() {
	fmt.Fprintln(os.Stderr, "Checking for updates...")
	checker := update.NewChecker(server.Version)
	checker.CacheTTL = 0 // force fresh check
	info := checker.Check()

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

	if err := update.Apply(info, binary, nil); err != nil {
		log.Fatalf("update: %v", err)
	}

	fmt.Fprintln(os.Stderr, "Updated successfully. Restart ZCP to use the new version.")
}

func checkAndApplyUpdate() *update.Info {
	checker := update.NewChecker(server.Version)
	info := checker.Check()

	if !info.Available || os.Getenv("ZCP_AUTO_UPDATE") == "0" {
		return info
	}

	// Auto-update: download, replace, re-exec.
	binary, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "zcp: auto-update: resolve executable: %v\n", err)
		return info
	}

	if err := update.Apply(info, binary, nil); err != nil {
		fmt.Fprintf(os.Stderr, "zcp: auto-update: %v\n", err)
		return info
	}

	// Re-exec with new binary. On success this never returns.
	if err := update.Exec(); err != nil {
		fmt.Fprintf(os.Stderr, "zcp: auto-update: %v\n", err)
	}

	return info
}

func run(updateInfo *update.Info) error {
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

	// Create and run MCP server on STDIO.
	// SSH deployer remains nil — requires running Zerops container with SSH access.
	srv := server.New(ctx, client, authInfo, store, logFetcher, nil, localDeployer, mounter, updateInfo, rtInfo)
	if err := srv.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("server: %w", err)
	}

	return nil
}
