package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/workflow"
)

// namingRepoConfigFile wraps a guidance tool's handler so every text it
// returns names the repo's config file as the repo keeps it — the name the
// latest repo read recorded on the engine (attachPartsSignal). Guidance and
// knowledge are written against one generic name; an agent told a name its
// repo does not use renames the file. The result is copied, never mutated:
// a cached knowledge result stays as the store wrote it. The typed output
// stays nil, as every tool's does (TestNoStructuredContentOnToolResults).
func namingRepoConfigFile[In any](engine *workflow.Engine, handler mcp.ToolHandlerFor[In, any]) mcp.ToolHandlerFor[In, any] {
	return func(ctx context.Context, req *mcp.CallToolRequest, input In) (*mcp.CallToolResult, any, error) {
		result, _, err := handler(ctx, req, input)
		file := engine.RepoConfigFile()
		if result == nil || file == "" {
			return result, nil, err
		}
		named := *result
		named.Content = make([]mcp.Content, len(result.Content))
		for i, c := range result.Content {
			if text, ok := c.(*mcp.TextContent); ok {
				copied := *text
				copied.Text = ops.NameRepoConfigFile(text.Text, file)
				c = &copied
			}
			named.Content[i] = c
		}
		return &named, nil, err
	}
}

// nameYamlBakedSource tags each yaml-baked env var of a discover result as
// coming from the repo's config file as the repo names it ("" leaves the
// tag as it is). Only the tag is renamed: env values are the platform's
// data and stay as read.
func nameYamlBakedSource(result *ops.DiscoverResult, file string) {
	for _, svc := range result.Services {
		for _, env := range svc.Envs {
			if env["source"] == string(ops.EnvLayerYamlBaked) {
				env["source"] = ops.NameRepoConfigFile(string(ops.EnvLayerYamlBaked), file)
			}
		}
	}
}
