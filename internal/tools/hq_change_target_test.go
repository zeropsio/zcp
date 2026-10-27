// Tests for: tools/hq_change_target.go — which change a change call means:
// the repository a lease or a pair names (pairs sharing one count once), the
// branch named or a lease's, and among the repository's open changes only
// the one there is — never the newest.
package tools

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

const targetHQ = "https://hq.example.com"

// writeHQLeaseRecord writes zcp's record of a lease of repo on branch, as an
// acquire leaves it.
func writeHQLeaseRecord(t *testing.T, stateDir, pool string, number int, repo, branch string) {
	t.Helper()
	dir := filepath.Join(stateDir, "leases")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{
		"poolHostname": pool, "poolServiceId": "svc-" + pool, "containerId": fmt.Sprintf("c-%s-%d", pool, number),
		"containerNumber": number, "containerHostname": fmt.Sprintf("node-id-%d.runtime.%s.zerops", number, pool),
		"lease": "lease-secret", "branch": branch, "repo": repo, "acquiredAt": "2026-10-25T10:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%s-%d.json", pool, number)), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

// writeWiredPair writes a pair wired to repo of app-1 in HQ.
func writeWiredPair(t *testing.T, stateDir, dev, repo string) {
	t.Helper()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname: dev, Mode: topology.PlanModeStandard, Stages: []topology.StageRef{{Hostname: dev + "stage"}},
		BootstrapSession: "test", BootstrappedAt: "2026-10-02", GitPushState: topology.GitPushConfigured,
		HQ: &workflow.HQRepoRef{AppID: labApp, Repo: repo},
	}); err != nil {
		t.Fatal(err)
	}
}

// refusalOf is the platform refusal err is, failing the test when it is none.
func refusalOf(t *testing.T, err error) *platform.PlatformError {
	t.Helper()
	var pe *platform.PlatformError
	if !errors.As(err, &pe) {
		t.Fatalf("want a refusal, got %v", err)
	}
	return pe
}

// kara is the Mate the refusal met live: four pairs on one repository, and
// two branches leased across them.
func kara(t *testing.T) string {
	t.Helper()
	stateDir := t.TempDir()
	for _, dev := range []string{"appsdev", "hqpool", "webpool", "boxpool"} {
		writeWiredPair(t, stateDir, dev, "appsdev")
	}
	repo := targetHQ + "/git/" + labOrg + "/" + labApp + "/appsdev.git"
	writeHQLeaseRecord(t, stateDir, "hqpool", 4, repo, "mate-test-badge")
	writeHQLeaseRecord(t, stateDir, "boxpool", 6, repo, "mate-test-badge")
	writeHQLeaseRecord(t, stateDir, "hqpool", 3, repo, "pool-previews")
	return stateDir
}

