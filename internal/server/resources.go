package server

import (
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const resourceURIPrefix = "zerops://docs/"

func (s *Server) registerResources() {
	s.server.AddResourceTemplate(
		&mcp.ResourceTemplate{
			URITemplate: "zerops://docs/{+path}",
			Name:        "zerops-docs",
			Description: "Zerops knowledge base documents. Use zerops_knowledge tool to search, then read specific docs via this resource.",
			MIMEType:    "text/markdown",
		},
		func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			uri := req.Params.URI
			if !strings.HasPrefix(uri, resourceURIPrefix) {
				return nil, mcp.ResourceNotFoundError(uri)
			}

			doc, err := s.store.Get(uri)
			if err != nil {
				return nil, mcp.ResourceNotFoundError(uri)
			}

			return &mcp.ReadResourceResult{
				Contents: []*mcp.ResourceContents{{
					URI:      uri,
					MIMEType: "text/markdown",
					Text:     doc.Content,
				}},
			}, nil
		},
	)
}
