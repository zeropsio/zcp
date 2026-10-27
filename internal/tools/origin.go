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
	Action string `json:"action"         jsonschema:"request, or withdraw: take back an ask no longer needed (re-planned, repository retired, asked by mistake)"`
	URL    string `json:"url,omitempty"  jsonschema:"The repository's https address as the person gave it (withdraw: as it was asked), without a token"`
	Repo   string `json:"repo,omitempty" jsonschema:"The repository in HQ the pair will wire: the dev service's hostname. withdraw: the repository the ask named; omit to withdraw every ask naming the address"`
	Why    string `json:"why,omitempty"  jsonschema:"request: one line, why finished work would go back there"`
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

// originWithdrawn is an ask the agent took back (action="withdraw"): Mate
// closes the card of every ask naming Origin.URL — of Origin.Repo alone when
// it names one.
type originWithdrawn struct {
	Origin ops.OriginWithdrawal `json:"origin"`
}

type originAnswer struct {
	Requested   *originRequested `json:"requested,omitempty"`
	Connected   *originConnected `json:"connected,omitempty"`
	Withdrawn   *originWithdrawn `json:"withdrawn,omitempty"`
	NextActions string           `json:"nextActions"`
}

// RegisterOrigin registers zerops_origin: a Mate's agent asks the person
// where the code of a repository it came from is kept (docs/spec-mate.md
// §5.10) — on GitHub, which HQ mounts through its GitHub App and the
// checkout's origin becomes, or in Mate, HQ's git staying the origin and the
// repository only followed. Like zerops_env action="request" it writes
// nothing; Mate draws the card and the person picks in HQ.
func RegisterOrigin(srv *mcp.Server, httpClient hq.Doer, projectID, orgID, stateDir string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_origin",
		Description: "Ask the person where a repository's code is kept: on GitHub, mounted in HQ as its origin, or in Mate, only following it. Writes nothing: Mate shows a card for repo, the pair's dev hostname; a message tells you their pick. Never ask for a token. action=withdraw url (repo) closes an ask no longer needed — not in chat.",
		Annotations: &mcp.ToolAnnotations{Title: "Ask where a repository's code is kept", ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: boolPtr(false), OpenWorldHint: boolPtr(false)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in OriginInput) (*mcp.CallToolResult, any, error) {
		if in.Action == originActionWithdraw {
			return originWithdraw(in), nil, nil
		}
		if in.Action != "request" {
			return convertError(platform.NewPlatformError(platform.ErrInvalidParameter,
				fmt.Sprintf("Invalid action %q", in.Action), "Use action=\"request\", or action=\"withdraw\" to take back an ask no longer needed.")), nil, nil
		}
		req, err := ops.NewOriginRequest(in.URL, in.Repo, in.Why)
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
		state, err := client.Once().Self(ctx)
		if err != nil {
			return observeFailure(fmt.Sprintf("HQ could not be read, so whether %s already has an origin is unknown and nothing was asked: %v. Ask again later.", req.Repo, err)), nil, nil
		}
		return jsonResult(originAnswerOf(req, state, client.Address(), orgID, stateDir)), nil, nil
	})
}

// originActionWithdraw takes back an ask the agent no longer needs.
const originActionWithdraw = "withdraw"

// originWithdraw answers action="withdraw": the agent no longer needs the
// person's pick of where a repository's code is kept — it re-planned, the
// repository was retired, or it asked by mistake. It writes nothing and asks
// HQ nothing: Mate closes the card of every ask naming the address — the one
// of repo alone when repo is named — from the answer, so the agent never
// tells the person in chat that a card is outdated (docs/spec-mate.md §5.10).
func originWithdraw(in OriginInput) *mcp.CallToolResult {
	w, err := ops.NewOriginWithdrawal(in.URL, in.Repo)
	if err != nil {
		return convertError(err)
	}
	which := "every ask naming " + w.URL + " is withdrawn"
	if w.Repo != "" {
		which = fmt.Sprintf("the ask of where %s's code is kept, at %s, is withdrawn", w.Repo, w.URL)
	}
	return jsonResult(originAnswer{
		Withdrawn: &originWithdrawn{Origin: w},
		NextActions: "Nothing was written: " + which + " — Mate closes its card, so there is nothing more to say about it in chat. " +
			"If the person already picked, a message of its own still tells you; ask again with action=\"request\" only if you need their pick after all.",
	})
}

