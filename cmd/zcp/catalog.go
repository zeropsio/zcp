package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/catalog"
	"github.com/zeropsio/zcp/internal/platform"
)

// defaultSnapshotPath is the committed snapshot location for test validation.
var defaultSnapshotPath = filepath.Join("internal", "knowledge", "testdata", "active_versions.json")

func runCatalog(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: zcp catalog sync")
		os.Exit(1)
	}

	switch args[0] {
	case "sync":
		runCatalogSync()
	default:
		fmt.Fprintf(os.Stderr, "unknown catalog subcommand: %s\n", args[0])
		os.Exit(1)
	}
}

func runCatalogSync() {
	creds, err := auth.ResolveCredentials()
	if err != nil {
		log.Fatalf("auth: %v", err)
	}

	client, err := platform.NewZeropsClient(creds.Token, creds.APIHost)
	if err != nil {
		log.Fatalf("client: %v", err)
	}

	snap, err := catalog.Sync(context.Background(), client, defaultSnapshotPath)
	if err != nil {
		log.Fatalf("catalog sync: %v", err)
	}

	fmt.Fprintf(os.Stderr, "Wrote %d versions to %s\n", len(snap.Versions), defaultSnapshotPath)
}
