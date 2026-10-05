package ops

import (
	"strconv"
	"strings"
)

// zcpCommitTrailer marks every commit zcp writes into a pair's history itself
// — a delivery's commit of the deployed tree, and the one commit that carries
// newer work onto a fresh base — so what a change holds is never read from
// zcp's own words: those are the work session's, the same for every
// repository the session touched. zcpCommitMarker is the trailer's key, which
// the baseline commit the briefing asks for carries too ("Zcp-Commit:
// baseline"): it is the starting point, not a change's work.
const (
	zcpCommitMarker  = "Zcp-Commit: "
	zcpCommitTrailer = zcpCommitMarker + "delivery"
)

// zcpCommitMessage is message as zcp commits under it: the trailer after it.
func zcpCommitMessage(message string) string {
	return message + "\n\n" + zcpCommitTrailer
}

// ChangeWork is what a change holds beyond `main`, as a bare change — pushed,
// not described yet — is titled by it (BuildChangeWorkCommand).
type ChangeWork struct {
	// Subject is the subject of the newest commit beyond `main` the Mate
	// wrote — never a merge, never zcp's own — "" when there is none.
	Subject string
	// Count is how many files differ from `main`; Files the first two of
	// them as git names them, path order. Read only when Subject is "".
	Count int
	Files []string
	// Kinds is each kind of difference among them once — A added, M
	// modified, D deleted, T its type changed — in no particular order.
	Kinds string
}

const (
	changeCommitMarker = "ZCP_COMMIT:"
	changeBaseMarker   = "ZCP_BASE:"
	changeFileMarker   = "ZCP_FILE:"
	changeFilesMarker  = "ZCP_FILES:"
)

// changeCommitsRead bounds how many commits beyond `main` are read for one
// the Mate wrote.
const changeCommitsRead = 50

// BuildChangeWorkCommand reads what the checkout's HEAD holds beyond `main`
// as last fetched (origin/main; the whole history when there is none), for
// ReadChangeWork:
//
//   - the newest commits there that are not merges, not marked as zcp's
//     (zcpCommitMarker), not the pair's `zcp init` and not a baseline commit
//     under the words the briefing gave it before the marker, with their
//     parents —
//     history that already landed left out: refs/zcp/landed/*, and each of
//     landedHeads the checkout has (HQ's landedHead of a merged change); a
//     checkout that took a squash in by an ordinary merge still holds the
//     commits it squashed;
//   - the commits a starting point sits on (ZCP_BASE): the pair's `zcp
//     init` and each join of the repository's base, so a baseline commit
//     made before the marker existed, under any words, is not taken for the
//     Mate's work;
//   - the files that differ from `main`.
//
// It reaches nothing: the delivery or push before it has fetched.
func BuildChangeWorkCommand(workingDir string, landedHeads []string) string {
	var landed strings.Builder
	for _, head := range landedHeads {
		if isHexSHA(head) {
			landed.WriteString(" " + head)
		}
	}
	return "cd " + shellQuote(workingDir) + " && " +
		`if git rev-parse -q --verify refs/remotes/origin/main >/dev/null; then base=origin/main; range=origin/main..HEAD; ` +
		`else base=$(git hash-object -t tree /dev/null); range=HEAD; fi && ` +
		`landed=$(for h in` + landed.String() + `; do git rev-parse -q --verify "$h^{commit}"; done) ; ` +
		`git log --max-parents=0 --format='` + changeBaseMarker + `%H' --grep='^zcp init$' HEAD && ` +
		`git log --merges --format='` + changeBaseMarker + `%H' --grep="^Join the repository's base\$" HEAD && ` +
		`git log --no-merges --invert-grep --grep=` + shellQuote("^"+zcpCommitMarker) + ` --grep='^zcp init$' --grep='^baseline commit$' -n ` + strconv.Itoa(changeCommitsRead) +
		` --format='` + changeCommitMarker + `%P%x1f%s' "$range" --not --glob='refs/zcp/landed/*' $landed && ` +
		`git -c core.quotePath=false diff --no-renames --name-status "$base" HEAD | ` +
		`awk -F '\t' 'NR<=2{print "` + changeFileMarker + `" $2} {k[substr($1,1,1)]=1} END{v=""; for(x in k) v=v x; print "` + changeFilesMarker + `" NR ":" v}'`
}

// isHexSHA reports whether s is a full git object name.
func isHexSHA(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return true
}

// ReadChangeWork reads BuildChangeWorkCommand's output: the subject of the
// newest commit the Mate wrote is the newest one read that does not sit on a
// starting point.
func ReadChangeWork(output string) ChangeWork {
	var (
		work    ChangeWork
		bases   = map[string]bool{}
		commits []string
	)
	for line := range strings.SplitSeq(output, "\n") {
		switch {
		case strings.HasPrefix(line, changeBaseMarker):
			bases[strings.TrimSpace(strings.TrimPrefix(line, changeBaseMarker))] = true
		case strings.HasPrefix(line, changeCommitMarker):
			commits = append(commits, strings.TrimPrefix(line, changeCommitMarker))
		case strings.HasPrefix(line, changeFileMarker):
			work.Files = append(work.Files, strings.TrimPrefix(line, changeFileMarker))
		case strings.HasPrefix(line, changeFilesMarker):
			count, kinds, _ := strings.Cut(strings.TrimSpace(strings.TrimPrefix(line, changeFilesMarker)), ":")
			work.Count, _ = strconv.Atoi(count)
			work.Kinds = kinds
		}
	}
	for _, commit := range commits {
		parent, subject, _ := strings.Cut(commit, "\x1f")
		if !bases[strings.TrimSpace(parent)] {
			work.Subject = strings.TrimSpace(subject)
			break
		}
	}
	return work
}
