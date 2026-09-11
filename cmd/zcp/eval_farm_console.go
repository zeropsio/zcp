package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/zeropsio/zcp/internal/eval/farm"
	"github.com/zeropsio/zcp/internal/eval/farm/console"
)

// observeTimeout is the observer's per-call budget (docs/spec-eval-farm.md
// §7.4 FM-44: "Timeout: 5 minutes").
const observeTimeout = 5 * time.Minute

// observerKillSwitchEnabled reports ZCP_FARM_OBSERVER=off (§8.1, §8.5): the
// console-wide kill switch, read once at startup.
func observerKillSwitchEnabled() bool {
	return os.Getenv("ZCP_FARM_OBSERVER") == console.ObserverOff
}

// anthropicAPIKeySet reports ANTHROPIC_API_KEY being set in the console's
// env (§8.5 FM-53): the observer must run under CLAUDE_CODE_OAUTH_TOKEN
// only, so an operator-set API key is treated exactly like a missing OAuth
// token — the worker stays idle and actions answer 503 "observer
// credential missing" — rather than letting `claude` silently switch to
// API-key billing.
func anthropicAPIKeySet() bool {
	return os.Getenv("ANTHROPIC_API_KEY") != ""
}

// resolveClaudePath resolves claudeFlag (the --claude value, default
// "claude") to an absolute path via exec.LookPath + filepath.Abs (§7.4
// FM-44: "made absolute ... before the child starts, because the child's
// working directory is the empty temp dir"). Returns "" when it cannot be
// resolved — the caller leaves the worker idle and the run/batch observe
// actions answer 503 "observer unavailable" rather than failing every
// observation (§8.5).
func resolveClaudePath(claudeFlag string) string {
	found, err := exec.LookPath(claudeFlag)
	if err != nil {
		return ""
	}
	abs, err := filepath.Abs(found)
	if err != nil {
		return ""
	}
	return abs
}

// runFarmConsole implements `zcp eval farm console --listen <addr> --claude
// <path>` (docs/spec-eval-farm.md §8.1 FM-49): a long-lived HTTP server
// over the farm bucket. It never holds an account-wide Zerops key (run
// state comes from the bucket alone) — only the farm bucket credentials
// (farm.ConfigFromEnv, ZCP_FARM_S3_*), the console's own bearer/login token
// (ZCP_FARM_CONSOLE_TOKEN, required — refuses to start without it, never
// printed or logged), and the observer's own CLAUDE_CODE_OAUTH_TOKEN
// (§7.4), passed to the runner explicitly rather than inherited by the
// child process.
func runFarmConsole(args []string) int {
	listen, claudeFlag, err := parseConsoleArgs(args)
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

	killSwitch := observerKillSwitchEnabled()
	oauthToken := os.Getenv("CLAUDE_CODE_OAUTH_TOKEN")
	apiKeySet := anthropicAPIKeySet()
	if apiKeySet {
		fmt.Fprintln(os.Stderr, "warning: ANTHROPIC_API_KEY is set; the observer worker stays idle (docs/spec-eval-farm.md §8.5 FM-53)")
	}
	credentialMissing := oauthToken == ""
	claudePath := resolveClaudePath(claudeFlag)
	claudeUnresolved := claudePath == ""
	if claudeUnresolved {
		fmt.Fprintf(os.Stderr, "warning: --claude %q could not be resolved; the observer worker stays idle (docs/spec-eval-farm.md §8.5 FM-53)\n", claudeFlag)
	}

	queue := console.NewQueue(console.NewBucketObserveFunc(store, console.BucketObserveConfig{
		ClaudePath: claudePath,
		OAuthToken: oauthToken,
		Timeout:    observeTimeout,
		Environ:    os.Environ,
		Now:        time.Now,
	}))
	worker := console.NewWorker(console.WorkerConfig{
		Bucket:   store,
		Queue:    queue,
		Disabled: killSwitch || credentialMissing || apiKeySet || claudeUnresolved,
	})

	srv := console.NewServer(console.Config{
		Store:                        store,
		Token:                        token,
		ObserverDisabled:             killSwitch,
		Queue:                        queue,
		Worker:                       worker,
		ObserverCredentialMissing:    credentialMissing,
		ObserverAPIKeySet:            apiKeySet,
		ObserverClaudePathUnresolved: claudeUnresolved,
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv.StartWorker(ctx)
	go srv.WarmCache(ctx)

	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", listen)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: listen %s: %v\n", listen, err)
		return 1
	}

	httpSrv := newConsoleHTTPServer(srv.Handler())
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

// consoleReadHeaderTimeout/consoleReadTimeout/consoleIdleTimeout bound the
// console's HTTP server (item 8): an unbounded read or idle timeout lets a
// slow or idle client tie up a connection indefinitely.
const (
	consoleReadHeaderTimeout = 10 * time.Second
	consoleReadTimeout       = 30 * time.Second
	consoleIdleTimeout       = 120 * time.Second
)

// newConsoleHTTPServer builds the console's http.Server with its fixed
// timeouts — a small constructor so the timeout values are testable
// without starting a real listener.
func newConsoleHTTPServer(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: consoleReadHeaderTimeout,
		ReadTimeout:       consoleReadTimeout,
		IdleTimeout:       consoleIdleTimeout,
	}
}

func parseConsoleArgs(args []string) (listen, claude string, err error) {
	listen = ":8080"
	claude = "claude"
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--listen":
			if i+1 >= len(args) {
				return "", "", fmt.Errorf("%s requires a value", arg)
			}
			listen = args[i+1]
			i++
		case "--claude":
			if i+1 >= len(args) {
				return "", "", fmt.Errorf("%s requires a value", arg)
			}
			claude = args[i+1]
			i++
		default:
			return "", "", fmt.Errorf("unknown flag %s", arg)
		}
	}
	return listen, claude, nil
}