// TestResolveChange_LeaseAliasNamesRepoAndBranch: a lease alias names its
// record's repository on this Mate's HQ and its branch — open-change needs no
// branch of its own then. A branch named beside it must be the lease's; a
// lease of another repository host, or one on main, opens nothing.
func TestResolveChange_LeaseAliasNamesRepoAndBranch(t *testing.T) {
	t.Parallel()
	stateDir := kara(t)
	writeHQLeaseRecord(t, stateDir, "docpool", 2, "https://github.com/acme/docs", "fix-docs")
	writeHQLeaseRecord(t, stateDir, "webpool", 5, targetHQ+"/git/"+labOrg+"/"+labApp+"/appsdev", "main")

	target, err := resolveChangeTarget(stateDir, targetHQ, changeAsk{Service: "hqpool-4", Verb: changeVerbOpen})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if target.AppID != labApp || target.Repo != "appsdev" || target.Branch != "mate-test-badge" || target.Lease != "hqpool-4" {
		t.Errorf("target = %+v, want appsdev on mate-test-badge from hqpool-4", target)
	}
	if target, err = resolveChangeTarget(stateDir, targetHQ, changeAsk{Service: "boxpool-6", Branch: "mate-test-badge", Verb: changeVerbDescribe}); err != nil || target.Branch != "mate-test-badge" {
		t.Errorf("the lease's own branch named beside it = %+v (%v)", target, err)
	}

	for _, tt := range []struct {
		name string
		ask  changeAsk
		want []string
	}{
		{name: "another branch than the lease's", ask: changeAsk{Service: "hqpool-4", Branch: "other", Verb: changeVerbOpen},
			want: []string{"hqpool-4", "mate-test-badge", "other"}},
		{name: "a repository not on this HQ", ask: changeAsk{Service: "docpool-2", Verb: changeVerbOpen},
			want: []string{"docpool-2", "not this Mate's HQ"}},
		{name: "a lease on main", ask: changeAsk{Service: "webpool-5", Verb: changeVerbOpen},
			want: []string{"webpool-5", "main"}},
		{name: "neither a pair nor a lease", ask: changeAsk{Service: "nosuch-9", Verb: changeVerbDescribe},
			want: []string{platform.ErrServiceNotFound, "nosuch-9"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := resolveChangeTarget(stateDir, targetHQ, tt.ask)
			pe := refusalOf(t, err)
			for _, want := range tt.want {
				if !strings.Contains(pe.Code+" "+pe.Message+" "+pe.Suggestion, want) {
					t.Errorf("the refusal misses %q: %s — %s (%s)", want, pe.Code, pe.Message, pe.Suggestion)
				}
			}
		})
	}
}

// TestResolveChange_PairsSharingARepositoryAreOne: the Kara refusal — four
// pairs wired to one repository are one repository, so a call naming none of
// them is that repository's; pairs of two repositories still need service=,
// unless the branch named is leased in only one of them.
func TestResolveChange_PairsSharingARepositoryAreOne(t *testing.T) {
	t.Parallel()
	stateDir := kara(t)

	target, err := resolveChangeTarget(stateDir, targetHQ, changeAsk{Branch: "mate-test-badge", Verb: changeVerbOpen})
	if err != nil {
		t.Fatalf("four pairs on one repository were refused: %v", err)
	}
	if target.Repo != "appsdev" || target.Branch != "mate-test-badge" || target.Pair == nil {
		t.Errorf("target = %+v, want appsdev on mate-test-badge with a pair of it", target)
	}
	if target, err = resolveChangeTarget(stateDir, targetHQ, changeAsk{Service: "webpool", Branch: "x", Verb: changeVerbOpen}); err != nil || target.Repo != "appsdev" || target.Pair.Hostname != "webpool" {
		t.Errorf("a pair named = %+v (%v), want appsdev from webpool", target, err)
	}

	writeWiredPair(t, stateDir, "docsdev", "docsdev")
	_, err = resolveChangeTarget(stateDir, targetHQ, changeAsk{Branch: "fix-docs", Verb: changeVerbOpen})
	pe := refusalOf(t, err)
	if pe.Code != platform.ErrServiceRequired || !strings.Contains(pe.Suggestion, `"appsdev"`) || !strings.Contains(pe.Suggestion, `"docsdev"`) {
		t.Errorf("two repositories: %s — %s (%s), want SERVICE_REQUIRED naming a pair of each", pe.Code, pe.Message, pe.Suggestion)
	}
	if target, err = resolveChangeTarget(stateDir, targetHQ, changeAsk{Branch: "mate-test-badge", Verb: changeVerbOpen}); err != nil || target.Repo != "appsdev" {
		t.Errorf("a branch leased in one repository only = %+v (%v), want appsdev", target, err)
	}
}

// openState is the Mate's state with the open changes of appsdev named.
func openState(changes ...hq.MateChange) *hq.MateState {
	app := labApp
	for i := range changes {
		changes[i].Repo = cmp.Or(changes[i].Repo, "appsdev")
		changes[i].State = cmp.Or(changes[i].State, hq.ChangeOpen)
	}
	return &hq.MateState{AppID: &app, Changes: changes, OpenPerBranch: true}
}

func titled(s string) *string { return &s }

