package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/authoring/port"
	"github.com/zeropsio/zcp/internal/authoring/recipe"
	"github.com/zeropsio/zcp/internal/content"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/schema"
	"github.com/zeropsio/zcp/internal/tools"
	"github.com/zeropsio/zcp/internal/workflow"
)

// Version, Commit, Built are set by ldflags at build time.
var (
	Version = "dev"
	Commit  = "unknown"
	Built   = "unknown"
)

// Server wraps the MCP server with Zerops-specific configuration.
type Server struct {
	server      *mcp.Server
	client      platform.Client
	authInfo    *auth.Info
	store       knowledge.Provider
	logFetcher  platform.LogFetcher
	sshDeployer ops.SSHDeployer
	mounter     ops.Mounter
	rtInfo      runtime.Info
	logger      *slog.Logger
	calls       atomic.Int64
}

// CallCount returns the number of tool calls served during this server's lifetime.
func (s *Server) CallCount() int64 { return s.calls.Load() }

// New creates a new ZCP MCP server with all tools registered.
//
// In local env, New eagerly auto-adopts the project: checks whether any
// ServiceMeta exists under .zcp/state/services/, and if not, writes one
// keyed by the Zerops project name with the appropriate topology
// (local-stage when exactly one runtime exists, local-only otherwise).
// Legacy local metas from the pre-A.4 layout are migrated in place. The
// resulting adoption note is appended to the MCP instructions so the LLM
// sees it in its system prompt from the first turn — no stale
// "not-adopted" window for any tool handler to observe.
//
// Adoption failures (API unreachable, project has no name) are logged
// and propagated as empty note so the server still starts; the LLM will
// see the consequence on the first state-reading tool call. Container
// env skips adoption entirely — container bootstrap is explicit.
func New(ctx context.Context, client platform.Client, authInfo *auth.Info, store knowledge.Provider, logFetcher platform.LogFetcher, sshDeployer ops.SSHDeployer, mounter ops.Mounter, rtInfo runtime.Info) *Server {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel()}))

	// stateDir resolution mirrors registerTools(); kept local here so the
	// MCP init payload can include a state hint without depending on tool
	// registration. Empty stateDir (no cwd) yields empty hints — same
	// degradation path as a project that has no .zcp state yet.
	stateDir := ""
	if cwd, err := os.Getwd(); err == nil {
		stateDir = filepath.Join(cwd, ".zcp", "state")
	}

	adoptionNote := ""
	if !rtInfo.InContainer && stateDir != "" {
		adoptionNote = runLocalAutoAdopt(ctx, client, authInfo.ProjectID, stateDir, logger)
	}

	// Idempotently refresh AGENTS.md (canonical body) and CLAUDE.md
	// (thin @AGENTS.md wrapper) from the embedded templates if the
	// on-disk managed sections drifted from this build's version.
	// Long-lived installs (container OR local) otherwise hold the
	// snapshot from the last manual `zcp init`, which can be days old
	// and carry wording the current description-drift lint would
	// refuse (G9). First-install (no files present) is left for
	// `zcp init` — this is incremental refresh only.
	if stateDir != "" {
		projectRoot := filepath.Dir(filepath.Dir(stateDir))
		agentsPath := filepath.Join(projectRoot, "AGENTS.md")
		claudePath := filepath.Join(projectRoot, "CLAUDE.md")
		// Guided is a local per-project preference (.zcp marker) — read it so
		// the refresh keeps the guided block a `zcp init --guided` wrote,
		// instead of regenerating without it.
		if agentsChanged, claudeChanged, err := content.RefreshAgentContext(agentsPath, claudePath, rtInfo, content.GuidedEnabled(projectRoot)); err != nil {
			logger.Warn("agent context refresh failed", "agents", agentsPath, "claude", claudePath, "err", err)
		} else {
			if agentsChanged {
				logger.Info("AGENTS.md refreshed from embedded template", "path", agentsPath)
			}
			if claudeChanged {
				logger.Info("CLAUDE.md wrapper refreshed from embedded template", "path", claudePath)
			}
		}
	}

	rc := RuntimeContext{
		AdoptionNote: adoptionNote,
		StateHint:    ComposeStateHint(stateDir, os.Getpid()),
	}

	srv := mcp.NewServer(
		&mcp.Implementation{Name: "zcp", Version: Version},
		&mcp.ServerOptions{
			Instructions: BuildInstructions(rc),
			Logger:       logger,
		},
	)

	s := &Server{
		server:      srv,
		client:      client,
		authInfo:    authInfo,
		store:       store,
		logFetcher:  logFetcher,
		sshDeployer: sshDeployer,
		mounter:     mounter,
		rtInfo:      rtInfo,
		logger:      logger,
	}

	srv.AddReceivingMiddleware(s.observe())
	s.registerTools() //nolint:contextcheck // registerTools wires a lazy background schema provider (schemaCache.Get(context.Background())); no request context applies at startup wiring time
	return s
}

