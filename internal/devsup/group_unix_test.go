//go:build linux || darwin

package devsup

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// startHelper starts a helper group and returns it with the pids it printed.
func startHelper(t *testing.T, mode string, port int) (*group, int, int) {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "server.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	g, err := startGroup(helperCmd(t, mode, "PORT="+strconv.Itoa(port)), dir, os.Environ(), f)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stopGroup(g.pgid, 0) })
	re := regexp.MustCompile(`listening pid=(\d+) grandchild=(\d+)`)
	var m []string
	eventually(t, "helper to listen", func() bool {
		b, _ := os.ReadFile(logPath)
		m = re.FindStringSubmatch(string(b))
		return m != nil && listening(port)
	})
	leader, _ := strconv.Atoi(m[1])
	grandchild, _ := strconv.Atoi(m[2])
	return g, leader, grandchild
}

func TestStopGroup_StoppingADevServerEndsItsWholeProcessGroupAndFreesItsPort(t *testing.T) {
	t.Parallel()
	port := freePort(t)
	g, leader, grandchild := startHelper(t, "listen", port)

	if err := stopGroup(g.pgid, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	<-g.done
	if processAlive(grandchild) {
		t.Fatalf("grandchild %d survived the stop", grandchild)
	}
	if processAlive(leader) {
		t.Fatalf("leader %d survived the stop", leader)
	}
	if !portFree(port) {
		t.Fatalf("port %d is still held", port)
	}
}

func TestStopGroup_KillsAGroupThatIgnoresTermAfterGrace(t *testing.T) {
	t.Parallel()
	port := freePort(t)
	g, _, grandchild := startHelper(t, "ignore-term", port)

	start := time.Now()
	if err := stopGroup(g.pgid, 300*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < 300*time.Millisecond {
		t.Fatalf("stopped after %v, before the grace period", elapsed)
	}
	<-g.done
	if processAlive(grandchild) {
		t.Fatalf("grandchild %d survived SIGKILL", grandchild)
	}
	if !portFree(port) {
		t.Fatalf("port %d is still held", port)
	}
}

func TestOwnsGroup_OnlyTheLeaderWithTheRecordedStartTime(t *testing.T) {
	t.Parallel()
	port := freePort(t)
	g, _, grandchild := startHelper(t, "listen", port)

	if g.leaderStart == 0 {
		t.Fatal("the leader's start time was not recorded")
	}
	if !ownsGroup(g.pgid, g.leaderStart) {
		t.Fatal("the group it started is not recognised")
	}
	if ownsGroup(g.pgid, g.leaderStart+1) {
		t.Fatal("a leader with another start time (a reused pid) is claimed")
	}
	if ownsGroup(grandchild, g.leaderStart) {
		t.Fatal("a member that does not lead the group is claimed")
	}
}

func TestWaitPortFree_WaitsUntilTheHolderLetsGo(t *testing.T) {
	t.Parallel()
	port := freePort(t)
	g, _, _ := startHelper(t, "listen", port)

	if waitPortFree(port, 200*time.Millisecond) {
		t.Fatal("port reported free while the helper listens")
	}
	go func() { _ = stopGroup(g.pgid, 5*time.Second) }()
	if !waitPortFree(port, 10*time.Second) {
		t.Fatal("port never became free after the stop")
	}
}
