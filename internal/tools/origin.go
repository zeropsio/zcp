package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
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
}

type originAnswer struct {
	Requested   *originRequested `json:"requested,omitempty"`
	Connected   *originConnected `json:"connected,omitempty"`
	NextActions string           `json:"nextActions"`
}

// RegisterOrigin registers zerops_origin: a Mate's agent asks the person
// where the code of a repository it came from is kept (docs/spec-mate.md
// §5.10) — on GitHub, which HQ mounts through its GitHub App and the
// checkout's origin becomes, or in Mate, HQ's git staying the origin and the
// repository only followed. Like zerops_env action="request" it writes
// nothing; Mate draws the card and the person picks in HQ.
func RegisterOrigin(srv *mcp.Server, httpClient hq.Doer, projectID, stateDir string) {
	registerOrigin(srv, httpClient, hq.EnrollmentPath(), projectID, stateDir)
}

func registerOrigin(srv *mcp.Server, httpClient hq.Doer, path, projectID, stateDir string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_origin",
		Description: "Ask the person where a repository's code is kept: on GitHub, mounted in HQ as the checkout's origin, or in Mate, only following it; one outside GitHub is only followed. Writes nothing: Mate shows a card for repo, the dev hostname the pair will wire. A message of its own tells you their pick. Never ask for a token.",
		Annotations: &mcp.ToolAnnotations{Title: "Ask where a repository's code is kept", ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: boolPtr(false), OpenWorldHint: boolPtr(false)},
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
			return observeFailure(fmt.Sprintf("HQ could not be read, so whether %s already has an origin is unknown and nothing was asked: %v. Ask again later.", req.Repo, err)), nil, nil
		}
		return jsonResult(originAnswerOf(req, state, client.Address(), stateDir)), nil, nil
	})
}

// originAnswerOf answers a checked request from the Mate's state in HQ:
// connected when the asked address already is the origin of the asked
// repository, or of another one of the application; else asked, also by an
// HQ older than repositories in that state. hqAddress and stateDir say how
// another repository's code comes in (originFromHolder).
//
// HQ's record names an origin's address and not whether HQ mounts it or only
// follows it, so a connected answer gives both ways and leaves the pick to
// the person's own message.
func originAnswerOf(req ops.OriginRequest, state hq.MateState, hqAddress, stateDir string) originAnswer {
	if holder := originHolder(req, state); holder != "" {
		connected := &originConnected{Origin: req, Repo: holder}
		if holder == req.Repo {
			return originAnswer{Connected: connected,
				NextActions: fmt.Sprintf("Nothing was asked: %s is already the origin of %s in HQ. Take its code from HQ first, and never ask for a token: %s. %s",
					req.URL, holder, originFromHQ(holder), originWays(req, "Which way the person keeps it, their own message said"))}
		}
		return originAnswer{Connected: connected,
			NextActions: fmt.Sprintf("Nothing was asked: %s is already the origin of %s in HQ, not of %s. Its code comes from that HQ repository, %s, never from the origin, and never ask for a token: %s.",
				req.URL, holder, req.Repo, holder, originFromHolder(req.Repo, holder, state, hqAddress, stateDir))}
	}
	if !ops.IsGitHubRepository(req.URL) {
		return originAnswer{
			Requested: &originRequested{Origin: req},
			NextActions: fmt.Sprintf("Now waiting on the person: Mate asks whether %[2]s follows %[1]s — a repository outside GitHub is only followed, never pushed to; the code is kept in Mate. Tell them in one sentence why, and nothing about how they pick. Never ask for a token in the chat. A message of its own tells you how it ended. Followed: take the code from HQ — %[3]s — then %[4]s; only a public one can be fetched. Declined: nothing changes. End your turn.",
				req.URL, req.Repo, originFromHQ(req.Repo), originFollowed(req)),
		}
	}
	return originAnswer{
		Requested: &originRequested{Origin: req},
		NextActions: fmt.Sprintf("Now waiting on the person: Mate asks where the code of %[2]s is kept — on GitHub at %[1]s, or in Mate, only following it. Tell them in one sentence why, and nothing about how they pick. Never ask for a token in the chat. A message of its own tells you what they picked. Either way take the code from HQ first: %[3]s. %[4]s Declined: nothing changes. End your turn.",
			req.URL, req.Repo, originFromHQ(req.Repo), originWays(req, "Which they picked, that message says")),
	}
}

