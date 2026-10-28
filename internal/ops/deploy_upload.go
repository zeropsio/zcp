package ops

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// A working-tree push measures what its upload carries inside its own SSH
// call, right before zcli starts, and waits for the upload by that size
// (docs/spec-workflows.md §8 DM-9): base plus perMiB for every MiB, at most
// cap. A push still uploading at its budget is stopped with no build and told
// as an upload that did not finish — slow, never a network failure.
const (
	uploadBudgetBase   = 5 * time.Minute
	uploadBudgetPerMiB = time.Second
	uploadBudgetCap    = 60 * time.Minute

	// pushCallCeiling bounds a push's SSH call as a whole: the remote side
	// stops an upload at its budget, at most uploadBudgetCap, and what runs
	// before zcli starts — the repo/HEAD ensure, the crew save, the
	// measurement — gets the 5 min every other foreground SSH call has.
	pushCallCeiling = uploadBudgetCap + 5*time.Minute

	// uploadWarnBytes is the upload a result warns about: past it, an upload
	// takes minutes, and its size is usually tracked assets the build does
	// not need.
	uploadWarnBytes = 256 << 20

	// uploadUnfinishedExit is the push call's exit status when its upload was
	// still running at its budget (timeout(1)'s status).
	uploadUnfinishedExit = 124

	// The push's report lines: what the upload carries, its largest
	// top-level paths, and the mark of an upload stopped at its budget.
	uploadReportMark     = "ZCP_UPLOAD"
	uploadTopMark        = "ZCP_UPLOAD_TOP"
	uploadUnfinishedMark = "ZCP_UPLOAD_UNFINISHED"
	uploadTopShown       = 3

	// uploadUnfinishedPhrase is in every message of an upload stopped before
	// it finished, the signal transport:upload-unfinished keys on.
	uploadUnfinishedPhrase = "did not finish uploading"

	// zcliClosedPipe is zcli's error when the connection carrying its upload
	// closed while it still sent the package (io.ErrClosedPipe).
	zcliClosedPipe = "read/write on closed pipe"
)

// uploadUnfinishedSuggestion is what an upload stopped before it finished
// suggests: a smaller upload, through zerops_deploy — never a push around it
// with zcp's own key.
const uploadUnfinishedSuggestion = "Nothing is building from this deploy. The upload was slow, not broken: make it smaller " +
	`(zerops_knowledge uri="zerops://themes/deploy-upload") and deploy again with zerops_deploy. ` +
	"zerops_deploy is the only push from here: never copy ZCP_API_KEY into a container or `zcli login` with it to push around it; " +
	"if the upload cannot be made smaller, tell the person what blocks the deploy."

// UploadSize is what a working-tree push uploaded, as it measured it in the
// source container right before zcli started (docs/spec-workflows.md §8
// DM-9): the archive's files and, for a self-deploy, the .git directory zcli
// adds to it.
type UploadSize struct {
	// Bytes is the archive's: the working directory's tracked files less
	// those `.gitattributes` marks export-ignore, plus its untracked files
	// .gitignore does not exclude.
	Bytes int64 `json:"bytes"`
	// Files counts the tracked files among them.
	Files int `json:"files"`
	// UntrackedBytes is the untracked files' part of Bytes.
	UntrackedBytes int64 `json:"untrackedBytes,omitempty"`
	// GitBytes is the .git directory a self-deploy uploads beside the archive.
	GitBytes int64 `json:"gitBytes,omitempty"`
	// Largest are the archive's largest top-level paths, largest first.
	Largest []UploadPath `json:"largest,omitempty"`
	// Summary says all of it in one line.
	Summary string `json:"summary"`
}

// UploadPath is one top-level path of an upload: a directory (with its
// trailing slash) or a file.
type UploadPath struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
}

// uploadBudget is how long a push waits for its upload: base plus perMiB for
// every MiB it carries, at most cap.
type uploadBudget struct {
	base, perMiB, cap time.Duration
}

func defaultUploadBudget() uploadBudget {
	return uploadBudget{base: uploadBudgetBase, perMiB: uploadBudgetPerMiB, cap: uploadBudgetCap}
}

