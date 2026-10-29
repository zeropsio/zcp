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
		Description: "Before a repository's code comes in, ask the person where it is kept: on GitHub, mounted in HQ as its origin, or in Mate, only followed. Writes nothing: Mate shows a card for repo, the pair's dev hostname; a message brings their pick. Never ask for a token. action=withdraw url (repo) closes an unneeded ask — not in chat.",
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

// originWithdrawWords says how an ask is taken back once the agent no
// longer needs the pick: withdrawn, so Mate closes its card — never said in
// chat to be outdated.
func originWithdrawWords(req ops.OriginRequest) string {
	return fmt.Sprintf("Should you no longer need their pick — re-planned, the repository gone, asked by mistake — withdraw it (`zerops_origin action=\"withdraw\" url=%q repo=%q`): Mate closes its card; never say in chat that it is outdated.", req.URL, req.Repo)
}

// originAnswerOf answers a checked request from the Mate's state in HQ:
// connected when the asked address already is the origin of the asked
// repository, or of another one of the application; else asked, also by an
// HQ older than repositories in that state. hqAddress and stateDir say where
// the code comes in (originFromHolder, originHost).
func originAnswerOf(req ops.OriginRequest, state hq.MateState, hqAddress, orgID, stateDir string) originAnswer {
	host := originHost(stateDir, req.Repo)
	if holder, origin := originHolder(req, state); holder != "" {
		connected := &originConnected{Origin: req, Repo: holder}
		if holder == req.Repo {
			return originAnswer{Connected: connected,
				NextActions: fmt.Sprintf("Nothing was asked: %s is already the origin of %s in HQ. Take its code first, and never ask for a token: %s. %s",
					req.URL, holder, originFromHQ(host), originKept(req, origin.Mode, host))}
		}
		return originAnswer{Connected: connected,
			NextActions: fmt.Sprintf("Nothing was asked: %s is already the origin of %s in HQ, not of %s. Its code comes from that repository, %s, never from the asked address, and never ask for a token: %s.",
				req.URL, holder, req.Repo, holder, originFromHolder(req.Repo, holder, origin, state, hqAddress, orgID, stateDir))}
	}
	if !ops.IsGitHubRepository(req.URL) {
		return originAnswer{
			Requested: &originRequested{Origin: req},
			NextActions: fmt.Sprintf("Now waiting on the person: Mate asks whether %[2]s follows %[1]s — a repository outside GitHub is only followed, never pushed to; the code is kept in Mate. Tell them in one sentence why, and nothing about how they pick. Never ask for a token in the chat. A message of its own tells you how it ended. Followed: take the code from HQ — %[3]s — then %[4]s; only a public one can be fetched. Declined: nothing changes. %[5]s End your turn.",
				req.URL, req.Repo, originFromHQ(host), originFollowed(req, host), originWithdrawWords(req)),
		}
	}
	return originAnswer{
		Requested: &originRequested{Origin: req},
		NextActions: fmt.Sprintf("Now waiting on the person: Mate asks where the code of %[2]s is kept — on GitHub at %[1]s, or in Mate, only following it. Tell them in one sentence why, and nothing about how they pick. Never ask for a token in the chat. A message of its own tells you what they picked. Either way take the code from HQ first: %[3]s. %[4]s Which they picked, that message says. %[5]s %[6]s Declined: nothing changes; should its code still come in, %[7]s. %[8]s End your turn.",
			req.URL, req.Repo, originFromHQ(host), originMainFromGitHub, "Kept on GitHub: "+originOnGitHub(host), originInMate(req, host),
			originAsItsOwnBranch(host, "fetch "+req.URL+" HEAD:<branch>"), originWithdrawWords(req)),
	}
}

// originMainFromGitHub is what connecting a GitHub repository does to its
// repository in HQ: HQ follows GitHub and takes the repository's `main` from
// it in place of the one initial commit HQ made `main` with, so `main` starts
// as GitHub's own and its history gains no commit of HQ's. The agent's own
// setup follows as an ordinary change, which HQ merges on GitHub by squash.
const originMainFromGitHub = "Connected, HQ takes the repository's `main` from GitHub, so `main` starts as GitHub's `main`: never clone it in and merge it, and never merge HQ's initial commit into its history; " +
	"your own setup goes in after it, as an ordinary change on a branch cut from that `main` — on GitHub a pull request, merged by squash."

