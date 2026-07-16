package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/studiows"
	"github.com/zeropsio/zcp/internal/tools"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// runStudio dispatches `zcp studio <subcommand>` — the transport the Zerops
// Studio VS Code extension shells out to (decision D2=b in the Studio PRD). The
// extension never speaks MCP or reads raw .zcp/state (LG5), and stays
// token-blind: it passes only the workspace cwd; zcp resolves credentials
// itself, the same way the `zcp serve` MCP child does.
//
// Verbs (frozen seam — slices add JS that shells out to these, never new Go):
//
//	topology  read-only: discover + classify JSON (E3)
//	deploy    mutating:  push local code, poll build, enable subdomain
//	sync-env  mutating:  render a local .env from zerops.yaml
//
// Each mutating verb wraps the SAME ops the corresponding MCP tool calls
// (ops.DeployLocal / ops.EnvGenerateDotenv), so the cockpit can't diverge from
// the agent-facing behaviour at the platform layer.
func runStudio(args []string) {
	if len(args) == 0 || isStudioHelp(args[0]) {
		printStudioUsage()
		if len(args) == 0 {
			os.Exit(1)
		}
		return
	}
	switch args[0] {
	case "topology":
		runStudioTopology(args[1:])
	case "deploy":
		runStudioDeploy(args[1:])
	case "sync-env":
		runStudioSyncEnv(args[1:])
	case "watch":
		runStudioWatch(args[1:])
	case "console":
		runStudioConsole(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown studio subcommand: %s\n\n", args[0])
		printStudioUsage()
		os.Exit(1)
	}
}

func isStudioHelp(s string) bool { return isHelpArg(s) }

func printStudioUsage() {
	fmt.Fprintln(os.Stderr, `Usage: zcp studio <subcommand>

Subcommands:
  topology              Emit zerops_discover JSON for the cockpit (read-only)
  deploy   --service H  Deploy local code to service H, poll build, enable subdomain
  sync-env [--setup S]  Render a local .env from zerops.yaml
  watch                 Stream live topology-change events (websocket) as JSON
  console serve         Serve the Data Console (viewer+editor for managed services)`)
}

// studioInit resolves credentials (token-blind: env -> ./.mcp.json -> zcli) and
// returns an authenticated client + the resolved project/auth info. Shared by
// every studio verb. Exits non-zero with a stderr diagnostic on any failure —
// the extension surfaces that to the user.
func studioInit() (platform.Client, *auth.Info, context.Context) {
	injectMCPEnvIfUnset(".")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	creds, err := auth.ResolveCredentials()
	if err != nil {
		stop()
		fmt.Fprintf(os.Stderr, "auth: %v\n", err)
		os.Exit(1)
	}
	client, err := platform.NewZeropsClient(creds.Token, creds.APIHost)
	if err != nil {
		stop()
		fmt.Fprintf(os.Stderr, "client: %v\n", err)
		os.Exit(1)
	}
	authInfo, err := auth.Resolve(ctx, client)
	if err != nil {
		stop()
		fmt.Fprintf(os.Stderr, "auth: %v\n", err)
		os.Exit(1)
	}
	return client, authInfo, ctx
}

func emitJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintf(os.Stderr, "encode: %v\n", err)
		os.Exit(1)
	}
}

// runStudioTopology emits the SAME discover+classify JSON the zerops_discover
// MCP tool produces — one owner (tools.EnrichWithMetaStatus) classifies for
// both. JSON to stdout, diagnostics to stderr (cmd/ is exempt from the MCP
// STDIO purity rule — TestNoStdoutOutsideJSONPath scopes to internal/).
func runStudioTopology(_ []string) {
	client, authInfo, ctx := studioInit()

	stateDir := ""
	if cwd, err := os.Getwd(); err == nil {
		stateDir = filepath.Join(cwd, ".zcp", "state")
	}

	// Lean topology for the cockpit Parts view: no env keys/values and no
	// per-service filter.
	result, err := ops.Discover(ctx, client, authInfo.ProjectID, "", false, false, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "discover: %v\n", err)
		os.Exit(1)
	}
	// The cockpit shows a live "Open" link per HTTP service, so resolve subdomain
	// URLs here (the lean Discover omits them to keep the agent path env-free).
	ops.FillSubdomainURLs(ctx, client, result)
	tools.EnrichWithMetaStatus(result, stateDir)
	emitJSON(result)
}

