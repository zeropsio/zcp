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

	// The measurement is strictly best effort: it runs in every dev container
	// before every push, so it gets uploadMeasureBound and no more, and a
	// measurement that fails, hangs or prints nothing usable leaves the
	// upload uploadBudgetUnmeasured — the time an upload of about 1.5 GB
	// gets — and a note in the result. It never fails or holds the push.
	uploadMeasureBound     = 25 * time.Second
	uploadBudgetUnmeasured = 30 * time.Minute

	// Why a measurement said nothing usable, as the unmeasured mark
	// carries it: past its bound, a non-zero exit, no report of its shape.
	unmeasuredTimeout  = "timeout"
	unmeasuredFailed   = "failed"
	unmeasuredUnparsed = "unparsed"

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
	uploadUnmeasuredMark = "ZCP_UPLOAD_UNMEASURED"
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
// every MiB it carries, at most cap — or unmeasured, when the measurement,
// given measureBound, said nothing usable. A zero measureBound or unmeasured
// is the package default.
type uploadBudget struct {
	base, perMiB, cap        time.Duration
	measureBound, unmeasured time.Duration
}

func defaultUploadBudget() uploadBudget {
	return uploadBudget{base: uploadBudgetBase, perMiB: uploadBudgetPerMiB, cap: uploadBudgetCap,
		measureBound: uploadMeasureBound, unmeasured: uploadBudgetUnmeasured}
}

// seconds is d in whole seconds, def when d is not positive.
func seconds(d, def time.Duration) int {
	if d <= 0 {
		d = def
	}
	return int(d / time.Second)
}

