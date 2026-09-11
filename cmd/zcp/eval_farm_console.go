package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zeropsio/zcp/internal/eval/farm"
	"github.com/zeropsio/zcp/internal/eval/farm/console"
)

// runFarmConsole implements `zcp eval farm console --listen <addr>`
// (docs/spec-eval-farm.md §8.1 FM-49): a long-lived HTTP server over the
// farm bucket. It never holds an account-wide Zerops key (run state comes
// from the bucket alone) — only the farm bucket credentials
// (farm.ConfigFromEnv, ZCP_FARM_S3_*) and the console's own bearer/login
// token (ZCP_FARM_CONSOLE_TOKEN, required — refuses to start without it,
// never printed or logged). --claude (the observer worker's binary path)
// arrives with S5.
func runFarmConsole(args []string) int {
	listen, err := parseConsoleArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 2
	}

	token := os.Getenv("ZCP_FARM_CONSOLE_TOKEN")
	if token == "" {
		fmt.Fprintln(os.Stderr, "error: ZCP_FARM_CONSOLE_TOKEN is required (docs/spec-eval-farm.md §8.1 FM-49)")
		return 1
	}

	sinkCfg, err := farm.ConfigFromEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	store := farm.NewSinkClient(sinkCfg)

	observerDisabled := os.Getenv("ZCP_FARM_OBSERVER") == console.ObserverOff

	srv := console.NewServer(console.Config{
		Store:            store,
		Token:            token,
		ObserverDisabled: observerDisabled,
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", listen)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: listen %s: %v\n", listen, err)
		return 1
	}

	httpSrv := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		// context.WithoutCancel keeps ctx's values (none here) without its
		// already-fired cancellation, so the 10s shutdown deadline below is
		// the only thing that can end this wait — not the SIGINT/SIGTERM
		// that just fired ctx.Done().
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
	}()

	fmt.Fprintf(os.Stderr, "farm console serving on %s\n", ln.Addr().String())
	serveErr := httpSrv.Serve(ln)
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		fmt.Fprintf(os.Stderr, "error: serve: %v\n", serveErr)
		return 1
	}
	return 0
}

func parseConsoleArgs(args []string) (listen string, err error) {
	listen = ":8080"
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--listen":
			if i+1 >= len(args) {
				return "", fmt.Errorf("%s requires a value", arg)
			}
			listen = args[i+1]
			i++
		default:
			return "", fmt.Errorf("unknown flag %s", arg)
		}
	}
	return listen, nil
}
