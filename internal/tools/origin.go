package tools

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// OriginInput is zerops_origin's input. It has no token field: the person
// gives the origin's token to HQ themselves.
type OriginInput struct {
	Action string `json:"action"         jsonschema:"request"`
	URL    string `json:"url,omitempty"  jsonschema:"The repository's https address as the person gave it, without a token"`
	Repo   string `json:"repo,omitempty" jsonschema:"The repository in HQ the pair will wire: the dev service's hostname"`
	Why    string `json:"why,omitempty"  jsonschema:"One line: why finished work would go back there"`
}

type originRequested struct {
	Origin ops.OriginRequest `json:"origin"`
}

type originConnected struct {
	Origin ops.OriginRequest `json:"origin"`
	// Environments are the application's Git environments that pushed Repo.
	Environments []string `json:"environments"`
}

type originAnswer struct {
	Requested   *originRequested `json:"requested,omitempty"`
	Connected   *originConnected `json:"connected,omitempty"`
	NextActions string           `json:"nextActions"`
}

// RegisterOrigin registers zerops_origin: a Mate's agent asks the person
// whether finished work goes back to the repository it came from
// (docs/spec-mate.md §5.10). Like zerops_env action="request" it writes
// nothing; Mate draws the card and the person connects the origin in HQ.
func RegisterOrigin(srv *mcp.Server, httpClient hq.Doer, projectID string) {
	registerOrigin(srv, httpClient, hq.EnrollmentPath(), projectID)
}

func registerOrigin(srv *mcp.Server, httpClient hq.Doer, path, projectID string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_origin",
		Description: "Ask the person whether finished work goes back to the repository it came from. Writes nothing: Mate shows them a card; Connect links the origin to the repository in HQ the pair will wire (repo, the dev hostname). A message of its own tells you connected or declined. Never ask for a token in the chat.",
		Annotations: &mcp.ToolAnnotations{Title: "Ask to deliver back to the origin", ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: boolPtr(false), OpenWorldHint: boolPtr(false)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in OriginInput) (*mcp.CallToolResult, any, error) {
		if in.Action != "request" {
			return convertError(platform.NewPlatformError(platform.ErrInvalidParameter,
				fmt.Sprintf("Invalid action %q", in.Action), "Use action=\"request\".")), nil, nil
		}
		req, err := ops.NewOriginRequest(in.URL, in.Repo, in.Why)
		if err != nil {
			return convertError(err), nil, nil
		}
		client, err := hq.Open(httpClient, path)
		if err != nil {
			return observeFailure(fmt.Sprintf("HQ enrollment could not be opened, so nothing was asked: %v", err)), nil, nil
		}
		if client.ProjectID() != projectID {
			return observeFailure("HQ enrollment belongs to another project, so nothing was asked."), nil, nil
		}
		ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		state, err := client.Once().Self(ctx)
		if err != nil {
			return observeFailure(fmt.Sprintf("HQ could not be read, so whether %s already delivers to an origin is unknown and nothing was asked: %v. Ask again later.", req.Repo, err)), nil, nil
		}
		return jsonResult(originAnswerOf(req, state.GitPushes)), nil, nil
	})
}

// originAnswerOf answers a checked request from HQ's Git environment pushes:
// connected when one of them pushed the asked repository, else asked.
func originAnswerOf(req ops.OriginRequest, pushes []hq.GitPush) originAnswer {
	var environments []string
	for _, push := range pushes {
		if push.Repo == req.Repo && !slices.Contains(environments, push.Environment) {
			environments = append(environments, push.Environment)
		}
	}
	if len(environments) > 0 {
		return originAnswer{
			Connected: &originConnected{Origin: req, Environments: environments},
			NextActions: fmt.Sprintf("Nothing was asked: HQ's Git environments %s already push %s to its origin. Take its code from HQ, never from the origin, and never ask for a token.",
				strings.Join(environments, ", "), req.Repo),
		}
	}
	return originAnswer{
		Requested: &originRequested{Origin: req},
		NextActions: fmt.Sprintf("Now waiting on the person: Mate asks whether finished work goes back to %s, and Connect links it to %s in HQ. Tell them in one sentence why, and nothing about how they connect it. Never ask for a token in the chat. A message of its own tells you how it ended: connected or declined. End your turn.",
			req.URL, req.Repo),
	}
}
