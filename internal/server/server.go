package server

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
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
}

// New creates a new ZCP MCP server with all tools registered.
func New(ctx context.Context, client platform.Client, authInfo *auth.Info, store knowledge.Provider, logFetcher platform.LogFetcher, sshDeployer ops.SSHDeployer, mounter ops.Mounter, rtInfo runtime.Info) *Server {
	// Determine workflow state directory for system prompt hint.
	var stateDir string
	if cwd, err := os.Getwd(); err == nil {
		stateDir = filepath.Join(cwd, ".zcp", "state")
	}

	srv := mcp.NewServer(
		&mcp.Implementation{Name: "zcp", Version: Version},
		&mcp.ServerOptions{
			Instructions: BuildInstructions(ctx, client, authInfo.ProjectID, rtInfo, stateDir),
			KeepAlive:    30 * time.Second,
			Logger:       slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
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
	}

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
	tools.RegisterLogs(s.server, s.client, s.logFetcher, projectID)
	tools.RegisterEvents(s.server, s.client, projectID)
	tools.RegisterProcess(s.server, s.client)
	tools.RegisterVerify(s.server, s.client, s.logFetcher, projectID)
	tools.RegisterPreprocess(s.server)

	// Mutating tools — deploy registration routes by environment.
	if s.sshDeployer != nil {
		tools.RegisterDeploySSH(s.server, s.client, projectID, s.sshDeployer, s.authInfo, s.logFetcher, s.rtInfo, stateDir)
	} else {
		tools.RegisterDeployLocal(s.server, s.client, projectID, s.authInfo, s.logFetcher, stateDir)
	}
	tools.RegisterExport(s.server, s.client, projectID)
	tools.RegisterManage(s.server, s.client, projectID)
	tools.RegisterScale(s.server, s.client, projectID)
	tools.RegisterEnv(s.server, s.client, projectID, s.rtInfo.ServiceName)
	tools.RegisterImport(s.server, s.client, projectID, stackCache, schemaCache, wfEngine)
	tools.RegisterDelete(s.server, s.client, projectID, stateDir, s.mounter, s.rtInfo)
	tools.RegisterSubdomain(s.server, s.client, projectID)
	tools.RegisterMount(s.server, s.client, projectID, s.mounter, s.rtInfo, wfEngine)
}

// Run starts the MCP server on stdio transport.
func (s *Server) Run(ctx context.Context) error {
	return s.server.Run(ctx, &mcp.StdioTransport{})
}

// MCPServer returns the underlying MCP server (for testing).
func (s *Server) MCPServer() *mcp.Server {
	return s.server
}