// originAnswerOf answers a checked request from the Mate's state in HQ:
// connected when the asked address already is the origin of the asked
// repository, or of another one of the application; else asked, also by an
// HQ older than repositories in that state. hqAddress and stateDir say where
// the code comes in (originFromHolder, originCheckout).
func originAnswerOf(req ops.OriginRequest, state hq.MateState, hqAddress, orgID, stateDir string) originAnswer {
	dir := originCheckout(stateDir, req.Repo)
	if holder, origin := originHolder(req, state); holder != "" {
		connected := &originConnected{Origin: req, Repo: holder}
		if holder == req.Repo {
			return originAnswer{Connected: connected,
				NextActions: fmt.Sprintf("Nothing was asked: %s is already the origin of %s in HQ. Take its code first, and never ask for a token: %s. %s",
					req.URL, holder, originFromHQ(dir), originKept(req, origin.Mode, dir))}
		}
		return originAnswer{Connected: connected,
			NextActions: fmt.Sprintf("Nothing was asked: %s is already the origin of %s in HQ, not of %s. Its code comes from that repository, %s, never from the asked address, and never ask for a token: %s.",
				req.URL, holder, req.Repo, holder, originFromHolder(req.Repo, holder, origin, state, hqAddress, orgID, stateDir))}
	}
	if !ops.IsGitHubRepository(req.URL) {
		return originAnswer{
			Requested: &originRequested{Origin: req},
			NextActions: fmt.Sprintf("Now waiting on the person: Mate asks whether %[2]s follows %[1]s — a repository outside GitHub is only followed, never pushed to; the code is kept in Mate. Tell them in one sentence why, and nothing about how they pick. Never ask for a token in the chat. A message of its own tells you how it ended. Followed: take the code from HQ — %[3]s — then %[4]s; only a public one can be fetched. Declined: nothing changes. End your turn.",
				req.URL, req.Repo, originFromHQ(dir), originFollowed(req, dir)),
		}
	}
	return originAnswer{
		Requested: &originRequested{Origin: req},
		NextActions: fmt.Sprintf("Now waiting on the person: Mate asks where the code of %[2]s is kept — on GitHub at %[1]s, or in Mate, only following it. Tell them in one sentence why, and nothing about how they pick. Never ask for a token in the chat. A message of its own tells you what they picked. Either way take the code from HQ first: %[3]s. Which they picked, that message says. %[4]s %[5]s Declined: nothing changes. End your turn.",
			req.URL, req.Repo, originFromHQ(dir), "Kept on GitHub: "+originOnGitHub(dir), originInMate(req, dir)),
	}
}

// originKept is what the way HQ says the person keeps a connected origin's
// code means for the pair's checkout. An HQ older than that way says none,
// and the agent asks the person rather than guess.
func originKept(req ops.OriginRequest, mode, dir string) string {
	switch {
	case !ops.IsGitHubRepository(req.URL):
		return "It is only followed: " + originFollowed(req, dir) + "; only a public one can be fetched."
	case mode == hq.OriginOnGitHub:
		return "Kept on GitHub: " + originOnGitHub(dir)
	case mode == hq.OriginFollowed:
		return originInMate(req, dir)
	}
	return fmt.Sprintf("HQ does not say which way the person keeps it: ask them in one sentence — on GitHub, or in Mate, only following it — and end your turn; their answer picks one of these. %s %s",
		"Kept on GitHub: "+originOnGitHub(dir), originInMate(req, dir))
}

// originOnGitHub is how a repository whose code is kept on GitHub is worked
// on in its checkout at dir: its origin is GitHub — the wiring points it there
// (hqPairOrigin), never a second remote — and git asks `zcp hq git-credential`
// for it, which only the Mate's own shell has; work goes on a branch of the
// agent's own, pushed to origin, and its change is opened after the push. HQ
// opens the branch's pull request and the person merges it in Mate, so the
// agent's own pull request is never where the work ends.
func originOnGitHub(dir string) string {
	return fmt.Sprintf("ZCP points the checkout's origin at GitHub, never a second remote; work on it in your own shell, never over ssh: `cd %s && git fetch origin`, a branch of your own (`git checkout -b <branch> origin/main`), commit, `git push origin <branch>`, and open its change with `zerops_workflow action=\"open-change\" branch=\"<branch>\" title=\"<what it does>\"`: HQ opens its pull request on GitHub from that branch and the person merges it in Mate, so none of your own (`gh pr create`) is where the work ends. Never push `main` and never force-push: `main` moves only by a merge.",
		dir)
}

// originInMate keeps HQ the checkout's origin and follows the repository.
func originInMate(req ops.OriginRequest, dir string) string {
	return "Kept in Mate: HQ stays the origin and GitHub is only followed — " + originFollowed(req, dir) +
		"; a private one is read with HQ's read token for it, and when HQ holds none, say so."
}

