// Tests for: ops/lease_activity.go — what zerops_lease action=status reads of
// each held lease's checkout: its activity and its unsaved work, from one
// bounded ssh invocation per lease (docs/spec-workflows.md §8 DP-21).
package ops

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// ctxSSH answers by host and command and honours the call's context, as
// SystemSSHDeployer does (its ssh is killed when the context ends).
type ctxSSH struct {
	mu    sync.Mutex
	fn    func(ctx context.Context, host, command string) ([]byte, error)
	calls []sshCall
}

func (s *ctxSSH) ExecSSH(ctx context.Context, host, command string) ([]byte, error) {
	s.mu.Lock()
	s.calls = append(s.calls, sshCall{hostname: host, command: command})
	fn := s.fn
	s.mu.Unlock()
	return fn(ctx, host, command)
}

// ExecSSHStdin answers one command as ExecSSH does; the input is not read.
func (s *ctxSSH) ExecSSHStdin(ctx context.Context, host, command string, _ []byte) ([]byte, error) {
	return s.ExecSSH(ctx, host, command)
}

func (s *ctxSSH) ExecSSHBackground(ctx context.Context, host, command string, _ time.Duration) ([]byte, error) {
	return s.ExecSSH(ctx, host, command)
}

func (s *ctxSSH) hosts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.calls))
	for _, c := range s.calls {
		out = append(out, c.hostname)
	}
	return out
}

// checkoutAnswer is what the one read of a checkout answers: the activity
// line (the .git/logs/HEAD mtime, then the newest modified file's mtime, ""
// when none), then the guard's two reads, each after its marker.
func checkoutAnswer(headAt int64, filesAt string, porcelain []string, unpushed int) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "@@zcp-lease activity %d %s\n@@zcp-lease status\n", headAt, filesAt)
	for _, l := range porcelain {
		b.WriteString(l + "\n")
	}
	fmt.Fprintf(&b, "@@zcp-lease unpushed\n%d\n", unpushed)
	return []byte(b.String())
}

func rfc3339(epoch int64) string { return time.Unix(epoch, 0).UTC().Format(time.RFC3339) }

// TestLeaseActivity_CommandIsReadOnly: the one invocation reads the
// checkout's activity first — never .git/index, which the guard's git status
// may rewrite — and then runs the release guard's own two reads, verbatim (one
// builder, never a second copy). Every git call skips the optional index
// lock, nothing in it writes, and every redirection throws output away.
func TestLeaseActivity_CommandIsReadOnly(t *testing.T) {
	t.Parallel()
	cmd := leaseActivityCommand()

	head := strings.Index(cmd, ".git/logs/HEAD")
	changed := strings.Index(cmd, "ls-files -z -m -o --exclude-standard")
	files := strings.Index(cmd, leaseUnsavedFilesCommand())
	unpushed := strings.Index(cmd, leaseUnpushedCommand())
	if head < 0 || changed < 0 || files < 0 || unpushed < 0 || head > files || changed > files || files > unpushed {
		t.Fatalf("command must read logs/HEAD and the changed files first, then the guard's two reads in order:\n%s", cmd)
	}
	if !strings.HasPrefix(cmd, "cd "+shellQuote(LeaseWorkDir)+" && ") {
		t.Errorf("command must run in the quoted checkout: %s", cmd)
	}
	if strings.Contains(cmd, ".git/index") {
		t.Errorf("command reads .git/index, which the guard's git status may rewrite: %s", cmd)
	}
	if all, read := strings.Count(cmd, "git "), strings.Count(cmd, "GIT_OPTIONAL_LOCKS=0 git "); all != read {
		t.Errorf("%d of %d git calls run without GIT_OPTIONAL_LOCKS=0: %s", all-read, all, cmd)
	}
	for _, write := range []string{" add ", " commit", " checkout", " reset", " clean", " stash", "update-index", " gc", " fetch", " pull",
		"rm ", "mv ", "touch ", "mkdir", "tee ", "chmod", "sudo"} {
		if strings.Contains(cmd, write) {
			t.Errorf("command contains %q, which writes: %s", write, cmd)
		}
	}
	for _, m := range regexp.MustCompile(`[0-9]?>\s*(&[0-9]|[^\s;|)"]+)`).FindAllStringSubmatch(cmd, -1) {
		if m[1] != "/dev/null" && m[1] != "&1" {
			t.Errorf("command redirects into %q; a read writes nowhere: %s", m[1], cmd)
		}
	}
	// The supervisor's preview activity is read last, by verbs every
	// zcp-devsup knows — `ctl version` and `ctl status`, never another verb
	// or flag — the status bounded so a supervisor that does not answer
	// cannot spend the checkout's budget, and the read exits with the
	// checkout part's status, so a git failure stays the read's failure.
	preview := strings.Index(cmd, previewMarker)
	if preview < unpushed {
		t.Fatalf("the supervisor part must come after the guard's reads:\n%s", cmd)
	}
	ctlCalls := regexp.MustCompile(`zcp-devsup ctl [^;]*`).FindAllString(cmd, -1)
	if len(ctlCalls) != 2 || !strings.HasPrefix(ctlCalls[0], "zcp-devsup ctl version --json") || !strings.HasPrefix(ctlCalls[1], "zcp-devsup ctl status --json") {
		t.Errorf("ctl calls = %q, want exactly `ctl version --json` then `ctl status --json`", ctlCalls)
	}
	if !strings.Contains(cmd, `"timeout 2"`) {
		t.Errorf("ctl status is not bounded by a 2 s timeout: %s", cmd)
	}
	if !strings.Contains(cmd[unpushed:preview], "rc=$?") || !strings.HasSuffix(cmd, "exit $rc") {
		t.Errorf("the read must keep the checkout part's exit status and end with it: %s", cmd)
	}
}

// previewMarker opens the supervisor part of the read's answer.
const previewMarker = "@@zcp-lease preview"

// previewPart is the supervisor part of the read's answer: its marker, then
// `ctl version` and `ctl status` each closed by its exit code — or only the
// version section when status is "" and statusCode is negative.
func previewPart(version string, versionCode int, status string, statusCode int) string {
	var b strings.Builder
	b.WriteString(previewMarker + "\n")
	if version != "" {
		b.WriteString(version + "\n")
	}
	fmt.Fprintf(&b, "@@zcp-devsup version %d\n", versionCode)
	if statusCode >= 0 {
		if status != "" {
			b.WriteString(status + "\n")
		}
		fmt.Fprintf(&b, "@@zcp-devsup status %d\n", statusCode)
	}
	return b.String()
}

// Supervisor answers the preview tests read.
const (
	capableVersion = `{"version":"v9.500.0","formats":[1],"capabilities":["env","activity"]}`
	envOnlyVersion = `{"version":"v9.400.0","formats":[1],"capabilities":["env"]}`
	olderVersion   = `{"version":"v9.300.0","formats":[1]}`
)

// previewState is a `ctl status --json` answer whose preview server web has
// the given activity (absent when nil) beside worker, which has no preview
// port.
func previewState(last string, open *int) string {
	web := `"status":"running","previewPort":9003`
	if last != "" {
		web += `,"previewLastConnAt":"` + last + `"`
	}
	if open != nil {
		web += `,"previewOpenConns":` + strconv.Itoa(*open)
	}
	return `{"format":1,"supervisor":{"version":"v9.500.0","pid":412},"declaration":{"version":1,"error":""},"env":{},"servers":{` +
		`"web":{` + web + `},"worker":{"status":"running"}}}`
}

