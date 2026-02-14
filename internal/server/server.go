package server

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/tools"
	"github.com/zeropsio/zcp/internal/update"
)

// Version, Commit, Built are set by ldflags at build time.
var (
	Version = "dev"
	Commit  = "unknown"
	Built   = "unknown"
)

// Server wraps the MCP server with Zerops-specific configuration.
type Server struct {
	server        *mcp.Server
	client        platform.Client
	authInfo      *auth.Info
	store         knowledge.Provider
	logFetcher    platform.LogFetcher
	sshDeployer   ops.SSHDeployer
	localDeployer ops.LocalDeployer
	mounter       ops.Mounter
	updateInfo    *update.Info
}

// New creates a new ZCP MCP server with all tools registered.
func New(client platform.Client, authInfo *auth.Info, store knowledge.Provider, logFetcher platform.LogFetcher, sshDeployer ops.SSHDeployer, localDeployer ops.LocalDeployer, mounter ops.Mounter, updateInfo *update.Info) *Server {
	srv := mcp.NewServer(
		&mcp.Implementation{Name: "zcp", Version: Version},
		&mcp.ServerOptions{Instructions: BuildInstructions()},
	)

	s := &Server{
		server:        srv,
		client:        client,
		authInfo:      authInfo,
		store:         store,
		logFetcher:    logFetcher,
		sshDeployer:   sshDeployer,
		localDeployer: localDeployer,
		mounter:       mounter,
		updateInfo:    updateInfo,
	}

	s.registerTools()
	s.registerResources()
	return s
}

func (s *Server) registerTools() {
	projectID := s.authInfo.ProjectID
	stackCache := ops.NewStackTypeCache(ops.DefaultStackTypeCacheTTL)

	// Read-only tools
	tools.RegisterContext(s.server, s.client, stackCache, s.updateInfo)
	tools.RegisterWorkflow(s.server, s.client, stackCache)
	tools.RegisterDiscover(s.server, s.client, projectID)
	tools.RegisterKnowledge(s.server, s.store, s.client, stackCache)
	tools.RegisterLogs(s.server, s.client, s.logFetcher, projectID)
	tools.RegisterEvents(s.server, s.client, projectID)
	tools.RegisterProcess(s.server, s.client)

	// Mutating tools
	if s.sshDeployer != nil || s.localDeployer != nil {
		tools.RegisterDeploy(s.server, s.client, projectID, s.sshDeployer, s.localDeployer, s.authInfo)
	}
	tools.RegisterManage(s.server, s.client, projectID)
	tools.RegisterScale(s.server, s.client, projectID)
	tools.RegisterEnv(s.server, s.client, projectID)
	tools.RegisterImport(s.server, s.client, projectID, stackCache)
	tools.RegisterDelete(s.server, s.client, projectID)
	tools.RegisterSubdomain(s.server, s.client, projectID)
	if s.mounter != nil {
		tools.RegisterMount(s.server, s.client, projectID, s.mounter)
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
