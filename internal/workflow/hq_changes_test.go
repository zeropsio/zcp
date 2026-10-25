package workflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/topology"
)

// TestHQChangeLedger_OneFilePerChange: each change the Mate holds is a file of
// its own under hq/changes/<appId>/<repo>/<number>.json, written 0600; the
// ledger lists them by application, repository and number, and a forgotten
// change leaves nothing behind.
func TestHQChangeLedger_OneFilePerChange(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	for _, c := range []HQChange{
		{AppID: "app-1", Repo: "appsdev", Number: 6, Branch: "badge", Title: "Badge", Source: HQChangeFromOpen},
		{AppID: "app-1", Repo: "appsdev", Number: 2, Branch: "pool-previews", Source: HQChangeFromState},
		{AppID: "app-1", Repo: "apidev", Number: 1, Branch: "fix-login", Source: HQChangeFromHanded},
	} {
		if err := RecordHQChange(stateDir, c); err != nil {
			t.Fatalf("record #%d: %v", c.Number, err)
		}
	}

	path := filepath.Join(stateDir, "hq", "changes", "app-1", "appsdev", "6.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("change #6's file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("change #6's file mode = %v, want 0600", info.Mode().Perm())
	}
	listed, err := ListHQChanges(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(listed))
	for _, c := range listed {
		got = append(got, fmt.Sprintf("%s/%s#%d@%s", c.AppID, c.Repo, c.Number, c.Branch))
		if c.RecordedAt == "" {
			t.Errorf("change #%d carries no recordedAt", c.Number)
		}
	}
	want := []string{"app-1/apidev#1@fix-login", "app-1/appsdev#2@pool-previews", "app-1/appsdev#6@badge"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("ledger = %v, want %v", got, want)
	}
	if c, found, err := ReadHQChange(stateDir, "app-1", "appsdev", 6); err != nil || !found || c.Title != "Badge" || c.Source != HQChangeFromOpen {
		t.Errorf("change #6 = %+v (%v)", c, err)
	}

	if err := ForgetHQChange(stateDir, "app-1", "appsdev", 6); err != nil {
		t.Fatal(err)
	}
	if c, found, err := ReadHQChange(stateDir, "app-1", "appsdev", 6); err != nil || found {
		t.Errorf("a forgotten change reads %+v (%v)", c, err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a forgotten change's file stays: %v", err)
	}
}

// TestHQChangeLedger_RefusesANameThatIsNoPathPart: an application or
// repository name that would reach outside the ledger is refused, never
// written.
func TestHQChangeLedger_RefusesANameThatIsNoPathPart(t *testing.T) {
	t.Parallel()
	for _, c := range []HQChange{
		{AppID: "..", Repo: "appsdev", Number: 1},
		{AppID: "app-1", Repo: "../../leases", Number: 1},
		{AppID: "", Repo: "appsdev", Number: 1},
		{AppID: "app-1", Repo: "appsdev", Number: 0},
	} {
		stateDir := t.TempDir()
		if err := RecordHQChange(stateDir, c); err == nil {
			t.Errorf("recorded %+v", c)
		}
	}
}

// TestHQChangeLedger_ConcurrentWritersKeepEachOthersFields: the zcp of two
// conversations writes one change at once — one keeping the Mate's words, the
// other what the agent was told — and neither loses the other's field; writers
// of different changes never wait on each other.
func TestHQChangeLedger_ConcurrentWritersKeepEachOthersFields(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := RecordHQChange(stateDir, HQChange{AppID: "app-1", Repo: "appsdev", Number: 6, Branch: "badge", Source: HQChangeFromOpen}); err != nil {
		t.Fatal(err)
	}
	const rounds = 40
	var wg sync.WaitGroup
	errs := make(chan error, 3*rounds)
	for i := range rounds {
		wg.Go(func() {
			errs <- UpsertHQChange(stateDir, "app-1", "appsdev", 6, func(c *HQChange, existed bool) error {
				if !existed {
					return errors.New("change #6 vanished")
				}
				c.Description = &ChangeDescription{Text: fmt.Sprintf("words %d", i), Change: 6}
				return nil
			})
		})
		wg.Go(func() {
			errs <- UpsertHQChange(stateDir, "app-1", "appsdev", 6, func(c *HQChange, existed bool) error {
				if !existed {
					return errors.New("change #6 vanished")
				}
				c.Told = &HQChangeTold{State: "open", Mergeability: "clean", Behind: i%2 == 0}
				return nil
			})
		})
		wg.Go(func() {
			errs <- RecordHQChange(stateDir, HQChange{AppID: "app-1", Repo: "appsdev", Number: 100 + i, Branch: fmt.Sprintf("b-%d", i)})
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	c, found, err := ReadHQChange(stateDir, "app-1", "appsdev", 6)
	if err != nil || !found {
		t.Fatalf("change #6: %v", err)
	}
	if c.Description == nil || c.Told == nil || c.Branch != "badge" {
		t.Errorf("a writer lost another's field: %+v", c)
	}
	listed, err := ListHQChanges(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != rounds+1 {
		t.Errorf("ledger holds %d changes, want %d", len(listed), rounds+1)
	}
}

// TestHQChangeLedger_HoldIsOneAtATime: a describe holds its change across its
// HQ calls; a pass that finds it held leaves it (no wait), and another describe
// waits for it up to its wait. Holding one change never holds another.
func TestHQChangeLedger_HoldIsOneAtATime(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	ctx := context.Background()
	release, err := HoldHQChange(ctx, stateDir, "app-1", "appsdev", 6, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := HoldHQChange(ctx, stateDir, "app-1", "appsdev", 6, 0); !errors.Is(err, ErrHQChangeBusy) {
		t.Errorf("a second hold with no wait = %v, want ErrHQChangeBusy", err)
	}
	other, err := HoldHQChange(ctx, stateDir, "app-1", "appsdev", 7, 0)
	if err != nil {
		t.Errorf("holding #6 held #7: %v", err)
	} else {
		other()
	}
	// A write of the held change is not a hold: it goes through.
	if err := RecordHQChange(stateDir, HQChange{AppID: "app-1", Repo: "appsdev", Number: 6, Branch: "badge"}); err != nil {
		t.Errorf("a write while the change is held: %v", err)
	}
	go func() {
		time.Sleep(50 * time.Millisecond)
		release()
	}()
	again, err := HoldHQChange(ctx, stateDir, "app-1", "appsdev", 6, 5*time.Second)
	if err != nil {
		t.Fatalf("a hold that waits for the first to let go: %v", err)
	}
	again()
}

// TestHQChangeLedger_MigrationFromPairsDedupsAcrossPairs: the first pass moves
// each pair's change on record — and the words kept for it — into the ledger,
// one entry per change however many pairs share its repository, and clears
// the pairs' fields; a second pass changes nothing. The branch a record names
// goes through branchOf, which maps the branch a released zcp kept.
func TestHQChangeLedger_MigrationFromPairsDedupsAcrossPairs(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	pair := func(dev string, ref *HQRepoRef) {
		t.Helper()
		if err := WriteServiceMeta(stateDir, &ServiceMeta{
			Hostname: dev, Mode: topology.PlanModeStandard, Stages: []topology.StageRef{{Hostname: dev + "stage"}},
			BootstrapSession: "test", BootstrappedAt: "2026-10-02", HQ: ref,
		}); err != nil {
			t.Fatal(err)
		}
	}
	pair("appsdev", &HQRepoRef{AppID: "app-1", Repo: "appsdev", Change: 2, Branch: "pool-previews"})
	pair("hqpool", &HQRepoRef{AppID: "app-1", Repo: "appsdev", Change: 2, Branch: "pool-previews",
		ChangeDescription: &ChangeDescription{Text: "the words", Change: 2}})
	pair("webpool", &HQRepoRef{AppID: "app-1", Repo: "appsdev"})
	pair("apidev", &HQRepoRef{AppID: "app-1", Repo: "apidev", Change: 4, Branch: "mate/p-mate"})
	branchOf := func(number int, branch string) string {
		if branch == "mate/p-mate" {
			return fmt.Sprintf("mate/p-mate/%d", number)
		}
		return branch
	}

	for range 2 {
		if err := MovePairChangesToLedger(stateDir, branchOf); err != nil {
			t.Fatal(err)
		}
	}

	listed, err := ListHQChanges(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(listed))
	for _, c := range listed {
		words := ""
		if c.Description != nil {
			words = c.Description.Text
		}
		got = append(got, fmt.Sprintf("%s#%d@%s[%s]%s", c.Repo, c.Number, c.Branch, words, c.Source))
	}
	want := []string{"apidev#4@mate/p-mate/4[]pair", "appsdev#2@pool-previews[the words]pair"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("ledger = %v, want %v", got, want)
	}
	metas, err := ListServiceMetas(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range metas {
		if m.HQ == nil || m.HQ.Repo == "" {
			t.Errorf("%s lost its repository: %+v", m.Hostname, m.HQ)
			continue
		}
		if m.HQ.Change != 0 || m.HQ.Branch != "" || m.HQ.ChangeDescription != nil {
			t.Errorf("%s still records a change: %+v", m.Hostname, m.HQ)
		}
	}
}

// TestHQChangeLedger_MigrationKeepsTheLedgersWords: a pair record a downgraded
// zcp wrote again moves in once more, but never writes over words or a branch
// the ledger already keeps for that change.
func TestHQChangeLedger_MigrationKeepsTheLedgersWords(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := RecordHQChange(stateDir, HQChange{AppID: "app-1", Repo: "appsdev", Number: 2, Branch: "pool-previews",
		Source: HQChangeFromOpen, Description: &ChangeDescription{Text: "newer words", Change: 2}}); err != nil {
		t.Fatal(err)
	}
	if err := WriteServiceMeta(stateDir, &ServiceMeta{
		Hostname: "appsdev", Mode: topology.PlanModeStandard, Stages: []topology.StageRef{{Hostname: "appsstage"}},
		BootstrapSession: "test", BootstrappedAt: "2026-10-02",
		HQ: &HQRepoRef{AppID: "app-1", Repo: "appsdev", Change: 2, ChangeDescription: &ChangeDescription{Text: "older words", Change: 2}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := MovePairChangesToLedger(stateDir, func(_ int, branch string) string { return branch }); err != nil {
		t.Fatal(err)
	}
	c, found, err := ReadHQChange(stateDir, "app-1", "appsdev", 2)
	if err != nil || !found {
		t.Fatalf("change #2: %v", err)
	}
	if c.Branch != "pool-previews" || c.Description == nil || c.Description.Text != "newer words" || c.Source != HQChangeFromOpen {
		t.Errorf("change #2 = %+v, want the ledger's branch, words and source kept", c)
	}
}