func intPtr(n int) *int { return &n }

// TestLeaseActivity_PreviewFromSupervisorWhenCapable: a supervisor whose
// `ctl version` lists `activity` gives the lease's preview facts from its
// status — when its preview forward last took a connection (RFC 3339 UTC,
// like every activity time) and how many it holds open, 0 being an observed
// zero — beside the checkout's facts, with nothing to note. A checkout part
// that fails keeps the preview facts the supervisor part read.
func TestLeaseActivity_PreviewFromSupervisorWhenCapable(t *testing.T) {
	t.Parallel()
	const head = int64(1792756800)
	clean := string(checkoutAnswer(head, "", nil, 0))
	statusFailed := fmt.Sprintf("@@zcp-lease activity %d \n@@zcp-lease status\nfatal: not a git repository\n", head)
	for _, tt := range []struct {
		name     string
		out      string
		exit     int // the remote's exit status; 0: no error
		wantLast string
		wantOpen int
		wantNote string // "" means no note
	}{
		{name: "one connection open", out: clean + previewPart(capableVersion, 0, previewState("2026-10-23T13:30:00.123456Z", intPtr(1)), 0),
			wantLast: "2026-10-23T13:30:00Z", wantOpen: 1},
		{name: "none open: an observed zero", out: clean + previewPart(capableVersion, 0, previewState("2026-10-23T13:30:00+02:00", intPtr(0)), 0),
			wantLast: "2026-10-23T11:30:00Z", wantOpen: 0},
		{name: "no connection yet in the supervisor's life", out: clean + previewPart(capableVersion, 0, previewState("", intPtr(0)), 0),
			wantOpen: 0},
		{name: "git status failed, the supervisor part still read", out: statusFailed + previewPart(capableVersion, 0, previewState("2026-10-23T13:30:00Z", intPtr(2)), 0), exit: 128,
			wantLast: "2026-10-23T13:30:00Z", wantOpen: 2, wantNote: "git status"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var err error
			if tt.exit != 0 {
				err = sshFailure(tt.out, exitError(t, tt.exit))
			}
			ssh := &ctxSSH{fn: func(context.Context, string, string) ([]byte, error) { return []byte(tt.out), err }}
			rec := leaseRecord{PoolHostname: testPoolHost, ContainerNumber: 3, AcquiredAt: "2026-10-23T11:00:00Z"}

			act, _ := readLeaseActivity(context.Background(), LeaseDeps{SSH: ssh}, rec)

			if act.PreviewLastConnAt != tt.wantLast {
				t.Errorf("previewLastConnAt = %q, want %q", act.PreviewLastConnAt, tt.wantLast)
			}
			if act.PreviewOpenConns == nil || *act.PreviewOpenConns != tt.wantOpen {
				t.Errorf("previewOpenConns = %v, want %d observed", act.PreviewOpenConns, tt.wantOpen)
			}
			if act.LastChangeAt != rfc3339(head) {
				t.Errorf("lastChangeAt = %q, want %q: the checkout part stands beside the preview", act.LastChangeAt, rfc3339(head))
			}
			switch {
			case tt.wantNote == "" && act.Note != "":
				t.Errorf("note = %q, want none", act.Note)
			case tt.wantNote != "" && (!strings.Contains(act.Note, tt.wantNote) || strings.Contains(act.Note, "preview")):
				t.Errorf("note = %q, want it to name %q and nothing about the preview, which was read", act.Note, tt.wantNote)
			}
		})
	}
}

// TestLeaseActivity_OlderSupervisor_PreviewNotObserved: the preview facts
// come only from a supervisor whose `ctl version` lists `activity`; from an
// older one — even one whose status carried such fields — and from a
// container with no supervisor or one that did not answer, they are absent,
// never zero, and the note says the preview was not observed and why. The
// checkout's facts stand.
func TestLeaseActivity_OlderSupervisor_PreviewNotObserved(t *testing.T) {
	t.Parallel()
	const head = int64(1792756800)
	clean := string(checkoutAnswer(head, "", nil, 0))
	withActivity := previewState("2026-10-23T13:30:00Z", intPtr(1))
	for _, tt := range []struct {
		name     string
		preview  string
		wantNote []string
	}{
		{name: "an older supervisor without capabilities", preview: previewPart(olderVersion, 0, withActivity, 0),
			wantNote: []string{"v9.300.0", "activity"}},
		{name: "a supervisor that lists env only", preview: previewPart(envOnlyVersion, 0, withActivity, 0),
			wantNote: []string{"v9.400.0", "activity"}},
		{name: "no zcp-devsup in the container", preview: previewPart("", 127, "", -1),
			wantNote: []string{"zcp-devsup is not installed"}},
		{name: "ctl version fails", preview: previewPart("", 1, "", -1),
			wantNote: []string{"ctl version exited 1"}},
		{name: "installed, but no supervisor runs", preview: previewPart(capableVersion, 0, `{"code":2,"error":"no zcp-devsup is running here"}`, 2),
			wantNote: []string{"no zcp-devsup runs"}},
		{name: "the supervisor did not answer in time", preview: previewPart(capableVersion, 0, "", 124),
			wantNote: []string{"did not answer"}},
		{name: "the supervisor said it was unresponsive", preview: previewPart(capableVersion, 0, `{"code":5,"error":"zcp-devsup is running here but did not answer in time"}`, 5),
			wantNote: []string{"did not answer"}},
		{name: "status fails otherwise", preview: previewPart(capableVersion, 0, `{"code":1,"error":"boom"}`, 1),
			wantNote: []string{"ctl status exited 1", "boom"}},
		{name: "a status that is no state", preview: previewPart(capableVersion, 0, "garbage", 0),
			wantNote: []string{"garbage"}},
		{name: "an unreadable supervisor part", preview: previewMarker + "\n@@zcp-devsup version soon\n",
			wantNote: []string{"soon"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ssh := &ctxSSH{fn: func(context.Context, string, string) ([]byte, error) { return []byte(clean + tt.preview), nil }}
			rec := leaseRecord{PoolHostname: testPoolHost, ContainerNumber: 3, AcquiredAt: "2026-10-23T11:00:00Z"}

			act, unsaved := readLeaseActivity(context.Background(), LeaseDeps{SSH: ssh}, rec)

			if act.PreviewLastConnAt != "" || act.PreviewOpenConns != nil {
				t.Errorf("preview = %q/%v, want absent: it was not observed", act.PreviewLastConnAt, act.PreviewOpenConns)
			}
			for _, want := range append([]string{"preview not observed"}, tt.wantNote...) {
				if !strings.Contains(act.Note, want) {
					t.Errorf("note %q lacks %q", act.Note, want)
				}
			}
			if act.LastChangeAt != rfc3339(head) || unsaved == nil {
				t.Errorf("checkout = %q / %+v, want lastChangeAt %s and the unsaved work read", act.LastChangeAt, unsaved, rfc3339(head))
			}
		})
	}
}