func (s *Server) registerTools() {
	projectID := s.authInfo.ProjectID
	// Host-derive the schema fetch from the resolved ZCP_API_HOST (authInfo is
	// non-nil by construction — ProjectID is dereferenced just above), so recipe
	// base-validation + workflow recipe-plan validation (which share this cache
	// via SetSchemaProvider/RegisterWorkflow) validate against the instance the
	// user actually deploys to. Empty host → CanonicalAPIHost via URLs.
	schemaCache := schema.NewCache(schema.DefaultCacheTTL, s.authInfo.APIHost, s.client.ActiveServiceTypeVersions)

	// Workflow engine: state at .zcp/state/ relative to working directory.
	var (
		wfEngine *workflow.Engine
		stateDir string
	)
	if cwd, err := os.Getwd(); err == nil {
		stateDir = filepath.Join(cwd, ".zcp", "state")
		env := workflow.DetectEnvironment(s.rtInfo)
		wfEngine = workflow.NewEngine(stateDir, env, s.store)
	}

	// Knowledge tracker shared between knowledge and workflow tools.
	knowledgeTracker := ops.NewKnowledgeTracker()

	// THE AUTHORING GATE (docs/spec-authoring-boundary.md): the
	// maintainer-only authoring domain (recipe engine v3 — and any future
	// authoring tool) registers ONLY when ZCP_AUTHORING=1. End users never
	// see the tools or pay their context cost. The gate reads the SINGLE
	// owner s.rtInfo.Authoring (resolved once by runtime.Detect), so this
	// tool-registration switch and the emitted agent-context renderer
	// (content.BuildAgentsMD, gated on the same rt.Authoring) cannot drift.
	// recipeProbe is the C1 cross-boundary contract: v2-shaped tools
	// (record_fact, workspace_manifest, import, mount, deploys) accept an
	// active recipe session as their workflow context through the
	// nil-tolerant tools.RecipeSessionProbe interface — gate off ⇒ untyped
	// nil ⇒ the guards behave exactly as "no recipe session", which is also
	// the only state an end user can be in.
	var recipeProbe tools.RecipeSessionProbe
	if s.rtInfo.Authoring {
		// Mount root defaults to ~/recipes; override with
		// ZCP_RECIPE_MOUNT_ROOT. See docs/zcprecipator3/plan.md §6.
		mountRoot := os.Getenv("ZCP_RECIPE_MOUNT_ROOT")
		if mountRoot == "" {
			if home, err := os.UserHomeDir(); err == nil {
				mountRoot = filepath.Join(home, "recipes")
			}
		}
		recipeStore := recipe.NewStore(mountRoot, Version)
		// Wire the live schema cache into recipe gates: the zerops-yaml gate
		// validates build/run base existence against the current (≤15-min) enums,
		// so a brand-new platform base is not false-rejected during recipe
		// authoring. The cache is embedded-seeded (never nil) + poison-guarded.
		// Background context is correct here: the provider is invoked lazily at
		// session-open time (not during this startup call), so no request context
		// governs it; the cache fetch imposes its own bounded timeout.
		recipeStore.SetSchemaProvider(func() *schema.Schemas {
			return schemaCache.Get(context.Background())
		})
		// zcprecipator3 (v3) recipe engine.
		recipe.Register(s.server, recipeStore)
		recipeProbe = recipeStore
		// OSS port flow (zerops_port) — same gate, same audience. Its
		// per-PID state lives in the authoring-owned .zcp/state/port/
		// namespace (boundary contract C3); the schema provider is the
		// same lazy background closure the recipe store gets (C2).
		port.Register(s.server, port.Deps{
			Schemas: func() *schema.Schemas {
				return schemaCache.Get(context.Background())
			},
			StateDir:    stateDir,
			ProjectID:   projectID,
			Environment: string(workflow.DetectEnvironment(s.rtInfo)),
		})
	}

	// Shared HTTP client for readiness probes (post-deploy subdomain
	// auto-enable, post-subdomain L7 warmup). 15 s ceiling matches the
	// per-tool maximum; individual readiness waits impose their own tight
	// request-level timeouts on top. Constructed before workflow registration
	// so action="record-deploy" can plumb it through to maybeAutoEnableSubdomain
	// (deploy-decomp Phase 7).
	httpClient := &http.Client{Timeout: 15 * time.Second}

	// Read-only tools
	tools.RegisterWorkflow(s.server, s.client, httpClient, projectID, schemaCache, wfEngine, s.logFetcher, stateDir, s.rtInfo.ServiceName, s.mounter, s.sshDeployer, s.rtInfo, s.authInfo.APIHost)
	tools.RegisterDiscover(s.server, s.client, projectID, stateDir)
	tools.RegisterKnowledge(s.server, s.store, s.client, schemaCache, knowledgeTracker, wfEngine)
	tools.RegisterRecordFact(s.server, wfEngine, recipeProbe)
	tools.RegisterWorkspaceManifest(s.server, wfEngine, recipeProbe)
	tools.RegisterLogs(s.server, s.client, s.logFetcher, projectID)
	tools.RegisterEvents(s.server, s.client, s.logFetcher, projectID)
	tools.RegisterProcess(s.server, s.client)
	tools.RegisterVerify(s.server, s.client, s.logFetcher, projectID, stateDir)
	tools.RegisterPreprocess(s.server)

	// Mutating tools — deploy registration routes by environment.
	// recipeProbe wires the recipe-authoring exemption into requireAdoption:
	// a recipe session whose Plan owns the deploy target satisfies the
	// adoption gate so cross-deploys (e.g. `apidev → apistage`) succeed
	// before any bootstrap workflow runs. Nil outside authoring mode.
	if s.sshDeployer != nil {
		tools.RegisterDeploySSH(s.server, s.client, httpClient, projectID, s.sshDeployer, s.authInfo, s.logFetcher, s.rtInfo, stateDir, wfEngine, recipeProbe)
		// v8.94: batch-deploy keeps multi-target parallelism server-side
		// so the MCP STDIO channel isn't saturated (v23 "Not connected"
		// failure class). SSH-only — local deploys don't face the same
		// parallelism problem.
		tools.RegisterDeployBatch(s.server, s.client, httpClient, projectID, s.sshDeployer, s.authInfo, s.logFetcher, s.rtInfo, stateDir, wfEngine, recipeProbe)
		// dev_server depends on the SSH deployer — it's the lifecycle
		// primitive for background dev servers on target containers.
		// Skipped in local-only mode where SSH to Zerops siblings is
		// not available.
		tools.RegisterDevServer(s.server, s.client, projectID, s.sshDeployer)
	} else {
		tools.RegisterDeployLocal(s.server, s.client, httpClient, projectID, s.authInfo, s.logFetcher, stateDir, wfEngine, recipeProbe)
	}
	tools.RegisterExport(s.server, s.client, projectID)
	tools.RegisterManage(s.server, s.client, projectID)
	tools.RegisterScale(s.server, s.client, projectID)
	tools.RegisterEnv(s.server, s.client, projectID, s.rtInfo.ServiceName)

	tools.RegisterImport(s.server, s.client, projectID, wfEngine, stateDir, recipeProbe)
	tools.RegisterDelete(s.server, s.client, projectID, stateDir, s.mounter, s.rtInfo)
	tools.RegisterSubdomain(s.server, s.client, httpClient, projectID, stateDir)
	tools.RegisterMount(s.server, s.client, projectID, s.mounter, s.rtInfo, stateDir, wfEngine, recipeProbe)

	// Container-only: zerops_browser wraps agent-browser with a guaranteed
	// open→work→close lifecycle. agent-browser is pre-installed in the ZCP
	// container but absent from local dev machines, so the tool is gated on
	// both container detection AND binary presence on PATH.
	if s.rtInfo.InContainer && ops.AgentBrowserAvailable() {
		tools.RegisterBrowser(s.server)
	}
}

