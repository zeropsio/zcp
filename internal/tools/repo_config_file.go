package tools

import (
	"context"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/workflow"
)

// configFileName matches the repo config file under either name it may have,
// never a longer name that ends in one (notzerops.yml).
var configFileName = regexp.MustCompile(`\bzerops\.ya?ml\b`)

// nameRepoConfigFile returns text with every mention of the repo's config
// file named as the repo keeps it (file is "zerops.yaml" or "zerops.yml";
// "" leaves text as it is). A GitHub Actions workflow file
// (`.github/workflows/zerops.yml`) is a different file and keeps its name.
func nameRepoConfigFile(text, file string) string {
	if file == "" {
		return text
	}
	var out strings.Builder
	last := 0
	for _, loc := range configFileName.FindAllStringIndex(text, -1) {
		if strings.HasSuffix(text[:loc[0]], "workflows/") {
			continue
		}
		out.WriteString(text[last:loc[0]])
		out.WriteString(file)
		last = loc[1]
	}
	if last == 0 {
		return text
	}
	out.WriteString(text[last:])
	return out.String()
}

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
				copied.Text = nameRepoConfigFile(text.Text, file)
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
				env["source"] = nameRepoConfigFile(string(ops.EnvLayerYamlBaked), file)
			}
		}
	}
}