// uploadAwk sums the measurement's tagged lines: A — `git check-attr
// export-ignore` of each tracked path and directory, a file under a marked
// directory left out as git archive leaves it; T — `git ls-tree -l` of each tracked
// file; P — an untracked path .gitignore does not exclude, then U — each
// regular file under it, from `ls -lnRA`; G — each regular file of a
// self-deploy's .git, likewise. File sizes come from ls, never du: what zcli
// sends is each file's length, and du counts disk blocks — on Zerops' ZFS a
// file written seconds ago shows almost none (live-verified 2026-10-28: a
// fresh 512 KiB file read 1 KiB). It prints the report lines with the budget
// the upload gets. POSIX awk only: mawk, gawk and busybox awk run it
// (live-verified on Ubuntu 22.04's mawk and Alpine 3.23's busybox).
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
$1 == "P" { cur = substr($0, 3); next }
$1 == "U" {
	if (cur != "") { b = $6 + 0; untracked += b; add(b, cur) }
	next
}
$1 == "G" { git += $6 + 0; next }
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
// directory and reads git and the file system only.
//
// It is strictly best effort and never fails or holds the push: the
// measurement runs as a job of its own, in a session of its own where setsid
// exists so its whole process group can be ended, and a watchdog ends it at
// measureBound. Only report lines that match their exact shape are printed,
// and only a budget of digits within the cap is used. A measurement that ran
// past its bound (timeout), exited non-zero (failed) or printed no such
// report (unparsed) prints the unmeasured mark with its reason, and the
// upload gets the unmeasured default. The group's last command always
// succeeds.
func uploadMeasureCommand(includeGit bool, budget uploadBudget) string {
	// git archive skips a directory marked export-ignore as a whole, which
	// check-attr reports on the directory alone: the trees (-t) are asked too.
	listing := `git ls-tree -r -t --name-only HEAD | git check-attr --stdin export-ignore | sed 's/^/A /'; ` +
		`git ls-tree -r -l HEAD | sed 's/^/T /'; ` +
		`git ls-files -o --exclude-standard --directory | while IFS= read -r p; do printf 'P %s\n' "$p"; ls -lnRA -- "$p" | sed -n 's/^-/U -/p'; done; `
	if includeGit {
		listing += `ls -lnRA -- "$(git rev-parse --git-dir)" | sed -n 's/^-/G -/p'; `
	}
	capSeconds := seconds(budget.cap, uploadBudgetCap)
	measure := shellQuote(fmt.Sprintf(`{ %s} < /dev/null 2>/dev/null | awk -v base=%d -v permib=%g -v cap=%d %s`,
		listing, seconds(budget.base, uploadBudgetBase), budget.perMiB.Seconds(), capSeconds, shellQuote(uploadAwk)))
	report := fmt.Sprintf(`^%[1]s files=[0-9]* bytes=[0-9]* untracked=[0-9]* git=[0-9]* budget=\([0-9][0-9]*\)$`, uploadReportMark)
	return `{ zcp_mf="${TMPDIR:-/tmp}/zcp-upload-$$"; rm -f "$zcp_mf" "$zcp_mf.late"; ` +
		// The job: in a session of its own when setsid exists, so the
		// watchdog's kill reaches git, ls and awk under it too.
		fmt.Sprintf(`if command -v setsid >/dev/null 2>&1; then setsid sh -c %[1]s > "$zcp_mf" 2>/dev/null < /dev/null & else sh -c %[1]s > "$zcp_mf" 2>/dev/null < /dev/null & fi; zcp_mp=$!; `, measure) +
		// The watchdog marks the timeout first, so the mark is there once
		// the wait below returns, then ends the job's process group — dash's
		// kill takes "-PGID" and refuses "--" (live-verified on Ubuntu 22.04),
		// busybox and bash take "-- -PGID" — or, with no group of its own,
		// the job alone.
		fmt.Sprintf(`( sleep %d; : > "$zcp_mf.late"; kill -TERM -"$zcp_mp" 2>/dev/null || kill -TERM -- -"$zcp_mp" 2>/dev/null || kill -TERM "$zcp_mp" 2>/dev/null ) > /dev/null 2>&1 < /dev/null & zcp_wp=$!; `,
			seconds(budget.measureBound, uploadMeasureBound)) +
		// The shell's own note of a job ended by the watchdog ("Terminated")
		// stays out of the push's output.
		`wait "$zcp_mp" 2>/dev/null; zcp_mrc=$?; kill "$zcp_wp" 2>/dev/null; ` +
		fmt.Sprintf(`zcp_up=$(grep -E '^%[1]s(_TOP)? ' "$zcp_mf" 2>/dev/null | head -n %[2]d); `, uploadReportMark, 1+uploadTopShown) +
		fmt.Sprintf(`zcp_budget=$(printf '%%s\n' "$zcp_up" | sed -n 's/%s/\1/p' | head -n 1); `, report) +
		fmt.Sprintf(`zcp_why=; if [ -e "$zcp_mf.late" ]; then zcp_why=%s; elif [ "$zcp_mrc" -ne 0 ]; then zcp_why=%s; `, unmeasuredTimeout, unmeasuredFailed) +
		fmt.Sprintf(`elif [ -z "$zcp_budget" ] || ! { [ "${#zcp_budget}" -le 5 ] && [ "$zcp_budget" -le %d ]; }; then zcp_why=%s; fi; `, capSeconds, unmeasuredUnparsed) +
		`rm -f "$zcp_mf" "$zcp_mf.late"; ` +
		fmt.Sprintf(`if [ -z "$zcp_why" ]; then printf '%%s\n' "$zcp_up"; else zcp_budget=%d; echo "%s reason=$zcp_why budget=$zcp_budget"; fi; }`,
			seconds(budget.unmeasured, uploadBudgetUnmeasured), uploadUnmeasuredMark)
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
	// unmeasured is why the measurement said nothing usable — timeout,
	// failed or unparsed — and the upload got the unmeasured default; ""
	// when it measured, or ran no measurement at all.
	unmeasured string
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
		case strings.HasPrefix(text, uploadUnfinishedMark+" "):
			report.unfinished = true
			if report.budget == 0 {
				if s, ok := markField(text, "budget"); ok {
					report.budget = time.Duration(s) * time.Second
				}
			}
		case strings.HasPrefix(text, uploadUnmeasuredMark+" "):
			report.unmeasured = unmeasuredUnparsed
			for field := range strings.FieldsSeq(text) {
				if why, ok := strings.CutPrefix(field, "reason="); ok && why != "" {
					report.unmeasured = why
				}
			}
			if s, ok := markField(text, "budget"); ok {
				report.budget = time.Duration(s) * time.Second
			}
		case strings.HasPrefix(text, uploadTopMark+" "):
			n, path, ok := strings.Cut(strings.TrimPrefix(text, uploadTopMark+" "), " ")
			if b, err := strconv.ParseInt(n, 10, 64); ok && err == nil && path != "" {
				size.Largest = append(size.Largest, UploadPath{Path: path, Bytes: b})
			}
		case strings.HasPrefix(text, uploadReportMark+" "):
			// A report missing a field, or carrying one that is no number, is
			// no report: the measurement printed something else.
			fields := map[string]int64{}
			for _, key := range []string{"files", "bytes", "untracked", "git", "budget"} {
				n, ok := markField(text, key)
				if !ok {
					fields = nil
					break
				}
				fields[key] = n
			}
			if fields == nil {
				continue
			}
			seen = true
			size.Files = int(fields["files"])
			size.Bytes, size.UntrackedBytes, size.GitBytes = fields["bytes"], fields["untracked"], fields["git"]
			report.budget = time.Duration(fields["budget"]) * time.Second
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
func uploadWhat(report uploadReport) string {
	switch {
	case report.size != nil:
		return report.size.Summary
	case report.unmeasured != "":
		return "its package (its size could not be measured: " + unmeasuredWhy(report.unmeasured) + ")"
	}
	return "its package"
}

// unmeasuredWhy says why the measurement said nothing usable.
func unmeasuredWhy(reason string) string {
	switch reason {
	case unmeasuredTimeout:
		return "its measurement ran past its time limit"
	case unmeasuredFailed:
		return "its measurement failed"
	case unmeasuredUnparsed:
		return "its measurement printed nothing usable"
	}
	return "its measurement said nothing usable"
}

// unmeasuredNote is the result's one line about an upload the push could not
// measure. "" when it measured, or ran no measurement.
func unmeasuredNote(report uploadReport) string {
	if report.unmeasured == "" {
		return ""
	}
	return fmt.Sprintf("zcp could not measure what this deploy uploads (%s), so it waited up to %s for the upload rather than a time set by its size.",
		unmeasuredWhy(report.unmeasured), report.budget)
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

// withUploadWarning is warnings with the upload's own line, when it has one:
// largeUploadWarning's, or the note that it could not be measured.
func withUploadWarning(warnings []string, report uploadReport) []string {
	for _, warn := range []string{largeUploadWarning(report.size), unmeasuredNote(report)} {
		if warn != "" {
			warnings = append(warnings, warn)
		}
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
