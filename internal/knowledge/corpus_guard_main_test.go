package knowledge_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/zeropsio/zcp/internal/knowledge"
)

// TestMain is the B20 corpus guard: nearly every test in this package reads
// the embedded recipe/guide corpus, which is gitignored and synced via
// `zcp sync pull`. On a fresh clone/worktree the corpus is absent and the
// tests fail with dozens of content-shaped assertions ("should contain Go
// runtime guide, got: ") that don't name the cause. Fail fast with one
// actionable message instead. CI pulls the corpus first, so CI runs normally.
func TestMain(m *testing.M) {
	if !knowledge.SyncedCorpusPresent() {
		fmt.Fprintln(os.Stderr, knowledge.UnsyncedCorpusMessage)
		os.Exit(1)
	}
	os.Exit(m.Run())
}