// Run starts the MCP server, framing JSON-RPC over the supplied writer
// (the real stdout captured before cmd/zcp repointed os.Stdout at stderr).
//
// We use an explicit IOTransport rather than mcp.StdioTransport because the
// latter reads the live os.Stdout at Connect time — and the serve path has
// already repointed os.Stdout to stderr so a stray dependency write (the
// zerops-go SDK's fmt.Println on transport errors) cannot corrupt the
// protocol. mcpStdout carries the real fd 1; everything else goes to stderr.
func (s *Server) Run(ctx context.Context, mcpStdout io.Writer) error {
	return s.server.Run(ctx, mcpTransport(mcpStdout))
}

// mcpTransport builds the stdio transport: stdin in, the supplied writer out.
// The writer is wrapped in a no-op closer (mirroring the SDK's own
// nopCloserWriter) so the SDK's connection teardown does not close fd 1.
func mcpTransport(mcpStdout io.Writer) *mcp.IOTransport {
	return &mcp.IOTransport{Reader: os.Stdin, Writer: nopWriteCloser{mcpStdout}}
}

// nopWriteCloser is an io.WriteCloser whose Close is a no-op, used to hand
// the real stdout to the MCP transport without surrendering its lifecycle.
type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

// MCPServer returns the underlying MCP server (for testing).
func (s *Server) MCPServer() *mcp.Server {
	return s.server
}

