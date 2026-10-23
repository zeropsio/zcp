// Tests for: ops/lease_activity.go — what zerops_lease action=status reads of
// each held lease's checkout: its activity and its unsaved work, from one
// bounded ssh invocation per lease (docs/spec-workflows.md §8 DP-21).
package ops

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
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
	if !strings.HasPrefix(cmd, "cd "+shellQuote(leaseWorkDir)+" && ") {
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
	for _, m := range regexp.MustCompile(`[0-9]?>\s*(\S+)`).FindAllStringSubmatch(cmd, -1) {
		if m[1] != "/dev/null" && m[1] != "&1" {
			t.Errorf("command redirects into %q; a read writes nowhere: %s", m[1], cmd)
		}
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
	for _, tt := range []struct {
		name        string
		out         string // what the call returns beside the error
		err         func(t *testing.T) error
		wantChange  string
		wantUnsaved bool
		wantNote    []string
	}{
		{
			name:     "ssh cannot reach the container",
			err:      func(t *testing.T) error { return sshFailure("ssh: Could not resolve hostname devpool-3", exitError(t, 255)) },
			wantNote: []string{"could not reach the container over ssh", "Could not resolve hostname"},
		},
		{
			name:     "the checkout directory is missing",
			err:      func(t *testing.T) error { return sshFailure("sh: cd: /var/www: No such file or directory", exitError(t, 2)) },
			wantNote: []string{"/var/www", "No such file or directory"},
		},
		{
			name: "git status fails after the activity read",
			out:  fmt.Sprintf("@@zcp-lease activity %d \n@@zcp-lease status\nfatal: not a git repository\n", head),
			err: func(t *testing.T) error {
				return sshFailure(fmt.Sprintf("@@zcp-lease activity %d \n@@zcp-lease status\nfatal: not a git repository\n", head), exitError(t, 128))
			},
			wantChange: rfc3339(head),
			wantNote:   []string{"git status", "fatal: not a git repository"},
		},
		{
			name: "the unpushed count fails, the output only in the error",
			err: func(t *testing.T) error {
				return sshFailure(fmt.Sprintf("@@zcp-lease activity %d \n@@zcp-lease status\n?? a.txt\n@@zcp-lease unpushed\nfatal: bad object HEAD\n", head), exitError(t, 128))
			},
			wantChange: rfc3339(head),
			wantNote:   []string{"commits on no remote branch", "fatal: bad object HEAD"},
		},
		{
			name:     "an answer with no activity line",
			out:      "ok",
			wantNote: []string{"ok"},
		},
		{
			name:       "an unpushed count that is no number",
			out:        fmt.Sprintf("@@zcp-lease activity %d \n@@zcp-lease status\n@@zcp-lease unpushed\nmany\n", head),
			wantChange: rfc3339(head),
			wantNote:   []string{"many"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var err error
			if tt.err != nil {
				err = tt.err(t)
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
			if (unsaved != nil) != tt.wantUnsaved {
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
	ssh := &ctxSSH{fn: func(context.Context, string, string) ([]byte, error) {
		return nil, sshFailure("ssh: connect to host node-id-3.runtime.devpool.zerops port 22: Connection refused", exitError(t, 255))
	}}
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