// runStudioDeploy pushes local code to a service, blocks on the build, and
// enables the subdomain — wrapping ops.DeployLocal + ops.PollBuild +
// ops.Subdomain (the same primitives zerops_deploy local uses). Build progress
// goes to stderr; the final DeployResult JSON goes to stdout. Prototype scope:
// this is the thin cockpit deploy path — it does not run the agent-workflow
// guards the MCP tool layers on (adoption gate, work-session tracking,
// repo-delivery redirect); the platform push itself is identical.
func runStudioDeploy(args []string) {
	fs := flag.NewFlagSet("studio deploy", flag.ExitOnError)
	service := fs.String("service", "", "target service hostname (required)")
	setup := fs.String("setup", "", "zerops.yaml setup block name (optional)")
	workingDir := fs.String("working-dir", ".", "local path to push from")
	_ = fs.Parse(args)

	if *service == "" {
		fmt.Fprintln(os.Stderr, "studio deploy: --service is required")
		os.Exit(1)
	}

	client, authInfo, ctx := studioInit()
	projectID := authInfo.ProjectID

	// Resolve the zerops.yaml setup-block name BEFORE deploying. The cockpit
	// Deploy button never sends --setup, and the hostname is NOT the setup name
	// once setups carry semantic names (dev/prod) — defaulting to it makes the
	// platform reject the push with "The setup was not found" (the exact
	// container-mode failure: appdev's yaml declares setups dev+prod, neither
	// equals "appdev"). Resolve from the ServiceMeta single owner the SAME way
	// the agent-facing zerops_deploy does, so the cockpit can't diverge. This is
	// part of the platform push itself, not an agent-workflow guard.
	setupName := *setup
	if setupName == "" {
		stateDir := ""
		if cwd, cerr := os.Getwd(); cerr == nil {
			stateDir = filepath.Join(cwd, ".zcp", "state")
		}
		resolved, rerr := resolveDeploySetup(ctx, client, stateDir, *service, *workingDir)
		switch rerr {
		case nil:
			setupName = resolved
		default:
			// A genuine multi-setup ambiguity (yaml has >1 setup, none matches
			// hostname/role) has no cockpit picker — surface the choices, don't
			// reproduce the cryptic platform error. Any other miss (e.g. no yaml
			// on disk) falls through with an empty setup so DeployLocal emits its
			// precise "zerops.yaml not found" message.
			var blocker *workflow.ErrRequiresSetupInput
			if errors.As(rerr, &blocker) && len(blocker.AvailableSetups) > 1 {
				fmt.Fprintf(os.Stderr,
					"deploy: zerops.yaml declares multiple setups (%s) and none matches %q — re-run with --setup <name>\n",
					strings.Join(blocker.AvailableSetups, ", "), *service)
				os.Exit(1)
			}
		}
	}

	onProgress := func(message string, _, _ float64) {
		if message != "" {
			fmt.Fprintf(os.Stderr, "  %s\n", message)
		}
	}

	result, err := ops.DeployLocal(ctx, client, projectID, *authInfo, *service, setupName, *workingDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "deploy: %v\n", err)
		os.Exit(1)
	}

	if result.TargetServiceID != "" {
		if event, perr := ops.PollBuild(ctx, client, projectID, result.TargetServiceID, onProgress); perr != nil {
			result.TimedOut = true
		} else {
			result.BuildStatus = event.Status
		}
	}

	// Best-effort subdomain enable for eligible (HTTP) services — mirrors the
	// deploy tool's auto-enable. Non-HTTP services error here; swallow it.
	if _, serr := ops.Subdomain(ctx, client, projectID, *service, "enable"); serr == nil {
		result.SubdomainAccessEnabled = true
	}
	// Read the live subdomain URL back through discover (one owner of the URL).
	if dr, derr := ops.Discover(ctx, client, projectID, *service, false, false, false); derr == nil {
		for i := range dr.Services {
			if dr.Services[i].Hostname == *service && dr.Services[i].SubdomainURL != "" {
				result.SubdomainURL = dr.Services[i].SubdomainURL
				result.SubdomainAccessEnabled = true
			}
		}
	}

	emitJSON(result)
}

