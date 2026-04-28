package server

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/content"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/recipe"
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

	// Container env: idempotently refresh CLAUDE.md from the embedded
	// template if the on-disk managed section drifted from this build's
	// version. Long-lived containers otherwise hold the snapshot from
	// the last manual `zcp init`, which can be days old and carry
	// wording the current description-drift lint would refuse (G9).
	// First-install (no file present) is left for `zcp init` — this is
	// incremental refresh only.
	if rtInfo.InContainer && stateDir != "" {
		claudemdPath := filepath.Join(filepath.Dir(filepath.Dir(stateDir)), "CLAUDE.md")
		if refreshed, err := content.RefreshClaudeMD(claudemdPath, rtInfo); err != nil {
			logger.Warn("CLAUDE.md refresh failed", "path", claudemdPath, "err", err)
		} else if refreshed {
			logger.Info("CLAUDE.md refreshed from embedded template", "path", claudemdPath)
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
	s.registerTools()
	s.registerResources()
	return s
}

func (s *Server) registerTools() {
	projectID := s.authInfo.ProjectID
	stackCache := ops.NewStackTypeCache(ops.DefaultStackTypeCacheTTL)
	schemaCache := schema.NewCache(schema.DefaultCacheTTL)

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

	// recipeStore is created early so v2-shaped tools (record_fact,
	// workspace_manifest, import, mount) can accept an active v3 recipe
	// session as their workflow context. Mount root defaults to ~/recipes;
	// override with ZCP_RECIPE_MOUNT_ROOT. See docs/zcprecipator3/plan.md §6.
	mountRoot := os.Getenv("ZCP_RECIPE_MOUNT_ROOT")
	if mountRoot == "" {
		if home, err := os.UserHomeDir(); err == nil {
			mountRoot = filepath.Join(home, "recipes")
		}
	}
	recipeStore := recipe.NewStore(mountRoot)

	// Shared HTTP client for readiness probes (post-deploy subdomain
	// auto-enable, post-subdomain L7 warmup). 15 s ceiling matches the
	// per-tool maximum; individual readiness waits impose their own tight
	// request-level timeouts on top. Constructed before workflow registration
	// so action="record-deploy" can plumb it through to maybeAutoEnableSubdomain
	// (deploy-decomp Phase 7).
	httpClient := &http.Client{Timeout: 15 * time.Second}

	// Read-only tools
	tools.RegisterWorkflow(s.server, s.client, httpClient, projectID, stackCache, schemaCache, wfEngine, s.logFetcher, stateDir, s.rtInfo.ServiceName, s.mounter, s.sshDeployer, s.rtInfo)
	tools.RegisterDiscover(s.server, s.client, projectID, stateDir)
	tools.RegisterKnowledge(s.server, s.store, s.client, stackCache, knowledgeTracker, wfEngine)
	tools.RegisterGuidance(s.server, wfEngine)
	tools.RegisterRecordFact(s.server, wfEngine, recipeStore)
	tools.RegisterWorkspaceManifest(s.server, wfEngine, recipeStore)
	tools.RegisterLogs(s.server, s.client, s.logFetcher, projectID)
	tools.RegisterEvents(s.server, s.client, projectID)
	tools.RegisterProcess(s.server, s.client)
	tools.RegisterVerify(s.server, s.client, s.logFetcher, projectID, stateDir)
	tools.RegisterPreprocess(s.server)

	// Mutating tools — deploy registration routes by environment.
	if s.sshDeployer != nil {
		tools.RegisterDeploySSH(s.server, s.client, httpClient, projectID, s.sshDeployer, s.authInfo, s.logFetcher, s.rtInfo, stateDir, wfEngine)
		// v8.94: batch-deploy keeps multi-target parallelism server-side
		// so the MCP STDIO channel isn't saturated (v23 "Not connected"
		// failure class). SSH-only — local deploys don't face the same
		// parallelism problem.
		tools.RegisterDeployBatch(s.server, s.client, httpClient, projectID, s.sshDeployer, s.authInfo, s.logFetcher, stateDir, wfEngine)
		// dev_server depends on the SSH deployer — it's the lifecycle
		// primitive for background dev servers on target containers.
		// Skipped in local-only mode where SSH to Zerops siblings is
		// not available.
		tools.RegisterDevServer(s.server, s.client, projectID, s.sshDeployer)
	} else {
		tools.RegisterDeployLocal(s.server, s.client, httpClient, projectID, s.authInfo, s.logFetcher, stateDir, wfEngine)
	}
	tools.RegisterExport(s.server, s.client, projectID)
	tools.RegisterManage(s.server, s.client, projectID)
	tools.RegisterScale(s.server, s.client, projectID)
	tools.RegisterEnv(s.server, s.client, projectID, s.rtInfo.ServiceName)

	// zcprecipator3 (v3) recipe engine ships alongside v2's zerops_workflow.
	// Both tools register; clients pick which to call. v2 deletion triggers
	// on first clean showcase via v3 — see docs/zcprecipator3/plan.md §14.
	// recipeStore was constructed above so v2-shaped tools can accept a
	// recipe session as their workflow context.
	recipe.Register(s.server, recipeStore)

	tools.RegisterImport(s.server, s.client, projectID, wfEngine, stateDir, recipeStore)
	tools.RegisterDelete(s.server, s.client, projectID, stateDir, s.mounter, s.rtInfo)
	tools.RegisterSubdomain(s.server, s.client, httpClient, projectID)
	tools.RegisterMount(s.server, s.client, projectID, s.mounter, s.rtInfo, stateDir, wfEngine, recipeStore)

	// Container-only: zerops_browser wraps agent-browser with a guaranteed
	// open→work→close lifecycle. agent-browser is pre-installed in the ZCP
	// container but absent from local dev machines, so the tool is gated on
	// both container detection AND binary presence on PATH.
	if s.rtInfo.InContainer && ops.AgentBrowserAvailable() {
		tools.RegisterBrowser(s.server)
	}
}

// Run starts the MCP server on stdio transport.
func (s *Server) Run(ctx context.Context) error {
	return s.server.Run(ctx, &mcp.StdioTransport{})
}

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

// runLocalAutoAdopt performs the eager local-env state bootstrap:
// legacy-meta migration first (so existing installs get their meta
// rewritten to the new shape), then auto-adoption if state is empty.
// Returns the formatted adoption note for MCP instructions (empty when
// nothing was adopted or an error prevented adoption).
//
// Errors are logged but non-fatal — we'd rather start the server with a
// missing note than fail startup on a transient API hiccup. The missing
// note just means the LLM doesn't know the project was auto-adopted;
// its first tool call will compute the envelope fresh.
func runLocalAutoAdopt(ctx context.Context, client platform.Client, projectID, stateDir string, logger *slog.Logger) string {
	existing, err := workflow.ListServiceMetas(stateDir)
	if err != nil {
		logger.Warn("auto-adopt: list metas failed", "err", err)
		return ""
	}
	if len(existing) > 0 {
		return ""
	}
	result, err := workflow.LocalAutoAdopt(ctx, client, projectID, stateDir)
	if err != nil {
		logger.Warn("auto-adopt: adoption failed", "err", err)
		return ""
	}
	return workflow.FormatAdoptionNote(result)
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
