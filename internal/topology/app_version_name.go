package topology

import "strings"

// AppVersionPrefix starts the name of every app version zcp creates: a
// commit deploy's, named by its whole commit, and a Review build's
// candidate's, named by a key that is never a commit id.
const AppVersionPrefix = "zcp "

// AppVersionCommit is the one decision whether an app version's name is a
// commit deploy's: AppVersionPrefix and a whole commit id. It answers that
// commit, and "" for every other name: a Review build's candidate and every
// version zcp did not commit-deploy (docs/spec-mate.md §1.7).
func AppVersionCommit(name string) string {
	commit, ok := strings.CutPrefix(name, AppVersionPrefix)
	if !ok || !IsCommitID(commit) {
		return ""
	}
	return commit
}