// uploadAwk sums the measurement's tagged lines: A — `git check-attr
// export-ignore` of each tracked path and directory, a file under a marked
// directory left out as git archive leaves it; T — `git ls-tree -l` of each tracked
// file; U — `du -sk` of each untracked path .gitignore does not exclude; G —
// `du -sk` of a self-deploy's .git. It prints the report lines with the
// budget the upload gets. POSIX awk only: mawk, gawk and busybox run it.
const uploadAwk = `function add(b, p,   i, d) {
	total += b
	i = index(p, "/")
	d = (i > 0) ? substr(p, 1, i) : p
	size[d] += b
}
function ignored(p,   pre, rest, i) {
	if (p in skip) return 1
	pre = ""
	rest = p
	while ((i = index(rest, "/")) > 0) {
		pre = pre substr(rest, 1, i - 1)
		if (pre in skip) return 1
		pre = pre "/"
		rest = substr(rest, i + 1)
	}
	return 0
}
$1 == "A" {
	i = index($0, ": export-ignore: ")
	if (i > 0 && substr($0, i + 17) == "set") skip[substr($0, 3, i - 3)] = 1
	next
}
$1 == "T" {
	t = index($0, "\t")
	if ($3 == "blob" && t > 0) {
		p = substr($0, t + 1)
		if (!ignored(p)) { files++; add($5 + 0, p) }
	}
	next
}
$1 == "U" {
	t = index($0, "\t")
	if (t > 0) { b = $2 * 1024; untracked += b; add(b, substr($0, t + 1)) }
	next
}
$1 == "G" { git += $2 * 1024; next }
END {
	budget = base + int((total + git) / 1048576 * permib)
	if (budget > cap) budget = cap
	printf "ZCP_UPLOAD files=%d bytes=%.0f untracked=%.0f git=%.0f budget=%d\n", files, total, untracked, git, budget
	for (k = 0; k < 3; k++) {
		best = ""
		bv = -1
		for (d in size) if (!(d in used) && size[d] > bv) { bv = size[d]; best = d }
		if (best == "") break
		used[best] = 1
		printf "ZCP_UPLOAD_TOP %.0f %s\n", bv, best
	}
}`

// uploadMeasureCommand prints what the push about to start uploads and sets
// zcp_budget, in seconds, for the wait on its upload. It runs in the working
// directory, reads git and the file system only, and never fails the push:
// whatever it cannot read counts as nothing, and an unreadable measurement
// leaves the base budget.
func uploadMeasureCommand(includeGit bool, budget uploadBudget) string {
	// git archive skips a directory marked export-ignore as a whole, which
	// check-attr reports on the directory alone: the trees (-t) are asked too.
	listing := `git ls-tree -r -t --name-only HEAD | git check-attr --stdin export-ignore | sed 's/^/A /'; ` +
		`git ls-tree -r -l HEAD | sed 's/^/T /'; ` +
		`git ls-files -o --exclude-standard --directory | while IFS= read -r p; do du -sk -- "$p"; done | sed 's/^/U /'; `
	if includeGit {
		listing += `du -sk -- "$(git rev-parse --git-dir)" | sed 's/^/G /'; `
	}
	base := int(budget.base / time.Second)
	measure := fmt.Sprintf(`zcp_up=$( { %s} < /dev/null 2>/dev/null | awk -v base=%d -v permib=%g -v cap=%d %s )`,
		listing, base, budget.perMiB.Seconds(), int(budget.cap/time.Second), shellQuote(uploadAwk))
	return fmt.Sprintf(`{ %s; printf '%%s\n' "$zcp_up"; zcp_budget=$(printf '%%s\n' "$zcp_up" | sed -n 's/^%s .* budget=\([0-9][0-9]*\).*/\1/p'); [ -n "$zcp_budget" ] || zcp_budget=%d; }`,
		measure, uploadReportMark, base)
}

// uploadReport is what a push's output says of its upload.
type uploadReport struct {
	// size is nil when the push printed no measurement (a deploy of a
	// commit, or a push older than the measurement).
	size *UploadSize
	// budget is how long the push waited for its upload.
	budget time.Duration
	// unfinished marks an upload stopped at its budget.
	unfinished bool
}

// splitUploadReport reads the push's report lines out of output and returns
// the rest — zcli's own output — as it was.
func splitUploadReport(output string) (uploadReport, string) {
	var (
		report uploadReport
		size   UploadSize
		seen   bool
		rest   strings.Builder
	)
	for line := range strings.Lines(output) {
		text := strings.TrimRight(line, "\r\n")
		switch {
		case strings.HasPrefix(text, uploadUnfinishedMark):
			report.unfinished = true
			if report.budget == 0 {
				if s, ok := markField(text, "budget"); ok {
					report.budget = time.Duration(s) * time.Second
				}
			}
		case strings.HasPrefix(text, uploadTopMark+" "):
			n, path, ok := strings.Cut(strings.TrimPrefix(text, uploadTopMark+" "), " ")
			if b, err := strconv.ParseInt(n, 10, 64); ok && err == nil && path != "" {
				size.Largest = append(size.Largest, UploadPath{Path: path, Bytes: b})
			}
		case strings.HasPrefix(text, uploadReportMark+" "):
			seen = true
			size.Bytes, _ = markField(text, "bytes")
			files, _ := markField(text, "files")
			size.Files = int(files)
			size.UntrackedBytes, _ = markField(text, "untracked")
			size.GitBytes, _ = markField(text, "git")
			if s, ok := markField(text, "budget"); ok {
				report.budget = time.Duration(s) * time.Second
			}
		default:
			rest.WriteString(line)
		}
	}
	if seen {
		size.Summary = size.summary()
		report.size = &size
	}
	return report, rest.String()
}

