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
	"slices"
	"syscall"
	"time"

	"github.com/zeropsio/zcp/cmd/zcp/analyze"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/content"
	zcpinit "github.com/zeropsio/zcp/internal/init"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/server"
	"github.com/zeropsio/zcp/internal/service"
	"github.com/zeropsio/zcp/internal/telemetry"
	"github.com/zeropsio/zcp/internal/telemetry/wire"
	"github.com/zeropsio/zcp/internal/update"
)

// telemetryShutdownTimeout is the fresh flush context both CLI one-shots and
// MCP serve-mode shutdown use before the process's single exit point
// returns (spec-telemetry.md §5.5).
const telemetryShutdownTimeout = 750 * time.Millisecond

func main() {
	// main() is the ONLY caller of os.Exit — run() always returns a code
	// instead of calling os.Exit/log.Fatal directly, so telemetry always
	// gets a chance to flush before the process terminates (spec-telemetry.md
	// §5.5).
	os.Exit(run(os.Args[1:]))
}

// run is the single entry point for both CLI subcommand dispatch and MCP
// server mode. A recognized first argument routes to runCLI (telemetry:
// one cli_command event, spec §5.5); anything else — including no
// arguments — falls through to runServe (telemetry: session_start/
// session_end), preserving the original code's behavior where an
// unrecognized first argument still starts the MCP server.
func run(args []string) int {
	if len(args) > 0 {
		if dispatch, ok := cliDispatch()[args[0]]; ok {
			return runCLI(args, dispatch)
		}
	}
	return runServe()
}

// cliDispatch is the single source of truth for first-level CLI verbs: a
// verb absent from this map is NOT a CLI subcommand — run() falls through
// to MCP serve mode for it. Built fresh per call (run() invokes it at most
// once per process) rather than as a package-level var, to stay clear of
// any global-mutable-state ambiguity — the map itself is never mutated
// after construction either way.
func cliDispatch() map[string]func(rest []string, cfg telemetry.Config) int {
	return map[string]func(rest []string, cfg telemetry.Config) int{
		"init":      func(rest []string, _ telemetry.Config) int { return runInitCmd(rest) },
		"service":   func(rest []string, _ telemetry.Config) int { return runServiceCmd(rest) },
		"version":   func(_ []string, _ telemetry.Config) int { printVersion(); return 0 },
		"update":    func(_ []string, _ telemetry.Config) int { return runUpdate() },
		"eval":      func(rest []string, _ telemetry.Config) int { return runEval(rest) },
		"catalog":   func(rest []string, _ telemetry.Config) int { return runCatalog(rest) },
		"schema":    func(rest []string, _ telemetry.Config) int { return runSchema(rest) },
		"sync":      func(rest []string, _ telemetry.Config) int { return runSync(rest) },
		"analyze":   func(rest []string, _ telemetry.Config) int { return analyze.Run(rest) },
		"telemetry": runTelemetryCmd,
	}
}

// runCLI resolves telemetry once (disclosure → stdout, spec §3.3), runs the
// matched subcommand, emits exactly one cli_command event (spec §4.2), and
// flushes before returning the exit code (spec §5.5 "CLI one-shots ...
// flushed before the single exit point returns").
func runCLI(args []string, dispatch func([]string, telemetry.Config) int) int {
	home, _ := os.UserHomeDir()
	cfg := telemetry.Resolve(os.Getenv, home, server.Version, wireRuntimeEnv(runtime.Detect()), os.Stdout)
	tc := telemetry.New(cfg)

	start := time.Now()
	code := dispatch(args[1:], cfg)
	emitCLICommand(tc, args, start, code == 0)

	ctx, cancel := context.WithTimeout(context.Background(), telemetryShutdownTimeout)
	defer cancel()
	_ = tc.Shutdown(ctx)

	return code
}

// runInitCmd implements `zcp init [nginx|sshfs] [--guided]`. args is
// everything after "init" (mirrors the original inline switch exactly:
// identical stderr messages + exit codes, log.Fatalf converted to
// log.Printf + return 1).
func runInitCmd(args []string) int {
	rt := runtime.Detect()
	if len(args) > 0 {
		switch args[0] {
		case "nginx":
			if err := zcpinit.RunNginx(); err != nil {
				log.Printf("init nginx: %v", err)
				return 1
			}
			return 0
		case "sshfs":
			if err := zcpinit.RunSSHFS(); err != nil {
				log.Printf("init sshfs: %v", err)
				return 1
			}
			return 0
		}
	}
	// `--guided` is a user-only flag: record the local per-project
	// preference (.zcp marker) before setup reads it. Present → guided
	// mode ON; absent → plain `zcp init` turns it OFF (clean tree).
	// Authoring never gets guided. Scanned order-agnostically.
	guided := slices.Contains(args, "--guided") && !rt.Authoring
	if err := content.SetGuided(".", guided); err != nil {
		log.Printf("init: record guided preference: %v", err)
		return 1
	}
	if err := zcpinit.Run(".", rt); err != nil {
		log.Printf("init: %v", err)
		return 1
	}
	return 0
}

