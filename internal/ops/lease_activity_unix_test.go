//go:build unix

// Tests for: ops/lease_activity.go — the one read of a lease's checkout runs
// in a real checkout, writes nothing into it, and agrees with the release
// guard (docs/spec-workflows.md §8 DP-21, DP-4).

package ops

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// gnuStatPath is a PATH entry that makes `stat` GNU coreutils' — the
// container's — or "" when the host's stat already is. A host with only BSD
// stat and no gstat skips: the read is written for the container's tools.
func gnuStatPath(t *testing.T) string {
	t.Helper()
	if exec.CommandContext(context.Background(), "stat", "-c", "%Y", "/").Run() == nil {
		return ""
	}
	gstat, err := exec.LookPath("gstat")
	if err != nil {
		t.Skip("the read uses GNU stat (the container's); this host has BSD stat and no gstat")
	}
	dir := t.TempDir()
	if err := os.Symlink(gstat, filepath.Join(dir, "stat")); err != nil {
		t.Fatal(err)
	}
	return dir
}

// checkoutIndex is what a rewrite of .git/index changes: its inode, its
// mtime, or its bytes.
type checkoutIndex struct {
	inode uint64
	mtime time.Time
	bytes []byte
}

func readCheckoutIndex(t *testing.T, dir string) checkoutIndex {
	t.Helper()
	inode, mtime := indexIdentity(t, dir)
	raw, err := os.ReadFile(filepath.Join(dir, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	return checkoutIndex{inode: inode, mtime: mtime, bytes: raw}
}

func setMtime(t *testing.T, path string, at time.Time) {
	t.Helper()
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

func writeAt(t *testing.T, path, body string, at time.Time) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	setMtime(t, path, at)
}

// activityLine is the first line of the read's raw answer.
func activityLine(t *testing.T, ssh *localCheckoutSSH) string {
	t.Helper()
	out, err := ssh.ExecSSH(context.Background(), "devpool-3", leaseActivityCommand())
	if err != nil {
		t.Fatalf("the read failed: %v\n%s", err, out)
	}
	line, _, _ := strings.Cut(string(out), "\n")
	return line
}

// TestLeaseActivity_RealCheckout_ReadsWithoutWritingAndAgreesWithTheGuard:
// in a real checkout, the read answers "<epoch> " (an empty second field)
// when nothing is modified and "<epoch> <epoch>" once files are; it takes the
// newest change git does not ignore; it works while .git/index.lock is held;
// it leaves .git/index's bytes, inode and mtime as they were — though a plain
// git status would rewrite them here (a tracked file's stat moved, same
// bytes); and the unsaved work it reports is what the release guard reads.
func TestLeaseActivity_RealCheckout_ReadsWithoutWritingAndAgreesWithTheGuard(t *testing.T) {
	t.Parallel()
	origin, _, _ := originWithBranches(t)
	work := t.TempDir()
	runCheckout(t, work, origin, "feat")
	// One commit on no remote branch.
	writeAt(t, filepath.Join(work, "c.txt"), "c", time.Now())
	gitIn(t, work, "add", "c.txt")
	gitIn(t, work, "commit", "-q", "-m", "local")

	now := time.Unix(time.Now().Unix(), 0)
	headAt, fileAt, newestAt := now.Add(-2*time.Hour), now.Add(-30*time.Minute), now.Add(-20*time.Minute)
	setMtime(t, filepath.Join(work, ".git", "logs", "HEAD"), headAt)
	// Same bytes, an older mtime: the index's stat info for the file is stale,
	// so a git status that may take the index lock refreshes and rewrites it.
	setMtime(t, filepath.Join(work, "app.txt"), now.Add(-3*time.Hour))
	before := readCheckoutIndex(t, work)

	ssh := &localCheckoutSSH{dir: work, path: withFakeSupervisor(t, gnuStatPath(t), 0)}
	d := LeaseDeps{SSH: ssh}
	rec := leaseRecord{PoolHostname: testPoolHost, ContainerNumber: 3, AcquiredAt: "2026-10-23T11:00:00Z"}

	// Nothing modified: the second field is empty.
	if line, want := activityLine(t, ssh), "@@zcp-lease activity "+strconv.FormatInt(headAt.Unix(), 10)+" "; line != want {
		t.Fatalf("clean checkout answered %q, want %q (the live shape: an empty second field)", line, want)
	}
	act, unsaved := readLeaseActivity(context.Background(), d, rec)
	if act.LastChangeAt != rfc3339(headAt.Unix()) || act.Note != "" {
		t.Errorf("clean activity = %+v, want lastChangeAt %s (logs/HEAD) and no note", act, rfc3339(headAt.Unix()))
	}
	if unsaved == nil || unsaved.FileCount != 0 || unsaved.UnpushedCommits != 1 {
		t.Errorf("clean unsaved = %+v, want no file and the 1 commit on no remote branch", unsaved)
	}

	// Another git holds the index lock while files change: a modified tracked
	// file, an untracked file, and a newer file git ignores.
	lock := filepath.Join(work, ".git", "index.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	writeAt(t, filepath.Join(work, "feat.txt"), "changed", fileAt)
	writeAt(t, filepath.Join(work, "notes.txt"), "n", newestAt)
	if err := os.MkdirAll(filepath.Join(work, ".git", "info"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, ".git", "info", "exclude"), []byte("*.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeAt(t, filepath.Join(work, "debug.log"), "ignored", now)

	if line, want := activityLine(t, ssh), "@@zcp-lease activity "+strconv.FormatInt(headAt.Unix(), 10)+" "+strconv.FormatInt(newestAt.Unix(), 10); line != want {
		t.Fatalf("modified checkout answered %q, want %q", line, want)
	}
	act, unsaved = readLeaseActivity(context.Background(), d, rec)
	if act.LastChangeAt != rfc3339(newestAt.Unix()) || act.Note != "" {
		t.Errorf("activity = %+v, want lastChangeAt %s (notes.txt; debug.log is ignored) and no note", act, rfc3339(newestAt.Unix()))
	}
	guard, err := readUnsavedWork(context.Background(), ssh, "devpool-3")
	if err != nil {
		t.Fatalf("the guard's read failed: %v", err)
	}
	if unsaved == nil || strings.Join(unsaved.Files, "|") != strings.Join(guard.files, "|") || unsaved.FileCount != len(guard.files) ||
		unsaved.UnpushedCommits != guard.unpushed {
		t.Errorf("status unsaved = %+v, guard read files %q and %d commit(s): they must agree", unsaved, guard.files, guard.unpushed)
	}
	if strings.Join(guard.files, "|") != " M feat.txt|?? notes.txt" || guard.unpushed != 1 {
		t.Errorf("guard read %q and %d commit(s), want feat.txt modified, notes.txt untracked, 1 commit", guard.files, guard.unpushed)
	}

	if _, err := os.Stat(lock); err != nil {
		t.Errorf("the read touched the held index.lock: %v", err)
	}
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	after := readCheckoutIndex(t, work)
	if after.inode != before.inode || !after.mtime.Equal(before.mtime) || !bytes.Equal(after.bytes, before.bytes) {
		t.Errorf(".git/index was rewritten: inode %d -> %d, mtime %s -> %s, bytes equal %v",
			before.inode, after.inode, before.mtime, after.mtime, bytes.Equal(after.bytes, before.bytes))
	}
	for _, c := range ssh.commands {
		if all, read := strings.Count(c, "git "), strings.Count(c, "GIT_OPTIONAL_LOCKS=0 git "); all != read {
			t.Errorf("%d of %d git calls run without GIT_OPTIONAL_LOCKS=0: %s", all-read, all, c)
		}
	}
}

// TestLeaseActivity_RealCheckout_NotACheckout_SaysGitStatusFailed: a /var/www
// that is no git checkout answers no change time and no unsaved work, and
// says git status failed in git's own words.
func TestLeaseActivity_RealCheckout_NotACheckout_SaysGitStatusFailed(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	ssh := &localCheckoutSSH{dir: t.TempDir(), path: gnuStatPath(t)}

	act, unsaved := readLeaseActivity(context.Background(), LeaseDeps{SSH: ssh}, leaseRecord{PoolHostname: testPoolHost, ContainerNumber: 3})

	if act.LastChangeAt != "" || unsaved != nil {
		t.Errorf("activity = %+v / %+v, want nothing read", act, unsaved)
	}
	for _, want := range []string{"git status", "not a git repository"} {
		if !strings.Contains(strings.ToLower(act.Note), want) {
			t.Errorf("note %q lacks %q", act.Note, want)
		}
	}
}

// fakeSupervisorStatus is what the fake zcp-devsup's `ctl status --json`
// answers: web, the preview server, holding one connection.
const fakeSupervisorStatus = `{"format":1,"supervisor":{"version":"v9.500.0","pid":412},"declaration":{"version":1,"error":""},"env":{},` +
	`"servers":{"web":{"status":"running","previewPort":9003,"previewLastConnAt":"2026-10-23T13:30:00.5Z","previewOpenConns":1}}}`

// withFakeSupervisor is a PATH prefix — pathEntry (may be "") and a directory
// holding a zcp-devsup script — whose `ctl version --json` lists activity and
// whose `ctl status --json` answers fakeSupervisorStatus with statusExit, as
// the container's binary does. Any other call fails: the read sends no other.
func withFakeSupervisor(t *testing.T, pathEntry string, statusExit int) string {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\n" +
		"[ \"$1 $3\" = 'ctl --json' ] || exit 1\n" +
		"case \"$2\" in\n" +
		"version) echo '{\"version\":\"v9.500.0\",\"formats\":[1],\"capabilities\":[\"env\",\"activity\"]}' ;;\n" +
		"status) echo '" + fakeSupervisorStatus + "'; exit " + strconv.Itoa(statusExit) + " ;;\n" +
		"*) exit 1 ;;\n" +
		"esac\n"
	if err := os.WriteFile(filepath.Join(dir, "zcp-devsup"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if pathEntry == "" {
		return dir
	}
	return pathEntry + string(os.PathListSeparator) + dir
}

// TestLeaseActivity_RealShell_SupervisorAnswersAfterGitFails_KeepsGitsFailure:
// run by a real shell, the read asks the container's zcp-devsup for the
// preview after the checkout part — even when git failed — and still exits
// with git's status: the failure is reported in git's words, the preview
// facts the supervisor answered stand, and a clean read takes them with no
// note.
func TestLeaseActivity_RealShell_SupervisorAnswersAfterGitFails_KeepsGitsFailure(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	rec := leaseRecord{PoolHostname: testPoolHost, ContainerNumber: 3, AcquiredAt: "2026-10-23T11:00:00Z"}

	notACheckout := &localCheckoutSSH{dir: t.TempDir(), path: withFakeSupervisor(t, gnuStatPath(t), 0)}
	act, unsaved := readLeaseActivity(context.Background(), LeaseDeps{SSH: notACheckout}, rec)
	if unsaved != nil || !strings.Contains(strings.ToLower(act.Note), "not a git repository") || strings.Contains(act.Note, "preview") {
		t.Errorf("not a checkout = %+v / %+v, want git status's failure in git's words, nothing about the preview", act, unsaved)
	}
	if act.PreviewLastConnAt != "2026-10-23T13:30:00Z" || act.PreviewOpenConns == nil || *act.PreviewOpenConns != 1 {
		t.Errorf("preview = %q/%v, want 2026-10-23T13:30:00Z and 1: the supervisor answered", act.PreviewLastConnAt, act.PreviewOpenConns)
	}

	// A supervisor whose status fails leaves the checkout's read whole.
	origin, _, _ := originWithBranches(t)
	work := t.TempDir()
	runCheckout(t, work, origin, "feat")
	failing := &localCheckoutSSH{dir: work, path: withFakeSupervisor(t, gnuStatPath(t), 2)}
	act, unsaved = readLeaseActivity(context.Background(), LeaseDeps{SSH: failing}, rec)
	if act.LastChangeAt == "" || unsaved == nil || act.PreviewOpenConns != nil || !strings.Contains(act.Note, "preview not observed") {
		t.Errorf("a failing supervisor = %+v / %+v, want the checkout read and the preview not observed", act, unsaved)
	}

	clean := &localCheckoutSSH{dir: work, path: withFakeSupervisor(t, gnuStatPath(t), 0)}
	act, unsaved = readLeaseActivity(context.Background(), LeaseDeps{SSH: clean}, rec)
	if act.LastChangeAt == "" || unsaved == nil || act.Note != "" || act.PreviewOpenConns == nil || *act.PreviewOpenConns != 1 {
		t.Errorf("clean = %+v / %+v, want the checkout and the preview read, no note", act, unsaved)
	}
}