// originCheckout is the repository's checkout as the Mate's own shell mounts
// it — a code-volume pair's below the mount, where the deploy reads it. Only
// that shell runs `zcp`, which git's GitHub helper is, so git that talks to
// GitHub runs there and never over ssh in the service; HQ's git runs there
// too, through the same helper.
func originCheckout(stateDir, repo string) string {
	workingDir := ""
	if meta, err := workflow.FindServiceMeta(stateDir, repo); err == nil && meta != nil {
		workingDir = meta.WorkingDirFor(repo)
	}
	return ops.SourceMountDir(ops.DefaultWorkingDir, repo, workingDir)
}

// originFollowed adds the repository as a fetch-only `upstream` beside HQ's
// origin: a push to it is refused by git itself. A GitHub one is fetched
// through `zcp hq git-credential`, which answers a private one with HQ's read
// token when HQ holds one.
func originFollowed(req ops.OriginRequest, dir string) string {
	helper := ""
	if ops.IsGitHubRepository(req.URL) {
		helper = " && " + hq.GitHubHelperConfig
	}
	return fmt.Sprintf("in your own shell, never over ssh: `cd %s && git remote add upstream %s && git remote set-url --push upstream no_push%s && git fetch upstream`, and take its new work in on a branch of your own with `git merge upstream/main`",
		dir, req.URL, helper)
}

// originHolder is the repository in HQ the asked address already is the
// origin of, with that origin: the asked repository first, else the first
// other one. "" for none.
func originHolder(req ops.OriginRequest, state hq.MateState) (string, hq.RepoOrigin) {
	holder, held := "", hq.RepoOrigin{}
	for _, repo := range state.Repos {
		if repo.Origin == nil || !topology.SameRepoURL(repo.Origin.URL, req.URL) {
			continue
		}
		if repo.Name == req.Repo {
			return repo.Name, *repo.Origin
		}
		if holder == "" {
			holder, held = repo.Name, *repo.Origin
		}
	}
	return holder, held
}

// originFromHQ takes the code of the checkout's origin into the checkout at
// dir (originTake).
func originFromHQ(dir string) string { return originTake(dir, "git fetch origin main") }

// originTake runs fetch in the checkout at dir, in the Mate's own shell, and
// takes the fetched main in. Taken onto an untouched repository, main is the
// origin's own history and shares nothing with HQ's seed, so a checkout
// holding only the seed — one commit of the empty tree, as the wiring cut it
// — moves onto it (`reset --keep` refuses rather than overwrite a file in
// the way); a checkout with work sharing history with it merges it in; work
// sharing none stays on a branch named after its commit and the checkout
// moves onto main — two unrelated histories are never joined.
func originTake(dir, fetch string) string {
	return fmt.Sprintf("in your own shell, never over ssh: `cd %s && %s && "+
		"if [ \"$(git rev-list --count HEAD)\" = 1 ] && [ -z \"$(git ls-tree HEAD)\" ]; then git reset --keep FETCH_HEAD; "+
		"elif git merge-base HEAD FETCH_HEAD >/dev/null; then git merge --no-edit FETCH_HEAD; "+
		"else %s && git reset --keep FETCH_HEAD; fi` — work of its own sharing no history with that main stays on its branch `own-history-<commit>`; carry what it needs over with `git cherry-pick` onto a branch cut from main",
		dir, fetch, ops.KeepOwnHistory)
}

// originFromHolder takes the code of holder, another repository in HQ, into
// a pair's checkout: into holder's own, when it is one of this project's
// services; else, since an HQ repository of the application can belong to a
// sibling Mate's project and name no host here, fetched into the asked
// repository's — from GitHub when the person keeps holder's code there, else
// from HQ.
func originFromHolder(repo, holder string, origin hq.RepoOrigin, state hq.MateState, hqAddress, orgID, stateDir string) string {
	if meta, err := workflow.FindServiceMeta(stateDir, holder); err == nil && meta != nil {
		return originFromHQ(originCheckout(stateDir, holder))
	}
	dir := originCheckout(stateDir, repo)
	if origin.Mode == hq.OriginOnGitHub {
		return originTake(dir, hq.GitHubHelperConfig+" && git fetch "+origin.URL+" main")
	}
	appID := ""
	if state.AppID != nil {
		appID = *state.AppID
	}
	holderURL, err := hq.RepoURLAt(hqAddress, orgID, appID, holder)
	if err != nil {
		return "(HQ's address for it cannot be built: " + err.Error() + ")"
	}
	return originTake(dir, "git fetch "+holderURL+" main")
}