// resolveDeploySetup resolves the canonical zerops.yaml setup-block name for a
// cockpit deploy, mirroring the agent-facing deploy's single owner: the local
// ServiceMeta cache (.zcp/state) first, then the working-dir zerops.yaml. Factored
// out of runStudioDeploy so the resolution is unit-testable without the auth /
// os.Exit boundary.
//
// ServiceID is intentionally omitted from the cascade input: a cockpit
// deploy-to-preview pushes the LOCAL working-dir code, so the local yaml + meta
// cache are the source of truth — not a service's github-integration setup
// (cascade steps 2-4, which a passed ServiceID would enable). Returns the
// resolver's error verbatim (a *workflow.ErrRequiresSetupInput on a genuine
// multi-setup ambiguity) so the caller can branch on it.
func resolveDeploySetup(ctx context.Context, client platform.Client, stateDir, service, workingDir string) (string, error) {
	var yamlBody string
	if raw, err := ops.ReadZeropsYmlRaw(workingDir); err == nil {
		yamlBody = string(raw)
	}
	var modeHint topology.Mode
	if meta, _ := workflow.FindServiceMeta(stateDir, service); meta != nil {
		modeHint = meta.ModeFor(service)
	}
	return workflow.ResolveCanonicalSetup(ctx, client, workflow.ResolveCanonicalSetupInput{
		StateDir:       stateDir,
		TargetHostname: service,
		Mode:           modeHint,
		LocalYAMLBody:  yamlBody,
	})
}

// runStudioSyncEnv renders a local .env from the working-dir zerops.yaml,
// wrapping ops.EnvGenerateDotenv (the same op zerops_env generate-dotenv uses).
func runStudioSyncEnv(args []string) {
	fs := flag.NewFlagSet("studio sync-env", flag.ExitOnError)
	setup := fs.String("setup", "", "zerops.yaml setup block name (optional)")
	workingDir := fs.String("working-dir", ".", "directory containing zerops.yaml")
	preview := fs.Bool("preview", false, "dry-run: report the diff without writing")
	force := fs.Bool("force", false, "bypass the refuse-on-unowned-edits gate")
	_ = fs.Parse(args)

	client, authInfo, ctx := studioInit()

	result, err := ops.EnvGenerateDotenv(ctx, client, authInfo.ProjectID, *setup, *workingDir, ops.EnvGenerateDotenvOptions{
		Preview: *preview,
		Force:   *force,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "sync-env: %v\n", err)
		os.Exit(1)
	}
	emitJSON(result)
}

// runStudioWatch streams live topology-change events over the platform
// websocket (internal/studiows) as newline-delimited JSON on stdout, so the
// cockpit updates on push instead of polling. Long-lived: runs until the parent
// closes stdin or a signal arrives. Token-blind seam: zcp owns the credentials
// + the socket; the extension only consumes the event stream.
func runStudioWatch(_ []string) {
	_, authInfo, ctx := studioInit()

	ctx, cancel := context.WithCancel(ctx)
	// Exit when the parent (the extension) closes our stdin, so an orphaned
	// watcher never lingers after the cockpit goes away.
	go func() {
		_, _ = io.Copy(io.Discard, os.Stdin)
		cancel()
	}()

	enc := json.NewEncoder(os.Stdout)
	var mu sync.Mutex
	emit := func(ev studiows.Event) {
		// Serialize: emit is called from the watcher's read goroutine.
		mu.Lock()
		defer mu.Unlock()
		if err := enc.Encode(ev); err != nil {
			// stdout is broken (the cockpit is gone) — stop the watcher.
			cancel()
		}
	}

	err := studiows.Watch(ctx, studiows.Config{
		APIHost:   authInfo.APIHost,
		Token:     authInfo.Token,
		ClientID:  authInfo.ClientID,
		ProjectID: authInfo.ProjectID,
	}, emit)
	cancel()
	if err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(os.Stderr, "watch: %v\n", err)
		os.Exit(1)
	}
}

// injectMCPEnvIfUnset reads dir/.mcp.json (the project-scoped config `zcp init`
// writes; the user may add an env block carrying ZCP_API_KEY) and, ONLY for
// variables not already in the environment, exports them so the standard
// env-first credential resolution finds the project token. This keeps the VS
// Code extension token-blind: it never reads or forwards the secret — zcp lifts
// it from the same file the serve child is configured from. The token is set
// into this short-lived process's own environment and never crosses into any
// response/log surface.
func injectMCPEnvIfUnset(dir string) {
	if os.Getenv("ZCP_API_KEY") != "" {
		return
	}
	data, err := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if err != nil {
		return
	}
	var cfg struct {
		MCPServers map[string]struct {
			Env map[string]string `json:"env"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return
	}
	srv, ok := cfg.MCPServers["zerops"]
	if !ok {
		return
	}
	for _, k := range []string{"ZCP_API_KEY", "ZCP_API_HOST", "ZCP_REGION"} {
		if v := srv.Env[k]; v != "" && os.Getenv(k) == "" {
			_ = os.Setenv(k, v)
		}
	}
}
