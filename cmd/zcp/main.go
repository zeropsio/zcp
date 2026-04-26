package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/zeropsio/zcp/cmd/zcp/analyze"
	"github.com/zeropsio/zcp/cmd/zcp/check"
	"github.com/zeropsio/zcp/internal/auth"
	zcpinit "github.com/zeropsio/zcp/internal/init"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/server"
	"github.com/zeropsio/zcp/internal/service"
	"github.com/zeropsio/zcp/internal/update"
)

func main() {
	// Subcommand dispatch.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "init":
			rt := runtime.Detect()
			if len(os.Args) > 2 {
				switch os.Args[2] {
				case "nginx":
					if err := zcpinit.RunNginx(); err != nil {
						log.Fatalf("init nginx: %v", err)
					}
					return
				case "sshfs":
					if err := zcpinit.RunSSHFS(); err != nil {
						log.Fatalf("init sshfs: %v", err)
					}
					return
				}
			}
			if err := zcpinit.Run(".", rt); err != nil {
				log.Fatalf("init: %v", err)
			}
			return
		case "service":
			if len(os.Args) < 4 || os.Args[2] != "start" {
				log.Fatal("usage: zcp service start <nginx|vscode>")
			}
			if err := service.Start(os.Args[3]); err != nil {
				log.Fatalf("service start: %v", err)
			}
			return
		case "version":
			printVersion()
			return
		case "update":
			runUpdate()
			return
		case "eval":
			runEval(os.Args[2:])
			return
		case "catalog":
			runCatalog(os.Args[2:])
			return
		case "sync":
			runSync(os.Args[2:])
			return
		case "check":
			check.Run(os.Args[2:])
			return
		case "dry-run":
			runDryRun(os.Args[2:])
			return
		case "analyze":
			analyze.Run(os.Args[2:])
			return
		}
	}

	// Ignore SIGPIPE: when Claude Code closes the stdio pipe, Go's default
	// behavior kills the process on writes to fd 1/2. Converting SIGPIPE to
	// EPIPE errors lets the MCP SDK shut down gracefully instead.
	signal.Ignore(syscall.SIGPIPE)

	// MCP server mode — starts immediately, no blocking update check.
	crashLog := setupCrashLog()
	startedAt := time.Now()

	srv, err := run()
	logShutdown(crashLog, err, startedAt, srv)

	if err != nil && !errors.Is(err, context.Canceled) {
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

// setupCrashLog opens ~/.zcp/serve.log for append, creating the directory if
// needed. Returns nil if the log cannot be created (non-fatal).
func setupCrashLog() io.WriteCloser {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	dir := filepath.Join(home, ".zcp")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil
	}
	f, err := os.OpenFile(filepath.Join(dir, "serve.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil
	}
	fmt.Fprintf(f, "[%s] zcp serve started (version=%s, pid=%d)\n",
		time.Now().Format(time.RFC3339), server.Version, os.Getpid())
	return f
}

// logShutdown writes a categorized shutdown reason to the crash log.
// Categories: client disconnected (stdin EOF), signal (SIGINT/SIGTERM),
// stdin closed, broken pipe, or error with details.
func logShutdown(f io.WriteCloser, err error, startedAt time.Time, srv *server.Server) {
	if f == nil {
		return
	}
	defer f.Close()

	ts := time.Now().Format(time.RFC3339)
	pid := os.Getpid()
	uptime := time.Since(startedAt).Truncate(time.Second)

	var calls int64
	if srv != nil {
		calls = srv.CallCount()
	}

	var reason string
	switch {
	case err == nil:
		reason = "client disconnected"
	case errors.Is(err, context.Canceled):
		reason = "signal"
	case errors.Is(err, io.EOF):
		reason = "stdin closed"
	case errors.Is(err, syscall.EPIPE):
		reason = "broken pipe"
	default:
		reason = fmt.Sprintf("error: %v", err)
	}

	fmt.Fprintf(f, "[%s] shutdown: %s (pid=%d, uptime=%s, calls=%d)\n",
		ts, reason, pid, uptime, calls)
}

func run() (*server.Server, error) {
	// Bootstrap: resolve credentials (env var or zcli) to create platform client.
	creds, err := auth.ResolveCredentials()
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}

	client, err := platform.NewZeropsClient(creds.Token, creds.APIHost)
	if err != nil {
		return nil, fmt.Errorf("create platform client: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Full auth: validate token via API and discover project.
	authInfo, err := auth.Resolve(ctx, client)
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}

	// Log fetcher for zerops_logs tool.
	logFetcher := platform.NewLogFetcher()

	// Knowledge store for zerops_knowledge tool.
	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		return nil, fmt.Errorf("knowledge store: %w", err)
	}

	// Detect runtime environment (Zerops container vs local dev).
	rtInfo := runtime.Detect()

	// Headless hygiene: warn when CLAUDE.md is missing in cwd. Doctrine
	// lives in CLAUDE.md (TestBuildInstructions_NoStaticRulesLeak forbids
	// injecting it into MCP Instructions); without the file, agents have
	// only tool descriptions and lack workflow guidance. zcp init writes it.
	if cwd, cwdErr := os.Getwd(); cwdErr == nil {
		zcpinit.WarnMissingClaudeMD(cwd, os.Stderr)
	}

	// Mounter requires SSHFS — only available inside Zerops containers.
	var mounter ops.Mounter
	if rtInfo.InContainer {
		mounter = platform.NewSystemMounter()
	}

	// SSH deployer for deploy — only available inside Zerops containers.
	var sshDeployer ops.SSHDeployer
	if rtInfo.InContainer {
		sshDeployer = platform.NewSystemSSHDeployer()
	}

	// Create and run MCP server on STDIO.
	srv := server.New(ctx, client, authInfo, store, logFetcher, sshDeployer, mounter, rtInfo)

	// Silent background update — completely invisible to LLM.
	// Checks GitHub (24h cache), downloads if newer. Binary is replaced on disk
	// but the running server is NOT restarted — new version activates on next start.
	if os.Getenv("ZCP_AUTO_UPDATE") != "0" {
		go update.Once(ctx, server.Version, os.Stderr)
	}

	err = srv.Run(ctx)
	if err != nil && !errors.Is(err, context.Canceled) {
		return srv, fmt.Errorf("server: %w", err)
	}
	return srv, err
}