// TestLeaseActivity_NoPreviewServer_ZeroOpen: a supervisor that lists
// `activity` and runs no dev server on a preview port has no preview forward,
// so no preview connection can be open: previewOpenConns reads an observed 0,
// previewLastConnAt stays absent, and no note says the preview was not
// observed — a client drawing idleness from the envelope (spec-mate §2.8a.3)
// reads the preview as observed, as zcp's own label does (DP-25).
func TestLeaseActivity_NoPreviewServer_ZeroOpen(t *testing.T) {
	t.Parallel()
	const head = int64(1792756800)
	clean := string(checkoutAnswer(head, "", nil, 0))
	for _, tt := range []struct {
		name  string
		state string
	}{
		{name: "a dev server on no preview port", state: noPreviewServer},
		{name: "no dev server at all", state: `{"format":1,"servers":{}}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ssh := &ctxSSH{fn: func(context.Context, string, string) ([]byte, error) {
				return []byte(clean + previewPart(capableVersion, 0, tt.state, 0)), nil
			}}
			rec := leaseRecord{PoolHostname: testPoolHost, ContainerNumber: 3, AcquiredAt: "2026-10-23T11:00:00Z"}

			act, _ := readLeaseActivity(context.Background(), LeaseDeps{SSH: ssh}, rec)

			if act.PreviewOpenConns == nil || *act.PreviewOpenConns != 0 {
				t.Errorf("previewOpenConns = %v, want an observed 0", act.PreviewOpenConns)
			}
			if act.PreviewLastConnAt != "" {
				t.Errorf("previewLastConnAt = %q, want absent", act.PreviewLastConnAt)
			}
			if strings.Contains(act.Note, "preview") {
				t.Errorf("note %q speaks of the preview, which was observed", act.Note)
			}
		})
	}
}

// TestLeaseActivity_ParsesNewestChange: lastChangeAt is the newer of the two
// times the read answers; "nothing modified" is an empty second field
// (live: "<epoch> "); a time the read could not give is absent, never zero,
// and the note says which. The guard's reads come back as unsaved work: the
// first files by name, all of them counted.
func TestLeaseActivity_ParsesNewestChange(t *testing.T) {
	t.Parallel()
	const head, file = int64(1792756800), int64(1792760400) // 2026-10-23 12:00 and 13:00 UTC
	var many []string
	for i := 1; i <= 25; i++ {
		many = append(many, fmt.Sprintf("?? file%02d.txt", i))
	}
	for _, tt := range []struct {
		name         string
		out          string
		wantChange   string
		wantNote     string // "" means no note
		wantFiles    []string
		wantCount    int
		wantUnpushed int
	}{
		{name: "HEAD moved last", out: string(checkoutAnswer(file, strconv.FormatInt(head, 10), []string{" M a.go"}, 0)),
			wantChange: rfc3339(file), wantFiles: []string{" M a.go"}, wantCount: 1},
		{name: "a file changed last", out: string(checkoutAnswer(head, strconv.FormatInt(file, 10), []string{" M a.go", "?? b.txt"}, 3)),
			wantChange: rfc3339(file), wantFiles: []string{" M a.go", "?? b.txt"}, wantCount: 2, wantUnpushed: 3},
		{name: "nothing modified: an empty second field", out: string(checkoutAnswer(head, "", nil, 0)),
			wantChange: rfc3339(head)},
		{name: "nothing modified, the trailing space trimmed", out: fmt.Sprintf("@@zcp-lease activity %d\n@@zcp-lease status\n@@zcp-lease unpushed\n0\n", head),
			wantChange: rfc3339(head)},
		{name: "no logs/HEAD, a modified file", out: string(checkoutAnswer(0, strconv.FormatInt(file, 10), []string{"?? b.txt"}, 0)),
			wantChange: rfc3339(file), wantNote: "logs/HEAD", wantFiles: []string{"?? b.txt"}, wantCount: 1},
		{name: "no logs/HEAD, nothing modified", out: string(checkoutAnswer(0, "", nil, 0)),
			wantNote: "logs/HEAD"},
		{name: "an answer that is no time", out: "@@zcp-lease activity soon later\n@@zcp-lease status\n@@zcp-lease unpushed\n0\n",
			wantNote: "soon later"},
		{name: "many files: the first twenty by name, all counted", out: string(checkoutAnswer(head, strconv.FormatInt(file, 10), many, 1)),
			wantChange: rfc3339(file), wantFiles: many[:20], wantCount: 25, wantUnpushed: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ssh := &ctxSSH{fn: func(context.Context, string, string) ([]byte, error) { return []byte(tt.out), nil }}
			rec := leaseRecord{PoolHostname: testPoolHost, ContainerNumber: 3, AcquiredAt: "2026-10-23T11:00:00Z"}

			act, unsaved := readLeaseActivity(context.Background(), LeaseDeps{SSH: ssh}, rec)

			if act == nil {
				t.Fatal("no activity for a lease that was read")
			}
			if act.LastChangeAt != tt.wantChange {
				t.Errorf("lastChangeAt = %q, want %q", act.LastChangeAt, tt.wantChange)
			}
			if act.AcquiredAt != rec.AcquiredAt {
				t.Errorf("acquiredAt = %q, want the record's %q", act.AcquiredAt, rec.AcquiredAt)
			}
			if _, err := time.Parse(time.RFC3339, act.ReadAt); err != nil {
				t.Errorf("readAt = %q, want an RFC 3339 time: %v", act.ReadAt, err)
			}
			if act.PreviewLastConnAt != "" || act.PreviewOpenConns != nil {
				t.Errorf("preview facts = %q/%v, want absent (not observed yet)", act.PreviewLastConnAt, act.PreviewOpenConns)
			}
			switch {
			case tt.wantNote == "" && act.Note != "":
				t.Errorf("note = %q, want none", act.Note)
			case tt.wantNote != "" && !strings.Contains(act.Note, tt.wantNote):
				t.Errorf("note = %q, want it to name %q", act.Note, tt.wantNote)
			}
			want := &topology.LeaseUnsaved{Files: tt.wantFiles, FileCount: tt.wantCount, UnpushedCommits: tt.wantUnpushed}
			if unsaved == nil || strings.Join(unsaved.Files, "|") != strings.Join(want.Files, "|") ||
				unsaved.FileCount != want.FileCount || unsaved.UnpushedCommits != want.UnpushedCommits {
				t.Errorf("unsaved = %+v, want %+v", unsaved, want)
			}
			if hosts := ssh.hosts(); len(hosts) != 1 || hosts[0] != "devpool-3" {
				t.Errorf("ssh calls on %v, want one on the alias devpool-3", hosts)
			}
		})
	}
}

// TestLeaseActivity_ReadFails_SaysWhatFailedAndKeepsWhatItRead: a read that
// fails says which part failed, with the remote's own words (an ssh error's
// Error() drops them); what an earlier part read stands, and what it did not
// read is absent — never zero.
func TestLeaseActivity_ReadFails_SaysWhatFailedAndKeepsWhatItRead(t *testing.T) {
	t.Parallel()
	const head = int64(1792756800)
	statusFailed := fmt.Sprintf("@@zcp-lease activity %d \n@@zcp-lease status\nfatal: not a git repository\n", head)
	for _, tt := range []struct {
		name       string
		out        string // what the call returns beside its error
		exit       int    // the remote's exit status; 0: no error
		errOut     string // what the *SSHExecError carries
		wantChange string
		wantNote   []string
	}{
		{
			name: "ssh cannot reach the container", exit: 255, errOut: "ssh: Could not resolve hostname devpool-3",
			wantNote: []string{"could not reach the container over ssh", "Could not resolve hostname"},
		},
		{
			name: "the checkout directory is missing", exit: 2, errOut: "sh: cd: /var/www: No such file or directory",
			wantNote: []string{"/var/www", "No such file or directory"},
		},
		{
			name: "git status fails after the activity read", out: statusFailed, exit: 128, errOut: statusFailed,
			wantChange: rfc3339(head), wantNote: []string{"git status", "fatal: not a git repository"},
		},
		{
			name: "the unpushed count fails, the output only in the error", exit: 128,
			errOut:     fmt.Sprintf("@@zcp-lease activity %d \n@@zcp-lease status\n?? a.txt\n@@zcp-lease unpushed\nfatal: bad object HEAD\n", head),
			wantChange: rfc3339(head), wantNote: []string{"commits on no remote branch", "fatal: bad object HEAD"},
		},
		{
			name: "an answer with no activity line", out: "ok",
			wantNote: []string{"ok"},
		},
		{
			name:       "an unpushed count that is no number",
			out:        fmt.Sprintf("@@zcp-lease activity %d \n@@zcp-lease status\n@@zcp-lease unpushed\nmany\n", head),
			wantChange: rfc3339(head), wantNote: []string{"many"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var err error
			if tt.exit != 0 {
				err = sshFailure(tt.errOut, exitError(t, tt.exit))
			}
			ssh := &ctxSSH{fn: func(context.Context, string, string) ([]byte, error) {
				if tt.out == "" {
					return nil, err
				}
				return []byte(tt.out), err
			}}
			rec := leaseRecord{PoolHostname: testPoolHost, ContainerNumber: 3, AcquiredAt: "2026-10-23T11:00:00Z"}

			act, unsaved := readLeaseActivity(context.Background(), LeaseDeps{SSH: ssh}, rec)

			if act == nil {
				t.Fatal("a lease whose read failed must still say why")
			}
			if act.LastChangeAt != tt.wantChange {
				t.Errorf("lastChangeAt = %q, want %q", act.LastChangeAt, tt.wantChange)
			}
			if act.AcquiredAt != rec.AcquiredAt {
				t.Errorf("acquiredAt = %q, want the record's %q", act.AcquiredAt, rec.AcquiredAt)
			}
			if unsaved != nil {
				t.Errorf("unsaved = %+v, want absent: it was not read", unsaved)
			}
			for _, want := range tt.wantNote {
				if !strings.Contains(act.Note, want) {
					t.Errorf("note %q lacks %q", act.Note, want)
				}
			}
		})
	}
}

// statusFixture is devpool holding records #3 (held) and #4 (its container
// gone), with #7 reserved by another holder; each held lease's checkout
// answers through ssh.
func statusFixture(t *testing.T, ssh SSHDeployer) *leaseFixture {
	t.Helper()
	fx := newLeaseFixture(t)
	recordLease(t, fx.stateDir, testPoolHost, "svc-pool", 3, "feat-a")
	recordLease(t, fx.stateDir, testPoolHost, "svc-pool", 4, "feat-b")
	fx.swarm.containers = []platform.SwarmContainer{reservedContainer("c-3", 3), reservedContainer("c-7", 7)}
	fx.deps.SSH = ssh
	return fx
}

// TestLeaseStatus_ReportsActivityAndUnsavedPerHeldLease: status reads each
// lease it holds — and only those — with one ssh invocation on its alias, and
// reports its activity (the checkout's last change, acquiredAt, when it was
// read) and its unsaved work. A lost lease and another holder's container are
// not read. Status mutates nothing: the orchestrator is only listed.
func TestLeaseStatus_ReportsActivityAndUnsavedPerHeldLease(t *testing.T) {
	t.Parallel()
	const head, file = int64(1792756800), int64(1792760400)
	for _, pool := range []string{testPoolHost, ""} {
		t.Run("pool="+pool, func(t *testing.T) {
			t.Parallel()
			ssh := &ctxSSH{fn: func(_ context.Context, host, _ string) ([]byte, error) {
				if host != "devpool-3" {
					return nil, fmt.Errorf("no container answers as %s", host)
				}
				return checkoutAnswer(head, strconv.FormatInt(file, 10), []string{" M src/App.tsx"}, 2), nil
			}}
			fx := statusFixture(t, ssh)
			rec, err := loadLeaseRecord(fx.stateDir, "devpool-3")
			if err != nil {
				t.Fatal(err)
			}

			status, err := LeaseStatus(context.Background(), fx.deps, pool)
			if err != nil {
				t.Fatalf("LeaseStatus: %v", err)
			}

			byAlias := map[string]LeaseView{}
			for _, v := range status.Leases {
				byAlias[v.Alias] = v
			}
			held := byAlias["devpool-3"]
			if held.State != LeaseStateHeld || held.Activity == nil {
				t.Fatalf("devpool-3 = %+v, want held with its activity", held)
			}
			if a := held.Activity; a.LastChangeAt != rfc3339(file) || a.AcquiredAt != rec.AcquiredAt || a.ReadAt == "" || a.Note != "" {
				t.Errorf("activity = %+v, want lastChangeAt %s, acquiredAt %s, a readAt and no note", a, rfc3339(file), rec.AcquiredAt)
			}
			if u := held.Unsaved; u == nil || u.FileCount != 1 || len(u.Files) != 1 || u.Files[0] != " M src/App.tsx" || u.UnpushedCommits != 2 {
				t.Errorf("unsaved = %+v, want src/App.tsx and 2 commits", u)
			}
			for _, alias := range []string{"devpool-4", "devpool-7"} {
				if v := byAlias[alias]; v.Activity != nil || v.Unsaved != nil {
					t.Errorf("%s (%s) = %+v, want nothing read: zcp holds no reachable lease there", alias, v.State, v)
				}
			}
			ssh.mu.Lock()
			calls := append([]sshCall(nil), ssh.calls...)
			ssh.mu.Unlock()
			if len(calls) != 1 || calls[0].hostname != "devpool-3" || calls[0].command != leaseActivityCommand() {
				t.Errorf("ssh calls = %+v, want one activity read on devpool-3", calls)
			}
			if len(fx.swarm.releases)+len(fx.swarm.deletes)+len(fx.swarm.acquireReqs)+fx.swarm.rollouts != 0 {
				t.Errorf("status changed the pool: releases %v, deletes %v, acquires %v, rollouts %d",
					fx.swarm.releases, fx.swarm.deletes, fx.swarm.acquireReqs, fx.swarm.rollouts)
			}
		})
	}
}

// TestLeaseStatus_ActivityUnreadable_SaysWhy: a held lease whose container
// ssh cannot reach stays held — the read never fails status — and its
// activity says why, with no change time and no unsaved work: a missing fact
// is never zero activity.
func TestLeaseStatus_ActivityUnreadable_SaysWhy(t *testing.T) {
	t.Parallel()
	refused := sshFailure("ssh: connect to host node-id-3.runtime.devpool.zerops port 22: Connection refused", exitError(t, 255))
	ssh := &ctxSSH{fn: func(context.Context, string, string) ([]byte, error) { return nil, refused }}
	fx := statusFixture(t, ssh)

	status, err := LeaseStatus(context.Background(), fx.deps, testPoolHost)
	if err != nil {
		t.Fatalf("an unreadable checkout must not fail status: %v", err)
	}
	for _, v := range status.Leases {
		if v.Alias != "devpool-3" {
			continue
		}
		if v.State != LeaseStateHeld {
			t.Errorf("state = %q, want held: the orchestrator holds it", v.State)
		}
		if v.Activity == nil || v.Activity.LastChangeAt != "" || v.Activity.AcquiredAt == "" ||
			!strings.Contains(v.Activity.Note, "could not reach") || !strings.Contains(v.Activity.Note, "Connection refused") {
			t.Errorf("activity = %+v, want no change time, acquiredAt kept and why in the note", v.Activity)
		}
		if v.Unsaved != nil {
			t.Errorf("unsaved = %+v, want absent: nothing was read", v.Unsaved)
		}
		return
	}
	t.Fatalf("leases = %+v, want devpool-3", status.Leases)
}

// TestLeaseStatus_ActivityReadsInParallelWithinBudget: every lease is read at
// once, each within its own budget. Three checkouts answer only once all
// three reads are in flight (one after another, each would wait out its
// budget); a fourth never answers and is cut at the budget, saying so, while
// the others stand.
func TestLeaseStatus_ActivityReadsInParallelWithinBudget(t *testing.T) {
	t.Parallel()
	const head = int64(1792756800)
	const budget = 300 * time.Millisecond
	fx := newLeaseFixture(t)
	fx.deps.ActivityTimeout = budget
	for n := int64(3); n <= 6; n++ {
		recordLease(t, fx.stateDir, testPoolHost, "svc-pool", n, "b"+strconv.FormatInt(n, 10))
		fx.swarm.containers = append(fx.swarm.containers, reservedContainer("c-"+strconv.FormatInt(n, 10), n))
	}
	var arrived sync.WaitGroup
	arrived.Add(3)
	allIn := make(chan struct{})
	go func() { arrived.Wait(); close(allIn) }()
	fx.deps.SSH = &ctxSSH{fn: func(ctx context.Context, host, _ string) ([]byte, error) {
		if host != "devpool-6" {
			arrived.Done()
			select {
			case <-allIn:
				return checkoutAnswer(head, "", nil, 0), nil
			case <-ctx.Done():
			}
		} else {
			<-ctx.Done()
		}
		return nil, &platform.SSHExecError{Hostname: host, Err: fmt.Errorf("ssh stopped: %w (signal: killed)", ctx.Err())}
	}}

	start := time.Now()
	status, err := LeaseStatus(context.Background(), fx.deps, testPoolHost)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("LeaseStatus: %v", err)
	}

	if elapsed > 3*time.Second {
		t.Errorf("status took %s with a %s budget per read", elapsed, budget)
	}
	for _, v := range status.Leases {
		if v.Activity == nil {
			t.Errorf("%s has no activity", v.Alias)
			continue
		}
		if v.Alias == "devpool-6" {
			if v.Activity.LastChangeAt != "" || v.Unsaved != nil || !strings.Contains(v.Activity.Note, "did not finish within "+budget.String()) {
				t.Errorf("devpool-6 = %+v / %+v, want cut at the budget, saying so, with nothing read", v.Activity, v.Unsaved)
			}
			continue
		}
		if v.Activity.LastChangeAt != rfc3339(head) || v.Unsaved == nil {
			t.Errorf("%s = %+v / %+v, want read: the reads run at once", v.Alias, v.Activity, v.Unsaved)
		}
	}
}

// TestLeaseStatus_ActivityNotRead_SaysWhyOrStaysAbsent: status reads a lease
// only through its installed alias, only once the orchestrator confirmed it,
// and only with ssh. A lease missing its alias, or one a pool that could not be
// read leaves unconfirmed, says why it was not read and runs nothing; without
// ssh (the envelope's lease reader) nothing is read and nothing is said.
func TestLeaseStatus_ActivityNotRead_SaysWhyOrStaysAbsent(t *testing.T) {
	t.Parallel()
	const head = int64(1792756800)
	answer := func(context.Context, string, string) ([]byte, error) { return checkoutAnswer(head, "", nil, 0), nil }

	t.Run("alias not installed", func(t *testing.T) {
		t.Parallel()
		ssh := &ctxSSH{fn: answer}
		fx := statusFixture(t, ssh)
		fx.deps.Host = LeaseHost{Run: func(context.Context, string, ...string) error { return nil }, AliasFile: filepath.Join(t.TempDir(), "zcp-leases.conf")}

		status, err := LeaseStatus(context.Background(), fx.deps, testPoolHost)
		if err != nil {
			t.Fatalf("LeaseStatus: %v", err)
		}
		for _, v := range status.Leases {
			if v.Alias != "devpool-3" {
				continue
			}
			if v.State != LeaseStateUnmounted || v.Activity == nil || v.Activity.LastChangeAt != "" || v.Unsaved != nil ||
				v.Activity.AcquiredAt == "" || !strings.Contains(v.Activity.Note, "ssh alias") {
				t.Errorf("devpool-3 = %+v / %+v, want unmounted, not read, the note naming its missing ssh alias", v, v.Activity)
			}
		}
		if hosts := ssh.hosts(); len(hosts) != 0 {
			t.Errorf("ssh ran on %v through an alias that is not installed", hosts)
		}
	})

	t.Run("pool not read leaves it unconfirmed", func(t *testing.T) {
		t.Parallel()
		ssh := &ctxSSH{fn: answer}
		fx := multiPoolFixture(t)
		fx.deps.SSH = ssh

		status, err := LeaseStatus(context.Background(), fx.deps, "")
		if err != nil {
			t.Fatalf("LeaseStatus: %v", err)
		}
		for _, v := range status.Leases {
			switch v.Alias {
			case "stoppool-2":
				if !v.Unverified || v.Activity == nil || v.Activity.LastChangeAt != "" || v.Unsaved != nil ||
					!strings.Contains(v.Activity.Note, "orchestrator") {
					t.Errorf("stoppool-2 = %+v / %+v, want unverified, not read, the note saying its orchestrator did not confirm it", v, v.Activity)
				}
			case "devpool-3":
				if v.Activity == nil || v.Activity.LastChangeAt != rfc3339(head) {
					t.Errorf("devpool-3 = %+v, want read: its orchestrator answered", v.Activity)
				}
			}
		}
		if hosts := ssh.hosts(); len(hosts) != 1 || hosts[0] != "devpool-3" {
			t.Errorf("ssh ran on %v, want devpool-3 only", hosts)
		}
	})

	t.Run("no ssh", func(t *testing.T) {
		t.Parallel()
		fx := statusFixture(t, nil)

		status, err := LeaseStatus(context.Background(), fx.deps, testPoolHost)
		if err != nil {
			t.Fatalf("LeaseStatus: %v", err)
		}
		for _, v := range status.Leases {
			if v.Activity != nil || v.Unsaved != nil {
				t.Errorf("%s = %+v / %+v, want nothing read or said without ssh", v.Alias, v.Activity, v.Unsaved)
			}
		}
	})
}

// TestLeaseActivity_ContextGone_SaysCancelled: a caller that goes away ends
// the read; the note says it was cancelled, not that it ran out of budget.
func TestLeaseActivity_ContextGone_SaysCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	ssh := &ctxSSH{fn: func(ctx context.Context, host, _ string) ([]byte, error) {
		cancel()
		<-ctx.Done()
		return nil, &platform.SSHExecError{Hostname: host, Err: fmt.Errorf("ssh stopped: %w (signal: killed)", ctx.Err())}
	}}
	act, unsaved := readLeaseActivity(ctx, LeaseDeps{SSH: ssh}, leaseRecord{PoolHostname: testPoolHost, ContainerNumber: 3})
	if act == nil || !strings.Contains(act.Note, "cancelled") || strings.Contains(act.Note, "did not finish within") || unsaved != nil {
		t.Errorf("activity = %+v / %+v, want a cancelled read, nothing read", act, unsaved)
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal("the caller's context was not cancelled")
	}
}

// idleNow is the clock the idle tests read: 2026-10-24 12:00 UTC.
var idleNow = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

// before is the time h hours before idleNow.
func before(h float64) time.Time { return idleNow.Add(-time.Duration(h * float64(time.Hour))) }

// noPreviewServer is a capable supervisor's status with no dev server on a
// preview port: the container has no preview to observe.
const noPreviewServer = `{"format":1,"servers":{"worker":{"status":"running"}}}`

// recordLeaseAt writes zcp's record of a lease of repo on pool's container
// c-<number>, acquired at acquired.
func recordLeaseAt(t *testing.T, stateDir, pool, poolServiceID string, number int64, branch, repo string, acquired time.Time) {
	t.Helper()
	rec := leaseRecord{
		PoolHostname: pool, PoolServiceID: poolServiceID,
		ContainerID: "c-" + strconv.FormatInt(number, 10), ContainerNumber: number,
		ContainerHostname: "node-id-" + strconv.FormatInt(number, 10) + ".runtime." + pool + ".zerops",
		Lease:             testLease, Branch: branch, Repo: topology.CanonicalRepoURL(repo), AcquiredAt: acquired.UTC().Format(time.RFC3339),
	}
	if err := writeLeaseRecord(stateDir, rec); err != nil {
		t.Fatalf("write record %s: %v", rec.alias(), err)
	}
}

// idleAnswer is a whole read of a checkout: last changed changed, holding
// porcelain and unpushed commits, its supervisor reporting the preview
// server's last connection (none when lastConn is zero) and open count.
func idleAnswer(changed time.Time, porcelain []string, unpushed int, lastConn time.Time, open int) string {
	last := ""
	if !lastConn.IsZero() {
		last = lastConn.UTC().Format(time.RFC3339)
	}
	return string(checkoutAnswer(changed.Unix(), "", porcelain, unpushed)) + previewPart(capableVersion, 0, previewState(last, intPtr(open)), 0)
}

// idleFixture is devpool holding lease #3 of testRepo's feat-a, acquired at
// acquired, whose checkout answers answer, read at idleNow.
func idleFixture(t *testing.T, acquired time.Time, answer string) *leaseFixture {
	t.Helper()
	fx := newLeaseFixture(t)
	recordLeaseAt(t, fx.stateDir, testPoolHost, "svc-pool", 3, "feat-a", testRepo, acquired)
	fx.swarm.containers = []platform.SwarmContainer{reservedContainer("c-3", 3)}
	fx.deps.Now = func() time.Time { return idleNow }
	fx.deps.SSH = &ctxSSH{fn: func(context.Context, string, string) ([]byte, error) { return []byte(answer), nil }}
	return fx
}

// statusLease is the lease alias of one status of fx's pool.
func statusLease(t *testing.T, fx *leaseFixture, pool, alias string) LeaseView {
	t.Helper()
	status, err := LeaseStatus(context.Background(), fx.deps, pool)
	if err != nil {
		t.Fatalf("LeaseStatus: %v", err)
	}
	for _, v := range status.Leases {
		if v.Alias == alias {
			return v
		}
	}
	t.Fatalf("leases = %+v, want %s", status.Leases, alias)
	return LeaseView{}
}

// TestLeaseStatus_IdleNothingUnsaved_NamesTheRelease: a lease zcp holds that
// nothing has happened in for 24 h or more — no checkout change, no preview
// connection, no acquire, and no preview connection open — is marked idle
// since its newest activity, and with nothing a release would lose it names
// the release call. A capable supervisor with no dev server on a preview port
// leaves the checkout to decide.
func TestLeaseStatus_IdleNothingUnsaved_NamesTheRelease(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name      string
		answer    string
		wantSince time.Time
		wantFor   string
	}{
		{name: "checkout 30 h ago, the preview's last connection 40 h ago, none open",
			answer: idleAnswer(before(30), nil, 0, before(40), 0), wantSince: before(30), wantFor: "for 30 h"},
		{name: "the preview's last connection is the newest fact",
			answer: idleAnswer(before(72), nil, 0, before(25), 0), wantSince: before(25), wantFor: "for 25 h"},
		{name: "exactly 24 h", answer: idleAnswer(before(24), nil, 0, time.Time{}, 0), wantSince: before(24), wantFor: "for 24 h"},
		{name: "no dev server on a preview port: the checkout decides",
			answer:    string(checkoutAnswer(before(80).Unix(), "", nil, 0)) + previewPart(capableVersion, 0, noPreviewServer, 0),
			wantSince: before(80), wantFor: "for 3 d"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fx := idleFixture(t, before(200), tt.answer)

			v := statusLease(t, fx, testPoolHost, "devpool-3")

			if v.Idle == nil {
				t.Fatalf("devpool-3 = %+v / %+v, want it marked idle", v, v.Activity)
			}
			if v.Idle.Since != tt.wantSince.UTC().Format(time.RFC3339) {
				t.Errorf("idle since %s, want %s", v.Idle.Since, tt.wantSince.UTC().Format(time.RFC3339))
			}
			for _, want := range []string{tt.wantFor, "no uncommitted, untracked or unpushed work", "zerops_lease action=release lease=devpool-3", "ignores and stashes"} {
				if !strings.Contains(v.Idle.Next, want) {
					t.Errorf("idle next %q lacks %q", v.Idle.Next, want)
				}
			}
			for _, banned := range []string{"force", "branch="} {
				if strings.Contains(v.Idle.Next, banned) {
					t.Errorf("idle next %q carries %q", v.Idle.Next, banned)
				}
			}
		})
	}
}

// TestLeaseStatus_ActiveOrUnread_NeverIdle: a lease with activity within the
// last 24 h — a checkout change, a preview connection, its acquire — or a
// preview connection open now (in use, however old the connection) is never
// idle, and neither is one whose facts were not all read: a missing fact is
// never no activity. Its activity is still reported.
func TestLeaseStatus_ActiveOrUnread_NeverIdle(t *testing.T) {
	t.Parallel()
	old := before(72)
	checkout := string(checkoutAnswer(old.Unix(), "", nil, 0))
	for _, tt := range []struct {
		name     string
		acquired time.Time
		answer   string
	}{
		{name: "a checkout change 23 h 59 min ago", acquired: before(200), answer: idleAnswer(idleNow.Add(-24*time.Hour+time.Minute), nil, 0, time.Time{}, 0)},
		{name: "a preview connection open now, opened days ago", acquired: before(200), answer: idleAnswer(old, nil, 0, before(100), 1)},
		{name: "a preview connection 2 h ago", acquired: before(200), answer: idleAnswer(old, nil, 0, before(2), 0)},
		{name: "acquired an hour ago", acquired: before(1), answer: idleAnswer(old, nil, 0, time.Time{}, 0)},
		{name: "an older supervisor: the preview not observed", acquired: before(200), answer: checkout + previewPart(olderVersion, 0, previewState("", intPtr(0)), 0)},
		{name: "the supervisor did not answer", acquired: before(200), answer: checkout + previewPart(capableVersion, 0, "", 124)},
		{name: "no supervisor runs", acquired: before(200), answer: checkout + previewPart(capableVersion, 0, `{"code":2,"error":"no zcp-devsup is running here"}`, 2)},
		{name: "no supervisor part", acquired: before(200), answer: checkout},
		{name: "the checkout's last change unknown", acquired: before(200),
			answer: "@@zcp-lease activity 0 \n@@zcp-lease status\n@@zcp-lease unpushed\n0\n" + previewPart(capableVersion, 0, noPreviewServer, 0)},
		{name: "the unsaved work not read", acquired: before(200),
			answer: fmt.Sprintf("@@zcp-lease activity %d \n@@zcp-lease status\n", old.Unix()) + previewPart(capableVersion, 0, noPreviewServer, 0)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fx := idleFixture(t, tt.acquired, tt.answer)

			v := statusLease(t, fx, testPoolHost, "devpool-3")

			if v.Idle != nil {
				t.Errorf("devpool-3 marked idle %+v, from activity %+v / unsaved %+v", *v.Idle, v.Activity, v.Unsaved)
			}
			if v.Activity == nil || v.Activity.AcquiredAt == "" {
				t.Errorf("activity = %+v, want it still reported", v.Activity)
			}
		})
	}
}

// TestLeaseStatus_IdleWithUnsaved_NamesWhatAndNeverForce: an idle lease
// holding work a release would lose is marked idle with what it holds — the
// files (the first 20 by name, all counted) and the commits on no remote
// branch — and how to keep it, HQ's push rule on an HQ repository. It never
// names a release, forced or not.
func TestLeaseStatus_IdleWithUnsaved_NamesWhatAndNeverForce(t *testing.T) {
	t.Parallel()
	var many []string
	for i := 1; i <= 25; i++ {
		many = append(many, fmt.Sprintf("?? file%02d.txt", i))
	}
	for _, tt := range []struct {
		name   string
		repo   string
		answer string
		want   []string
	}{
		{name: "files and commits", repo: testRepo, answer: idleAnswer(before(50), []string{" M src/App.tsx", "?? notes.txt"}, 2, time.Time{}, 0),
			want: []string{"for 2 d", "2 uncommitted or untracked file(s) ( M src/App.tsx, ?? notes.txt)", "2 commit(s) on no remote branch", "Commit and push it from the container"}},
		{name: "more files than a release names", repo: testRepo, answer: idleAnswer(before(30), many, 0, time.Time{}, 0),
			want: []string{"25 uncommitted or untracked file(s)", "?? file20.txt, first 20 shown"}},
		{name: "a repository on HQ", repo: leaseHQRepo, answer: idleAnswer(before(30), nil, 1, time.Time{}, 0),
			want: []string{"1 commit(s) on no remote branch", "a branch of your own", "HQ refuses a push of `main`"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fx := newLeaseFixture(t)
			recordLeaseAt(t, fx.stateDir, testPoolHost, "svc-pool", 3, "feat-a", tt.repo, before(200))
			fx.swarm.containers = []platform.SwarmContainer{reservedContainer("c-3", 3)}
			fx.deps.Now = func() time.Time { return idleNow }
			fx.deps.HQAddress = func() string { return leaseHQURL }
			fx.deps.SSH = &ctxSSH{fn: func(context.Context, string, string) ([]byte, error) { return []byte(tt.answer), nil }}

			v := statusLease(t, fx, testPoolHost, "devpool-3")

			if v.Idle == nil {
				t.Fatalf("devpool-3 = %+v / %+v / %+v, want it marked idle", v, v.Activity, v.Unsaved)
			}
			for _, want := range tt.want {
				if !strings.Contains(v.Idle.Next, want) {
					t.Errorf("idle next %q lacks %q", v.Idle.Next, want)
				}
			}
			// HQ's own rule names "any force push"; a release's force is force=true.
			for _, banned := range []string{"force=true", "action=release"} {
				if strings.Contains(v.Idle.Next, banned) {
					t.Errorf("idle next %q carries %q: a lease with unsaved work is never offered a release", v.Idle.Next, banned)
				}
			}
		})
	}
}

// idleGroupFixture is devpool and webpool, each with its orchestrator, holding a
// lease of testRepo's feat-x on each (devpool-3, webpool-5), every checkout
// answering by its alias from answers, read at idleNow.
func idleGroupFixture(t *testing.T, answers map[string]string) *leaseFixture {
	t.Helper()
	fx := newLeaseFixture(t)
	pool := func(id, name string) platform.ServiceStack {
		return platform.ServiceStack{ID: id, Name: name, Status: "ACTIVE",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}}
	}
	token := []platform.ServiceEnvVar{{Key: "API_TOKEN", Content: testPoolToken, Sensitive: true}}
	fx.deps.Client = platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "proj"}).
		WithServices([]platform.ServiceStack{pool("svc-pool", testPoolHost), pool("svc-web", "webpool")}).
		WithServiceEnv("svc-pool", token).WithServiceEnv("svc-web", token)
	web := &fakeSwarm{containers: []platform.SwarmContainer{{ID: "c-5", Number: 5, Hostname: "node-id-5.runtime.webpool.zerops", Status: "ACTIVE", ServiceID: "svc-web", LockState: "reserved"}}}
	fx.deps.Swarm = func(baseURL, _ string) SwarmAPI {
		if baseURL == orchestratorURL("webpool") {
			return web
		}
		return fx.swarm
	}
	fx.swarm.containers = []platform.SwarmContainer{reservedContainer("c-3", 3), reservedContainer("c-4", 4)}
	recordLeaseAt(t, fx.stateDir, testPoolHost, "svc-pool", 3, "feat-x", testRepo, before(200))
	recordLeaseAt(t, fx.stateDir, "webpool", "svc-web", 5, "feat-x", testRepo, before(190))
	fx.deps.Now = func() time.Time { return idleNow }
	fx.deps.SSH = &ctxSSH{fn: func(_ context.Context, host, _ string) ([]byte, error) {
		answer, ok := answers[host]
		if !ok {
			return nil, fmt.Errorf("no container answers as %s", host)
		}
		return []byte(answer), nil
	}}
	return fx
}

// TestLeaseStatus_GroupIdle_NamesTheBranchRelease: the leases of one branch
// on several pools (a group) are idle together or not at all: only when every
// one of them was read and is idle, since the newest activity of any. With
// nothing any of them would lose, each names the branch's release (with repo
// when zcp leases the branch for several repositories); with unsaved work,
// what each holds, never a release. A status of one pool reads none of the
// group's other leases, so it marks none of them idle.
func TestLeaseStatus_GroupIdle_NamesTheBranchRelease(t *testing.T) {
	t.Parallel()
	clean30, clean26 := idleAnswer(before(30), nil, 0, time.Time{}, 0), idleAnswer(before(26), nil, 0, time.Time{}, 0)
	for _, tt := range []struct {
		name      string
		answers   map[string]string
		pool      string
		otherRepo bool     // devpool-4 holds feat-x of another repository
		wantIdle  []string // aliases marked idle
		want      map[string][]string
		banned    []string
	}{
		{name: "every lease idle and clean", answers: map[string]string{"devpool-3": clean30, "webpool-5": clean26},
			wantIdle: []string{"devpool-3", "webpool-5"},
			want: map[string][]string{
				"devpool-3": {"for 26 h", "devpool-3 and webpool-5", "zerops_lease action=release branch=feat-x releases them all"},
				"webpool-5": {"zerops_lease action=release branch=feat-x releases them all"},
			},
			banned: []string{"force", "lease=", "repo="}},
		{name: "one lease in use now", answers: map[string]string{"devpool-3": clean30, "webpool-5": idleAnswer(before(26), nil, 0, before(50), 1)}},
		{name: "one lease changed an hour ago", answers: map[string]string{"devpool-3": clean30, "webpool-5": idleAnswer(before(1), nil, 0, time.Time{}, 0)}},
		{name: "one lease not read", answers: map[string]string{"devpool-3": clean30}},
		{name: "one lease holds unsaved work", answers: map[string]string{"devpool-3": idleAnswer(before(30), nil, 1, time.Time{}, 0), "webpool-5": clean26},
			wantIdle: []string{"devpool-3", "webpool-5"},
			want: map[string][]string{
				"devpool-3": {"devpool-3 holds 1 commit(s) on no remote branch", "webpool-5 holds no unsaved work", "each container that holds it"},
				"webpool-5": {"devpool-3 holds 1 commit(s) on no remote branch"},
			},
			banned: []string{"force", "action=release"}},
		{name: "a status of one pool", pool: testPoolHost, answers: map[string]string{"devpool-3": clean30, "webpool-5": clean26}},
		{name: "the branch leased for two repositories", otherRepo: true,
			answers:  map[string]string{"devpool-3": clean30, "webpool-5": clean26, "devpool-4": clean30},
			wantIdle: []string{"devpool-3", "devpool-4", "webpool-5"},
			want: map[string][]string{
				"devpool-3": {"zerops_lease action=release branch=feat-x repo=https://github.com/acme/app releases them all"},
				"devpool-4": {"zerops_lease action=release lease=devpool-4"},
			}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fx := idleGroupFixture(t, tt.answers)
			if tt.otherRepo {
				recordLeaseAt(t, fx.stateDir, testPoolHost, "svc-pool", 4, "feat-x", "https://github.com/acme/other", before(200))
			}

			status, err := LeaseStatus(context.Background(), fx.deps, tt.pool)
			if err != nil {
				t.Fatalf("LeaseStatus: %v", err)
			}

			var idle []string
			for _, v := range status.Leases {
				if v.Idle == nil {
					continue
				}
				idle = append(idle, v.Alias)
				for _, want := range tt.want[v.Alias] {
					if !strings.Contains(v.Idle.Next, want) {
						t.Errorf("%s idle next %q lacks %q", v.Alias, v.Idle.Next, want)
					}
				}
				for _, banned := range tt.banned {
					if strings.Contains(v.Idle.Next, banned) {
						t.Errorf("%s idle next %q carries %q", v.Alias, v.Idle.Next, banned)
					}
				}
				if v.Alias != "devpool-4" && v.Idle.Since != before(26).Format(time.RFC3339) {
					t.Errorf("%s idle since %s, want the group's newest activity %s", v.Alias, v.Idle.Since, before(26).Format(time.RFC3339))
				}
			}
			slices.Sort(idle)
			if !slices.Equal(idle, tt.wantIdle) {
				t.Errorf("idle = %v, want %v", idle, tt.wantIdle)
			}
		})
	}
}

// TestLeaseStatus_NeverMutates: a status that marks leases idle — one with
// nothing unsaved, one with unsaved work — beside a lost lease and another
// holder's container changes nothing anywhere: the orchestrator is only
// listed (no release, delete, acquire or rollout), no record or alias is
// written, no host command runs, and the only ssh is each held lease's
// read-only checkout read.
func TestLeaseStatus_NeverMutates(t *testing.T) {
	t.Parallel()
	answers := map[string]string{
		"devpool-3": idleAnswer(before(30), nil, 0, time.Time{}, 0),
		"devpool-4": idleAnswer(before(30), []string{" M src/App.tsx"}, 1, time.Time{}, 0),
	}
	ssh := &ctxSSH{fn: func(_ context.Context, host, _ string) ([]byte, error) {
		if a, ok := answers[host]; ok {
			return []byte(a), nil
		}
		return nil, fmt.Errorf("no container answers as %s", host)
	}}
	fx := newLeaseFixture(t)
	recordLeaseAt(t, fx.stateDir, testPoolHost, "svc-pool", 3, "feat-a", testRepo, before(200))
	recordLeaseAt(t, fx.stateDir, testPoolHost, "svc-pool", 4, "feat-b", testRepo, before(200))
	recordLeaseAt(t, fx.stateDir, testPoolHost, "svc-pool", 6, "feat-c", testRepo, before(200)) // its container is gone: lost
	fx.swarm.containers = []platform.SwarmContainer{reservedContainer("c-3", 3), reservedContainer("c-4", 4), reservedContainer("c-7", 7)}
	fx.deps.Now = func() time.Time { return idleNow }
	fx.deps.SSH = ssh
	aliasFile := filepath.Join(t.TempDir(), "zcp-leases.conf")
	if err := os.WriteFile(aliasFile, []byte("Host devpool-3\nHost devpool-4\nHost devpool-6\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var hostCommands []string
	var hostMu sync.Mutex
	fx.deps.Host = LeaseHost{AliasFile: aliasFile, Run: func(_ context.Context, name string, args ...string) error {
		hostMu.Lock()
		defer hostMu.Unlock()
		hostCommands = append(hostCommands, name+" "+strings.Join(args, " "))
		return nil
	}}
	snapshot := func() map[string]string {
		files := map[string]string{}
		for _, dir := range []string{leaseDir(fx.stateDir), filepath.Dir(aliasFile)} {
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				p := filepath.Join(dir, e.Name())
				raw, err := os.ReadFile(p)
				if err != nil {
					t.Fatal(err)
				}
				info, err := e.Info()
				if err != nil {
					t.Fatal(err)
				}
				files[p] = string(raw) + "@" + info.ModTime().String()
			}
		}
		return files
	}
	was := snapshot()

	for _, pool := range []string{testPoolHost, ""} {
		status, err := LeaseStatus(context.Background(), fx.deps, pool)
		if err != nil {
			t.Fatalf("LeaseStatus(%q): %v", pool, err)
		}
		idle := map[string]bool{}
		for _, v := range status.Leases {
			idle[v.Alias] = v.Idle != nil
		}
		if !idle["devpool-3"] || !idle["devpool-4"] || idle["devpool-6"] || idle["devpool-7"] {
			t.Errorf("status(%q) idle = %v, want devpool-3 and devpool-4 only", pool, idle)
		}
	}

	if len(fx.swarm.releases)+len(fx.swarm.deletes)+len(fx.swarm.acquireReqs)+fx.swarm.rollouts != 0 {
		t.Errorf("status changed the pool: releases %v, deletes %v, acquires %v, rollouts %d",
			fx.swarm.releases, fx.swarm.deletes, fx.swarm.acquireReqs, fx.swarm.rollouts)
	}
	if hostCommands != nil {
		t.Errorf("status ran host commands %q", hostCommands)
	}
	if now := snapshot(); !maps.Equal(now, was) {
		t.Errorf("status wrote files:\nbefore %v\nafter  %v", was, now)
	}
	ssh.mu.Lock()
	defer ssh.mu.Unlock()
	for _, c := range ssh.calls {
		if c.command != leaseActivityCommand() {
			t.Errorf("ssh on %s ran %q, want only the read-only checkout read", c.hostname, c.command)
		}
	}
}