// runServiceCmd implements `zcp service start <nginx|vscode>`. args is
// everything after "service".
func runServiceCmd(args []string) int {
	if len(args) < 2 || args[0] != "start" {
		log.Print("usage: zcp service start <nginx|vscode>")
		return 1
	}
	if err := service.Start(args[1]); err != nil {
		log.Printf("service start: %v", err)
		return 1
	}
	return 0
}

func printVersion() {
	fmt.Fprintf(os.Stdout, "zcp %s (%s, %s)\n", server.Version, server.Commit, server.Built)
}

func runUpdate() int {
	ctx := context.Background()

	fmt.Fprintln(os.Stderr, "Checking for updates...")
	checker := update.NewChecker(server.Version)
	checker.CacheTTL = 0 // force fresh check
	info := checker.Check(ctx)

	if !info.Available {
		fmt.Fprintf(os.Stderr, "Already up to date (%s).\n", server.Version)
		return 0
	}

	fmt.Fprintf(os.Stderr, "Update available: %s → %s\n", info.CurrentVersion, info.LatestVersion)
	fmt.Fprintln(os.Stderr, "Downloading...")

	binary, err := os.Executable()
	if err != nil {
		log.Printf("resolve executable: %v", err)
		return 1
	}

	if err := update.Apply(ctx, info, binary, nil); err != nil {
		log.Printf("update: %v", err)
		return 1
	}

	fmt.Fprintln(os.Stderr, "Updated successfully. Restart ZCP to use the new version.")
	return 0
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

// logShutdown writes a categorized shutdown reason to the crash log and, if
// tel is non-nil, emits the session_end event (spec §5.5): uptime →
// duration_ms, shutdown reason enum → dims[shutdown_reason], dropped is the
// caller's already-read Client.DroppedCount() (spec §5.1/§5.2 cumulative
// count). Categories: client disconnected (stdin EOF), signal (SIGINT/
// SIGTERM), stdin closed, broken pipe, or error with details.
func logShutdown(f io.WriteCloser, err error, startedAt time.Time, srv *server.Server, tel telemetry.Emitter, dropped int64) {
	ts := time.Now().Format(time.RFC3339)
	pid := os.Getpid()
	uptime := time.Since(startedAt).Truncate(time.Second)

	var calls int64
	if srv != nil {
		calls = srv.CallCount()
	}

	var reason, shutdownReason string
	switch {
	case err == nil:
		reason, shutdownReason = "client disconnected", wire.ShutdownClientDisconnect
	case errors.Is(err, context.Canceled):
		reason, shutdownReason = "signal", wire.ShutdownSignal
	case errors.Is(err, io.EOF):
		reason, shutdownReason = "stdin closed", wire.ShutdownStdinClosed
	case errors.Is(err, syscall.EPIPE):
		reason, shutdownReason = "broken pipe", wire.ShutdownBrokenPipe
	default:
		reason, shutdownReason = fmt.Sprintf("error: %v", err), wire.ShutdownError
	}

	if tel != nil {
		tel.Emit(wire.Event{
			EventType:    wire.EventSessionEnd,
			DurationMs:   time.Since(startedAt).Milliseconds(),
			Dims:         map[string]string{wire.DimShutdownReason: shutdownReason},
			DroppedCount: dropped,
		})
	}

	if f == nil {
		return
	}
	defer f.Close()

	fmt.Fprintf(f, "[%s] shutdown: %s (pid=%d, uptime=%s, calls=%d)\n",
		ts, reason, pid, uptime, calls)
}

// runServe starts the MCP server on STDIO — the fallback path for no
// arguments or an unrecognized first argument (preserves the original
// behavior). Telemetry: session_start after consent resolve, session_end
// inside logShutdown, final Shutdown(750ms) flush before returning.
func runServe() int {
	// Ignore SIGPIPE: when Claude Code closes the stdio pipe, Go's default
	// behavior kills the process on writes to fd 1/2. Converting SIGPIPE to
	// EPIPE errors lets the MCP SDK shut down gracefully instead.
	signal.Ignore(syscall.SIGPIPE)

	// The MCP stdio transport owns fd 1 (the JSON-RPC stream). Repoint
	// os.Stdout at stderr BEFORE runServer() so any stray stdout write from
	// a dependency — notably the zerops-go SDK's fmt.Println on transport
	// errors, which runServer()'s auth + GetUserInfo can trigger before the
	// server is even built — cannot corrupt the protocol. The saved real
	// stdout is handed to the transport explicitly.
	mcpStdout := os.Stdout
	os.Stdout = os.Stderr

	crashLog := setupCrashLog()
	startedAt := time.Now()

	home, _ := os.UserHomeDir()
	cfg := telemetry.Resolve(os.Getenv, home, server.Version, wireRuntimeEnv(runtime.Detect()), os.Stderr)
	tc := telemetry.New(cfg)
	tc.Emit(wire.Event{EventType: wire.EventSessionStart})

	srv, err := runServer(mcpStdout, tc)
	logShutdown(crashLog, err, startedAt, srv, tc, tc.DroppedCount())

	ctx, cancel := context.WithTimeout(context.Background(), telemetryShutdownTimeout)
	defer cancel()
	_ = tc.Shutdown(ctx)

	if err != nil && !errors.Is(err, context.Canceled) {
		log.Print(err)
		return 1
	}
	return 0
}

func runServer(mcpStdout io.Writer, tel telemetry.Emitter) (*server.Server, error) {
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

	// Headless hygiene: warn when neither AGENTS.md nor CLAUDE.md is
	// present in cwd. Doctrine lives in AGENTS.md (canonical) + CLAUDE.md
	// (Claude's @AGENTS.md include wrapper) — TestBuildInstructions_
	// NoStaticRulesLeak forbids injecting it into MCP Instructions.
	// Without either file the agent has only tool descriptions and lacks
	// workflow guidance. `zcp init` writes both.
	if cwd, cwdErr := os.Getwd(); cwdErr == nil {
		zcpinit.WarnMissingAgentContext(cwd, os.Stderr)
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
	srv := server.New(ctx, client, authInfo, store, logFetcher, sshDeployer, mounter, rtInfo, tel)

	// Silent background update — completely invisible to LLM.
	// Checks GitHub (24h cache), downloads if newer. Binary is replaced on disk
	// but the running server is NOT restarted — new version activates on next start.
	if os.Getenv("ZCP_AUTO_UPDATE") != "0" {
		go update.Once(ctx, server.Version, os.Stderr)
	}

	err = srv.Run(ctx, mcpStdout)
	if err != nil && !errors.Is(err, context.Canceled) {
		return srv, fmt.Errorf("server: %w", err)
	}
	return srv, err
}

// wireRuntimeEnv maps runtime.Info to the wire's closed runtime_env enum
// (spec §4.2/§4.3).
func wireRuntimeEnv(rt runtime.Info) string {
	if rt.InContainer {
		return wire.RuntimeContainer
	}
	return wire.RuntimeLocal
}

// cliActionVerbs is the closed set of top-level commands whose second
// positional argument is a small, stable verb (spec §5.5 item 2: "action =
// second arg for sync|schema|catalog|service|telemetry"). Every other
// command's second argument is free-form (a scenario id, a path, a flag)
// and must never reach the wire (spec B2).
var cliActionVerbs = map[string]bool{
	"sync": true, "schema": true, "catalog": true, "service": true, "telemetry": true,
}

// emitCLICommand builds and emits the cli_command wire.Event for one CLI
// dispatch (spec §4.2 table). command/action are shape-checked against the
// same identifier pattern the MCP middleware uses (spec §5.3 "Values
// failing shape checks → unknown") so a stray value collapses to the
// UnknownIdentifier sentinel instead of silently dropping the whole event
// at ValidateLite. Takes the Emitter seam (not the concrete *Client) so
// tests can inject an in-memory recorder.
func emitCLICommand(tc telemetry.Emitter, args []string, start time.Time, success bool) {
	e := wire.Event{
		EventType:  wire.EventCLICommand,
		Command:    shapeOrUnknownCLI(args[0]),
		DurationMs: time.Since(start).Milliseconds(),
		Success:    success,
	}
	if cliActionVerbs[args[0]] && len(args) > 1 {
		e.Action = shapeOrUnknownCLI(args[1])
	}
	tc.Emit(e)
}

// shapeOrUnknownCLI mirrors internal/server's identical private helper
// (spec §5.3) for the CLI tap point — the two packages can't share it
// directly, so both call sites reuse wire's exported
// ValidIdentifierShape/UnknownIdentifier (the single owner of the shape +
// sentinel) instead of each re-authoring the substitution rule.
func shapeOrUnknownCLI(v string) string {
	if v == "" || wire.ValidIdentifierShape(v) {
		return v
	}
	return wire.UnknownIdentifier
}
