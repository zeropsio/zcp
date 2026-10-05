package ops

import (
	"strconv"
	"strings"
)

// zcpCommitTrailer marks every commit zcp writes into a pair's history itself
// — a delivery's commit of the deployed tree, and the one commit that carries
// newer work onto a fresh base — so what a change holds is never read from
// zcp's own words: those are the work session's, the same for every
// repository the session touched.
const zcpCommitTrailer = "Delivered-by: zcp"

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
	changeSubjectMarker = "ZCP_SUBJECT:"
	changeFileMarker    = "ZCP_FILE:"
	changeFilesMarker   = "ZCP_FILES:"
)

// BuildChangeWorkCommand reads what the checkout's HEAD holds beyond `main`
// as last fetched (origin/main; the whole history when there is none): the
// subject of the newest commit there that is not a merge, not zcp's
// (zcpCommitTrailer) and not the pair's `zcp init`; and only when there is no
// such commit, the files that differ from `main` (ReadChangeWork). It reaches
// nothing: the delivery or push before it has fetched.
func BuildChangeWorkCommand(workingDir string) string {
	return "cd " + shellQuote(workingDir) + " && " +
		`if git rev-parse -q --verify refs/remotes/origin/main >/dev/null; then base=origin/main; range=origin/main..HEAD; ` +
		`else base=$(git hash-object -t tree /dev/null); range=HEAD; fi && ` +
		`s=$(git log --no-merges --invert-grep --grep=` + shellQuote("^"+zcpCommitTrailer+"$") + ` --grep='^zcp init$' -n 1 --format=%s "$range") && ` +
		`if [ -n "$s" ]; then printf '` + changeSubjectMarker + `%s\n' "$s"; ` +
		`else git -c core.quotePath=false diff --no-renames --name-status "$base" HEAD | ` +
		`awk -F '\t' 'NR<=2{print "` + changeFileMarker + `" $2} {k[substr($1,1,1)]=1} END{v=""; for(x in k) v=v x; print "` + changeFilesMarker + `" NR ":" v}'; fi`
}

// ReadChangeWork reads BuildChangeWorkCommand's output.
func ReadChangeWork(output string) ChangeWork {
	var work ChangeWork
	for line := range strings.SplitSeq(output, "\n") {
		switch {
		case strings.HasPrefix(line, changeSubjectMarker):
			work.Subject = strings.TrimSpace(strings.TrimPrefix(line, changeSubjectMarker))
		case strings.HasPrefix(line, changeFileMarker):
			work.Files = append(work.Files, strings.TrimPrefix(line, changeFileMarker))
		case strings.HasPrefix(line, changeFilesMarker):
			count, kinds, _ := strings.Cut(strings.TrimSpace(strings.TrimPrefix(line, changeFilesMarker)), ":")
			work.Count, _ = strconv.Atoi(count)
			work.Kinds = kinds
		}
	}
	return work
}
