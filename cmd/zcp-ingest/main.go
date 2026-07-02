// zcp-ingest is the standalone telemetry ingest service
// (docs/spec-telemetry.md §6): POST /v1/events + GET /healthz, backed by
// ClickHouse. Deployed separately from the zcp CLI/MCP binary.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/zeropsio/zcp/internal/ingest"
)

func main() {
	os.Exit(run())
}

// run is main's body, split out so `defer stop()` actually executes before
// the process exits (calling os.Exit directly inside main would skip it).
func run() int {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	cfg := ingest.LoadConfig(os.Getenv)
	if err := ingest.Run(ctx, cfg, logger); err != nil {
		logger.Error("ingest: fatal", "error", err)
		return 1
	}
	return 0
}
