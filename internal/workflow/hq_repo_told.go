package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// What zcp last told the agent of a repository's mirror on GitHub, one file
// per repository — the repository's, never a pair's, as its changes are:
//
//	<stateDir>/hq/repos/<appId>/<repo>.json   (0600)
//
// A pass says a mirror or a divergence once, when it differs from what the
// file holds, and writes what it said in the same hold, so passes running at
// once never both say it.

// hqReposDir is the told records' directory under the state dir.
const hqReposDir = "hq/repos"

// HQRepoTold is what the agent was last told of a repository HQ mirrors to
// GitHub.
type HQRepoTold struct {
	// Mirror is the GitHub repository, as owner/name, the agent was told HQ
	// mirrors the repository to; "" while it was told none, or the mirror
	// ended since.
	Mirror string `json:"mirror,omitempty"`
	// DivergedSince is the divergence of GitHub's default branch and HQ's
	// `main` the agent was told of, as HQ dates it; "" while none, or they
	// are in step again.
	DivergedSince string `json:"divergedSince,omitempty"`
}

// hqRepoToldPath is the told record of appID's repo, refusing a name that is
// no single path part.
func hqRepoToldPath(stateDir, appID, repo string) (string, error) {
	for _, part := range []string{appID, repo} {
		if part == "" || part == "." || part == ".." || strings.ContainsAny(part, `/\`+"\x00") {
			return "", fmt.Errorf("hq repository record: %q is no application or repository name", part)
		}
	}
	return filepath.Join(stateDir, hqReposDir, appID, repo+".json"), nil
}

// ReadHQRepoTold is what the agent was told of appID's repo, holding nothing:
// nothing yet for no record, a record that does not decode, or a name that
// is none.
func ReadHQRepoTold(stateDir, appID, repo string) HQRepoTold {
	var told HQRepoTold
	path, err := hqRepoToldPath(stateDir, appID, repo)
	if err != nil {
		return told
	}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &told)
	}
	return told
}

// TellHQRepo reads what the agent was told of appID's repo — nothing yet
// for none, or for a record that does not decode — lets fn change it, and
// writes it, all while holding the record, so of two passes at once only one
// says a thing. fn returning ErrSkipWrite writes nothing; any other error is
// returned.
func TellHQRepo(stateDir, appID, repo string, fn func(told *HQRepoTold) error) error {
	path, err := hqRepoToldPath(stateDir, appID, repo)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("hq repository record mkdir: %w", err)
	}
	release, err := lockHQChangeFile(hqChangeLockPath(path, "lock"), hqChangeWriteWait, nil)
	if err != nil {
		return fmt.Errorf("hq repository record of %s: %w", repo, err)
	}
	defer release()
	var told HQRepoTold
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return fmt.Errorf("read hq repository record %s: %w", path, err)
	default:
		// A record cut short says nothing was told: the next say is one
		// more, never none.
		_ = json.Unmarshal(data, &told)
	}
	if err := fn(&told); err != nil {
		if errors.Is(err, ErrSkipWrite) {
			return nil
		}
		return err
	}
	// The temp file atomicWriteJSON renames into place is 0600 already.
	if err := atomicWriteJSON(filepath.Dir(path), ".repo-*.tmp", path, told); err != nil {
		return fmt.Errorf("write hq repository record: %w", err)
	}
	return nil
}