// markField is the number a report line carries as key=<n>.
func markField(line, key string) (int64, bool) {
	for field := range strings.FieldsSeq(line) {
		if v, ok := strings.CutPrefix(field, key+"="); ok {
			n, err := strconv.ParseInt(v, 10, 64)
			return n, err == nil
		}
	}
	return 0, false
}

// summary says what the upload carried in one line: "1.3 GB in 14604 files,
// largest: app/ 1.2 GB, sessions/ 93 MB; plus the repository, .git 1.9 GB".
func (u UploadSize) summary() string {
	var b strings.Builder
	b.WriteString(formatBytes(u.Bytes))
	if u.Files > 0 {
		fmt.Fprintf(&b, " in %d files", u.Files)
	}
	if u.UntrackedBytes > 0 {
		fmt.Fprintf(&b, " (%s of it untracked files)", formatBytes(u.UntrackedBytes))
	}
	if len(u.Largest) > 0 {
		parts := make([]string, 0, len(u.Largest))
		for _, p := range u.Largest {
			parts = append(parts, p.Path+" "+formatBytes(p.Bytes))
		}
		b.WriteString(", largest: " + strings.Join(parts, ", "))
	}
	if u.GitBytes > 0 {
		fmt.Fprintf(&b, "; plus the repository, .git %s", formatBytes(u.GitBytes))
	}
	return b.String()
}

// formatBytes writes n in decimal units: "1.3 GB", "93 MB", "4.2 MB", "512 B".
func formatBytes(n int64) string {
	units := []struct {
		size float64
		name string
	}{{1e9, "GB"}, {1e6, "MB"}, {1e3, "KB"}}
	for _, u := range units {
		if v := float64(n) / u.size; v >= 1 {
			if v >= 10 && u.name != "GB" {
				return fmt.Sprintf("%.0f %s", v, u.name)
			}
			return fmt.Sprintf("%.1f %s", v, u.name)
		}
	}
	return fmt.Sprintf("%d B", n)
}

// uploadWhat names what an upload carried in a message, or "its package" when
// the push measured nothing.
func uploadWhat(size *UploadSize) string {
	if size == nil {
		return "its package"
	}
	return size.Summary
}

// largeUploadWarning is the result's warning about an upload past
// uploadWarnBytes: what it carried, and what does and does not make it
// smaller. "" for a smaller upload or none measured.
func largeUploadWarning(size *UploadSize) string {
	if size == nil || size.Bytes+size.GitBytes < uploadWarnBytes {
		return ""
	}
	return "This deploy uploaded " + size.Summary + ". An upload this size takes minutes: a zcli push carries the source's git snapshot " +
		"(a self-deploy its .git too), and .deployignore and deployFiles act after the build, so they do not make it smaller. " +
		`zerops_knowledge uri="zerops://themes/deploy-upload" says what does.`
}

// withUploadWarning is warnings with largeUploadWarning's, when it has one.
func withUploadWarning(warnings []string, size *UploadSize) []string {
	if warn := largeUploadWarning(size); warn != "" {
		return append(warnings, warn)
	}
	return warnings
}

// stdinWithinExecer is an SSHDeployer whose stdin call takes a ceiling of its
// own (platform.SystemSSHDeployer); one without it bounds the push at its
// usual ceiling.
type stdinWithinExecer interface {
	ExecSSHStdinWithin(ctx context.Context, hostname, command string, stdin []byte, ceiling time.Duration) ([]byte, error)
}

// execPush runs a push's SSH call bounded by pushCallCeiling: it lasts as long
// as the push's upload, which its remote side waits for by the upload's size.
func execPush(ctx context.Context, sshDeployer SSHDeployer, hostname, command string, stdin []byte) ([]byte, error) {
	if within, ok := sshDeployer.(stdinWithinExecer); ok {
		return within.ExecSSHStdinWithin(ctx, hostname, command, stdin, pushCallCeiling)
	}
	return sshDeployer.ExecSSHStdin(ctx, hostname, command, stdin)
}
