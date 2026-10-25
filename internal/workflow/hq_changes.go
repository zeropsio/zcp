package workflow

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// A Mate's changes in HQ are its repository's, never a pair's: an open change
// is (application, repository, branch), and pairs that share a repository —
// and leases, which have no pair at all — have one place to find it. zcp keeps
// what it knows of each change in a ledger of its own, one file per change,
// so the zcp of every conversation, and every subagent of one, writes
// different files: nothing here is a "current" change of a session.
//
//	<stateDir>/hq/changes/<appId>/<repo>/<number>.json   (0600)
//
// What became of a change is HQ's to say, in the Mate's own state; the ledger
// is what zcp recorded and what it last told the agent.

// hqChangesDir is the ledger's directory under the state dir.
const hqChangesDir = "hq/changes"

// hqChangeWriteWait bounds how long a write waits for another writer of the
// same change: a write is one read and one rename, never an HQ call.
const hqChangeWriteWait = 10 * time.Second

// The ways a change came into the ledger.
const (
	// HQChangeFromOpen: open-change opened it, or HQ answered it open.
	HQChangeFromOpen = "open-change"
	// HQChangeFromHanded: a person handed it to this Mate from a Mate that
	// is gone.
	HQChangeFromHanded = "handed"
	// HQChangeFromState: the Mate's own state named it open — a describe
	// found it with nothing recorded.
	HQChangeFromState = "state"
	// HQChangeFromPair: a pair's record held it (ServiceMeta.HQ.Change),
	// written before the ledger, or by a zcp downgraded since.
	HQChangeFromPair = "pair"
)

// ErrHQChangeBusy is HoldHQChange's answer when another holder keeps the
// change past the wait.
var ErrHQChangeBusy = errors.New("another step holds the change")

// HQChange is one of the Mate's changes in HQ as zcp keeps it.
type HQChange struct {
	AppID  string `json:"appId"`
	Repo   string `json:"repo"`
	Number int    `json:"number"`
	// Branch is the branch the change is on, by its short name, as HQ named
	// it; "" for one HQ answered without naming it.
	Branch string `json:"branch,omitempty"`
	// Title is the change's title as zcp last knew it, "" for none known.
	Title string `json:"title,omitempty"`
	// RecordedAt is when zcp first recorded it, RFC 3339.
	RecordedAt string `json:"recordedAt"`
	// Source is how it came into the ledger (HQChangeFrom*).
	Source string `json:"source,omitempty"`
	// Description is what the Mate wrote about the change while HQ did not
	// take the edit: there only until the change carries it.
	Description *ChangeDescription `json:"description,omitempty"`
	// Told is the change's standing as the agent was last told it — a pass
	// says it again only when it differs.
	Told *HQChangeTold `json:"told,omitempty"`
}

// HQChangeTold is a change's standing in HQ as last read from the Mate's own
// state, and so as last told: open, whether it is described at its head
// (Ready, nil from an HQ older than it), how it merges into `main` and whether
// `main` moved past it, and when HQ last changed it.
type HQChangeTold struct {
	State        string `json:"state,omitempty"`
	Ready        *bool  `json:"ready,omitempty"`
	Mergeability string `json:"mergeability,omitempty"`
	Behind       bool   `json:"behind,omitempty"`
	UpdatedAt    string `json:"updatedAt,omitempty"`
}