// originWays is what each of the person's picks for a GitHub repository
// means for the pair's checkout, after lead; a repository outside GitHub is
// only ever followed.
func originWays(req ops.OriginRequest, lead string) string {
	if !ops.IsGitHubRepository(req.URL) {
		return "It is only followed: " + originFollowed(req) + "; only a public one can be fetched."
	}
	return fmt.Sprintf("%s. Kept on GitHub: GitHub is the checkout's origin — %s; then work on a branch of your own (`git checkout -b <branch> origin/main`), commit, `git push origin <branch>` and open its pull request with %s. Never push `main` and never force-push: `main` moves only by a merge. Kept in Mate: HQ stays the origin and GitHub is only followed — %s; a private one is read with HQ's read token for it, and when HQ holds none, say so.",
		lead, originMounted(req), originPullRequest(req), originFollowed(req))
}

// originMountedDir is the repository as the Mate's own shell mounts it: only
// that shell runs `zcp`, which git's GitHub helper is, so git that talks to
// GitHub runs there and never over ssh in the service.
func originMountedDir(repo string) string { return "/var/www/" + repo }

// originMounted makes GitHub the checkout's origin, its fetches and pushes
// answered by `zcp hq git-credential` with the token HQ mints at each call.
func originMounted(req ops.OriginRequest) string {
	return fmt.Sprintf("in your own shell, never over ssh: `cd %s && git remote set-url origin %s && %s && git fetch origin`",
		originMountedDir(req.Repo), req.URL, hq.GitHubHelperConfig)
}

// originPullRequest opens a branch's pull request with the token HQ mints for
// the repository, handed to gh for that one command.
func originPullRequest(req ops.OriginRequest) string {
	return fmt.Sprintf("`GH_TOKEN=\"$(printf 'protocol=https\\nhost=github.com\\npath=%s\\n\\n' | zcp hq git-credential get | sed -n 's/^password=//p')\" gh pr create --fill`",
		ops.GitHubRepositoryPath(req.URL))
}

// originFollowed adds the repository as a fetch-only `upstream` beside HQ's
// origin: a push to it is refused by git itself. A GitHub one is fetched
// through `zcp hq git-credential`, which answers a private one with HQ's read
// token when HQ holds one.
func originFollowed(req ops.OriginRequest) string {
	helper := ""
	if ops.IsGitHubRepository(req.URL) {
		helper = " && " + hq.GitHubHelperConfig
	}
	return fmt.Sprintf("in your own shell, never over ssh: `cd %s && git remote add upstream %s && git remote set-url --push upstream no_push%s && git fetch upstream`, and take its new work in on a branch of your own with `git merge upstream/main`",
		originMountedDir(req.Repo), req.URL, helper)
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
// repository in HQ, whose main took the origin's history. Taken onto an
// untouched repository, main is the origin's own history and shares nothing
// with HQ's seed, so a checkout holding only the seed — one commit of the
// empty tree, as the wiring cut it — moves onto main (`reset --keep` refuses
// rather than overwrite a file in the way); a checkout with work of its own
// merges main in, and git refuses by name a main its work shares no history
// with.
func originFromHQ(repo string) string {
	return fmt.Sprintf("`ssh %s 'cd <repo dir> && git fetch origin main && "+
		"if [ \"$(git rev-list --count HEAD)\" = 1 ] && [ -z \"$(git ls-tree HEAD)\" ]; "+
		"then git reset --keep FETCH_HEAD; else git merge --no-edit FETCH_HEAD; fi'`", repo)
}

// originFromHolder takes the code of holder, another repository in HQ, into
// the pair's working copy: in holder's own, when it is one of this project's
// services; else, since an HQ repository of the application can belong to a
// sibling Mate's project and name no host here, fetched from HQ into the
// asked repository's.
func originFromHolder(repo, holder string, state hq.MateState, hqAddress, stateDir string) string {
	if meta, err := workflow.FindServiceMeta(stateDir, holder); err == nil && meta != nil {
		return originFromHQ(holder)
	}
	appID := ""
	if state.AppID != nil {
		appID = *state.AppID
	}
	return fmt.Sprintf("`ssh %s \"cd <repo dir> && git fetch %s main && git merge --no-edit FETCH_HEAD\"`",
		repo, hq.RepoURLAt(hqAddress, appID, holder))
}
