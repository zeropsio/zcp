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
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/server"
)

func main() {
	// Init dispatch: generate project config files.
	if len(os.Args) > 1 && os.Args[1] == "init" {
		if err := zcpinit.Run("."); err != nil {
			log.Fatalf("init: %v", err)
		}
		return
	}

	// MCP server mode.
	if err := run(); err != nil {
		log.Fatal(err)
	}
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

	// Real mounter for SSHFS operations (only functional on Zerops containers).
	mounter := platform.NewSystemMounter()

	// Create and run MCP server on STDIO.
	// SSH and local deployers will be implemented with real implementations later.
	srv := server.New(client, authInfo, store, logFetcher, nil, nil, mounter)
	if err := srv.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("server: %w", err)
	}

	return nil
}
