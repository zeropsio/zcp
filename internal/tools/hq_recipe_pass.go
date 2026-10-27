package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/zeropsio/zcp/internal/runtime"
)

// One group recipe pass runs at a time on a Mate (groupRecipeOutcome). A
// pass reads the Mate's changes and main of the recipe repository, composes,
// and only then opens or pushes its change, so two at once both read HQ from
// before either change existed: on 2026-10-27 Core landed the first pass's
// change onto Mate Web's empty recipe repository, and the second, composed
// against that empty main, pushed a second root commit onto a change of its
// own that HQ judged unrelated to main and could never land. Passes start
// together within one zcp — its start (GroupRecipeAtStart) beside the
// agent's ask — and across several, since a Mate runs a zcp per
// conversation and each start is a pass. So the hold is an exclusive file
// lock in the HQ state directory, which every zcp of the Mate shares and two
// passes in one zcp each open on their own, held for the pass alone.

// recipePassLockFile is the lock, beside recipeRequiredFile: a name with a
// dash, which no hostname has.
const recipePassLockFile = "group-recipe.lock"

// recipePassWait bounds a pass's wait for another when its call sets no
// sooner end: a pass's HQ calls each end, so one held longer is stuck, and
// the next pass tries again.
const recipePassWait = 2 * time.Minute

// recipePassPoll is how often a waiting pass tries the lock again.
const recipePassPoll = 100 * time.Millisecond

// errRecipePassRunning is another pass holding the recipe past the wait.
var errRecipePassRunning = errors.New("another group recipe pass is running on this Mate")

// holdRecipePass takes the Mate's one recipe pass, waiting for another to
// finish until ctx ends or recipePassWait passes — errRecipePassRunning
// then. release lets go. A holder never takes it again while it has it: a
// second hold in the same process waits on the first. Outside a Mate no pass
// reaches HQ, and there is nothing to hold.
func holdRecipePass(ctx context.Context, rt runtime.Info, stateDir string) (release func(), err error) {
	if !rt.InContainer || !rt.MateEnabled || stateDir == "" {
		return func() {}, nil
	}
	dir := filepath.Join(stateDir, hqStateDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("recipe pass lock: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(dir, recipePassLockFile), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("recipe pass lock: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, recipePassWait)
	defer cancel()
	for {
		held, err := tryLockFile(f)
		switch {
		case err != nil:
			_ = f.Close()
			return nil, fmt.Errorf("recipe pass lock: %w", err)
		case held:
			return func() {
				_ = unlockFile(f)
				_ = f.Close()
			}, nil
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, errRecipePassRunning
		case <-time.After(recipePassPoll):
		}
	}
}

// recipePassHeldLine is the line of a pass that did not get the recipe:
// another pass kept it, or its lock could not be taken.
func recipePassHeldLine(err error) string {
	if errors.Is(err, errRecipePassRunning) {
		return "another group recipe pass is running on this Mate — zcp's start, or another conversation's — and did not finish in time, so this one changed nothing; the next pass reads what it leaves"
	}
	return fmt.Sprintf("the group recipe is not proposed yet (%v) — retrying on the next pass.", err)
}
