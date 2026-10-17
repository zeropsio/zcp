package tools

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
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
	// Repo is the repository in HQ whose origin it is: Origin.Repo, or another
	// repository of the application already holding it.
	Repo string `json:"repo"`
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
		return jsonResult(originAnswerOf(req, state)), nil, nil
	})
}

// originAnswerOf answers a checked request from the Mate's state in HQ:
// connected when the asked address already is the origin of the asked
// repository, or of another one of the application; else asked, also by an
// HQ older than repositories in that state.
func originAnswerOf(req ops.OriginRequest, state hq.MateState) originAnswer {
	if holder := originHolder(req, state); holder != "" {
		environments := []string{}
		for _, push := range state.GitPushes {
			if push.Repo == holder && !slices.Contains(environments, push.Environment) {
				environments = append(environments, push.Environment)
			}
		}
		connected := &originConnected{Origin: req, Repo: holder, Environments: environments}
		if holder == req.Repo {
			return originAnswer{Connected: connected,
				NextActions: fmt.Sprintf("Nothing was asked: %s is already the origin of %s in HQ. Take its code from HQ, never from the origin, and never ask for a token: %s.",
					req.URL, holder, originFromHQ(holder))}
		}
		return originAnswer{Connected: connected,
			NextActions: fmt.Sprintf("Nothing was asked: %s is already the origin of %s in HQ, not of %s. Its code comes from that HQ repository, %s, never from the origin, and never ask for a token: %s.",
				req.URL, holder, req.Repo, holder, originFromHQ(holder))}
	}
	return originAnswer{
		Requested: &originRequested{Origin: req},
		NextActions: fmt.Sprintf("Now waiting on the person: Mate asks whether finished work goes back to %[1]s, and Connect links it to %[2]s in HQ. Tell them in one sentence why, and nothing about how they connect it. Never ask for a token in the chat. A message of its own tells you how it ended. Connected: take the code from HQ — %[3]s. Declined: a public repository only — `ssh %[2]s \"cd <repo dir> && GIT_TERMINAL_PROMPT=0 git fetch %[1]s HEAD && git merge --allow-unrelated-histories --no-edit FETCH_HEAD\"`; a private one cannot be fetched without its origin connected, so say so and ask again. End your turn.",
			req.URL, req.Repo, originFromHQ(req.Repo)),
	}
}

// originHolder is the repository in HQ the asked address already is the
// origin of: the asked repository first, else the first other one. "" for
// none.
func originHolder(req ops.OriginRequest, state hq.MateState) string {
	holder := ""
	for _, repo := range state.Repos {
		if repo.Origin == nil || !topology.SameRepoURL(repo.Origin.URL, req.URL) {
			continue
		}
		if repo.Name == req.Repo {
			return repo.Name
		}
		if holder == "" {
			holder = repo.Name
		}
	}
	return holder
}

// originFromHQ takes the origin's code into the pair's working copy from the
// repository in HQ, whose main holds the origin's adopted history.
func originFromHQ(repo string) string {
	return fmt.Sprintf("`ssh %s \"cd <repo dir> && git fetch origin main && git merge --no-edit FETCH_HEAD\"`", repo)
}
