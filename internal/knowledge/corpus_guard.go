package knowledge

// CorpusFloor is the minimum number of embedded recipes a synced checkout
// carries. The recipe/guide markdown is gitignored and pulled via
// `zcp sync pull` before a build; a fresh clone or git worktree that hasn't
// pulled embeds only the force-tracked mailpit recipe, so an embedded recipe
// count below this floor means the corpus is unsynced.
const CorpusFloor = 5

// SyncedCorpusPresent reports whether the embedded recipe corpus looks synced.
// Test guards use it to turn the dozens of cryptic content-shaped failures a
// fresh checkout produces into one actionable "run zcp sync pull" message.
func SyncedCorpusPresent() bool {
	store, err := GetEmbeddedStore()
	if err != nil {
		return false
	}
	return len(store.ListRecipes()) >= CorpusFloor
}

// UnsyncedCorpusMessage is the actionable remedy shown when the corpus is absent.
const UnsyncedCorpusMessage = "knowledge corpus not synced — run `zcp sync pull` before testing " +
	"(a fresh clone/worktree embeds only the mailpit recipe; CI pulls the corpus first)."