// hqChangeDir is the ledger directory of one repository, refusing a name that
// is no single path part.
func hqChangeDir(stateDir, appID, repo string) (string, error) {
	for _, part := range []string{appID, repo} {
		if part == "" || part == "." || part == ".." || strings.ContainsAny(part, `/\`+"\x00") {
			return "", fmt.Errorf("hq change ledger: %q is no application or repository name", part)
		}
	}
	return filepath.Join(stateDir, hqChangesDir, appID, repo), nil
}

// hqChangePath is the file of change number of appID's repo.
func hqChangePath(stateDir, appID, repo string, number int) (string, error) {
	if number <= 0 {
		return "", fmt.Errorf("hq change ledger: %d is no change number", number)
	}
	dir, err := hqChangeDir(stateDir, appID, repo)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, strconv.Itoa(number)+".json"), nil
}

// ReadHQChange is the ledger's change number of appID's repo; found is false
// when it keeps none.
func ReadHQChange(stateDir, appID, repo string, number int) (c HQChange, found bool, err error) {
	path, err := hqChangePath(stateDir, appID, repo, number)
	if err != nil {
		return HQChange{}, false, err
	}
	return readHQChangeFile(path)
}

func readHQChangeFile(path string) (HQChange, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return HQChange{}, false, nil
	}
	if err != nil {
		return HQChange{}, false, fmt.Errorf("read hq change %s: %w", path, err)
	}
	var c HQChange
	if err := json.Unmarshal(data, &c); err != nil {
		return HQChange{}, false, fmt.Errorf("decode hq change %s: %w", path, err)
	}
	return c, true, nil
}

// RecordHQChange records c in the ledger: a change it keeps already takes c's
// branch, title and source where c names one, and keeps its words and what
// the agent was told.
func RecordHQChange(stateDir string, c HQChange) error {
	return UpsertHQChange(stateDir, c.AppID, c.Repo, c.Number, func(kept *HQChange, _ bool) error {
		if c.Branch != "" {
			kept.Branch = c.Branch
		}
		if c.Title != "" {
			kept.Title = c.Title
		}
		if c.Source != "" {
			kept.Source = c.Source
		}
		if c.Description != nil {
			kept.Description = c.Description
		}
		if c.Told != nil {
			kept.Told = c.Told
		}
		return nil
	})
}

// UpsertHQChange reads change number of appID's repo, lets fn change it —
// existed false for one the ledger did not keep — and writes it, all while
// holding the change's write lock, so writers of one change never lose each
// other's fields. fn returning ErrSkipWrite writes nothing; any other error
// is returned. Writers of different changes never wait on each other.
func UpsertHQChange(stateDir, appID, repo string, number int, fn func(c *HQChange, existed bool) error) error {
	path, err := hqChangePath(stateDir, appID, repo, number)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("hq change ledger mkdir: %w", err)
	}
	release, err := lockHQChangeFile(hqChangeLockPath(path, "lock"), hqChangeWriteWait, nil)
	if err != nil {
		return fmt.Errorf("hq change #%d of %s: %w", number, repo, err)
	}
	defer release()
	kept, existed, err := readHQChangeFile(path)
	if err != nil {
		return err
	}
	if !existed {
		kept = HQChange{RecordedAt: time.Now().UTC().Format(time.RFC3339)}
	}
	if err := fn(&kept, existed); err != nil {
		if errors.Is(err, ErrSkipWrite) {
			return nil
		}
		return err
	}
	kept.AppID, kept.Repo, kept.Number = appID, repo, number
	return writeHQChangeFile(path, &kept)
}

// writeHQChangeFile writes c to path atomically, mode 0600.
func writeHQChangeFile(path string, c *HQChange) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("encode hq change: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".change-*.tmp")
	if err != nil {
		return fmt.Errorf("hq change temp: %w", err)
	}
	defer os.Remove(tmp.Name()) // best effort: gone after a successful rename
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("hq change chmod: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("hq change write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("hq change close: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("hq change rename: %w", err)
	}
	return nil
}

// ForgetHQChange drops change number of appID's repo from the ledger, once HQ
// says it is no longer open; one the ledger does not keep is no error.
func ForgetHQChange(stateDir, appID, repo string, number int) error {
	path, err := hqChangePath(stateDir, appID, repo, number)
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Dir(path)); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	release, err := lockHQChangeFile(hqChangeLockPath(path, "lock"), hqChangeWriteWait, nil)
	if err != nil {
		return fmt.Errorf("hq change #%d of %s: %w", number, repo, err)
	}
	defer release()
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("forget hq change #%d of %s: %w", number, repo, err)
	}
	return nil
}

// ListHQChanges is every change the ledger keeps, by application, repository
// and number. A file that cannot be read, or that names another change than
// its place, names none and is skipped.
func ListHQChanges(stateDir string) ([]HQChange, error) {
	if stateDir == "" {
		return nil, nil
	}
	root := filepath.Join(stateDir, hqChangesDir)
	apps, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read hq change ledger: %w", err)
	}
	var out []HQChange
	for _, app := range apps {
		if !app.IsDir() {
			continue
		}
		repos, err := os.ReadDir(filepath.Join(root, app.Name()))
		if err != nil {
			continue
		}
		for _, repo := range repos {
			if !repo.IsDir() {
				continue
			}
			out = append(out, readHQChangesIn(filepath.Join(root, app.Name(), repo.Name()), app.Name(), repo.Name())...)
		}
	}
	slices.SortFunc(out, func(a, b HQChange) int {
		return cmp.Or(cmp.Compare(a.AppID, b.AppID), cmp.Compare(a.Repo, b.Repo), cmp.Compare(a.Number, b.Number))
	})
	return out, nil
}

// readHQChangesIn reads the change files of one repository's directory.
func readHQChangesIn(dir, appID, repo string) []HQChange {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []HQChange
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".json")
		number, err := strconv.Atoi(name)
		if e.IsDir() || !ok || err != nil || number <= 0 || strconv.Itoa(number) != name {
			continue
		}
		c, found, err := readHQChangeFile(filepath.Join(dir, e.Name()))
		if err != nil || !found || c.AppID != appID || c.Repo != repo || c.Number != number {
			continue
		}
		out = append(out, c)
	}
	return out
}

// HoldHQChange holds change number of appID's repo for one step at a time: a
// describe holds it across its calls to HQ, so a pass never learns the
// change merged and forgets it — words and all — while the describe writes
// onto it. A holder waits up to wait for another to let go (0: not at all),
// then gives up with ErrHQChangeBusy; ctx ending gives up sooner. It is not
// the write lock: a holder writes the change as any writer does.
func HoldHQChange(ctx context.Context, stateDir, appID, repo string, number int, wait time.Duration) (release func(), err error) {
	path, err := hqChangePath(stateDir, appID, repo, number)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("hq change ledger mkdir: %w", err)
	}
	release, err = lockHQChangeFile(hqChangeLockPath(path, "hold"), wait, ctx.Done())
	if errors.Is(err, errHQChangeLockGivenUp) {
		return nil, ctx.Err()
	}
	return release, err
}

// hqChangeLockPath is the lock file of kind ("lock", "hold") beside a change's
// file: hidden, so the ledger never reads it as a change.
func hqChangeLockPath(path, kind string) string {
	return filepath.Join(filepath.Dir(path), "."+strings.TrimSuffix(filepath.Base(path), ".json")+"."+kind)
}

// errHQChangeLockGivenUp is lockHQChangeFile giving up because done closed.
var errHQChangeLockGivenUp = errors.New("gave up waiting for the change")

// lockHQChangeFile takes an exclusive lock on path, waiting up to wait, or
// until done closes (nil: never) — ErrHQChangeBusy past the wait,
// errHQChangeLockGivenUp once done closed.
func lockHQChangeFile(path string, wait time.Duration, done <-chan struct{}) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("hq change lock open: %w", err)
	}
	deadline := time.Now().Add(wait)
	for {
		held, err := tryLockExclusive(f)
		if err != nil {
			_ = f.Close()
			return nil, err
		}
		if held {
			return func() {
				unlockFile(f)
				_ = f.Close()
			}, nil
		}
		left := time.Until(deadline)
		if left <= 0 {
			_ = f.Close()
			return nil, ErrHQChangeBusy
		}
		select {
		case <-done:
			_ = f.Close()
			return nil, errHQChangeLockGivenUp
		case <-time.After(min(flockInterval, left)):
		}
	}
}

// MovePairChangesToLedger moves every pair's change on record
// (ServiceMeta.HQ.Change, .Branch, .ChangeDescription) into the ledger — one
// change however many pairs that share its repository record it — and clears
// the pair's fields. branchOf maps the branch a record names (a released
// zcp's own branch to the change's). Words kept for the change go with it;
// a change the ledger keeps already keeps its branch, words and source. A
// pair whose change could not be recorded keeps its fields for the next
// call. Idempotent: every reader of the ledger runs it first, so a record a
// downgraded zcp wrote moves in too.
func MovePairChangesToLedger(stateDir string, branchOf func(number int, branch string) string) error {
	metas, err := ListServiceMetas(stateDir)
	if err != nil {
		return fmt.Errorf("move pair changes: %w", err)
	}
	var errs []error
	for _, m := range metas {
		ref := m.HQ
		if ref == nil || (ref.Change == 0 && ref.Branch == "" && ref.ChangeDescription == nil) {
			continue
		}
		number := ref.Change
		if number != 0 {
			words := ref.ChangeDescription
			if words != nil && words.Change != 0 && words.Change != number {
				words = nil
			}
			branch := branchOf(number, ref.Branch)
			if err := UpsertHQChange(stateDir, ref.AppID, ref.Repo, number, func(c *HQChange, existed bool) error {
				if !existed {
					c.Source = HQChangeFromPair
				}
				if c.Branch == "" {
					c.Branch = branch
				}
				if c.Description == nil && words != nil {
					kept := *words
					kept.Change = number
					c.Description = &kept
				}
				return nil
			}); err != nil {
				errs = append(errs, err)
				continue
			}
		}
		if err := UpsertServiceMeta(stateDir, m.Hostname, func(meta *ServiceMeta, existed bool) error {
			if !existed || meta.HQ == nil || meta.HQ.Change != number {
				return ErrSkipWrite
			}
			meta.HQ.Change, meta.HQ.Branch, meta.HQ.ChangeDescription = 0, "", nil
			return nil
		}); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// ReleasedBranchOf is the branch a pair's record names for its change number,
// as the ledger keeps it: the branch a released zcp kept the work on — the
// Mate's own, `mate/<project id>`, which HQ binds no change to — is the
// change's `mate/<project id>/<number>`, as HQ bound that zcp's changes,
// opened naming no branch; any other is the change's as recorded.
func ReleasedBranchOf(number int, branch string) string {
	if rest, ok := strings.CutPrefix(branch, "mate/"); ok && rest != "" && !strings.Contains(rest, "/") {
		return branch + "/" + strconv.Itoa(number)
	}
	return branch
}

// HQChanges is every change the ledger keeps, the pairs' records moved into it
// first (MovePairChangesToLedger, ReleasedBranchOf): what every reader of the
// Mate's changes reads. A pair record that could not move stays for the next
// read; what moved is listed either way.
func HQChanges(stateDir string) ([]HQChange, error) {
	moveErr := MovePairChangesToLedger(stateDir, ReleasedBranchOf)
	changes, err := ListHQChanges(stateDir)
	if err != nil {
		return nil, errors.Join(moveErr, err)
	}
	return changes, moveErr
}

// PeekHQChanges is HQChanges for a reader that writes nothing (the pool
// migration's read side, docs/spec-workflows.md §13 PM-1): every change the
// ledger keeps, with each pair's change on record that has not moved yet
// folded in as MovePairChangesToLedger would move it — its branch by
// ReleasedBranchOf, its words — in memory only; neither the ledger nor any
// meta is written.
func PeekHQChanges(stateDir string) ([]HQChange, error) {
	changes, err := ListHQChanges(stateDir)
	if err != nil {
		return nil, err
	}
	metas, err := ListServiceMetas(stateDir)
	if err != nil {
		return changes, fmt.Errorf("peek pair changes: %w", err)
	}
	for _, m := range metas {
		ref := m.HQ
		if ref == nil || ref.Change == 0 {
			continue
		}
		i := slices.IndexFunc(changes, func(c HQChange) bool {
			return c.AppID == ref.AppID && c.Repo == ref.Repo && c.Number == ref.Change
		})
		if i < 0 {
			changes = append(changes, HQChange{AppID: ref.AppID, Repo: ref.Repo, Number: ref.Change, Source: HQChangeFromPair})
			i = len(changes) - 1
		}
		c := &changes[i]
		if c.Branch == "" {
			c.Branch = ReleasedBranchOf(ref.Change, ref.Branch)
		}
		if words := ref.ChangeDescription; c.Description == nil && words != nil && (words.Change == 0 || words.Change == ref.Change) {
			kept := *words
			kept.Change = ref.Change
			c.Description = &kept
		}
	}
	slices.SortFunc(changes, func(a, b HQChange) int {
		return cmp.Or(cmp.Compare(a.AppID, b.AppID), cmp.Compare(a.Repo, b.Repo), cmp.Compare(a.Number, b.Number))
	})
	return changes, nil
}