// TestResolveChange_SeveralOpen_RefusesListingEach: a describe that names no
// branch, in a repository with several open changes, is refused with each
// one's branch, number and title — the call that names it.
func TestResolveChange_SeveralOpen_RefusesListingEach(t *testing.T) {
	t.Parallel()
	stateDir := kara(t)
	target, err := resolveChangeTarget(stateDir, targetHQ, changeAsk{Verb: changeVerbDescribe})
	if err != nil {
		t.Fatal(err)
	}
	state := openState(
		hq.MateChange{Number: 6, Title: titled("Badge"), Branch: "mate-test-badge"},
		hq.MateChange{Number: 2, Title: titled("Pool previews"), Branch: "pool-previews"},
		hq.MateChange{Number: 1, Branch: "old", State: hq.ChangeMerged},
	)

	_, err = target.pick(state, nil, changeVerbDescribe)

	pe := refusalOf(t, err)
	for _, want := range []string{`branch="pool-previews"`, `#2 "Pool previews"`, `branch="mate-test-badge"`, `#6 "Badge"`} {
		if !strings.Contains(pe.Message+" "+pe.Suggestion, want) {
			t.Errorf("the refusal misses %s: %s (%s)", want, pe.Message, pe.Suggestion)
		}
	}
	if strings.Contains(pe.Message+pe.Suggestion, `"old"`) {
		t.Errorf("the refusal lists a merged change: %s", pe.Message)
	}

	// The only open one is picked; one named by branch is that one.
	one := openState(hq.MateChange{Number: 6, Title: titled("Badge"), Branch: "mate-test-badge"}, hq.MateChange{Number: 2, Branch: "pool-previews", State: hq.ChangeMerged})
	if pick, err := target.pick(one, nil, changeVerbDescribe); err != nil || pick.Number != 6 || pick.Branch != "mate-test-badge" {
		t.Errorf("the one open change = %+v (%v), want #6 on mate-test-badge", pick, err)
	}
	target.Branch = "pool-previews"
	if pick, err := target.pick(state, nil, changeVerbDescribe); err != nil || pick.Number != 2 {
		t.Errorf("the change on pool-previews = %+v (%v), want #2", pick, err)
	}
}

// TestResolveChange_NeverTheNewest: nothing infers a change from what is
// newest — not among open changes in the Mate's state, not among the ledger's
// when the state cannot be read, and open-change never infers its branch.
func TestResolveChange_NeverTheNewest(t *testing.T) {
	t.Parallel()
	stateDir := kara(t)
	target, err := resolveChangeTarget(stateDir, targetHQ, changeAsk{Verb: changeVerbDescribe})
	if err != nil {
		t.Fatal(err)
	}
	several := openState(hq.MateChange{Number: 6, Branch: "mate-test-badge"}, hq.MateChange{Number: 2, Branch: "pool-previews"})
	if pick, err := target.pick(several, nil, changeVerbDescribe); err == nil {
		t.Errorf("picked #%d of several open changes", pick.Number)
	}
	ledger := []workflow.HQChange{
		{AppID: labApp, Repo: "appsdev", Number: 2, Branch: "pool-previews"},
		{AppID: labApp, Repo: "appsdev", Number: 6, Branch: "mate-test-badge"},
	}
	if pick, err := target.pick(nil, ledger, changeVerbDescribe); err == nil {
		t.Errorf("picked #%d of the ledger's several changes with the state unread", pick.Number)
	}
	if pick, err := target.pick(nil, ledger[:1], changeVerbDescribe); err != nil || pick.Number != 2 {
		t.Errorf("the ledger's one change with the state unread = %+v (%v), want #2", pick, err)
	}

	_, err = resolveChangeTarget(stateDir, targetHQ, changeAsk{Service: "appsdev", Verb: changeVerbOpen})
	if pe := refusalOf(t, err); !strings.Contains(pe.Message, "branch=") {
		t.Errorf("open-change on a pair with no branch: %s, want it to ask for branch=", pe.Message)
	}
}