// originAsItsOwnBranch is how code HQ takes from no origin comes in: its
// history, put on a branch by onto (a git command's arguments, run in host's
// dev service), pushed exactly as it is and its change opened from that
// branch — merged, HQ makes that history `main`. It is never merged or
// rebased onto the initial commit HQ made `main` with, which would leave
// `main` holding commits its origin never had.
func originAsItsOwnBranch(host, onto string) string {
	return fmt.Sprintf("its history goes in exactly as it is, as a branch of its own (%s) "+
		"whose change you open with `zerops_workflow action=\"open-change\" branch=\"<branch>\" title=\"<what it brings in>\"`: "+
		"merged, HQ makes that history `main`; never merge or rebase it onto `main`'s initial commit, nor merge that commit into it",
		inDevService(host, onto, "push origin <branch>"))
}

// inDevService is cmds — each a git command's arguments — run one after
// another in host's dev service through `zcp git`, which runs git in the
// checkout zcp's record names and answers it for HQ and GitHub
// (docs/spec-mate.md §6.8): one command line in backticks, stopping at the
// first that fails.
func inDevService(host string, cmds ...string) string {
	return repoRoute{host: host}.git(cmds...)
}

// originKept is what the way HQ says the person keeps a connected origin's
// code means for the pair's checkout, in host's dev service. An HQ older than
// that way says none, and the agent asks the person rather than guess.
func originKept(req ops.OriginRequest, mode, host string) string {
	switch {
	case !ops.IsGitHubRepository(req.URL):
		return "It is only followed: " + originFollowed(req, host) + "; only a public one can be fetched."
	case mode == hq.OriginOnGitHub:
		return "Kept on GitHub: " + originOnGitHub(host)
	case mode == hq.OriginFollowed:
		return originInMate(req, host)
	}
	return fmt.Sprintf("HQ does not say which way the person keeps it: ask them in one sentence — on GitHub, or in Mate, only following it — and end your turn; their answer picks one of these. %s %s",
		"Kept on GitHub: "+originOnGitHub(host), originInMate(req, host))
}

// originOnGitHub is how a repository whose code is kept on GitHub is worked
// on in host's dev service: its origin is GitHub — the wiring points it there
// (hqPairOrigin), never a second remote — and zcp answers git there for it
// (`zcp git`); work goes on a branch of the agent's own, pushed to origin,
// and its change is opened after the push. HQ opens the branch's pull request
// and the person merges it in Mate, so the agent's own pull request is never
// where the work ends.
func originOnGitHub(host string) string {
	return fmt.Sprintf("ZCP points the checkout's origin at GitHub, never a second remote; work on it in its dev service, where zcp answers git for GitHub: %s, a branch of your own (%s), commit, %s, and open its change with `zerops_workflow action=\"open-change\" branch=\"<branch>\" title=\"<what it does>\"`: HQ opens its pull request on GitHub from that branch and the person merges it in Mate, so none of your own (`gh pr create`) is where the work ends. Never push `main` and never force-push: `main` moves only by a merge.",
		inDevService(host, "fetch origin"), inDevService(host, "checkout -b <branch> origin/main"), inDevService(host, "push origin <branch>"))
}

// originInMate keeps HQ the checkout's origin and follows the repository.
func originInMate(req ops.OriginRequest, host string) string {
	return "Kept in Mate: HQ stays the origin and GitHub is only followed — " + originFollowed(req, host) +
		"; a private one is read with HQ's read token for it, and when HQ holds none, say so."
}

// originHost is the dev service whose checkout repo's code comes into: the
// dev half of the pair the records keep under that hostname — `zcp git` runs
// git in the directory the record says its deploys run in, a code-volume
// pair's volume — else repo itself, a service the records do not name yet.
func originHost(stateDir, repo string) string {
	if meta, err := workflow.FindServiceMeta(stateDir, repo); err == nil && meta != nil {
		return meta.Hostname
	}
	return repo
}