const methodCallTool = "tools/call"

// observe returns middleware that counts tool calls and logs timing at Info level.
func (s *Server) observe() mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method != methodCallTool {
				return next(ctx, method, req)
			}
			s.calls.Add(1)
			start := time.Now()
			result, err := next(ctx, method, req)
			s.logger.Info("tool call", "ms", time.Since(start).Milliseconds())
			return result, err
		}
	}
}

// runLocalAutoAdopt performs the eager local-env state bootstrap and
// always returns a current-state note for MCP instructions:
//
//   - Empty state dir → run LocalAutoAdopt, then format the just-written
//     state.
//   - Already-adopted state → re-list metas and re-format. Pre-Phase-10
//     this returned "" on the second call; agents joining an existing
//     adopted project never saw the actionable hint and could still
//     bounce off the local-only-with-runtimes case without the
//     adopt-local prompt.
//
// Errors are logged but non-fatal — we'd rather start the server with a
// missing note than fail startup on a transient API hiccup.
func runLocalAutoAdopt(ctx context.Context, client platform.Client, projectID, stateDir string, logger *slog.Logger) string {
	metas, err := workflow.ListServiceMetas(stateDir)
	if err != nil {
		logger.Warn("auto-adopt: list metas failed", "err", err)
		return ""
	}
	if len(metas) == 0 {
		if _, adoptErr := workflow.LocalAutoAdopt(ctx, client, projectID, stateDir); adoptErr != nil {
			logger.Warn("auto-adopt: adoption failed", "err", adoptErr)
			return ""
		}
		metas, err = workflow.ListServiceMetas(stateDir)
		if err != nil {
			logger.Warn("auto-adopt: re-list metas failed", "err", err)
			return ""
		}
	}

	projectName := ""
	if project, projectErr := client.GetProject(ctx, projectID); projectErr == nil && project != nil {
		projectName = project.Name
	} else if projectErr != nil {
		logger.Warn("auto-adopt: get project failed", "err", projectErr)
	}

	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		logger.Warn("auto-adopt: list services failed", "err", err)
		return workflow.FormatLocalStateNote(metas, nil, projectName)
	}
	return workflow.FormatLocalStateNote(metas, services, projectName)
}

// logLevel returns the slog level from ZCP_LOG_LEVEL env var (default: debug).
func logLevel() slog.Level {
	switch strings.ToLower(os.Getenv("ZCP_LOG_LEVEL")) {
	case "warn":
		return slog.LevelWarn
	case "info":
		return slog.LevelInfo
	case "error":
		return slog.LevelError
	default:
		return slog.LevelDebug
	}
}
