package server

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
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
func New(ctx context.Context, client platform.Client, authInfo *auth.Info, store knowledge.Provider, logFetcher platform.LogFetcher, sshDeployer ops.SSHDeployer, mounter ops.Mounter, rtInfo runtime.Info) *Server {
	// Determine workflow state directory for system prompt hint.
	var stateDir string
	if cwd, err := os.Getwd(); err == nil {
		stateDir = filepath.Join(cwd, ".zcp", "state")
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel()}))

	srv := mcp.NewServer(
		&mcp.Implementation{Name: "zcp", Version: Version},
		&mcp.ServerOptions{
			Instructions: BuildInstructions(ctx, client, authInfo.ProjectID, rtInfo, stateDir),
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

	// Read-only tools
	tools.RegisterWorkflow(s.server, s.client, projectID, stackCache, schemaCache, wfEngine, s.logFetcher, stateDir, s.rtInfo.ServiceName, s.mounter)
	tools.RegisterDiscover(s.server, s.client, projectID, stateDir)
	tools.RegisterKnowledge(s.server, s.store, s.client, stackCache, knowledgeTracker, wfEngine)
	tools.RegisterGuidance(s.server, wfEngine)
	tools.RegisterLogs(s.server, s.client, s.logFetcher, projectID)
	tools.RegisterEvents(s.server, s.client, projectID)
	tools.RegisterProcess(s.server, s.client)
	tools.RegisterVerify(s.server, s.client, s.logFetcher, projectID)
	tools.RegisterPreprocess(s.server)

	// Mutating tools — deploy registration routes by environment.
	if s.sshDeployer != nil {
		tools.RegisterDeploySSH(s.server, s.client, projectID, s.sshDeployer, s.authInfo, s.logFetcher, s.rtInfo, stateDir, wfEngine)
		// dev_server depends on the SSH deployer — it's the lifecycle
		// primitive for background dev servers on target containers.
		// Skipped in local-only mode where SSH to Zerops siblings is
		// not available.
		tools.RegisterDevServer(s.server, s.client, projectID, s.sshDeployer)
	} else {
		tools.RegisterDeployLocal(s.server, s.client, projectID, s.authInfo, s.logFetcher, stateDir, wfEngine)
	}
	tools.RegisterExport(s.server, s.client, projectID)
	tools.RegisterManage(s.server, s.client, projectID)
	tools.RegisterScale(s.server, s.client, projectID)
	tools.RegisterEnv(s.server, s.client, projectID, s.rtInfo.ServiceName)
	tools.RegisterImport(s.server, s.client, projectID, stackCache, schemaCache, wfEngine, stateDir)
	tools.RegisterDelete(s.server, s.client, projectID, stateDir, s.mounter, s.rtInfo)
	tools.RegisterSubdomain(s.server, s.client, projectID)
	tools.RegisterMount(s.server, s.client, projectID, s.mounter, s.rtInfo, stateDir, wfEngine)

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

// observe returns middleware that counts tool calls and logs timing at Info level.
func (s *Server) observe() mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method != "tools/call" {
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

// logLevel returns the slog level from ZCP_LOG_LEVEL env var (default: warn).
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
