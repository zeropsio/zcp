package captureinspector

import (
	"context"

	"github.com/zeropsio/zcp/internal/captureinspector/internal/web"
)

// Config contains the explicit inputs required to start the local inspector.
// Constructing Config and importing this package have no runtime side effects.
type Config struct {
	ListenAddr      string
	CaptureRoot     string
	SessionDir      string
	CapabilityToken string
	RevealToken     string
}

// Server is the narrow lifecycle surface exposed to the CLI adapter.
type Server struct {
	web *web.Server
}

// Start creates the inspector's loopback listener and HTTP service. It is the
// domain's only runtime activation point and must be called explicitly by the
// capture UI CLI path.
func Start(ctx context.Context, config Config) (*Server, error) {
	server, err := web.Start(ctx, web.Config{
		ListenAddr:      config.ListenAddr,
		CaptureRoot:     config.CaptureRoot,
		SessionDir:      config.SessionDir,
		CapabilityToken: config.CapabilityToken,
		RevealToken:     config.RevealToken,
	})
	if err != nil {
		return nil, err
	}
	return &Server{web: server}, nil
}

// URL returns the authenticated service's loopback origin without a capability.
func (server *Server) URL() string { return server.web.URL() }

// LaunchURL returns the one-time capability launch URL.
func (server *Server) LaunchURL() string { return server.web.LaunchURL() }

// Close shuts down only the inspector HTTP service.
func (server *Server) Close(ctx context.Context) error { return server.web.Close(ctx) }
