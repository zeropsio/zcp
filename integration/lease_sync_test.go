// Tests for: integration — zerops_lease action=sync through the full MCP
// server: the branch pushed from the lease that committed, every other lease
// of it fast-forwarded (docs/spec-workflows.md §8 DP-24).

package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

const (
	syncIntFrom = "1111111111111111111111111111111111111111"
	syncIntTo   = "2222222222222222222222222222222222222222"
)

// syncLeaseSSH is the leased containers as a sync reaches them: dirty names
// the aliases whose checkout holds an untracked file; the push is accepted
// and every member fast-forwards; every other command (checkout, prepare,
// supervisor) answers ok. It records the host of every push and fast-forward.
type syncLeaseSSH struct {
	mu     sync.Mutex
	dirty  map[string]bool
	pushes []string
	ffs    []string
}

func (s *syncLeaseSSH) ExecSSH(_ context.Context, hostname, command string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case strings.Contains(command, "status --porcelain"):
		if s.dirty[hostname] {
			return []byte("?? notes.txt\n"), nil
		}
		return nil, nil
	case strings.Contains(command, "rev-list --count --branches"):
		return []byte("1\n"), nil
	case strings.Contains(command, "push --porcelain"):
		s.pushes = append(s.pushes, hostname)
		return []byte("@@zcp-sync commit " + syncIntTo + "\nTo https://github.com/acme/app\n \trefs/heads/feat-x:refs/heads/feat-x\t1111111..2222222\nDone\n"), nil
	case strings.Contains(command, "merge --ff-only"):
		s.ffs = append(s.ffs, hostname)
		return []byte("@@zcp-sync from " + syncIntFrom + "\n@@zcp-sync origin " + syncIntTo + "\n@@zcp-sync to " + syncIntTo + "\n"), nil
	}
	return []byte("ok"), nil
}

// ExecSSHStdin answers one command as ExecSSH does; the input is not read.
func (s *syncLeaseSSH) ExecSSHStdin(ctx context.Context, host, command string, _ []byte) ([]byte, error) {
	return s.ExecSSH(ctx, host, command)
}

func (s *syncLeaseSSH) ExecSSHBackground(ctx context.Context, hostname, command string, _ time.Duration) ([]byte, error) {
	return s.ExecSSH(ctx, hostname, command)
}

// TestLease_Integration_Sync_PushesThenFastForwardsTheBranchsOtherLeases:
// through the full MCP server, a sync of a lease whose checkout holds an
// untracked file is refused with nothing pushed; once clean, the sync pushes
// the branch from it and fast-forwards the branch's other lease, the result
// naming each step's outcome and duration and never a lease or a bearer.
func TestLease_Integration_Sync_PushesThenFastForwardsTheBranchsOtherLeases(t *testing.T) {
	// Non-parallel: t.Chdir and a swap of http.DefaultTransport, as above.
	t.Chdir(t.TempDir())

	hqTarget, _ := groupPoolOrchestrator(t, "hqpool", "3")
	webTarget, _ := groupPoolOrchestrator(t, "webpool", "7")
	orig := http.DefaultTransport
	http.DefaultTransport = orchestratorRoutes{targets: map[string]*url.URL{"orch.hqpool.zerops": hqTarget, "orch.webpool.zerops": webTarget}, next: orig}
	t.Cleanup(func() { http.DefaultTransport = orig })

	mock := defaultMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-hqpool", Name: "hqpool", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}},
			{ID: "svc-webpool", Name: "webpool", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}},
		}).
		WithServiceEnv("svc-hqpool", []platform.ServiceEnvVar{{ID: "env-h", Key: "API_TOKEN", Content: leaseIntToken, Sensitive: true}}).
		WithServiceEnv("svc-webpool", []platform.ServiceEnvVar{{ID: "env-w", Key: "API_TOKEN", Content: leaseIntToken, Sensitive: true}})
	ssh := &syncLeaseSSH{dirty: map[string]bool{"hqpool-3": true}}
	session, cleanup := setupLeaseTestServer(t, mock, defaultLogFetcher(), ssh)
	defer cleanup()

	res := callAndGetResult(t, session, "zerops_lease", map[string]any{"action": "acquire", "pools": []string{"hqpool", "webpool"}, "repo": "https://github.com/acme/app", "branch": "feat-x"})
	if text := getTextContent(t, res); res.IsError {
		t.Fatalf("group acquire: %s", text)
	}

	res = callAndGetResult(t, session, "zerops_lease", map[string]any{"action": "sync", "lease": "hqpool-3"})
	text := getTextContent(t, res)
	if !res.IsError {
		t.Fatalf("want the sync refused while hqpool-3 holds an untracked file, got: %s", text)
	}
	for _, want := range []string{"PREREQUISITE_MISSING", "hqpool-3", "notes.txt"} {
		if !strings.Contains(text, want) {
			t.Errorf("refusal %s lacks %q", text, want)
		}
	}
	ssh.mu.Lock()
	refusedReached := len(ssh.pushes) + len(ssh.ffs)
	ssh.dirty = nil
	ssh.mu.Unlock()
	if refusedReached != 0 {
		t.Errorf("a refused sync pushed or fast-forwarded")
	}

	res = callAndGetResult(t, session, "zerops_lease", map[string]any{"action": "sync", "lease": "hqpool-3"})
	text = getTextContent(t, res)
	if res.IsError {
		t.Fatalf("sync: %s", text)
	}
	if strings.Contains(text, leaseIntLease) || strings.Contains(text, leaseIntToken) {
		t.Fatalf("result carries a secret: %s", text)
	}
	var got struct {
		Action string `json:"action"`
		Sync   struct {
			Branch  string `json:"branch"`
			Source  string `json:"source"`
			Commit  string `json:"commit"`
			CheckMs *int64 `json:"checkMs"`
			Ms      *int64 `json:"ms"`
			Push    struct {
				Outcome string `json:"outcome"`
				Ms      *int64 `json:"ms"`
			} `json:"push"`
			Members []struct {
				Alias   string `json:"alias"`
				Outcome string `json:"outcome"`
				From    string `json:"from"`
				To      string `json:"to"`
				Ms      *int64 `json:"ms"`
			} `json:"members"`
		} `json:"sync"`
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, text)
	}
	s := got.Sync
	if got.Action != "sync" || s.Branch != "feat-x" || s.Source != "hqpool-3" || s.Commit != syncIntTo || s.Push.Outcome != "pushed" {
		t.Errorf("sync = %+v, want feat-x pushed from hqpool-3 at %s", s, syncIntTo)
	}
	if len(s.Members) != 1 || s.Members[0].Alias != "webpool-7" || s.Members[0].Outcome != "fast-forwarded" ||
		s.Members[0].From != syncIntFrom || s.Members[0].To != syncIntTo {
		t.Errorf("members = %+v, want webpool-7 fast-forwarded %s..%s", s.Members, syncIntFrom, syncIntTo)
	}
	if s.CheckMs == nil || s.Ms == nil || s.Push.Ms == nil || len(s.Members) == 0 || s.Members[0].Ms == nil {
		t.Errorf("a step's duration is missing: %s", text)
	}
	ssh.mu.Lock()
	defer ssh.mu.Unlock()
	if strings.Join(ssh.pushes, ",") != "hqpool-3" || strings.Join(ssh.ffs, ",") != "webpool-7" {
		t.Errorf("pushes %v, fast-forwards %v; want hqpool-3's push and webpool-7's fast-forward", ssh.pushes, ssh.ffs)
	}
}
