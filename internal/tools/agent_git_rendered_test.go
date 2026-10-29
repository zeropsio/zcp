// Tests for: the git commands zcp's texts give a Mate's agent, as rendered —
// each runs in the dev service through `zcp git <host>`, or over ssh in a
// lease; a substitution of zcp git fails closed (docs/spec-mate.md §6.8,
// content.AgentGitViolations).
package tools

import (
	"net/http"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/content"
	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestAgentGitTexts_RenderedCommandsRunInTheService: every text that gives a
// Mate's agent git commands for its dev repositories — taking an origin's
// code, the GitHub realignment, a change's branch, its standing and HQ's
// push into it, a branch refusal's remedy, the git-push refusal, a
// pool-migrate's work branch, the AGENTS.md git host — runs each in the dev
// service through `zcp git <host>` (a lease's over ssh), never on the mount,
// in the agent's own shell or a zcp-side worktree, and decides nothing on a
// substitution that fails open. A project without a Mate keeps `zcp hq git`
// on its mounted checkout (owner decision Q1): its texts are not checked
// here — TestBaseBranchRefusalRemedy_PlainProject_Unchanged,
// TestGitPushToHQ_PlainProject_KeepsTheCheckoutPushWords and
// TestPoolMigrate_WorkBranch_FromChangeCurrentOrUpstream pin them.
func TestAgentGitTexts_RenderedCommandsRunInTheService(t *testing.T) {
	t.Parallel()
	hqc, err := hq.Open(http.DefaultClient, hq.Identity{Origin: "https://hq.example", Key: "key", ProjectID: labMate, OrgID: labOrg})
	if err != nil {
		t.Fatalf("hq.Open: %v", err)
	}
	stateDir := t.TempDir()
	writeWiredPair(t, stateDir, "appdev", "appdev")
	plain, pool, none := repoDevHost(stateDir, labApp, "appdev"), repoRoute{pool: "webpool"}, repoRoute{}
	leased := pool.inLease("webpool-3")
	yes := true
	behind := workflow.HQChangeTold{State: hq.ChangeOpen, Ready: &yes, Mergeability: hq.MergeClean, Behind: true}
	conflict := workflow.HQChangeTold{State: hq.ChangeOpen, Ready: &yes, Mergeability: hq.MergeConflict}
	released := "mate/" + labMate + "/1"
	head := "0123456789abcdef0123456789abcdef01234567"
	app := labApp

	texts := map[string]string{
		"originFromHQ":                        originFromHQ("appdev"),
		"originTake, from GitHub by address":  originTake("appdev", "fetch https://github.com/acme/app main"),
		"originOnGitHub":                      originOnGitHub("appdev"),
		"originFollowed, GitHub":              originFollowed(ops.OriginRequest{URL: "https://github.com/acme/app", Repo: "appdev"}, "appdev"),
		"originFollowed, elsewhere":           originFollowed(ops.OriginRequest{URL: "https://gitlab.com/acme/app", Repo: "appdev"}, "appdev"),
		"originAsItsOwnBranch":                originAsItsOwnBranch("appdev", "fetch https://github.com/acme/app HEAD:<branch>"),
		"originFromHolder, a service here":    originFromHolder("appdev", "appdev", hq.RepoOrigin{}, hq.MateState{}, "https://hq.example", labOrg, stateDir),
		"originFromHolder, a sibling's on GH": originFromHolder("appdev", "web", hq.RepoOrigin{URL: "https://github.com/acme/web", Mode: hq.OriginOnGitHub}, hq.MateState{}, "https://hq.example", labOrg, stateDir),
		"originFromHolder, a sibling's in HQ": originFromHolder("appdev", "web", hq.RepoOrigin{}, hq.MateState{AppID: &app}, "https://hq.example", labOrg, stateDir),
		"divergedWords":                       divergedWords(stateDir, hqc, labApp, "appdev", "2026-10-28T10:00:00.000Z"),
		"hqGitPushRefusal":                    hqGitPushRefusal("appdev", true).Suggestion,
		"AGENTS.md":                           agentsMDForAMate(t),
	}
	for name, route := range map[string]repoRoute{"its dev service": plain, "a dev pool": pool, "a lease": leased, "no pair here": none} {
		texts["corePushWords, "+name] = corePushWords(6, 5, "badge", route, nil, true)
		texts["takeChangeBranch, "+name] = takeChangeBranch(route, hqc, released)
		texts["hqOpenChangeWork, "+name] = hqOpenChangeWork(route, hqc, workflow.HQChange{Number: 1, Repo: "appdev", Branch: released})
		texts["handedWork, pushed, "+name] = handedWork(route, hq.MateChange{Number: 2, Head: &head}, "appdev", "fix-login", true)
		texts["handedWork, unpushed, "+name] = handedWork(route, hq.MateChange{Number: 2}, "appdev", "fix-login", true)
		texts["openChangesWords, "+name] = openChangesWords([]workflow.HQChange{{Number: 1, Branch: "fix-login"}}, route)
		texts["standingWords behind, "+name] = standingWords(6, "badge", behind, workflow.HQChangeTold{}, route, nil)
		texts["standingWords conflict, "+name] = standingWords(6, "badge", conflict, workflow.HQChangeTold{}, route, nil)
		texts["mainTakesTheMerge, "+name] = mainTakesTheMerge(route)
		texts["answeredOpenChange, "+name] = answeredOpenChange(route, hqc, "appdev", "add-signup", "fix-login", 1)
	}
	texts["corePushWords, its leases"] = corePushWords(6, 5, "badge", plain, []string{"webpool-3", "boxpool-6"}, false)
	texts["standingWords behind, its leases"] = standingWords(6, "badge", behind, workflow.HQChangeTold{}, plain, []string{"webpool-3"})
	for _, refusal := range []string{"ZCP_BASE_NOT_SEED 0123456789ab", "ZCP_BASE_NOT_SEED", "ZCP_BRANCH_ELSEWHERE", "ZCP_STAGED_CHANGES", "ZCP_UNTRACKED_COLLISION app.js"} {
		texts["BaseBranchRefusalRemedy "+refusal] = ops.BaseBranchRefusalRemedy(refusal, ops.RemedyRunner{DevHost: "appdev"})
	}
	meta := &workflow.ServiceMeta{Hostname: "appdev"}
	for _, branch := range []string{"", hqBase, "feat"} {
		census := &ops.PoolMigrateCensus{Trees: []ops.PoolMigrateTree{{Path: "/var/www", Branch: branch}}}
		texts["poolMigrateWorkBranchOf in a Mate, on "+branch] = poolMigrateWorkBranchOf(meta, census, runtime.Info{MateEnabled: true}, nil).Missing
	}

	for name, text := range texts {
		for _, v := range content.AgentGitViolations(text) {
			t.Errorf("%s: %s\n%s", name, v, text)
		}
	}
	// The take's reads are assignments chained with &&, so a failed zcp git
	// stops it before anything is decided.
	if take := texts["originFromHQ"]; !strings.Contains(take, "n=$(zcp git appdev rev-list --count HEAD) && t=$(zcp git appdev ls-tree HEAD) && ") {
		t.Errorf("the take does not read the checkout into variables chained with &&: %s", take)
	}
}

// agentsMDForAMate is a Mate's AGENTS.md: its git-host block names the
// commands its agent runs for every repository.
func agentsMDForAMate(t *testing.T) string {
	t.Helper()
	out, err := content.BuildAgentsMD(runtime.Info{InContainer: true, ServiceName: "zcp", MateEnabled: true}, false)
	if err != nil {
		t.Fatalf("BuildAgentsMD: %v", err)
	}
	if !strings.Contains(out, "**Your git host.**") {
		t.Fatal("a Mate's AGENTS.md has no git-host block")
	}
	return out
}