// originFollowed adds the repository as a fetch-only `upstream` beside HQ's
// origin, in host's dev service: a push to it is refused by git itself. A
// GitHub one is fetched through `zcp git`, which zcp answers for the
// repository's origin with HQ's read token for a private one when HQ holds
// one, and HQ takes its `main` itself; one elsewhere first comes in as a
// branch of its own (originAsItsOwnBranch), its new work merged only once
// `main` shares its history.
func originFollowed(req ops.OriginRequest, host string) string {
	follow := inDevService(host, "remote add upstream "+req.URL, "remote set-url --push upstream no_push", "fetch upstream")
	merge := inDevService(host, "merge upstream/main")
	if ops.IsGitHubRepository(req.URL) {
		return fmt.Sprintf("in its dev service: %s, and take its new work in on a branch of your own with %s", follow, merge)
	}
	// HQ takes `main` from GitHub alone: a repository followed elsewhere
	// first comes in as a branch of its own.
	return fmt.Sprintf("in its dev service: %s; "+
		"while `main` still holds only HQ's initial commit, %s; after that, take its new work in on a branch of your own with %s",
		follow, originAsItsOwnBranch(host, "checkout -b <branch> upstream/main"), merge)
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

// originFromHQ takes the code of the checkout's origin into host's checkout
// (originTake).
func originFromHQ(host string) string { return originTake(host, "fetch origin main") }

// originTake runs fetch — a git fetch's arguments — in host's dev service
// through `zcp git` and takes the fetched main in. Taken onto an untouched
// repository, main is the origin's own history and shares nothing with HQ's
// seed, so a checkout holding only the seed — one commit of the empty tree,
// as the wiring cut it — moves onto it (`reset --keep` refuses rather than
// overwrite a file in the way); a checkout with work sharing history with it
// merges it in; work sharing none stays on a branch named after its commit
// and the checkout moves onto main — two unrelated histories are never
// joined. Each read the decision rests on is an assignment chained with &&,
// so a `zcp git` that fails — ssh, a restarting service — stops the chain
// before anything is decided; a merge-base that fails for any reason but "no
// common history" (its status 1) decides nothing either.
func originTake(host, fetch string) string {
	git := func(args string) string { return "zcp git " + host + " " + args }
	return fmt.Sprintf("in its dev service: `%s && n=$(%s) && t=$(%s) && "+
		"if [ \"$n\" = 1 ] && [ -z \"$t\" ]; then %s; "+
		"elif %s >/dev/null; then %s; "+
		"elif [ $? = 1 ]; then h=$(%s) && %s && %s; fi` — work of its own sharing no history with that main stays on its branch `own-history-<commit>`; carry what it needs over with %s onto a branch cut from main",
		git(fetch), git("rev-list --count HEAD"), git("ls-tree HEAD"),
		git("reset --keep FETCH_HEAD"),
		git("merge-base HEAD FETCH_HEAD"), git("merge --no-edit FETCH_HEAD"),
		git("rev-parse --short=12 HEAD"), git(`branch -f "own-history-$h"`), git("reset --keep FETCH_HEAD"),
		inDevService(host, "cherry-pick <commit>"))
}

// originFromHolder takes the code of holder, another repository in HQ, into
// a pair's checkout: into holder's own, when it is one of this project's
// services; else, since an HQ repository of the application can belong to a
// sibling Mate's project and name no host here, fetched into the asked
// repository's — from GitHub when the person keeps holder's code there, else
// from HQ.
func originFromHolder(repo, holder string, origin hq.RepoOrigin, state hq.MateState, hqAddress, orgID, stateDir string) string {
	if meta, err := workflow.FindServiceMeta(stateDir, holder); err == nil && meta != nil {
		return originFromHQ(meta.Hostname)
	}
	host := originHost(stateDir, repo)
	if origin.Mode == hq.OriginOnGitHub {
		// GitHub's address in the fetch's own argv is what zcp answers.
		return originTake(host, "fetch "+origin.URL+" main")
	}
	appID := ""
	if state.AppID != nil {
		appID = *state.AppID
	}
	holderURL, err := hq.RepoURLAt(hqAddress, orgID, appID, holder)
	if err != nil {
		return "(HQ's address for it cannot be built: " + err.Error() + ")"
	}
	return originTake(host, "fetch "+holderURL+" main")
}
