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

// MirrorInput is zerops_mirror's input. It has no token or address field: the
// person picks the GitHub account and repository in Mate's card and connects
// it through HQ themselves.
type MirrorInput struct {
	Action string `json:"action"        jsonschema:"request, or withdraw: take back an ask no longer needed (re-planned, asked by mistake)"`
	Repo   string `json:"repo"          jsonschema:"The repository in HQ to connect to GitHub, by its name in the application: the dev service's hostname (app)"`
	Why    string `json:"why,omitempty" jsonschema:"request: one short line the card shows, why the code goes to GitHub"`
}

type mirrorRequested struct {
	Mirror ops.MirrorRequest `json:"mirror"`
}

type mirrorConnected struct {
	Mirror ops.MirrorConnection `json:"mirror"`
}

// mirrorWithdrawn is an ask the agent took back (action="withdraw"): Mate
// closes the card of the ask naming Mirror.Repo.
type mirrorWithdrawn struct {
	Mirror ops.MirrorWithdrawal `json:"mirror"`
}

type mirrorAnswer struct {
	Requested   *mirrorRequested `json:"requested,omitempty"`
	Connected   *mirrorConnected `json:"connected,omitempty"`
	Withdrawn   *mirrorWithdrawn `json:"withdrawn,omitempty"`
	NextActions string           `json:"nextActions"`
}

// mirrorActionWithdraw takes back an ask the agent no longer needs.
const mirrorActionWithdraw = "withdraw"

// RegisterMirror registers zerops_mirror: a Mate's agent asks the person to
// connect one of the application's repositories in HQ to GitHub
// (docs/spec-mate.md §5.11) — the reverse of zerops_origin, whose code comes
// from GitHub. Like zerops_origin it writes nothing; Mate draws a card with the
// person's GitHub accounts and repositories, and the person connects it
// through HQ.
func RegisterMirror(srv *mcp.Server, httpClient hq.Doer, projectID string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_mirror",
		Description: "When the person wants a repository's code on GitHub — a GitHub copy, pull requests there — and it has no remote, ask them to connect it: Mate shows a card where they pick the GitHub account and repository. Writes nothing; never ask for a token or address. action=withdraw repo closes an unneeded ask — not in chat.",
		Annotations: &mcp.ToolAnnotations{Title: "Ask to connect a repository to GitHub", ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: boolPtr(false), OpenWorldHint: boolPtr(false)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in MirrorInput) (*mcp.CallToolResult, any, error) {
		if in.Action == mirrorActionWithdraw {
			return mirrorWithdraw(in), nil, nil
		}
		if in.Action != "request" {
			return convertError(platform.NewPlatformError(platform.ErrInvalidParameter,
				fmt.Sprintf("Invalid action %q", in.Action), "Use action=\"request\", or action=\"withdraw\" to take back an ask no longer needed.")), nil, nil
		}
		req, err := ops.NewMirrorRequest(in.Repo, in.Why)
		if err != nil {
			return convertError(err), nil, nil
		}
		client, err := hq.Open(httpClient, liveHQIdentity())
		if err != nil {
			return observeFailure(fmt.Sprintf("HQ could not be addressed, so nothing was asked: %v", err)), nil, nil
		}
		if client.ProjectID() != projectID {
			return observeFailure("This Mate's key belongs to another project, so nothing was asked."), nil, nil
		}
		ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		once := client.Once()
		state, err := once.Self(ctx)
		if err != nil {
			return observeFailure(fmt.Sprintf("HQ could not be read, so whether %s already has a remote is unknown and nothing was asked: %v. Ask again later.", req.Repo, err)), nil, nil
		}
		if state.AppID == nil {
			return observeFailure("HQ holds this Mate in no application yet, so it has no repository to connect and nothing was asked."), nil, nil
		}
		repos, err := once.AppRepos(ctx, *state.AppID)
		if err != nil {
			return observeFailure(fmt.Sprintf("HQ could not be read, so whether %s already has a remote is unknown and nothing was asked: %v. Ask again later.", req.Repo, err)), nil, nil
		}
		answer, err := mirrorAnswerOf(req, repos)
		if err != nil {
			return convertError(err), nil, nil
		}
		return jsonResult(answer), nil, nil
	})
}

// mirrorAnswerOf answers a checked request from the application's
// repositories in HQ: connected when the asked one already has a remote —
// nothing to ask — else asked. A repository the application does not have is
// refused, naming the ones it has.
func mirrorAnswerOf(req ops.MirrorRequest, repos []hq.AppRepo) (mirrorAnswer, error) {
	i := slices.IndexFunc(repos, func(r hq.AppRepo) bool { return r.Name == req.Repo })
	if i < 0 {
		names := make([]string, 0, len(repos))
		for _, r := range repos {
			names = append(names, r.Name)
		}
		have := "it has none yet"
		if len(names) > 0 {
			have = "name one of " + strings.Join(names, ", ")
		}
		return mirrorAnswer{}, platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("The application in HQ has no repository %q, so nothing was asked", req.Repo),
			"repo is a repository of the application in HQ, by its name: "+have+".")
	}
	if remote := repos[i].Remote; remote != nil {
		return mirrorAnswer{
			Connected: &mirrorConnected{Mirror: ops.MirrorConnection{Repo: req.Repo, URL: remote.URL}},
			NextActions: fmt.Sprintf("Nothing was asked: %s already has a remote in HQ, %s. If the person asked to connect it, tell them so in one sentence; "+
				"they change or disconnect it in Mate, never through you.", req.Repo, remote.URL),
		}, nil
	}
	return mirrorAnswer{
		Requested: &mirrorRequested{Mirror: req},
		NextActions: fmt.Sprintf("Now waiting on the person: Mate shows a card where they pick the GitHub account and repository %[1]s connects to. "+
			"Say one sentence — that you asked them to connect %[1]s to GitHub, in the card above — and nothing about how they pick; never ask for a token or a repository address in the chat. "+
			"Once connected, HQ pushes the repository to GitHub itself: never push to GitHub yourself. "+
			"Should you no longer need it — re-planned, asked by mistake — withdraw it (`zerops_mirror action=\"withdraw\" repo=%[1]q`): Mate closes its card; never say in chat that it is outdated. "+
			"End your turn.", req.Repo),
	}, nil
}

// mirrorWithdraw answers action="withdraw": the agent no longer needs the
// repository connected. It writes nothing and asks HQ nothing: Mate closes the
// card of the ask naming repo from the answer, so the agent never tells the
// person in chat that a card is outdated (docs/spec-mate.md §5.11).
func mirrorWithdraw(in MirrorInput) *mcp.CallToolResult {
	w, err := ops.NewMirrorWithdrawal(in.Repo)
	if err != nil {
		return convertError(err)
	}
	return jsonResult(mirrorAnswer{
		Withdrawn: &mirrorWithdrawn{Mirror: w},
		NextActions: fmt.Sprintf("Nothing was written: the ask to connect %s to GitHub is withdrawn — Mate closes its card, so there is nothing more to say about it in chat. "+
			"Ask again with action=\"request\" only if it should be connected after all.", w.Repo),
	})
}
