package recipe

import (
	"fmt"
	"os"
	"testing"

	"github.com/zeropsio/zcp/internal/knowledge"
)

// TestMain is the B20 corpus guard (see internal/knowledge): many recipe-engine
// tests read the embedded recipe corpus, gitignored and synced via
// `zcp sync pull`. On a fresh clone/worktree it is absent and tests fail with
// content-shaped assertions that don't name the cause. Fail fast with one
// actionable message; CI pulls the corpus first so CI runs normally.
func TestMain(m *testing.M) {
	if !knowledge.SyncedCorpusPresent() {
		fmt.Fprintln(os.Stderr, knowledge.UnsyncedCorpusMessage)
		os.Exit(1)
	}
	os.Exit(m.Run())
}
