package workflow

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/zeropsio/zcp/internal/topology"
)

// WH-COPY — The where text names each working copy with its part, branch, what is uncommitted and unpushed, its change and each dev server's state, the caller's own first and marked, and is empty when there is nothing to name.
// witness: internal/workflow/where.go | worktree.Name == alias → worktree.Part == alias
func TestWhere_WHCOPY_NamesWorkingCopies(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	change := &HQChangeLine{Number: 42, Branch: "feat/b", Labels: []string{"draft", "behind main"}}
	copies := []WhereCopy{
		{Name: "pool-1", Part: "api", Branch: "feat/a", Lease: true, State: "held", Unsaved: &topology.LeaseUnsaved{FileCount: 2, UnpushedCommits: 3}, HasHQ: true, DevServers: []topology.DevServerState{{Name: "api", Status: "running", URL: "http://api:3000"}, {Name: "jobs", Status: "stopped"}}},
		{Name: "pool-2", Part: "web", Branch: "feat/b", Lease: true, State: "held", Unsaved: &topology.LeaseUnsaved{}, HasHQ: true, Change: change, DevServers: []topology.DevServerState{}},
		{Name: "webdev", Part: "web", Branch: "main", Dirty: true, DevServers: []topology.DevServerState{{Name: "web", Status: "starting"}}},
	}
	header := "Where the work stands, read by zcp at 2026-11-25T12:00:00Z. A tool result you received after that is newer."
	first := "pool-1: part api, branch feat/a, 2 files uncommitted, 3 commits unpushed, no change yet; dev server api running at http://api:3000, dev server jobs stopped"
	second := "pool-2: part web, branch feat/b, clean, change #42 (draft · behind main); no dev server"
	dev := "webdev: part web, branch main, uncommitted changes; dev server web starting"
	for _, tc := range []struct {
		name  string
		facts WhereFacts
		want  string
	}{
		{"named second", WhereFacts{ReadAt: at, Lease: "pool-2", Copies: copies}, header + "\n" + strings.Replace(second, "pool-2:", "pool-2 (this conversation's):", 1) + "\n" + first + "\n" + dev},
		{"no alias", WhereFacts{ReadAt: at, Copies: copies}, header + "\n" + first + "\n" + second + "\n" + dev},
		{"unknown alias", WhereFacts{ReadAt: at, Lease: "missing", Copies: copies}, header + "\n" + first + "\n" + second + "\n" + dev},
		{"parked and landed", WhereFacts{ReadAt: at, Copies: []WhereCopy{{Name: "pool-3", Part: "api", Branch: "feat/c", Lease: true, State: "parked", Unverified: true, HasHQ: true, Change: &HQChangeLine{Number: 43, LandedWith: 41}, ServersUnread: "lease parked"}}, Changes: []HQChangeLine{{Number: 44, Branch: "feat/d", Labels: []string{"draft"}}}}, header + "\n" + "pool-3: part api, branch feat/c, parked (unverified), change #43 merged as part of #41; dev servers not read (lease parked)\nchange #44 feat/d (draft): no working copy"},
		{"empty", WhereFacts{ReadAt: at}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := RenderWhere(tc.facts); got != tc.want {
				t.Errorf("where text:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}

// WH-UNREAD — A fact that was not read says not read with why, never clean, none or nothing.
// witness: internal/workflow/where.go | work = "working copy not read (no answer)" → work = "clean"
func TestWhere_WHUNREAD_ExplainsMissingFacts(t *testing.T) {
	t.Parallel()
	facts := WhereFacts{
		ReadAt: time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC),
		Copies: []WhereCopy{{Name: "pool-1", Part: "api", Branch: "feat", Lease: true, State: "held", ServersUnread: "ssh refused"}},
		Stages: []WhereStage{{Hostname: "apistage", Part: "api", Unread: "platform refused"}},
	}
	want := "Where the work stands, read by zcp at 2026-11-25T12:00:00Z. A tool result you received after that is newer.\n" +
		"pool-1: part api, branch feat, working copy not read (no answer); dev servers not read (ssh refused)\n" +
		"stage apistage (part api) version not read (platform refused)"
	if got := RenderWhere(facts); got != want {
		t.Errorf("where text:\n%s\nwant:\n%s", got, want)
	}
}

// WH-STAGE — A stage's line says the name of the version it runs exactly as the platform holds it, and says when it runs none.
// witness: internal/workflow/where.go | version := "runs " + stage.Version → version := "runs " + strings.Split(stage.Version, " ")[0]
func TestWhere_WHSTAGE_PreservesActiveVersionName(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ version, want string }{
		{"main 7e2d4c1", "runs main 7e2d4c1"},
		{"feat/x 1a2b3c4-dirty", "runs feat/x 1a2b3c4-dirty"},
		{"release 20260930", "runs release 20260930"},
		{"", "runs no deployed version"},
	} {
		t.Run(tc.version, func(t *testing.T) {
			t.Parallel()
			facts := WhereFacts{ReadAt: time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC), Stages: []WhereStage{{Hostname: "apistage", Part: "api", Version: tc.version}}}
			want := "Where the work stands, read by zcp at 2026-11-25T12:00:00Z. A tool result you received after that is newer.\nstage apistage (part api) " + tc.want
			if got := RenderWhere(facts); got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

// WH-SIZE — The text never passes 1024 bytes: line 1 and the caller's copy stay, later whole lines give way to one line counting them.
// witness: internal/workflow/where.go | lines = lines[:len(lines)-1] → lines = lines[1:]
func TestWhere_WHSIZE_KeepsNamedCopyAndWholeLines(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	aliases := []string{"pool-01", "pool-02", "pool-03", "pool-04", "pool-05", "pool-06", "pool-07", "pool-08", "pool-09", "pool-10", "pool-11", "pool-12", "pool-13", "pool-14", "pool-15", "pool-16", "pool-17", "pool-18", "pool-19", "pool-20", "pool-21", "pool-22", "pool-23", "pool-24", "pool-25", "pool-26", "pool-27", "pool-28", "pool-29", "pool-30"}
	for _, branch := range []string{"feature-with-a-long-name-for-work", "feature-\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c"} {
		t.Run(branch, func(t *testing.T) {
			t.Parallel()
			facts := WhereFacts{ReadAt: at, Lease: "pool-30"}
			for _, alias := range aliases {
				facts.Copies = append(facts.Copies, WhereCopy{Name: alias, Part: "api", Branch: branch, Lease: true, State: "held", Unsaved: &topology.LeaseUnsaved{}, DevServers: []topology.DevServerState{}})
			}
			got := RenderWhere(facts)
			lines := strings.Split(got, "\n")
			if len(got) > WhereBudgetBytes || !utf8.ValidString(got) || !strings.HasPrefix(lines[0], "Where the work stands, read by zcp at ") || !strings.HasPrefix(lines[1], "pool-30 (this conversation's):") || !strings.HasPrefix(lines[2], "pool-01:") {
				t.Fatalf("bound or order: %d bytes\n%s", len(got), got)
			}
			wantLast := fmt.Sprintf("%d more lines: zerops_workflow action=\"status\" lists them.", 31-len(lines)+1)
			if lines[len(lines)-1] != wantLast {
				t.Errorf("last line %q, want %q", lines[len(lines)-1], wantLast)
			}
		})
	}
	t.Run("oversized server list", func(t *testing.T) {
		t.Parallel()
		servers := make([]topology.DevServerState, 40)
		for i := range servers {
			servers[i] = topology.DevServerState{Name: "api", Status: "running"}
		}
		facts := WhereFacts{ReadAt: at, Lease: "pool-1", Copies: []WhereCopy{{Name: "pool-1", Part: "api", Branch: "feature", Lease: true, State: "held", Unsaved: &topology.LeaseUnsaved{}, DevServers: servers}}}
		got := RenderWhere(facts)
		if len(got) > WhereBudgetBytes || !strings.Contains(got, "pool-1 (this conversation's):") || !strings.Contains(got, "dev server api running") || !strings.HasSuffix(got, "more (zcp status)") {
			t.Errorf("oversized server list: %d bytes\n%s", len(got), got)
		}
	})
}
